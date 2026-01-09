// Package main implements a CLI for interacting with Claude AI with tool use,
// conversation management, and local context storage.
package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/marcopeereboom/go-claude/pkg/llm"
)

const (
	defaultModel         = "claude-sonnet-4-20250514"
	apiVersion           = "2023-06-01"
	maxContextTokens     = 100000
	defaultMaxIterations = 15
	defaultMaxCost       = 1.0 // dollars

	// Defaults
	defaultMaxTokens = 8192
	defaultTimeout   = 300

	// Verbosity levels
	verbositySilent  = "silent"
	verbosityNormal  = "normal"
	verbosityVerbose = "verbose"
	verbosityDebug   = "debug"
	defaultVerbosity = verbosityNormal

	// Tool permissions
	toolNone    = "none"
	toolRead    = "read"
	toolWrite   = "write"
	toolCommand = "command"
	toolAll     = "all"
	defaultTool = "" // dry-run

	// Output formats
	outputText    = "text"
	outputJSON    = "json"
	defaultOutput = outputText

	// bash_command timeout
	bashCommandTimeout = 30 * time.Second

	// Default Ollama URL
	defaultOllamaURL = "http://localhost:11434"
)

//go:embed defaultprompt.txt
var defaultSystemPrompt string

// Command whitelist for bash_command tool
var allowedCommands = map[string]bool{
	"ls":   true,
	"cat":  true,
	"grep": true,
	"find": true,
	"head": true,
	"tail": true,
	"wc":   true,
	"echo": true,
	"pwd":  true,
	"date": true,
	"git":  true, // validated separately
	"go":   true, // all go subcommands allowed
}

// apiURL can be overridden in tests
var apiURL = "https://api.anthropic.com/v1/messages"

// API types
type APIRequest struct {
	Model     string      `json:"model"`
	MaxTokens int         `json:"max_tokens"`
	System    string      `json:"system,omitempty"`
	Messages  interface{} `json:"messages"`
	Tools     []Tool      `json:"tools,omitempty"`
}

type APIResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model"`
	StopReason string         `json:"stop_reason"`
	Usage      Usage          `json:"usage"`
	Error      *APIError      `json:"error,omitempty"`
}

type APIError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Type aliases for LLM interface types
type ContentBlock = llm.ContentBlock
type MessageContent = llm.MessageContent
type Tool = llm.Tool
type Usage = llm.Usage

// CLI options
type options struct {
	// Modes
	modelsList    bool
	modelsRefresh bool
	reset         bool
	showStats     bool
	replay        string
	pruneOld      int
	estimate      bool
	execute       bool
	maxCostFlag   float64

	// Core
	maxTokens     int
	maxCost       float64
	maxIterations int
	model         string
	timeout       int
	systemPrompt  string
	truncate      int
	resumeDir     string
	outputFile    string
	ollamaURL     string

	// Behavior
	verbosity string
	tool      string
	output    string
}

// Helper methods for options
func (o *options) isVerbose() bool {
	return o.verbosity == verbosityVerbose || o.verbosity == verbosityDebug
}

func (o *options) isDebug() bool {
	return o.verbosity == verbosityDebug
}

func (o *options) isSilent() bool {
	return o.verbosity == verbositySilent
}

func (o *options) canExecuteWrite() bool {
	if o.tool == "" {
		return false // dry-run
	}
	return strings.Contains(o.tool, toolWrite) || o.tool == toolAll
}

func (o *options) canExecuteCommand() bool {
	if o.tool == "" {
		return false // dry-run
	}
	return strings.Contains(o.tool, toolCommand) || o.tool == toolAll
}

func (o *options) canUseTools() bool {
	return o.tool != toolNone
}

func (o *options) wantsJSON() bool {
	return o.output == outputJSON
}

// session holds all state needed for a conversation execution.
type session struct {
	opts       *options
	claudeDir  string
	apiKey     string
	config     *Config
	model      string
	sysPrompt  string
	timestamp  string
	workingDir string
	client     *http.Client
	llmClient  llm.LLM
}

