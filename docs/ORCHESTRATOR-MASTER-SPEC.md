# Orchestrator System Specification - Master Outline

**Version:** 0.1  
**Status:** Design Phase  
**Last Updated:** 2026-01-06

## Overview

Transform go-claude from single-turn CLI tool into autonomous orchestrator capable of multi-step execution with planning, verification, and recovery.

**Design Philosophy:**
- Non-interactive (Unix way - scriptable)
- Test-first (code + tests together)
- Incremental (each spec is independently buildable)
- Composable (packages, not monolith)

---

## Specification Structure

Each spec is a discrete, buildable unit with:
- **Prerequisites:** What must exist first
- **Deliverables:** What this spec produces
- **Testing:** How to verify it works
- **Integration Points:** How it connects to other specs

---

## PHASE 0: Foundation (Refactoring)

### SPEC-0.1: Package Structure
**File:** `specs/SPEC-0.1-package-structure.md`

**Goal:** Refactor monolithic `cmd/claude/claude.go` into clean packages

**Packages:**
```
pkg/
├── orchestrator/  # Planning, execution, state management
├── client/        # API wrapper, cost tracking
├── tools/         # Tool implementations (read_file, write_file, bash_command)
├── storage/       # Persistence (plans, config, audit)
├── capabilities/  # Capability discovery
└── display/       # Terminal UI (already exists)

cmd/
└── claude/        # Thin CLI wrapper (~100 lines)
```

**Prerequisites:** None  
**Deliverables:** Package structure, no new features  
**Dependencies:** SPEC-0.2 depends on this  
**Estimated Effort:** 2-3 days

---

### SPEC-0.2: Core Types & Interfaces
**File:** `specs/SPEC-0.2-core-types.md`

**Goal:** Define data structures and interfaces used across all specs

**Types:**
- `Plan`, `Step`, `PlanEstimate`, `PlanState`
- `Capabilities`, `CommandInfo`
- `Orchestrator` interface
- `Storage` interface
- `ToolRegistry` interface

**Prerequisites:** SPEC-0.1  
**Deliverables:** `pkg/orchestrator/types.go`, interfaces  
**Testing:** Type definitions compile  
**Estimated Effort:** 1 day

---

## PHASE 1: Capability Discovery

### SPEC-1.1: Basic Capability Discovery
**File:** `specs/SPEC-1.1-capability-discovery.md`

**Goal:** Detect what commands/tools are available on the system

**Features:**
- Check PATH for common commands (go, git, python, etc.)
- Get versions (`go version`, `git --version`)
- Detect languages (Go, Python, Node)
- Environment info (OS, arch, pwd)
- Cache results to `.claude/capabilities.json`

**API:**
```go
func DiscoverCapabilities() (*Capabilities, error)
func CheckCommand(name string) CommandInfo
func SaveCapabilities(caps *Capabilities) error
func LoadCapabilities() (*Capabilities, error)
```

**Prerequisites:** SPEC-0.1, SPEC-0.2  
**Deliverables:** `pkg/capabilities/discovery.go`  
**Testing:** Unit tests for each command check  
**CLI:** `claude --show-capabilities`  
**Estimated Effort:** 2 days

---

### SPEC-1.2: Dynamic Command Whitelist
**File:** `specs/SPEC-1.2-dynamic-whitelist.md`

**Goal:** Generate bash_command whitelist from discovered capabilities

**Features:**
- Base whitelist (always safe: ls, cat, grep, etc.)
- Add discovered tools (go, git, python, etc.)
- Per-session whitelist in `session` struct
- Replace hardcoded `allowedCommands` map

**API:**
```go
func BuildWhitelist(caps *Capabilities) map[string]bool
func ValidateCommand(cmd string, whitelist map[string]bool) error
```

**Prerequisites:** SPEC-1.1  
**Deliverables:** `pkg/tools/whitelist.go`  
**Testing:** Whitelist changes based on capabilities  
**Integration:** Update `validateCommand()` to use dynamic whitelist  
**Estimated Effort:** 1 day

---

## PHASE 2: Cost Estimation

### SPEC-2.1: Cost Estimation & Tracking
**File:** `specs/SPEC-2.1-cost-estimation.md`

**Goal:** Estimate costs before execution, track actual costs during

**Features:**
- Estimate tokens per step (heuristic: 2k input, 1k output)
- Calculate cost (Sonnet 4.5: $3/$15 per million)
- Track actual usage per step
- Compare estimated vs actual
- Budget enforcement (stop if exceeded)

**API:**
```go
func EstimateStepCost(step *Step) float64
func EstimatePlanCost(plan *Plan) *PlanEstimate
func TrackStepCost(step *Step, usage Usage) error
func CheckBudget(plan *Plan) error
```

