# go-claude

Terminal-only CLI for Claude AI with agentic tool support and conversation replay.

Built for Unix workflows - no GUI, no IDE, just shell and vim.

## Features

- **Agentic tool execution** - Claude reads/writes files, executes commands
- **Dry-run by default** - see what would happen before applying
- **Replay without API calls** - re-execute tools, save tokens/costs
- **Request/response file pairs** - zero duplication, perfect audit trail
- **Git-based rollbacks** - auto-commit before tool execution (coming soon)
- **Permission system** - granular control over what Claude can do
- **Syntax highlighting**: Chroma-powered code and diff coloring
- **Audit logging**: Complete tool execution history

## Quick Start

### Installation

```bash
git clone https://github.com/marcopeereboom/go-claude.git
cd go-claude
go install ./cmd/claude
```

Set your API key:
```bash
export ANTHROPIC_API_KEY=your-key-here
```

### Basic Usage

**Ask Claude to do something:**
```bash
echo "add error handling to user.go" | claude
```

Claude shows what it would do (dry-run). Review the diffs.

**Apply the changes:**
```bash
claude --replay="" --tool=write
```

**Check statistics:**
```bash
claude --stats
```

# View with syntax highlighting (automatic in terminal)
echo "show me a web server in Go" | claude

# Save without colors (automatic when piping)
echo "show me a web server in Go" | claude > output.txt
```

## Syntax Highlighting

The CLI automatically applies syntax highlighting when outputting to a terminal:
- **200+ languages** supported via [chroma](https://github.com/alecthomas/chroma)
- **Git-style diffs** for file changes
- **No ANSI codes** in piped/file output

See [docs/syntax-highlighting.md](docs/syntax-highlighting.md) for details.

### Permission System

```bash
# Dry-run (default) - shows preview, doesn't execute
echo "create test.txt" | claude

# Allow file writes
echo "create test.txt" | claude --tool=write

# Allow all operations
echo "run tests and fix failures" | claude --tool=all

# Disable tools entirely
claude --tool=none
```

### Cost Estimation

Preview costs before executing expensive operations:

```bash
# Estimate cost before executing
echo "refactor display.go to pkg/display/" | claude --tool=all --estimate

# Output shows:
#   Input tokens:  ~3,500
#   Output tokens: ~1,200
#   Total cost:    ~$0.033

# Execute if cost is acceptable
claude --execute --max-cost-override=0.05
```

**Workflows:**

**Safe refactoring:**
```bash
# 1. Estimate
echo "complex refactor task" | claude --tool=all --estimate

# 2. Review cost, execute if OK
claude --execute --max-cost-override=0.10

# 3. Verify
git diff
go test ./...

# 4. Commit or rollback
git commit -m "refactor: ..." 
# OR git reset --hard
```

**Retry with higher budget:**
```bash
# Hit cost limit
echo "big task" | claude --tool=all
# Error: max cost exceeded ($1.20 > $1.00)

# Retry with override
claude --execute --max-cost-override=2.00
```

**Re-run after manual changes:**
```bash
# Made code changes, want Claude to try again
claude --execute --tool=write
```

**How it works:**
- `--estimate` calculates tokens (4 chars/token heuristic), saves message, doesn't execute
- `--execute` runs the last user message from conversation
- `--max-cost-override` overrides default max-cost for this run
- Model-specific pricing: Sonnet ($3/$15), Opus ($15/$75), Haiku ($0.80/$4)

**Limitations:** Current estimation uses heuristics (good for dogfooding). Doesn't account for tool iterations or context truncation.

## How It Works

### Storage System

Conversations stored as timestamped request/response file pairs:

```
.claude/
├── config.json                      # aggregate stats
├── request_20060102_150405.json     # what you sent
└── response_20060102_150405.json    # what Claude returned (array)
```

**Why file pairs?**
- Zero duplication (no conversation.json/history.json)
- Easy to prune old conversations
- DB export ready (SQLite/Postgres)
- Perfect audit trail

### Replay Workflow

```bash
# 1. Dry-run - see what Claude wants to do
echo "refactor database.go" | claude

