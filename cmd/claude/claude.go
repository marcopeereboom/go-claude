package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	claudeAPIURL        = "https://api.anthropic.com/v1/messages"
	modelsAPIURL        = "https://api.anthropic.com/v1/models"
	defaultModel        = "claude-sonnet-4-5-20250929"
	conversationDirName = ".claude"
	conversationFile    = "claude_conversation.json"
	modelFile           = "claude_model.json"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ConversationLogEntry struct {
	Request   json.RawMessage `json:"request"`
	Response  json.RawMessage `json:"response"`
	Timestamp string          `json:"timestamp"`
}

// stdinIsPiped returns true if stdin is piped
func stdinIsPiped() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}

func main() {
	flagJSON := flag.Bool("json", false, "output raw JSON")
	flagList := flag.Bool("list-models", false, "list supported Claude models")
	flagMaxTokens := flag.Int("max-tokens", 1000, "max tokens for prompt")
	flagModel := flag.String("model", defaultModel, "model id for single prompt")
	flagOut := flag.String("o", "", "write output to file (default stdout)")
	flagResume := flag.String("r", "", "directory to resume/save conversation context (defaults to cwd)")
	flagTimeout := flag.Int("timeout", 30, "timeout in seconds")
	flagSystem := flag.String("system", "", "override default system prompt")
	flag.Parse()

	if err := runCLI(*flagJSON, *flagList, *flagMaxTokens, *flagModel, *flagOut, *flagResume, *flagTimeout, *flagSystem); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func runCLI(jsonFlag, listModelsFlag bool, maxTokens int, model, outFile, resumeDir string, timeout int, systemFlag string) error {
	apiKey := os.Getenv("CLAUDE_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("CLAUDE_API_KEY not set")
	}

	if listModelsFlag {
		return listModels(apiKey, jsonFlag, timeout)
	}

	if resumeDir == "" {
		resumeDir = "."
	}
	resumeDir = filepath.Clean(resumeDir)
	convDir := filepath.Join(resumeDir, conversationDirName)
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		return fmt.Errorf("creating conversation dir: %w", err)
	}

	convPath := filepath.Join(convDir, conversationFile)
	modelPath := filepath.Join(convDir, modelFile)

	// Read stdin prompt
	var prompt string
	if stdinIsPiped() {
		promptBytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		prompt = string(bytes.TrimSpace(promptBytes))
	} else {
		prompt = ""
	}

	if prompt == "" {
		return fmt.Errorf("no input provided")
	}

	// Load conversation
	messages, err := loadConversationContext(convPath)
	if err != nil {
		return fmt.Errorf("loading conversation: %w", err)
	}

	// Append current user message
	messages = append(messages, Message{Role: "user", Content: prompt})

	// Determine system prompt
	systemPrompt := "Always label code blocks with filenames: ```language filename"
	if systemFlag != "" {
		systemPrompt = systemFlag
	}

	reqBody := map[string]interface{}{
		"model":      model,
		"messages":   messages,
		"system":     systemPrompt,
		"max_tokens": maxTokens,
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	respBytes, err := callClaude(apiKey, reqBytes, timeout)
	if err != nil {
		return err
	}

	// Output
	outF := os.Stdout
	if outFile != "" {
		f, err := os.OpenFile(outFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("opening output file: %w", err)
		}
		defer f.Close()
		outF = f
	}

	if jsonFlag {
		if _, err := outF.Write(append(respBytes, '\n')); err != nil {
			return fmt.Errorf("writing output: %w", err)
		}
	} else {
		var respObj struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(respBytes, &respObj); err != nil {
			if _, err2 := outF.Write(append(respBytes, '\n')); err2 != nil {
				return fmt.Errorf("writing raw output: %w", err2)
			}
		} else {
			for _, c := range respObj.Content {
				if _, err := fmt.Fprintln(outF, c.Text); err != nil {
					return fmt.Errorf("writing output text: %w", err)
				}
			}
		}
	}

	if err := appendLogEntry(convPath, reqBytes, respBytes); err != nil {
		return fmt.Errorf("saving conversation: %w", err)
	}

	if err := os.WriteFile(modelPath, []byte(model), 0o644); err != nil {
		return fmt.Errorf("saving model: %w", err)
	}

	return nil
}

// listModels lists all available Claude models
func listModels(apiKey string, jsonFlag bool, timeout int) error {
	body, err := doRequest(apiKey, http.MethodGet, modelsAPIURL, nil, timeout)
	if err != nil {
		return err
	}

	if jsonFlag {
		fmt.Println(string(body))
		return nil
	}

	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		fmt.Println(string(body))
		return nil
	}
	for _, m := range parsed.Data {
		fmt.Println(m.ID)
	}
	return nil
}

func loadConversationContext(path string) ([]Message, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening conversation file: %w", err)
	}
	defer file.Close()

	var messages []Message
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var entry ConversationLogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		var reqObj struct {
			Messages []Message `json:"messages"`
		}
		if err := json.Unmarshal(entry.Request, &reqObj); err == nil {
			messages = append(messages, reqObj.Messages...)
		}

		var respObj struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(entry.Response, &respObj); err == nil {
			var combined string
			for _, c := range respObj.Content {
				combined += c.Text
			}
			if combined != "" {
				messages = append(messages, Message{Role: "assistant", Content: combined})
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading conversation file: %w", err)
	}
	return messages, nil
}

func appendLogEntry(path string, req, resp []byte) error {
	entry := ConversationLogEntry{
		Request:   json.RawMessage(req),
		Response:  json.RawMessage(resp),
		Timestamp: time.Now().Format(time.RFC3339),
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}
	defer f.Close()

	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling log entry: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("writing log entry: %w", err)
	}
	return nil
}

// Unified HTTP request function for both prompts and model listing
func doRequest(apiKey, method, url string, body []byte, timeoutSec int) ([]byte, error) {
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	// Unified error handling
	switch resp.StatusCode {
	case http.StatusOK:
		return respBody, nil
	case http.StatusUnauthorized:
		return nil, errors.New("unauthorized: invalid API key")
	case http.StatusNotFound:
		return nil, errors.New("resource not found (404)")
	case http.StatusUnprocessableEntity:
		return nil, fmt.Errorf("request could not be processed: %s", string(respBody))
	case http.StatusTooManyRequests:
		return nil, errors.New("rate limit exceeded (429)")
	case http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return nil, fmt.Errorf("server error %d: %s", resp.StatusCode, string(respBody))
	default:
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}
}

// callClaude simply calls the unified doRequest
func callClaude(apiKey string, req []byte, timeoutSec int) ([]byte, error) {
	return doRequest(apiKey, http.MethodPost, claudeAPIURL, req, timeoutSec)
}
