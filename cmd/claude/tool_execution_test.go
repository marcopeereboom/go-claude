package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestToolPermissions verifies the permission checking logic
func TestToolPermissions(t *testing.T) {
	tests := []struct {
		name           string
		toolFlag       string
		expectWrite    bool
		expectCommand  bool
		expectUseTools bool
	}{
		{
			name:           "dry-run (empty string)",
			toolFlag:       "",
			expectWrite:    false,
			expectCommand:  false,
			expectUseTools: true,
		},
		{
			name:           "none",
			toolFlag:       "none",
			expectWrite:    false,
			expectCommand:  false,
			expectUseTools: false,
		},
		{
			name:           "read",
			toolFlag:       "read",
			expectWrite:    false,
			expectCommand:  false,
			expectUseTools: true,
		},
		{
			name:           "write",
			toolFlag:       "write",
			expectWrite:    true,
			expectCommand:  false,
			expectUseTools: true,
		},
		{
			name:           "command",
			toolFlag:       "command",
			expectWrite:    false,
			expectCommand:  true,
			expectUseTools: true,
		},
		{
			name:           "all",
			toolFlag:       "all",
			expectWrite:    true,
			expectCommand:  true,
			expectUseTools: true,
		},
		{
			name:           "write,command",
			toolFlag:       "write,command",
			expectWrite:    true,
			expectCommand:  true,
			expectUseTools: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &options{tool: tt.toolFlag}

			gotWrite := opts.canExecuteWrite()
			if gotWrite != tt.expectWrite {
				t.Errorf("canExecuteWrite() = %v, want %v",
					gotWrite, tt.expectWrite)
			}

			gotCommand := opts.canExecuteCommand()
			if gotCommand != tt.expectCommand {
				t.Errorf("canExecuteCommand() = %v, want %v",
					gotCommand, tt.expectCommand)
			}

			gotUseTools := opts.canUseTools()
			if gotUseTools != tt.expectUseTools {
				t.Errorf("canUseTools() = %v, want %v",
					gotUseTools, tt.expectUseTools)
			}
		})
	}
}

// TestWriteFileExecution verifies write_file actually writes
func TestWriteFileExecution(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	tests := []struct {
		name        string
		toolFlag    string
		shouldWrite bool
	}{
		{
			name:        "dry-run",
			toolFlag:    "",
			shouldWrite: false,
		},
		{
			name:        "write permission",
			toolFlag:    "write",
			shouldWrite: true,
		},
		{
			name:        "all permission",
			toolFlag:    "all",
			shouldWrite: true,
		},
		{
			name:        "read only",
			toolFlag:    "read",
			shouldWrite: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up test file
			os.Remove(testFile)

			opts := &options{
				tool:      tt.toolFlag,
				verbosity: "silent",
			}

			toolUse := ContentBlock{
				Type: "tool_use",
				ID:   "test-id",
				Name: "write_file",
				Input: map[string]interface{}{
					"path":    testFile,
					"content": "test content",
				},
			}

			result, err := executeWriteFile(toolUse, tmpDir, opts, "test-conv")
			if err != nil {
				t.Fatalf("executeWriteFile failed: %v", err)
			}

			// Check if file exists
			_, statErr := os.Stat(testFile)
			fileExists := statErr == nil

			if fileExists != tt.shouldWrite {
				t.Errorf("file exists = %v, want %v", fileExists, tt.shouldWrite)
				t.Logf("tool flag: %q", tt.toolFlag)
				t.Logf("canExecuteWrite: %v", opts.canExecuteWrite())
				t.Logf("result content: %s", result.Content)
			}

			if tt.shouldWrite {
				content, _ := os.ReadFile(testFile)
				if string(content) != "test content" {
					t.Errorf("file content = %q, want %q",
						string(content), "test content")
				}
			}
		})
	}
}

