# go-claude Development Context

**Last Updated:** 2026-01-05

## Project Overview
Terminal-only CLI for Claude AI with tool support. Built for Unix workflows (tmux, vim, shell). No GUI, no IDE.

## Current Architecture

### File Structure
```
cmd/claude/
  ├── claude.go      # Main CLI, API calls, tool execution
  └── storage.go     # Request/response file pair management
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
- Basic tool execution (read_file, write_file)
- Request/response saving as file pairs
- Response saved as array (captures all iterations)
- Replay from saved responses
- Dry-run mode
- Cost tracking
- Token estimation
- Conversation truncation

### What Doesn't Work / Missing
- **NO TESTS** (critical gap)
- bash_command tool (only have read/write)
- Proper unified diff display (currently just dumps new content)
- Syntax highlighting for terminal output
- Atomic file writes (crash-safe)

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
1. **Add comprehensive tests** (`cmd/claude/claude_test.go`)
   - Test storage functions (saveRequest, saveResponse, loadConversationHistory)
   - Test replay logic
   - Test tool execution
   - Test permission system
   - Mock HTTP server for API simulation (heavily documented)

2. **Add bash_command tool**
   - Safety checks (whitelist/blacklist?)
   - Respect `--tool=command` or `--tool=all`

3. **Implement proper unified diff**
   - Use diff library or implement basic diff3
   - Show context lines, +/- markers
   - Current: just dumps new content

### Medium Priority
4. **Atomic file writes** (crash-safe)
   - Write to temp file, rename on success
   - Prevents corruption on crash

5. **Syntax highlighting for terminal output**
   - Detect TTY
   - Use ANSI escape codes for markdown/code blocks
   - Never write escape codes to files

6. **DB export capability**
   - SQLite/Postgres from request/response pairs
   - Useful for analytics, searching old conversations

### Low Priority / Future
7. **Dynamic model list**
   - Query https://docs.anthropic.com/en/docs/about-claude/models
   - Currently hardcoded

8. **Streaming responses** (SSE from API)

9. **Project-level context** (scan all .go files)

10. **Git integration** (auto-commit on changes?)

11. **Templates for common tasks**

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
