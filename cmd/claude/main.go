// Package main implements a CLI for interacting with Claude AI with tool use,
// conversation management, and local context storage.
package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed defaultprompt.txt
var defaultSystemPrompt string

// apiURL can be overridden in tests
var apiURL = "https://api.anthropic.com/v1/messages"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	opts := parseFlags()

	claudeDir, err := getClaudeDir(opts.resumeDir)
	if err != nil {
		return err
	}

	// Handle models commands first (don't need stdin)
	if opts.modelsList {
		return listModelsCommand(claudeDir, opts.ollamaURL)
	}

	if opts.modelsRefresh {
		return refreshModelsCommand(claudeDir, opts.ollamaURL)
	}

	// Handle --execute mode (use last message from conversation)
	if opts.execute {
		messages, err := loadConversationHistory(claudeDir)
		if err != nil {
			return fmt.Errorf("loading conversation: %w", err)
		}

		var userMsg string

		// Try to get last user message from completed conversation
		if len(messages) > 0 {
			userMsg, err = getLastUserMessage(messages)
			if err != nil {
				return fmt.Errorf("no user message in conversation")
			}
		} else {
			// No complete pairs - check for unpaired request (from --estimate)
			entries, err := os.ReadDir(claudeDir)
			if err != nil {
				return fmt.Errorf("no conversation history")
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "request_") {
					reqPath := filepath.Join(claudeDir, entry.Name())
					var req Request
					data, _ := os.ReadFile(reqPath)
					json.Unmarshal(data, &req)
					userMsg, _ = getLastUserMessage(req.Messages)
					break
				}
			}
			if userMsg == "" {
				return fmt.Errorf("no message to execute")
			}
		}

		// Override max-cost if provided
		if opts.maxCostFlag > 0 {
			opts.maxCost = opts.maxCostFlag
		}

		// Normal execution flow with last user message
		return executeWithSavedInput(userMsg, opts, claudeDir)
	}

	// Handle --estimate mode
	if opts.estimate {
		// Must have stdin
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) != 0 {
			return fmt.Errorf("no input (pipe required)")
		}

		userMsg, err := readInput()
		if err != nil {
			return err
		}

		// Load conversation history
		messages, _ := loadConversationHistory(claudeDir)

		// Get model for pricing
		configPath := filepath.Join(claudeDir, "config.json")
		cfg := loadOrCreateConfig(configPath)
		model := selectModel(opts.model, cfg.Model)

		// Estimate and display
		estimate := estimateCost(userMsg, messages, model)
		displayEstimate(estimate)

		// Save this message to conversation so --execute can use it
		timestamp := time.Now().Format("20060102_150405")
		messages = append(messages, MessageContent{
			Role: "user",
			Content: []ContentBlock{{
				Type: "text",
				Text: userMsg,
			}},
		})
		if err := saveRequest(claudeDir, timestamp, messages); err != nil {
			return fmt.Errorf("saving request: %w", err)
		}

		// Update config
		cfg.Model = model
		cfg.LastRun = timestamp
		if cfg.FirstRun == "" {
			cfg.FirstRun = timestamp
		}
		configPath = filepath.Join(claudeDir, "config.json")
		if err := saveJSON(configPath, cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		return nil
	}

	// Handle special modes that don't need full setup
	if opts.showStats {
		return showStats(claudeDir)
	}

	if opts.reset {
		return resetConversation(claudeDir, opts.isVerbose())
	}

	if opts.replay != "NOREPLAY" {
		return replayResponse(claudeDir, opts)
	}

	if opts.pruneOld > 0 {
		return pruneResponses(claudeDir, opts.pruneOld, opts.isVerbose())
	}

	// Check if stdin is a pipe/redirect, not interactive terminal
	stat, err := os.Stdin.Stat()
	if err != nil {
		return fmt.Errorf("checking stdin: %w", err)
	}
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		// Interactive terminal - no input piped
		flag.Usage()
		return fmt.Errorf("no input provided (pipe or redirect required)")
	}

	// Normal execution
	userMsg, err := readInput()
	if err != nil {
		return err
	}
	return executeWithSavedInput(userMsg, opts, claudeDir)
}

func executeWithSavedInput(userMsg string, opts *options, claudeDir string) error {
	// Initialize session
	sess, err := initSession(opts, claudeDir)
	if err != nil {
		return err
	}

	// Execute conversation with tool support
	result, err := executeConversation(sess, userMsg)
	if err != nil {
		return err
	}

	// Save and output results
	return finalizeSession(sess, result)
}

