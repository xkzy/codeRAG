# Changelog

All notable changes to codeRAG are documented here.

## [v1.4.0] — 2026-09-21

### New Features

#### Self-Update from GitHub Releases
- `codergag update [--check] [--force]` — check for and install updates from GitHub releases
- `codergag update --check` — check only, don't install
- `codergag update --force` — non-interactive install (CI-friendly)
- Automatic background update checks every 24h when running `codergag serve`
- Notifies on stderr when update available; runs `codergag update` to install

#### Security Fixes
- Removed hardcoded `admin/admin` default database credentials (CWE-798 fix)
- Default config now uses empty User/Password; requires explicit configuration

#### CI/CD Skill
- New `ci-cd` skill with GitHub Actions release pipeline template
- Self-update and auto-update documentation
- Multi-platform build patterns (Linux musl static, macOS, ARM64)

### Improvements
- Version embedded at build time via `-X main.Version=${{ github.ref_name }}`
- Release workflow generates SHA256SUMS for binary verification
- Installer updated to v1.4.0 with update command in next steps

### Configuration
- New `version` field in config (auto-populated from build ldflags)

---

## [v1.2.0] — 2026-09-21

### New Features

#### Context Store (`codergag ctx`)
Durable project context (decisions, conventions, tasks, notes) stored as graph memories and injected into every agent session automatically.

- `codergag ctx add` — record a decision, convention, note, or task
- `codergag ctx search` — BM25 search over stored records
- `codergag ctx status` — record counts + session digest token cost
- `codergag ctx export` — Markdown export grouped by kind
- `codergag ctx profile set` — per-developer preference key/values
- `codergag ctx reset` — wipe all records for a project
- Session digest: pinned records → decisions/conventions → other records → codebase line, newest-first, token-budgeted
- Prompt digest: records relevant to each prompt, skipping pinned (already in session) and seen IDs

#### Agent Hooks (`codergag setup --hooks`)
- `SessionStart` hook: injects token-budgeted context digest at the start of every session
- `UserPromptSubmit` hook: injects records relevant to each prompt (budget: `context.prompt_budget`, default 500 tokens)
- `codergag uninstall` removes only hooks this tool added
- Hook state (seen record IDs) stored per-session in `~/.codergag/hook-state/`
- Supports Claude Code hooks format; other agents via `codergag inject`

#### Canvas Web UI
- Unified canvas SPA replaces the previous dashboard + graphify pages
- D3 force-graph visualization of the code graph
- `/graphify` → `/` redirect for backward compatibility
- `graph.html` export from canvas

#### CLI Subcommands
- `codergag config get/set` — read and write config keys at runtime
- `codergag daemon` — start/stop/status of the background indexer
- `codergag doctor` — environment diagnostics (Go version, git, config)
- `codergag index` — trigger a manual index from the CLI
- `codergag inject` — inject context digest into a prompt (non-hook agents)
- `codergag model` — show/set the active LLM model config
- `codergag report` — generate project health report
- `codergag session` — manage session-scoped state
- `codergag watch` — start the file watcher directly

### Improvements

#### Bounded Memory & Disk-First Graph
- `PersistentGraphRepository` now lazy-loads state and caches on disk; resident node/edge cache bounded to `graph.cache_nodes` / `graph.cache_edges` (default 1024 each)
- `SemanticCache` and `ExactCache` bounded with FIFO eviction (`cache.semantic.max_entries`, `cache.exact.max_entries`, default 10,000)
- `EventEngine` queue/history bounded; deterministic oldest-entry eviction
- Daemon dirty-file tracking bounded; overflow triggers full index instead of unbounded growth
- Bounded background jobs and worker lifecycle tracking
- `debug.SetMemoryLimit` enforced at 1 GB by default (`CODERAG_MEMORY_LIMIT_MB`)
- Skip oversized files before reading content
- Graphify limited to one node per project during incremental indexing

#### Cache
- `CacheManager.Flush(projectID)` — remove all cache entries for a project
- `CacheManager.Config()` / `SetTTL()` — read and update cache config at runtime
- `StorePrivacyPolicy` / `GetPrivacyPolicy` — cache privacy policy per project
- `StorePseudonymSnapshot` / `GetPseudonymSnapshot` — cache pseudonymizer state per project
- `CheckCache` multi-level lookup: L0 exact → L1 semantic, returns `CACHE_HIT / CACHE_PARTIAL / STALE / CACHE_MISS`

#### Uninstall
- `uninstall.sh` — complete uninstall: binary, config, hooks, service file

### Bug Fixes
- Fixed nil-pointer panic when upserting a node loaded from disk in the persistent graph
- Restored identity-based deduplication during journal merges
- Fixed stale `putNode`/`putEdge` call signatures
- Removed unused variables and corrected `gobNode` map-key handling

### Tests
- Regression tests: bounded caches, persistent graph round trips, concurrent writer merging, deletions, migrations, daemon behavior, memory compaction
- `CacheManager`: store/check/flush, privacy/pseudonym cache, disabled-mode (coverage: 29% → 57%)
- `internal/config`: `ConfigGet`/`ConfigSet` round-trips, duration helpers, save/load (coverage: 23% → 79%)
- `internal/ctxstore`: session digest order/budget, prompt digest seen-skipping, profile round-trip, seen-state sanitization

### Configuration

New keys in `codergag.yaml.example`:

```yaml
context:
  session_budget: 1500    # tokens for SessionStart digest
  prompt_budget: 500      # tokens for UserPromptSubmit digest

graph:
  cache_nodes: 1024       # resident node cache size
  cache_edges: 1024       # resident edge cache size

watch:
  max_dirty_files: 10000  # dirty-file buffer before full re-index
```

---

## [v1.1.1] — 2026-09-20

Dashboard graph/activity fixes; no daemon for read-only commands; optional in-serve dashboard.

## [v1.1.0] — 2026-09-xx

Unified canvas SPA, D3 graph visualization.

## [v1.0.3] — 2026-09-xx

Stability fixes.

## [v1.0.0] — 2026-09-xx

Initial release.
