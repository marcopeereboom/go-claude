// claude.go
// Simple Claude CLI: single-shot or list models with JSON/human output and resumable conversations.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	apiBase          = "https://api.anthropic.com/v1"
	apiVersion       = "2023-06-01"
	defaultModel     = "claude-sonnet-4-5-20250929"
	defaultMaxTokens = 1000
	defaultTimeout   = 30 * time.Second
)

// API errors
var (
	ErrUnauthorized = errors.New("unauthorized: invalid or missing API key")
	ErrRateLimited  = errors.New("rate limited: too many requests")
)

// ====================
// API structures
// ====================

// Model represents a Claude model in /v1/models
type Model struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// ModelListResponse is the API response from /v1/models
type ModelListResponse struct {
	Data []Model `json:"data"`
}

// Message represents a message in a conversation
type Message struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"` // message text
}

// MessagesRequest is used to send a prompt to the API
type MessagesRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []Message `json:"messages"`
}

// Delta represents incremental content from a streaming response
type Delta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// MessageChoice represents a single response choice from Claude
type MessageChoice struct {
	Index  int    `json:"index"`
	Delta  Delta  `json:"delta"`
	Finish string `json:"finish_reason"`
}

// MessagesResponse represents the response from /v1/messages
type MessagesResponse struct {
	ID      string          `json:"id"`
	Choices []MessageChoice `json:"choices"`
	Created int64           `json:"created"`
	Model   string          `json:"model"`
	Object  string          `json:"object"`
}

// UsageInfo contains token usage counts
type UsageInfo struct {
	CompletionTokens int `json:"completion_tokens"`
	PromptTokens     int `json:"prompt_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ====================
// Main
// ====================

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

// run parses flags, handles CLI logic, and writes output.
func run() error {
	jsonOut := flag.Bool("json", false, "output raw JSON")
	outFile := flag.String("o", "", "write output to file (default stdout)")
	listModels := flag.Bool("list-models", false, "list supported Claude models")
	model := flag.String("model", defaultModel, "model id for single prompt")
	maxTokens := flag.Int("max-tokens", defaultMaxTokens, "max tokens for prompt")
	timeout := flag.Int("timeout", int(defaultTimeout.Seconds()), "timeout in seconds")
	resumeDir := flag.String("r", "", "directory to resume/save conversation context (defaults to cwd)")
	flag.StringVar(resumeDir, "resume", "", "same as -r")
	flag.Parse()

	if *resumeDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("cannot get cwd: %w", err)
		}
		*resumeDir = cwd
	}
	convFile := fmt.Sprintf("%s/.claude_conversation.json", *resumeDir)

	apiKey := os.Getenv("CLAUDE_API_KEY")
	if apiKey == "" {
		return errors.New("CLAUDE_API_KEY not set")
	}

	client := &http.Client{Timeout: time.Duration(*timeout) * time.Second}

	var output []byte
	var err error

	// Load previous conversation if resuming
	var conversation []Message
	if !*listModels {
		if _, err := os.Stat(convFile); err == nil {
			data, err := os.ReadFile(convFile)
			if err == nil {
				_ = json.Unmarshal(data, &conversation)
				fmt.Fprintf(os.Stderr, "Loaded conversation with %d messages from %s\n", len(conversation), convFile)
			}
		}
	}

	switch {
	case *listModels:
		output, err = listModelsCmd(client, apiKey, *jsonOut)
	default:
		output, err = singlePromptCmd(client, apiKey, *model, *maxTokens, *jsonOut, conversation, flag.Args())
		if err == nil {
			// Append user + assistant messages and save
			if len(flag.Args()) > 0 {
				userMsg := Message{Role: "user", Content: fmt.Sprintf("%s", flag.Args())}
				conversation = append(conversation, userMsg)
				conversation = append(conversation, Message{Role: "assistant", Content: string(output)})
				if err := saveConversation(convFile, conversation); err != nil {
					fmt.Fprintf(os.Stderr, "warning: cannot save conversation: %v\n", err)
				}
			}
		}
	}

	// Handle errors: pretty or JSON
	if err != nil {
		if *jsonOut {
			errJSON, _ := json.Marshal(map[string]string{"error": err.Error()})
			fmt.Fprintln(os.Stdout, string(errJSON))
			return nil
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return nil
	}

	return writeOutput(*outFile, output)
}

// ====================
// API calls
// ====================

// doRequest performs an HTTP request to the Claude API, supporting GET or POST.
func doRequest(client *http.Client, url, apiKey string, body any, method string) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	if method == "" {
		method = http.MethodPost
	}

	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return data, nil
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case http.StatusTooManyRequests:
		return nil, ErrRateLimited
	default:
		return nil, fmt.Errorf("api error (%d): %s", resp.StatusCode, bytes.TrimSpace(data))
	}
}

// ====================
// Command functions
// ====================

// listModelsCmd fetches all Claude models and returns output in JSON or human-readable form.
func listModelsCmd(client *http.Client, apiKey string, jsonOut bool) ([]byte, error) {
	resp, err := doRequest(client, apiBase+"/models", apiKey, nil, http.MethodGet)
	if err != nil {
		return nil, err
	}

	if jsonOut {
		return resp, nil
	}

	var decoded ModelListResponse
	if err := json.Unmarshal(resp, &decoded); err != nil {
		return nil, fmt.Errorf("parse model list: %w", err)
	}

	var out bytes.Buffer
	for _, m := range decoded.Data {
		fmt.Fprintf(&out, "- %-35s %s\n", m.ID, m.DisplayName)
	}

	return out.Bytes(), nil
}

// singlePromptCmd sends a single prompt to Claude and returns the raw assistant output.
func singlePromptCmd(client *http.Client, apiKey, model string, maxTokens int, jsonOut bool, conversation []Message, args []string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("no prompt provided")
	}

	userMsg := Message{
		Role:    "user",
		Content: fmt.Sprintf("%s", args),
	}
	conversation = append(conversation, userMsg)

	reqBody := MessagesRequest{
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  conversation,
	}

	resp, err := doRequest(client, apiBase+"/messages", apiKey, reqBody, http.MethodPost)
	if err != nil {
		return nil, fmt.Errorf("send prompt: %w", err)
	}

	if jsonOut {
		return resp, nil
	}

	var decoded MessagesResponse
	if err := json.Unmarshal(resp, &decoded); err != nil {
		return nil, fmt.Errorf("parse assistant response: %w", err)
	}

	var out bytes.Buffer
	for _, choice := range decoded.Choices {
		out.WriteString(choice.Delta.Content)
	}

	return out.Bytes(), nil
}

// ====================
// Output helpers
// ====================

// writeOutput writes the output to a file or stdout if filename is empty.
func writeOutput(filename string, data []byte) error {
	if filename == "" {
		fmt.Print(string(data))
		return nil
	}

	return os.WriteFile(filename, data, 0o644)
}

// saveConversation writes the conversation to the specified file.
func saveConversation(filename string, conv []Message) error {
	data, err := json.MarshalIndent(conv, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal conversation: %w", err)
	}
	return os.WriteFile(filename, data, 0o644)
}
