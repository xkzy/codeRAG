---
name: reverse-engineer
description: Use this skill whenever the user needs to reverse engineer binary code, compiled programs, assembly code, or firmware — especially when they want to understand what a binary does, extract its logic, or convert it into a high-level language like C, Python, Go, or Rust. This skill leverages codeRAG's semantic graph tools to analyze code structure cheaply (instead of dumping expensive raw assembly to the LLM), uses RetDec for automated decompilation to C as a first pass, traces call graphs and data flow through the graph, and guides the user through recording hypotheses with evidence. Use this skill when the user says "reverse engineer", "decompile", "translate to C", "convert binary to source", "understand what this binary does", "analyze firmware", or any reverse engineering task. Do not use for simple disassembler output reading — use codeRAG's get_callees/get_callers/codergag_get_related_code tools directly for that.
---

# Reverse Engineering & Language Conversion Skill

## Overview

This skill provides a structured, token-efficient workflow for reverse engineering binaries and converting the analysis into a target programming language. Instead of feeding raw assembly to an LLM (which costs thousands of tokens per function), this workflow leverages:

1. **RetDec decompiler** — produces decompiled C automatically (no LLM cost for initial pass)
2. **codeRAG semantic graph** — stores structured analysis (call graphs, data flow, cross-refs) as graph nodes/edges
3. **Semantic tools** — query the graph for specific information instead of dumping everything

## Prerequisites

Before starting, ensure the binary has been indexed by codeRAG. Required tools:

- `codergag_index_repository` — index the binary's source or the binary itself
- `codergag_get_subsystem` — check for existing reverse engineering infrastructure
- `codergag_find_symbol` — find functions by name

### Check if binary is indexed

```
codergag_get_subsystem(project_id="<project_id>", name="reverse")
```

If the binary isn't indexed yet, the user needs to run:
```
codergag_index_binary --json '{"binary_id": "<id>", "path": "<path>", "sha256": "<hash>", "tool": "retdec", "functions": [...]}'
```

## Workflow

### Step 1: Decompile with RetDec

Use the RetDec adapter (`adapters/retdec`) to convert the binary to decompiled C. RetDec can be run via:

- **Online**: `https://retdec.com/decompilation/` (REST API available)
- **Locally**: Install from `https://github.com/avast/retdec`
- **Docker**: `docker run -v $PWD:/mount retdec-decompiler <binary>`

The RetDec output includes:
- Decompiled C code per function (`decompiler_output` field)
- Function metadata: addresses, sizes, call targets, strings
- Basic blocks with instructions (if available)

Parse the RetDec JSON output into `NormalizedBinary` format using the `RetDecAdapter`.

### Step 2: Import into codeRAG graph

```
codergag_index_binary --json '{"binary_id": "<id>", "path": "<path>", "sha256": "<hash>", "tool": "retdec", "functions": [...]}'
```

This stores:
- `Binary` nodes (one per binary)
- `BinaryFunction` nodes (one per function)
- `BasicBlock`, `Instruction` nodes
- `DECOMPILED_AS` edges (function → decompiler output)
- `CALLS` edges (call graph)
- `REFERENCES` edges (function → strings)

**Token safety**: Decompiler output is automatically truncated to 50,000 characters during import to prevent token explosion from complex functions. The full output can be retrieved from the original source if needed. If `truncated` is true on a `DecompilerOutput` node, the output was cut short.

### Step 3: Query the graph for structural understanding

Instead of dumping all assembly, query specific information:

```
codergag_get_callees(project_id="<project_id>", function_id="<func_node_id>")
```
→ Get what this function calls (transitive call graph)

```
codergag_get_callers(project_id="<project_id>", function_id="<func_node_id>")
```
→ Get what calls this function (entry points, callers)

```
codergag_trace_call_path(project_id="<project_id>", source_id="<entry>", target_id="<func>")
```
→ Trace call path from entry point to target function

```
codergag_get_related_code(project_id="<project_id>", node_id="<func_node_id>")
```
→ Get all related code (data refs, code refs, callers, callees)