// conversationResult holds the outcome of a conversation execution.
type conversationResult struct {
	assistantText string
	respBody      []byte
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	opts := parseFlags()

	claudeDir, err := getClaudeDir(opts.resumeDir)
	if err != nil {
		return err
	}

	// Handle models commands first (don't need stdin)
	if opts.modelsList {
		return listModelsCommand(claudeDir, opts.ollamaURL)
	}

	if opts.modelsRefresh {
		return refreshModelsCommand(claudeDir, opts.ollamaURL)
	}

	// Handle --execute mode (use last message from conversation)
	if opts.execute {
		messages, err := loadConversationHistory(claudeDir)
		if err != nil {
			return fmt.Errorf("loading conversation: %w", err)
		}

		var userMsg string

		// Try to get last user message from completed conversation
		if len(messages) > 0 {
			userMsg, err = getLastUserMessage(messages)
			if err != nil {
				return fmt.Errorf("no user message in conversation")
			}
		} else {
			// No complete pairs - check for unpaired request (from --estimate)
			entries, err := os.ReadDir(claudeDir)
			if err != nil {
				return fmt.Errorf("no conversation history")
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "request_") {
					reqPath := filepath.Join(claudeDir, entry.Name())
					var req Request
					data, _ := os.ReadFile(reqPath)
					json.Unmarshal(data, &req)
					userMsg, _ = getLastUserMessage(req.Messages)
					break
				}
			}
			if userMsg == "" {
				return fmt.Errorf("no message to execute")
			}
		}

		// Override max-cost if provided
		if opts.maxCostFlag > 0 {
			opts.maxCost = opts.maxCostFlag
		}

		// Normal execution flow with last user message
		return executeWithSavedInput(userMsg, opts, claudeDir)
	}

	// Handle --estimate mode
	if opts.estimate {
		// Must have stdin
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) != 0 {
			return fmt.Errorf("no input (pipe required)")
		}

		userMsg, err := readInput()
		if err != nil {
			return err
		}

		// Load conversation history
		messages, _ := loadConversationHistory(claudeDir)

		// Get model for pricing
		configPath := filepath.Join(claudeDir, "config.json")
		cfg := loadOrCreateConfig(configPath)
		model := selectModel(opts.model, cfg.Model)

		// Estimate and display
		estimate := estimateCost(userMsg, messages, model)
		displayEstimate(estimate)

		// Save this message to conversation so --execute can use it
		timestamp := time.Now().Format("20060102_150405")
		messages = append(messages, MessageContent{
			Role: "user",
			Content: []ContentBlock{{
				Type: "text",
				Text: userMsg,
			}},
		})
		if err := saveRequest(claudeDir, timestamp, messages); err != nil {
			return fmt.Errorf("saving request: %w", err)
		}

		// Update config
		cfg.Model = model
		cfg.LastRun = timestamp
		if cfg.FirstRun == "" {
			cfg.FirstRun = timestamp
		}
		configPath = filepath.Join(claudeDir, "config.json")
		if err := saveJSON(configPath, cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		return nil
	}

	// Handle special modes that don't need full setup
	if opts.showStats {
		return showStats(claudeDir)
	}

	if opts.reset {
		return resetConversation(claudeDir, opts.isVerbose())
	}

	if opts.replay != "NOREPLAY" {
		return replayResponse(claudeDir, opts)
	}

	if opts.pruneOld > 0 {
		return pruneResponses(claudeDir, opts.pruneOld, opts.isVerbose())
	}

	// Check if stdin is a pipe/redirect, not interactive terminal
	stat, err := os.Stdin.Stat()
	if err != nil {
		return fmt.Errorf("checking stdin: %w", err)
	}
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		// Interactive terminal - no input piped
		flag.Usage()
		return fmt.Errorf("no input provided (pipe or redirect required)")
	}

	// Normal execution
	userMsg, err := readInput()
	if err != nil {
		return err
	}
	return executeWithSavedInput(userMsg, opts, claudeDir)
}

func executeWithSavedInput(userMsg string, opts *options, claudeDir string) error {
	// Initialize session
	sess, err := initSession(opts, claudeDir)
	if err != nil {
		return err
	}

	// Execute conversation with tool support
	result, err := executeConversation(sess, userMsg)
	if err != nil {
		return err
	}

	// Save and output results
	return finalizeSession(sess, result)
}

