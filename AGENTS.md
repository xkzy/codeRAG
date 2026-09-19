# AGENTS.md

## Mission

codeRAG is an agent-agnostic MCP server and shared engineering knowledge base. Keep the LLM outside the source of truth: source and binaries remain on the filesystem, while the persistent graph (gob-backed) stores relationships, metadata, provenance, evidence, bounded derived context, and a multi-level analysis cache.

## Architecture rules

- Keep the dependency direction `CLI/MCP -> application services -> GraphRepository -> storage`.
- MCP handlers must call services; do not place storage queries in tool handlers.
- Preserve the in-memory repository for fast tests and use `PersistentGraphRepository` for production.
- Require and validate `project_id` on every semantic operation. Never allow cross-project edges or evidence.
- Prefer semantic, bounded tools over arbitrary queries. `query_graph` must remain read-only and project-scoped.
- Source paths and binary paths are references, not copied repositories.
- Reverse-engineering conclusions must retain state, confidence, method, analyst/agent, timestamp, and supporting or contradicting evidence.
- Fixed read-only Git inspection is allowed for change analysis. Do not add shell, debugger, network, or destructive-file execution tools for LLM use.
- The multi-level cache (L0 exact, L1 semantic, L2 tool result, L3 analysis artifact) prevents redundant LLM calls and expensive re-analysis.

## Development

```bash
go build ./...
go test ./...
```

Add a regression test for every behavior change. Prefer normalized adapters for external engines instead of coupling services to a vendor.

## MCP and schema changes

When adding a tool, update `ToolRegistry`, preserve project validation, return concise structured data, and document provenance/limits. Update the domain/service model and tests together. Maintain compatibility with existing graph classes and use additive migrations where possible.
