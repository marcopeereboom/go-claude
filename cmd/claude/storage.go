package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/marcopeereboom/go-claude/pkg/storage"
)

// Direct pkg/storage imports
var (
	saveRequest              = storage.SaveRequest
	saveResponse             = storage.SaveResponse
	loadConversationHistory  = storage.LoadConversationHistory
	listRequestResponsePairs = storage.ListRequestResponsePairs
	loadOrCreateConfig       = storage.LoadOrCreateConfig
	loadModelsCache          = storage.LoadModelsCache
	saveModelsCache          = storage.SaveModelsCache
	pruneResponses           = storage.PruneResponses
)

// appendAuditLog wraps storage.AppendAuditLog with claudeDir lookup
func appendAuditLog(entry AuditLogEntry) error {
	claudeDir, err := getClaudeDir("")
	if err != nil {
		return err
	}
	return storage.AppendAuditLog(claudeDir, entry)
}

// replayResponse re-executes tools from a saved response without calling API
func replayResponse(claudeDir string, opts *options) error {
	var timestamp string

	if opts.replay == "" {
		pairs, err := listRequestResponsePairs(claudeDir)
		if err != nil {
			return err
		}
		if len(pairs) == 0 {
			return fmt.Errorf("no responses to replay")
		}
		timestamp = pairs[len(pairs)-1]
	} else {
		timestamp = opts.replay
	}

	respPath := filepath.Join(claudeDir, fmt.Sprintf("response_%s.json", timestamp))
	respBody, err := os.ReadFile(respPath)
	if err != nil {
		return fmt.Errorf("loading response %s: %w", timestamp, err)
	}

	var responses []APIResponse
	if err := json.Unmarshal(respBody, &responses); err != nil {
		return fmt.Errorf("parsing responses: %w", err)
	}

	if len(responses) == 0 {
		return fmt.Errorf("no responses in file")
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working dir: %w", err)
	}

	if opts.isVerbose() {
		fmt.Fprintf(os.Stderr, "Replaying response: %s\n", timestamp)
	}

	toolCount := 0
	for respIdx, apiResp := range responses {
		for _, block := range apiResp.Content {
			if block.Type != "tool_use" {
				continue
			}
			toolCount++
			if opts.isVerbose() {
				fmt.Fprintf(os.Stderr, "Iteration %d: %s\n", respIdx, block.Name)
			}
			if _, err := executeTool(block, workingDir, opts, timestamp); err != nil {
				return fmt.Errorf("tool %s failed: %w", block.Name, err)
			}
		}
	}

	if opts.isVerbose() {
		fmt.Fprintf(os.Stderr, "Replayed %d tools\n", toolCount)
	}
	return nil
}