```
codergag_get_evidence(project_id="<project_id>", subject_id="<func_node_id>")
```
→ Get existing hypotheses and evidence for this function

```
codergag_get_hypotheses(project_id="<project_id>", subject_id="<func_node_id>")
```
→ Get competing hypotheses about what this function does

### Step 4: Record hypotheses with evidence

As you reverse engineer, record findings as hypotheses with supporting/contradicting evidence. This keeps analysis stateful and allows future sessions to resume:

```
codergag_record_re_hypothesis --json '{"id": "<id>", "subject_id": "<func_id>", "claim": "<what this function does>", "confidence": 0.8, "status": "active", "evidence_for": [...], "evidence_against": [...]}'
```

Record observations:
```
codergag_record_observation --json '{"agent": "<name>", "confidence": 0.9, "description": "<what was observed>", "method": "<static|dynamic|manual>"}'
```

### Step 4b: Auto-intercept for LLM looping/hallucination

After recording hypotheses, check for LLM analysis integrity issues:

```
codergag_check_interception --json '{"project_id": "<id>", "subject_id": "<func_id>"}'
```

This tool automatically detects:
- **Looping**: Same claim recorded multiple times (possible analysis repetition)
- **Hallucination**: Contradictory high-confidence hypotheses, unverified high-confidence evidence, or hypotheses without supporting evidence
- **Stale claims**: Too many hypotheses recorded in a short time window (30s)

If `loop_detected` or `hallucination_risk` is true, pause and:
1. Re-check against decompiled C output or original binary
2. Cross-reference with `codergag_get_evidence` for existing findings
3. Use `codergag_trace_data_flow` to verify data flow claims

### Step 5: Convert to target language

After understanding the function's purpose, data flow, and call graph, convert the decompiled C (or raw assembly if no C available) into the target language.

**Token-saving technique**: Only convert the functions that matter. Use the call graph to identify the relevant subgraph, and only send that subset to the LLM for language conversion.

For each function to convert:
1. Get its decompiled C from `DECOMPILED_AS` edge neighbor
2. Get its callees and their signatures
3. Get data flow with `codergag_trace_data_flow`
4. Convert the C pseudocode to the target language

### Step 6: Validate the reconstruction

```
codergag_record_validation --json '{"binary_function_id": "<bf_id>", "implementation_id": "<src_fn_id>", "test_name": "<test>", "status": "passed|failed", "confidence": 0.9, "method": "differential"}'
```

This records the equivalence between binary and source functions, allowing future lookups.

## Available codeRAG Tools (Token-Efficient)

### Small-Model Offloading Tools
When Ollama or another small-model provider is configured (`llm.enabled: true` in config), use these tools to offload simple tasks to cheap models like phi3, gemma2, or llama3:

- `classify_task` - Determines if a task is simple enough for a small model. Use before sending queries to decide routing.
- `small_ask` - Sends a simple query directly to the small model. Use for questions like "what is this function name?" or "list all exported symbols".

Workflow:
1. Run `classify_task` with the task description
2. If `can_offload` is true, use `small_ask` for the actual query
3. If `can_offload` is false, use the main model with full codeRAG tools

This can reduce token costs by 10-90x for simple operations like:
- Function naming suggestions
- Pattern matching across binaries
- Simple categorization tasks
- Basic string/symbol extraction

| Tool | Use Case | Token Savings |
|------|----------|---------------|
| `codergag_find_function` | Find function by name | Avoid scanning entire disassembly |
| `codergag_find_symbol` | Find any symbol (class, struct, var) | Targeted lookup vs. grep |
| `codergag_get_callees` | Get called functions (transitive) | Call graph from graph, not assembly |
| `codergag_get_callers` | Get calling functions | Reverse call graph |
| `codergag_trace_call_path` | Trace call path between two functions | Shortest path, not all paths |
| `codergag_trace_data_flow` | Trace data flow between functions | Understand variable passing |
| `codergag_get_related_code` | All code related to a node | Comprehensive but targeted |
| `codergag_get_type_hierarchy` | Inheritance/subtype relationships | C++ vtable/RTTI analysis |
| `codergag_find_related_tests` | Find tests for a function | Validate reconstruction |
| `codergag_get_evidence` | Get evidence for a hypothesis | Build on prior analysis |
| `codergag_get_hypotheses` | Get competing hypotheses | Avoid re-analyzing |
| `codergag_resolve_reference` | Resolve stable IDs to current locations | Handle code changes |

