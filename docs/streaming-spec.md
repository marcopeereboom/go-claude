# Streaming Response Specification

**Status:** Not implemented (v0.2 target)  
**Goal:** Display API responses incrementally as they arrive

## Overview

Add Server-Sent Events (SSE) streaming support to improve UX during long responses. Streaming is **opt-in** via flag, disabled by default (assume backend/non-interactive use).

## Motivation

**Pain point:** Current implementation waits for complete response before displaying anything. For complex tool use with multiple iterations, this can take 30-60 seconds with no feedback.

**Solution:** Stream response chunks as they arrive, display incrementally.

## User-Facing Behavior

### Enabling Streaming

```bash
# Enable streaming
echo "refactor database.go" | claude --stream

# Works with other flags
claude --stream --tool=write --verbosity=verbose
```

**Default:** Streaming OFF (assume non-interactive/backend use)

### Display Output

**Stream to stderr** (keeps stdout clean for `--output-file`):

```
[Streaming to stderr, output will be in stdout when complete]

Let me refactor the database code...

[Tool: read_file(database.go)]
[Tool: write_file(database.go)]
--- database.go ---
...diff shown...

The refactoring is complete. Changes made:
- Extracted connection pooling
- Added error handling
...
```

**Verbose mode shows token usage:**
```
[Streaming to stderr, output will be in stdout when complete]
[Tokens: 150 in, streaming out...]

Let me refactor...
[Tokens: 150 in, 45 out]
...
[Tokens: 150 in, 234 out, complete]
```

### Tool Execution During Stream

**When tool_use block arrives:**
1. Pause stream display
2. Show tool being called: `[Tool: write_file(path.go)]`
3. Execute tool (respecting --tool flags)
4. Show tool result if --verbosity=verbose
5. Resume streaming

**Example:**
```
Analyzing the code structure...

[Tool: read_file(database.go)]
[Verbose: Read 234 lines]

[Tool: write_file(database.go)]
--- database.go ---
...diff shown...
[Dry-run: use --tool=write to apply]

Based on the analysis, here are the changes...
```

### Interruption (Ctrl-C)

**User hits Ctrl-C during stream:**
```
Streaming response...
^C
[Interrupted - no changes saved]
[Partial request saved as: .claude/request_20260105_120000.partial.json]
```

