package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcopeereboom/go-claude/pkg/llm"
)

// TestSaveAndLoadRequestResponse tests the basic storage cycle
func TestSaveAndLoadRequestResponse(t *testing.T) {
	tmpDir := t.TempDir()
	timestamp := "20260105_120000"

	// Create test messages
	messages := []MessageContent{
		{
			Role: "user",
			Content: []ContentBlock{
				{Type: "text", Text: "hello"},
			},
		},
	}

	// Save request
	err := SaveRequest(tmpDir, timestamp, messages)
	if err != nil {
		t.Fatalf("SaveRequest failed: %v", err)
	}

	// Verify request file exists
	reqPath := filepath.Join(tmpDir, "request_20260105_120000.json")
	if _, err := os.Stat(reqPath); os.IsNotExist(err) {
		t.Fatal("request file not created")
	}

	// Save response
	resp := []APIResponse{{
		ID:   "test-id",
		Type: "message",
		Role: "assistant",
		Content: []ContentBlock{
			{Type: "text", Text: "world"},
		},
		StopReason: "end_turn",
	}}
	respBody, _ := json.Marshal(resp)
	err = SaveResponse(tmpDir, timestamp, respBody)
	if err != nil {
		t.Fatalf("SaveResponse failed: %v", err)
	}

	// Verify response file exists
	respPath := filepath.Join(tmpDir, "response_20260105_120000.json")
	if _, err := os.Stat(respPath); os.IsNotExist(err) {
		t.Fatal("response file not created")
	}
}

// TestListRequestResponsePairs tests pair discovery
func TestListRequestResponsePairs(t *testing.T) {
	tmpDir := t.TempDir()

	// Create complete pairs
	timestamps := []string{"20260105_100000", "20260105_110000"}
	for _, ts := range timestamps {
		SaveRequest(tmpDir, ts, []MessageContent{})
		SaveResponse(tmpDir, ts, []byte("[]"))
	}

	// Create orphan request (no response)
	SaveRequest(tmpDir, "20260105_120000", []MessageContent{})

	pairs, err := ListRequestResponsePairs(tmpDir)
	if err != nil {
		t.Fatalf("ListRequestResponsePairs failed: %v", err)
	}

	// Should only return complete pairs
	if len(pairs) != 2 {
		t.Errorf("expected 2 pairs, got %d", len(pairs))
	}

	// Verify sorted order
	if pairs[0] != "20260105_100000" || pairs[1] != "20260105_110000" {
		t.Errorf("pairs not sorted correctly: %v", pairs)
	}
}

// TestLoadConversationHistory tests reconstruction
func TestLoadConversationHistory(t *testing.T) {
	tmpDir := t.TempDir()

	// Save first turn
	ts1 := "20260105_100000"
	messages1 := []MessageContent{
		{
			Role: "user",
			Content: []ContentBlock{
				{Type: "text", Text: "question 1"},
			},
		},
	}
	SaveRequest(tmpDir, ts1, messages1)
	resp1 := []APIResponse{{
		Content: []ContentBlock{
			{Type: "text", Text: "answer 1"},
		},
	}}
	respBody1, _ := json.Marshal(resp1)
	SaveResponse(tmpDir, ts1, respBody1)

	// Save second turn
	ts2 := "20260105_110000"
	messages2 := []MessageContent{
		{
			Role: "user",
			Content: []ContentBlock{
				{Type: "text", Text: "question 2"},
			},
		},
	}
	SaveRequest(tmpDir, ts2, messages2)
	resp2 := []APIResponse{{
		Content: []ContentBlock{
			{Type: "text", Text: "answer 2"},
		},
	}}
	respBody2, _ := json.Marshal(resp2)
	SaveResponse(tmpDir, ts2, respBody2)

	// Load history
	history, err := LoadConversationHistory(tmpDir)
	if err != nil {
		t.Fatalf("LoadConversationHistory failed: %v", err)
	}

	// Should have 4 messages: user, assistant, user, assistant
	if len(history) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(history))
	}

	// Verify order and content
	if history[0].Role != "user" {
		t.Errorf("message 0 should be user, got %s", history[0].Role)
	}
	if history[1].Role != "assistant" {
		t.Errorf("message 1 should be assistant, got %s", history[1].Role)
	}
	if history[2].Role != "user" {
		t.Errorf("message 2 should be user, got %s", history[2].Role)
	}
	if history[3].Role != "assistant" {
		t.Errorf("message 3 should be assistant, got %s", history[3].Role)
	}
}

