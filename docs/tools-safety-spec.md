# Tool Safety Specification

**Status as of 2026-01-06:**
- Phase 1 (bash_command) is **IMPLEMENTED**
- Phase 2 (git integration) is **NOT YET IMPLEMENTED**  
- Phase 3 (firejail) is **PLANNED**

## 1. bash_command Tool (IMPLEMENTED)

### Current Implementation Status

**âœ" Implemented in claude.go:**
- Whitelist validation (11 commands: ls, cat, grep, find, head, tail, wc, echo, pwd, date, git, go)
- Blocklist patterns (sudo, rm, mv, cp, chmod, chown, curl, wget, ||, &&)
- Git subcommand filtering (only: log, diff, show, status, blame)
- Pipe parsing with per-command validation
- Path traversal detection (..)
- 30-second timeout with partial output capture
- Full audit logging to .claude/tool_log.jsonl

**âœ— Not Yet Implemented:**
- Git auto-commit before tool execution
- --rollback command
- Firejail process isolation
- Redirect validation
- Domain whitelisting for curl/wget

### Tool Schema
```
{
  Name: "bash_command",
  Description: "Execute a bash command. Whitelist enforced. Shows preview in dry-run.",
  InputSchema: {
    command: string  // the command to run
    reason: string   // why this command is needed (for audit)
  }
}
```

### Safety: Command Whitelist (Phase 1 - IMPLEMENTED)
```
Allowed commands (exact match on first word):
  ls, cat, grep, find, head, tail, wc, echo, pwd, date
  git (read-only: log, diff, show, status, blame)
  go (all subcommands allowed)

Allowed patterns:
  pipes: cmd1 | cmd2  (each command validated separately)

Blocked patterns:
  rm, mv, cp, chmod, chown, sudo, su
  curl, wget (Phase 3: allow with domain whitelist)
  ||, && (command chaining)
  path traversal: .. anywhere in command

Current Implementation (validateCommand in claude.go):
- Split on pipes, validate each command
- Check first word against allowedCommands map
- Block dangerous patterns via strings.Contains()
- Special git validation: only safe read operations
- Path traversal blocked (..)
```
```

### Dry-run behavior (IMPLEMENTED)
```
Dry-run (default, --tool=""):
- Parse and validate command
- Show what WOULD execute with reason
- Display: "Dry-run: would execute command: <cmd>\nReason: <reason>\nUse --tool=command or --tool=all to execute"
- Log to audit with dry_run=true flag
- Return tool_result with dry-run message

Execute (--tool=command or --tool=all):
- Validate command (same whitelist checks)
- Execute with 30-second timeout (bashCommandTimeout const)
- Capture stdout/stderr
- Return exit code, duration, output
- Log to audit with dry_run=false flag
```

### Execution restrictions (Phase 3 - NOT YET IMPLEMENTED)
```
Future enhancement: firejail integration

Use firejail if available:
  firejail \
    --noprofile \
    --private-tmp \
    --private-dev \
    --noroot \
    --net=none \
    --whitelist=${workingDir} \
    -- bash -c "${command}"

Current: Run with whitelist-only protection
  - No process isolation (runs as current user)
  - Working directory set to project root
  - Full environment inherited

Why firejail over chroot:
- Don't need root
- Can inspect results in working directory
- Process isolation without filesystem complexity
- Easy to install: apt/brew install firejail
```

### Tool result handling (IMPLEMENTED)
```
Current implementation in executeBashCommand():

Success (exit_code == 0):
{
  type: "tool_result",
  tool_use_id: "<id>",
  content: "Exit code: 0\nDuration: 123ms\nStdout:\n<output>\nStderr:\n<errors>"
}

Failure (exit_code != 0):
{
  type: "tool_result",
  tool_use_id: "<id>",
  content: "Error: Exit code: 1\nDuration: 234ms\nStdout:\n<output>\nStderr:\n<errors>",
  is_error: true
}

Timeout (context.DeadlineExceeded):
{
  type: "tool_result", 
  tool_use_id: "<id>",
  content: "Error: Command timeout after 30s\nStdout: <partial>\nStderr: <partial>",
  is_error: true
}

Validation failure:
{
  type: "tool_result",
  tool_use_id: "<id>",
  content: "Error: blocked pattern: sudo",
  is_error: true
}

All results logged to audit trail with:
- exit_code, stdout, stderr, duration
- success=true only if exit_code==0
```

## 2. Tool Audit Log (IMPLEMENTED)

### Log file: .claude/tool_log.jsonl

**Status: Fully implemented in storage.go**

```
Format: newline-delimited JSON (one entry per line)

Actual implementation (AuditLogEntry struct):
{
  timestamp: "20060102_150405",
  tool: "bash_command",
  input: {command: "ls -la", reason: "check directory"},
  result: {exit_code: 0, stdout: "...", stderr: "", duration: 123},
  success: true,
  duration_ms: 123,
  conversation_id: "20060102_150000",
  dry_run: false,
  error: ""  // populated on failure
}

