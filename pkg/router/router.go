package router

import (
	"fmt"

	"github.com/marcopeereboom/go-claude/pkg/llm"
	"github.com/marcopeereboom/go-claude/pkg/storage"
)

// Decision represents a routing decision for which LLM provider to use.
type Decision struct {
	Provider        string // "claude" or "ollama"
	ModelName       string
	Reason          string
	FallbackAllowed bool
}

// Options configures the routing behavior.
type Options struct {
	PreferLocal    bool    // Prefer Ollama when possible
	AllowFallback  bool    // Allow fallback from Ollama to Claude on failure
	MaxClaudeRatio float64 // Maximum ratio of Claude usage (0.0-1.0), e.g., 0.1 for 10%
	OllamaModel    string  // Ollama model to use
	ClaudeModel    string  // Claude model to use
	RequireTools   bool    // Task requires tool support
	RequireVision  bool    // Task requires vision support
	LargeContext   bool    // Task requires large context window
}

// Router makes intelligent decisions about which LLM provider to use.
type Router struct {
	ollamaClient llm.LLM
	claudeClient llm.LLM
	config       *storage.Config
	opts         Options
}

// NewRouter creates a new router instance.
func NewRouter(ollamaClient, claudeClient llm.LLM, config *storage.Config, opts Options) *Router {
	return &Router{
		ollamaClient: ollamaClient,
		claudeClient: claudeClient,
		config:       config,
		opts:         opts,
	}
}

// Route determines which provider to use based on task complexity, capabilities, and cost constraints.
func (r *Router) Route(prompt string) (*Decision, error) {
	// Analyze task complexity
	analysis := AnalyzeTask(prompt)

	// Get capabilities
	var ollamaCaps llm.ModelCapabilities
	if r.ollamaClient != nil {
		ollamaCaps = r.ollamaClient.GetCapabilities()
	}

	// Merge analysis features with explicit requirements
	needsTools := r.opts.RequireTools || analysis.Features.NeedsTools
	needsVision := r.opts.RequireVision || analysis.Features.NeedsVision
	needsLargeContext := r.opts.LargeContext || analysis.Features.NeedsLargeContext

	// Check if we're over Claude quota
	overQuota := storage.IsOverClaudeQuota(r.config, r.opts.MaxClaudeRatio)

	// Decision logic
	decision := &Decision{
		FallbackAllowed: r.opts.AllowFallback,
	}

	// Rule 1: If over quota, must use Ollama (unless impossible)
	if overQuota {
		if r.canUseOllama(&analysis, ollamaCaps, needsTools, needsVision) {
			decision.Provider = "ollama"
			decision.ModelName = r.opts.OllamaModel
			decision.Reason = fmt.Sprintf("over Claude quota (%.1f%%), using Ollama", storage.GetClaudeUsageRatio(r.config)*100)
			return decision, nil
		}
		// Can't use Ollama but over quota - must use Claude anyway
		decision.Provider = "claude"
		decision.ModelName = r.opts.ClaudeModel
		decision.Reason = "over quota but task requires Claude capabilities"
		decision.FallbackAllowed = false // No fallback makes sense here
		return decision, nil
	}

	// Rule 2: Vision or large context always needs Claude
	if needsVision || needsLargeContext {
		decision.Provider = "claude"
		decision.ModelName = r.opts.ClaudeModel

		reasons := []string{}
		if needsVision {
			reasons = append(reasons, "requires vision")
		}
		if needsLargeContext {
			reasons = append(reasons, "large context")
		}
		decision.Reason = fmt.Sprintf("requires Claude: %v", reasons)
		decision.FallbackAllowed = false
		return decision, nil
	}

	// Rule 3: Complex tasks go to Claude
	if analysis.Complexity == ComplexityComplex {
		decision.Provider = "claude"
		decision.ModelName = r.opts.ClaudeModel
		decision.Reason = "complex task requires Claude"
		decision.FallbackAllowed = false
		return decision, nil
	}

	// Rule 4: Check if Ollama can handle this task
	if r.opts.PreferLocal && r.canUseOllama(&analysis, ollamaCaps, needsTools, needsVision) {
		// Prefer Ollama for simple and moderate tasks
		if analysis.Complexity == ComplexitySimple || (analysis.Complexity == ComplexityModerate && ollamaCaps.SupportsTools) {
			decision.Provider = "ollama"
			decision.ModelName = r.opts.OllamaModel
			decision.Reason = fmt.Sprintf("local model capable (%s task)", analysis.Complexity)
			return decision, nil
		}
	}

	// Rule 5: Tools required but Ollama doesn't support them
	if needsTools && !ollamaCaps.SupportsTools {
		decision.Provider = "claude"
		decision.ModelName = r.opts.ClaudeModel
		decision.Reason = "requires tools, Ollama model doesn't support them"
		decision.FallbackAllowed = false
		return decision, nil
	}

	// Default: Use Ollama if prefer local AND client available, otherwise Claude
	if r.opts.PreferLocal && r.ollamaClient != nil {
		decision.Provider = "ollama"
		decision.ModelName = r.opts.OllamaModel
		decision.Reason = "default to local"
	} else {
		decision.Provider = "claude"
		decision.ModelName = r.opts.ClaudeModel
		decision.Reason = "default to Claude"
		decision.FallbackAllowed = false
	}

	return decision, nil
}

// canUseOllama checks if Ollama can handle the task given complexity and capabilities.
func (r *Router) canUseOllama(analysis *TaskAnalysis, caps llm.ModelCapabilities, needsTools, needsVision bool) bool {
	if r.ollamaClient == nil {
		return false
	}

	// Can't use Ollama if it lacks required capabilities
	if needsVision && !caps.SupportsVision {
		return false
	}

	if needsTools && !caps.SupportsTools {
		return false
	}

	// Simple tasks: Ollama can always handle
	if analysis.Complexity == ComplexitySimple {
		return true
	}

	// Moderate tasks: Can handle if has tools (when needed)
	if analysis.Complexity == ComplexityModerate {
		return true
	}

	// Complex tasks: Generally prefer Claude
	if analysis.Complexity == ComplexityComplex {
		return false
	}

	return false
}

// String returns a human-readable representation of the decision.
func (d *Decision) String() string {
	return fmt.Sprintf("%s (%s): %s", d.Provider, d.ModelName, d.Reason)
}
