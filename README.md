# codeRAG

codeRAG is a local-first, agent-agnostic MCP server and shared engineering knowledge base. Multiple coding agents (Claude Code, OpenCode, Codex, Gemini CLI) connect to one server and one persistent graph; no agent-specific business logic is required.

It supports code intelligence (incremental indexing, symbols, imports, calls, impact, complexity, dependencies, architecture, tests, documentation, memory, and Git context) and reverse engineering (normalized binary functions, strings, decompiler output, runtime observations, evidence-backed hypotheses, source mappings, ports, and behavioral validation).

A multi-level persistent cache (exact, semantic, tool result, analysis artifact) prevents redundant LLM calls and expensive re-analysis across agents.

## Comparison

| Feature | **codeRAG** | **Sourcegraph** | **CodeGraph / GraphCode** | **FalkorDB** | **codegraph-mcp** |
|---------|-------------|-----------------|---------------------------|--------------|-------------------|
| **Type** | Local MCP server | Cloud SaaS + self-hosted | Research prototypes | Graph DB (Redis module) | MCP wrapper |
| **Deployment** | Single binary, SQLite | Kubernetes/Docker | Python/Neo4j | Docker/Redis | Python MCP |
| **Data locality** | 100% local | Cloud (code leaves) | Local | Local | Local |
| **Languages** | 7 (Go, Py, JS/TS, Java, Rust, C/C++) | 40+ | Varies | Any (Cypher) | Limited |
| **Indexing** | Regex incremental | Precise (LSIF/SCIP) | AST-based | Manual | Regex |
| **Graph** | In-memory + gob persist | Distributed | Neo4j/NetworkX | Property graph (Cypher) | NetworkX |
| **MCP native** | ✅ Yes | ❌ (API only) | ❌ | ❌ | ✅ Yes |
| **Binary analysis** | 6 adapters (Ghidra, IDA, etc.) | ❌ | ❌ | ❌ | ❌ |
| **Privacy firewall** | ✅ Modes + pseudonyms | Enterprise only | ❌ | ❌ | ❌ |
| **Cache** | Multi-level (L0/L1/L2/L3) | CDN | ❌ | Redis | ❌ |
| **Benchmarking** | Deterministic (ref/refs/slice/ctx) | ❌ | ❌ | ❌ | ❌ |
| **License** | MIT | Proprietary | MIT/Apache | AGPL | MIT |
| **Cost** | Free | $49/user/mo | Free | Free/Commercial | Free |

**Key differentiators for codeRAG:**
- **Agent-native**: Built as MCP server from ground up
- **Binary + source unified**: Same graph for source and reverse-engineered binaries
- **Privacy-first**: Context firewall with pseudonymization before any LLM sees code
- **No infrastructure**: Single binary + SQLite, runs on laptop
- **Correctness verification**: Deterministic benchmark suite (reference accuracy, invalid ref rejection, slice arithmetic, context token efficiency)
- **Compiler-aware cache**: CACHE_HIT/PARTIAL/MISS/STALE with git/binary fingerprint invalidation

## Install and run

```bash
go build -o bin/codergag ./cmd/codergag
go test ./...
```

The server uses stdio MCP. The CLI command is `codergag`. Main tools include `find_function`, `get_callers`, `impact_analysis`, `analyze_complexity`, `pr_context`, `index_markdown`, `memory_search`, `index_binary`, `record_hypothesis`, and `record_evidence`.

Cache tools include `cache_lookup`, `cache_store`, `cache_invalidate`, `cache_stats`, `cache_explain`, `get_cached_analysis`, `refresh_analysis`, `ensure_fresh`, and `prepare_context`.

Responses are bounded and include provenance, evidence, and confidence where available. `query_graph` is an expert-only guarded read-only query and requires a project predicate. The server exposes no arbitrary shell, debugger, network, privilege, or destructive-file tool.

## Data and safety model

Git/filesystem remains the source of truth for source and binary files. The persistent graph (gob-backed) stores references, hashes, relationships, analysis metadata, memories, documents, hypotheses, evidence, and validation results. The cache layer stores reusable results with deterministic cache keys based on project_id, tool_name, normalized arguments, parser version, and schema version. Major objects are project-scoped. Reverse-engineering knowledge distinguishes `FACT`, `OBSERVATION`, `INFERENCE`, `HYPOTHESIS`, and `CONFIRMED`; competing hypotheses are preserved.

## Architecture

```text
MCP/CLI -> application services -> GraphRepository -> storage (gob)
                              (memory repository in tests)
                      |
                Cache Manager
                      |
            Persistent Knowledge
            (graph nodes + cache entries)
```

External analyzers are replaceable adapters. Ghidra, IDA, Binary Ninja, objdump/readelf, GDB, and LLDB are not required for source indexing.

## Cache hierarchy

```text
                    Persistent Knowledge
                           |
                     Graph (gob)
                           |
             +-------------+-------------+
             |            |            |
          Facts       Evidence     Hypotheses
             |            |            |
             +-------------+-------------+
                           |
                     Analysis Artifacts
                           |
                     Tool Result Cache
                           |
                     Semantic Cache
                           |
                       Exact Cache
                           |
                           LLM
```

Read [`AGENTS.md`](AGENTS.md) before changing the project. Keep handlers thin, services testable, graph access replaceable, responses compact, and uncertain conclusions explainable.
