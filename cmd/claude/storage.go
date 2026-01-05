package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Request is what we send to the API (saved for replay/audit)
type Request struct {
	Timestamp string           `json:"timestamp"`
	Messages  []MessageContent `json:"messages"`
}

// Config stores aggregate stats and settings
type Config struct {
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	TotalInput   int    `json:"total_input_tokens"`
	TotalOutput  int    `json:"total_output_tokens"`
	FirstRun     string `json:"first_run"`
	LastRun      string `json:"last_run"`
}

// AuditLogEntry represents a single tool execution for audit trail
type AuditLogEntry struct {
	Timestamp      string                 `json:"timestamp"`
	Tool           string                 `json:"tool"`
	Input          map[string]interface{} `json:"input"`
	Result         map[string]interface{} `json:"result"`
	Success        bool                   `json:"success"`
	DurationMs     int64                  `json:"duration_ms"`
	ConversationID string                 `json:"conversation_id"`
	DryRun         bool                   `json:"dry_run"`
	Error          string                 `json:"error,omitempty"`
}

// saveRequest saves the request (conversation context + user message) to disk
func saveRequest(claudeDir, timestamp string, messages []MessageContent) error {
	req := Request{
		Timestamp: timestamp,
		Messages:  messages,
	}
	path := filepath.Join(claudeDir, fmt.Sprintf("request_%s.json", timestamp))
	return saveJSON(path, req)
}

// saveResponse saves the raw API response to disk
func saveResponse(claudeDir, timestamp string, respBody []byte) error {
	path := filepath.Join(claudeDir, fmt.Sprintf("response_%s.json", timestamp))
	return os.WriteFile(path, respBody, 0o644)
}

// loadConversationHistory reconstructs conversation from request/response pairs
func loadConversationHistory(claudeDir string) ([]MessageContent, error) {
	pairs, err := listRequestResponsePairs(claudeDir)
	if err != nil {
		return nil, err
	}

	var messages []MessageContent

	for _, ts := range pairs {
		// Load request - extract the user message (always last in request)
		reqPath := filepath.Join(claudeDir, fmt.Sprintf("request_%s.json", ts))
		reqData, err := os.ReadFile(reqPath)
		if err != nil {
			continue
		}

		var req Request
		if err := json.Unmarshal(reqData, &req); err != nil {
			continue
		}

		// Add user message from request (last message is always user)
		if len(req.Messages) > 0 {
			messages = append(messages, req.Messages[len(req.Messages)-1])
		}

		// Load response - extract assistant content
		respPath := filepath.Join(claudeDir, fmt.Sprintf("response_%s.json", ts))
		respData, err := os.ReadFile(respPath)
		if err != nil {
			continue
		}

		var responses []APIResponse
		if err := json.Unmarshal(respData, &responses); err != nil {
			continue
		}

		// Add assistant response (use last response, which has final text)
		if len(responses) > 0 {
			lastResp := responses[len(responses)-1]
			messages = append(messages, MessageContent{
				Role:    "assistant",
				Content: lastResp.Content,
			})
		}
	}

	return messages, nil
}

// listRequestResponsePairs returns sorted list of timestamps with complete pairs
func listRequestResponsePairs(claudeDir string) ([]string, error) {
	entries, err := os.ReadDir(claudeDir)
	if err != nil {
		return nil, err
	}

	timestamps := make(map[string]bool)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "request_") && strings.HasSuffix(name, ".json") {
			ts := strings.TrimPrefix(strings.TrimSuffix(name, ".json"), "request_")

			// Verify response exists (pair must be complete)
			respPath := filepath.Join(claudeDir, fmt.Sprintf("response_%s.json", ts))
			if _, err := os.Stat(respPath); err == nil {
				timestamps[ts] = true
			}
		}
	}

	// Sort timestamps chronologically
	result := make([]string, 0, len(timestamps))
	for ts := range timestamps {
		result = append(result, ts)
	}
	sort.Strings(result)

	return result, nil
}

// loadOrCreateConfig loads config or returns empty one
func loadOrCreateConfig(path string) *Config {
	cfg := &Config{}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	json.Unmarshal(data, cfg)
	return cfg
}

// pruneResponses deletes old request/response pairs, keeping last N
func pruneResponses(claudeDir string, keepLast int, verbose bool) error {
	pairs, err := listRequestResponsePairs(claudeDir)
	if err != nil {
		return err
	}

	if len(pairs) <= keepLast {
		if verbose {
			fmt.Fprintf(os.Stderr, "Nothing to prune (%d pairs, keeping %d)\n",
				len(pairs), keepLast)
		}
		return nil
	}

	toDelete := pairs[:len(pairs)-keepLast]

	for _, ts := range toDelete {
		reqPath := filepath.Join(claudeDir, fmt.Sprintf("request_%s.json", ts))
		respPath := filepath.Join(claudeDir, fmt.Sprintf("response_%s.json", ts))

		os.Remove(reqPath)
		os.Remove(respPath)

		if verbose {
			fmt.Fprintf(os.Stderr, "Pruned: %s\n", ts)
		}
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "Deleted %d pairs, kept %d\n",
			len(toDelete), keepLast)
	}
	return nil
}

// replayResponse re-executes tools from a saved response without calling API
func replayResponse(claudeDir string, opts *options) error {
	var timestamp string

	if opts.replay == "" {
		// Find latest response
		pairs, err := listRequestResponsePairs(claudeDir)
		if err != nil {
			return err
		}
		if len(pairs) == 0 {
			return fmt.Errorf("no responses to replay")
		}
		timestamp = pairs[len(pairs)-1]
	} else {
		timestamp = opts.replay
	}

	respPath := filepath.Join(claudeDir, fmt.Sprintf("response_%s.json", timestamp))
	respBody, err := os.ReadFile(respPath)
	if err != nil {
		return fmt.Errorf("loading response %s: %w", timestamp, err)
	}

	// Response file is array of all API responses from agentic loop
	var responses []APIResponse
	if err := json.Unmarshal(respBody, &responses); err != nil {
		return fmt.Errorf("parsing responses: %w", err)
	}

	if len(responses) == 0 {
		return fmt.Errorf("no responses in file")
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working dir: %w", err)
	}

	if opts.isVerbose() {
		fmt.Fprintf(os.Stderr, "Replaying response: %s\n", timestamp)
	}

	// Execute all tool_use blocks from all responses
	toolCount := 0
	for respIdx, apiResp := range responses {
		for _, block := range apiResp.Content {
			if block.Type != "tool_use" {
				continue
			}
			toolCount++
			if opts.isVerbose() {
				fmt.Fprintf(os.Stderr, "Iteration %d: %s\n", respIdx, block.Name)
			}
			if _, err := executeTool(block, workingDir, opts, timestamp); err != nil {
				return fmt.Errorf("tool %s failed: %w", block.Name, err)
			}
		}
	}

	if opts.isVerbose() {
		fmt.Fprintf(os.Stderr, "Replayed %d tools\n", toolCount)
	}
	return nil
}

// appendAuditLog appends a tool execution entry to the audit log
func appendAuditLog(entry AuditLogEntry) error {
	claudeDir := ".claude"
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("ensure .claude dir: %w", err)
	}

	logPath := filepath.Join(claudeDir, "tool_log.jsonl")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal audit entry: %w", err)
	}

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}

	return f.Sync()
}
