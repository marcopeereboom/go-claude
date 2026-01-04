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
	defaultModel         = "claude-sonnet-4-5-20250929"
	apiURL               = "https://api.anthropic.com/v1/messages"
	apiVersion           = "2023-06-01"
	maxContextTokens     = 100000
	defaultMaxIterations = 15
	defaultMaxCost       = 1.0 // dollars
	defaultSystemPrompt  = `You are a helpful coding assistant. Always wrap:
- Filenames in backticks with language: ` + "```go filename.go```" + `
- Code blocks in triple backticks with language specified
- Shell commands in ` + "```bash```" + ` blocks
This helps with automated extraction and saving.`

	// Defaults
	defaultMaxTokens = 1000
	defaultTimeout   = 30

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

type Conversation struct {
	Messages []Message `json:"messages"`
}

// CLI options
type options struct {
	// Modes
	listModels bool
	reset      bool
	showStats  bool
	replay     bool

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
	userMsg    string
	convo      *Conversation
	workingDir string
	client     *http.Client
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

	// Handle special modes that don't need full setup
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
		return resetConversation(claudeDir, opts.isVerbose())
	}

	// Initialize session
	sess, err := initSession(opts, claudeDir)
	if err != nil {
		return err
	}

	// Execute conversation with tool support
	result, err := executeConversation(sess)
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
		fmt.Fprintf(os.Stderr, "  claude --replay --tool=all\n\n")
		fmt.Fprintf(os.Stderr, "  # Show statistics\n")
		fmt.Fprintf(os.Stderr, "  claude --stats\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	// Modes
	flag.BoolVar(&opts.listModels, "list-models", false,
		"list supported Claude models")
	flag.BoolVar(&opts.reset, "reset", false,
		"reset conversation (delete .claude/ directory)")
	flag.BoolVar(&opts.showStats, "stats", false,
		"show conversation statistics")

	flag.BoolVar(&opts.replay, "replay", false,
		"replay last conversation's tool calls without calling API")

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
	convo, _ := loadConversation(filepath.Join(claudeDir, "conversation.json"))

	fmt.Fprintf(os.Stderr, "Project: %s\n", claudeDir)
	fmt.Fprintf(os.Stderr, "Model: %s\n", cfg.Model)
	fmt.Fprintf(os.Stderr, "Total tokens: %d in, %d out\n",
		cfg.TotalInput, cfg.TotalOutput)
	fmt.Fprintf(os.Stderr, "Approximate cost: $%.4f\n",
		float64(cfg.TotalInput)*3.0/1000000+
			float64(cfg.TotalOutput)*15.0/1000000)
	fmt.Fprintf(os.Stderr, "History: %d messages\n", len(hist.Messages))
	fmt.Fprintf(os.Stderr, "Conversation: %d messages\n", len(convo.Messages))
	fmt.Fprintf(os.Stderr, "Created: %s\n", cfg.CreatedAt)
	fmt.Fprintf(os.Stderr, "Updated: %s\n", cfg.UpdatedAt)

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
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, err
	}

	selectedModel := selectModel(opts.model, cfg.Model)
	cfg.Model = selectedModel

	sysPrompt := selectSystemPrompt(opts.systemPrompt, cfg.SystemPrompt)

	// Read user input
	userMsg, err := readInput()
	if err != nil {
		return nil, err
	}

	if opts.isVerbose() {
		fmt.Fprintf(os.Stderr, "Claude dir: %s\n", claudeDir)
		fmt.Fprintf(os.Stderr, "Model: %s\n", selectedModel)
	}

	// Load and prepare conversation
	convoPath := filepath.Join(claudeDir, "conversation.json")
	convo, err := loadConversation(convoPath)
	if err != nil {
		return nil, err
	}

	if opts.isVerbose() {
		fmt.Fprintf(os.Stderr, "Loaded %d messages\n", len(convo.Messages))
	}

	// Handle truncation
	if opts.truncate > 0 && len(convo.Messages) > opts.truncate {
		if opts.isVerbose() {
			fmt.Fprintf(os.Stderr, "Truncating: %d → %d messages\n",
				len(convo.Messages), opts.truncate)
		}
		convo.Messages = convo.Messages[len(convo.Messages)-opts.truncate:]
	}

	// Add user message
	convo.Messages = append(convo.Messages, Message{
		Role:    "user",
		Content: userMsg,
	})

	// Check context size
	estimatedTokens := estimateTokens(convo.Messages)
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

	return &session{
		opts:       opts,
		claudeDir:  claudeDir,
		apiKey:     apiKey,
		config:     cfg,
		model:      selectedModel,
		sysPrompt:  sysPrompt,
		userMsg:    userMsg,
		convo:      convo,
		workingDir: workingDir,
		client: &http.Client{
			Timeout: time.Duration(opts.timeout) * time.Second,
		},
	}, nil
}

