package claude_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcopeereboom/go-claude/pkg/claude"
	"github.com/marcopeereboom/go-claude/pkg/storage"
)

// TestSaveAndLoadRequestResponse tests the basic storage cycle
func TestSaveAndLoadRequestResponse(t *testing.T) {
	tmpDir := t.TempDir()
	timestamp := "20260105_120000"

	// Create test messages
	messages := []storage.MessageContent{
		{
			Role: "user",
			Content: []storage.ContentBlock{
				{Type: "text", Text: "hello"},
			},
		},
	}

	// Save request
	err := storage.SaveRequest(tmpDir, timestamp, messages)
	if err != nil {
		t.Fatalf("SaveRequest failed: %v", err)
	}

	// Verify request file exists
	reqPath := filepath.Join(tmpDir, "request_20260105_120000.json")
	if _, err := os.Stat(reqPath); os.IsNotExist(err) {
		t.Fatal("request file not created")
	}

	// Save response
	resp := []storage.APIResponse{{
		ID:   "test-id",
		Type: "message",
		Role: "assistant",
		Content: []storage.ContentBlock{
			{Type: "text", Text: "world"},
		},
		StopReason: "end_turn",
	}}
	respBody, _ := json.Marshal(resp)
	err = storage.SaveResponse(tmpDir, timestamp, respBody)
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
		storage.SaveRequest(tmpDir, ts, []storage.MessageContent{})
		storage.SaveResponse(tmpDir, ts, []byte("[]"))
	}

	// Create orphan request (no response)
	storage.SaveRequest(tmpDir, "20260105_120000", []storage.MessageContent{})

	pairs, err := storage.ListRequestResponsePairs(tmpDir)
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
	messages1 := []storage.MessageContent{
		{
			Role: "user",
			Content: []storage.ContentBlock{
				{Type: "text", Text: "question 1"},
			},
		},
	}
	storage.SaveRequest(tmpDir, ts1, messages1)
	resp1 := []storage.APIResponse{{
		Content: []storage.ContentBlock{
			{Type: "text", Text: "answer 1"},
		},
	}}
	respBody1, _ := json.Marshal(resp1)
	storage.SaveResponse(tmpDir, ts1, respBody1)

	// Save second turn
	ts2 := "20260105_110000"
	messages2 := []storage.MessageContent{
		{
			Role: "user",
			Content: []storage.ContentBlock{
				{Type: "text", Text: "question 2"},
			},
		},
	}
	storage.SaveRequest(tmpDir, ts2, messages2)
	resp2 := []storage.APIResponse{{
		Content: []storage.ContentBlock{
			{Type: "text", Text: "answer 2"},
		},
	}}
	respBody2, _ := json.Marshal(resp2)
	storage.SaveResponse(tmpDir, ts2, respBody2)

	// Load history
	history, err := storage.LoadConversationHistory(tmpDir)
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
		storage.SaveRequest(tmpDir, ts, []storage.MessageContent{})
		storage.SaveResponse(tmpDir, ts, []byte("[]"))
	}

	// Prune to keep last 2
	err := storage.PruneResponses(tmpDir, 2, false)
	if err != nil {
		t.Fatalf("PruneResponses failed: %v", err)
	}

	// Verify only last 2 remain
	pairs, _ := storage.ListRequestResponsePairs(tmpDir)
	if len(pairs) != 2 {
		t.Errorf("expected 2 pairs after prune, got %d", len(pairs))
	}

	// Verify correct ones remain
	if pairs[0] != "20260105_130000" || pairs[1] != "20260105_140000" {
		t.Errorf("wrong pairs remaining: %v", pairs)
	}
}