**Prerequisites:** SPEC-0.2  
**Deliverables:** `pkg/client/costs.go`  
**Testing:** Mock API responses, verify calculations  
**CLI:** `--estimate-only` flag  
**Estimated Effort:** 2 days

---

### SPEC-2.2: Non-Interactive Approval
**File:** `specs/SPEC-2.2-approval-workflow.md`

**Goal:** User must explicitly approve plan execution (no interactive prompts)

**Workflow:**
```bash
# Step 1: Create plan (dry-run, no execution)
echo "build API" | claude --plan --tool=all
# Output: Plan saved to plan_ID.json
#         Estimated cost: $0.45
#         Run: claude --execute-plan plan_ID --tool=all

# Step 2: User reviews plan file
cat .claude/plans/plan_ID.json

# Step 3: User explicitly executes
claude --execute-plan plan_ID --tool=all
```

**Prerequisites:** SPEC-2.1  
**Deliverables:** `--plan` flag behavior, plan file format  
**Testing:** Plan created but not executed until explicit command  
**Estimated Effort:** 1 day

---

## PHASE 3: Basic Planning

### SPEC-3.1: Planning API Call
**File:** `specs/SPEC-3.1-planning-api.md`

**Goal:** Call Claude API to break task into steps

**Features:**
- Special system prompt for planning
- Include capabilities in prompt
- Parse response into `Plan` struct
- Save plan to `.claude/plans/plan_ID.json`
- Include dependencies and cost estimate

**Planning Prompt Template:**
```
You are a task planning assistant.

AVAILABLE CAPABILITIES:
- Commands: [go, git, python]
- Languages: [Go, Python]
- OS: linux/amd64

Break the user's request into 5-10 executable steps.
Include:
- dependencies: list of required commands
- missing: list of required but unavailable commands
- steps: ordered list of concrete actions
- tests: verification for each step

Format as JSON.
```

**Prerequisites:** SPEC-1.1, SPEC-2.1  
**Deliverables:** `pkg/orchestrator/planner.go`  
**Testing:** Mock API, verify plan structure  
**Estimated Effort:** 3 days

---

### SPEC-3.2: Plan Validation
**File:** `specs/SPEC-3.2-plan-validation.md`

**Goal:** Validate plan before execution

**Checks:**
- All dependencies available
- No missing critical commands
- Steps are well-formed
- Cost within budget (if specified)

**API:**
```go
func ValidatePlan(plan *Plan, caps *Capabilities) error
func CheckDependencies(plan *Plan, caps *Capabilities) []string
```

**Prerequisites:** SPEC-3.1  
**Deliverables:** `pkg/orchestrator/validator.go`  
**Testing:** Valid and invalid plan scenarios  
**Estimated Effort:** 1 day

---

### SPEC-3.3: Step Execution
**File:** `specs/SPEC-3.3-step-execution.md`

**Goal:** Execute a single step with tool support

**Features:**
- Call API with step context
- Execute tools (write_file, bash_command, etc.)
- Track tokens/cost for this step
- Save step state after completion
- Handle tool errors gracefully

**API:**
```go
func ExecuteStep(plan *Plan, step *Step, opts *Options) error
func BuildStepContext(plan *Plan, stepIndex int) string
```

**Prerequisites:** SPEC-3.1  
**Deliverables:** `pkg/orchestrator/executor.go`  
**Testing:** Execute single step, verify state  
**Estimated Effort:** 3 days

---

### SPEC-3.4: Plan Execution Loop
**File:** `specs/SPEC-3.4-execution-loop.md`

**Goal:** Execute all steps in sequence with state tracking

**Features:**
- Loop through steps 1..N
- Save plan state after each step
- Stop on failure (resumable)
- Enforce budget limit
- Track total time
- Final summary

**API:**
```go
func ExecutePlan(plan *Plan, opts *Options) error
func ResumePlan(planID string, opts *Options) error
```

**Prerequisites:** SPEC-3.3  
**Deliverables:** Complete execution loop  
**Testing:** Multi-step plan, verify all steps execute  
**CLI:** `--execute-plan plan_ID --tool=all`  
**Estimated Effort:** 2 days

---

## PHASE 4: Verification & Testing

### SPEC-4.1: Test Step Generation
**File:** `specs/SPEC-4.1-test-generation.md`

**Goal:** Planning includes verification steps for each code step

**Planning Enhancement:**
- For each code-creating step, add verification step
- Types: "compile", "unit", "integration", "run"
- Example: Step 2 creates main.go → Step 3 runs `go build`