// executeConversation runs the agentic loop with tool support.
func executeConversation(sess *session) (*conversationResult, error) {
	messages := convertToMessageContent(sess.convo.Messages)
	iterationCost := 0.0

	maxIter := sess.opts.maxIterations
	if maxIter == 0 {
		maxIter = 1000 // Effective unlimited
	}

	// Agentic loop: iterate until Claude is done or limits reached
	for i := 0; i < maxIter; i++ {
		apiResp, respBody, err := callAPI(sess.client, sess.apiKey,
			sess.model, sess.opts.maxTokens, sess.sysPrompt, messages,
			sess.opts)
		if err != nil {
			return nil, err
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

		// Handle different stop reasons
		switch apiResp.StopReason {
		case "end_turn":
			// Conversation complete
			assistantText := extractResponse(apiResp)
			sess.convo.Messages = append(sess.convo.Messages, Message{
				Role:    "assistant",
				Content: assistantText,
			})
			return &conversationResult{
				assistantText: assistantText,
				respBody:      respBody,
			}, nil

		case "tool_use":
			// Execute tools and continue
			toolResults, err := executeTools(apiResp.Content,
				sess.workingDir, sess.opts)
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
	opts *options,
) ([]ContentBlock, error) {
	results := []ContentBlock{}
	for _, block := range content {
		if block.Type == "tool_use" {
			result, err := executeTool(block, workingDir, opts)
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
	sess.config.UpdatedAt = time.Now().Format(time.RFC3339)
	if sess.config.CreatedAt == "" {
		sess.config.CreatedAt = sess.config.UpdatedAt
	}

	// Save conversation
	// TODO: Use append-only writes to survive crashes
	convoPath := filepath.Join(sess.claudeDir, "conversation.json")
	if err := saveJSON(convoPath, sess.convo); err != nil {
		return fmt.Errorf("saving conversation: %w", err)
	}

	// Save history
	historyPath := filepath.Join(sess.claudeDir, "history.json")
	if err := appendHistory(historyPath, sess.userMsg,
		result.assistantText); err != nil {
		return fmt.Errorf("saving history: %w", err)
	}

	// Save config
	configPath := filepath.Join(sess.claudeDir, "config.json")
	if err := saveJSON(configPath, sess.config); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	// Output result
	// TODO: add syntax highligted md and code blocks when printing to
	// terminal. Do not write escape codes to any files ever.
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

func loadConversation(path string) (*Conversation, error) {
	convo := &Conversation{Messages: []Message{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return convo, nil
		}
		return nil, fmt.Errorf("reading conversation: %w", err)
	}

	if err := json.Unmarshal(data, convo); err != nil {
		return nil, fmt.Errorf("parsing conversation: %w", err)
	}

	return convo, nil
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
	if opts.canUseTools() {
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
		return ContentBlock{}, fmt.Errorf("unknown tool: %s",
			toolUse.Name)
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

	if opts.isVerbose() {
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

	if !opts.canExecuteWrite() {
		fmt.Fprintf(os.Stderr, "(dry-run: use --execute to apply)\n\n")
		return ContentBlock{
			Type:      "tool_result",
			ToolUseID: toolUse.ID,
			Content: "Dry-run: changes not applied. " +
				"Use --execute flag.",
		}, nil
	}

	if opts.isVerbose() {
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
