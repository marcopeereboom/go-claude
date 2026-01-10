package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/marcopeereboom/go-claude/pkg/storage"
)

// ReplayResponse re-executes tools from a saved response without calling API
func ReplayResponse(claudeDir string, opts *Options) error {
	var timestamp string

	if opts.Replay == "" {
		pairs, err := storage.ListRequestResponsePairs(claudeDir)
		if err != nil {
			return err
		}
		if len(pairs) == 0 {
			return fmt.Errorf("no responses to replay")
		}
		timestamp = pairs[len(pairs)-1]
	} else {
		timestamp = opts.Replay
	}

	respPath := filepath.Join(claudeDir, fmt.Sprintf("response_%s.json", timestamp))
	respBody, err := os.ReadFile(respPath)
	if err != nil {
		return fmt.Errorf("loading response %s: %w", timestamp, err)
	}

	var responses []storage.APIResponse
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

	if opts.IsVerbose() {
		fmt.Fprintf(os.Stderr, "Replaying response: %s\n", timestamp)
	}

	toolCount := 0
	for respIdx, apiResp := range responses {
		for _, block := range apiResp.Content {
			if block.Type != "tool_use" {
				continue
			}
			toolCount++
			if opts.IsVerbose() {
				fmt.Fprintf(os.Stderr, "Iteration %d: %s\n", respIdx, block.Name)
			}
			if _, err := ExecuteTool(block, workingDir, claudeDir, opts, timestamp); err != nil {
				return fmt.Errorf("tool %s failed: %w", block.Name, err)
			}
		}
	}

	if opts.IsVerbose() {
		fmt.Fprintf(os.Stderr, "Replayed %d tools\n", toolCount)
	}
	return nil
}