// TestPruneResponses tests cleanup of old pairs
func TestPruneResponses(t *testing.T) {
	tmpDir := t.TempDir()

	// Create 5 pairs
	timestamps := []string{
		"20260105_100000",
		"20260105_110000",
		"20260105_120000",
		"20260105_130000",
		"20260105_140000",
	}
	for _, ts := range timestamps {
		SaveRequest(tmpDir, ts, []MessageContent{})
		SaveResponse(tmpDir, ts, []byte("[]"))
	}

	// Prune to keep last 2
	err := PruneResponses(tmpDir, 2, false)
	if err != nil {
		t.Fatalf("PruneResponses failed: %v", err)
	}

	// Verify only last 2 remain
	pairs, _ := ListRequestResponsePairs(tmpDir)
	if len(pairs) != 2 {
		t.Errorf("expected 2 pairs after prune, got %d", len(pairs))
	}

	// Verify correct ones remain (newest)
	if pairs[0] != "20260105_130000" || pairs[1] != "20260105_140000" {
		t.Errorf("wrong pairs remaining: %v", pairs)
	}
}

// TestPruneResponsesVerbose tests verbose output mode
func TestPruneResponsesVerbose(t *testing.T) {
	tmpDir := t.TempDir()

	// Create 3 pairs
	timestamps := []string{
		"20260105_100000",
		"20260105_110000",
		"20260105_120000",
	}
	for _, ts := range timestamps {
		SaveRequest(tmpDir, ts, []MessageContent{})
		SaveResponse(tmpDir, ts, []byte("[]"))
	}

	// Prune with verbose=true (should not error, just output to stderr)
	err := PruneResponses(tmpDir, 2, true)
	if err != nil {
		t.Fatalf("PruneResponses with verbose failed: %v", err)
	}

	// Verify correct pairs remain
	pairs, _ := ListRequestResponsePairs(tmpDir)
	if len(pairs) != 2 {
		t.Errorf("expected 2 pairs after prune, got %d", len(pairs))
	}
}

// TestPruneResponsesKeepsNewest verifies that newest pairs are kept
func TestPruneResponsesKeepsNewest(t *testing.T) {
	tmpDir := t.TempDir()

	// Create pairs with clear timestamps
	timestamps := []string{
		"20260105_100000", // oldest
		"20260105_110000",
		"20260105_120000",
		"20260105_130000",
		"20260105_140000", // newest
	}
	for _, ts := range timestamps {
		SaveRequest(tmpDir, ts, []MessageContent{})
		SaveResponse(tmpDir, ts, []byte("[]"))
	}

	// Keep last 3
	err := PruneResponses(tmpDir, 3, false)
	if err != nil {
		t.Fatalf("PruneResponses failed: %v", err)
	}

	// Verify newest 3 remain
	pairs, _ := ListRequestResponsePairs(tmpDir)
	if len(pairs) != 3 {
		t.Fatalf("expected 3 pairs, got %d", len(pairs))
	}

	expected := []string{"20260105_120000", "20260105_130000", "20260105_140000"}
	for i, exp := range expected {
		if pairs[i] != exp {
			t.Errorf("pair %d: expected %s, got %s", i, exp, pairs[i])
		}
	}
}