**Behavior:**
- Stop streaming immediately
- Discard accumulated response (don't save)
- Keep request as `.partial.json` (can inspect later)
- Show clear message that nothing was saved
- Exit cleanly

**Cost:** Interrupted responses still cost tokens (API charged for partial generation)

### Stream Errors

**Network error mid-stream:**
```
Streaming response...
[Error: stream interrupted - connection lost]
[No changes saved]
[Partial request saved as: .claude/request_20260105_120000.partial.json]
```

**Behavior:**
- Treat like Ctrl-C (discard partial)
- Save request as .partial
- Exit with error

**Timeout (no chunks for 120s):**
```
Streaming response...
[Error: stream timeout - no data received for 120s]
[No changes saved]
```

**Behavior:**
- Abort stream
- Discard partial response
- Save request as .partial
- Exit with timeout error

## Storage Behavior

### Atomic Request/Response Saving

**Normal (non-streaming) flow:**
```
1. Save request_TIMESTAMP.json
2. Call API
3. Save response_TIMESTAMP.json
```

**Streaming flow:**
```
1. Save request_TIMESTAMP.partial.json (immediately)
2. Stream API response (accumulate in memory)
3. On success:
   - Rename request_TIMESTAMP.partial.json → request_TIMESTAMP.json
   - Write response_TIMESTAMP.json
4. On failure/Ctrl-C:
   - Leave request_TIMESTAMP.partial.json
   - Don't write response
```

**Why .partial:**
- Never lose user's prompt (saved immediately)
- No orphaned pairs (listRequestResponsePairs ignores .partial)
- Can inspect interrupted requests manually
- Atomic from storage perspective (pairs only exist when complete)

### Listing Conversations

**listRequestResponsePairs() ignores partials:**
```go
if strings.HasSuffix(name, ".partial.json") {
    continue // skip incomplete requests
}
```

**Cleaning up partials:**
```bash
# Manual
rm .claude/*.partial.json

# Or add flag (future)
claude --clean-partials
```

## Compatibility

### With --replay

**Works normally:**
- Replay loads saved response (complete, not streaming)
- Executes tools from saved response
- No streaming during replay

### With --output=json

**Buffer then output:**
- Stream internally (display to stderr)
- Accumulate complete response in memory
- Output complete JSON to stdout at end
- Atomic write (all or nothing)

### With --output-file

**Buffer then write:**
- Stream display to stderr (user sees progress)
- Accumulate complete response in memory
- Write to file atomically when complete
- Partial responses never written to file

**Rationale:** Atomic writes prevent corrupt output files

## Implementation Details

### API Changes

**New SSE endpoint:**
```
POST https://api.anthropic.com/v1/messages
Headers:
  anthropic-version: 2023-06-01
  x-api-key: ...
  content-type: application/json
Body:
  {
    "model": "...",
    "stream": true,  // ← enable streaming
    ...
  }

Response: text/event-stream
```

**SSE Event Format:**
```
event: message_start
data: {"type":"message_start","message":{"id":"msg_..."}}

event: content_block_delta
data: {"type":"content_block_delta","delta":{"type":"text","text":"Hello"}}

event: content_block_delta  
data: {"type":"content_block_delta","delta":{"type":"text","text":" world"}}

event: message_stop
data: {"type":"message_stop"}
```

### Code Structure

**New file: cmd/claude/streaming.go (~200 lines)**
```go
// SSE parsing and accumulation
func streamAPIResponse(url, apiKey string, req APIRequest, 
    opts *options) (*APIResponse, error)

// Parse SSE events
func parseSSEEvent(line string) (event, data string, error)

// Accumulate chunks into final response
func accumulateResponse(chunks <-chan StreamChunk) APIResponse

// Display chunk to stderr
func displayChunk(chunk StreamChunk, opts *options)

// Handle Ctrl-C during stream
func handleInterrupt(partialPath string)
```

**Changes to cmd/claude/claude.go (~50 lines)**
```go
func executeConversation(sess *session) (*conversationResult, error) {
    // Save request as .partial immediately
    partialPath := saveRequestPartial(sess.claudeDir, sess.timestamp, messages)
    
    if sess.opts.stream {
        // Streaming path
        resp, err := streamAPIResponse(...)
        if err != nil {
            // Leave .partial file, don't save response
            return nil, err
        }
        // Success: rename .partial, save response
        renamePartial(partialPath, sess.claudeDir, sess.timestamp)
        saveResponse(...)
    } else {
        // Non-streaming path (current behavior)
        ...
    }
}
```

**Changes to cmd/claude/storage.go (~20 lines)**
```go
func saveRequestPartial(dir, ts string, msgs []MessageContent) string {
    path := filepath.Join(dir, fmt.Sprintf("request_%s.partial.json", ts))
    saveJSON(path, Request{Timestamp: ts, Messages: msgs})
    return path
}

func renamePartial(partialPath, dir, ts string) error {
    finalPath := filepath.Join(dir, fmt.Sprintf("request_%s.json", ts))
    return os.Rename(partialPath, finalPath)
}

func listRequestResponsePairs(dir string) ([]string, error) {
    // ...existing code...
    if strings.HasSuffix(name, ".partial.json") {
        continue // skip partials
    }
    // ...
}
```

### Error Handling

**Stream interruption:**
```go
select {
case chunk := <-chunks:
    displayChunk(chunk)
case <-ctx.Done():
    return nil, fmt.Errorf("stream interrupted")
}
```

**Timeout:**
```go
timeout := time.After(time.Duration(opts.timeout) * time.Second)
select {
case chunk := <-chunks:
    // process
case <-timeout:
    return nil, fmt.Errorf("stream timeout after %ds", opts.timeout)
}
```

**Network errors:**
```go
resp, err := http.Get(url)
if err != nil {
    // Leave .partial file
    return nil, fmt.Errorf("stream error: %w", err)
}
```

## Flag Changes

### New Flag

```go
flag.BoolVar(&opts.stream, "stream", false,
    "stream responses (display incrementally, output to stderr)")
```

**Usage:**
```bash
claude --stream                    # enable streaming
claude --stream --verbosity=verbose # with token counts
```

**Default:** `false` (non-streaming)

**Interaction with other flags:**
- `--stream` with `--output=json` → buffers, outputs complete JSON
- `--stream` with `--output-file` → buffers, writes atomically
- `--stream` with `--replay` → replay doesn't stream (uses saved response)
- `--stream` with `--tool=none` → streams, but shows tool blocks (won't execute)

## Testing Requirements

**Unit tests:**
- SSE event parsing
- Chunk accumulation
- Partial file handling
- Response reconstruction

**Integration tests (with mock SSE server):**
- Complete stream end-to-end
- Tool execution mid-stream
- Ctrl-C interruption
- Network error mid-stream
- Timeout handling
- Multiple content blocks
- Token counting during stream

**Mock SSE server:**
```go
func mockSSEServer(t *testing.T, events []string) *httptest.Server {
    return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/event-stream")
        for _, event := range events {
            fmt.Fprintf(w, "%s\n\n", event)
            w.(http.Flusher).Flush()
        }
    }))
}
```

## Performance Considerations

### Chunk Display Rate

**Problem:** Character-by-character display is jarring

**Solution:** Buffer chunks, display when:
- Accumulated 50+ characters, OR
- 100ms elapsed since last display, OR
- Complete sentence/paragraph detected

**Implementation:**
```go
type displayBuffer struct {
    text      string
    lastFlush time.Time
}

func shouldFlush(buf *displayBuffer) bool {
    return len(buf.text) >= 50 || 
           time.Since(buf.lastFlush) >= 100*time.Millisecond
}
```

### Memory Usage

**Accumulate response in memory:**
- Typical response: 1-8k tokens = 4-32KB
- Max response: 16k tokens = ~64KB
- Negligible memory overhead

**No memory concerns** unless hitting API limits (>100k tokens)

## Migration Path

### Phase 1: Basic Streaming
- Parse SSE events
- Display to stderr
- Handle Ctrl-C
- Atomic saving with .partial

### Phase 2: Polish
- Tool execution display
- Token count display (verbose mode)
- Better chunk buffering
- Comprehensive tests

### Phase 3: Advanced (future)
- Configurable display rate
- Progress indicators
- Resume interrupted streams?
- Stream to file incrementally (opt-in)

## Open Questions

None - all answered in this spec.

## References

- Anthropic Streaming API: https://docs.anthropic.com/en/api/messages-streaming
- SSE Specification: https://html.spec.whatwg.org/multipage/server-sent-events.html
- Go SSE libraries: consider `github.com/tmaxmax/go-sse` or roll own (simple enough)

## Related Specs

- See tool-safety-spec.md for tool execution behavior
- See context.md for storage system architecture
- See testing-guide.md for test patterns
