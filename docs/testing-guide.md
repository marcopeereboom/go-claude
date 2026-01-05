# Testing Guide

**How to write effective tests for go-claude**

## Testing Philosophy

### What We Test

**Unit tests:**
- Individual functions with clear inputs/outputs
- Storage operations (save, load, list)
- Permission checks (canExecuteWrite, etc)
- Validation logic (isSafePath, validateCommand)
- Token estimation, cost calculation

**Integration tests:**
- API calls with mock server
- Tool execution end-to-end
- Replay functionality
- Git integration

**What we DON'T test:**
- External APIs (mock them)
- User interface/terminal output (too brittle)
- Filesystem implementation (trust the OS)

### Testing Priorities

**High priority (must test):**
1. Security: path traversal, command injection
2. Data integrity: request/response pairing
3. Cost control: token limits, iteration limits
4. Core functionality: save, load, replay

**Medium priority (should test):**
1. Edge cases: empty files, malformed JSON
2. Error handling: missing files, bad permissions
3. Configuration: flag parsing, defaults

**Low priority (nice to have):**
1. Output formatting
2. Help text
3. Performance benchmarks

## Test File Organization

### Naming Convention

```
cmd/claude/
├── claude.go          # main code
├── claude_test.go     # core functionality tests
├── storage.go         # storage code
├── storage_test.go    # storage tests (future split)
├── tools.go           # tool code (future)
├── tools_test.go      # tool tests (future)
└── api_test.go        # API/mock server tests
```

**Rules:**
- Test file = code file + `_test.go`
- Keep tests close to code
- Split when file > 500 lines

### Test Function Naming

```go
// Pattern: Test<FunctionName><Scenario>
func TestSaveRequest(t *testing.T)                    // basic case
func TestSaveRequestInvalidPath(t *testing.T)         // error case
func TestLoadConversationHistoryEmpty(t *testing.T)   // edge case
func TestValidateCommandWithWhitelist(t *testing.T)   // specific behavior
```

**Good names are:**
- Descriptive: what's being tested
- Specific: which scenario
- Scannable: easy to find when it fails

## Test Patterns

### 1. Table-Driven Tests

**Use for:** Testing same logic with different inputs

```go
func TestValidateCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{"allowed command", "ls -la", false},
		{"blocked command", "rm -rf /", true},
		{"pipe allowed", "ls | grep .go", false},
		{"sudo blocked", "sudo ls", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCommand(tt.command)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateCommand() error = %v, wantErr %v", 
					err, tt.wantErr)
			}
		})
	}
}
```

**Benefits:**
- Easy to add cases
- Clear structure
- Self-documenting

### 2. Temporary Directories

**Use for:** File system operations

```go
func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir() // auto-cleanup
	
	// Use tmpDir for all file operations
	err := saveRequest(tmpDir, "test", messages)
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	
	// Load it back
	loaded, err := loadRequest(tmpDir, "test")
	// ... assertions
}
```

**Why t.TempDir():**
- Auto-cleanup after test
- Isolated from other tests
- No race conditions

### 3. Mock HTTP Server

**Use for:** API testing without real API calls

```go
func TestAPICall(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request
			if r.Header.Get("x-api-key") == "" {
				t.Error("missing API key")
			}
			
			// Return canned response
			json.NewEncoder(w).Encode(APIResponse{
				Content: []ContentBlock{{Type: "text", Text: "mock"}},
			})
		}),
	)
	defer server.Close()
	
	// Override apiURL to use mock
	oldURL := apiURL
	apiURL = server.URL
	defer func() { apiURL = oldURL }()
	
	// Test your code
	resp, _, err := callAPI(...)
	// ... assertions
}
```

**Benefits:**
- No API costs
- Fast (no network)
- Reliable (no flaky failures)
- Testable error conditions

### 4. Subtests

**Use for:** Grouping related test cases

```go
func TestOptions(t *testing.T) {
	t.Run("write permission", func(t *testing.T) {
		opts := &options{tool: "write"}
		if !opts.canExecuteWrite() {
			t.Error("should allow write")
		}
	})
	
	t.Run("dry-run mode", func(t *testing.T) {
		opts := &options{tool: ""}
		if opts.canExecuteWrite() {
			t.Error("should not allow write in dry-run")
		}
	})
}
```

**Benefits:**
- Related tests grouped
- Can run subset: `go test -run TestOptions/write`
- Failures show context

## Assertion Patterns

### Basic Assertions