Why JSONL:
- Append-only (safe on crash)
- Easy to grep/parse: grep '"tool":"bash_command"' .claude/tool_log.jsonl
- One tool execution = one line
- Best-effort logging (doesn't fail tool execution if log write fails)

Current usage:
- All tools log: read_file, write_file, bash_command
- Logged via appendAuditLog() in storage.go
- Logs both dry-run and actual executions (dry_run flag differentiates)

Not yet implemented:
- git_commit field (Phase 2)
- --tool-log viewer flag
```

## 3. Git Integration (Phase 2 - NOT YET IMPLEMENTED)

### Auto-commit before tool execution
```
Goal: Create safety checkpoint before each tool execution

Implementation plan:
1. Before executing tool (write_file, bash_command):
   - Check if in git repo (git rev-parse --git-dir)
   - Stage all changes (git add -A)
   - Commit with message: "pre-tool: <tool_name> at <timestamp>"
   - Store commit hash in audit log

2. If commit fails:
   - Log warning
   - Continue with tool execution (don't block)

3. User can rollback:
   - --rollback [N]: reset to N tool executions ago
   - Uses git log + audit log to find correct commit
   - git reset --hard <commit_hash>
```

### Rollback command (NOT YET IMPLEMENTED)
```
Usage: claude --rollback[=N]

Examples:
  claude --rollback      # undo last tool execution
  claude --rollback=3    # undo last 3 tool executions

Implementation:
1. Read tool_log.jsonl
2. Find last N entries with git_commit field
3. Get oldest commit hash
4. git reset --hard <hash>
5. Remove those entries from audit log (or mark as rolled back)
```

## 4. Configuration

### Flags (implementation status)

**Implemented:**
```
--tool=<mode>         permission mode: "", none, read, write, command, all
--replay[=timestamp]  replay tools from saved response
--verbosity=<level>   silent, normal, verbose, debug
--max-cost=<dollars>  stop if cost exceeds limit
--stats               show conversation statistics
--prune-old=<N>       keep only last N request/response pairs
```

**Not yet implemented (from spec):**
```
--no-git              disable git auto-commit
--rollback[=N]        rollback last N tool executions
--tool-log [N]        show last N tool executions from audit
```

### config.json fields

**Current implementation (Config struct in storage.go):**
```json
{
  "model": "claude-sonnet-4-5-20250929",
  "system_prompt": "...",
  "total_input_tokens": 12345,
  "total_output_tokens": 6789,
  "first_run": "20060102_150405",
  "last_run": "20060102_160405"
}
```

**Future (Phase 2) - tool_safety section:**
```json
{
  ...existing fields...,
  "tool_safety": {
    "git_enabled": true,
    "command_timeout": 30,
    "firejail_enabled": false
  }
}
```

Note: command_whitelist is currently hardcoded in claude.go (allowedCommands map).
Future: Make configurable per-project.

## 5. Implementation Status Summary

**Phase 1 - COMPLETED:**
1. âœ" Tool audit log (tool_log.jsonl) - appendAuditLog() in storage.go
2. âœ" bash_command with whitelist - validateCommand() + executeBashCommand()
3. âœ" Dry-run preview - default behavior, checks canExecuteCommand()
4. âœ" Command timeout - 30s hardcoded (bashCommandTimeout const)
5. âœ" Pipe validation - regex split + per-command check

**Phase 2 - IN PROGRESS:**
6. âœ— Git pre-tool commits - not implemented
7. âœ— --rollback command - not implemented
8. âœ— --tool-log viewer - not implemented

**Phase 3 - PLANNED:**
9. Firejail integration
10. Tool result size limits
11. Per-tool permissions granularity (--tool=read,write but not command)
12. Domain whitelist for curl/wget
13. Undo stack (more granular than git)

## 6. Error Handling (Current Implementation)

```
Tool validation failure:
  Current: validateCommand() returns error
  Result: makeToolError() with error message
  Example: "Error: blocked pattern: sudo"
  → Tool does not execute, error returned to Claude

Path safety violation:
  Current: isSafePath() checks working directory prefix
  Result: makeToolError() with "path outside project: <path>"
  → Tool does not execute

Command timeout:
  Current: context.WithTimeout(bashCommandTimeout)
  Result: Partial stdout/stderr captured, exit_code=-1
  Error message: "Command timeout after 30s"
  → Logged as failed execution (success=false)

Audit log write failure:
  Current: appendAuditLog() fails
  Result: Warning printed to stderr, tool execution continues
  Message: "Warning: failed to write audit log: <err>"
  → Best-effort logging, doesn't block tool

Git integration (Phase 2, not yet implemented):
Git commit failure:
  "Warning: git auto-commit failed (not in git repo?)"
  → Continue with tool execution, log warning

Rollback when no git:
  "Error: rollback requires git repository"
  → Exit with error

Firejail not found (Phase 3):
  "Warning: firejail not installed, running without isolation"
  → Continue with whitelist-only protection
```

## 7. User Communication (Current Implementation)

**Normal/Verbose output shows:**
- Tool header with dry-run indicator
- Diffs for write_file (via ShowDiff from display.go)
- Command preview for bash_command dry-runs
- Execution results (exit code, duration, stdout/stderr)

**Current output examples:**

**write_file dry-run:**
```
=== path/to/file.go (dry-run) ===
--- old
+++ new
@@ -1,3 +1,5 @@
 package main
 
+import "fmt"
+
 func main() {
(dry-run: use --tool=write to apply)
```

**bash_command dry-run:**
```
=== bash_command (dry-run) ===
Dry-run: would execute command: ls -la | grep .go
Reason: check Go source files
Use --tool=command or --tool=all to execute
```

**bash_command execution:**
```
=== bash_command ===
Exit code: 0
Duration: 45ms
Stdout:
-rw-r--r-- 1 user user 1234 Jan 5 15:30 main.go
-rw-r--r-- 1 user user 5678 Jan 5 15:30 types.go
Stderr:

```

**Verbose mode additions (--verbosity=verbose):**
- "Tool: bash_command("ls -la")"
- "Replaying response: 20060102_150405"
- Token counts and API call details

**Future enhancements (Phase 2):**
- Git commit hash display
- Command validation breakdown
- Timeout warnings before execution
