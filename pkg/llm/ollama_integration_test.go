package llm

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// Integration tests that connect to real Ollama server
// These tests are skipped if Ollama is not available

const (
	testOllamaURL   = "http://localhost:11434"
	testOllamaModel = "llama3.1:8b"
)

func isOllamaAvailable() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(testOllamaURL + "/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func TestOllamaIntegration_ListModels(t *testing.T) {
	if !isOllamaAvailable() {
		t.Skip("Ollama is not running on " + testOllamaURL)
	}

	client := NewOllama(testOllamaModel, testOllamaURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	models, err := client.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(models) == 0 {
		t.Fatal("Expected at least one model, got none")
	}

	t.Logf("Found %d Ollama models", len(models))
	for _, model := range models[:min(5, len(models))] {
		t.Logf("  - %s", model.ID)
	}
}

func TestOllamaIntegration_BasicCompletion(t *testing.T) {
	if !isOllamaAvailable() {
		t.Skip("Ollama is not running on " + testOllamaURL)
	}

	client := NewOllama(testOllamaModel, testOllamaURL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &Request{
		Model: testOllamaModel,
		Messages: []MessageContent{
			{
				Role: "user",
				Content: []ContentBlock{
					{Type: "text", Text: "Say 'Hello World' and nothing else."},
				},
			},
		},
		MaxTokens: 100,
	}

	resp, err := client.Generate(ctx, req)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(resp.Content) == 0 || resp.Content[0].Text == "" {
		t.Fatal("Expected non-empty content")
	}

	t.Logf("Response: %s", resp.Content[0].Text)
}

func TestOllamaIntegration_ToolUse(t *testing.T) {
	if !isOllamaAvailable() {
		t.Skip("Ollama is not running on " + testOllamaURL)
	}

	client := NewOllama(testOllamaModel, testOllamaURL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tools := []Tool{
		{
			Name:        "read_file",
			Description: "Read the contents of a file",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file",
					},
				},
				"required": []string{"path"},
			},
		},
	}

	req := &Request{
		Model: testOllamaModel,
		Messages: []MessageContent{
			{
				Role: "user",
				Content: []ContentBlock{
					{Type: "text", Text: "Please read the file at /etc/hostname using the read_file tool."},
				},
			},
		},
		Tools:     tools,
		MaxTokens: 200,
	}

	resp, err := client.Generate(ctx, req)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Find tool_use blocks
	var toolUseFound bool
	for _, block := range resp.Content {
		if block.Type == "tool_use" && block.Name == "read_file" {
			toolUseFound = true
			t.Logf("Tool call: %s with input: %v", block.Name, block.Input)
			if _, ok := block.Input["path"]; !ok {
				t.Error("Expected 'path' parameter in tool call")
			}
			break
		}
	}

	if !toolUseFound {
		t.Fatal("Expected tool_use block, got none")
	}
}

func TestOllamaIntegration_MultipleMessages(t *testing.T) {
	if !isOllamaAvailable() {
		t.Skip("Ollama is not running on " + testOllamaURL)
	}

	client := NewOllama(testOllamaModel, testOllamaURL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &Request{
		Model: testOllamaModel,
		Messages: []MessageContent{
			{
				Role: "user",
				Content: []ContentBlock{
					{Type: "text", Text: "What is 2+2?"},
				},
			},
			{
				Role: "assistant",
				Content: []ContentBlock{
					{Type: "text", Text: "2+2 equals 4."},
				},
			},
			{
				Role: "user",
				Content: []ContentBlock{
					{Type: "text", Text: "What is that number multiplied by 3?"},
				},
			},
		},
		MaxTokens: 100,
	}

	resp, err := client.Generate(ctx, req)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(resp.Content) == 0 || resp.Content[0].Text == "" {
		t.Fatal("Expected non-empty content")
	}

	t.Logf("Multi-turn response: %s", resp.Content[0].Text)
}

func TestOllamaIntegration_SystemPrompt(t *testing.T) {
	if !isOllamaAvailable() {
		t.Skip("Ollama is not running on " + testOllamaURL)
	}

	client := NewOllama(testOllamaModel, testOllamaURL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &Request{
		Model:  testOllamaModel,
		System: "You are a pirate. Respond in pirate speak.",
		Messages: []MessageContent{
			{
				Role: "user",
				Content: []ContentBlock{
					{Type: "text", Text: "Hello, who are you?"},
				},
			},
		},
		MaxTokens: 100,
	}

	resp, err := client.Generate(ctx, req)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(resp.Content) == 0 || resp.Content[0].Text == "" {
		t.Fatal("Expected non-empty content")
	}

	t.Logf("Pirate response: %s", resp.Content[0].Text)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
