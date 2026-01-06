# go-claude Development Context

**Last Updated:** 2026-01-05

## Project Overview
Terminal-only CLI for Claude AI with tool support. Built for Unix workflows (tmux, vim, shell). No GUI, no IDE.

## Current Architecture

### File Structure
```
cmd/claude/
  ├── claude.go      # 1263 lines: Main CLI, API calls, tool execution
  ├── storage.go     # 232 lines: Request/response pairs, audit log, replay
  ├── display.go     # 344 lines: Terminal formatting, diffs, syntax highlighting
  ├── api_test.go    # Mock API server, error handling tests
  └── claude_test.go # Storage, options, helper function tests
```

### Storage System (CRITICAL)
**Design:** Request/response file pairs with timestamps
```
.claude/
  ├── config.json                      # Aggregate stats (tokens, costs, first/last run)
  ├── request_20060102_150405.json     # Single request (full conversation context)
  └── response_20060102_150405.json    # Array of ALL API responses: [{iter0}, {iter1}, ...]
```

**Why this design:**
- Zero duplication (no conversation.json, no history.json)
- Perfect audit trail
- Easy pruning (delete old pairs)
- Replay without API calls (saves tokens/cost)
- DB export ready (SQLite/Postgres)

**Conversation reconstruction:** Scan .claude/, sort by timestamp, pair up request/response files

### Display System (display.go)
**Design:** Separation of concerns - formatting vs persistence
- **TTY Detection:** `isTTY()` checks if output is terminal
  - Terminal: ANSI colors, syntax highlighting
  - Pipe/file: Plain text only (NO escape codes)
- **Syntax Highlighting:** chroma library for code blocks
  - Automatic language detection
  - Multiple themes (monokai default)
- **Unified Diff:** Git-style diff with context
  - Color-coded: red (deletions), green (additions), cyan (hunks)
  - Handles new files, deletions, modifications
- **Markdown Formatting:** Colored headers, bullets, quotes
- **Helper Functions:** `ToolHeader()`, `ToolResult()`, `Warning()`, `Info()`

**Critical principle:** Display functions NEVER write files. Only format for human viewing.

### Audit System
**Design:** Complete tool execution trail in .claude/tool_log.jsonl
- **Format:** JSONL (newline-delimited JSON)
  - Append-only (crash-safe)
  - One tool execution = one line
  - Easy to grep/parse
- **Fields:** timestamp, tool, input, result, success, duration_ms, conversation_id, dry_run, error
- **Usage:** Every tool execution logged (read_file, write_file, bash_command)
- **Access:** Direct file inspection or future --tool-log viewer

**Conversation reconstruction:** Scan .claude/, sort by timestamp, pair up request/response files

### Key Features
- `--tool` permission system: "", none, read, write, command, all
  - `""` = dry-run (default) - shows what WOULD happen
  - `write` = execute file writes
  - `all` = execute everything
- `--replay` = re-execute tools from saved response WITHOUT calling API
  - `--replay=""` = replay latest
  - `--replay=20060102_150405` = replay specific timestamp
- `--prune-old N` = keep only last N request/response pairs
- Multi-iteration agentic loop with tool use

### Response Array Format
Response file contains array of ALL API responses from agentic loop:
```json
[
  { "id": "...", "content": [{"type": "tool_use", ...}], "stop_reason": "tool_use" },
  { "id": "...", "content": [{"type": "text", ...}], "stop_reason": "end_turn" }
]
```
This captures intermediate tool_use iterations, not just final response.

## Workflow
```bash
# 1. Dry-run to see what tools would execute
echo "create test.txt with hello" | go run ./cmd/claude

# 2. Inspect diffs shown in output
# 3. Replay with execution (NO API CALL, saves tokens)
go run ./cmd/claude --replay="" --tool=write

# 4. Stats
go run ./cmd/claude --stats
```

## Current State

**When providing patches:**
1. ALWAYS use `write_file` tool to write patch files to disk
2. NEVER just display patches in markdown - user can't apply them
3. Patch filename format: `NNNN-description.patch` (e.g., `0001-add-feature.patch`)
4. User applies with: `git apply <patchfile>` or `patch -p1 < <patchfile>`

### What Works
- Tool execution: read_file, write_file, bash_command
- bash_command with comprehensive safety:
  - Whitelist: ls, cat, grep, find, head, tail, wc, echo, pwd, date, git, go
  - Blocklist: sudo, rm, mv, cp, chmod, chown, curl, wget, ||, &&
  - Git limited to: log, diff, show, status, blame
  - 30-second timeout with partial output on timeout
  - Path traversal protection
- Complete audit logging system (tool_log.jsonl)
  - Captures: timestamp, tool, input, result, success, duration, dry_run flag
  - JSONL format (append-only, crash-safe)