// TestPruneResponsesNothingToPrune tests when keepLast >= pair count
func TestPruneResponsesNothingToPrune(t *testing.T) {
	tmpDir := t.TempDir()

	// Create 3 pairs
	timestamps := []string{
		"20260105_100000",
		"20260105_110000",
		"20260105_120000",
	}
	for _, ts := range timestamps {
		SaveRequest(tmpDir, ts, []MessageContent{})
		SaveResponse(tmpDir, ts, []byte("[]"))
	}

	// Try to keep 5 (more than we have)
	err := PruneResponses(tmpDir, 5, false)
	if err != nil {
		t.Fatalf("PruneResponses failed: %v", err)
	}

	// Verify all remain
	pairs, _ := ListRequestResponsePairs(tmpDir)
	if len(pairs) != 3 {
		t.Errorf("expected 3 pairs to remain, got %d", len(pairs))
	}
}

// TestPruneResponsesNothingToPruneVerbose tests verbose when nothing to prune
func TestPruneResponsesNothingToPruneVerbose(t *testing.T) {
	tmpDir := t.TempDir()

	// Create 2 pairs
	timestamps := []string{
		"20260105_100000",
		"20260105_110000",
	}
	for _, ts := range timestamps {
		SaveRequest(tmpDir, ts, []MessageContent{})
		SaveResponse(tmpDir, ts, []byte("[]"))
	}

	// Try to keep 5 with verbose (should print "Nothing to prune")
	err := PruneResponses(tmpDir, 5, true)
	if err != nil {
		t.Fatalf("PruneResponses failed: %v", err)
	}

	// Verify all remain
	pairs, _ := ListRequestResponsePairs(tmpDir)
	if len(pairs) != 2 {
		t.Errorf("expected 2 pairs to remain, got %d", len(pairs))
	}
}

// TestPruneResponsesOrphanedFiles tests behavior with mismatched pairs
func TestPruneResponsesOrphanedFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create complete pairs
	timestamps := []string{
		"20260105_100000",
		"20260105_110000",
		"20260105_120000",
	}
	for _, ts := range timestamps {
		SaveRequest(tmpDir, ts, []MessageContent{})
		SaveResponse(tmpDir, ts, []byte("[]"))
	}

	// Manually delete one response file to create orphan
	os.Remove(filepath.Join(tmpDir, "response_20260105_110000.json"))

	// ListRequestResponsePairs should only return complete pairs
	pairs, _ := ListRequestResponsePairs(tmpDir)
	if len(pairs) != 2 {
		t.Errorf("expected 2 complete pairs, got %d", len(pairs))
	}

	// Prune should work on complete pairs only
	err := PruneResponses(tmpDir, 1, false)
	if err != nil {
		t.Fatalf("PruneResponses failed: %v", err)
	}

	// Should keep newest complete pair
	pairsAfter, _ := ListRequestResponsePairs(tmpDir)
	if len(pairsAfter) != 1 {
		t.Errorf("expected 1 pair after prune, got %d", len(pairsAfter))
	}
	if pairsAfter[0] != "20260105_120000" {
		t.Errorf("expected newest pair 20260105_120000, got %s", pairsAfter[0])
	}
}

// TestLoadOrCreateConfig tests config loading
func TestLoadOrCreateConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Load non-existent config (should return empty)
	cfg1 := LoadOrCreateConfig(configPath)
	if cfg1.Model != "" {
		t.Error("new config should have empty model")
	}

	// Save config
	cfg1.Model = "test-model"
	cfg1.TotalInput = 1000
	SaveJSON(configPath, cfg1)

	// Load existing config
	cfg2 := LoadOrCreateConfig(configPath)
	if cfg2.Model != "test-model" {
		t.Errorf("expected model 'test-model', got '%s'", cfg2.Model)
	}
	if cfg2.TotalInput != 1000 {
		t.Errorf("expected 1000 tokens, got %d", cfg2.TotalInput)
	}
}

