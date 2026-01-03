// Package main implements a CLI for interacting with Claude AI with tool use,
// conversation management, and local context storage.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
)

const (
	defaultModel        = "claude-sonnet-4-5-20250929"
	apiURL              = "https://api.anthropic.com/v1/messages"
	apiVersion          = "2023-06-01"
	maxContextTokens    = 100000
	defaultSystemPrompt = `You are a helpful coding assistant. Always wrap:
- Filenames in backticks with language: ` + "```go filename.go```" + `
- Code blocks in triple backticks with language specified
- Shell commands in ` + "```bash```" + ` blocks
This helps with automated extraction and saving.`
)

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

type ContentBlock struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	Content   string                 `json:"content,omitempty"`
}

type MessageContent struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"input_schema"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Storage types
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Config struct {
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	TotalInput   int    `json:"total_input_tokens"`
	TotalOutput  int    `json:"total_output_tokens"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type History struct {
	Messages []Message `json:"messages"`
}

type Context struct {
	Messages []Message `json:"messages"`
}

// CLI options
type options struct {
	debug        bool
	execute      bool
	jsonOutput   bool
	listModels   bool
	maxTokens    int
	model        string
	noTools      bool
	outputFile   string
	reset        bool
	resumeDir    string
	showStats    bool
	timeout      int
	systemPrompt string
	truncate     int
	verbose      bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	opts := parseFlags()

	if opts.listModels {
		return listModels()
	}

	claudeDir, err := getClaudeDir(opts.resumeDir)
	if err != nil {
		return err
	}
	if opts.showStats {
		return showStats(claudeDir)
	}

	if opts.reset {
		return resetConversation(claudeDir, opts.verbose)
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("creating .claude dir: %w", err)
	}

	configPath := filepath.Join(claudeDir, "config.json")
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	selectedModel := selectModel(opts.model, cfg.Model)
	cfg.Model = selectedModel

	sysPrompt := selectSystemPrompt(opts.systemPrompt, cfg.SystemPrompt)

	userMsg, err := readInput()
	if err != nil {
		return err
	}

	if opts.verbose {
		fmt.Fprintf(os.Stderr, "Claude dir: %s\n", claudeDir)
		fmt.Fprintf(os.Stderr, "Model: %s\n", selectedModel)
	}

	contextPath := filepath.Join(claudeDir, "context.json")
	// TODO don't use context or ctx sice those are implicitely reserved by
	// context.Context
	context, err := loadContext(contextPath)
	if err != nil {
		return err
	}
	if opts.verbose {
		fmt.Fprintf(os.Stderr, "Loaded %d messages from context\n",
			len(context.Messages))
	}
	if opts.truncate > 0 && len(context.Messages) > opts.truncate {
		if opts.verbose {
			fmt.Fprintf(os.Stderr, "Truncating context: %d → %d messages\n",
				len(context.Messages), opts.truncate)
		}
		context.Messages = context.Messages[len(context.Messages)-opts.truncate:]
	}

	context.Messages = append(context.Messages, Message{
		Role:    "user",
		Content: userMsg,
	})

	estimatedTokens := estimateTokens(context.Messages)
	if estimatedTokens > maxContextTokens {
		return fmt.Errorf(
			"context too large (%d tokens, max %d)\n"+
				"Options:\n"+
				"  claude --reset           # start fresh conversation\n"+
				"  claude --truncate N      # keep last N messages\n"+
				"  (auto-summarize coming later)",
			estimatedTokens, maxContextTokens)
	}

	client := &http.Client{
		Timeout: time.Duration(opts.timeout) * time.Second,
	}

	// Tool loop
	workingDir, _ := os.Getwd()
	messages := convertToMessageContent(context.Messages)

	for i := 0; i < 10; i++ { // Max 10 tool iterations
		apiResp, respBody, err := callAPI(client, apiKey, selectedModel,
			opts.maxTokens, sysPrompt, messages, opts)
		if err != nil {
			return err
		}

		if apiResp.Error != nil {
			if opts.jsonOutput {
				fmt.Println(string(respBody))
			}
			return fmt.Errorf("API error [%s]: %s",
				apiResp.Error.Type, apiResp.Error.Message)
		}

		// Update token counts
		cfg.TotalInput += apiResp.Usage.InputTokens
		cfg.TotalOutput += apiResp.Usage.OutputTokens

		if opts.verbose {
			fmt.Fprintf(os.Stderr, "Tokens: %d in, %d out\n",
				apiResp.Usage.InputTokens, apiResp.Usage.OutputTokens)
		}

		// Add assistant response to messages
		messages = append(messages, MessageContent{
			Role:    "assistant",
			Content: apiResp.Content,
		})

		// Check stop reason
		if apiResp.StopReason == "end_turn" {
			// Done, extract text and save
			assistantText := extractResponse(apiResp)

			cfg.UpdatedAt = time.Now().Format(time.RFC3339)
			if cfg.CreatedAt == "" {
				cfg.CreatedAt = cfg.UpdatedAt
			}

			context.Messages = append(context.Messages, Message{
				Role:    "assistant",
				Content: assistantText,
			})

			// TODO: Use append-only writes to survive crashes
			if err := saveJSON(contextPath, context); err != nil {
				return fmt.Errorf("saving context: %w", err)
			}

			historyPath := filepath.Join(claudeDir, "history.json")
			if err := appendHistory(historyPath, userMsg,
				assistantText); err != nil {
				return fmt.Errorf("saving history: %w", err)
			}

			if err := saveJSON(configPath, cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			return writeOutput(opts.outputFile, opts.jsonOutput,
				assistantText, respBody)
		}

		if apiResp.StopReason == "tool_use" {
			// Execute tools and continue
			toolResults := []ContentBlock{}

			for _, content := range apiResp.Content {
				if content.Type == "tool_use" {
					result, err := executeTool(content, workingDir, opts)
					if err != nil {
						return fmt.Errorf("tool error: %w", err)
					}
					toolResults = append(toolResults, result)
				}
			}

			// Add tool results as user message
			messages = append(messages, MessageContent{
				Role:    "user",
				Content: toolResults,
			})

			// Continue loop for next API call
			continue
		}

		return fmt.Errorf("unexpected stop_reason: %s", apiResp.StopReason)
	}

	return fmt.Errorf("max tool iterations reached")
}

func parseFlags() *options {
	opts := &options{}

	flag.BoolVar(&opts.debug, "debug", false, "debug output")
	flag.BoolVar(&opts.execute, "execute", false,
		"actually execute tool operations (default: dry-run)")
	flag.BoolVar(&opts.jsonOutput, "json", false, "output raw JSON")
	flag.BoolVar(&opts.listModels, "list-models", false,
		"list supported Claude models")
	flag.IntVar(&opts.maxTokens, "max-tokens", 1000,
		"max tokens for prompt")
	flag.StringVar(&opts.model, "model", "", "model id for single prompt")
	flag.BoolVar(&opts.noTools, "no-tools", false,
		"disable tool use (chat only)")
	flag.StringVar(&opts.outputFile, "o", "",
		"write output to file (default stdout)")
	flag.BoolVar(&opts.reset, "reset", false,
		"reset conversation (delete .claude/ directory)")
	var resumeDir2 string
	flag.StringVar(&opts.resumeDir, "r", "",
		"directory to resume/save conversation context")
	flag.StringVar(&resumeDir2, "resume", "", "same as -r")
	flag.BoolVar(&opts.showStats, "stats", false,
		"show conversation statistics")

	flag.IntVar(&opts.timeout, "timeout", 30, "timeout in seconds")
	flag.IntVar(&opts.truncate, "truncate", 0,
		"keep only last N messages in context (0 = keep all)")
	flag.StringVar(&opts.systemPrompt, "system", "",
		"custom system prompt")
	flag.BoolVar(&opts.verbose, "verbose", false,
		"verbose output (show context size, tokens, etc)")

	flag.Parse()

	if opts.resumeDir == "" {
		opts.resumeDir = resumeDir2
	}

	return opts
}

func listModels() error {
	// TODO: Query https://docs.anthropic.com/en/docs/about-claude/models
	// for dynamic model list instead of hardcoding
	fmt.Fprintln(os.Stderr, "Supported Claude models:")
	fmt.Fprintln(os.Stderr, "  claude-opus-4-20250514")
	fmt.Fprintln(os.Stderr, "  claude-sonnet-4-5-20250929")
	fmt.Fprintln(os.Stderr, "  claude-sonnet-4-20250514")
	fmt.Fprintln(os.Stderr, "  claude-haiku-4-5-20251001")
	return nil
}

func showStats(claudeDir string) error {
	cfg, _ := loadConfig(filepath.Join(claudeDir, "config.json"))
	hist, _ := loadHistory(filepath.Join(claudeDir, "history.json"))
	ctx, _ := loadContext(filepath.Join(claudeDir, "context.json"))

	fmt.Fprintf(os.Stderr, "Project: %s\n", claudeDir)
	fmt.Fprintf(os.Stderr, "Model: %s\n", cfg.Model)
	fmt.Fprintf(os.Stderr, "Total tokens: %d in, %d out\n",
		cfg.TotalInput, cfg.TotalOutput)
	fmt.Fprintf(os.Stderr, "Approximate cost: $%.4f\n",
		float64(cfg.TotalInput)*3.0/1000000+
			float64(cfg.TotalOutput)*15.0/1000000)
	fmt.Fprintf(os.Stderr, "History: %d messages\n", len(hist.Messages))
	fmt.Fprintf(os.Stderr, "Context: %d messages\n", len(ctx.Messages))
	fmt.Fprintf(os.Stderr, "Created: %s\n", cfg.CreatedAt)
	fmt.Fprintf(os.Stderr, "Updated: %s\n", cfg.UpdatedAt)

	return nil
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
	switch {
	case flagPrompt != "":
		return flagPrompt
	case cfgPrompt != "":
		return cfgPrompt
	default:
		return defaultSystemPrompt
	}
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

	if opts.debug {
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

	if opts.debug {
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
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error != nil {
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

func appendHistory(path, userMsg, assistantMsg string) error {
	hist, err := loadHistory(path)
	if err != nil {
		return err
	}

	hist.Messages = append(hist.Messages,
		Message{Role: "user", Content: userMsg},
		Message{Role: "assistant", Content: assistantMsg},
	)

	// TODO: Use append-only writes to survive crashes
	return saveJSON(path, hist)
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
		err := os.WriteFile(outputFile, []byte(output), 0o644)
		if err != nil {
			return fmt.Errorf("writing output file: %w", err)
		}
	default:
		fmt.Println(output)
	}

	return nil
}

func loadConfig(path string) (*Config, error) {
	cfg := &Config{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return cfg, nil
}

func loadContext(path string) (*Context, error) {
	context := &Context{Messages: []Message{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return context, nil
		}
		return nil, fmt.Errorf("reading context: %w", err)
	}

	if err := json.Unmarshal(data, context); err != nil {
		return nil, fmt.Errorf("parsing context: %w", err)
	}

	return context, nil
}

func loadHistory(path string) (*History, error) {
	hist := &History{Messages: []Message{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return hist, nil
		}
		return nil, fmt.Errorf("reading history: %w", err)
	}

	if err := json.Unmarshal(data, hist); err != nil {
		return nil, fmt.Errorf("parsing history: %w", err)
	}

	return hist, nil
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

func estimateTokens(messages []Message) int {
	// Rough estimate: ~4 chars per token
	total := 0
	for _, msg := range messages {
		total += len(msg.Content) / 4
	}
	return total
}

func getTools(opts *options) []Tool {
	if opts.noTools {
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
	}}
}

func executeTool(toolUse ContentBlock, workingDir string,
	opts *options,
) (ContentBlock, error) {
	switch toolUse.Name {
	case "read_file":
		return executeReadFile(toolUse, workingDir, opts)
	case "write_file":
		return executeWriteFile(toolUse, workingDir, opts)
	default:
		return ContentBlock{}, fmt.Errorf("unknown tool: %s", toolUse.Name)
	}
}

func executeReadFile(toolUse ContentBlock, workingDir string,
	opts *options,
) (ContentBlock, error) {
	path, ok := toolUse.Input["path"].(string)
	if !ok {
		return makeToolError(toolUse.ID, "path must be a string")
	}

	if !isSafePath(path, workingDir) {
		return makeToolError(toolUse.ID,
			fmt.Sprintf("path outside project: %s", path))
	}

	if opts.verbose {
		fmt.Fprintf(os.Stderr, "Tool: read_file(%s)\n", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return makeToolError(toolUse.ID, err.Error())
	}

	return ContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUse.ID,
		Content:   string(content),
	}, nil
}

func executeWriteFile(toolUse ContentBlock, workingDir string,
	opts *options,
) (ContentBlock, error) {
	path, ok := toolUse.Input["path"].(string)
	if !ok {
		return makeToolError(toolUse.ID, "path must be a string")
	}

	content, ok := toolUse.Input["content"].(string)
	if !ok {
		return makeToolError(toolUse.ID, "content must be a string")
	}

	if !isSafePath(path, workingDir) {
		return makeToolError(toolUse.ID,
			fmt.Sprintf("path outside project: %s", path))
	}

	old, _ := os.ReadFile(path)

	fmt.Fprintf(os.Stderr, "\n=== %s ===\n", path)
	showDiff(string(old), content)

	if !opts.execute {
		fmt.Fprintf(os.Stderr, "(dry-run: use --execute to apply)\n\n")
		return ContentBlock{
			Type:      "tool_result",
			ToolUseID: toolUse.ID,
			Content:   "Dry-run: changes not applied. Use --execute flag.",
		}, nil
	}

	if opts.verbose {
		fmt.Fprintf(os.Stderr, "Tool: write_file(%s)\n", path)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return makeToolError(toolUse.ID, err.Error())
	}

	return ContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUse.ID,
		Content:   fmt.Sprintf("Successfully wrote to %s", path),
	}, nil
}

func isSafePath(path, workingDir string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	return strings.HasPrefix(abs, workingDir)
}

func showDiff(old, new string) {
	// TODO: Use proper unified diff library
	fmt.Fprintf(os.Stderr, "--- old\n+++ new\n")
	fmt.Fprintf(os.Stderr, "%s\n", new)
}

func makeToolError(toolUseID, errMsg string) (ContentBlock, error) {
	return ContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUseID,
		Content:   fmt.Sprintf("Error: %s", errMsg),
	}, nil
}

func convertToMessageContent(messages []Message) []MessageContent {
	result := make([]MessageContent, len(messages))
	for i, msg := range messages {
		result[i] = MessageContent{
			Role: msg.Role,
			Content: []ContentBlock{
				{Type: "text", Text: msg.Content},
			},
		}
	}
	return result
}
