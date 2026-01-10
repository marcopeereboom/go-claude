```markdown
# Syntax Highlighting

The `claude` CLI provides automatic syntax highlighting for both code blocks in responses and file diffs during tool execution.

## Overview

All syntax highlighting is **display-only** and **terminal-aware**:
- ✅ Colored output when writing to a terminal (TTY)
- ✅ Plain text when piping to files or other commands
- ✅ No ANSI escape codes ever written to files on disk

## Architecture

### Separation of Concerns

The codebase maintains strict separation between display and file I/O:

```
display.go          - Format output for humans (terminal only)
storage.go          - Save/load files (always plain text)
claude.go           - Business logic (uses both, never mixes)
```

**Critical rule**: Functions in `display.go` NEVER write to files. File writes in `storage.go` and `claude.go` NEVER use display functions.

### Implementation

Syntax highlighting uses the [chroma](https://github.com/alecthomas/chroma) library:
- **200+ languages** automatically supported
- **Automatic detection** from code fence markers (```go, ```python, etc.)
- **Terminal256** color palette
- **Monokai** color scheme (configurable)
- **Zero manual parsing** - chroma handles everything

## Features

### 1. Markdown Response Formatting

When Claude's response is displayed to terminal, markdown is automatically formatted:


```
echo "explain this code" | claude
```

**Terminal output:**
- Headers: Bold blue
- Bullet points: Cyan
- Code blocks: Full syntax highlighting via chroma
- Block quotes: Gray
- Regular text: Default color

**File output** (no colors):

```
echo "explain this code" | claude > output.txt
# or
echo "explain this code" | claude --output-file=result.txt
```

### 2. File Diff Display

When using `write_file` tool, diffs are shown with git-style colors:


```
echo "add error handling to main.go" | claude --tool=write
```

**Terminal output:**
- `---` / `+++` headers: Bold
- `@@` hunk markers: Cyan
- `-` deleted lines: Red
- `+` added lines: Green
- Context lines: Default color

**Dry-run mode** (default):

```
echo "refactor auth.go" | claude
# Shows colored diff, doesn't modify files
```

### 3. Tool Execution Headers

Tool calls show styled headers in terminal:


```
=== path/to/file.go ===          # Cyan + bold (executing)
=== path/to/file.go (dry-run) === # Yellow + bold (preview)
```

### 4. Status Messages


```
✓ Successfully wrote to config.go     # Green checkmark
✗ Error: file not found               # Red X
⚠ Warning: large file (>1MB)          # Yellow warning
```

## Supported Languages

Chroma automatically detects and highlights **200+ languages**, including:

**Common languages:**
- Go, Python, JavaScript, TypeScript, Rust, C, C++, Java
- Bash, Shell, PowerShell, Batch
- HTML, CSS, SCSS, LESS
- JSON, YAML, TOML, XML
- Markdown, reStructuredText

**Less common but supported:**
- Zig, Nim, Crystal, Elixir, Erlang, Haskell, OCaml
- Assembly (x86, ARM, etc.)
- Dockerfile, Makefile, CMake
- Terraform, HCL, Jsonnet
- SQL, PostgreSQL, MySQL
- And 170+ more...

**Unknown languages**: Fall back to plain yellow text (no highlighting).

## Configuration

### Color Scheme

Default is `monokai`. To change, edit `display.go`:


```
// In highlightCode function
err := quick.Highlight(&buf, code, language, "terminal256", "github")
//                                                           ^^^^^^^^
// Options: monokai, github, vim, dracula, solarized-dark, etc.
```

Available schemes: https://xyproto.github.io/splash/docs/

### Formatter

Default is `terminal256` (best for modern terminals). Alternatives:


```
err := quick.Highlight(&buf, code, language, "terminal16", "monokai")
//                                            ^^^^^^^^^^^
// terminal16  - 16 colors (better compatibility)
// terminal256 - 256 colors (richer)
// terminal    - 8 colors (maximum compatibility)
```

## TTY Detection

The CLI automatically detects output destination:


```
func isTTY(f *os.File) bool {
    return term.IsTerminal(int(f.Fd()))
}
```

**When TTY is detected** (stdout/stderr to terminal):
- Apply syntax highlighting
- Add ANSI color codes
- Format markdown

**When TTY is NOT detected** (piped/redirected):
- Plain text output
- No ANSI codes
- No formatting

## Examples

### Example 1: View Response with Colors


```
echo "show me a hello world in Go" | claude
```

Output includes colored Go code with blue keywords, green strings, etc.

### Example 2: Save Response Without Colors


```
echo "show me a hello world in Go" | claude > output.txt
cat output.txt  # Plain text, no escape codes
```

### Example 3: Diff Preview (Dry-Run)


```
echo "add logging to auth.go" | claude
# Shows colored diff, doesn't modify file
```

### Example 4: Execute with Diff


```
echo "add logging to auth.go" | claude --tool=write
# Shows colored diff, THEN modifies file
```

### Example 5: JSON Output (No Formatting)


```
echo "explain async" | claude --output=json
# Raw JSON response, no markdown formatting
```

### Example 6: Replay with Colors


```
claude --replay
# Shows last response with full syntax highlighting
```

## Testing

Verify no ANSI codes leak into files:


```
# Test 1: Check file output
echo "write hello world" | claude --output-file=test.txt
hexdump -C test.txt | grep -E '\x1b\[' && echo "FAIL" || echo "PASS"

