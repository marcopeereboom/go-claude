package claude

import (
	"net/http"
	"strings"
	"time"

	"github.com/marcopeereboom/go-claude/pkg/llm"
	"github.com/marcopeereboom/go-claude/pkg/storage"
)

const (
	DefaultModel         = "claude-sonnet-4-20250514"
	APIVersion           = "2023-06-01"
	MaxContextTokens     = 100000
	DefaultMaxIterations = 15
	DefaultMaxCost       = 1.0 // dollars

	// Defaults
	DefaultMaxTokens = 8192
	DefaultTimeout   = 300

	// Verbosity levels
	VerbositySilent  = "silent"
	VerbosityNormal  = "normal"
	VerbosityVerbose = "verbose"
	VerbosityDebug   = "debug"
	DefaultVerbosity = VerbosityNormal

	// Tool permissions
	ToolNone    = "none"
	ToolRead    = "read"
	ToolWrite   = "write"
	ToolCommand = "command"
	ToolAll     = "all"
	DefaultTool = "" // dry-run

	// Output formats
	OutputText    = "text"
	OutputJSON    = "json"
	DefaultOutput = OutputText

	// bash_command timeout
	BashCommandTimeout = 30 * time.Second

	// Default Ollama URL
	DefaultOllamaURL = "http://localhost:11434"
)

// Type aliases for LLM interface types
type ContentBlock = llm.ContentBlock
type MessageContent = llm.MessageContent
type Tool = llm.Tool
type Usage = llm.Usage

// Type aliases for storage types
type Config = storage.Config
type ModelsCache = storage.ModelsCache
type AuditLogEntry = storage.AuditLogEntry
type Request = storage.Request

// API types (for backward compatibility with existing code)
type APIRequest struct {
	Model     string      `json:"model"`
	MaxTokens int         `json:"max_tokens"`
	System    string      `json:"system,omitempty"`
	Messages  interface{} `json:"messages"`
	Tools     []Tool      `json:"tools,omitempty"`
}

type APIResponse = storage.APIResponse

type APIError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Options for CLI
type Options struct {
	// Modes
	ModelsList    bool
	ModelsRefresh bool
	Reset         bool
	ShowStats     bool
	Replay        string
	PruneOld      int
	Estimate      bool
	Execute       bool
	MaxCostFlag   float64

	// Core
	MaxTokens     int
	MaxCost       float64
	MaxIterations int
	Model         string
	Timeout       int
	SystemPrompt  string
	Truncate      int
	ResumeDir     string
	OutputFile    string
	OllamaURL     string

	// Behavior
	Verbosity string
	Tool      string
	Output    string
}

// NewOptions creates a new Options with default values (for tests)
func NewOptions() *Options {
	return &Options{
		Model:         DefaultModel,
		MaxTokens:     DefaultMaxTokens,
		MaxCost:       DefaultMaxCost,
		MaxIterations: DefaultMaxIterations,
		Timeout:       DefaultTimeout,
		Truncate:      0,
		OllamaURL:     DefaultOllamaURL,
		Verbosity:     DefaultVerbosity,
		Tool:          DefaultTool,
		Output:        DefaultOutput,
		Replay:        "NOREPLAY",
	}
}

// SetTool sets the tool permission (for tests)
func (o *Options) SetTool(tool string) {
	o.Tool = tool
}

// SetVerbosity sets the verbosity level (for tests)
func (o *Options) SetVerbosity(verbosity string) {
	o.Verbosity = verbosity
}

// Helper methods for Options
func (o *Options) IsVerbose() bool {
	return o.Verbosity == VerbosityVerbose || o.Verbosity == VerbosityDebug
}

func (o *Options) IsDebug() bool {
	return o.Verbosity == VerbosityDebug
}

func (o *Options) IsSilent() bool {
	return o.Verbosity == VerbositySilent
}

func (o *Options) CanExecuteWrite() bool {
	if o.Tool == "" {
		return false // dry-run
	}
	return strings.Contains(o.Tool, ToolWrite) || o.Tool == ToolAll
}

func (o *Options) CanExecuteCommand() bool {
	if o.Tool == "" {
		return false // dry-run
	}
	return strings.Contains(o.Tool, ToolCommand) || o.Tool == ToolAll
}

func (o *Options) CanUseTools() bool {
	return o.Tool != ToolNone
}

func (o *Options) WantsJSON() bool {
	return o.Output == OutputJSON
}

// session holds all state needed for a conversation execution.
type session struct {
	opts       *Options
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
