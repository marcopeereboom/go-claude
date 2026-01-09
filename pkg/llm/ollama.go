package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OllamaClient implements the LLM interface for Ollama.
type OllamaClient struct {
	model   string
	baseURL string
	client  *http.Client
}

// NewOllama creates a new Ollama client.
func NewOllama(model, baseURL string) *OllamaClient {
	return &OllamaClient{
		model:   model,
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

// Generate sends a request to Ollama API.
func (o *OllamaClient) Generate(ctx context.Context, req *Request) (*Response, error) {
	// Convert messages to Ollama format
	var messages []map[string]interface{}
	for _, msg := range req.Messages {
		content := ""
		for _, block := range msg.Content {
			if block.Type == "text" {
				content += block.Text
			}
		}
		messages = append(messages, map[string]interface{}{
			"role":    msg.Role,
			"content": content,
		})
	}

	// Build Ollama request
	apiReq := map[string]interface{}{
		"model":    o.model,
		"messages": messages,
		"stream":   false,
	}
	if req.System != "" {
		apiReq["system"] = req.System
	}
	if len(req.Tools) > 0 {
		apiReq["tools"] = convertToolsToOllama(req.Tools)
	}

	reqBody, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	endpoint := strings.TrimRight(o.baseURL, "/") + "/api/chat"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("content-type", "application/json")

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("making API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Function struct {
					Name      string                 `json:"name"`
					Arguments map[string]interface{} `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls,omitempty"`
		} `json:"message"`
		Done bool `json:"done"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	// Convert Ollama response to unified format
	var content []ContentBlock
	stopReason := "end_turn"

	if len(apiResp.Message.ToolCalls) > 0 {
		// Tool use response
		for _, tc := range apiResp.Message.ToolCalls {
			content = append(content, ContentBlock{
				Type:  "tool_use",
				ID:    fmt.Sprintf("call_%s", tc.Function.Name),
				Name:  tc.Function.Name,
				Input: tc.Function.Arguments,
			})
		}
		stopReason = "tool_use"
	} else {
		// Text response
		content = []ContentBlock{{
			Type: "text",
			Text: apiResp.Message.Content,
		}}
	}

	return &Response{
		Content:    content,
		StopReason: stopReason,
		Usage: Usage{
			InputTokens:  0, // Ollama doesn't provide token counts
			OutputTokens: 0,
		},
	}, nil
}

// ListModels returns available Ollama models.
func (o *OllamaClient) ListModels(ctx context.Context) ([]ModelInfo, error) {
	endpoint := strings.TrimRight(o.baseURL, "/") + "/api/tags"
	httpReq, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("making API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		Models []struct {
			Name       string `json:"name"`
			Model      string `json:"model"`
			ModifiedAt string `json:"modified_at"`
			Size       int64  `json:"size"`
		} `json:"models"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	var models []ModelInfo
	for _, m := range apiResp.Models {
		models = append(models, ModelInfo{
			ID:       m.Name,
			Name:     m.Name,
			Provider: "ollama",
		})
	}

	return models, nil
}

func convertToolsToOllama(tools []Tool) []map[string]interface{} {
	var ollamaTools []map[string]interface{}
	for _, tool := range tools {
		ollamaTools = append(ollamaTools, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.InputSchema,
			},
		})
	}
	return ollamaTools
}
