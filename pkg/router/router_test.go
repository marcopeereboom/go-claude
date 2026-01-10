package router_test

import (
	"context"
	"testing"

	"github.com/marcopeereboom/go-claude/pkg/llm"
	"github.com/marcopeereboom/go-claude/pkg/router"
	"github.com/marcopeereboom/go-claude/pkg/storage"
)

// Mock LLM client for testing
type mockLLM struct {
	caps llm.ModelCapabilities
}

func (m *mockLLM) Generate(ctx context.Context, req *llm.Request) (*llm.Response, error) {
	return &llm.Response{}, nil
}

func (m *mockLLM) ListModels(ctx context.Context) ([]llm.ModelInfo, error) {
	return nil, nil
}

func (m *mockLLM) GetCapabilities() llm.ModelCapabilities {
	return m.caps
}

func TestRouter_SimpleTask_PreferLocal(t *testing.T) {
	ollama := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools: true,
			Provider:      "ollama",
		},
	}
	claude := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools: true,
			Provider:      "claude",
		},
	}

	config := &storage.Config{}
	opts := router.Options{
		PreferLocal:    true,
		AllowFallback:  true,
		MaxClaudeRatio: 0.1,
		OllamaModel:    "llama3.1:8b",
		ClaudeModel:    "claude-sonnet-4",
	}

	r := router.NewRouter(ollama, claude, config, opts)
	decision, err := r.Route("What is the capital of France?")

	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	if decision.Provider != "ollama" {
		t.Errorf("Expected ollama for simple task, got %s", decision.Provider)
	}

	if decision.ModelName != "llama3.1:8b" {
		t.Errorf("Expected llama3.1:8b, got %s", decision.ModelName)
	}
}

func TestRouter_ComplexTask_UseClaude(t *testing.T) {
	ollama := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools: true,
			Provider:      "ollama",
		},
	}
	claude := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools: true,
			Provider:      "claude",
		},
	}

	config := &storage.Config{}
	opts := router.Options{
		PreferLocal:    true,
		AllowFallback:  true,
		MaxClaudeRatio: 0.1,
		OllamaModel:    "llama3.1:8b",
		ClaudeModel:    "claude-sonnet-4",
	}

	r := router.NewRouter(ollama, claude, config, opts)
	decision, err := r.Route("Read main.go and refactor it to use better patterns")

	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	if decision.Provider != "claude" {
		t.Errorf("Expected claude for complex task, got %s", decision.Provider)
	}

	if decision.FallbackAllowed {
		t.Error("Should not allow fallback for complex task that requires Claude")
	}
}

func TestRouter_OverQuota_ForceOllama(t *testing.T) {
	ollama := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools: true,
			Provider:      "ollama",
		},
	}
	claude := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools: true,
			Provider:      "claude",
		},
	}

	// Config with 15% Claude usage (over 10% quota)
	config := &storage.Config{
		ClaudeStats: storage.ProviderStats{RequestCount: 15},
		OllamaStats: storage.ProviderStats{RequestCount: 85},
	}

	opts := router.Options{
		PreferLocal:    true,
		AllowFallback:  true,
		MaxClaudeRatio: 0.1, // 10% max
		OllamaModel:    "llama3.1:8b",
		ClaudeModel:    "claude-sonnet-4",
	}

	r := router.NewRouter(ollama, claude, config, opts)
	decision, err := r.Route("What is 2+2?")

	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	if decision.Provider != "ollama" {
		t.Errorf("Expected ollama when over quota, got %s", decision.Provider)
	}
}

func TestRouter_OverQuota_ButNeedsClaude(t *testing.T) {
	ollama := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools: false, // Doesn't support tools
			Provider:      "ollama",
		},
	}
	claude := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools: true,
			Provider:      "claude",
		},
	}

	// Config with 15% Claude usage (over 10% quota)
	config := &storage.Config{
		ClaudeStats: storage.ProviderStats{RequestCount: 15},
		OllamaStats: storage.ProviderStats{RequestCount: 85},
	}

	opts := router.Options{
		PreferLocal:    true,
		AllowFallback:  true,
		MaxClaudeRatio: 0.1,
		OllamaModel:    "codellama:7b",
		ClaudeModel:    "claude-sonnet-4",
		RequireTools:   true,
	}

	r := router.NewRouter(ollama, claude, config, opts)
	decision, err := r.Route("Read test.go and write a fix")

	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	if decision.Provider != "claude" {
		t.Errorf("Expected claude when Ollama can't handle task, got %s", decision.Provider)
	}

	if decision.FallbackAllowed {
		t.Error("Should not allow fallback when we're forced to use Claude")
	}
}

