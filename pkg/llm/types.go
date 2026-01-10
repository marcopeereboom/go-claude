// Package llm provides interfaces and types for interacting with different LLM backends.
package llm

import "context"

// ModelInfo contains metadata about an available model.
type ModelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Provider    string `json:"provider"` // "claude" or "ollama"
}

// ModelCapabilities describes what features a model supports.
type ModelCapabilities struct {
	SupportsTools       bool     // Can the model use function calling/tools?
	SupportsVision      bool     // Can the model process images?
	SupportsStreaming   bool     // Can the model stream responses?
	MaxContextTokens    int      // Maximum context window size
	Provider            string   // "claude" or "ollama"
	RecommendedForTasks []string // e.g., ["code", "chat", "reasoning"]
}

// LLM is the interface that all LLM backends must implement.
type LLM interface {
	// Generate sends a request to the LLM and returns the response.
	Generate(ctx context.Context, req *Request) (*Response, error)

	// ListModels returns all available models for this provider.
	ListModels(ctx context.Context) ([]ModelInfo, error)

	// GetCapabilities returns the capabilities of the current model.
	GetCapabilities() ModelCapabilities
}

// Request contains all parameters needed for an LLM API call.
type Request struct {
	Model     string           `json:"model"`
	Messages  []MessageContent `json:"messages"`
	Tools     []Tool           `json:"tools,omitempty"`
	MaxTokens int              `json:"max_tokens"`
	System    string           `json:"system,omitempty"`
}

// Response contains the LLM's response.
type Response struct {
	Content    []ContentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      Usage          `json:"usage"`
}

// MessageContent represents a single message in the conversation.
type MessageContent struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// ContentBlock represents a piece of content (text, tool_use, or tool_result).
type ContentBlock struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	Content   string                 `json:"content,omitempty"`
}

// Tool represents a tool that can be called by the LLM.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"input_schema"`
}

// Usage contains token usage statistics.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
