package router_test

import (
	"strings"
	"testing"

	"github.com/marcopeereboom/go-claude/pkg/router"
)

func TestAnalyzeTask_SimpleQuestions(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
	}{
		{"basic question", "What is Go?"},
		{"explanation", "Explain how interfaces work"},
		{"documentation", "How do I use channels?"},
		{"simple help", "Tell me about goroutines"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis := router.AnalyzeTask(tt.prompt)
			if analysis.Complexity != router.ComplexitySimple {
				t.Errorf("Expected simple complexity, got %v", analysis.Complexity)
			}
			if analysis.Features.NeedsTools {
				t.Errorf("Simple question should not need tools")
			}
		})
	}
}

func TestAnalyzeTask_CodeGeneration(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
	}{
		{"write code", "Write code to parse JSON"},
		{"implement function", "Implement a function to calculate fibonacci"},
		{"generate API", "Generate an API for user management"},
		{"review code", "Review this code for issues"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis := router.AnalyzeTask(tt.prompt)
			if analysis.Complexity != router.ComplexityModerate {
				t.Errorf("Expected moderate complexity, got %v", analysis.Complexity)
			}
		})
	}
}

func TestAnalyzeTask_FileOperations(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
	}{
		{"read file", "Read file config.json and show me the contents"},
		{"write file", "Write this code to file main.go"},
		{"modify file", "Modify the file to add error handling"},
		{"create file", "Create a file called test.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis := router.AnalyzeTask(tt.prompt)
			if analysis.Complexity != router.ComplexityComplex {
				t.Errorf("Expected complex complexity, got %v", analysis.Complexity)
			}
			if !analysis.Features.NeedsTools {
				t.Errorf("File operations should need tools")
			}
		})
	}
}

func TestAnalyzeTask_BashCommands(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
	}{
		{"run command", "Run command to list files"},
		{"execute script", "Execute this bash script"},
		{"shell command", "Use shell to find all .go files"},
		{"grep", "Grep for TODO comments in the codebase"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis := router.AnalyzeTask(tt.prompt)
			if analysis.Complexity != router.ComplexityComplex {
				t.Errorf("Expected complex complexity, got %v", analysis.Complexity)
			}
			if !analysis.Features.NeedsTools {
				t.Errorf("Bash commands should need tools")
			}
		})
	}
}

func TestAnalyzeTask_ComplexReasoning(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
	}{
		{"refactor", "Refactor this codebase for better maintainability"},
		{"architecture", "Design the architecture for a microservices system"},
		{"optimize", "Optimize this algorithm for performance"},
		{"security audit", "Security audit this authentication code"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis := router.AnalyzeTask(tt.prompt)
			if analysis.Complexity != router.ComplexityComplex {
				t.Errorf("Expected complex complexity, got %v", analysis.Complexity)
			}
		})
	}
}

func TestAnalyzeTask_VisionRequirements(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
	}{
		{"image", "Analyze this image and explain what it shows"},
		{"diagram", "Look at this diagram and suggest improvements"},
		{"screenshot", "Debug this screenshot of an error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis := router.AnalyzeTask(tt.prompt)
			if !analysis.Features.NeedsVision {
				t.Errorf("Vision-related task should need vision features")
			}
			if analysis.Complexity < router.ComplexityModerate {
				t.Errorf("Vision tasks should be at least moderate complexity, got %v", analysis.Complexity)
			}
		})
	}
}

func TestAnalyzeTask_LargeContext(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
	}{
		{"entire codebase", "Analyze the entire codebase for bugs"},
		{"multiple files", "Review all files in this directory"},
		{"large file", "Parse this large file and extract data"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis := router.AnalyzeTask(tt.prompt)
			if !analysis.Features.NeedsLargeContext {
				t.Errorf("Should detect need for large context")
			}
		})
	}
}

func TestComplexityString(t *testing.T) {
	tests := []struct {
		complexity router.TaskComplexity
		expected   string
	}{
		{router.ComplexitySimple, "simple"},
		{router.ComplexityModerate, "moderate"},
		{router.ComplexityComplex, "complex"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.complexity.String()
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestAnalyzeTask_Reasoning(t *testing.T) {
	analysis := router.AnalyzeTask("What is Go?")
	if analysis.Reasoning == "" {
		t.Errorf("Analysis should include reasoning")
	}

	analysis = router.AnalyzeTask("Write file test.txt")
	if !strings.Contains(analysis.Reasoning, "tool") {
		t.Errorf("Tool-based task should mention tools in reasoning, got: %s", analysis.Reasoning)
	}
}