// TestBashCommandExecution verifies bash_command execution
func TestBashCommandExecution(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name         string
		toolFlag     string
		command      string
		shouldExec   bool
		shouldError  bool
		errorPattern string
	}{
		{
			name:       "dry-run ls",
			toolFlag:   "",
			command:    "ls",
			shouldExec: false,
		},
		{
			name:       "execute ls with command",
			toolFlag:   "command",
			command:    "ls",
			shouldExec: true,
		},
		{
			name:       "execute ls with all",
			toolFlag:   "all",
			command:    "ls",
			shouldExec: true,
		},
		{
			name:         "blocked command sudo",
			toolFlag:     "all",
			command:      "sudo ls",
			shouldExec:   false,
			shouldError:  true,
			errorPattern: "blocked pattern: sudo",
		},
		{
			name:         "blocked command rm",
			toolFlag:     "all",
			command:      "rm file.txt",
			shouldExec:   false,
			shouldError:  true,
			errorPattern: "blocked pattern: rm",
		},
		{
			name:       "whitelisted echo",
			toolFlag:   "all",
			command:    "echo hello",
			shouldExec: true,
		},
		{
			name:       "pipe with whitelist",
			toolFlag:   "all",
			command:    "echo hello | grep hello",
			shouldExec: true,
		},
		{
			name:         "not in whitelist",
			toolFlag:     "all",
			command:      "python script.py",
			shouldExec:   false,
			shouldError:  true,
			errorPattern: "not in whitelist: python",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &options{
				tool:      tt.toolFlag,
				verbosity: "silent",
			}

			toolUse := ContentBlock{
				Type: "tool_use",
				ID:   "test-id",
				Name: "bash_command",
				Input: map[string]interface{}{
					"command": tt.command,
					"reason":  "test execution",
				},
			}

			result, err := executeBashCommand(toolUse, tmpDir, opts, "test-conv")
			if err != nil {
				t.Fatalf("executeBashCommand returned error: %v", err)
			}

			// Check if validation error occurred
			if tt.shouldError {
				if result.Type != "tool_result" {
					t.Errorf("expected tool_result, got %s", result.Type)
				}
				// Error should be in Content
				if tt.errorPattern != "" && 
					!contains(result.Content, tt.errorPattern) {
					t.Errorf("expected error containing %q, got %q",
						tt.errorPattern, result.Content)
				}
				return
			}

			// Check if command was executed or dry-run
			isDryRun := contains(result.Content, "Dry-run")
			wasExecuted := contains(result.Content, "Exit code")

			if tt.shouldExec && !wasExecuted {
				t.Errorf("command should execute but got dry-run\n"+
					"tool flag: %q\n"+
					"canExecuteCommand: %v\n"+
					"result: %s",
					tt.toolFlag,
					opts.canExecuteCommand(),
					result.Content)
			}

			if !tt.shouldExec && wasExecuted {
				t.Errorf("command should not execute but did\n"+
					"result: %s", result.Content)
			}

			if !tt.shouldExec && !isDryRun {
				t.Errorf("expected dry-run message but got: %s",
					result.Content)
			}
		})
	}
}

// TestBashCommandValidation tests the validation logic
func TestBashCommandValidation(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "whitelisted ls",
			command: "ls -la",
			wantErr: false,
		},
		{
			name:    "whitelisted cat",
			command: "cat file.txt",
			wantErr: false,
		},
		{
			name:    "whitelisted pipe",
			command: "ls | grep .go",
			wantErr: false,
		},
		{
			name:    "git log allowed",
			command: "git log --oneline",
			wantErr: false,
		},
		{
			name:    "git diff allowed",
			command: "git diff HEAD",
			wantErr: false,
		},
		{
			name:    "git push blocked",
			command: "git push origin main",
			wantErr: true,
			errMsg:  "git subcommand not allowed: push",
		},
		{
			name:    "sudo blocked",
			command: "sudo ls",
			wantErr: true,
			errMsg:  "blocked pattern: sudo",
		},
		{
			name:    "rm blocked",
			command: "rm file.txt",
			wantErr: true,
			errMsg:  "blocked pattern: rm",
		},
		{
			name:    "mv blocked",
			command: "mv a.txt b.txt",
			wantErr: true,
			errMsg:  "blocked pattern: mv",
		},
		{
			name:    "curl blocked",
			command: "curl https://example.com",
			wantErr: true,
			errMsg:  "blocked pattern: curl",
		},
		{
			name:    "path traversal blocked",
			command: "cat ../../../etc/passwd",
			wantErr: true,
			errMsg:  "path traversal not allowed",
		},
		{
			name:    "command chaining blocked",
			command: "ls && rm file.txt",
			wantErr: true,
			errMsg:  "blocked pattern: &&",
		},
		{
			name:    "or chaining blocked",
			command: "ls || echo fail",
			wantErr: true,
			errMsg:  "blocked pattern: ||",
		},
		{
			name:    "not in whitelist",
			command: "python script.py",
			wantErr: true,
			errMsg:  "not in whitelist: python",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCommand(tt.command)

			if tt.wantErr && err == nil {
				t.Errorf("validateCommand() expected error, got nil")
			}

			if !tt.wantErr && err != nil {
				t.Errorf("validateCommand() unexpected error: %v", err)
			}

			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("error = %q, want substring %q",
						err.Error(), tt.errMsg)
				}
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && 
		(s == substr || len(s) > 0 && anySubstring(s, substr))
}

func anySubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