func parseFlags() *options {
	opts := &options{}

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: claude [options]\n\n")
		fmt.Fprintf(os.Stderr, "A CLI for interacting with Claude AI with tool support.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  # Dry-run (shows what would happen)\n")
		fmt.Fprintf(os.Stderr, "  echo \"add error handling to users.go\" | claude\n\n")
		fmt.Fprintf(os.Stderr, "  # Execute with write permission\n")
		fmt.Fprintf(os.Stderr, "  echo \"add tests\" | claude --tool=write\n\n")
		fmt.Fprintf(os.Stderr, "  # Replay last run and execute everything\n")
		fmt.Fprintf(os.Stderr, "  claude --replay --tool=all\n")
		fmt.Fprintf(os.Stderr, "  claude --replay=20260104_153022 --tool=all\n\n")
		fmt.Fprintf(os.Stderr, "  # Show statistics\n")
		fmt.Fprintf(os.Stderr, "  claude --stats\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	// Modes
	flag.BoolVar(&opts.modelsList, "models-list", false,
		"list available Claude and Ollama models (creates cache if missing)")
	flag.BoolVar(&opts.modelsRefresh, "models-refresh", false,
		"refresh models cache from Claude API and Ollama")
	flag.BoolVar(&opts.reset, "reset", false,
		"reset conversation (delete .claude/ directory)")
	flag.BoolVar(&opts.showStats, "stats", false,
		"show conversation statistics")

	flag.StringVar(&opts.replay, "replay", "NOREPLAY",
		"replay response (empty=latest, or timestamp like 20260104_153022)")
	flag.IntVar(&opts.pruneOld, "prune-old", 0,
		"keep only last N request/response pairs, delete older")

	// Cost estimation
	flag.BoolVar(&opts.estimate, "estimate", false,
		"estimate cost without executing (shows cost for piped input)")
	flag.BoolVar(&opts.execute, "execute", false,
		"re-execute last user message from conversation")
	flag.Float64Var(&opts.maxCostFlag, "max-cost-override", 0,
		"override max-cost for this run (use with --execute)")

	// Core settings
	flag.StringVar(&opts.model, "model", "",
		fmt.Sprintf("model to use (default: %s)", defaultModel))
	flag.IntVar(&opts.maxTokens, "max-tokens", defaultMaxTokens,
		"maximum tokens per API call")
	flag.Float64Var(&opts.maxCost, "max-cost", defaultMaxCost,
		"maximum cost in dollars per conversation (0 = unlimited)")
	flag.IntVar(&opts.maxIterations, "max-iterations", defaultMaxIterations,
		"maximum tool loop iterations (0 = unlimited)")
	flag.IntVar(&opts.timeout, "timeout", defaultTimeout,
		"HTTP timeout in seconds")
	flag.IntVar(&opts.truncate, "truncate", 0,
		"keep only last N messages in conversation (0 = keep all)")
	flag.StringVar(&opts.ollamaURL, "ollama-url", defaultOllamaURL,
		"Ollama API URL")

	// Behavior
	flag.StringVar(&opts.verbosity, "verbosity", defaultVerbosity,
		"output verbosity: silent, normal, verbose, debug")
	flag.StringVar(&opts.tool, "tool", defaultTool,
		"tool permissions: \"\" (dry-run), none, read, write, command, all, or comma-separated")
	flag.StringVar(&opts.output, "output", defaultOutput,
		"output format: text, json")

	// Advanced
	flag.StringVar(&opts.systemPrompt, "system", "",
		"custom system prompt")
	flag.StringVar(&opts.resumeDir, "resume-dir", "",
		"directory for conversation state (default: current directory)")
	flag.StringVar(&opts.outputFile, "output-file", "",
		"write output to file instead of stdout")

	flag.Parse()

	return opts
}

func showStats(claudeDir string) error {
	cfg := loadOrCreateConfig(filepath.Join(claudeDir, "config.json"))

	pairs, err := listRequestResponsePairs(claudeDir)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Project: %s\n", claudeDir)
	fmt.Fprintf(os.Stderr, "Model: %s\n", cfg.Model)
	fmt.Fprintf(os.Stderr, "Total tokens: %d in, %d out\n",
		cfg.TotalInput, cfg.TotalOutput)
	fmt.Fprintf(os.Stderr, "Approximate cost: $%.4f\n",
		float64(cfg.TotalInput)*3.0/1000000+
			float64(cfg.TotalOutput)*15.0/1000000)
	fmt.Fprintf(os.Stderr, "Conversation turns: %d\n", len(pairs))
	fmt.Fprintf(os.Stderr, "First run: %s\n", cfg.FirstRun)
	fmt.Fprintf(os.Stderr, "Last run: %s\n", cfg.LastRun)

	return nil
}

func getClaudeDir(resumeDir string) (string, error) {
	dir := resumeDir
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getting cwd: %w", err)
		}
	}
	return filepath.Join(dir, ".claude"), nil
}

func readInput() (string, error) {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("reading stdin: %w", err)
	}

	msg := string(input)
	if msg == "" {
		return "", fmt.Errorf("no input provided")
	}

	return msg, nil
}

func writeOutput(outputFile string, jsonOutput bool,
	assistantText string, respBody []byte,
) error {
	var output string
	if jsonOutput {
		output = string(respBody)
	} else {
		output = assistantText
	}

	switch {
	case outputFile != "":
		// Never write escape codes to files
		err := os.WriteFile(outputFile, []byte(output), 0o644)
		if err != nil {
			return fmt.Errorf("writing output file: %w", err)
		}
	default:
		// FormatResponse handles TTY check and chroma highlighting
		if !jsonOutput && isTTY(os.Stdout) {
			FormatResponse(os.Stdout, output)
		} else {
			if strings.HasSuffix(output, "\n") {
				fmt.Print(output)
			} else {
				fmt.Println(output)
			}
		}

	}

	return nil
}

func resetConversation(claudeDir string, verbose bool) error {
	if err := os.RemoveAll(claudeDir); err != nil {
		return fmt.Errorf("removing %s: %w", claudeDir, err)
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "Reset: removed %s\n", claudeDir)
	}
	return nil
}

func saveJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "\t")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	return nil
}
