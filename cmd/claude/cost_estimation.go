package main

import (
	"fmt"
	"os"
	"strings"
)

// CostEstimate represents estimated token usage and cost
type CostEstimate struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	InputCost    float64
	OutputCost   float64
	TotalCost    float64
	Model        string
}

// ModelPricing holds per-million-token pricing for a model
type ModelPricing struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

// estimateCost calculates rough cost estimate based on conversation size
func estimateCost(userMsg string, history []MessageContent, model string) *CostEstimate {
	// Count tokens in conversation history
	historyTokens := 0
	for _, msg := range history {
		for _, block := range msg.Content {
			if block.Type == "text" {
				historyTokens += len(block.Text) / 4
			}
		}
	}

	// Count tokens in user message
	userTokens := len(userMsg) / 4

	// System prompt tokens (roughly)
	systemTokens := len(defaultSystemPrompt) / 4

	// Total input
	inputTokens := historyTokens + userTokens + systemTokens

	// Estimate output (heuristic: 30% of input, min 500)
	outputTokens := inputTokens / 3
	if outputTokens < 500 {
		outputTokens = 500
	}

	// Get pricing for model
	pricing := getModelPricing(model)

	// Calculate cost
	inputCost := float64(inputTokens) * pricing.InputPerMillion / 1_000_000
	outputCost := float64(outputTokens) * pricing.OutputPerMillion / 1_000_000
	totalCost := inputCost + outputCost

	return &CostEstimate{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  inputTokens + outputTokens,
		InputCost:    inputCost,
		OutputCost:   outputCost,
		TotalCost:    totalCost,
		Model:        model,
	}
}

// getModelPricing returns pricing per million tokens for a model
func getModelPricing(model string) ModelPricing {
	// Sonnet 4.5 pricing
	if strings.Contains(model, "sonnet") {
		return ModelPricing{
			InputPerMillion:  3.0,
			OutputPerMillion: 15.0,
		}
	}

	// Opus pricing
	if strings.Contains(model, "opus") {
		return ModelPricing{
			InputPerMillion:  15.0,
			OutputPerMillion: 75.0,
		}
	}

	// Haiku pricing
	if strings.Contains(model, "haiku") {
		return ModelPricing{
			InputPerMillion:  0.80,
			OutputPerMillion: 4.0,
		}
	}

	// Default to Sonnet
	return ModelPricing{
		InputPerMillion:  3.0,
		OutputPerMillion: 15.0,
	}
}

// getLastUserMessage extracts the most recent user message from conversation
func getLastUserMessage(messages []MessageContent) (string, error) {
	// Search backwards for last user message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			for _, block := range messages[i].Content {
				if block.Type == "text" {
					return block.Text, nil
				}
			}
		}
	}
	return "", fmt.Errorf("no user message found in conversation")
}

// displayEstimate shows cost estimation to user
func displayEstimate(estimate *CostEstimate) {
	fmt.Fprintln(os.Stderr, "\nAnalyzing task...")
	fmt.Fprintln(os.Stderr, "\nEstimated Execution:")
	fmt.Fprintf(os.Stderr, "  Input tokens:  ~%d\n", estimate.InputTokens)
	fmt.Fprintf(os.Stderr, "  Output tokens: ~%d\n", estimate.OutputTokens)
	fmt.Fprintf(os.Stderr, "  Total cost:    ~$%.3f\n\n", estimate.TotalCost)
	fmt.Fprintf(os.Stderr, "  Model: %s\n", estimate.Model)
	
	pricing := getModelPricing(estimate.Model)
	fmt.Fprintf(os.Stderr, "  Pricing: $%.2f/million input, $%.2f/million output\n\n",
		pricing.InputPerMillion, pricing.OutputPerMillion)

	// Suggest execution command with 50% buffer
	suggestedCost := estimate.TotalCost * 1.5
	fmt.Fprintf(os.Stderr, "To execute: claude --execute --max-cost-override=%.2f\n", suggestedCost)
}
