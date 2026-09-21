---
name: refactoring
description: Use this skill whenever the user asks to refactor code, improve code structure, reduce complexity, eliminate circular dependencies, remove dead code, or reorganize modules. This skill leverages codeRAG's semantic graph tools to analyze the impact of changes before making them, find related code that needs updating, detect circular dependencies, identify dead imports, and trace data flow to ensure refactoring preserves behavior. Use this skill when the user says "refactor", "restructure", "reduce complexity", "clean up", "improve architecture", or "reorganize code".
---

# Refactoring Skill

## Overview

This skill provides a safe, graph-driven approach to refactoring. Before changing code, analyze the impact using the semantic graph to understand what will break, what needs updating, and how to preserve behavior.

## Workflow

### Step 1: Analyze current state

```
codergag_get_architecture(project_id="<project_id>", depth=2)
```
→ Understand module structure before refactoring

```
codergag_find_circular_deps(project_id="<project_id>")
```
→ Identify circular dependencies to break during refactoring

```
codergag_find_dead_imports(project_id="<project_id>")
```
→ Find dead imports that can be removed safely

### Step 2: Analyze impact of proposed changes

```
codergag_impact_analysis(project_id="<project_id>", node_id="<node_id>", depth=3)
```
→ Find all code affected by changes to a specific node

```
codergag_get_dependents(project_id="<project_id>", node_id="<node_id>")
```
→ Find files that depend on a specific file

```
codergag_get_dependencies(project_id="<project_id>", node_id="<node_id>")
```
→ Find files that a specific file depends on

### Step 3: Trace data flow

```
codergag_trace_data_flow(project_id="<project_id>", source_id="<src>", target_id="<tgt>")
```
→ Verify data flow is preserved after refactoring

```
codergag_get_related_code(project_id="<project_id>", node_id="<node_id>")
```
→ Find all code related to a node (callers, callees, data refs)

### Step 4: Execute refactoring

1. Update the source code
2. Re-index the project: `codergag_index_repository`
3. Verify no new issues:
   - `codergag_find_circular_deps` — no new cycles introduced
   - `codergag_find_dead_imports` — no new dead imports
   - `codergag_check_docs` — docs still valid

## Available codeRAG Tools

| Tool | Use Case |
|------|----------|
| `codergag_impact_analysis` | Find all affected code |
| `codergag_get_dependents` | Find files depending on a file |
| `codergag_get_dependencies` | Find files a file depends on |
| `codergag_trace_data_flow` | Verify data flow preservation |
| `codergag_get_related_code` | Find all related code |
| `codergag_find_circular_deps` | Detect import cycles |
| `codergag_find_dead_imports` | Find dead imports |
| `codergag_get_type_hierarchy` | Find inheritance relationships |
| `codergag_check_docs` | Verify docs after refactoring |
| `codergag_get_architecture` | Understand module structure |

## Output Template

```markdown
# Refactoring Plan: <project_name>

## Current Architecture
<codergag_get_architecture output>

## Issues to Address
- Circular dependencies: <count>
- Dead imports: <count>
- High complexity functions: <count>

## Impact Analysis
### <file_or_function>
- **Dependents**: <count> files depend on this
- **Dependencies**: <count> files this depends on
- **Callers**: <list>
- **Callees**: <list>

## Refactoring Steps
1. <Step with rationale>
2. <Step>
3. <Step>

## Verification
- [ ] No new circular dependencies
- [ ] No new dead imports
- [ ] Documentation still valid
- [ ] Data flow preserved
- [ ] Tests pass
```

## Best Practices

1. **Always analyze impact first** — never refactor without `impact_analysis`
2. **Break cycles gradually** — one cycle at a time to maintain compilability
3. **Update docs** — `check_docs` catches stale references
4. **Re-index after changes** — the graph must reflect current code
5. **Record refactoring decisions** — use `codergag_record_observation` to document why
