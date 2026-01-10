package storage_test

import (
	"testing"

	"github.com/marcopeereboom/go-claude/pkg/storage"
)

func TestUpdateProviderStats(t *testing.T) {
	tests := []struct {
		name         string
		provider     string
		inputTokens  int
		outputTokens int
		wantClaude   storage.ProviderStats
		wantOllama   storage.ProviderStats
	}{
		{
			name:         "update claude stats",
			provider:     "claude",
			inputTokens:  100,
			outputTokens: 200,
			wantClaude: storage.ProviderStats{
				RequestCount: 1,
				TokensInput:  100,
				TokensOutput: 200,
			},
			wantOllama: storage.ProviderStats{},
		},
		{
			name:         "update ollama stats",
			provider:     "ollama",
			inputTokens:  500,
			outputTokens: 1000,
			wantClaude:   storage.ProviderStats{},
			wantOllama: storage.ProviderStats{
				RequestCount: 1,
				TokensInput:  500,
				TokensOutput: 1000,
			},
		},
		{
			name:         "unknown provider does nothing",
			provider:     "unknown",
			inputTokens:  100,
			outputTokens: 200,
			wantClaude:   storage.ProviderStats{},
			wantOllama:   storage.ProviderStats{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &storage.Config{}
			storage.UpdateProviderStats(cfg, tt.provider, tt.inputTokens, tt.outputTokens)

			if cfg.ClaudeStats != tt.wantClaude {
				t.Errorf("ClaudeStats = %+v, want %+v", cfg.ClaudeStats, tt.wantClaude)
			}
			if cfg.OllamaStats != tt.wantOllama {
				t.Errorf("OllamaStats = %+v, want %+v", cfg.OllamaStats, tt.wantOllama)
			}

			// Check totals are updated
			expectedTotal := tt.inputTokens + tt.outputTokens
			actualTotal := cfg.TotalInput + cfg.TotalOutput
			if tt.provider != "unknown" && actualTotal != expectedTotal {
				t.Errorf("Total tokens = %d, want %d", actualTotal, expectedTotal)
			}
		})
	}
}

func TestUpdateProviderStats_Multiple(t *testing.T) {
	cfg := &storage.Config{}

	// 2 Claude requests
	storage.UpdateProviderStats(cfg, "claude", 100, 200)
	storage.UpdateProviderStats(cfg, "claude", 150, 250)

	// 8 Ollama requests
	for i := 0; i < 8; i++ {
		storage.UpdateProviderStats(cfg, "ollama", 50, 100)
	}

	if cfg.ClaudeStats.RequestCount != 2 {
		t.Errorf("ClaudeStats.RequestCount = %d, want 2", cfg.ClaudeStats.RequestCount)
	}
	if cfg.ClaudeStats.TokensInput != 250 {
		t.Errorf("ClaudeStats.TokensInput = %d, want 250", cfg.ClaudeStats.TokensInput)
	}
	if cfg.ClaudeStats.TokensOutput != 450 {
		t.Errorf("ClaudeStats.TokensOutput = %d, want 450", cfg.ClaudeStats.TokensOutput)
	}

	if cfg.OllamaStats.RequestCount != 8 {
		t.Errorf("OllamaStats.RequestCount = %d, want 8", cfg.OllamaStats.RequestCount)
	}
	if cfg.OllamaStats.TokensInput != 400 {
		t.Errorf("OllamaStats.TokensInput = %d, want 400", cfg.OllamaStats.TokensInput)
	}
	if cfg.OllamaStats.TokensOutput != 800 {
		t.Errorf("OllamaStats.TokensOutput = %d, want 800", cfg.OllamaStats.TokensOutput)
	}

	// Check totals
	expectedInput := 250 + 400
	expectedOutput := 450 + 800
	if cfg.TotalInput != expectedInput {
		t.Errorf("TotalInput = %d, want %d", cfg.TotalInput, expectedInput)
	}
	if cfg.TotalOutput != expectedOutput {
		t.Errorf("TotalOutput = %d, want %d", cfg.TotalOutput, expectedOutput)
	}
}

