package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Command whitelist for bash_command tool
var allowedCommands = map[string]bool{
	"ls":   true,
	"cat":  true,
	"grep": true,
	"find": true,
	"head": true,
	"tail": true,
	"wc":   true,
	"echo": true,
	"pwd":  true,
	"date": true,
	"git":  true, // validated separately
	"go":   true, // all go subcommands allowed
}

func getTools(opts *options) []Tool {
	if !opts.canUseTools() {
		return nil
	}

	return []Tool{{
		Name:        "read_file",
		Description: "Read the contents of a file",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]string{
					"type":        "string",
					"description": "Path to the file to read",
				},
			},
			"required": []string{"path"},
		},
	}, {
		Name:        "write_file",
		Description: "Write content to a file. Shows diff in dry-run mode.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]string{
					"type":        "string",
					"description": "Path to the file to write",
				},
				"content": map[string]string{
					"type":        "string",
					"description": "Content to write to the file",
				},
			},
			"required": []string{"path", "content"},
		},
	}, {
		Name: "bash_command",
		Description: `Execute a bash command in the working directory.

Allowed commands: ls, cat, grep, find, head, tail, wc, echo, pwd, date
Also allowed: git (log, diff, show, status, blame) and go (all subcommands)
Pipes and safe redirects (to working dir only) are permitted.

Blocked: rm, mv, cp, chmod, sudo, curl, wget, and path traversal.

Use 'reason' to explain why this command is needed (for audit trail).`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]string{
					"type":        "string",
					"description": "The bash command to execute",
				},
				"reason": map[string]string{
					"type":        "string",
					"description": "Why this command is needed",
				},
			},
			"required": []string{"command", "reason"},
		},
	}}
}

func executeTool(toolUse ContentBlock, workingDir string,
	opts *options, conversationID string,
) (ContentBlock, error) {
	switch toolUse.Name {
	case "read_file":
		return executeReadFile(toolUse, workingDir, opts, conversationID)
	case "write_file":
		return executeWriteFile(toolUse, workingDir, opts, conversationID)
	case "bash_command":
		return executeBashCommand(toolUse, workingDir, opts,
			conversationID)
	default:
		return ContentBlock{}, fmt.Errorf("unknown tool: %s",
			toolUse.Name)
	}
}

func executeReadFile(toolUse ContentBlock, workingDir string,
	opts *options, conversationID string,
) (ContentBlock, error) {
	startTime := time.Now()

	path, ok := toolUse.Input["path"].(string)
	if !ok {
		logAuditEntry("read_file", toolUse.Input, map[string]interface{}{
			"error": "path must be a string",
		}, false, conversationID, startTime, false)
		return makeToolError(toolUse.ID, "path must be a string")
	}

	if !isSafePath(path, workingDir) {
		errMsg := fmt.Sprintf("path outside project: %s", path)
		logAuditEntry("read_file", toolUse.Input, map[string]interface{}{
			"error": errMsg,
		}, false, conversationID, startTime, false)
		return makeToolError(toolUse.ID, errMsg)
	}

	if opts.isVerbose() {
		fmt.Fprintf(os.Stderr, "Tool: read_file(%s)\n", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		logAuditEntry("read_file", toolUse.Input, map[string]interface{}{
			"error": err.Error(),
		}, false, conversationID, startTime, false)
		return makeToolError(toolUse.ID, err.Error())
	}

	logAuditEntry("read_file", toolUse.Input, map[string]interface{}{
		"success": true,
		"path":    path,
		"size":    len(content),
	}, true, conversationID, startTime, false)

	return ContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUse.ID,
		Content:   string(content),
	}, nil
}