func TestRouter_ModerateTask_OllamaWithTools(t *testing.T) {
	ollama := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools: true,
			Provider:      "ollama",
		},
	}
	claude := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools: true,
			Provider:      "claude",
		},
	}

	config := &storage.Config{}
	opts := router.Options{
		PreferLocal:    true,
		AllowFallback:  true,
		MaxClaudeRatio: 0.1,
		OllamaModel:    "llama3.1:8b",
		ClaudeModel:    "claude-sonnet-4",
	}

	r := router.NewRouter(ollama, claude, config, opts)
	decision, err := r.Route("Write a function to calculate fibonacci")

	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	if decision.Provider != "ollama" {
		t.Errorf("Expected ollama for moderate task with tool support, got %s", decision.Provider)
	}
}

func TestRouter_ModerateTask_NoTools_UseClaude(t *testing.T) {
	ollama := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools: false, // No tool support
			Provider:      "ollama",
		},
	}
	claude := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools: true,
			Provider:      "claude",
		},
	}

	config := &storage.Config{}
	opts := router.Options{
		PreferLocal:    true,
		AllowFallback:  true,
		MaxClaudeRatio: 0.1,
		OllamaModel:    "codellama:7b",
		ClaudeModel:    "claude-sonnet-4",
		RequireTools:   true,
	}

	r := router.NewRouter(ollama, claude, config, opts)
	decision, err := r.Route("Write a function to calculate fibonacci")

	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	if decision.Provider != "claude" {
		t.Errorf("Expected claude when tools required but Ollama doesn't support, got %s", decision.Provider)
	}
}

func TestRouter_VisionRequired_UseClaude(t *testing.T) {
	ollama := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools:  true,
			SupportsVision: false,
			Provider:       "ollama",
		},
	}
	claude := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools:  true,
			SupportsVision: true,
			Provider:       "claude",
		},
	}

	config := &storage.Config{}
	opts := router.Options{
		PreferLocal:    true,
		AllowFallback:  true,
		MaxClaudeRatio: 0.1,
		OllamaModel:    "llama3.1:8b",
		ClaudeModel:    "claude-sonnet-4",
		RequireVision:  true,
	}

	r := router.NewRouter(ollama, claude, config, opts)
	decision, err := r.Route("Describe this image")

	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	if decision.Provider != "claude" {
		t.Errorf("Expected claude when vision required, got %s", decision.Provider)
	}
}

func TestRouter_LargeContext_UseClaude(t *testing.T) {
	ollama := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools:    true,
			MaxContextTokens: 8192,
			Provider:         "ollama",
		},
	}
	claude := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools:    true,
			MaxContextTokens: 200000,
			Provider:         "claude",
		},
	}

	config := &storage.Config{}
	opts := router.Options{
		PreferLocal:    true,
		AllowFallback:  true,
		MaxClaudeRatio: 0.1,
		OllamaModel:    "llama3.1:8b",
		ClaudeModel:    "claude-sonnet-4",
		LargeContext:   true,
	}

	r := router.NewRouter(ollama, claude, config, opts)
	decision, err := r.Route("Analyze this entire codebase")

	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	if decision.Provider != "claude" {
		t.Errorf("Expected claude when large context required, got %s", decision.Provider)
	}
}

func TestRouter_NoPreferLocal_DefaultClaude(t *testing.T) {
	ollama := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools: true,
			Provider:      "ollama",
		},
	}
	claude := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools: true,
			Provider:      "claude",
		},
	}

	config := &storage.Config{}
	opts := router.Options{
		PreferLocal:    false, // Don't prefer local
		AllowFallback:  false,
		MaxClaudeRatio: 1.0, // No quota
		OllamaModel:    "llama3.1:8b",
		ClaudeModel:    "claude-sonnet-4",
	}

	r := router.NewRouter(ollama, claude, config, opts)
	decision, err := r.Route("What is 2+2?")

	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	if decision.Provider != "claude" {
		t.Errorf("Expected claude when not preferring local, got %s", decision.Provider)
	}
}

func TestDecision_String(t *testing.T) {
	decision := &router.Decision{
		Provider:  "ollama",
		ModelName: "llama3.1:8b",
		Reason:    "simple task",
	}

	str := decision.String()
	expected := "ollama (llama3.1:8b): simple task"

	if str != expected {
		t.Errorf("Expected %q, got %q", expected, str)
	}
}

func TestRouter_NilOllamaClient(t *testing.T) {
	claude := &mockLLM{
		caps: llm.ModelCapabilities{
			SupportsTools: true,
			Provider:      "claude",
		},
	}

	config := &storage.Config{}
	opts := router.Options{
		PreferLocal:    true,
		AllowFallback:  true,
		MaxClaudeRatio: 0.1,
		OllamaModel:    "llama3.1:8b",
		ClaudeModel:    "claude-sonnet-4",
	}

	r := router.NewRouter(nil, claude, config, opts) // No Ollama client
	decision, err := r.Route("What is 2+2?")

	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}

	// Should fall back to Claude when Ollama not available
	if decision.Provider != "claude" {
		t.Errorf("Expected claude when Ollama not available, got %s", decision.Provider)
	}
}