func TestGetClaudeUsageRatio(t *testing.T) {
	tests := []struct {
		name         string
		claudeReqs   int
		ollamaReqs   int
		wantRatio    float64
		wantRatioStr string
	}{
		{
			name:         "no requests",
			claudeReqs:   0,
			ollamaReqs:   0,
			wantRatio:    0.0,
			wantRatioStr: "0%",
		},
		{
			name:         "only claude",
			claudeReqs:   10,
			ollamaReqs:   0,
			wantRatio:    1.0,
			wantRatioStr: "100%",
		},
		{
			name:         "only ollama",
			claudeReqs:   0,
			ollamaReqs:   10,
			wantRatio:    0.0,
			wantRatioStr: "0%",
		},
		{
			name:         "10% claude (target ratio)",
			claudeReqs:   1,
			ollamaReqs:   9,
			wantRatio:    0.1,
			wantRatioStr: "10%",
		},
		{
			name:         "20% claude (over quota)",
			claudeReqs:   2,
			ollamaReqs:   8,
			wantRatio:    0.2,
			wantRatioStr: "20%",
		},
		{
			name:         "50-50 split",
			claudeReqs:   5,
			ollamaReqs:   5,
			wantRatio:    0.5,
			wantRatioStr: "50%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &storage.Config{
				ClaudeStats: storage.ProviderStats{RequestCount: tt.claudeReqs},
				OllamaStats: storage.ProviderStats{RequestCount: tt.ollamaReqs},
			}

			ratio := storage.GetClaudeUsageRatio(cfg)
			if ratio != tt.wantRatio {
				t.Errorf("GetClaudeUsageRatio() = %.2f, want %.2f (%s)",
					ratio, tt.wantRatio, tt.wantRatioStr)
			}
		})
	}
}

func TestIsOverClaudeQuota(t *testing.T) {
	tests := []struct {
		name       string
		claudeReqs int
		ollamaReqs int
		maxRatio   float64
		wantOver   bool
	}{
		{
			name:       "no usage - under quota",
			claudeReqs: 0,
			ollamaReqs: 0,
			maxRatio:   0.1,
			wantOver:   false,
		},
		{
			name:       "exactly at 10% quota",
			claudeReqs: 1,
			ollamaReqs: 9,
			maxRatio:   0.1,
			wantOver:   false,
		},
		{
			name:       "under 10% quota (5%)",
			claudeReqs: 1,
			ollamaReqs: 19,
			maxRatio:   0.1,
			wantOver:   false,
		},
		{
			name:       "over 10% quota (20%)",
			claudeReqs: 2,
			ollamaReqs: 8,
			maxRatio:   0.1,
			wantOver:   true,
		},
		{
			name:       "way over quota (50%)",
			claudeReqs: 5,
			ollamaReqs: 5,
			maxRatio:   0.1,
			wantOver:   true,
		},
		{
			name:       "only claude - over any quota",
			claudeReqs: 10,
			ollamaReqs: 0,
			maxRatio:   0.1,
			wantOver:   true,
		},
		{
			name:       "only ollama - never over",
			claudeReqs: 0,
			ollamaReqs: 10,
			maxRatio:   0.1,
			wantOver:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &storage.Config{
				ClaudeStats: storage.ProviderStats{RequestCount: tt.claudeReqs},
				OllamaStats: storage.ProviderStats{RequestCount: tt.ollamaReqs},
			}

			isOver := storage.IsOverClaudeQuota(cfg, tt.maxRatio)
			if isOver != tt.wantOver {
				ratio := storage.GetClaudeUsageRatio(cfg)
				t.Errorf("IsOverClaudeQuota(%.2f) = %v, want %v (actual ratio: %.2f)",
					tt.maxRatio, isOver, tt.wantOver, ratio)
			}
		})
	}
}

func TestProviderStats_90_10_Ratio(t *testing.T) {
	// Test the target 90% Ollama / 10% Claude ratio
	cfg := &storage.Config{}

	// Simulate 90 Ollama requests
	for i := 0; i < 90; i++ {
		storage.UpdateProviderStats(cfg, "ollama", 100, 200)
	}

	// Simulate 10 Claude requests
	for i := 0; i < 10; i++ {
		storage.UpdateProviderStats(cfg, "claude", 1000, 2000)
	}

	// Check request counts
	if cfg.OllamaStats.RequestCount != 90 {
		t.Errorf("OllamaStats.RequestCount = %d, want 90", cfg.OllamaStats.RequestCount)
	}
	if cfg.ClaudeStats.RequestCount != 10 {
		t.Errorf("ClaudeStats.RequestCount = %d, want 10", cfg.ClaudeStats.RequestCount)
	}

	// Check ratio
	ratio := storage.GetClaudeUsageRatio(cfg)
	if ratio != 0.1 {
		t.Errorf("GetClaudeUsageRatio() = %.2f, want 0.10 (10%%)", ratio)
	}

	// Check quota enforcement
	if storage.IsOverClaudeQuota(cfg, 0.1) {
		t.Error("IsOverClaudeQuota(0.1) = true, want false (exactly at quota)")
	}

	// Add one more Claude request - should go over quota
	storage.UpdateProviderStats(cfg, "claude", 1000, 2000)
	if !storage.IsOverClaudeQuota(cfg, 0.1) {
		t.Error("IsOverClaudeQuota(0.1) = false, want true (over quota after 11th Claude request)")
	}
}
