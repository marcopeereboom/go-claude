# Development Workflow

**How to develop go-claude using go-claude itself**

## Core Principle

We use go-claude to build go-claude. This ensures:
- Tool actually works for real development
- We feel the pain points immediately
- Dogfooding catches bugs early
- Natural workflow emerges

## Session Setup

### Starting a Development Session

**1. Feed context to go-claude:**
```bash
cat docs/context.md - | go run ./cmd/claude
# Then type your task
```

**2. Or use it directly:**
```bash
echo "add validation to bash_command whitelist" | go run ./cmd/claude
```

**3. Review dry-run output, then replay:**
```bash
go run ./cmd/claude --replay="" --tool=write
```

### Maintaining Context

**When to update context.md:**
- After implementing a feature (update "What Works")
- After making design decision (add to "Design Decisions")
- When discovering a gotcha (add to "Important Details")
- When changing architecture (update diagrams/structure)

**What NOT to put in context.md:**
- Implementation details (that's what code is for)
- Temporary debugging notes
- Personal reminders
- Completed TODOs (move to git commit message)

## Git Workflow

### Commits

**Before starting work:**
```bash
git status  # ensure clean
git pull    # get latest
```

**During development:**
```bash
# go-claude will auto-commit before tool execution
# These are safety commits: "pre-tool: write_file at TIMESTAMP"
# Keep them - they enable rollback
```

**After feature complete:**
```bash
git add -A
git commit -m "feat: descriptive message

- What changed
- Why it changed
- Any breaking changes"
```

**Commit message style:**
- `feat:` new feature
- `fix:` bug fix
- `refactor:` code improvement, no behavior change
- `test:` adding/fixing tests
- `docs:` documentation only
- `chore:` maintenance (deps, tools, etc)

### Branches

**For experiments:**
```bash
git checkout -b experiment/feature-name
# try things
# if works: merge
# if fails: delete branch
```

**For main development:**
- Work on `main` for now (small team/solo)
- Create branch only for risky changes
- Keep commits atomic and revertible

## Testing Workflow

### Write Tests First (When Reasonable)

**For new functions:**
```bash
echo "write tests for validateCommand function" | go run ./cmd/claude
# Review tests
go run ./cmd/claude --replay="" --tool=write
go test ./...
# If fail, fix function
```

**For bug fixes:**
```bash
# Write failing test that demonstrates bug
echo "add test for bug: whitelist bypass with pipes" | go run ./cmd/claude
# Apply test
go test ./...  # should fail
# Fix bug
echo "fix whitelist bypass in bash_command" | go run ./cmd/claude
# Apply fix
go test ./...  # should pass
```

### Running Tests

**All tests:**
```bash
go test -v ./...
```

**Specific package:**
```bash
cd cmd/claude
go test -v
```

**Specific test:**
```bash
go test -v -run TestBashCommandWhitelist
```

**With coverage:**
```bash
go test -cover ./...
```

## Prompting go-claude

### Effective Prompts

**Good prompts are:**
- Specific: "add timeout to bash_command" not "improve tools"
- Contextual: mention relevant files/functions
- Scoped: one feature at a time
- Clear: state expected behavior

**Examples:**

✓ **Good:**
```bash
echo "add 30 second timeout to executeBashCommand, \
return error with partial output if exceeded" | go run ./cmd/claude
```

✗ **Bad:**
```bash
echo "make bash better" | go run ./cmd/claude
```

✓ **Good:**
```bash
echo "refactor validateCommand to use table-driven approach, \
see parseFlags for example pattern" | go run ./cmd/claude
```

✗ **Bad:**
```bash
echo "clean up code" | go run ./cmd/claude
```

### Multi-step Work

**For complex features:**
1. Break into steps
2. One prompt per step
3. Test after each step
4. Update context.md when step complete

**Example: Adding bash_command tool**
```bash
# Step 1: Define tool schema
echo "add bash_command tool definition to getTools, \
see tool-safety-spec.md for schema" | go run ./cmd/claude

# Step 2: Add validation
echo "implement validateCommand with whitelist from spec" | go run ./cmd/claude

# Step 3: Add execution
echo "implement executeBashCommand with timeout and error handling" | go run ./cmd/claude

# Step 4: Add tests
echo "add comprehensive tests for bash_command" | go run ./cmd/claude
```

## When Claude Gets It Wrong

### Reviewing Changes

**Before applying:**
- Read the diffs carefully
- Check for logic errors
- Verify it matches your intent
- Look for edge cases

**If wrong:**
```bash
# Don't apply, revise prompt
echo "the previous approach had X problem, instead do Y" | go run ./cmd/claude
```

### Fixing Bad Code

**If already applied:**
```bash
# Option 1: Ask Claude to fix
echo "the bash_command validation is too permissive, \
block commands with sudo or &&" | go run ./cmd/claude

# Option 2: Fix manually and commit
vim cmd/claude/tools.go
git add -A && git commit -m "fix: tighten bash validation"

# Option 3: Rollback and retry
go run ./cmd/claude --rollback
echo "different approach: ..." | go run ./cmd/claude
```

## File Organization

### Where Things Go

**Code:**
```
cmd/claude/
├── claude.go      # Main CLI, flags, run()
├── storage.go     # Request/response file management
├── tools.go       # Tool definitions and execution
├── api.go         # API client (future split)
└── *_test.go      # Tests alongside code
```

**Documentation:**
```
docs/
├── context.md              # Active development state
├── tool-safety-spec.md     # Feature specifications
├── development-workflow.md # This file
└── *.md                    # Other guides
```

**When to split files:**
- File > 1000 lines → consider split
- Clear domain boundary → make new file
- Related functions → keep together

## Performance & Cost

### Token Management

**Current conversation token usage:**
- Check periodically: look at response headers
- At ~150k tokens: wrap up and start fresh
- Feed context.md to new session

**API cost optimization:**
- Use `--replay` to avoid redundant API calls
- Dry-run first, inspect, then execute
- Don't repeatedly ask same question
- Keep prompts focused

### Development Speed

**Fast iteration:**
```bash
# Terminal 1: test watcher
while true; do
  go test ./... && echo "✓ pass" || echo "✗ fail"
  sleep 2
done

# Terminal 2: development
echo "fix failing test" | go run ./cmd/claude
go run ./cmd/claude --replay="" --tool=write
```

## Debugging

### When Tests Fail

**1. Read the error carefully**
```bash
go test -v ./... | less
# Look for actual vs expected
# Find which assertion failed
```

**2. Run specific test with more detail**
```bash
go test -v -run TestSpecificThing
```

**3. Add debug output**
```bash
# In test:
t.Logf("debug: value=%v", someValue)
```

**4. Use go-claude to fix**
```bash
echo "TestBashCommand is failing with 'exit code 1', \
debug why command validation is rejecting 'ls -la'" | go run ./cmd/claude
```

### When Code Doesn't Work

**1. Check assumptions**
- Is the file where you think it is?
- Are permissions correct?
- Is the API key set?

**2. Add logging**
```bash
echo "add debug logging to validateCommand showing \
which rule rejected the command" | go run ./cmd/claude
```

**3. Use verbose mode**
```bash
go run ./cmd/claude --verbosity=debug
```

## Releasing

**Not yet defined - we're pre-1.0**

When ready:
- Semantic versioning (0.1.0, 0.2.0, ...)
- Tag releases in git
- Update CHANGELOG.md
- Build binaries for distribution

## Summary

**The loop:**
1. Write prompt describing change
2. Review dry-run output
3. Apply with --replay --tool=write
4. Run tests
5. If pass: commit, update context.md
6. If fail: fix and repeat

**Key habits:**
- Use go-claude for go-claude development
- Test after every change
- Update context.md after features
- Keep commits atomic
- Trust but verify AI output

**When stuck:**
- Read context.md
- Check tool-safety-spec.md
- Look at existing tests for patterns
- Ask go-claude for help
- Take a break