# Test 2: Check pipe output
echo "write hello world" | claude | cat > test2.txt
hexdump -C test2.txt | grep -E '\x1b\[' && echo "FAIL" || echo "PASS"

# Test 3: Visual terminal output
echo "write hello world in Go, Python, and Rust" | claude
# Should see colored code blocks
```

## Troubleshooting

### Colors not showing

**Check terminal support:**

```
echo $TERM
# Should be: xterm-256color, screen-256color, or similar
```

**Force TTY detection (for testing):**

```
# Use 'script' command to create pseudo-TTY
script -q /dev/null <<< "echo 'test' | claude"
```

### Wrong colors

Some terminals have custom color schemes that override ANSI codes.

**Fix**: Use a different chroma style in `display.go`:

```
// Try "github" for light terminals
err := quick.Highlight(&buf, code, language, "terminal256", "github")
```

### Colors in file output

If you see escape codes like `\033[31m` in files:

1. Check `isTTY()` is working:
   ```bash
   # This should NOT have colors
   echo "test" | claude > file.txt
   ```

2. Verify display functions aren't called during file writes:
   ```bash
   grep -n "FormatResponse\|ShowDiff" cmd/claude/storage.go
   # Should return nothing
   ```

### Unsupported language

If a code block isn't highlighted:

1. Check the language name in fence marker:
   ```markdown
   ```golang  ❌ (not recognized)
   ```go      ✅ (correct)
   ```

2. See supported languages:
   ```bash
   # In your Go code
   import "github.com/alecthomas/chroma/v2/lexers"
   lexers.Names(true)  // Prints all supported languages
   ```

## Performance

Chroma is fast for typical code blocks:
- **Small files (<1KB)**: <1ms overhead
- **Medium files (1-10KB)**: <5ms overhead
- **Large files (>100KB)**: ~50ms overhead

If highlighting becomes slow, increase timeout in `claude.go`:

```
client: &http.Client{
    Timeout: time.Duration(opts.timeout) * time.Second,
}
```

## Security

Syntax highlighting is **display-only** and poses no security risk:
- ✅ No code execution
- ✅ No file system access
- ✅ No network requests
- ✅ Pure text transformation

The chroma lexer only **parses** code structure, it never **executes** it.

## Related Documentation

- [Tool Usage](tools.md) - How file diffs are shown during tool execution
- [Output Modes](output-modes.md) - JSON vs text output formats
- [Replay Mode](replay.md) - Viewing historical responses with highlighting

```

---

**```markdown docs/architecture.md```** (add new section)

```markdown
## Display Layer Separation

### Critical Design Principle

The codebase maintains **absolute separation** between display formatting and file I/O:

```
┌─────────────────────────────────────────────┐
│  display.go                                 │
│  - FormatResponse() - markdown + chroma     │
│  - ShowDiff() - git-style diffs             │
│  - ToolHeader() - status messages           │
│  - NEVER writes to files                    │
└─────────────────────────────────────────────┘
                    │
                    ▼ (calls from)
┌─────────────────────────────────────────────┐
│  claude.go                                  │
│  - Business logic                           │
│  - Calls display.* for terminal output      │
│  - Calls storage.* for file I/O             │
│  - Keeps concerns separate                  │
└─────────────────────────────────────────────┘
                    │
                    ▼ (writes via)
┌─────────────────────────────────────────────┐
│  storage.go                                 │
│  - saveRequest() - writes JSON              │
│  - saveResponse() - writes JSON             │
│  - saveJSON() - generic file write          │
│  - NEVER uses display functions             │
└─────────────────────────────────────────────┘
```

### Rules

1. **display.go functions**:
   - ✅ May write to `os.Stdout`, `os.Stderr` (streams)
   - ✅ May add ANSI color codes
   - ✅ Check `isTTY()` before formatting
   - ❌ NEVER call `os.WriteFile()` or similar
   - ❌ NEVER called from storage.go

2. **storage.go functions**:
   - ✅ May write to disk
   - ✅ Always write plain text/JSON
   - ❌ NEVER add ANSI codes
   - ❌ NEVER call display.go functions

3. **claude.go orchestration**:
   - ✅ Calls display.* to show user output
   - ✅ Calls storage.* to persist state
   - ✅ Keeps the two layers completely separate

### Example: write_file Tool


```
func executeWriteFile(...) (ContentBlock, error) {
    // 1. Display diff to user (terminal only)
    if !opts.isSilent() {
        ToolHeader(path, !opts.canExecuteWrite())  // display.go
        ShowDiff(string(old), content)             // display.go
    }
    
    // 2. Write file (never uses display functions)
    if opts.canExecuteWrite() {
        err := os.WriteFile(path, []byte(content), 0o644)  // Direct write
        if err != nil {
            return makeToolError(toolUseID, err.Error())
        }
    }
    
    // 3. Return result (plain text, no formatting)
    return ContentBlock{
        Type:      "tool_result",
        ToolUseID: toolUse.ID,
        Content:   fmt.Sprintf("Successfully wrote to %s", path),
    }, nil
}
```

**Note**: The diff is shown via `ShowDiff()` which adds colors only if `isTTY(os.Stderr)`. The file write via `os.WriteFile()` always writes the raw `content` string with zero formatting.

### Testing Separation

Verify clean separation:


```
# Test 1: No display functions in storage
grep -rn "Format\|Show\|Tool\|Warning\|Info" cmd/claude/storage.go
# Should be empty

# Test 2: No file writes in display
grep -rn "WriteFile\|Create\|Open.*O_" cmd/claude/display.go
# Should be empty

# Test 3: No ANSI codes in saved files
find .claude/ -type f -exec grep -l $'\033\\[' {} \\;
# Should be empty
```

### Why This Matters

**Without separation**:

```
// BAD: Mixing concerns
func saveResponse(path string, content string) error {
    formatted := FormatResponse(content)  // ❌ Adds ANSI codes
    return os.WriteFile(path, formatted, 0644)  // ❌ Saves codes to disk
}
```

**With separation**:

```
// GOOD: Separate concerns
func saveResponse(path string, content string) error {
    return os.WriteFile(path, []byte(content), 0644)  // ✅ Plain text
}

func displayResponse(content string) {
    if isTTY(os.Stdout) {
        FormatResponse(os.Stdout, content)  // ✅ Colors to terminal
    } else {
        fmt.Print(content)  // ✅ Plain text to pipe
    }
}
```

This ensures:
- Files are always git-diffable
- Output is always pipeable
- Terminal UX is always beautiful
- No escape codes leak into storage

```
