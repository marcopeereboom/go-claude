package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