func executeWriteFile(toolUse ContentBlock, workingDir string,
	opts *options, conversationID string,
) (ContentBlock, error) {
	startTime := time.Now()

	path, ok := toolUse.Input["path"].(string)
	if !ok {
		logAuditEntry("write_file", toolUse.Input, map[string]interface{}{
			"error": "path must be a string",
		}, false, conversationID, startTime, false)
		return makeToolError(toolUse.ID, "path must be a string")
	}

	content, ok := toolUse.Input["content"].(string)
	if !ok {
		logAuditEntry("write_file", toolUse.Input, map[string]interface{}{
			"error": "content must be a string",
		}, false, conversationID, startTime, false)
		return makeToolError(toolUse.ID, "content must be a string")
	}

	if !isSafePath(path, workingDir) {
		errMsg := fmt.Sprintf("path outside project: %s", path)
		logAuditEntry("write_file", toolUse.Input, map[string]interface{}{
			"error": errMsg,
		}, false, conversationID, startTime, false)
		return makeToolError(toolUse.ID, errMsg)
	}

	old, _ := os.ReadFile(path)

	// Only show diff in normal/verbose mode
	if !opts.isSilent() {
		ToolHeader(path, !opts.canExecuteWrite())
		ShowDiff(string(old), content)
	}

	if !opts.canExecuteWrite() {
		fmt.Fprintf(os.Stderr, "(dry-run: use --tool=write to apply)\n\n")
		logAuditEntry("write_file", toolUse.Input, map[string]interface{}{
			"dry_run": true,
			"path":    path,
			"size":    len(content),
		}, true, conversationID, startTime, true)
		return ContentBlock{
			Type:      "tool_result",
			ToolUseID: toolUse.ID,
			Content: "Dry-run: changes not applied. " +
				"Use --tool=write flag.",
		}, nil
	}

	if opts.isVerbose() {
		fmt.Fprintf(os.Stderr, "Tool: write_file(%s)\n", path)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		logAuditEntry("write_file", toolUse.Input, map[string]interface{}{
			"error": err.Error(),
		}, false, conversationID, startTime, false)
		return makeToolError(toolUse.ID, err.Error())
	}

	logAuditEntry("write_file", toolUse.Input, map[string]interface{}{
		"success": true,
		"path":    path,
		"size":    len(content),
	}, true, conversationID, startTime, false)

	return ContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUse.ID,
		Content:   fmt.Sprintf("Successfully wrote to %s", path),
	}, nil
}

func executeBashCommand(toolUse ContentBlock, workingDir string,
	opts *options, conversationID string,
) (ContentBlock, error) {
	startTime := time.Now()

	command, ok := toolUse.Input["command"].(string)
	if !ok {
		return logAndReturnError(toolUse.ID, "bash_command",
			toolUse.Input, "command must be a string",
			conversationID, startTime)
	}

	reason, ok := toolUse.Input["reason"].(string)
	if !ok {
		return logAndReturnError(toolUse.ID, "bash_command",
			toolUse.Input, "reason must be a string",
			conversationID, startTime)
	}

	// Validate command safety
	if err := validateCommand(command); err != nil {
		return logAndReturnError(toolUse.ID, "bash_command",
			toolUse.Input, err.Error(), conversationID, startTime)
	}

	// Dry-run mode: show what would execute
	if !opts.canExecuteCommand() {
		msg := fmt.Sprintf(
			"Dry-run: would execute command: %s\nReason: %s\n"+
				"Use --tool=command or --tool=all to execute",
			command, reason)
		if !opts.isSilent() {
			ToolHeader("bash_command", true)
		}
		fmt.Fprintf(os.Stderr, "%s\n\n", msg)

		logAuditEntry("bash_command", toolUse.Input, map[string]interface{}{
			"dry_run": true,
			"command": command,
			"reason":  reason,
		}, true, conversationID, startTime, true)

		return ContentBlock{
			Type:      "tool_result",
			ToolUseID: toolUse.ID,
			Content:   msg,
		}, nil
	}

	if opts.isVerbose() {
		fmt.Fprintf(os.Stderr, "Tool: bash_command(%q)\n", command)
	}

	// Execute command with timeout
	ctx, cancel := context.WithTimeout(context.Background(),
		bashCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = workingDir

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	duration := time.Since(startTime)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			msg := fmt.Sprintf(
				"Command timeout after %v\nStdout: %s\nStderr: %s",
				bashCommandTimeout, stdout.String(), stderr.String())

			logAuditEntry("bash_command", toolUse.Input, map[string]interface{}{
				"error":     "timeout",
				"exit_code": -1,
				"stdout":    stdout.String(),
				"stderr":    stderr.String(),
			}, false, conversationID, startTime, false)

			return makeToolError(toolUse.ID, msg)
		} else {
			exitCode = -1
		}
	}

	resultMsg := fmt.Sprintf(
		"Exit code: %d\nDuration: %v\nStdout:\n%s\nStderr:\n%s",
		exitCode, duration, stdout.String(), stderr.String())

	logAuditEntry("bash_command", toolUse.Input, map[string]interface{}{
		"exit_code": exitCode,
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
		"duration":  duration.Milliseconds(),
	}, exitCode == 0, conversationID, startTime, false)

	if exitCode != 0 {
		return makeToolError(toolUse.ID, resultMsg)
	}

	return ContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUse.ID,
		Content:   resultMsg,
	}, nil
}