func parseFlags() *options {
	opts := &options{}

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: claude [options]\n\n")
		fmt.Fprintf(os.Stderr, "A CLI for interacting with Claude AI with tool support.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  # Dry-run (shows what would happen)\n")
		fmt.Fprintf(os.Stderr, "  echo \"add error handling to users.go\" | claude\n\n")
		fmt.Fprintf(os.Stderr, "  # Execute with write permission\n")
		fmt.Fprintf(os.Stderr, "  echo \"add tests\" | claude --tool=write\n\n")
		fmt.Fprintf(os.Stderr, "  # Replay last run and execute everything\n")
		fmt.Fprintf(os.Stderr, "  claude --replay --tool=all\n")
		fmt.Fprintf(os.Stderr, "  claude --replay=20260104_153022 --tool=all\n\n")
		fmt.Fprintf(os.Stderr, "  # Show statistics\n")
		fmt.Fprintf(os.Stderr, "  claude --stats\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	// Modes
	flag.BoolVar(&opts.modelsList, "models-list", false,
		"list available Claude and Ollama models (creates cache if missing)")
	flag.BoolVar(&opts.modelsRefresh, "models-refresh", false,
		"refresh models cache from Claude API and Ollama")
	flag.BoolVar(&opts.reset, "reset", false,
		"reset conversation (delete .claude/ directory)")
	flag.BoolVar(&opts.showStats, "stats", false,
		"show conversation statistics")

	flag.StringVar(&opts.replay, "replay", "NOREPLAY",
		"replay response (empty=latest, or timestamp like 20260104_153022)")
	flag.IntVar(&opts.pruneOld, "prune-old", 0,
		"keep only last N request/response pairs, delete older")

	// Cost estimation
	flag.BoolVar(&opts.estimate, "estimate", false,
		"estimate cost without executing (shows cost for piped input)")
	flag.BoolVar(&opts.execute, "execute", false,
		"re-execute last user message from conversation")
	flag.Float64Var(&opts.maxCostFlag, "max-cost-override", 0,
		"override max-cost for this run (use with --execute)")

	// Core settings
	flag.StringVar(&opts.model, "model", "",
		fmt.Sprintf("model to use (default: %s)", defaultModel))
	flag.IntVar(&opts.maxTokens, "max-tokens", defaultMaxTokens,
		"maximum tokens per API call")
	flag.Float64Var(&opts.maxCost, "max-cost", defaultMaxCost,
		"maximum cost in dollars per conversation (0 = unlimited)")
	flag.IntVar(&opts.maxIterations, "max-iterations", defaultMaxIterations,
		"maximum tool loop iterations (0 = unlimited)")
	flag.IntVar(&opts.timeout, "timeout", defaultTimeout,
		"HTTP timeout in seconds")
	flag.IntVar(&opts.truncate, "truncate", 0,
		"keep only last N messages in conversation (0 = keep all)")
	flag.StringVar(&opts.ollamaURL, "ollama-url", defaultOllamaURL,
		"Ollama API URL")

	// Behavior
	flag.StringVar(&opts.verbosity, "verbosity", defaultVerbosity,
		"output verbosity: silent, normal, verbose, debug")
	flag.StringVar(&opts.tool, "tool", defaultTool,
		"tool permissions: \"\" (dry-run), none, read, write, command, all, or comma-separated")
	flag.StringVar(&opts.output, "output", defaultOutput,
		"output format: text, json")

	// Advanced
	flag.StringVar(&opts.systemPrompt, "system", "",
		"custom system prompt")
	flag.StringVar(&opts.resumeDir, "resume-dir", "",
		"directory for conversation state (default: current directory)")
	flag.StringVar(&opts.outputFile, "output-file", "",
		"write output to file instead of stdout")

	flag.Parse()

	return opts
}

func showStats(claudeDir string) error {
	cfg := loadOrCreateConfig(filepath.Join(claudeDir, "config.json"))

	pairs, err := listRequestResponsePairs(claudeDir)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Project: %s\n", claudeDir)
	fmt.Fprintf(os.Stderr, "Model: %s\n", cfg.Model)
	fmt.Fprintf(os.Stderr, "Total tokens: %d in, %d out\n",
		cfg.TotalInput, cfg.TotalOutput)
	fmt.Fprintf(os.Stderr, "Approximate cost: $%.4f\n",
		float64(cfg.TotalInput)*3.0/1000000+
			float64(cfg.TotalOutput)*15.0/1000000)
	fmt.Fprintf(os.Stderr, "Conversation turns: %d\n", len(pairs))
	fmt.Fprintf(os.Stderr, "First run: %s\n", cfg.FirstRun)
	fmt.Fprintf(os.Stderr, "Last run: %s\n", cfg.LastRun)

	return nil
}

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

// executeTools processes all tool use requests in the response.
func executeTools(content []ContentBlock, workingDir string,
	opts *options, conversationID string,
) ([]ContentBlock, error) {
	results := []ContentBlock{}
	for _, block := range content {
		if block.Type == "tool_use" {
			result, err := executeTool(block, workingDir, opts,
				conversationID)
			if err != nil {
				return nil, fmt.Errorf("tool error: %w", err)
			}
			results = append(results, result)
		}
	}
	return results, nil
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

func getClaudeDir(resumeDir string) (string, error) {
	dir := resumeDir
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getting cwd: %w", err)
		}
	}
	return filepath.Join(dir, ".claude"), nil
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

func readInput() (string, error) {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("reading stdin: %w", err)
	}

	msg := string(input)
	if msg == "" {
		return "", fmt.Errorf("no input provided")
	}

	return msg, nil
}