// TestLoadOrCreateConfig tests config loading
func TestLoadOrCreateConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Load non-existent config (should return empty)
	cfg1 := storage.LoadOrCreateConfig(configPath)
	if cfg1.Model != "" {
		t.Error("new config should have empty model")
	}

	// Save config
	cfg1.Model = "test-model"
	cfg1.TotalInput = 1000
	storage.SaveJSON(configPath, cfg1)

	// Load existing config
	cfg2 := storage.LoadOrCreateConfig(configPath)
	if cfg2.Model != "test-model" {
		t.Errorf("expected model 'test-model', got '%s'", cfg2.Model)
	}
	if cfg2.TotalInput != 1000 {
		t.Errorf("expected 1000 tokens, got %d", cfg2.TotalInput)
	}
}

// TestOptionsHelperMethods tests permission checking
func TestOptionsHelperMethods(t *testing.T) {
	tests := []struct {
		name        string
		tool        string
		canWrite    bool
		canCommand  bool
		canUseTools bool
	}{
		{"dry-run", "", false, false, true},
		{"none", "none", false, false, false},
		{"read", "read", false, false, true},
		{"write", "write", true, false, true},
		{"command", "command", false, true, true},
		{"all", "all", true, true, true},
		{"write,command", "write,command", true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := claude.NewOptions()
			opts.SetTool(tt.tool)

			if got := opts.CanExecuteWrite(); got != tt.canWrite {
				t.Errorf("CanExecuteWrite() = %v, want %v",
					got, tt.canWrite)
			}
			if got := opts.CanExecuteCommand(); got != tt.canCommand {
				t.Errorf("CanExecuteCommand() = %v, want %v",
					got, tt.canCommand)
			}
			if got := opts.CanUseTools(); got != tt.canUseTools {
				t.Errorf("CanUseTools() = %v, want %v",
					got, tt.canUseTools)
			}
		})
	}
}

// TestVerbosityLevels tests verbosity checking
func TestVerbosityLevels(t *testing.T) {
	tests := []struct {
		verbosity string
		verbose   bool
		debug     bool
		silent    bool
	}{
		{"silent", false, false, true},
		{"normal", false, false, false},
		{"verbose", true, false, false},
		{"debug", true, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.verbosity, func(t *testing.T) {
			opts := claude.NewOptions()
			opts.SetVerbosity(tt.verbosity)

			if got := opts.IsVerbose(); got != tt.verbose {
				t.Errorf("IsVerbose() = %v, want %v", got, tt.verbose)
			}
			if got := opts.IsDebug(); got != tt.debug {
				t.Errorf("IsDebug() = %v, want %v", got, tt.debug)
			}
			if got := opts.IsSilent(); got != tt.silent {
				t.Errorf("IsSilent() = %v, want %v", got, tt.silent)
			}
		})
	}
}

// TestEstimateTokens tests token estimation
func TestEstimateTokens(t *testing.T) {
	messages := []claude.MessageContent{
		{
			Role: "user",
			Content: []claude.ContentBlock{
				{Type: "text", Text: "1234"}, // ~1 token
			},
		},
		{
			Role: "assistant",
			Content: []claude.ContentBlock{
				{Type: "text", Text: "12345678"}, // ~2 tokens
			},
		},
	}

	tokens := claude.EstimateTokens(messages)
	if tokens != 3 {
		t.Errorf("expected ~3 tokens, got %d", tokens)
	}
}

// TestResponseArrayFormat tests multi-iteration response handling
func TestResponseArrayFormat(t *testing.T) {
	tmpDir := t.TempDir()
	timestamp := "20260105_120000"

	// Create multi-iteration response
	responses := []storage.APIResponse{
		{
			ID: "iter1",
			Content: []storage.ContentBlock{
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
			Content: []storage.ContentBlock{
				{Type: "text", Text: "final answer"},
			},
			StopReason: "end_turn",
		},
	}

	respBody, _ := json.Marshal(responses)
	err := storage.SaveResponse(tmpDir, timestamp, respBody)
	if err != nil {
		t.Fatalf("SaveResponse failed: %v", err)
	}

	// Read back and verify it's an array
	respPath := filepath.Join(tmpDir, "response_20260105_120000.json")
	data, _ := os.ReadFile(respPath)

	var loaded []storage.APIResponse
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