**Test Types:**
```go
type Test struct {
    Type         string // "compile", "unit", "run"
    Command      string // "go test ./..."
    ExpectedExit int    // 0
    Timeout      int    // seconds
}
```

**Prerequisites:** SPEC-3.1  
**Deliverables:** Enhanced planning prompt  
**Testing:** Plans include test steps  
**Estimated Effort:** 1 day

---

### SPEC-4.2: Test Execution
**File:** `specs/SPEC-4.2-test-execution.md`

**Goal:** Run verification tests after each step

**Features:**
- Execute test commands via bash_command
- Check exit codes
- Collect output
- Fail step if test fails
- Retry logic (optional)

**API:**
```go
func RunTest(test Test) error
func VerifyStep(step *Step) error
```

**Prerequisites:** SPEC-4.1, SPEC-3.3  
**Deliverables:** `pkg/orchestrator/verifier.go`  
**Testing:** Failing tests stop execution  
**Estimated Effort:** 2 days

---

## PHASE 5: Robustness

### SPEC-5.1: Git Integration (Auto-Commit)
**File:** `specs/SPEC-5.1-git-integration.md`

**Goal:** Auto-commit before each step for rollback capability

**Features:**
- Check if in git repo
- Stage all changes (`git add -A`)
- Commit with message: "pre-step-N: <description>"
- Store commit hash in step
- `--no-git` flag to disable

**Prerequisites:** SPEC-3.3  
**Deliverables:** `pkg/orchestrator/git.go`  
**Testing:** Commits created before each step  
**Depends on:** `specs/tool-safety-spec.md` (git integration section)  
**Estimated Effort:** 2 days

---

### SPEC-5.2: Rollback on Failure
**File:** `specs/SPEC-5.2-rollback.md`

**Goal:** Undo changes when step fails

**Features:**
- `--rollback` flag
- Use git to reset to pre-step commit
- Remove entries from audit log
- Mark plan as "rolled back"

**API:**
```go
func RollbackToStep(plan *Plan, stepIndex int) error
func RollbackPlan(planID string) error
```

**Prerequisites:** SPEC-5.1  
**Deliverables:** Rollback implementation  
**Testing:** Fail a step, rollback, verify state  
**CLI:** `claude --rollback plan_ID [--to-step N]`  
**Estimated Effort:** 2 days

---

### SPEC-5.3: Error Recovery & Retry
**File:** `specs/SPEC-5.3-error-recovery.md`

**Goal:** Retry failed steps with error context

**Features:**
- Detect recoverable vs fatal errors
- Retry up to N times (default: 1)
- Pass error context to Claude on retry
- Exponential backoff (optional)

**API:**
```go
func IsRecoverableError(err error) bool
func RetryStep(plan *Plan, step *Step, attempt int) error
```

**Prerequisites:** SPEC-3.3  
**Deliverables:** Retry logic in executor  
**Testing:** Simulate failures, verify retries  
**Estimated Effort:** 2 days

---

### SPEC-5.4: State Machine
**File:** `specs/SPEC-5.4-state-machine.md`

**Goal:** Formal state transitions for plans

**States:**
```
CREATED → VALIDATED → APPROVED → EXECUTING → COMPLETE
                   ↓                      ↓
                REJECTED              FAILED → ROLLED_BACK
```

**API:**
```go
func TransitionState(plan *Plan, newState PlanState) error
func CanTransition(from, to PlanState) bool
```

**Prerequisites:** SPEC-0.2  
**Deliverables:** `pkg/orchestrator/state.go`  
**Testing:** Invalid transitions rejected  
**Estimated Effort:** 1 day

---

## PHASE 6: Advanced Features

### SPEC-6.1: Partial Execution
**File:** `specs/SPEC-6.1-partial-execution.md`

**Goal:** Execute subset of steps

**Features:**
- `--steps 2-4` execute only steps 2,3,4
- `--skip-steps 3,5` skip specific steps
- `--from-step N` start from step N
- `--dry-run-step N` simulate single step

**Prerequisites:** SPEC-3.4  
**Deliverables:** Step filtering logic  
**Testing:** Various step ranges  
**Estimated Effort:** 1 day

---

### SPEC-6.2: Parallelization
**File:** `specs/SPEC-6.2-parallel-execution.md`

**Goal:** Run independent steps concurrently

**Features:**
- Dependency graph (DependsOn field)
- Detect independent steps
- Goroutine pool for parallel execution
- Aggregate results

**API:**
```go
func BuildDependencyGraph(plan *Plan) *Graph
func ExecuteParallel(plan *Plan, maxConcurrency int) error
```

**Prerequisites:** SPEC-3.4  
**Deliverables:** Parallel executor  
**Testing:** Independent steps run concurrently  
**Estimated Effort:** 3-4 days