func callAPI(client *http.Client, apiKey, model string, maxTokens int,
	system string, messages []MessageContent, opts *options,
) (*APIResponse, []byte, error) {
	apiReq := APIRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  messages,
		Tools:     getTools(opts),
	}

	reqBody, err := json.Marshal(apiReq)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling request: %w", err)
	}

	if opts.isDebug() {
		fmt.Fprintln(os.Stderr, "=== request ===")
		spew.Dump(reqBody)
	}

	httpReq, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", apiVersion)
	httpReq.Header.Set("content-type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("making API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("reading response: %w", err)
	}

	if err := checkHTTPStatus(resp.StatusCode, respBody); err != nil {
		return nil, respBody, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, nil, fmt.Errorf("parsing response: %w\nBody: %s",
			err, respBody)
	}

	if opts.isDebug() {
		fmt.Fprintln(os.Stderr, "=== response ===")
		spew.Dump(respBody)
	}

	return &apiResp, respBody, nil
}

func checkHTTPStatus(status int, body []byte) error {
	// Try to parse Claude API error first (has more detail)
	var apiErr struct {
		Error *APIError `json:"error"`
	}
	if err := json.Unmarshal(body, &apiErr); err == nil &&
		apiErr.Error != nil {
		return fmt.Errorf("API error [%s]: %s",
			apiErr.Error.Type, apiErr.Error.Message)
	}

	// Standard HTTP status codes
	switch status {
	case http.StatusOK:
		return nil
	case http.StatusBadRequest:
		return fmt.Errorf("%s: %s", http.StatusText(status), body)
	case http.StatusUnauthorized:
		return fmt.Errorf("%s: check API key", http.StatusText(status))
	case http.StatusForbidden:
		return fmt.Errorf("%s: %s", http.StatusText(status), body)
	case http.StatusNotFound:
		return fmt.Errorf("%s: invalid endpoint", http.StatusText(status))
	case http.StatusTooManyRequests:
		return fmt.Errorf("%s: %s", http.StatusText(status), body)
	case http.StatusInternalServerError:
		return fmt.Errorf("%s: %s", http.StatusText(status), body)
	case http.StatusServiceUnavailable:
		return fmt.Errorf("%s: %s", http.StatusText(status), body)
	case 529: // Anthropic-specific: overloaded
		return fmt.Errorf("service overloaded (529): %s", body)
	default:
		return fmt.Errorf("HTTP %d: %s", status, body)
	}
}

func extractResponse(apiResp *APIResponse) string {
	for _, content := range apiResp.Content {
		if content.Type == "text" {
			return content.Text
		}
	}
	return ""
}

func writeOutput(outputFile string, jsonOutput bool,
	assistantText string, respBody []byte,
) error {
	var output string
	if jsonOutput {
		output = string(respBody)
	} else {
		output = assistantText
	}

	switch {
	case outputFile != "":
		// Never write escape codes to files
		err := os.WriteFile(outputFile, []byte(output), 0o644)
		if err != nil {
			return fmt.Errorf("writing output file: %w", err)
		}
	default:
		// FormatResponse handles TTY check and chroma highlighting
		if !jsonOutput && isTTY(os.Stdout) {
			FormatResponse(os.Stdout, output)
		} else {
			if strings.HasSuffix(output, "\n") {
				fmt.Print(output)
			} else {
				fmt.Println(output)
			}
		}

	}

	return nil
}

func saveJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "\t")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	return nil
}

func resetConversation(claudeDir string, verbose bool) error {
	if err := os.RemoveAll(claudeDir); err != nil {
		return fmt.Errorf("removing %s: %w", claudeDir, err)
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "Reset: removed %s\n", claudeDir)
	}
	return nil
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

func getTools(opts *options) []Tool {
	if !opts.canUseTools() {
		return nil
	}

	return []Tool{{
		Name:        "read_file",
		Description: "Read the contents of a file",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]string{
					"type":        "string",
					"description": "Path to the file to read",
				},
			},
			"required": []string{"path"},
		},
	}, {
		Name:        "write_file",
		Description: "Write content to a file. Shows diff in dry-run mode.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]string{
					"type":        "string",
					"description": "Path to the file to write",
				},
				"content": map[string]string{
					"type":        "string",
					"description": "Content to write to the file",
				},
			},
			"required": []string{"path", "content"},
		},
	}, {
		Name: "bash_command",
		Description: `Execute a bash command in the working directory.

Allowed commands: ls, cat, grep, find, head, tail, wc, echo, pwd, date
Also allowed: git (log, diff, show, status, blame) and go (all subcommands)
Pipes and safe redirects (to working dir only) are permitted.

Blocked: rm, mv, cp, chmod, sudo, curl, wget, and path traversal.

Use 'reason' to explain why this command is needed (for audit trail).`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]string{
					"type":        "string",
					"description": "The bash command to execute",
				},
				"reason": map[string]string{
					"type":        "string",
					"description": "Why this command is needed",
				},
			},
			"required": []string{"command", "reason"},
		},
	}}
}

func executeTool(toolUse ContentBlock, workingDir string,
	opts *options, conversationID string,
) (ContentBlock, error) {
	switch toolUse.Name {
	case "read_file":
		return executeReadFile(toolUse, workingDir, opts, conversationID)
	case "write_file":
		return executeWriteFile(toolUse, workingDir, opts, conversationID)
	case "bash_command":
		return executeBashCommand(toolUse, workingDir, opts,
			conversationID)
	default:
		return ContentBlock{}, fmt.Errorf("unknown tool: %s",
			toolUse.Name)
	}
}

func executeReadFile(toolUse ContentBlock, workingDir string,
	opts *options, conversationID string,
) (ContentBlock, error) {
	startTime := time.Now()

	path, ok := toolUse.Input["path"].(string)
	if !ok {
		logAuditEntry("read_file", toolUse.Input, map[string]interface{}{
			"error": "path must be a string",
		}, false, conversationID, startTime, false)
		return makeToolError(toolUse.ID, "path must be a string")
	}

	if !isSafePath(path, workingDir) {
		errMsg := fmt.Sprintf("path outside project: %s", path)
		logAuditEntry("read_file", toolUse.Input, map[string]interface{}{
			"error": errMsg,
		}, false, conversationID, startTime, false)
		return makeToolError(toolUse.ID, errMsg)
	}

	if opts.isVerbose() {
		fmt.Fprintf(os.Stderr, "Tool: read_file(%s)\n", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		logAuditEntry("read_file", toolUse.Input, map[string]interface{}{
			"error": err.Error(),
		}, false, conversationID, startTime, false)
		return makeToolError(toolUse.ID, err.Error())
	}

	logAuditEntry("read_file", toolUse.Input, map[string]interface{}{
		"success": true,
		"path":    path,
		"size":    len(content),
	}, true, conversationID, startTime, false)

	return ContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUse.ID,
		Content:   string(content),
	}, nil
}

