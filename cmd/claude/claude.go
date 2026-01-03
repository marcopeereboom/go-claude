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
	"time"

	"github.com/davecgh/go-spew/spew"
)

const (
	defaultModel        = "claude-sonnet-4-5-20250929"
	apiURL              = "https://api.anthropic.com/v1/messages"
	apiVersion          = "2023-06-01"
	defaultSystemPrompt = `You are a helpful coding assistant. Always wrap:
- Filenames in backticks with language: ` + "```go filename.go```" + `
- Code blocks in triple backticks with language specified
- Shell commands in ` + "```bash```" + ` blocks
This helps with automated extraction and saving.`
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type APIRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []Message `json:"messages"`
}

type APIResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model      string    `json:"model"`
	StopReason string    `json:"stop_reason"`
	Usage      Usage     `json:"usage"`
	Error      *APIError `json:"error,omitempty"`
}

type APIError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
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

type options struct {
	debug        bool
	jsonOutput   bool
	listModels   bool
	maxTokens    int
	model        string
	outputFile   string
	resumeDir    string
	showStats    bool
	timeout      int
	systemPrompt string
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

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	// TODO audit all errors, these duplicate text, e.g. mkdir returns
	// already a readable error.
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

	sysPrompt := selectSystemPrompt(
		opts.systemPrompt,
		cfg.SystemPrompt,
	)

	userMsg, err := readInput()
	if err != nil {
		return err
	}

	if opts.verbose {
		fmt.Fprintf(os.Stderr, "Claude dir: %s\n", claudeDir)
		fmt.Fprintf(os.Stderr, "Model: %s\n", selectedModel)
	}

	contextPath := filepath.Join(claudeDir, "context.json")
	// TODO don't use ctx ever, that is essentially a reserved word in go code.
	ctx, err := loadContext(contextPath)
	if err != nil {
		return err
	}
	if opts.verbose {
		fmt.Fprintf(os.Stderr, "Loaded %d messages from context\n", len(ctx.Messages))
	}

	ctx.Messages = append(ctx.Messages, Message{
		Role:    "user",
		Content: userMsg,
	})

	client := &http.Client{
		Timeout: time.Duration(opts.timeout) * time.Second,
	}

	apiResp, respBody, err := callAPI(client, apiKey, selectedModel, opts.maxTokens,
		sysPrompt, ctx.Messages, opts)
	if err != nil {
		return err
	}

	if apiResp.Error != nil {
		if opts.jsonOutput {
			fmt.Println(string(respBody))
		}
		return fmt.Errorf("API error [%s]: %s", apiResp.Error.Type,
			apiResp.Error.Message,
		)
	}

	if opts.verbose {
		fmt.Fprintf(os.Stderr, "Tokens: %d in, %d out (total: %d in, %d out)\n",
			apiResp.Usage.InputTokens, apiResp.Usage.OutputTokens,
			cfg.TotalInput, cfg.TotalOutput)
	}

	assistantText := extractResponse(apiResp)

	cfg.TotalInput += apiResp.Usage.InputTokens
	cfg.TotalOutput += apiResp.Usage.OutputTokens
	cfg.UpdatedAt = time.Now().Format(time.RFC3339)
	if cfg.CreatedAt == "" {
		cfg.CreatedAt = cfg.UpdatedAt
	}

	ctx.Messages = append(ctx.Messages, Message{
		Role:    "assistant",
		Content: assistantText,
	})

	if err := saveJSON(contextPath, ctx); err != nil {
		return fmt.Errorf("saving context: %w", err)
	}

	historyPath := filepath.Join(claudeDir, "history.json")
	if err := appendHistory(
		historyPath,
		userMsg,
		assistantText,
	); err != nil {
		return fmt.Errorf("saving history: %w", err)
	}

	if err := saveJSON(configPath, cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	return writeOutput(opts.outputFile, opts.jsonOutput, assistantText, respBody)
}

func parseFlags() *options {
	opts := &options{}

	flag.BoolVar(&opts.debug, "debug", false, "debug output")
	flag.BoolVar(&opts.jsonOutput, "json", false, "output raw JSON")
	flag.BoolVar(&opts.listModels, "list-models", false,
		"list supported Claude models")
	flag.IntVar(&opts.maxTokens, "max-tokens", 1000,
		"max tokens for prompt")
	flag.StringVar(&opts.model, "model", "", "model id for single prompt")
	flag.StringVar(&opts.outputFile, "o", "", "write output to file (default stdout)")

	var resumeDir2 string
	flag.StringVar(&opts.resumeDir, "r", "",
		"directory to resume/save conversation context")
	flag.StringVar(&resumeDir2, "resume", "", "same as -r")
	flag.BoolVar(&opts.showStats, "stats", false, "show conversation statistics")

	flag.IntVar(&opts.timeout, "timeout", 30, "timeout in seconds")
	flag.StringVar(&opts.systemPrompt, "system", "", "custom system prompt")
	flag.BoolVar(&opts.verbose, "verbose", false,
		"verbose output (show context size, tokens, etc)")

	flag.Parse()

	if opts.resumeDir == "" {
		opts.resumeDir = resumeDir2
	}

	return opts
}

func listModels() error {
	// TODO: Query API for models instead of hardcoding
	fmt.Println("Supported Claude models:")
	fmt.Println("  claude-opus-4-20250514")
	fmt.Println("  claude-sonnet-4-5-20250929")
	fmt.Println("  claude-sonnet-4-20250514")
	fmt.Println("  claude-haiku-4-5-20251001")
	return nil
}

func showStats(claudeDir string) error {
	cfg, _ := loadConfig(filepath.Join(claudeDir, "config.json"))
	hist, _ := loadHistory(filepath.Join(claudeDir, "history.json"))
	ctx, _ := loadContext(filepath.Join(claudeDir, "context.json"))

	fmt.Printf("Project: %s\n", claudeDir)
	fmt.Printf("Model: %s\n", cfg.Model)
	fmt.Printf("Total tokens: %d in, %d out\n", cfg.TotalInput, cfg.TotalOutput)
	fmt.Printf("Approximate cost: $%.4f\n",
		float64(cfg.TotalInput)*3.0/1000000+float64(cfg.TotalOutput)*15.0/1000000)
	fmt.Printf("History: %d messages\n", len(hist.Messages))
	fmt.Printf("Context: %d messages\n", len(ctx.Messages))
	fmt.Printf("Created: %s\n", cfg.CreatedAt)
	fmt.Printf("Updated: %s\n", cfg.UpdatedAt)

	return nil
}

func getClaudeDir(resumeDir string) (string, error) {
	dir := resumeDir
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf(
				"getting cwd: %w",
				err,
			)
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

func callAPI(client *http.Client, apiKey string, model string, maxTokens int, system string, messages []Message, opts *options) (*APIResponse, []byte, error) {
	apiReq := APIRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  messages,
	}

	reqBody, err := json.Marshal(apiReq)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling request: %w", err)
	}

	if opts.debug {
		fmt.Println("=== request ===")
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
		return nil, nil, fmt.Errorf(
			"making API call: %w",
			err,
		)
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
		return nil, nil, fmt.Errorf("parsing response: %w\nBody: %s", err,
			string(respBody))
	}
	if opts.debug {
		fmt.Println("=== response ===")
		spew.Dump(respBody)
	}

	return &apiResp, respBody, nil
}