// TestResponseArrayFormat tests multi-iteration response handling
func TestResponseArrayFormat(t *testing.T) {
	tmpDir := t.TempDir()
	timestamp := "20260105_120000"

	// Create multi-iteration response
	responses := []APIResponse{
		{
			ID: "iter1",
			Content: []ContentBlock{
				{
					Type: "tool_use",
					ID:   "tool1",
					Name: "read_file",
				},
			},
			StopReason: "tool_use",
		},
		{
			ID: "iter2",
			Content: []ContentBlock{
				{Type: "text", Text: "final answer"},
			},
			StopReason: "end_turn",
		},
	}

	respBody, _ := json.Marshal(responses)
	err := SaveResponse(tmpDir, timestamp, respBody)
	if err != nil {
		t.Fatalf("SaveResponse failed: %v", err)
	}

	// Read back and verify it's an array
	respPath := filepath.Join(tmpDir, "response_20260105_120000.json")
	data, _ := os.ReadFile(respPath)

	var loaded []APIResponse
	err = json.Unmarshal(data, &loaded)
	if err != nil {
		t.Fatalf("failed to parse response array: %v", err)
	}

	if len(loaded) != 2 {
		t.Errorf("expected 2 responses, got %d", len(loaded))
	}

	if loaded[0].StopReason != "tool_use" {
		t.Errorf("first response should be tool_use, got %s",
			loaded[0].StopReason)
	}
	if loaded[1].StopReason != "end_turn" {
		t.Errorf("second response should be end_turn, got %s",
			loaded[1].StopReason)
	}
}

// TestPruneResponsesSortingOrder verifies chronological sorting
func TestPruneResponsesSortingOrder(t *testing.T) {
	tmpDir := t.TempDir()

	// Create pairs in non-sorted order
	timestamps := []string{
		"20260105_140000", // newest
		"20260105_100000", // oldest
		"20260105_120000",
		"20260105_110000",
		"20260105_130000",
	}
	for _, ts := range timestamps {
		SaveRequest(tmpDir, ts, []MessageContent{})
		SaveResponse(tmpDir, ts, []byte("[]"))
	}

	// Prune to keep last 2
	err := PruneResponses(tmpDir, 2, false)
	if err != nil {
		t.Fatalf("PruneResponses failed: %v", err)
	}

	// Verify only newest 2 remain
	pairs, _ := ListRequestResponsePairs(tmpDir)
	if len(pairs) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(pairs))
	}

	// Must be the newest timestamps
	if pairs[0] != "20260105_130000" {
		t.Errorf("first pair should be 20260105_130000, got %s", pairs[0])
	}
	if pairs[1] != "20260105_140000" {
		t.Errorf("second pair should be 20260105_140000, got %s", pairs[1])
	}
}

// TestPruneResponsesKeepLastOne tests edge case of keeping only 1 pair
func TestPruneResponsesKeepLastOne(t *testing.T) {
	tmpDir := t.TempDir()

	timestamps := []string{
		"20260105_100000",
		"20260105_110000",
		"20260105_120000",
	}
	for _, ts := range timestamps {
		SaveRequest(tmpDir, ts, []MessageContent{})
		SaveResponse(tmpDir, ts, []byte("[]"))
	}

	// Keep only 1 (the newest)
	err := PruneResponses(tmpDir, 1, false)
	if err != nil {
		t.Fatalf("PruneResponses failed: %v", err)
	}

	pairs, _ := ListRequestResponsePairs(tmpDir)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	if pairs[0] != "20260105_120000" {
		t.Errorf("expected newest pair 20260105_120000, got %s", pairs[0])
	}
}

