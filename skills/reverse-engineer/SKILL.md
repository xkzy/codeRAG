---
name: reverse-engineer
description: Use this skill whenever the user needs to reverse engineer binary code, compiled programs, assembly code, or firmware — especially when they want to understand what a binary does, extract its logic, or convert it into a high-level language like C, Python, Go, or Rust. This skill leverages codeRAG's semantic graph tools to analyze code structure cheaply (instead of dumping expensive raw assembly to the LLM), uses RetDec for automated decompilation to C as a first pass, traces call graphs and data flow through the graph, and guides the user through recording hypotheses with evidence. Use this skill when the user says "reverse engineer", "decompile", "translate to C", "convert binary to source", "understand what this binary does", "analyze firmware", "port this binary to Rust", or any reverse engineering task. Do not use for simple disassembler output reading — use codeRAG's get_callees/get_callers/codergag_get_related_code tools directly for that.
---

# Reverse Engineering & Language Porting Skill

## Overview

This skill provides a structured, token-efficient workflow for reverse engineering binaries and converting the analysis into a target programming language. Instead of feeding raw assembly to an LLM (which costs thousands of tokens per function), this workflow leverages:

1. **Decompiler adapters** (RetDec, Ghidra, IDA, Binary Ninja) — produces decompiled C automatically
2. **codeRAG persistent knowledge graph** — stores structured analysis (call graphs, CFG, data flow, cross-refs) as graph nodes/edges
3. **Lazy analysis** — CFG, data-flow, and call graphs are computed on-demand and cached
4. **Semantic porting tools** — track semantic mappings and known differences between binary and ported code
5. **Differential verification** — compare binary vs ported output to validate behavioral equivalence

## Core Principle

> **Do expensive reverse engineering once, persist the knowledge, incrementally refine it, and let future agents consume the accumulated knowledge instead of rediscovering the binary from scratch.**

## Workflow

### Phase 1: Index Binary

```
codergag_index_binary --json '{
  "binary_id": "<id>",
  "path": "<path>",
  "sha256": "<hash>",
  "tool": "retdec",
  "functions": [...]
}'
```

This stores:
- `Binary` nodes (one per binary)
- `BinaryFunction` nodes (one per function with address, name, size)
- `BasicBlock`, `Instruction` nodes
- `DECOMPILED_AS` edges (function → decompiler output)
- `CALLS` edges (direct call graph)
- `BRANCHES_TO` edges (control flow)
- `REFERENCES` edges (function → strings)
- `REFERENCES_DATA` edges (data access)

**Token safety**: Decompiler output is automatically truncated to 50,000 characters during import.

### Phase 2: Lazy Analysis (On-Demand)

Query specific analysis without triggering full binary analysis:

```
codergag_get_cfg --project_id <id> --binary_id <bin> --function_address <addr>
```
→ Returns control flow graph, basic blocks, branch edges

```
codergag_get_data_flow --project_id <id> --binary_id <bin> --function_address <addr>
```
→ Returns arguments, return values, global reads/writes, register usage

```
codergag_get_call_graph --project_id <id> --binary_id <bin>
```
→ Returns full call graph with callers/callees counts

```
codergag_get_function_facts --project_id <id> --binary_id <bin> --function_address <addr>
```
→ Returns known facts, hypotheses, caller/callee counts

### Phase 3: Prepare Reverse Engineering Context

For complex tasks, use the context engine that assembles all relevant information:

```
codergag_prepare_reverse_engineering_context --project_id <id> --question "<task>" --target "<bin>|<addr>"
```

This returns:
- Function info (address, name, size, stable_id)
- Facts (strings, constants, data refs)
- Hypotheses with confidence
- CFG (if requested)
- Data flow (if requested)
- Callers and callees

### Phase 4: Record Hypotheses with Evidence

As you reverse engineer, record findings as hypotheses with supporting/contradicting evidence:

```
codergag_record_re_hypothesis --json '{
  "id": "<id>",
  "subject_id": "<func_id>",
  "claim": "<what this function does>",
  "confidence": 0.8,
  "status": "active",
  "evidence_for": [...],
  "evidence_against": [...]
}'
```