## Output Template

Always produce this structure:

```markdown
# Reverse Engineering Report: <binary_name>

## Summary
- **Binary**: <path> (SHA256: <hash>)
- **Tool**: RetDec + codeRAG semantic graph
- **Target language**: <C | Python | Go | Rust | ...>
- **Functions analyzed**: <count>
- **Key hypotheses**: <count>

## Architecture Overview
<Use codergag_get_architecture to get a 1-paragraph summary>

## Entry Points
<List of functions matching main/init/handler/on_*/handle_*>

## Call Graph (relevant subset)
<For each entry point: get_callees with depth-limited traversal>

## Key Functions

### <function_name> (<address>)
- **Decompiled C**:
```c
<decompiled code from DECOMPILED_AS edge>
```
- **Purpose** (hypothesis): <what this function does>
- **Callees**: <list>
- **Callers**: <list>
- **Data flow**: <codergag_trace_data_flow results>

## Conversion to <target_language>

```<target_language>
<converted code>
```

## Hypotheses & Evidence
| Hypothesis | Confidence | Evidence For | Evidence Against |
|-----------|-----------|-------------|-----------------|
| ... | ... | ... | ... |

## Validation
| Test | Status | Confidence |
|------|--------|-----------|
| ... | ... | ... |
```

## Language Conversion Guide

### C → Python
- Preserve algorithmic logic
- Replace manual memory management with Python constructs
- Replace pointer arithmetic with list/dict operations where appropriate
- Use `ctypes` for low-level operations if needed
- Add type hints for clarity

### C → Go
- Map C types: `int` → `int`, `char*` → `string`, `void*` → `unsafe.Pointer`
- Replace `malloc/free` with Go allocation
- Use Go slices instead of pointer + length
- Preserve control flow structure

### C → Rust
- Map C types to Rust equivalents
- Use `unsafe` blocks for raw pointer operations
- Replace `malloc/free` with `Box` and `Drop`
- Use `enum` for state machines
- Add proper error handling with `Result`

## Scripts

This skill bundles a script in `scripts/`:

- **`scripts/run_retdec.sh`** — Runs RetDec decompiler on a binary and outputs JSON. Usage: `./scripts/run_retdec.sh <binary_path> [output_dir]`. Tries local RetDec first, falls back to Docker.

After running the script, parse the output JSON using the `RetDecAdapter` from `adapters/retdec/adapter.go`.

## Test Cases

When evaluating this skill, use these prompts:

1. **Binary analysis**: "Reverse engineer this firmware binary at /samples/firmware.bin — I need to understand the communication protocol between the main loop and the radio module."
2. **Language conversion**: "Convert the decompiled function at 0x401238 into Rust. I have its RetDec output already imported into codeRAG project 'firmware_analysis'."
3. **Hypothesis tracking**: "I reversed the USB descriptor handler in firmware.bin. The function at 0x405600 seems to validate device capabilities but I'm not sure about the control transfer flow. Record this as a hypothesis with the evidence from get_callers and get_callees."

## Best Practices

1. **Start broad, then narrow**: Use Architecture Overview to understand the binary at a high level before diving into specific functions
2. **Always record hypotheses**: Even partial hypotheses help future analysis sessions resume quickly
3. **Use evidence references**: Link observations to evidence nodes so the analysis is traceable
4. **Validate early**: Use `find_related_tests` to find existing tests that can validate your reconstruction
5. **Map to source when available**: If source code exists in the graph, use `codergag_find_related_code` to find equivalent functions

## References

- RetDec documentation: https://retdec.readthedocs.io/
- codeRAG semantic tools: see codergag MCP handlers
- Reverse engineering workflow: `internal/services/reverse.go`
