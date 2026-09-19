# codeRAG roadmap

## Current state

- Self-contained Go binary with gob-backed persistent graph storage.
- Full source code intelligence (indexing, symbols, calls, impact, complexity, etc.).
- Binary reverse engineering support (normalized functions, mappings, validation).
- Multi-level cache layer (exact, semantic, tool result, analysis artifact).
- MCP server with semantic bounded tools and cache tools.
- Concurrent job deduplication for multi-agent coordination.

## Future milestones

1. **Local embeddings** — Replace the current TF-IDF semantic cache with a locally-run embedding model for improved semantic similarity accuracy.
2. **BM25 retrieval** — Add BM25 search behind a replaceable search interface, with all results persisted as graph references and constrained by `project_id`.
3. **Source-analysis improvements** — Add circular dependencies, hot paths, dead imports, entry points, implementation discovery, related tests, complexity, signature search, and bounded custom traversal (some already implemented).
4. **Change intelligence** — Add git-aware PR/change context, stale-document checks, and impact summaries. Git is read-only.
5. **Documentation memory** — Index Markdown as structured `Document` / `DocumentSection` vertices and allow source-to-document verification (implemented).
6. **External adapters** — Add replaceable Ghidra, IDA, Binary Ninja, objdump/readelf, GDB, and LLDB adapters for reverse engineering.
7. **IDE integration** — Add optional LSP/VS Code/JetBrains shells that talk only to the MCP/application service boundary; no agent-specific logic enters the graph server.
8. **Quality and testing** — Add schema migration checks, cache controls, pagination, and multi-process integration tests.

Each milestone requires parser conformance fixtures, project-isolation tests, bounded response tests, and provenance metadata.