---

### SPEC-6.3: Streaming Progress
**File:** `specs/SPEC-6.3-streaming.md`

**Goal:** Show real-time progress during execution

**Features:**
- Progress bar per step
- Token streaming from API (SSE)
- Live output display
- Time estimates

**Prerequisites:** SPEC-3.4  
**Deliverables:** `pkg/display/progress.go`  
**Testing:** Visual verification  
**CLI:** `--progress` flag  
**Estimated Effort:** 2 days

---

### SPEC-6.4: Plan Templates
**File:** `specs/SPEC-6.4-templates.md`

**Goal:** Pre-defined plan templates for common tasks

**Templates:**
- `go-rest-api`
- `python-cli`
- `react-app`
- `rust-binary`

**Format:**
```json
{
  "template": "go-rest-api",
  "variables": {
    "project_name": "{{ .ProjectName }}",
    "port": "{{ .Port | default 8080 }}"
  },
  "steps": [...]
}
```

**Prerequisites:** SPEC-3.1  
**Deliverables:** Template engine, built-in templates  
**Testing:** Template rendering  
**CLI:** `claude --template go-rest-api`  
**Estimated Effort:** 3 days

---

### SPEC-6.5: Observability
**File:** `specs/SPEC-6.5-observability.md`

**Goal:** Debug and trace plan execution

**Features:**
- `--trace` logs all API calls with prompts/responses
- `--debug-step N` shows context for specific step
- `--show-context plan_ID` dumps full context
- Structured logging (JSON lines)
- Trace IDs across requests

**Prerequisites:** SPEC-3.4  
**Deliverables:** `pkg/observability/tracing.go`  
**Testing:** Logs written correctly  
**Estimated Effort:** 2 days

---

## Implementation Roadmap

### Month 1: Foundation + Basic Planning
- Week 1: SPEC-0.1, SPEC-0.2 (refactoring)
- Week 2: SPEC-1.1, SPEC-1.2 (capabilities)
- Week 3: SPEC-2.1, SPEC-2.2 (cost estimation)
- Week 4: SPEC-3.1, SPEC-3.2 (basic planning)

### Month 2: Execution + Verification
- Week 5: SPEC-3.3, SPEC-3.4 (execution loop)
- Week 6: SPEC-4.1, SPEC-4.2 (testing)
- Week 7: SPEC-5.1, SPEC-5.2 (git + rollback)
- Week 8: SPEC-5.3, SPEC-5.4 (recovery + state machine)

### Month 3: Advanced Features
- Week 9-10: SPEC-6.1, SPEC-6.2 (partial + parallel)
- Week 11: SPEC-6.3 (streaming)
- Week 12: SPEC-6.4, SPEC-6.5 (templates + observability)

---

## Spec Dependencies Graph

```
SPEC-0.1 (package structure)
    ↓
SPEC-0.2 (core types)
    ↓
    ├─→ SPEC-1.1 (capability discovery)
    │       ↓
    │   SPEC-1.2 (dynamic whitelist)
    │
    ├─→ SPEC-2.1 (cost estimation)
    │       ↓
    │   SPEC-2.2 (approval workflow)
    │
    └─→ SPEC-3.1 (planning API)
            ↓
        SPEC-3.2 (validation)
            ↓
        SPEC-3.3 (step execution) ←── SPEC-4.1 (test generation)
            ↓                           ↓
        SPEC-3.4 (execution loop)   SPEC-4.2 (test execution)
            ↓                           ↓
            ├─→ SPEC-5.1 (git)      SPEC-5.3 (retry)
            │       ↓
            │   SPEC-5.2 (rollback)
            │
            └─→ SPEC-6.1 (partial exec)
                SPEC-6.2 (parallel)
                SPEC-6.3 (streaming)
                SPEC-6.4 (templates)
                SPEC-6.5 (observability)
```

---

## Testing Strategy

Each spec must include:
1. **Unit Tests** - Test functions in isolation
2. **Integration Tests** - Test spec working with dependencies
3. **End-to-End Tests** - CLI invocation → expected result
4. **Mock Strategy** - How to mock API calls, filesystem, etc.

---

## Questions for Refinement

Before implementing each spec, answer:
1. What existing specs does this depend on?
2. What's the success criteria?
3. How do we test this in isolation?
4. What's the migration path from current code?
5. What can go wrong and how do we handle it?

---

## Next Steps

1. Review this outline - add/remove/reorder specs
2. Start with SPEC-0.1 (package structure) - detailed design
3. Implement SPEC-0.1, test, merge
4. Move to SPEC-0.2, repeat

Each spec becomes its own markdown file in `specs/` directory with full implementation details.
