package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/marcopeereboom/go-claude/pkg/llm"
)

// listModelsCommand handles --models-list flag
func listModelsCommand(claudeDir, ollamaURL string) error {
	cache, err := loadModelsCache(claudeDir)
	if err != nil || cache == nil {
		// No cache exists - fetch and create
		cache, err = refreshModelsCache(claudeDir, ollamaURL)
		if err != nil {
			return fmt.Errorf("fetching models: %w", err)
		}
	}

	fmt.Fprintln(os.Stderr, "Available models:")
	for _, model := range cache.Models {
		fmt.Fprintf(os.Stderr, "  %s (%s)\n", model.Name, model.Provider)
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "Last updated: %s\n", cache.LastUpdated.Format("2006-01-02 15:04:05"))

	return nil
}

// refreshModelsCommand handles --models-refresh flag
func refreshModelsCommand(claudeDir, ollamaURL string) error {
	cache, err := refreshModelsCache(claudeDir, ollamaURL)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Refreshed models cache\n")
	fmt.Fprintf(os.Stderr, "  Total models: %d\n", len(cache.Models))
	fmt.Fprintf(os.Stderr, "  Saved to: %s\n",
		filepath.Join(claudeDir, "models.json"))

	return nil
}

// refreshModelsCache queries Claude and Ollama for available models
func refreshModelsCache(claudeDir, ollamaURL string) (*ModelsCache, error) {
	ctx := context.Background()

	var allModels []llm.ModelInfo

	// Query Claude models
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey != "" {
		client := llm.NewClaude(apiKey, apiURL)
		models, err := client.ListModels(ctx)
		if err != nil {
			// Non-fatal: continue with hardcoded list
			fmt.Fprintf(os.Stderr,
				"Warning: couldn't fetch Claude models: %v\n", err)
			allModels = append(allModels, getDefaultClaudeModels()...)
		} else {
			allModels = append(allModels, models...)
		}
	} else {
		// No API key - use defaults
		allModels = append(allModels, getDefaultClaudeModels()...)
	}

	// Query Ollama models
	ollamaClient := llm.NewOllama("", ollamaURL)
	ollamaModels, err := ollamaClient.ListModels(ctx)
	if err != nil {
		// Non-fatal: Ollama might not be running
		fmt.Fprintf(os.Stderr, "Warning: couldn't fetch Ollama models: %v\n",
			err)
	} else {
		allModels = append(allModels, ollamaModels...)
	}

	// Sort by provider then name
	sort.Slice(allModels, func(i, j int) bool {
		if allModels[i].Provider != allModels[j].Provider {
			return allModels[i].Provider < allModels[j].Provider
		}
		return allModels[i].Name < allModels[j].Name
	})

	cache := &ModelsCache{
		LastUpdated: time.Now(),
		Models:      allModels,
	}

	// Save cache
	if err := saveModelsCache(claudeDir, cache); err != nil {
		return nil, fmt.Errorf("saving models cache: %w", err)
	}

	return cache, nil
}

// getDefaultClaudeModels returns hardcoded list of Claude models
// Used when API query fails or no API key available
func getDefaultClaudeModels() []llm.ModelInfo {
	return []llm.ModelInfo{
		{Name: "claude-opus-4-20250514", Provider: "claude"},
		{Name: "claude-sonnet-4-20250514", Provider: "claude"},
		{Name: "claude-sonnet-4-5-20250929", Provider: "claude"},
		{Name: "claude-haiku-4-5-20251001", Provider: "claude"},
		{Name: "claude-3-5-sonnet-20241022", Provider: "claude"},
		{Name: "claude-3-5-haiku-20241022", Provider: "claude"},
	}
}

// validateModel checks if model exists in cache
// If no cache, creates one and validates
func validateModel(model, claudeDir, ollamaURL string) error {
	cache, err := loadModelsCache(claudeDir)
	if err != nil || cache == nil {
		// Try to create cache
		cache, err = refreshModelsCache(claudeDir, ollamaURL)
		if err != nil {
			// Can't validate - allow it
			return nil
		}
	}

	// Check if model is in list
	for _, m := range cache.Models {
		if m.Name == model {
			return nil
		}
	}

	// Model not found - but this might be okay if cache is stale
	// Just warn, don't error
	fmt.Fprintf(os.Stderr,
		"Warning: model %s not in cache (run --models-refresh to update)\n",
		model)
	return nil
}
