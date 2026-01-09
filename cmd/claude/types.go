package main

import (
	"net/http"
	"strings"
	"time"

	"github.com/marcopeereboom/go-claude/pkg/llm"
)

const (
	defaultModel         = "claude-sonnet-4-20250514"
	apiVersion           = "2023-06-01"
	maxContextTokens     = 100000
	defaultMaxIterations = 15
	defaultMaxCost       = 1.0 // dollars

	// Defaults
	defaultMaxTokens = 8192
	defaultTimeout   = 300

	// Verbosity levels
	verbositySilent  = "silent"
	verbosityNormal  = "normal"
	verbosityVerbose = "verbose"
	verbosityDebug   = "debug"
	defaultVerbosity = verbosityNormal

	// Tool permissions
	toolNone    = "none"
	toolRead    = "read"
	toolWrite   = "write"
	toolCommand = "command"
	toolAll     = "all"
	defaultTool = "" // dry-run

	// Output formats
	outputText    = "text"
	outputJSON    = "json"
	defaultOutput = outputText

	// bash_command timeout
	bashCommandTimeout = 30 * time.Second

	// Default Ollama URL
	defaultOllamaURL = "http://localhost:11434"
)

// Type aliases for LLM interface types
type ContentBlock = llm.ContentBlock
type MessageContent = llm.MessageContent
type Tool = llm.Tool
type Usage = llm.Usage

// API types (for backward compatibility with existing code)
type APIRequest struct {
	Model     string      `json:"model"`
	MaxTokens int         `json:"max_tokens"`
	System    string      `json:"system,omitempty"`
	Messages  interface{} `json:"messages"`
	Tools     []Tool      `json:"tools,omitempty"`
}

type APIResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model"`
	StopReason string         `json:"stop_reason"`
	Usage      Usage          `json:"usage"`
	Error      *APIError      `json:"error,omitempty"`
}

type APIError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// CLI options
type options struct {
	// Modes
	modelsList    bool
	modelsRefresh bool
	reset         bool
	showStats     bool
	replay        string
	pruneOld      int
	estimate      bool
	execute       bool
	maxCostFlag   float64

	// Core
	maxTokens     int
	maxCost       float64
	maxIterations int
	model         string
	timeout       int
	systemPrompt  string
	truncate      int
	resumeDir     string
	outputFile    string
	ollamaURL     string

	// Behavior
	verbosity string
	tool      string
	output    string
}

// Helper methods for options
func (o *options) isVerbose() bool {
	return o.verbosity == verbosityVerbose || o.verbosity == verbosityDebug
}

func (o *options) isDebug() bool {
	return o.verbosity == verbosityDebug
}

func (o *options) isSilent() bool {
	return o.verbosity == verbositySilent
}

func (o *options) canExecuteWrite() bool {
	if o.tool == "" {
		return false // dry-run
	}
	return strings.Contains(o.tool, toolWrite) || o.tool == toolAll
}

func (o *options) canExecuteCommand() bool {
	if o.tool == "" {
		return false // dry-run
	}
	return strings.Contains(o.tool, toolCommand) || o.tool == toolAll
}

func (o *options) canUseTools() bool {
	return o.tool != toolNone
}

func (o *options) wantsJSON() bool {
	return o.output == outputJSON
}

// session holds all state needed for a conversation execution.
type session struct {
	opts       *options
	claudeDir  string
	apiKey     string
	config     *Config
	model      string
	sysPrompt  string
	timestamp  string
	workingDir string
	client     *http.Client
	llmClient  llm.LLM
}

// conversationResult holds the outcome of a conversation execution.
type conversationResult struct {
	assistantText string
	respBody      []byte
}
