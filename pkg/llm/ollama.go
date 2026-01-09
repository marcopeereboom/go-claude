package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OllamaClient implements the LLM interface for Ollama.
type OllamaClient struct {
	model   string
	baseURL string
	client  *http.Client
}

// NewOllama creates a new Ollama client.
func NewOllama(model, baseURL string) *OllamaClient {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &OllamaClient{
		model:   model,
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

// ollamaMessage represents Ollama's message format
type ollamaMessage struct {
	Role    string                   `json:"role"`
	Content string                   `json:"content"`
	Tools   []map[string]interface{} `json:"tool_calls,omitempty"`
}

// ollamaRequest represents the Ollama API request
type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Tools    []Tool          `json:"tools,omitempty"`
	Stream   bool            `json:"stream"`
}

// ollamaResponse represents the Ollama API response
type ollamaResponse struct {
	Model     string        `json:"model"`
	CreatedAt string        `json:"created_at"`
	Message   ollamaMessage `json:"message"`
	Done      bool          `json:"done"`
}

// Generate implements the LLM interface for Ollama.
func (o *OllamaClient) Generate(ctx context.Context, req *Request) (*Response, error) {
	// Convert our messages to Ollama format
	ollamaMessages := make([]ollamaMessage, 0, len(req.Messages))
	
	// Add system message if provided
	if req.System != "" {
		ollamaMessages = append(ollamaMessages, ollamaMessage{
			Role:    "system",
			Content: req.System,
		})
	}
	
	// Convert conversation history
	for _, msg := range req.Messages {
		ollamaMsg := o.convertToOllamaMessage(msg)
		ollamaMessages = append(ollamaMessages, ollamaMsg)
	}

	// Build Ollama request
	ollamaReq := ollamaRequest{
		Model:    o.model,
		Messages: ollamaMessages,
		Tools:    req.Tools,
		Stream:   false,
	}

	reqBody, err := json.Marshal(ollamaReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	// Make API call
	url := fmt.Sprintf("%s/api/chat", o.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("making API call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, body)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var ollamaResp ollamaResponse
	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	// Convert Ollama response to our format
	return o.convertFromOllamaResponse(&ollamaResp)
}

// convertToOllamaMessage converts our MessageContent to Ollama's format
func (o *OllamaClient) convertToOllamaMessage(msg MessageContent) ollamaMessage {
	ollamaMsg := ollamaMessage{
		Role: msg.Role,
	}

	// Combine all text blocks into content
	var textParts []string
	var toolCalls []map[string]interface{}

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			// Ollama format for tool calls
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   block.ID,
				"type": "function",
				"function": map[string]interface{}{
					"name":      block.Name,
					"arguments": block.Input,
				},
			})
		case "tool_result":
			// Tool results go in content as JSON
			resultJSON, _ := json.Marshal(map[string]interface{}{
				"tool_use_id": block.ToolUseID,
				"content":     block.Content,
			})
			textParts = append(textParts, string(resultJSON))
		}
	}

	if len(textParts) > 0 {
		ollamaMsg.Content = textParts[0] // For now, just use first text block
	}
	if len(toolCalls) > 0 {
		ollamaMsg.Tools = toolCalls
	}

	return ollamaMsg
}

// convertFromOllamaResponse converts Ollama's response to our format
func (o *OllamaClient) convertFromOllamaResponse(resp *ollamaResponse) (*Response, error) {
	result := &Response{
		Content:    []ContentBlock{},
		StopReason: "end_turn",
		Usage: Usage{
			InputTokens:  0, // Ollama doesn't provide token counts
			OutputTokens: 0,
		},
	}

	// Add text content if present
	if resp.Message.Content != "" {
		result.Content = append(result.Content, ContentBlock{
			Type: "text",
			Text: resp.Message.Content,
		})
	}

	// Convert tool calls if present
	if len(resp.Message.Tools) > 0 {
		result.StopReason = "tool_use"
		for _, toolCall := range resp.Message.Tools {
			// Extract function call details
			funcData, ok := toolCall["function"].(map[string]interface{})
			if !ok {
				continue
			}

			name, _ := funcData["name"].(string)
			args, _ := funcData["arguments"].(map[string]interface{})
			id, _ := toolCall["id"].(string)

			result.Content = append(result.Content, ContentBlock{
				Type:  "tool_use",
				ID:    id,
				Name:  name,
				Input: args,
			})
		}
	}

	return result, nil
}
