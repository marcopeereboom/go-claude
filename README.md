# go-claude

Terminal-only CLI for Claude AI with agentic tool support and conversation replay.

Built for Unix workflows - no GUI, no IDE, just shell and vim.

## Features

- **Local + Cloud LLMs** - Use Ollama (local/free) or Claude (cloud/paid)
- **Smart routing** - 90% local, 10% cloud by default
- **Automatic fallback** - Falls back to Claude if Ollama fails
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

### Setup

**Option 1: Local-first (recommended)**

Install [Ollama](https://ollama.ai):
```bash
# Install Ollama
curl -fsSL https://ollama.com/install.sh | sh

# Pull a model with tool support
ollama pull llama3.1:8b

# No API key needed!
```

**Option 2: Claude only**

Set your API key:
```bash
export ANTHROPIC_API_KEY=your-key-here
```

**Option 3: Hybrid (best of both)**

Both Ollama + Claude API key for automatic routing.

### Basic Usage

**Use local Ollama (free):**
```bash
echo "write a fizzbuzz function in Go" | claude --model llama3.1:8b
```

**Use Claude (paid):**
```bash
echo "complex refactor task" | claude --model claude-sonnet-4-20250514
```

**Smart routing (automatic):**
```bash
# Simple tasks → Ollama (free)
echo "add godoc comments" | claude --prefer-local

# Complex tasks → Claude when needed
# Automatically falls back to Claude if Ollama fails
```

**Apply the changes:**
```bash
claude --replay="" --tool=write
```

**Check statistics:**
```bash
claude --stats

# Output shows provider usage:
# Provider usage:
#   Ollama: 45 requests (90.0%)
#   Claude: 5 requests (10.0%)
```

## Local LLM Support (Ollama)

go-claude integrates with [Ollama](https://ollama.ai) for local, free LLM execution.

### Why Local LLMs?

- **90% cost reduction** - Most tasks run locally for free
- **Privacy** - Your code never leaves your machine
- **Speed** - No network latency
- **Offline** - Works without internet

### Supported Models

Models with tool/function calling support:
- **llama3.1** (8b, 70b) - Best balance, recommended
- **qwen2.5** (7b, 32b) - Good for code
- **mistral** (7b) - Fast and capable
- **command-r** - Strong reasoning

See full list:
```bash
claude --models-list
```

### Smart Routing

go-claude analyzes each task and routes to the best provider:

**Ollama (local):**
- Documentation tasks
- Simple code changes
- Code review
- Basic questions

**Claude (cloud):**
- Complex refactoring
- Multi-file changes
- Tasks requiring tool execution
- When Ollama fails or doesn't support tools

**Configuration:**
```bash
# Default: 90% local, 10% cloud
echo "task" | claude --prefer-local

# Increase cloud quota to 20%
claude --max-claude-ratio 0.20

# Force local only (fails if Ollama unavailable)
claude --prefer-local --allow-fallback=false

# Force Claude (no local routing)
claude --prefer-local=false
```

### Ollama Examples

**List available models:**
```bash
claude --models-list

# Output shows:
# Ollama Models (local):
#   llama3.1:8b
#   llama3.1:70b
#   qwen2.5-coder:7b
#   mistral:7b
#
# Claude Models (cloud):
#   claude-sonnet-4-20250514
#   claude-opus-4-20250514
```

**Refresh model cache:**
```bash
claude --models-reload
```

**Use specific Ollama model:**
```bash
echo "optimize this function" | claude --model llama3.1:8b --tool=write
```

**Use larger model for complex tasks:**
```bash
echo "refactor entire module" | claude --model llama3.1:70b --tool=all
```

**Compare local vs cloud:**
```bash
# Try with Ollama first
echo "add tests for parseConfig" | claude --model llama3.1:8b

# Compare with Claude
echo "add tests for parseConfig" | claude --model claude-sonnet-4-20250514
```

### Troubleshooting Ollama

**Ollama not running:**
```bash
# Check if Ollama is running
ollama list

# Start Ollama service
ollama serve
```

**Model not found:**
```bash
# List installed models
ollama list

# Pull a model
ollama pull llama3.1:8b
```

**Custom Ollama URL:**
```bash
# Default: http://localhost:11434
claude --ollama-url http://other-host:11434
```

**Check provider stats:**
```bash
claude --stats

# Shows which provider handled each request
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
# Estimate cost before executing (Claude only)
echo "refactor display.go to pkg/display/" | claude --model claude-sonnet-4-20250514 --tool=all --estimate

# Output shows:
#   Input tokens:  ~3,500
#   Output tokens: ~1,200
#   Total cost:    ~$0.033

# Execute if cost is acceptable
claude --execute --max-cost-override=0.05

# Ollama costs nothing!
echo "same task" | claude --model llama3.1:8b --tool=all
# Cost: $0.00 (local execution)
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
├── config.json                      # aggregate stats + provider usage
├── request_20060102_150405.json     # what you sent
└── response_20060102_150405.json    # what Claude/Ollama returned (array)
```

**Why file pairs?**
- Zero duplication (no conversation.json/history.json)
- Easy to prune old conversations
- DB export ready (SQLite/Postgres)
- Perfect audit trail
- Provider-agnostic (same format for Claude/Ollama)

### Replay Workflow

```bash
# 1. Dry-run - see what the LLM wants to do
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

Claude/Ollama can:
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

**Create a new file (local):**
```bash
echo "create main.go with hello world" | claude --model llama3.1:8b --tool=write
```

**Refactor code (smart routing):**
```bash
echo "extract duplicate error handling into a helper function" | claude --prefer-local
# Review changes
claude --replay="" --tool=write
```

**Add tests (local):**
```bash
echo "add tests for parseConfig function" | claude --model qwen2.5-coder:7b --tool=write
```

**Debug failing tests (cloud for complex reasoning):**
```bash
go test ./... 2>&1 | claude --model claude-sonnet-4-20250514 --tool=write
```

**Code review (local):**
```bash
git diff | claude --model llama3.1:8b
```

## Flags

### Modes
- `--stats` - show conversation statistics and provider usage
- `--reset` - delete conversation history
- `--replay[=TIMESTAMP]` - replay tool execution (empty = latest)
- `--prune-old N` - keep only last N conversations
- `--models-list` - list available models (Claude + Ollama)
- `--models-reload` - refresh model cache from providers

### Smart Routing
- `--prefer-local` - prefer Ollama when possible (default: true)
- `--allow-fallback` - fallback to Claude on Ollama failure (default: true)
- `--max-claude-ratio N` - max fraction of Claude requests (default: 0.10 = 10%)

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
- `--model=MODEL` - LLM model to use (Claude or Ollama)
- `--ollama-url=URL` - Ollama API URL (default: http://localhost:11434)
- `--max-tokens=N` - tokens per API call (default: 1000)
- `--max-cost=N` - max cost in dollars for Claude (default: $1.00)
- `--max-iterations=N` - max tool loop iterations (default: 15)
- `--verbosity=LEVEL` - silent, normal, verbose, debug
- `--truncate=N` - keep last N messages only

## Development

We use go-claude to develop go-claude:

```bash
# Feed context and make changes (local)
cat docs/context.md - | claude --model llama3.1:8b
# Type your task, review, apply

# Run tests
go test -v ./...

# Update context after changes
vim docs/context.md
```

See [docs/development-workflow.md](docs/development-workflow.md) for details.

## Roadmap

**Current (v0.2):**
- ✓ Ollama integration (local LLMs)
- ✓ Smart routing (90% local, 10% cloud)
- ✓ Automatic fallback
- ✓ Provider usage tracking
- ✓ Model capability detection
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

**Future:**
- Command sandboxing (firejail)
- DB export (SQLite/Postgres)
- Multi-file context
- Streaming responses
- RAG integration
- Multi-model orchestration

## Requirements

- Go 1.21+
- Ollama (optional, for local execution)
- Anthropic API key (optional, for Claude)
- Git (optional, for rollback feature)

**Note:** Either Ollama or Claude API key required. Both recommended for best experience.

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
- Privacy-conscious users who want local execution

No Electron. No VS Code extension. Just stdin/stdout and files.