// TestPruneResponsesBothFilesDeleted verifies that both files are deleted for each pair
func TestPruneResponsesBothFilesDeleted(t *testing.T) {
	tmpDir := t.TempDir()

	// Create 3 pairs
	timestamps := []string{
		"20260105_100000",
		"20260105_110000",
		"20260105_120000",
	}
	for _, ts := range timestamps {
		SaveRequest(tmpDir, ts, []MessageContent{})
		SaveResponse(tmpDir, ts, []byte("[]"))
	}

	// Prune keeping last 1
	err := PruneResponses(tmpDir, 1, false)
	if err != nil {
		t.Fatalf("PruneResponses failed: %v", err)
	}

	// Verify both request and response files were deleted for the old pairs
	for _, ts := range []string{"20260105_100000", "20260105_110000"} {
		reqPath := filepath.Join(tmpDir, "request_"+ts+".json")
		respPath := filepath.Join(tmpDir, "response_"+ts+".json")

		if _, err := os.Stat(reqPath); !os.IsNotExist(err) {
			t.Errorf("request file %s should be deleted", ts)
		}
		if _, err := os.Stat(respPath); !os.IsNotExist(err) {
			t.Errorf("response file %s should be deleted", ts)
		}
	}

	// Verify kept pair still has both files
	ts := "20260105_120000"
	reqPath := filepath.Join(tmpDir, "request_"+ts+".json")
	respPath := filepath.Join(tmpDir, "response_"+ts+".json")

	if _, err := os.Stat(reqPath); os.IsNotExist(err) {
		t.Error("kept request file should still exist")
	}
	if _, err := os.Stat(respPath); os.IsNotExist(err) {
		t.Error("kept response file should still exist")
	}
}

// TestPruneResponsesErrorHandling tests that errors during deletion are properly reported
func TestPruneResponsesErrorHandling(t *testing.T) {
	tmpDir := t.TempDir()

	// Create 3 pairs
	timestamps := []string{
		"20260105_100000",
		"20260105_110000",
		"20260105_120000",
	}
	for _, ts := range timestamps {
		SaveRequest(tmpDir, ts, []MessageContent{})
		SaveResponse(tmpDir, ts, []byte("[]"))
	}

	// Make the first request file read-only to trigger deletion error
	reqPath := filepath.Join(tmpDir, "request_20260105_100000.json")
	os.Chmod(reqPath, 0444)

	// Make directory read-only to prevent deletion
	os.Chmod(tmpDir, 0555)
	defer os.Chmod(tmpDir, 0755) // Restore for cleanup

	// Prune keeping last 1 - should report error for failed deletion
	err := PruneResponses(tmpDir, 1, false)

	// Restore permissions for cleanup
	os.Chmod(tmpDir, 0755)
	os.Chmod(reqPath, 0644)

	// Should get error because deletion failed
	if err == nil {
		t.Error("expected error when file deletion fails")
	} else {
		errMsg := err.Error()
		// Error message changed - now reports "prune completed with errors"
		if !strings.Contains(errMsg, "prune completed with errors") {
			t.Errorf("error should mention 'prune completed with errors', got: %s", errMsg)
		}
	}
}

// TestPruneResponsesListError tests error handling when ListRequestResponsePairs fails
func TestPruneResponsesListError(t *testing.T) {
	// Use a non-existent directory to trigger error in ListRequestResponsePairs
	err := PruneResponses("/nonexistent/path/to/dir", 5, false)
	if err == nil {
		t.Error("expected error when directory doesn't exist")
	}
}

// TestSaveJSONAtomicWrite tests atomic write behavior
func TestSaveJSONAtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "test.json")

	// Test data
	data := map[string]string{"key": "value"}

	// Save JSON
	err := SaveJSON(testPath, data)
	if err != nil {
		t.Fatalf("SaveJSON failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(testPath); os.IsNotExist(err) {
		t.Error("JSON file should exist")
	}

	// Verify temp file was cleaned up
	tmpPath := testPath + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("temp file should be cleaned up")
	}

	// Verify content
	content, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatalf("failed to read JSON file: %v", err)
	}

	var loaded map[string]string
	if err := json.Unmarshal(content, &loaded); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if loaded["key"] != "value" {
		t.Errorf("expected 'value', got '%s'", loaded["key"])
	}
}

