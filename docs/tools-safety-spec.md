{
  Name: "bash_command",
  Description: "Execute a bash command. Whitelist enforced. Shows preview in dry-run.",
  InputSchema: {
    command: string  // the command to run
    reason: string   // why this command is needed (for audit)
  }
}
```

### Safety: Command Whitelist (Phase 1)
```
Allowed commands (exact match on first word):
  ls, cat, grep, find, head, tail, wc, echo, pwd, date
  git (read-only: log, diff, show, status)
  go (build, test, run, mod)

Allowed patterns:
  pipes: cmd1 | cmd2
  redirects: cmd > file (only to working directory)

Blocked:
  rm, mv, cp, chmod, chown, sudo, su
  curl, wget (unless to whitelist domains)
  any command with sudo, su, or |sudo, ||, &&
  path traversal in redirects: > ../outside.txt

Implementation:
- parseCommand(cmd) → validate first word in whitelist
- checkPatterns(cmd) → ensure no blocked patterns
- checkRedirects(cmd) → ensure redirects stay in workingDir
```

### Dry-run behavior
```
Dry-run (default):
- Parse and validate command
- Show what WOULD execute
- Show environment (cwd, env vars)
- Return mock success

Execute (--tool=command or --tool=all):
- Validate command
- Set timeout (default 30s, configurable --command-timeout)
- Execute with restricted environment
- Capture stdout/stderr
- Return actual result
```

### Execution restrictions (Phase 2 - firejail)
```
Use firejail if available:
  firejail \
    --noprofile \
    --private-tmp \
    --private-dev \
    --noroot \
    --net=none \
    --whitelist=${workingDir} \
    -- bash -c "${command}"

Fallback without firejail:
  Run with current restrictions (whitelist only)
  Warn user: "firejail not found, running without isolation"

Why firejail over chroot:
- Don't need root
- Can inspect results in working directory
- Process isolation without filesystem complexity
- Easy to install: apt/brew install firejail
```

### Tool result handling
```
Success:
{
  stdout: "output here",
  stderr: "warnings here",
  exit_code: 0,
  duration_ms: 1234
}

Failure:
{
  error: "command failed: exit code 1",
  stdout: "partial output",
  stderr: "error message",
  exit_code: 1
}

Timeout:
{
  error: "command timeout after 30s",
  stdout: "partial output before timeout",
  stderr: "",
  exit_code: -1
}
```

## 3. Tool Audit Log

### Log file: .claude/tool_log.jsonl
```
Format: newline-delimited JSON (one entry per line)

Entry structure:
{
  timestamp: "20060102_150405",
  tool: "write_file",
  input: {path: "test.go", content: "..."},
  result: {success: true},
  git_commit: "abc123",  // commit hash before tool
  duration_ms: 45,
  conversation_id: "20060102_150000"  // which conversation
}

Why JSONL:
- Append-only (safe on crash)
- Easy to grep/parse
- One tool = one line

Access:
  --tool-log [N]  # show last N tool executions
```

## 4. Configuration

### New flags
```
--no-git              disable git auto-commit
--command-timeout N   bash_command timeout in seconds (default 30)
--tool-log [N]        show last N tool executions
--rollback[=N]        rollback last N tool executions
```

### New config.json fields
```
{
  ...existing fields...
  "tool_safety": {
    "git_enabled": true,
    "command_whitelist": ["ls", "cat", ...],
    "command_timeout": 30,
    "firejail_enabled": true
  }
}
```

## 5. Implementation Order

**Phase 1 (Immediate):**
1. Tool audit log (tool_log.jsonl)
2. Git pre-tool commits
3. bash_command with whitelist only
4. Dry-run preview for bash_command
5. --rollback command

**Phase 2 (Medium):**
6. Firejail integration
7. Command timeout enforcement
8. --tool-log viewer
9. Tool result size limits

**Phase 3 (Future):**
10. Per-tool permissions (--tool=read,write but not command)
11. Domain whitelist for curl/wget
12. Undo stack (more granular than git)

## 6. Error Handling
```
Tool validation failure:
  "Command 'rm' not in whitelist"
  → Return error to Claude, don't execute

Git commit failure:
  "Warning: git auto-commit failed (not in git repo?)"
  → Continue with tool execution, log warning

Firejail not found:
  "Warning: firejail not installed, running without isolation"
  → Continue with whitelist-only protection

Rollback when no git:
  "Error: rollback requires git repository"
  → Exit with error
```

## 7. User Communication

**Verbose output shows:**
- Tool being executed
- Command validation result
- Git commit created
- Execution time
- Tool result summary

**Example:**
```
Tool: bash_command
Command: ls -la | grep .go
Validation: ✓ passed (ls, grep in whitelist)
Git: committed abc123 (pre-tool: bash_command at 20060102_150405)
Executing with timeout 30s...
Duration: 123ms
Result: 15 .go files found
