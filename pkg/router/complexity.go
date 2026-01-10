package router

import (
	"strings"
)

// TaskComplexity represents how difficult a task is
type TaskComplexity int

const (
	// ComplexitySimple - basic questions, explanations, simple code
	ComplexitySimple TaskComplexity = iota
	// ComplexityModerate - code generation, analysis, multi-step logic
	ComplexityModerate
	// ComplexityComplex - file operations, bash commands, complex reasoning
	ComplexityComplex
)

// RequiredFeatures represents what features a task needs
type RequiredFeatures struct {
	NeedsTools        bool
	NeedsVision       bool
	NeedsLargeContext bool
}

// TaskAnalysis contains the result of analyzing a user prompt
type TaskAnalysis struct {
	Complexity TaskComplexity
	Features   RequiredFeatures
	Reasoning  string
}

// AnalyzeTask examines a user prompt and determines its complexity and required features
func AnalyzeTask(prompt string) TaskAnalysis {
	lower := strings.ToLower(prompt)

	analysis := TaskAnalysis{
		Complexity: ComplexitySimple,
		Features:   RequiredFeatures{},
		Reasoning:  "Simple question or explanation",
	}

	// Check for tool requirements (file operations, bash commands)
	toolKeywords := []string{
		"read file", "write file", "to file", "create file", "create a file",
		"modify file", "modify the file", "edit file",
		"run command", "execute", "bash", "shell", "script",
		"search", "find files", "grep",
	}
	for _, keyword := range toolKeywords {
		if strings.Contains(lower, keyword) {
			analysis.Features.NeedsTools = true
			analysis.Complexity = ComplexityComplex
			analysis.Reasoning = "Requires tool execution (file operations or bash commands)"
			break
		}
	}

	// Check for vision requirements
	visionKeywords := []string{"image", "picture", "photo", "diagram", "screenshot", "visual"}
	for _, keyword := range visionKeywords {
		if strings.Contains(lower, keyword) {
			analysis.Features.NeedsVision = true
			if analysis.Complexity < ComplexityModerate {
				analysis.Complexity = ComplexityModerate
				analysis.Reasoning = "May involve image analysis"
			}
			break
		}
	}

	// Check for complex reasoning requirements
	complexKeywords := []string{
		"refactor", "redesign", "architecture", "design pattern",
		"optimize", "performance", "security audit",
		"debug complex", "multi-step", "chain of thought",
	}
	for _, keyword := range complexKeywords {
		if strings.Contains(lower, keyword) {
			if analysis.Complexity < ComplexityComplex {
				analysis.Complexity = ComplexityComplex
				analysis.Reasoning = "Requires complex reasoning and analysis"
			}
			break
		}
	}

	// Check for moderate complexity (code generation, analysis)
	if analysis.Complexity == ComplexitySimple {
		moderateKeywords := []string{
			"write code", "generate code", "implement", "function",
			"algorithm", "data structure", "class", "api",
			"review", "analyze", "explain code",
		}
		for _, keyword := range moderateKeywords {
			if strings.Contains(lower, keyword) {
				analysis.Complexity = ComplexityModerate
				analysis.Reasoning = "Code generation or analysis task"
				break
			}
		}
	}

	// Check for large context requirements (long files, multiple files)
	largeContextKeywords := []string{
		"entire codebase", "all files", "multiple files",
		"long file", "large file", "full context",
	}
	for _, keyword := range largeContextKeywords {
		if strings.Contains(lower, keyword) {
			analysis.Features.NeedsLargeContext = true
			break
		}
	}

	return analysis
}

// String returns a human-readable complexity level
func (c TaskComplexity) String() string {
	switch c {
	case ComplexitySimple:
		return "simple"
	case ComplexityModerate:
		return "moderate"
	case ComplexityComplex:
		return "complex"
	default:
		return "unknown"
	}
}