// TestSaveJSONInvalidPath tests error handling for invalid paths
func TestSaveJSONInvalidPath(t *testing.T) {
	// Try to save to a read-only directory
	err := SaveJSON("/nonexistent/path/file.json", map[string]string{})
	if err == nil {
		t.Error("expected error when saving to invalid path")
	}
}

// TestLoadModelsCache tests loading cached models
func TestLoadModelsCache(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test cache
	cache := &ModelsCache{
		LastUpdated: time.Now(),
		Models: []llm.ModelInfo{
			{ID: "model1", Name: "Model 1", Provider: "test"},
		},
	}

	// Save cache
	err := SaveModelsCache(tmpDir, cache)
	if err != nil {
		t.Fatalf("SaveModelsCache failed: %v", err)
	}

	// Load cache
	loaded, err := LoadModelsCache(tmpDir)
	if err != nil {
		t.Fatalf("LoadModelsCache failed: %v", err)
	}

	if len(loaded.Models) != 1 {
		t.Errorf("expected 1 model, got %d", len(loaded.Models))
	}
}

// TestLoadModelsCacheNotFound tests loading when cache doesn't exist
func TestLoadModelsCacheNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := LoadModelsCache(tmpDir)
	if err == nil {
		t.Error("expected error when cache doesn't exist")
	}
}

// TestSaveModelsCache tests saving model cache
func TestSaveModelsCache(t *testing.T) {
	tmpDir := t.TempDir()

	cache := &ModelsCache{
		LastUpdated: time.Now(),
		Models:      []llm.ModelInfo{},
	}

	err := SaveModelsCache(tmpDir, cache)
	if err != nil {
		t.Fatalf("SaveModelsCache failed: %v", err)
	}

	// Verify file exists
	cachePath := filepath.Join(tmpDir, "models.json")
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Error("cache file should exist")
	}
}

// TestAppendAuditLog tests audit log functionality
func TestAppendAuditLog(t *testing.T) {
	tmpDir := t.TempDir()

	entry := AuditLogEntry{
		Timestamp:      "20260105_120000",
		Tool:           "read_file",
		Input:          map[string]interface{}{"path": "test.txt"},
		Result:         map[string]interface{}{"content": "test"},
		Success:        true,
		DurationMs:     100,
		ConversationID: "test-conv",
		DryRun:         false,
	}

	// Append first entry
	err := AppendAuditLog(tmpDir, entry)
	if err != nil {
		t.Fatalf("AppendAuditLog failed: %v", err)
	}

	// Append second entry
	entry.Timestamp = "20260105_120001"
	err = AppendAuditLog(tmpDir, entry)
	if err != nil {
		t.Fatalf("AppendAuditLog second entry failed: %v", err)
	}

	// Verify file exists
	logPath := filepath.Join(tmpDir, "tool_log.jsonl")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("audit log file should exist")
	}

	// Verify content
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 log entries, got %d", len(lines))
	}
}

// TestAppendAuditLogWithError tests logging tool errors
func TestAppendAuditLogWithError(t *testing.T) {
	tmpDir := t.TempDir()

	entry := AuditLogEntry{
		Timestamp:      "20260105_120000",
		Tool:           "bash_command",
		Input:          map[string]interface{}{"command": "invalid"},
		Result:         map[string]interface{}{},
		Success:        false,
		DurationMs:     50,
		ConversationID: "test-conv",
		DryRun:         false,
		Error:          "command not found",
	}

	err := AppendAuditLog(tmpDir, entry)
	if err != nil {
		t.Fatalf("AppendAuditLog failed: %v", err)
	}

	// Verify error is in log
	logPath := filepath.Join(tmpDir, "tool_log.jsonl")
	content, _ := os.ReadFile(logPath)

	var logged AuditLogEntry
	json.Unmarshal(content, &logged)

	if logged.Error != "command not found" {
		t.Errorf("expected error 'command not found', got '%s'", logged.Error)
	}
	if logged.Success {
		t.Error("expected Success to be false")
	}
}

