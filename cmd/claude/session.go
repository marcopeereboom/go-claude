package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcopeereboom/go-claude/pkg/llm"
)

// initSession sets up all state needed for a conversation.
func initSession(opts *options, claudeDir string) (*session, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating .claude dir: %w", err)
	}

	// Load configuration
	configPath := filepath.Join(claudeDir, "config.json")
	cfg := loadOrCreateConfig(configPath)

	selectedModel := selectModel(opts.model, cfg.Model)
	cfg.Model = selectedModel

	// Validate model exists in cache
	if err := validateModel(selectedModel, claudeDir, opts.ollamaURL); err != nil {
		return nil, err
	}

	sysPrompt := selectSystemPrompt(opts.systemPrompt, cfg.SystemPrompt)

	timestamp := time.Now().Format("20060102_150405")

	if opts.isVerbose() {
		fmt.Fprintf(os.Stderr, "Claude dir: %s\n", claudeDir)
		fmt.Fprintf(os.Stderr, "Model: %s\n", selectedModel)
	}

	// Load conversation history from request/response pairs
	messages, err := loadConversationHistory(claudeDir)
	if err != nil {
		return nil, err
	}

	if opts.isVerbose() {
		fmt.Fprintf(os.Stderr, "Loaded %d messages\n", len(messages))
	}

	// Handle truncation
	if opts.truncate > 0 && len(messages) > opts.truncate {
		if opts.isVerbose() {
			fmt.Fprintf(os.Stderr, "Truncating: %d → %d messages\n",
				len(messages), opts.truncate)
		}
		messages = messages[len(messages)-opts.truncate:]
	}

	// Check context size (will add user message in executeConversation)
	estimatedTokens := estimateTokens(messages)
	if estimatedTokens > maxContextTokens {
		return nil, fmt.Errorf(
			"conversation too large (%d tokens, max %d)\n"+
				"Options:\n"+
				"  claude --reset           # start fresh\n"+
				"  claude --truncate N      # keep last N messages",
			estimatedTokens, maxContextTokens)
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting working dir: %w", err)
	}

	// Detect LLM provider based on model name
	var llmClient llm.LLM
	if strings.HasPrefix(selectedModel, "claude-") {
		llmClient = llm.NewClaude(apiKey, apiURL)
	} else {
		llmClient = llm.NewOllama(selectedModel, opts.ollamaURL)
	}

	return &session{
		opts:       opts,
		claudeDir:  claudeDir,
		apiKey:     apiKey,
		config:     cfg,
		model:      selectedModel,
		sysPrompt:  sysPrompt,
		timestamp:  timestamp,
		workingDir: workingDir,
		client: &http.Client{
			Timeout: time.Duration(opts.timeout) * time.Second,
		},
		llmClient: llmClient,
	}, nil
}