### Phase 5: Semantic Porting

Record semantic mappings to track how binary constructs map to target language:

```
codergag_record_semantic_mapping --json '{
  "project_id": "<id>",
  "binary_id": "<bin>",
  "function_address": "<addr>",
  "source_construct": "32-bit signed arithmetic",
  "semantic_meaning": "wraparound semantics required",
  "target_construct": "int32_t in Rust",
  "translation_rule": "use i32 with wrapping_* methods",
  "compatibility_issue": "overflow detection differs",
  "confidence": 0.85
}'
```

Record porting decisions:

```
codergag_record_porting_decision --project_id <id> --binary_id <bin> --function_address <addr> --language rust --original_construct "malloc/free" --target_construct "Box::new/Drop" --reason "Rust memory model" --confidence 0.9
```

### Phase 6: Track Known Differences

When behavioral differences are discovered:

```
codergag_record_known_difference --project_id <id> --binary_id <bin> --function_address <addr> --diff_type "integer_overflow" --description "signed conversion causes different wraparound" --severity major --impact "may cause rare bugs in production"
```

### Phase 7: Differential Verification

After porting, validate behavioral equivalence:

```
codergag_record_verification_test --json '{
  "project_id": "<id>",
  "binary_id": "<bin>",
  "function_address": "<addr>",
  "test_name": "test_parse_packet_valid",
  "input": {"buffer": "...", "length": 10},
  "binary_output": {"result": 0, "parsed": {...}},
  "ported_output": {"result": 0, "parsed": {...}},
  "match": true
}'
```

For mismatches, analyze root cause:

```
codergag_analyze_mismatch --project_id <id> --binary_id <bin> --function_address <addr> --test_id <test_id>
```
→ Returns likely causes (signed_conversion, type_mismatch, overflow, etc.)

Get overall mismatch summary:

```
codergag_get_mismatch_summary --project_id <id> --binary_id <bin> --function_address <addr>
```
→ Returns counts of passed/failed tests, critical/major/minor issues

## Porting Model

### Semantic Preservation Rules

When porting, preserve:
- **Integer width** — int8 vs int16 vs int32 vs int64
- **Signedness** — unsigned vs signed affects overflow behavior
- **Overflow semantics** — wrapping vs trapping
- **Alignment** — struct padding differences
- **Pointer semantics** — NULL, validity assumptions
- **Endianness** — byte order for multi-byte values
- **Floating-point behavior** — precision, NaN, infinity
- **Bit operations** — shift, rotate, mask behaviors
- **Memory ordering** — when relevant (locks, atomics)
- **State transitions** — error handling paths
- **Calling conventions** — when ABI-relevant

### Porting Status Tracking

```
codergag_get_porting_status --project_id <id> --binary_id <bin> --function_address <addr> --language rust
```
→ Returns status (PENDING, IN_PROGRESS, COMPLETED, BLOCKED), progress percentage, issues

## Available Tools

### Binary Analysis
| Tool | Purpose |
|------|---------|
| `codergag_index_binary` | Import binary into knowledge graph |
| `codergag_get_cfg` | Get control flow graph for function |
| `codergag_get_data_flow` | Get data flow analysis |
| `codergag_get_call_graph` | Get call graph for binary |
| `codergag_get_function_facts` | Get known facts about function |

### Context & Memory
| Tool | Purpose |
|------|---------|
| `codergag_prepare_reverse_engineering_context` | Assemble full context for task |
| `codergag_record_re_hypothesis` | Record hypothesis with evidence |
| `codergag_get_hypotheses` | Get competing hypotheses |
| `codergag_get_evidence` | Get supporting/contradicting evidence |
| `codergag_check_interception` | Detect LLM looping/hallucination |

### Porting
| Tool | Purpose |
|------|---------|
| `codergag_record_semantic_mapping` | Record source→target semantic mapping |
| `codergag_record_porting_decision` | Record porting decision |
| `codergag_record_known_difference` | Record known behavioral difference |
| `codergag_get_semantic_mappings` | Get all mappings for function |
| `codergag_get_porting_status` | Get porting progress |

