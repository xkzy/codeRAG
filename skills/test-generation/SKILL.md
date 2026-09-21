---
name: test-generation
description: Use this skill whenever the user asks to generate tests for their codebase, find untested code paths, improve test coverage, or verify that existing tests cover critical functionality. This skill leverages codeRAG's semantic graph tools to identify untested functions, find related tests, analyze call paths that need coverage, and generate targeted test cases based on the code structure. Use this skill when the user says "write tests", "add tests", "improve coverage", "find untested code", "test this function", or "verify my tests".
---

# Test Generation & Coverage Skill

## Overview

This skill helps generate targeted tests using codeRAG's semantic graph. Instead of writing random tests, identify untested functions, find existing test coverage gaps, and generate tests based on actual call paths and data flow.

## Workflow

### Step 1: Index the project

Ensure the target codebase is indexed:

```
codergag_index_repository --json '{"project_id": "<project_id>", "path": "<repo_path>"}'
```

### Step 2: Find untested functions

```
codergag_find_entry_points(project_id="<project_id>")
```
→ Find public APIs that need test coverage

```
codergag_find_function(project_id="<project_id>", query="<name>")
```
→ Find specific functions to test

### Step 3: Find existing tests

```
codergag_find_related_tests(project_id="<project_id>", function_name="<func_name>")
```
→ Find existing tests for a function

### Step 4: Analyze call paths for test targets

```
codergag_trace_call_path(project_id="<project_id>", source_id="<entry>", target_id="<target>")
```
→ Trace the call path between entry point and target function

```
codergag_get_callees(project_id="<project_id>", function_id="<func_id>")
```
→ Find what a function calls (need to mock these in tests)

```
codergag_get_callers(project_id="<project_id>", function_id="<func_id>")
```
→ Find what calls a function (integration test entry points)

### Step 5: Generate test cases

Based on the analysis, generate tests that:
1. **Unit tests**: Test each function in isolation, mocking callees
2. **Integration tests**: Test call paths end-to-end
3. **Edge case tests**: Test error paths identified by `analyze_complexity`

## Available codeRAG Tools

| Tool | Use Case |
|------|----------|
| `codergag_find_entry_points` | Find public APIs needing tests |
| `codergag_find_function` | Find specific functions |
| `codergag_find_related_tests` | Find existing tests |
| `codergag_trace_call_path` | Trace call paths for integration tests |
| `codergag_get_callees` | Find dependencies to mock |
| `codergag_get_callers` | Find integration test entry points |
| `codergag_analyze_complexity` | Find complex code needing tests |
| `codergag_get_related_code` | Find all related code for a function |
| `codergag_find_hot_paths` | Find most-called functions (highest ROI for tests) |

## Output Template

```markdown
# Test Coverage Report: <project_name>

## Summary
- **Project**: <project_id>
- **Functions**: <total>
- **Tested**: <count> (<percentage>%)
- **Untested**: <count>
- **Hot paths untested**: <count>

## Coverage Gaps
| Function | File | Complexity | Has Tests | Priority |
|----------|------|-----------|-----------|----------|
| <name> | <path> | <score> | No | High |

## Suggested Tests

### Unit: <function_name>
```go
func Test<FunctionName>(t *testing.T) {
    // Mock callees from get_callees output
    // Test: happy path, error path, edge cases
}
```

### Integration: <call_path>
```go
// Trace from entry point to target function
// Test: end-to-end behavior
```

## Best Practices

1. **Prioritize hot paths** — tests on frequently-called functions have highest ROI
2. **Mock callees** — use `get_callees` to identify what to mock
3. **Test error paths** — `analyze_complexity` reveals branches that need tests
4. **Integration test entry points** — `get_callers` shows where to start integration tests
5. **Record test results** — use `codergag_record_validation` to track test outcomes
```

## Best Practices

1. **Start with hot paths** — highest ROI for test coverage
2. **Mock external dependencies** — use callees to identify what to mock
3. **Test error paths** — complexity analysis reveals branches needing tests
4. **Use entry points** — integration tests should start from entry points
5. **Track validation** — record test results in the graph for future reference

## References

- Test patterns: `internal/services/calls_test.go` (example tests)
- codeRAG testing tools: `codergag_record_validation`
