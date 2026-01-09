package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/marcopeereboom/go-claude/pkg/llm"
)

// Use llm types directly instead of redefining
type MessageContent = llm.MessageContent
type ContentBlock = llm.ContentBlock

// Request is what we send to the API (saved for replay/audit)
type Request struct {
	Timestamp string           `json:"timestamp"`
	Messages  []MessageContent `json:"messages"`
}

// APIError represents an API error response
type APIError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// APIResponse represents Claude's API response
type APIResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model"`
	Usage      llm.Usage      `json:"usage"`
	StopReason string         `json:"stop_reason,omitempty"`
	Error      *APIError      `json:"error,omitempty"`
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

// ModelsCache stores cached model listings from providers
type ModelsCache struct {
	LastUpdated time.Time       `json:"last_updated"`
	Models      []llm.ModelInfo `json:"models"`
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

// CurrentTimestamp returns the current timestamp in the standard format
func CurrentTimestamp() string {
	return time.Now().Format("20060102_150405")
}

// LoadRequest loads a request from the given path
func LoadRequest(path string) (*Request, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read request: %w", err)
	}

	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("unmarshal request: %w", err)
	}

	return &req, nil
}

// SaveRequest saves the request (conversation context + user message) to disk
func SaveRequest(claudeDir, timestamp string, messages []MessageContent) error {
	req := Request{
		Timestamp: timestamp,
		Messages:  messages,
	}
	path := filepath.Join(claudeDir, fmt.Sprintf("request_%s.json", timestamp))
	return SaveJSON(path, req)
}

// SaveResponse saves the raw API response to disk
func SaveResponse(claudeDir, timestamp string, respBody []byte) error {
	path := filepath.Join(claudeDir, fmt.Sprintf("response_%s.json", timestamp))
	return os.WriteFile(path, respBody, 0o644)
}

// LoadConversationHistory reconstructs conversation from request/response pairs
func LoadConversationHistory(claudeDir string) ([]MessageContent, error) {
	pairs, err := ListRequestResponsePairs(claudeDir)
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

// ListRequestResponsePairs returns sorted list of timestamps with complete pairs
// Ignores .deleting files (part of atomic deletion process)
func ListRequestResponsePairs(claudeDir string) ([]string, error) {
	entries, err := os.ReadDir(claudeDir)
	if err != nil {
		return nil, err
	}

	timestamps := make(map[string]bool)
	for _, entry := range entries {
		name := entry.Name()

		// Skip .deleting files (in-progress deletions)
		if strings.HasSuffix(name, ".deleting") {
			continue
		}

		if strings.HasPrefix(name, "request_") && strings.HasSuffix(name, ".json") {
			ts := strings.TrimPrefix(strings.TrimSuffix(name, ".json"), "request_")

			// Verify response exists (pair must be complete)
			respPath := filepath.Join(claudeDir, fmt.Sprintf("response_%s.json", ts))
			if _, err := os.Stat(respPath); err == nil {
				timestamps[ts] = true
			}
		}
	}

	// Sort timestamps chronologically (oldest first)
	result := make([]string, 0, len(timestamps))
	for ts := range timestamps {
		result = append(result, ts)
	}
	sort.Strings(result)

	return result, nil
}

// LoadOrCreateConfig loads config or returns empty one
func LoadOrCreateConfig(path string) *Config {
	cfg := &Config{}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	json.Unmarshal(data, cfg)
	return cfg
}

// LoadModelsCache loads cached models from disk
func LoadModelsCache(claudeDir string) (*ModelsCache, error) {
	path := filepath.Join(claudeDir, "models.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cache ModelsCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	return &cache, nil
}

// SaveModelsCache saves model cache to disk
func SaveModelsCache(claudeDir string, cache *ModelsCache) error {
	path := filepath.Join(claudeDir, "models.json")
	return SaveJSON(path, cache)
}

// CleanupOrphanedDeletingFiles removes any .deleting files left over from interrupted operations
func CleanupOrphanedDeletingFiles(claudeDir string) error {
	entries, err := os.ReadDir(claudeDir)
	if err != nil {
		return err
	}

	var cleanupErrors []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".deleting") {
			path := filepath.Join(claudeDir, name)
			if err := os.Remove(path); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Sprintf("%s: %v", name, err))
			}
		}
	}

	if len(cleanupErrors) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(cleanupErrors, "; "))
	}

	return nil
}