```go
// Error checking
if err != nil {
	t.Fatalf("unexpected error: %v", err)  // stop test
}
if err == nil {
	t.Error("expected error, got nil")    // continue test
}

// Value checking
if got != want {
	t.Errorf("got %v, want %v", got, want)
}

// Boolean checks
if !condition {
	t.Error("condition should be true")
}

// Collection length
if len(items) != 3 {
	t.Errorf("expected 3 items, got %d", len(items))
}
```

### When to Use Fatal vs Error

```go
// Use Fatal when can't continue
data, err := os.ReadFile("test.json")
if err != nil {
	t.Fatalf("setup failed: %v", err)  // can't test without data
}

// Use Error when can continue
if result.Count != 5 {
	t.Errorf("wrong count: %d", result.Count)  // check other fields too
}
if result.Name != "test" {
	t.Errorf("wrong name: %s", result.Name)
}
```

### Testing Errors

```go
// Check error occurred
err := doSomething()
if err == nil {
	t.Fatal("expected error, got nil")
}

// Check error message
if !strings.Contains(err.Error(), "not found") {
	t.Errorf("wrong error: %v", err)
}

// Check error type (if using custom errors)
var pathErr *PathError
if !errors.As(err, &pathErr) {
	t.Errorf("expected PathError, got %T", err)
}
```

## Testing Best Practices

### 1. Arrange-Act-Assert

```go
func TestSomething(t *testing.T) {
	// Arrange: set up test data
	tmpDir := t.TempDir()
	messages := []MessageContent{{...}}
	
	// Act: call function under test
	err := saveRequest(tmpDir, "test", messages)
	
	// Assert: verify results
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

### 2. Test One Thing

**Good:**
```go
func TestSaveRequest(t *testing.T) {
	// Only tests saving
}

func TestLoadRequest(t *testing.T) {
	// Only tests loading
}
```

**Bad:**
```go
func TestSaveAndLoadAndValidateAndTransform(t *testing.T) {
	// Tests too many things - hard to debug failures
}
```

### 3. Avoid Test Interdependence

**Good:**
```go
func TestA(t *testing.T) {
	tmpDir := t.TempDir()  // isolated
	// test A
}

func TestB(t *testing.T) {
	tmpDir := t.TempDir()  // isolated
	// test B
}
```

**Bad:**
```go
var sharedDir string  // state leak

func TestA(t *testing.T) {
	sharedDir = "/tmp/shared"
	// test A modifies shared state
}

func TestB(t *testing.T) {
	// test B depends on TestA running first
}
```

### 4. Use Helper Functions

```go
// Helper for creating test messages
func makeTestMessage(role, text string) MessageContent {
	return MessageContent{
		Role: role,
		Content: []ContentBlock{{Type: "text", Text: text}},
	}
}

// Helper for creating test response
func makeTestResponse(text string) APIResponse {
	return APIResponse{
		Content: []ContentBlock{{Type: "text", Text: text}},
		StopReason: "end_turn",
	}
}

// Use in tests
func TestSomething(t *testing.T) {
	msg := makeTestMessage("user", "hello")
	resp := makeTestResponse("hi")
	// ... test logic
}
```

### 5. Test Data Management

**Small data: inline**
```go
func TestParse(t *testing.T) {
	input := `{"name": "test"}`
	// use directly
}
```

**Medium data: constants**
```go
const testJSON = `{
  "messages": [...],
  "config": {...}
}`

func TestLoad(t *testing.T) {
	// use testJSON
}
```

**Large data: testdata/ directory**
```
cmd/claude/
├── testdata/
│   ├── sample_request.json
│   ├── sample_response.json
│   └── invalid_data.json
└── claude_test.go
```

```go
func TestLoadReal(t *testing.T) {
	data, _ := os.ReadFile("testdata/sample_request.json")
	// parse and test
}
```

## Coverage

### Running Coverage

```bash
# Generate coverage report
go test -coverprofile=coverage.out ./...

# View in terminal
go tool cover -func=coverage.out