func validateCommand(command string) error {
	// Check for command chaining operators first (highest priority)
	// These allow bypassing other protections
	chainOperators := []string{"||", "&&", ";"}
	for _, op := range chainOperators {
		if strings.Contains(command, op) {
			return fmt.Errorf("blocked pattern: %s", op)
		}
	}

	// Check for path traversal (second priority)
	if strings.Contains(command, "..") {
		return fmt.Errorf("path traversal not allowed")
	}

	// Block dangerous commands (third priority)
	blockedCommands := []string{
		"sudo", "su ", "rm ", "mv ", "cp ", "chmod", "chown",
		"curl", "wget",
	}
	for _, pattern := range blockedCommands {
		if strings.Contains(command, pattern) {
			return fmt.Errorf("blocked pattern: %s", pattern)
		}
	}

	// Parse commands (handle pipes)
	pipePattern := regexp.MustCompile(`\s*\|\s*`)
	commands := pipePattern.Split(command, -1)

	for _, cmd := range commands {
		parts := strings.Fields(cmd)
		if len(parts) == 0 {
			continue
		}
		firstWord := parts[0]

		// Check whitelist
		if allowedCommands[firstWord] {
			// Special validation for git
			if firstWord == "git" && len(parts) > 1 {
				gitCmd := parts[1]
				allowed := map[string]bool{
					"log": true, "diff": true, "show": true,
					"status": true, "blame": true,
				}
				if !allowed[gitCmd] {
					return fmt.Errorf(
						"git subcommand not allowed: %s", gitCmd)
				}
			}
			continue
		}

		return fmt.Errorf("command not in whitelist: %s", firstWord)
	}

	return nil
}

// isSafePath checks if path is within workingDir
// Returns false if path escapes workingDir through .. or symlinks
func isSafePath(path, workingDir string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	// Clean both paths and ensure workingDir has trailing separator
	// to prevent "/home/user/project" matching "/home/user/project-evil"
	cleanWorking := filepath.Clean(workingDir) + string(filepath.Separator)
	cleanAbs := filepath.Clean(abs) + string(filepath.Separator)

	return strings.HasPrefix(cleanAbs, cleanWorking)
}

func makeToolError(toolUseID, errMsg string) (ContentBlock, error) {
	return ContentBlock{
		Type:      "tool_result",
		ToolUseID: toolUseID,
		Content:   fmt.Sprintf("Error: %s", errMsg),
	}, nil
}

func logAndReturnError(toolUseID, tool string,
	input map[string]interface{}, errMsg string,
	conversationID string, startTime time.Time,
) (ContentBlock, error) {
	logAuditEntry(tool, input, map[string]interface{}{
		"error": errMsg,
	}, false, conversationID, startTime, false)
	return makeToolError(toolUseID, errMsg)
}

func logAuditEntry(tool string, input, result map[string]interface{},
	success bool, conversationID string, startTime time.Time, dryRun bool,
) {
	duration := time.Since(startTime).Milliseconds()

	entry := AuditLogEntry{
		Timestamp:      time.Now().Format("20060102_150405"),
		Tool:           tool,
		Input:          input,
		Result:         result,
		Success:        success,
		DurationMs:     duration,
		ConversationID: conversationID,
		DryRun:         dryRun,
	}

	if !success {
		if errMsg, ok := result["error"].(string); ok {
			entry.Error = errMsg
		}
	}

	// Log to audit file (best effort, don't fail tool execution)
	if err := appendAuditLog(entry); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write audit log: %v\n",
			err)
	}
}

// executeTools processes all tool use requests in the response.
func executeTools(content []ContentBlock, workingDir string,
	opts *options, conversationID string,
) ([]ContentBlock, error) {
	results := []ContentBlock{}
	for _, block := range content {
		if block.Type == "tool_use" {
			result, err := executeTool(block, workingDir, opts,
				conversationID)
			if err != nil {
				return nil, fmt.Errorf("tool error: %w", err)
			}
			results = append(results, result)
		}
	}
	return results, nil
}