// TestCleanupOrphanedDeletingFiles tests cleanup of .deleting files
func TestCleanupOrphanedDeletingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some .deleting files (simulate interrupted deletion)
	deletingFiles := []string{
		"request_20260105_100000.json.deleting",
		"response_20260105_100000.json.deleting",
		"request_20260105_110000.json.deleting",
	}

	for _, f := range deletingFiles {
		path := filepath.Join(tmpDir, f)
		os.WriteFile(path, []byte("test"), 0644)
	}

	// Also create normal files that should NOT be deleted
	normalFiles := []string{
		"request_20260105_120000.json",
		"response_20260105_120000.json",
	}
	for _, f := range normalFiles {
		path := filepath.Join(tmpDir, f)
		os.WriteFile(path, []byte("test"), 0644)
	}

	// Run cleanup
	err := CleanupOrphanedDeletingFiles(tmpDir)
	if err != nil {
		t.Fatalf("CleanupOrphanedDeletingFiles failed: %v", err)
	}

	// Verify .deleting files are gone
	for _, f := range deletingFiles {
		path := filepath.Join(tmpDir, f)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf(".deleting file %s should be removed", f)
		}
	}

	// Verify normal files remain
	for _, f := range normalFiles {
		path := filepath.Join(tmpDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("normal file %s should still exist", f)
		}
	}
}

// TestListRequestResponsePairsIgnoresDeletingFiles tests that .deleting files are filtered
func TestListRequestResponsePairsIgnoresDeletingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a complete pair
	SaveRequest(tmpDir, "20260105_100000", []MessageContent{})
	SaveResponse(tmpDir, "20260105_100000", []byte("[]"))

	// Create .deleting files (should be ignored)
	os.WriteFile(filepath.Join(tmpDir, "request_20260105_110000.json.deleting"), []byte("[]"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "response_20260105_110000.json.deleting"), []byte("[]"), 0644)

	// List pairs
	pairs, err := ListRequestResponsePairs(tmpDir)
	if err != nil {
		t.Fatalf("ListRequestResponsePairs failed: %v", err)
	}

	// Should only see the complete pair, not .deleting files
	if len(pairs) != 1 {
		t.Errorf("expected 1 pair, got %d", len(pairs))
	}
	if pairs[0] != "20260105_100000" {
		t.Errorf("expected pair 20260105_100000, got %s", pairs[0])
	}
}

// TestPruneResponsesAtomicRollback tests that failed response rename rolls back request rename
func TestPruneResponsesAtomicRollback(t *testing.T) {
	tmpDir := t.TempDir()

	// Create 2 pairs
	timestamps := []string{
		"20260105_100000",
		"20260105_110000",
	}
	for _, ts := range timestamps {
		SaveRequest(tmpDir, ts, []MessageContent{})
		SaveResponse(tmpDir, ts, []byte("[]"))
	}

	// Make response file read-only to force rename failure
	respPath := filepath.Join(tmpDir, "response_20260105_100000.json")
	os.Chmod(respPath, 0444)
	os.Chmod(tmpDir, 0555) // Read-only directory
	defer os.Chmod(tmpDir, 0755)

	// Try to prune - should fail to rename response, rollback request rename
	err := PruneResponses(tmpDir, 1, false)

	// Restore permissions
	os.Chmod(tmpDir, 0755)
	os.Chmod(respPath, 0644)

	// Should get error
	if err == nil {
		t.Error("expected error when rename fails")
	}

	// Verify request file was NOT renamed (rollback worked)
	reqPath := filepath.Join(tmpDir, "request_20260105_100000.json")
	if _, err := os.Stat(reqPath); os.IsNotExist(err) {
		t.Error("request file should still exist (rollback should have restored it)")
	}

	// Verify no .deleting files left behind
	reqDeleting := reqPath + ".deleting"
	if _, err := os.Stat(reqDeleting); !os.IsNotExist(err) {
		t.Error(".deleting file should not exist after rollback")
	}
}
