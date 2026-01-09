package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Test helper types to match Claude API response structure
type claudeResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model"`
	StopReason string         `json:"stop_reason"`
	Usage      Usage          `json:"usage"`
}

// mockClaudeServer creates a test HTTP server that simulates Claude API
func mockClaudeServer(t *testing.T, responses []claudeResponse) *httptest.Server {
	callCount := 0

	return httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// Verify request headers
			if r.Header.Get("anthropic-version") != claudeAPIVersion {
				t.Errorf("wrong API version header: got %s", r.Header.Get("anthropic-version"))
			}
			if r.Header.Get("x-api-key") == "" {
				t.Errorf("missing API key")
			}
			if r.Header.Get("content-type") != "application/json" {
				t.Errorf("wrong content type: got %s", r.Header.Get("content-type"))
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

func TestClaudeGenerate_Success(t *testing.T) {
	responses := []claudeResponse{{
		ID:   "msg_test",
		Type: "message",
		Role: "assistant",
		Content: []ContentBlock{
			{Type: "text", Text: "Hello, world!"},
		},
		Model:      "claude-sonnet-4-5-20250929",
		StopReason: "end_turn",
		Usage: Usage{
			InputTokens:  10,
			OutputTokens: 5,
		},
	}}

	server := mockClaudeServer(t, responses)
	defer server.Close()

	client := NewClaude("test-key", server.URL)
	ctx := context.Background()

	req := &Request{
		Model:     "claude-sonnet-4-5-20250929",
		MaxTokens: 100,
		Messages: []MessageContent{{
			Role: "user",
			Content: []ContentBlock{
				{Type: "text", Text: "Hello"},
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

	if len(resp.Content) != 1 || resp.Content[0].Text != "Hello, world!" {
		t.Errorf("unexpected content: %+v", resp.Content)
	}

	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 5 {
		t.Errorf("unexpected usage: %+v", resp.Usage)
	}
}

func TestClaudeGenerate_ToolUse(t *testing.T) {
	responses := []claudeResponse{{
		ID:   "msg_tool",
		Type: "message",
		Role: "assistant",
		Content: []ContentBlock{{
			Type: "tool_use",
			ID:   "tool_123",
			Name: "read_file",
			Input: map[string]interface{}{
				"path": "test.txt",
			},
		}},
		Model:      "claude-sonnet-4-5-20250929",
		StopReason: "tool_use",
		Usage: Usage{
			InputTokens:  20,
			OutputTokens: 15,
		},
	}}

	server := mockClaudeServer(t, responses)
	defer server.Close()

	client := NewClaude("test-key", server.URL)
	ctx := context.Background()

	req := &Request{
		Model:     "claude-sonnet-4-5-20250929",
		MaxTokens: 100,
		Messages: []MessageContent{{
			Role: "user",
			Content: []ContentBlock{
				{Type: "text", Text: "Read test.txt"},
			},
		}},
		Tools: []Tool{{
			Name:        "read_file",
			Description: "Read a file",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]string{"type": "string"},
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

	if len(resp.Content) != 1 || resp.Content[0].Type != "tool_use" {
		t.Errorf("expected tool_use content: %+v", resp.Content)
	}
}

func TestClaudeGenerate_Errors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   interface{}
		wantErr    string
	}{
		{
			name:       "rate limit",
			statusCode: http.StatusTooManyRequests,
			response: map[string]interface{}{
				"error": map[string]string{
					"type":    "rate_limit_error",
					"message": "Rate limit exceeded",
				},
			},
			wantErr: "rate_limit_error",
		},
		{
			name:       "invalid request",
			statusCode: http.StatusBadRequest,
			response: map[string]interface{}{
				"error": map[string]string{
					"type":    "invalid_request_error",
					"message": "Invalid parameters",
				},
			},
			wantErr: "invalid_request_error",
		},
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			response:   map[string]interface{}{},
			wantErr:    "API error 401",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.statusCode)
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(tt.response)
				}))
			defer server.Close()

			client := NewClaude("test-key", server.URL)
			ctx := context.Background()

			req := &Request{
				Model:     "claude-sonnet-4-5-20250929",
				MaxTokens: 100,
				Messages: []MessageContent{{
					Role: "user",
					Content: []ContentBlock{
						{Type: "text", Text: "test"},
					},
				}},
			}

			_, err := client.Generate(ctx, req)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if tt.wantErr != "" && !contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestClaudeGenerate_ContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// Simulate slow response
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
	defer server.Close()

	client := NewClaude("test-key", server.URL)
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	req := &Request{
		Model:     "claude-sonnet-4-5-20250929",
		MaxTokens: 100,
		Messages: []MessageContent{{
			Role: "user",
			Content: []ContentBlock{
				{Type: "text", Text: "test"},
			},
		}},
	}

	_, err := client.Generate(ctx, req)
	if err == nil {
		t.Fatal("expected error due to cancelled context")
	}

	if !contains(err.Error(), "context canceled") {
		t.Errorf("expected context canceled error, got: %v", err)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