// PruneResponses atomically deletes old request/response pairs, keeping last N
// Uses two-phase commit: rename to .deleting, then delete .deleting files
// This ensures no orphaned files are left behind even if interrupted
func PruneResponses(claudeDir string, keepLast int, verbose bool) error {
	// First cleanup any orphaned .deleting files from previous interrupted operations
	if err := CleanupOrphanedDeletingFiles(claudeDir); err != nil {
		return fmt.Errorf("cleanup orphaned files: %w", err)
	}

	pairs, err := ListRequestResponsePairs(claudeDir)
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

	// Calculate which pairs to delete (oldest ones)
	// pairs is sorted oldest to newest, so we delete from the beginning
	toDelete := pairs[:len(pairs)-keepLast]

	// Phase 1: Rename both files to .deleting (atomic marking for deletion)
	var markedForDeletion []string
	var renameErrors []string

	for _, ts := range toDelete {
		reqPath := filepath.Join(claudeDir, fmt.Sprintf("request_%s.json", ts))
		respPath := filepath.Join(claudeDir, fmt.Sprintf("response_%s.json", ts))
		reqDeleting := reqPath + ".deleting"
		respDeleting := respPath + ".deleting"

		// Rename request file
		if err := os.Rename(reqPath, reqDeleting); err != nil {
			renameErrors = append(renameErrors, fmt.Sprintf("request %s: %v", ts, err))
			continue
		}

		// Rename response file - rollback request rename if this fails
		if err := os.Rename(respPath, respDeleting); err != nil {
			// Rollback: restore request file
			os.Rename(reqDeleting, reqPath)
			renameErrors = append(renameErrors, fmt.Sprintf("response %s: %v", ts, err))
			continue
		}

		// Both renames succeeded - mark this pair for deletion
		markedForDeletion = append(markedForDeletion, ts)
	}

	// Phase 2: Delete all .deleting files
	var deleteErrors []string
	deletedCount := 0

	for _, ts := range markedForDeletion {
		reqDeleting := filepath.Join(claudeDir, fmt.Sprintf("request_%s.json.deleting", ts))
		respDeleting := filepath.Join(claudeDir, fmt.Sprintf("response_%s.json.deleting", ts))

		reqErr := os.Remove(reqDeleting)
		respErr := os.Remove(respDeleting)

		// Track errors but continue - files are already marked for deletion
		if reqErr != nil {
			deleteErrors = append(deleteErrors, fmt.Sprintf("request %s: %v", ts, reqErr))
		}
		if respErr != nil {
			deleteErrors = append(deleteErrors, fmt.Sprintf("response %s: %v", ts, respErr))
		}

		// Count as deleted even if Remove failed - files are renamed and invisible to system
		deletedCount++
		if verbose {
			fmt.Fprintf(os.Stderr, "Pruned: %s\n", ts)
		}
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "Deleted %d pairs, kept %d\n",
			deletedCount, len(pairs)-deletedCount)
	}

	// Report any errors encountered
	var allErrors []string
	allErrors = append(allErrors, renameErrors...)
	allErrors = append(allErrors, deleteErrors...)

	if len(allErrors) > 0 {
		return fmt.Errorf("prune completed with errors:\n%s",
			strings.Join(allErrors, "\n"))
	}

	return nil
}

// AppendAuditLog appends a tool execution entry to the audit log
func AppendAuditLog(claudeDir string, entry AuditLogEntry) error {
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

// SaveJSON is a helper to atomically write JSON to disk
func SaveJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomic rename: %w", err)
	}

	return nil
}