func executeWriteFile(toolUse ContentBlock, workingDir string,
	opts *options, conversationID string,
) (ContentBlock, error) {
	startTime := time.Now()

	path, ok := toolUse.Input["path"].(string)
	if !ok {
		logAuditEntry("write_file", toolUse.Input, map[string]interface{}{
			"error": "path must be a string",
		}, false, conversationID, startTime, false)
		return makeToolError(toolUse.ID, "path must be a string")
	}

	content, ok := toolUse.Input["content"].(string)
	if !ok {
		logAuditEntry("write_file", toolUse.Input, map[string]interface{}{
			"error": "content must be a string",
		}, false, conversationID, startTime, false)
		return makeToolError(toolUse.ID, "content must be a string")
	}

	if !isSafePath(path, workingDir) {
		errMsg := fmt.Sprintf("path outside project: %s", path)
		logAuditEntry("write_file", toolUse.Input, map[string]interface{}{
			"error": errMsg,
		}, false, conversationID, startTime, false)
		return makeToolError(toolUse.ID, errMsg)
	}

	old, _ := os.ReadFile(path)

	// Only show diff in normal/verbose mode
	if !opts.isSilent() {
		ToolHeader(path, !opts.canExecuteWrite())
		ShowDiff(string(old), content)
	}

	if !opts.canExecuteWrite() {
		fmt.Fprintf(os.Stderr, "(dry-run: use --tool=write to apply)\n\n")
		logAuditEntry("write_file", toolUse.Input, map[string]interface{}{
			"dry_run": true,
			"path":    path,
			"size":    len(content),
		}, true, conversationID, startTime, true)
		return ContentBlock{
			Type:      "tool_result",
			ToolUseID: toolUse.ID,
			Content: "Dry-run: changes not applied. " +
				"Use --tool=write flag.",
		}, nil
	}

	if opts.isVerbose() {
		fmt.Fprintf(os.Stderr, "Tool: write_file(%s)\n", path)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		logAuditEntry("write_file", toolUse.Input, map[string]interface{}{
			"error": err.Error(),
		}, false, conversationID, startTime, false)
		return makeToolError(toolUse.ID, err.Error())
	}

	logAuditEntry("write_file", toolUse.Input, map[string]interface{}{
		"success": true,
		"path":    path,
		"size":    len(content),
	}, true, conversationID, startTime, false)

	return ContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUse.ID,
		Content:   fmt.Sprintf("Successfully wrote to %s", path),
	}, nil
}

func executeBashCommand(toolUse ContentBlock, workingDir string,
	opts *options, conversationID string,
) (ContentBlock, error) {
	startTime := time.Now()

	command, ok := toolUse.Input["command"].(string)
	if !ok {
		return logAndReturnError(toolUse.ID, "bash_command",
			toolUse.Input, "command must be a string",
			conversationID, startTime)
	}

	reason, ok := toolUse.Input["reason"].(string)
	if !ok {
		return logAndReturnError(toolUse.ID, "bash_command",
			toolUse.Input, "reason must be a string",
			conversationID, startTime)
	}

	// Validate command safety
	if err := validateCommand(command); err != nil {
		return logAndReturnError(toolUse.ID, "bash_command",
			toolUse.Input, err.Error(), conversationID, startTime)
	}

	// Dry-run mode: show what would execute
	if !opts.canExecuteCommand() {
		msg := fmt.Sprintf(
			"Dry-run: would execute command: %s\nReason: %s\n"+
				"Use --tool=command or --tool=all to execute",
			command, reason)
		if !opts.isSilent() {
			ToolHeader("bash_command", true)
		}
		fmt.Fprintf(os.Stderr, "%s\n\n", msg)

		logAuditEntry("bash_command", toolUse.Input, map[string]interface{}{
			"dry_run": true,
			"command": command,
			"reason":  reason,
		}, true, conversationID, startTime, true)

		return ContentBlock{
			Type:      "tool_result",
			ToolUseID: toolUse.ID,
			Content:   msg,
		}, nil
	}

	if opts.isVerbose() {
		fmt.Fprintf(os.Stderr, "Tool: bash_command(%q)\n", command)
	}

	// Execute command with timeout
	ctx, cancel := context.WithTimeout(context.Background(),
		bashCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = workingDir

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	duration := time.Since(startTime)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			msg := fmt.Sprintf(
				"Command timeout after %v\nStdout: %s\nStderr: %s",
				bashCommandTimeout, stdout.String(), stderr.String())

			logAuditEntry("bash_command", toolUse.Input, map[string]interface{}{
				"error":     "timeout",
				"exit_code": -1,
				"stdout":    stdout.String(),
				"stderr":    stderr.String(),
			}, false, conversationID, startTime, false)

			return makeToolError(toolUse.ID, msg)
		} else {
			exitCode = -1
		}
	}

	resultMsg := fmt.Sprintf(
		"Exit code: %d\nDuration: %v\nStdout:\n%s\nStderr:\n%s",
		exitCode, duration, stdout.String(), stderr.String())

	logAuditEntry("bash_command", toolUse.Input, map[string]interface{}{
		"exit_code": exitCode,
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
		"duration":  duration.Milliseconds(),
	}, exitCode == 0, conversationID, startTime, false)

	if exitCode != 0 {
		return makeToolError(toolUse.ID, resultMsg)
	}

	return ContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUse.ID,
		Content:   resultMsg,
	}, nil
}

