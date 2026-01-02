package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// -------------------- constants --------------------
const (
	ClaudeMessagesURL = "https://api.anthropic.com/v1/messages"
	ClaudeModelsURL   = "https://api.anthropic.com/v1/models"
	AnthropicVersion  = "2023-06-01"
	DefaultModel      = "claude-sonnet-4-5-20250929"
)

// -------------------- types --------------------
type ClaudeMessageRequest struct {
	Model     string          `json:"model"`
	Messages  []ClaudeMessage `json:"messages"`
	MaxTokens int             `json:"max_tokens,omitempty"`
}

type ClaudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ModelFile struct {
	Model string `json:"model"`
}

// -------------------- main --------------------
func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Flags
	jsonFlag := flag.Bool("json", false, "output raw JSON")
	listModels := flag.Bool("list-models", false, "list supported Claude models")
	maxTokens := flag.Int("max-tokens", 1000, "max tokens for prompt")
	model := flag.String("model", DefaultModel, "model id for single prompt")
	outputFile := flag.String("o", "", "write output to file (default stdout)")
	resumeDir := flag.String("r", "", "directory to resume/save conversation context (defaults to cwd)")
	resumeDir2 := flag.String("resume", "", "same as -r")
	timeoutSec := flag.Int("timeout", 15, "timeout in seconds")
	flag.Parse()

	// Resolve resume directory
	dir := *resumeDir
	if dir == "" {
		dir = *resumeDir2
	}
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting cwd: %w", err)
		}
		dir = cwd
	}

	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("creating .claude dir: %w", err)
	}

	conversationFile := filepath.Join(claudeDir, "claude_conversation.json")
	modelFile := filepath.Join(claudeDir, "claude_model.json")

	apiKey := os.Getenv("CLAUDE_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("CLAUDE_API_KEY not set")
	}

	client := &http.Client{
		Timeout: time.Duration(*timeoutSec) * time.Second,
	}

	writer, f, err := getOutputWriter(*outputFile)
	if err != nil {
		return err
	}
	if f != nil {
		defer f.Close()
	}

	if *listModels {
		return fetchModels(client, apiKey, *jsonFlag, writer)
	}

	prompt, err := readPrompt(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading prompt: %w", err)
	}

	if err := saveModel(modelFile, *model); err != nil {
		return fmt.Errorf("saving model: %w", err)
	}

	respBytes, err := callClaude(client, apiKey, *model, *maxTokens, prompt)
	if err != nil {
		return err
	}

	if err := writeOutput(writer, respBytes, *jsonFlag); err != nil {
		return err
	}

	// Append raw Claude JSON response to conversation NDJSON file
	if err := appendConversation(conversationFile, respBytes); err != nil {
		return fmt.Errorf("saving conversation: %w", err)
	}

	return nil
}

// -------------------- helpers --------------------
func getOutputWriter(path string) (*os.File, *os.File, error) {
	if path == "" {
		return os.Stdout, nil, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("opening output file: %w", err)
	}
	return f, f, nil
}

func readPrompt(r io.Reader) (string, error) {
	var buf bytes.Buffer
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		buf.WriteString(scanner.Text())
		buf.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return "", err
	}
	return buf.String(), nil
}

func writeOutput(w io.Writer, respBytes []byte, rawJSON bool) error {
	if rawJSON {
		_, err := fmt.Fprintf(w, "%s\n", respBytes)
		return err
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return fmt.Errorf("parsing response JSON: %w", err)
	}

	// Extract assistant content
	if contentArr, ok := resp["content"].([]interface{}); ok && len(contentArr) > 0 {
		if first, ok := contentArr[0].(map[string]interface{}); ok {
			if text, ok := first["text"].(string); ok {
				_, err := fmt.Fprintf(w, "%s\n", text)
				return err
			}
		}
	}

	// fallback: raw JSON
	_, err := fmt.Fprintf(w, "%s\n", respBytes)
	return err
}

func saveModel(path, model string) error {
	m := ModelFile{Model: model}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(&m)
}

// -------------------- conversation --------------------
func appendConversation(path string, resp []byte) error {
	var raw interface{}
	if err := json.Unmarshal(resp, &raw); err != nil {
		// fallback if parsing fails
		return appendRaw(path, resp)
	}

	pretty, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return appendRaw(path, resp)
	}

	return appendRaw(path, pretty)
}

func appendRaw(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	// NDJSON: append a newline
	_, err = f.Write(append(data, '\n'))
	return err
}

// -------------------- API calls --------------------
func doRequest(client *http.Client, method, url, apiKey string, body []byte) ([]byte, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", AnthropicVersion)
	if method == http.MethodPost || method == http.MethodPut {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("performing request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200:
		// ok
	case 401:
		return nil, fmt.Errorf("unauthorized: invalid API key")
	case 402:
		return nil, fmt.Errorf("payment required: out of tokens or quota exceeded")
	default:
		return nil, fmt.Errorf("HTTP error %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	return data, nil
}

func callClaude(client *http.Client, apiKey, model string, maxTokens int, prompt string) ([]byte, error) {
	reqBody := ClaudeMessageRequest{
		Model: model,
		Messages: []ClaudeMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens: maxTokens,
	}
	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	return doRequest(client, http.MethodPost, ClaudeMessagesURL, apiKey, reqBytes)
}

func fetchModels(client *http.Client, apiKey string, raw bool, writer io.Writer) error {
	respBytes, err := doRequest(client, http.MethodGet, ClaudeModelsURL, apiKey, nil)
	if err != nil {
		return err
	}

	if raw {
		_, err = fmt.Fprintf(writer, "%s\n", respBytes)
		return err
	}

	var parsed interface{}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return err
	}
	out, _ := json.MarshalIndent(parsed, "", "  ")
	_, err = fmt.Fprintf(writer, "%s\n", out)
	return err
}