func checkHTTPStatus(status int, body []byte) error {
	switch status {
	case http.StatusOK:
		return nil
	case http.StatusBadRequest:
		return fmt.Errorf(
			"bad request (400): %s",
			string(body),
		)
	case http.StatusUnauthorized:
		return fmt.Errorf(
			"unauthorized (401): check API key",
		)
	case http.StatusForbidden:
		return fmt.Errorf(
			"forbidden (403): %s",
			string(body),
		)
	case http.StatusNotFound:
		return fmt.Errorf(
			"not found (404): invalid endpoint",
		)
	case http.StatusTooManyRequests:
		return fmt.Errorf(
			"rate limited (429): %s",
			string(body),
		)
	case http.StatusInternalServerError:
		return fmt.Errorf(
			"server error (500): %s",
			string(body),
		)
	case http.StatusServiceUnavailable:
		return fmt.Errorf(
			"service unavailable (503): %s",
			string(body),
		)
	default:
		return fmt.Errorf(
			"unexpected status %d: %s",
			status,
			string(body),
		)
	}
}

func extractResponse(apiResp *APIResponse) string {
	if len(apiResp.Content) > 0 {
		return apiResp.Content[0].Text
	}
	return ""
}

func appendHistory(
	path string,
	userMsg string,
	assistantMsg string,
) error {
	hist, err := loadHistory(path)
	if err != nil {
		return err
	}

	hist.Messages = append(hist.Messages,
		Message{Role: "user", Content: userMsg},
		Message{Role: "assistant", Content: assistantMsg},
	)

	return saveJSON(path, hist)
}

func writeOutput(
	outputFile string,
	jsonOutput bool,
	assistantText string,
	respBody []byte,
) error {
	var output string
	if jsonOutput {
		output = string(respBody)
	} else {
		output = assistantText
	}

	switch {
	case outputFile != "":
		err := os.WriteFile(
			outputFile,
			[]byte(output),
			0o644,
		)
		if err != nil {
			return fmt.Errorf(
				"writing output file: %w",
				err,
			)
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
	ctx := &Context{Messages: []Message{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ctx, nil
		}
		return nil, fmt.Errorf("reading context: %w", err)
	}

	if err := json.Unmarshal(data, ctx); err != nil {
		return nil, fmt.Errorf("parsing context: %w", err)
	}

	return ctx, nil
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
