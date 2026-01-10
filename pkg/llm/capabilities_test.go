package llm_test

import (
	"testing"

	"github.com/marcopeereboom/go-claude/pkg/llm"
)

func TestClaudeCapabilities(t *testing.T) {
	client := llm.NewClaude("test-key", "https://api.anthropic.com/v1/messages")
	caps := client.GetCapabilities()

	if !caps.SupportsTools {
		t.Error("Claude should support tools")
	}
	if !caps.SupportsVision {
		t.Error("Claude should support vision")
	}
	if !caps.SupportsStreaming {
		t.Error("Claude should support streaming")
	}
	if caps.Provider != "claude" {
		t.Errorf("Expected provider 'claude', got '%s'", caps.Provider)
	}
	if caps.MaxContextTokens <= 0 {
		t.Error("Claude should have positive max context tokens")
	}
	if len(caps.RecommendedForTasks) == 0 {
		t.Error("Claude should have recommended tasks")
	}
}

func TestOllamaCapabilities_ToolSupportDetection(t *testing.T) {
	tests := []struct {
		name          string
		model         string
		expectTools   bool
		expectedTasks []string
	}{
		{
			name:          "llama3.1 supports tools",
			model:         "llama3.1:8b",
			expectTools:   true,
			expectedTasks: []string{"chat"},
		},
		{
			name:          "llama3.1 70b supports tools",
			model:         "llama3.1:70b",
			expectTools:   true,
			expectedTasks: []string{"chat"},
		},
		{
			name:          "qwen2.5 supports tools",
			model:         "qwen2.5:32b",
			expectTools:   true,
			expectedTasks: []string{"chat"},
		},
		{
			name:          "mistral supports tools",
			model:         "mistral:7b",
			expectTools:   true,
			expectedTasks: []string{"chat"},
		},
		{
			name:          "codellama specializes in code",
			model:         "codellama:7b",
			expectTools:   false,
			expectedTasks: []string{"code", "programming"},
		},
		{
			name:          "deepseek-coder specializes in code",
			model:         "deepseek-coder:6.7b",
			expectTools:   false,
			expectedTasks: []string{"code", "programming"},
		},
		{
			name:          "qwen2.5-coder supports tools and code",
			model:         "qwen2.5-coder:7b",
			expectTools:   true,
			expectedTasks: []string{"code", "programming"},
		},
		{
			name:          "embed models for embeddings",
			model:         "mxbai-embed-large:latest",
			expectTools:   false,
			expectedTasks: []string{"embeddings"},
		},
		{
			name:          "llama3.2 small model",
			model:         "llama3.2:1b",
			expectTools:   true,
			expectedTasks: []string{"chat"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := llm.NewOllama(tt.model, "http://localhost:11434")
			caps := client.GetCapabilities()

			if caps.SupportsTools != tt.expectTools {
				t.Errorf("Model %s: expected SupportsTools=%v, got %v", tt.model, tt.expectTools, caps.SupportsTools)
			}

			if caps.Provider != "ollama" {
				t.Errorf("Expected provider 'ollama', got '%s'", caps.Provider)
			}

			if caps.MaxContextTokens <= 0 {
				t.Error("Should have positive max context tokens")
			}

			if len(caps.RecommendedForTasks) == 0 {
				t.Error("Should have recommended tasks")
			}

			// Check if recommended tasks match expectations
			tasksMatch := false
			for _, task := range caps.RecommendedForTasks {
				for _, expected := range tt.expectedTasks {
					if task == expected {
						tasksMatch = true
						break
					}
				}
			}
			if !tasksMatch {
				t.Errorf("Model %s: expected tasks %v, got %v", tt.model, tt.expectedTasks, caps.RecommendedForTasks)
			}
		})
	}
}

func TestOllamaCapabilities_ContextSizeDetection(t *testing.T) {
	tests := []struct {
		model            string
		minContextTokens int
	}{
		{"llama3.1:8b", 100000}, // 128k context
		{"qwen2.5:32b", 30000},  // 32k+ context
		{"mistral:7b", 8000},    // 8k+ context
		{"codellama:7b", 8000},  // 8k+ context
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			client := llm.NewOllama(tt.model, "http://localhost:11434")
			caps := client.GetCapabilities()

			if caps.MaxContextTokens < tt.minContextTokens {
				t.Errorf("Model %s: expected at least %d context tokens, got %d",
					tt.model, tt.minContextTokens, caps.MaxContextTokens)
			}
		})
	}
}

func TestModelCapabilities_AllProvidersImplement(t *testing.T) {
	// Verify both providers implement the GetCapabilities method
	var _ interface {
		GetCapabilities() llm.ModelCapabilities
	} = (*llm.ClaudeClient)(nil)

	var _ interface {
		GetCapabilities() llm.ModelCapabilities
	} = (*llm.OllamaClient)(nil)
}
