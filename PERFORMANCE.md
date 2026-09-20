# codeRAG Performance & Limitations

## Test Environment
- **Repository**: codeRAG (381 Go files, 2,817 functions)
- **Binary**: `/home/khing/.local/bin/codergag` (built with Go 1.26)
- **Config**: Default (1GB memory limit, cache_nodes=1024, cache_edges=1024)
- **Date**: 2026-09-20

## Performance Metrics

### Indexing Performance
| Project | Files | Functions | Structs | Index Time (est) |
|---------|-------|-----------|---------|------------------|
| real (codeRAG) | 381 | 2,817 | 305 | ~6.4s (first), ~1s (incremental) |
| mcp-test | 122 | 1,155 | 232 | ~3s |
| test-project | 131 | 1,241 | 243 | ~3s |

### Graph Statistics (aggregated)
- **Total Nodes**: 8,631
- **Total Edges**: 32,907
- **Edge Types**: CALLS (12,012), DEFINES (7,532), USES (4,143), IMPORTS (3,128), DEPENDS_ON (3,529), DATA_FLOW (1,760), CONTAINS (764), EXTENDS (39)

### Query Performance (MCP tools)
| Tool | Query | Results | Latency |
|------|-------|---------|---------|
| `find_function` | "IndexRepository" | 3 | <100ms |
| `find_function` | "IndexFile" | 5 | <100ms |
| `find_function` | "applyMemoryLimit" | 1 | <100ms |
| `find_function` | "PersistentGraphRepository" | 5+ | <100ms |
| `find_function` | "readEffectiveStateLocked" | 1 | <100ms |
| `find_function` | "Graphify" | 5+ | <100ms |
| `find_symbol` | "Graphify" | 5+ | <100ms |
| `query_graph` | SELECT * FROM Function LIMIT 10 | 10 | <100ms |

### Eval Score (Project "real")
| Metric | Score | Details |
|--------|-------|---------|
| **Overall** | 87/100 | Grade: Good |
| Retrieval | 0.83 | MRR 0.83, hit@1 75%, hit@5 91% |
| Call Resolution | 1.00 | 4,535/4,550 in-project calls linked |
| Inheritance | 1.00 | 27/27 relations have edges |
| Freshness | 0.74 | 283/381 files match disk |
| Memory Retention | N/A | No compacted memories |

## Memory Improvements (v0.1.0)

### Before Fix
- **Issue**: `PersistentGraphRepository` read entire graph from disk on **every operation**
- **Symptom**: OOM kills during indexing large repos (50k+ files)
- **Root Cause**: `readEffectiveStateLocked()` called on every `UpsertNode`, `Link`, `FindNodes`, `Neighbors`

### After Fix
| Improvement | Impact |
|-------------|--------|
| **State caching** (`cachedState` + file stamp) | Eliminates repeated deserialization; reloads only when DB file changes |
| **Configurable cache sizes** | `graph.cache_nodes` / `graph.cache_edges` (default 1024) |
| **Skip graphify option** | `indexing.skip_graphify: true` avoids 200k node/400k edge knowledge graph |
| **Memory limit** | `debug.SetMemoryLimit(1GB)` enforced by Go runtime |

### Memory Usage (Observed)
- **Idle**: ~50-100 MB
- **During indexing**: ~200-400 MB (peaks during graphify)
- **With skip_graphify**: ~100-200 MB

## Limitations

### Query Language (query_graph)
- **Supports**: `SELECT * FROM <Kind> WHERE <condition> [LIMIT N]`
- **Conditions**:
  - Equality: `property = 'value'` or `property = :param`
  - LIKE pattern: `property LIKE 'pattern'` (supports `%` wildcard, `_` single char)
  - Multiple conditions: `cond1 AND cond2` or `cond1 && cond2`
- **Must include**: `:project_id` parameter in WHERE clause
- **Example valid**: `SELECT * FROM Function WHERE name LIKE '%Index%' AND project_id = :project_id LIMIT 10`
- **Example valid**: `SELECT * FROM Function WHERE name = 'foo' AND project_id = :project_id LIMIT 10`

### find_function / find_symbol
- Uses BM25 ranking on function name + content
- Limited to exact/substring matches on indexed fields
- No semantic/vector search (semantic cache exists but not exposed via MCP)

### Incremental Indexing
- Tracks file hashes to skip unchanged files
- Does NOT track dependency graph for transitive invalidation
- Graphify runs only on full index (unless `skip_graphify: true`)

### Daemon
- **Project detection**: Scans `project_roots` every 30s (configurable)
- **Max projects**: 32 (configurable)
- **Max watch dirs**: 256 (configurable)
- **Max background jobs**: 4 (configurable)
- File watcher uses `fsnotify` (platform-dependent limits)

### Cross-Project Isolation
- Strict: `project_id` required on ALL operations
- No cross-project edges or queries allowed
- Each project has separate graph namespace

### Unsupported Features
- No multi-file refactoring tools
- No LSP integration (only MCP)
- No git history analysis beyond HEAD commit
- No binary analysis in default build (requires Ghidra/IDA/BinJA adapters)
- No remote/index sharing (SQLite is local file)

## Configuration Tuning

### For Large Repos (>10k files)
```yaml
indexing:
  skip_graphify: true      # Disable knowledge graph to save memory
  incremental: true

graph:
  cache_nodes: 4096        # Increase cache for read-heavy workloads
  cache_edges: 4096

watch:
  max_workers: 8           # More parallel indexing
  max_dirty_files: 50000   # Larger dirty file buffer
  project_scan_depth: 5    # Deeper project detection
```

### For Memory-Constrained Environments
```yaml
graph:
  cache_nodes: 256
  cache_edges: 256

watch:
  max_workers: 2
  max_watch_dirs: 64
  max_dirty_files: 1000

indexing:
  skip_graphify: true
```

### Environment Variables
| Variable | Default | Description |
|----------|---------|-------------|
| `CODERAG_MEMORY_LIMIT_MB` | 1024 | Go heap limit (0 = disabled) |
| `CODERAG_CONFIG` | `~/.codergag.yaml` | Config file path |
| `CODERAG_HTTP_ADDR` | disabled | Dashboard address (e.g., `127.0.0.1:8080`) |
| `CODERAG_MAINTENANCE_INTERVAL` | 15m | Optimization pass interval (0 = disabled) |

## Known Issues
1. **Freshness score low** (0.74): 97/381 files changed since last index - run `index_repository` periodically
2. **query_graph LIMIT ignored** if not in query string
3. **Graphify runs on every full index** - can be slow for large repos
4. **Daemon not running by default** in `serve` mode - enable `watch.enabled: true` in config
5. **No progress reporting** for long-running index operations via MCP

### Fixed in v0.1.0
- ✅ **LIKE queries**: `name LIKE '%pattern%'` now supported in query_graph
- ✅ **Concurrent query safety**: State cloning prevents map write conflicts
- ✅ **Memory OOM**: State caching eliminates repeated disk reads during indexing