package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	claudeAPIVersion = "2023-06-01"
)

// ClaudeClient implements the LLM interface for Claude API.
type ClaudeClient struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewClaude creates a new Claude client.
func NewClaude(apiKey, baseURL string) *ClaudeClient {
	return &ClaudeClient{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

// Generate sends a request to Claude API.
func (c *ClaudeClient) Generate(ctx context.Context, req *Request) (*Response, error) {
	// Convert to Claude API format
	apiReq := map[string]interface{}{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"messages":   req.Messages,
	}
	if req.System != "" {
		apiReq["system"] = req.System
	}
	if len(req.Tools) > 0 {
		apiReq["tools"] = req.Tools
	}

	reqBody, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", claudeAPIVersion)
	httpReq.Header.Set("content-type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("making API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, parseClaudeError(resp.StatusCode, respBody)
	}

	var apiResp struct {
		Content    []ContentBlock `json:"content"`
		StopReason string         `json:"stop_reason"`
		Usage      Usage          `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &Response{
		Content:    apiResp.Content,
		StopReason: apiResp.StopReason,
		Usage:      apiResp.Usage,
	}, nil
}

// ListModels returns available Claude models.
func (c *ClaudeClient) ListModels(ctx context.Context) ([]ModelInfo, error) {
	// Claude doesn't have a public models API endpoint yet
	// Return hardcoded list of known models
	return []ModelInfo{
		{
			ID:          "claude-opus-4-20250514",
			Name:        "claude-opus-4-20250514",
			Description: "Claude Opus 4",
			Provider:    "claude",
		},
		{
			ID:          "claude-sonnet-4-20250514",
			Name:        "claude-sonnet-4-20250514",
			Description: "Claude Sonnet 4",
			Provider:    "claude",
		},
		{
			ID:          "claude-sonnet-4-5-20250929",
			Name:        "claude-sonnet-4-5-20250929",
			Description: "Claude Sonnet 4.5",
			Provider:    "claude",
		},
		{
			ID:          "claude-haiku-4-5-20251001",
			Name:        "claude-haiku-4-5-20251001",
			Description: "Claude Haiku 4.5",
			Provider:    "claude",
		},
		{
			ID:          "claude-3-5-sonnet-20241022",
			Name:        "claude-3-5-sonnet-20241022",
			Description: "Claude 3.5 Sonnet",
			Provider:    "claude",
		},
	}, nil
}

func parseClaudeError(statusCode int, body []byte) error {
	var apiErr struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error.Type != "" {
		return fmt.Errorf("API error [%s]: %s", apiErr.Error.Type, apiErr.Error.Message)
	}
	return fmt.Errorf("API error %d: %s", statusCode, string(body))
}