### Verification
| Tool | Purpose |
|------|---------|
| `codergag_record_verification_test` | Record differential test result |
| `codergag_get_verification_results` | Get all verification results |
| `codergag_analyze_mismatch` | Analyze verification mismatch |
| `codergag_get_mismatch_summary` | Get summary of all mismatches |

### Legacy Tools (Still Available)
| Tool | Purpose |
|------|---------|
| `codergag_get_callees` | Get called functions |
| `codergag_get_callers` | Get calling functions |
| `codergag_trace_call_path` | Trace call path |
| `codergag_trace_data_flow` | Trace data flow |
| `codergag_record_validation` | Record equivalence validation |

## Output Template

```markdown
# Reverse Engineering Report: <binary_name>

## Summary
- **Binary**: <path> (SHA256: <hash>)
- **Analysis Tool**: <retdec|ghidra|ida|binja>
- **Target Language**: <C | Python | Go | Rust | ...>
- **Functions Analyzed**: <count>
- **Key Hypotheses**: <count>

## Binary Metadata
| Entity | Count |
|--------|-------|
| Functions | <n> |
| Basic Blocks | <n> |
| Strings | <n> |
| Imports | <n> |
| Exports | <n> |

## Architecture Overview
<Use codergag_get_architecture to get summary>

## Entry Points
<List of functions matching main/init/handler>

## Key Functions

### <function_name> (<address>)
- **Purpose** (hypothesis): <what this function does>
- **CFG**: <basic blocks, branches>
- **Callees**: <list>
- **Callers**: <list>
- **Data Flow**: <args, returns, globals>

### <another_function>
...

## Semantic Mappings
| Binary Construct | Target | Notes |
|----------------|--------|-------|
| int32_t | i32 | Preserves wraparound |
| malloc/free | Box/Drop | Memory safety |
| ...

## Porting Status
- **Overall Progress**: <n>%
- **Critical Issues**: <n>
- **Major Issues**: <n>

## Validation
| Test | Status | Notes |
|------|--------|-------|
| test_<name> | PASSED/FAILED | ... |

## Hypotheses & Evidence
| Hypothesis | Confidence | Evidence For | Against |
|-----------|-----------|-------------|---------|
| ... | ... | ... | ... |

## Behavioral Differences
| Type | Severity | Impact | Resolution |
|------|----------|--------|-----------|
| signed_conversion | major | rare edge case | use wrapping_add |
```

## Best Practices

1. **Start with entry points** — Use call graph to identify main/handler functions
2. **Query lazily** — Don't compute CFG/data-flow for entire binary; request only what's needed
3. **Record everything** — Hypotheses, mappings, decisions persist for future sessions
4. **Validate early** — Run verification tests as soon as possible to catch semantic gaps
5. **Track differences** — Known differences are better than silent divergence
6. **Use confidence** — Always track confidence; low confidence should trigger more analysis

## Scripts

This skill bundles a script in `scripts/`:

- **`scripts/run_retdec.sh`** — Runs RetDec decompiler on a binary and outputs JSON

## Test Cases

1. **Binary analysis**: "Reverse engineer this firmware at /samples/firmware.bin — port the packet parser to Rust"
2. **Language conversion**: "Convert the decompiled function at 0x401238 into Rust. I have it indexed in project 'firmware_analysis'."
3. **Hypothesis tracking**: "I analyzed the USB handler at 0x405600. Record that it validates device capabilities with medium confidence based on the string references and the branch structure."
4. **Verification**: "Run differential verification on my Rust port of parse_packet against the original binary"

## References

- RetDec: https://retdec.readthedocs.io/
- codeRAG semantic tools: `internal/mcp/tools.go`
- Reverse engineering service: `internal/services/reverse.go`
- Binary analysis service: `internal/services/binary_analysis.go`
- Porting service: `internal/services/porting.go`
- Verification service: `internal/services/binary_verification.go`
