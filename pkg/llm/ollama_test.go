package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Test helper types to match Ollama API response structure
type ollamaMessage struct {
	Role    string                   `json:"role"`
	Content string                   `json:"content"`
	Tools   []map[string]interface{} `json:"tool_calls,omitempty"`
}

type ollamaResponse struct {
	Model     string        `json:"model"`
	CreatedAt string        `json:"created_at"`
	Message   ollamaMessage `json:"message"`
	Done      bool          `json:"done"`
}

// mockOllamaServer creates a test HTTP server that simulates Ollama API
func mockOllamaServer(t *testing.T, responses []ollamaResponse) *httptest.Server {
	callCount := 0

	return httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// Verify request
			if r.Method != "POST" {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.Header.Get("content-type") != "application/json" {
				t.Errorf("wrong content type")
			}
			if r.URL.Path != "/api/chat" {
				t.Errorf("wrong path: %s", r.URL.Path)
			}

			// Return response based on call count
			if callCount >= len(responses) {
				t.Errorf("unexpected API call #%d", callCount+1)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			resp := responses[callCount]
			callCount++

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
}

func TestOllamaGenerate_Success(t *testing.T) {
	responses := []ollamaResponse{{
		Model:     "llama2",
		CreatedAt: "2024-01-01T00:00:00Z",
		Message: ollamaMessage{
			Role:    "assistant",
			Content: "Hello! How can I help you?",
		},
		Done: true,
	}}

	server := mockOllamaServer(t, responses)
	defer server.Close()

	client := NewOllama("llama2", server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &Request{
		Model: "llama2",
		Messages: []MessageContent{{
			Role: "user",
			Content: []ContentBlock{
				{Type: "text", Text: "hi"},
			},
		}},
	}

	resp, err := client.Generate(ctx, req)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if resp.StopReason != "end_turn" {
		t.Errorf("expected end_turn, got %s", resp.StopReason)
	}

	if len(resp.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(resp.Content))
	}

	if resp.Content[0].Type != "text" {
		t.Errorf("expected text block, got %s", resp.Content[0].Type)
	}

	if resp.Content[0].Text != "Hello! How can I help you?" {
		t.Errorf("unexpected text: %s", resp.Content[0].Text)
	}
}

func TestOllamaGenerate_ToolUse(t *testing.T) {
	responses := []ollamaResponse{{
		Model:     "llama2",
		CreatedAt: "2024-01-01T00:00:00Z",
		Message: ollamaMessage{
			Role:    "assistant",
			Content: "",
			Tools: []map[string]interface{}{
				{
					"id":   "call_123",
					"type": "function",
					"function": map[string]interface{}{
						"name": "read_file",
						"arguments": map[string]interface{}{
							"path": "test.txt",
						},
					},
				},
			},
		},
		Done: true,
	}}

	server := mockOllamaServer(t, responses)
	defer server.Close()

	client := NewOllama("llama2", server.URL)
	ctx := context.Background()

	req := &Request{
		Model: "llama2",
		Messages: []MessageContent{{
			Role: "user",
			Content: []ContentBlock{
				{Type: "text", Text: "read test.txt"},
			},
		}},
		Tools: []Tool{{
			Name:        "read_file",
			Description: "Read a file",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{"type": "string"},
				},
			},
		}},
	}

	resp, err := client.Generate(ctx, req)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if resp.StopReason != "tool_use" {
		t.Errorf("expected tool_use, got %s", resp.StopReason)
	}

	if len(resp.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(resp.Content))
	}

	block := resp.Content[0]
	if block.Type != "tool_use" {
		t.Errorf("expected tool_use block, got %s", block.Type)
	}
	if block.Name != "read_file" {
		t.Errorf("expected read_file, got %s", block.Name)
	}
	// Ollama generates IDs as call_{function_name}
	if block.ID != "call_read_file" {
		t.Errorf("expected call_read_file, got %s", block.ID)
	}

	if block.Input["path"] != "test.txt" {
		t.Errorf("expected test.txt path, got %v", block.Input["path"])
	}
}

func TestOllamaGenerate_Errors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "bad request",
			statusCode: http.StatusBadRequest,
			wantErr:    true,
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.statusCode)
					w.Write([]byte("error"))
				}))
			defer server.Close()

			client := NewOllama("llama2", server.URL)
			ctx := context.Background()

			req := &Request{
				Model: "llama2",
				Messages: []MessageContent{{
					Role: "user",
					Content: []ContentBlock{
						{Type: "text", Text: "hi"},
					},
				}},
			}

			_, err := client.Generate(ctx, req)
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
		})
	}
}

func TestOllamaGenerate_ContextCancel(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Delay to allow context cancellation
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
	defer server.Close()

	client := NewOllama("llama2", server.URL)
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	req := &Request{
		Model: "llama2",
		Messages: []MessageContent{{
			Role: "user",
			Content: []ContentBlock{
				{Type: "text", Text: "hi"},
			},
		}},
	}

	_, err := client.Generate(ctx, req)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestOllamaGenerate_SystemMessage(t *testing.T) {
	responses := []ollamaResponse{{
		Model:     "llama2",
		CreatedAt: "2024-01-01T00:00:00Z",
		Message: ollamaMessage{
			Role:    "assistant",
			Content: "I am a helpful assistant.",
		},
		Done: true,
	}}

	server := mockOllamaServer(t, responses)
	defer server.Close()

	client := NewOllama("llama2", server.URL)
	ctx := context.Background()

	req := &Request{
		Model:  "llama2",
		System: "You are a helpful assistant.",
		Messages: []MessageContent{{
			Role: "user",
			Content: []ContentBlock{
				{Type: "text", Text: "who are you?"},
			},
		}},
	}

	resp, err := client.Generate(ctx, req)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(resp.Content) == 0 {
		t.Fatal("expected response content")
	}
}
