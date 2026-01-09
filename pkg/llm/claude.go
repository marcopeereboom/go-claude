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
	apiVersion = "2023-06-01"
)

// ClaudeClient implements LLM interface for Anthropic's Claude API
type ClaudeClient struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewClaude creates a new Claude API client
func NewClaude(apiKey, baseURL string) *ClaudeClient {
	return &ClaudeClient{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

// claudeRequest is the API request format for Claude
type claudeRequest struct {
	Model     string      `json:"model"`
	MaxTokens int         `json:"max_tokens"`
	System    string      `json:"system,omitempty"`
	Messages  interface{} `json:"messages"`
	Tools     []Tool      `json:"tools,omitempty"`
}

// claudeResponse is the API response format from Claude
type claudeResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model"`
	StopReason string         `json:"stop_reason"`
	Usage      Usage          `json:"usage"`
	Error      *apiError      `json:"error,omitempty"`
}

type apiError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Generate sends a request to Claude API and returns the response
func (c *ClaudeClient) Generate(ctx context.Context, req *Request) (*Response, error) {
	// Build API request
	apiReq := claudeRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		System:    req.System,
		Messages:  req.Messages,
		Tools:     req.Tools,
	}

	reqBody, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// Set required headers
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", apiVersion)
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

	// Check HTTP status
	if err := checkHTTPStatus(resp.StatusCode, respBody); err != nil {
		return nil, err
	}

	var apiResp claudeResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w\nBody: %s", err, respBody)
	}

	// Check for API-level errors
	if apiResp.Error != nil {
		return nil, fmt.Errorf("API error [%s]: %s", apiResp.Error.Type, apiResp.Error.Message)
	}

	return &Response{
		Content:    apiResp.Content,
		StopReason: apiResp.StopReason,
		Usage:      apiResp.Usage,
	}, nil
}

func checkHTTPStatus(status int, body []byte) error {
	// Try to parse Claude API error first (has more detail)
	var apiErr struct {
		Error *apiError `json:"error"`
	}
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error != nil {
		return fmt.Errorf("API error [%s]: %s", apiErr.Error.Type, apiErr.Error.Message)
	}

	// Standard HTTP status codes
	switch status {
	case http.StatusOK:
		return nil
	case http.StatusBadRequest:
		return fmt.Errorf("%s: %s", http.StatusText(status), body)
	case http.StatusUnauthorized:
		return fmt.Errorf("%s: check API key", http.StatusText(status))
	case http.StatusForbidden:
		return fmt.Errorf("%s: %s", http.StatusText(status), body)
	case http.StatusNotFound:
		return fmt.Errorf("%s: invalid endpoint", http.StatusText(status))
	case http.StatusTooManyRequests:
		return fmt.Errorf("%s: %s", http.StatusText(status), body)
	case http.StatusInternalServerError:
		return fmt.Errorf("%s: %s", http.StatusText(status), body)
	case http.StatusServiceUnavailable:
		return fmt.Errorf("%s: %s", http.StatusText(status), body)
	case 529: // Anthropic-specific: overloaded
		return fmt.Errorf("service overloaded (529): %s", body)
	default:
		return fmt.Errorf("HTTP %d: %s", status, body)
	}
}