// executeConversation runs the agentic loop with tool support.
func executeConversation(sess *session, userMsg string) (*conversationResult, error) {
	// Load conversation history
	messages, err := loadConversationHistory(sess.claudeDir)
	if err != nil {
		return nil, err
	}

	// Add current user message
	messages = append(messages, MessageContent{
		Role: "user",
		Content: []ContentBlock{{
			Type: "text",
			Text: userMsg,
		}},
	})

	// Save request before calling API
	if err := saveRequest(sess.claudeDir, sess.timestamp, messages); err != nil {
		return nil, fmt.Errorf("saving request: %w", err)
	}

	var responses []json.RawMessage
	iterationCost := 0.0

	maxIter := sess.opts.maxIterations
	if maxIter == 0 {
		maxIter = 1000 // Effective unlimited
	}

	// Agentic loop: iterate until Claude is done or limits reached
	for i := 0; i < maxIter; i++ {
		// Call LLM via unified interface
		req := &llm.Request{
			Model:     sess.model,
			Messages:  messages,
			Tools:     getTools(sess.opts),
			MaxTokens: sess.opts.maxTokens,
			System:    sess.sysPrompt,
		}

		ctx := context.Background()
		llmResp, err := sess.llmClient.Generate(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("LLM API call failed: %w", err)
		}

		// Convert to existing APIResponse format for backward compat
		apiResp := &APIResponse{
			Content:    llmResp.Content,
			StopReason: llmResp.StopReason,
			Usage:      llmResp.Usage,
		}

		// Marshal response for saving
		respBody, err := json.Marshal(apiResp)
		if err != nil {
			return nil, fmt.Errorf("marshaling response: %w", err)
		}

		if apiResp.Error != nil {
			if sess.opts.wantsJSON() {
				fmt.Println(string(respBody))
			}
			return nil, fmt.Errorf("API error [%s]: %s",
				apiResp.Error.Type, apiResp.Error.Message)
		}

		// Track cost this iteration
		costIn := float64(apiResp.Usage.InputTokens) * 3.0 / 1000000
		costOut := float64(apiResp.Usage.OutputTokens) * 15.0 / 1000000
		iterationCost += costIn + costOut

		// Check cost limit
		if sess.opts.maxCost > 0 && iterationCost > sess.opts.maxCost {
			return nil, fmt.Errorf(
				"max cost exceeded ($%.4f > $%.4f) after %d iterations",
				iterationCost, sess.opts.maxCost, i+1)
		}

		// Update token counts
		sess.config.TotalInput += apiResp.Usage.InputTokens
		sess.config.TotalOutput += apiResp.Usage.OutputTokens

		if sess.opts.isVerbose() {
			fmt.Fprintf(os.Stderr,
				"Iteration %d - Tokens: %d in, %d out (cost: $%.4f)\n",
				i+1, apiResp.Usage.InputTokens, apiResp.Usage.OutputTokens,
				costIn+costOut)
		}

		// Add assistant response to messages
		messages = append(messages, MessageContent{
			Role:    "assistant",
			Content: apiResp.Content,
		})

		// Collect all responses
		responses = append(responses, json.RawMessage(respBody))

		// Handle different stop reasons
		switch apiResp.StopReason {
		case "end_turn":
			// Conversation complete - save response
			assistantText := extractResponse(apiResp)

			// Save all responses as array
			responsesJSON, err := json.MarshalIndent(responses, "", "\t")
			if err != nil {
				return nil, fmt.Errorf("marshaling responses: %w", err)
			}
			if err := saveResponse(sess.claudeDir, sess.timestamp, responsesJSON); err != nil {
				return nil, fmt.Errorf("saving responses: %w", err)
			}

			return &conversationResult{
				assistantText: assistantText,
				respBody:      respBody,
			}, nil

		case "tool_use":
			// Execute tools and continue
			toolResults, err := executeTools(apiResp.Content,
				sess.workingDir, sess.opts, sess.timestamp)
			if err != nil {
				return nil, err
			}

			messages = append(messages, MessageContent{
				Role:    "user",
				Content: toolResults,
			})
			// Continue loop

		default:
			return nil, fmt.Errorf("unexpected stop_reason: %s",
				apiResp.StopReason)
		}
	}

	return nil, fmt.Errorf("max iterations (%d) reached", maxIter)
}

// finalizeSession saves all state and outputs the result.
func finalizeSession(sess *session, result *conversationResult) error {
	// Update timestamps
	sess.config.LastRun = sess.timestamp
	if sess.config.FirstRun == "" {
		sess.config.FirstRun = sess.timestamp
	}

	// Save config
	configPath := filepath.Join(sess.claudeDir, "config.json")
	if err := saveJSON(configPath, sess.config); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	// Output result
	return writeOutput(sess.opts.outputFile, sess.opts.wantsJSON(),
		result.assistantText, result.respBody)
}

func selectModel(flagModel, cfgModel string) string {
	switch {
	case flagModel != "":
		return flagModel
	case cfgModel != "":
		return cfgModel
	default:
		return defaultModel
	}
}

func selectSystemPrompt(flagPrompt, cfgPrompt string) string {
	// System prompt priority:
	// 1. --system flag (highest priority - one-time override)
	// 2. CLAUDE_SYSTEM_PROMPT env var (session-level)
	// 3. config.json SystemPrompt (persisted across conversations)
	// 4. defaultSystemPrompt (fallback)

	if flagPrompt != "" {
		return flagPrompt
	}

	if envPrompt := os.Getenv("CLAUDE_SYSTEM_PROMPT"); envPrompt != "" {
		return envPrompt
	}

	if cfgPrompt != "" {
		return cfgPrompt
	}

	return defaultSystemPrompt
}

func estimateTokens(messages []MessageContent) int {
	// Rough estimate: ~4 chars per token
	total := 0
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == "text" {
				total += len(block.Text) / 4
			}
		}
	}
	return total
}

func extractResponse(apiResp *APIResponse) string {
	for _, content := range apiResp.Content {
		if content.Type == "text" {
			return content.Text
		}
	}
	return ""
}