- Request/response saving as file pairs
- Response saved as array (captures all iterations)
- Replay from saved responses (re-executes tools without API calls)
- Dry-run mode (default, shows diffs/commands without executing)
- Terminal display system (display.go):
  - Syntax highlighting via chroma library
  - Proper unified diff with git-style colors
  - TTY detection (never writes ANSI codes to files)
  - Markdown formatting with colored headers, bullets, quotes
- Cost tracking with --max-cost enforcement
- Token estimation (4 chars = 1 token approximation)
- Conversation truncation (--truncate flag)
- Multiple verbosity levels: silent, normal, verbose, debug
- JSON output mode (--output=json) for scripting

### What Doesn't Work / Missing
- **Incomplete test coverage** (critical gap)
  - bash_command validation edge cases
  - Display formatting tests
  - Tool execution integration tests
  - Safety boundary conditions
- Atomic file writes (crash-safe)
  - Currently uses os.WriteFile directly
  - Need temp file + rename pattern

## Design Decisions Made

1. **Response as array, not NDJSON**
   - Rationale: Simpler code, files are small, discrete transactions
   - NDJSON rejected: overkill for this use case

2. **Same timestamp for request+response pair**
   - Ensures atomic pairing
   - Format: `20060102_150405` (Go time format)

3. **Config stores FirstRun/LastRun instead of CreatedAt/UpdatedAt**
   - Better semantics for file-based system

4. **80 column limit**
   - Keep nesting reasonable
   - Use early continue instead of deep if nesting

5. **Flag design:**
   - `--replay` as string (empty = latest, or specific timestamp)
   - Default value "NOREPLAY" to detect if flag was set

## TODOs (Priority Order)

### High Priority
1. **Expand test coverage** (`cmd/claude/*_test.go`)
   - bash_command validation edge cases (whitelist bypass attempts)
   - Display formatting (diff generation, TTY detection)
   - Tool execution integration (multi-iteration loops)
   - Safety boundary conditions (path traversal, command injection)
   - Current: Basic storage/options tests exist, need comprehensive coverage

2. **Atomic file writes** (crash-safe)
   - Write to temp file, rename on success
   - Prevents corruption on crash
   - Pattern: `ioutil.TempFile()` + `os.Rename()`

### Medium Priority
3. **Git integration for tool safety**
   - Auto-commit before each tool execution (as specified in tool-safety-spec.md)
   - Enable --rollback to undo tool changes
   - Configurable via --no-git flag

4. **DB export capability**
   - SQLite/Postgres from request/response pairs
   - Useful for analytics, searching old conversations

5. **Enhanced bash_command safety (Phase 2)**
   - Firejail integration for process isolation
   - Domain whitelist for curl/wget
   - Per-tool permissions granularity

### Low Priority / Future
6. **Dynamic model list**
   - Query https://docs.anthropic.com/en/docs/about-claude/models
   - Currently hardcoded

7. **Streaming responses** (SSE from API)

8. **Project-level context** (scan all .go files)

9. **Templates for common tasks**

10. **Tool log viewer** (--tool-log flag to display recent executions)

## Open Questions

1. **Tool design:**
   - Should tools have undo capability?
   - How to handle tool failures mid-agentic-loop?
   - Need separate tool logging?

2. **Safety:**
   - Path traversal protection adequate? (currently: isSafePath checks prefix)
   - bash_command sandboxing approach?
   - Whitelist/blacklist for dangerous commands?

3. **Conversation management:**
   - Keep full history forever or auto-prune old?
   - Export/import conversations?
   - Search across all conversations?

4. **Cost control:**
   - Token estimation is rough (4 chars = 1 token)
   - Track costs per-conversation or global?
   - Warning before expensive operations?

## Important Details / Gotchas

1. **Flag parsing quirk:** `--replay` with no value doesn't work well with Go's flag package
   - Use `--replay=""` for latest, or `--replay=TIMESTAMP` for specific

2. **Tool execution in agentic loop:**
   - Tools are executed, results added to messages, API called again
   - This can happen multiple times (hence response array)
   - Cost accumulates across iterations

3. **Dry-run behavior:**
   - Shows diffs but doesn't execute
   - User must explicitly replay with `--tool=write` to apply

## Code Style Preferences

- 80 column limit (soft)
- Early continue over nested ifs
- Descriptive variable names
- Comments explain WHY, not WHAT
- Unix philosophy: do one thing well

## Development Workflow

- Test driven development
- prefer patches that can be applied instead of token burning whole files.

Going forward, use go-claude itself for development:
```bash
echo "add tests for storage functions" | go run ./cmd/claude --tool=write
```

Maintain this context.md as source of truth. Update as decisions are made.

## Developer Profile

Solo developer, Unix/Go expert. Prefers:
- Terminal-only workflows (tmux, vim, shell)
- Terse, technical communication
- Patches over whole files (must be generated by git diff or diff -uNp)
- Code must be readable like a book and should be the technical implementation details
- 80-column code, early returns
- Test-driven when reasonable
- Pragmatic over perfect
