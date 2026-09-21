---
name: code-review
description: Use this skill whenever the user asks to review code for bugs, security vulnerabilities, code quality issues, architectural problems, or style violations. This skill leverages codeRAG's semantic graph tools to perform deep static analysis: find circular dependencies, detect security CWE patterns, check documentation coverage, trace data flow for taint analysis, identify hot paths, and analyze complexity. Use this skill when the user says "review my code", "find bugs", "security audit", "check code quality", "analyze complexity", "find vulnerabilities", or "code review".
---

# Code Review & Static Analysis Skill

## Overview

This skill provides a systematic, token-efficient approach to code review using codeRAG's semantic analysis tools. Instead of manually reading every file, leverage the graph to find specific issues: circular dependencies, security vulnerabilities, missing documentation, dead code, high-complexity functions, and data flow problems.

## Workflow

### Step 1: Index the project

Ensure the target codebase is indexed:

```
codergag_index_repository --json '{"project_id": "<project_id>", "path": "<repo_path>"}'
```

### Step 2: Architectural analysis

Find high-level issues:

```
codergag_find_circular_deps(project_id="<project_id>")
```
→ Detect import cycles that indicate architectural problems

```
codergag_get_architecture(project_id="<project_id>", depth=2)
```
→ Get a high-level overview of modules and their dependencies

```
codergag_find_dead_imports(project_id="<project_id>", limit=50)
```
→ Find imports that are used only once (likely stale imports or copy-paste errors)

### Step 3: Security analysis

```
codergag_find_vulnerabilities(project_id="<project_id>", cwe="CWE-78")
```
→ Find OS command injection vulnerabilities

```
codergag_find_vulnerabilities(project_id="<project_id>", cwe="CWE-79")
```
→ Find XSS (Cross-Site Scripting) vulnerabilities

```
codergag_find_vulnerabilities(project_id="<project_id>", cwe="CWE-89")
```
→ Find SQL injection vulnerabilities

```
codergag_audit_security(project_id="<project_id>")
```
→ Full security audit with CWE pattern matching

Commonly useful CWEs:
- `CWE-79` — XSS (Cross-Site Scripting)
- `CWE-89` — SQL Injection
- `CWE-78` — OS Command Injection
- `CWE-73` — External Control of File Name or Path
- `CWE-409` — Improper Handling of Unicode
- `CWE-352` — CSRF (Cross-Site Request Forgery)

### Step 4: Complexity analysis

```
codergag_analyze_complexity(project_id="<project_id>", function_id="<func_id>")
```
→ Get cyclomatic complexity and branch breakdown for a function

```
codergag_find_hot_paths(project_id="<project_id>", limit=20)
```
→ Find the most-called functions (performance + complexity)

### Step 5: Documentation check

```
codergag_check_docs(project_id="<project_id>")
```
→ Find identifiers referenced in docs that no longer exist, and code that has no documentation

### Step 6: Data flow analysis

```
codergag_trace_data_flow(project_id="<project_id>", source_id="<user_input_func>", target_id="<db_query_func>")
```
→ Trace data from untrusted sources to sinks (taint analysis)

```
codergag_get_related_code(project_id="<project_id>", node_id="<func_id>")
```
→ Get all code related to a function (callers, callees, data refs)

## Available codeRAG Tools

| Tool | Use Case |
|------|----------|
| `codergag_find_circular_deps` | Find import cycles |
| `codergag_find_dead_imports` | Find unused/stale imports |
| `codergag_find_hot_paths` | Find most-called functions |
| `codergag_find_vulnerabilities` | Search by CWE pattern |
| `codergag_audit_security` | Full security audit |
| `codergag_analyze_complexity` | Cyclomatic complexity analysis |
| `codergag_check_docs` | Stale doc references |
| `codergag_trace_data_flow` | Taint analysis |
| `codergag_get_architecture` | High-level module overview |
| `codergag_get_related_code` | All related code for a node |
| `codergag_find_related_tests` | Find tests for a function |

## Output Template

```markdown
# Code Review Report: <project_name>

## Summary
- **Project**: <project_id>
- **Files analyzed**: <count>
- **Issues found**: <count>
- **Security issues**: <count>
- **Complexity issues**: <count>

## Architecture
<codergag_get_architecture output>

## Circular Dependencies
<List cycles found>

## Security Findings
### Critical
- <CWE description with file:node reference>

### High
- <...>

### Medium
- <...>

## Complexity Analysis
| Function | Complexity | File |
|----------|-----------|------|
| <name> | <score> | <path:line> |

## Dead Imports
| Module | File |
|--------|------|
| <...> | <...> |

## Documentation Issues
<List stale doc references>

## Recommendations
1. <...>
2. <...>
3. <...>
```

## Best Practices

1. **Start with architecture** — understand the module structure before diving into specific files
2. **Use CWE-based searching** — targeted scans are cheaper than full audits
3. **Combine tools** — `find_related_tests` + `check_docs` together validate that documented APIs have tests
4. **Record findings as evidence** — use `codergag_record_evidence` to make issues traceable
5. **Prioritize by hot paths** — high-complexity functions on hot paths are most worth refactoring

## References

- CWE list: https://cwe.mitre.org/
- codeRAG security tools: `internal/services/security.go`
- codeRAG analysis tools: `internal/services/analysis.go`