# View in browser (HTML)
go tool cover -html=coverage.out
```

### Coverage Goals

**Target: 80%+ for critical code**
- Storage functions: 90%+
- Tool validation: 95%+
- Permission checks: 100%
- API client: 70%+ (mock limitations)

**Don't obsess over 100%:**
- Some error paths hard to trigger
- Some code is trivial (getters)
- Focus on high-risk areas

### What Coverage Doesn't Catch

- Logic errors (wrong algorithm, right syntax)
- Race conditions
- Integration issues
- Real-world edge cases

Coverage is necessary but not sufficient.

## Common Testing Mistakes

### 1. Testing Implementation, Not Behavior

**Bad:**
```go
func TestParseCommandUsesRegex(t *testing.T) {
	// Tests HOW it works (regex)
	if !usesRegex(parseCommand) {
		t.Error("should use regex")
	}
}
```

**Good:**
```go
func TestParseCommandValidatesWhitelist(t *testing.T) {
	// Tests WHAT it does (validates)
	err := parseCommand("rm -rf /")
	if err == nil {
		t.Error("should reject dangerous command")
	}
}
```

### 2. Brittle String Matching

**Bad:**
```go
if err.Error() != "file not found: /path/to/file" {
	t.Error("wrong error")
}
```

**Good:**
```go
if !strings.Contains(err.Error(), "not found") {
	t.Error("should mention file not found")
}
```

### 3. Not Testing Error Cases

**Bad:**
```go
func TestLoad(t *testing.T) {
	data, _ := load("file.json")  // ignores error
	// only tests happy path
}
```

**Good:**
```go
func TestLoad(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		data, err := load("valid.json")
		if err != nil {
			t.Fatal(err)
		}
		// assertions
	})
	
	t.Run("file not found", func(t *testing.T) {
		_, err := load("missing.json")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
```

### 4. Flaky Tests

**Causes:**
- Time-dependent logic
- Race conditions
- External dependencies
- Filesystem state

**Solutions:**
- Use fixed timestamps in tests
- Use proper locking
- Mock external calls
- Use t.TempDir() for isolation

## Writing New Tests

### Checklist

When adding a new function, write tests for:
- [ ] Happy path (normal operation)
- [ ] Empty inputs
- [ ] Nil/zero values
- [ ] Boundary conditions
- [ ] Error cases
- [ ] Edge cases specific to domain

### Example: Testing New Function

**Function to test:**
```go
func validateCommand(cmd string) error {
	// validates command against whitelist
}
```

**Test suite:**
```go
func TestValidateCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		// Happy path
		{"simple ls", "ls", false},
		{"ls with flags", "ls -la", false},
		
		// Whitelist
		{"cat allowed", "cat file.txt", false},
		{"grep allowed", "grep pattern", false},
		
		// Blocked
		{"rm blocked", "rm file", true},
		{"sudo blocked", "sudo ls", true},
		
		// Pipes
		{"pipe allowed", "ls | grep .go", false},
		{"pipe with blocked", "ls | rm", true},
		
		// Edge cases
		{"empty command", "", true},
		{"only spaces", "   ", true},
		{"case sensitive", "LS", true},
		
		// Injection attempts
		{"semicolon", "ls; rm -rf /", true},
		{"ampersand", "ls && rm", true},
		{"backticks", "ls `rm file`", true},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCommand(tt.command)
			if (err != nil) != tt.wantErr {
				t.Errorf("got error = %v, wantErr = %v", 
					err, tt.wantErr)
			}
		})
	}
}
```

## Debugging Test Failures

### 1. Read The Error Message

```bash
--- FAIL: TestValidateCommand/sudo_blocked (0.00s)
    tools_test.go:45: got error = <nil>, wantErr = true
```

**What it tells you:**
- Test name: `TestValidateCommand/sudo_blocked`
- Line: `tools_test.go:45`
- Problem: expected error but got nil

### 2. Add Debug Output

```go
func TestSomething(t *testing.T) {
	result := doSomething()
	t.Logf("debug: result = %#v", result)  // shows Go syntax
	
	if result != expected {
		t.Errorf("got %v, want %v", result, expected)
	}
}
```

### 3. Run Specific Test

```bash
# Run one test
go test -v -run TestValidateCommand

# Run one subtest
go test -v -run TestValidateCommand/sudo_blocked

# Run with more detail
go test -v -run TestValidateCommand 2>&1 | less
```

### 4. Use go-claude

```bash
echo "TestValidateCommand/sudo_blocked is failing, \
it's not blocking sudo commands. Debug why." | go run ./cmd/claude
```

## Summary

**Good tests:**
- Are fast (< 1 second for unit tests)
- Are isolated (no shared state)
- Are readable (clear arrange-act-assert)
- Are maintainable (don't test implementation)
- Are reliable (no flaky failures)

**Test pyramid:**
```
     /\     E2E tests (few)
    /  \
   /----\   Integration tests (some)
  /      \
 /--------\ Unit tests (many)
```

**When in doubt:**
- Write table-driven tests
- Test error cases
- Use t.TempDir() for files
- Mock external dependencies
- Keep tests simple

**Remember:**
Tests are code too. Keep them clean, readable, and well-organized.
