# Architecture and safety boundaries

`MCP/CLI → application services → GraphRepository → storage (gob)` is a deliberate dependency direction. MCP handlers are semantic and bounded; they do not contain database queries. Source paths and binary paths remain the source of truth, while the graph stores hashes, locations, relationships, provenance, evidence, and cache entries.

`query_graph` is for controlled read-only administration only: it rejects mutation keywords and multi-statement input, and requires a `:project_id` predicate which the server supplies. Semantic tools additionally reject node IDs owned by another project. The server provides no shell, network, privilege, or destructive-file tool. Write operations are limited to explicit graph records such as evidence, hypotheses, mappings, ports, validations, and cache entries.

Project identifiers are required on all MCP operations and services verify ownership before attaching evidence or cache entries. Claims are separate from evidence, so conflicting hypotheses remain available for later review.

The multi-level cache (L0 exact, L1 semantic, L2 tool result, L3 analysis artifact) uses deterministic content-addressable cache keys. Cache entries are invalidated based on fingerprints of the source revision, file hashes, binary hashes, and tool/parser versions. Concurrent analysis requests for the same key share a single in-flight job.
