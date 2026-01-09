package claude_test

import (
	"strings"
	"testing"

	"github.com/marcopeereboom/go-claude/pkg/claude"
)

func TestEstimateCost(t *testing.T) {
	messages := []claude.MessageContent{
		{
			Role: "user",
			Content: []claude.ContentBlock{
				{Type: "text", Text: strings.Repeat("a", 4000)},
			},
		},
	}

	userMsg := strings.Repeat("b", 2000)
	model := "claude-sonnet-4-5-20250929"

	estimate := claude.EstimateCost(userMsg, messages, model)

	// Check ballpark (rough heuristic)
	if estimate.InputTokens < 1000 || estimate.InputTokens > 3000 {
		t.Errorf("unexpected input tokens: %d", estimate.InputTokens)
	}

	if estimate.TotalCost < 0.01 || estimate.TotalCost > 0.10 {
		t.Errorf("unexpected cost: $%.3f", estimate.TotalCost)
	}
}

func TestGetLastUserMessage(t *testing.T) {
	messages := []claude.MessageContent{
		{
			Role: "user",
			Content: []claude.ContentBlock{
				{Type: "text", Text: "first message"},
			},
		},
		{
			Role: "assistant",
			Content: []claude.ContentBlock{
				{Type: "text", Text: "response"},
			},
		},
		{
			Role: "user",
			Content: []claude.ContentBlock{
				{Type: "text", Text: "second message"},
			},
		},
	}

	msg, err := claude.GetLastUserMessage(messages)
	if err != nil {
		t.Fatalf("GetLastUserMessage failed: %v", err)
	}

	if msg != "second message" {
		t.Errorf("got %q, want %q", msg, "second message")
	}
}

func TestGetLastUserMessage_Empty(t *testing.T) {
	messages := []claude.MessageContent{}

	_, err := claude.GetLastUserMessage(messages)
	if err == nil {
		t.Error("expected error for empty messages")
	}
}

func TestGetModelPricing(t *testing.T) {
	tests := []struct {
		model               string
		expectedInput       float64
		expectedOutput      float64
	}{
		{"claude-sonnet-4-5-20250929", 3.0, 15.0},
		{"claude-opus-4-20250514", 15.0, 75.0},
		{"claude-haiku-4-5-20251001", 0.80, 4.0},
		{"unknown-model", 3.0, 15.0}, // defaults to sonnet
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			pricing := claude.GetModelPricing(tt.model)
			if pricing.InputPerMillion != tt.expectedInput {
				t.Errorf("input: got %.2f, want %.2f", pricing.InputPerMillion, tt.expectedInput)
			}
			if pricing.OutputPerMillion != tt.expectedOutput {
				t.Errorf("output: got %.2f, want %.2f", pricing.OutputPerMillion, tt.expectedOutput)
			}
		})
	}
}