# 2. Review diffs shown in output

# 3. Replay with execution - NO API CALL
claude --replay="" --tool=write

# Result: same changes applied, zero API cost
```

**Why replay?**
- Saves tokens (no redundant API calls)
- Saves money (especially on large responses)
- Faster (no network latency)
- Safer (inspect before execute)

### Tool Execution

Claude can:
- **read_file** - read any file in project
- **write_file** - create/modify files
- **bash_command** - execute shell commands (coming soon)

All tools respect permission flags and stay within project directory.

## Documentation

- [docs/context.md](docs/context.md) - Current state, architecture, TODOs
- [docs/development-workflow.md](docs/development-workflow.md) - How to develop
- [docs/testing-guide.md](docs/testing-guide.md) - How to write tests
- [docs/tool-safety-spec.md](docs/tool-safety-spec.md) - Safety features spec

## Examples

**Create a new file:**
```bash
echo "create main.go with hello world" | claude --tool=write
```

**Refactor code:**
```bash
echo "extract duplicate error handling into a helper function" | claude
# Review changes
claude --replay="" --tool=write
```

**Add tests:**
```bash
echo "add tests for parseConfig function" | claude --tool=write
```

**Debug failing tests:**
```bash
go test ./... 2>&1 | claude --tool=write
```

## Flags

### Modes
- `--stats` - show conversation statistics
- `--reset` - delete conversation history
- `--replay[=TIMESTAMP]` - replay tool execution (empty = latest)
- `--prune-old N` - keep only last N conversations

### Cost Estimation
- `--estimate` - show estimated cost without executing (saves message for --execute)
- `--execute` - execute last user message from conversation history
- `--max-cost-override N` - override max-cost for this run (use with --execute)

### Permissions
- `--tool=""` - dry-run (default)
- `--tool=none` - disable tools
- `--tool=read` - allow read_file only
- `--tool=write` - allow file modifications
- `--tool=command` - allow bash commands
- `--tool=all` - allow everything

### Configuration
- `--model=MODEL` - Claude model to use
- `--max-tokens=N` - tokens per API call (default: 1000)
- `--max-cost=N` - max cost in dollars (default: $1.00)
- `--max-iterations=N` - max tool loop iterations (default: 15)
- `--verbosity=LEVEL` - silent, normal, verbose, debug
- `--truncate=N` - keep last N messages only

## Development

We use go-claude to develop go-claude:

```bash
# Feed context and make changes
cat docs/context.md - | claude
# Type your task, review, apply

# Run tests
go test -v ./...

# Update context after changes
vim docs/context.md
```

See [docs/development-workflow.md](docs/development-workflow.md) for details.

## Roadmap

**Current (v0.1):**
- ✓ Basic tool execution (read/write files)
- ✓ Replay functionality
- ✓ Permission system
- ✓ Cost tracking
- ✓ Comprehensive tests

**Coming Soon:**
- bash_command tool with safety controls
- Git-based rollbacks
- Tool audit logging
- Proper unified diffs
- Syntax highlighting for terminal

**Future:**
- Command sandboxing (firejail)
- DB export (SQLite/Postgres)
- Multi-file context
- Streaming responses

## Requirements

- Go 1.21+
- Anthropic API key
- Git (optional, for rollback feature)

## License

MIT see [LICENSE](LICENSE)

## Contributing

Not accepting contributions yet (pre-1.0). Open issues for bugs/suggestions.

## Why Another Claude CLI?

Most AI tools are GUIs or IDE plugins. This is built for:
- Unix users who live in the terminal
- Workflows built around tmux, vim, shell
- People who want control and transparency
- Developers who script everything

No Electron. No VS Code extension. Just stdin/stdout and files.