func validateCommand(command string) error {
	// Check for command chaining operators first (highest priority)
	// These allow bypassing other protections
	chainOperators := []string{"||", "&&", ";"}
	for _, op := range chainOperators {
		if strings.Contains(command, op) {
			return fmt.Errorf("blocked pattern: %s", op)
		}
	}

	// Check for path traversal (second priority)
	if strings.Contains(command, "..") {
		return fmt.Errorf("path traversal not allowed")
	}

	// Block dangerous commands (third priority)
	blockedCommands := []string{
		"sudo", "su ", "rm ", "mv ", "cp ", "chmod", "chown",
		"curl", "wget",
	}
	for _, pattern := range blockedCommands {
		if strings.Contains(command, pattern) {
			return fmt.Errorf("blocked pattern: %s", pattern)
		}
	}

	// Parse commands (handle pipes)
	pipePattern := regexp.MustCompile(`\s*\|\s*`)
	commands := pipePattern.Split(command, -1)

	for _, cmd := range commands {
		parts := strings.Fields(cmd)
		if len(parts) == 0 {
			continue
		}
		firstWord := parts[0]

		// Check whitelist
		if allowedCommands[firstWord] {
			// Special validation for git
			if firstWord == "git" && len(parts) > 1 {
				gitCmd := parts[1]
				allowed := map[string]bool{
					"log": true, "diff": true, "show": true,
					"status": true, "blame": true,
				}
				if !allowed[gitCmd] {
					return fmt.Errorf(
						"git subcommand not allowed: %s", gitCmd)
				}
			}
			continue
		}

		return fmt.Errorf("command not in whitelist: %s", firstWord)
	}

	return nil
}

// isSafePath checks if path is within workingDir
// Returns false if path escapes workingDir through .. or symlinks
func isSafePath(path, workingDir string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	// Clean both paths and ensure workingDir has trailing separator
	// to prevent "/home/user/project" matching "/home/user/project-evil"
	cleanWorking := filepath.Clean(workingDir) + string(filepath.Separator)
	cleanAbs := filepath.Clean(abs) + string(filepath.Separator)

	return strings.HasPrefix(cleanAbs, cleanWorking)
}

func makeToolError(toolUseID, errMsg string) (ContentBlock, error) {
	return ContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUseID,
		Content:   fmt.Sprintf("Error: %s", errMsg),
	}, nil
}

func logAndReturnError(toolUseID, tool string,
	input map[string]interface{}, errMsg string,
	conversationID string, startTime time.Time,
) (ContentBlock, error) {
	logAuditEntry(tool, input, map[string]interface{}{
		"error": errMsg,
	}, false, conversationID, startTime, false)
	return makeToolError(toolUseID, errMsg)
}

func logAuditEntry(tool string, input, result map[string]interface{},
	success bool, conversationID string, startTime time.Time, dryRun bool,
) {
	duration := time.Since(startTime).Milliseconds()

	entry := AuditLogEntry{
		Timestamp:      time.Now().Format("20060102_150405"),
		Tool:           tool,
		Input:          input,
		Result:         result,
		Success:        success,
		DurationMs:     duration,
		ConversationID: conversationID,
		DryRun:         dryRun,
	}

	if !success {
		if errMsg, ok := result["error"].(string); ok {
			entry.Error = errMsg
		}
	}

	// Log to audit file (best effort, don't fail tool execution)
	if err := appendAuditLog(entry); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write audit log: %v\n",
			err)
	}
}
