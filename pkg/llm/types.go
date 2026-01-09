// Package llm provides interfaces and types for interacting with different LLM backends.
// Reuse MessageContent, ContentBlock, Tool, Usage from cmd/claude/claude.go for now.
// Will consolidate later.
package llm

import "context"

// LLM is the interface that all LLM backends must implement.
type LLM interface {
	// Generate sends a request to the LLM and returns the response.
	Generate(ctx context.Context, req *Request) (*Response, error)
}

// Request contains all parameters needed for an LLM API call.
type Request struct {
	Model     string           // Model name (e.g., "claude-sonnet-4-5-20250929")
	Messages  []MessageContent // Conversation history
	Tools     []Tool           // Available tools
	MaxTokens int              // Maximum tokens to generate
	System    string           // System prompt
}

// Response contains the LLM's response.
type Response struct {
	Content    []ContentBlock // Response content (text and/or tool_use blocks)
	StopReason string         // Why generation stopped (end_turn, tool_use, etc)
	Usage      Usage          // Token usage statistics
}

// MessageContent represents a single message in the conversation.
// Defined in cmd/claude/claude.go - imported here for reference.
type MessageContent struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// ContentBlock represents a piece of content (text, tool_use, or tool_result).
// Defined in cmd/claude/claude.go - imported here for reference.
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
// Defined in cmd/claude/claude.go - imported here for reference.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"input_schema"`
}

// Usage contains token usage statistics.
// Defined in cmd/claude/claude.go - imported here for reference.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
