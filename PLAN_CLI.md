# codeRAG CLI — cctx feature parity plan

## Goal

Port the cctx (Claude Context Optimizer) CLI surface onto `codergag` so that
agents and humans who are used to cctx's command set get a familiar entry
point, while the underlying engine stays codeRAG's own knowledge graph.

## Status: complete

Every cctx command now has a `codergag` equivalent. See `cmd/codergag/main.go`
for the dispatch table and the `usage` string for the full list.

## Command mapping

| cctx | codergag | Behaviour |
|---|---|---|
| `setup` | `codergag setup [--yes]` | Writes a default config, prints the MCP registration snippet, points at `index run`. No Ollama/model to install — codeRAG is a single binary + SQLite. |
| `daemon` | `codergag daemon [start|stop|restart|status]` | Thin wrapper over `services.Application.Daemon`. |
| `model` | `codergag model [list|set|pull|remove]` | Compat shim. `list` returns an empty inventory with a note; `set`/`pull`/`remove` are no-ops that explain codeRAG has no local LLM. |
| `index` | `codergag index [run|status|watch] [root]` | `run` calls `CodeIndexService.IndexRepository`; `status` lists indexed projects; `watch` delegates to `runWatch`. |
| `session` | `codergag session [list|stats|flush|export] [--json]` | Reads `UsageSession` nodes from the graph. |
| `inject` | `codergag inject [--project ID] [--file CLAUDE.md]` | Writes a compact codebase map from the graph. |
| `register-instructions` | `codergag register-instructions [--agent claude|all] [--file ...]` | Writes the 115-tool instruction set and prints the MCP snippet. |
| `doctor` | `codergag doctor [--json]` | Health checks: graph, cache, daemon, config, storage. |
| `config` | `codergag config [show|get|set]` | Reads/edits the YAML at `CODERAG_CONFIG` (default `~/.codergag.yaml`). |
| `mcp` | `codergag mcp` | Alias for `serve` (cctx calls this internally). |
| `uninstall` | `codergag uninstall [--keep-db]` | Removes config, instructions, db, and install.sh-managed binaries. |

## What is deliberately different

- cctx persists agent *conversation* sessions to `.cctx/sessions.db`; codeRAG
  persists MCP *tool-call* sessions as `UsageSession` nodes in the graph. The
  `session` command therefore reports codeRAG usage, not cctx conversations.
- cctx's `model` manages a local Ollama model for LLM-backed compression.
  codeRAG has no local LLM — it is an MCP server that delegates to whatever
  model the agent uses. `model` is a documented no-op shim, not a fake.
- cctx's `setup` installs Ollama + pulls a model + registers MCP + indexes.
  codeRAG's `setup` only writes config and prints the registration snippet.

## Files

- `cmd/codergag/main.go` — dispatch table, `configForCommand`, usage string.
- `cmd/codergag/setup.go`, `daemon.go`, `model.go`, `index.go`, `config.go`,
  `uninstall.go` — new cctx-compat commands.
- `cmd/codergag/session.go`, `inject.go`, `register.go`, `doctor.go` — ported
  commands (doctor now has a real storage check).
- `cmd/codergag/commands_test.go` — regression tests for every new command.
- `internal/cli/cli.go` — shared `PrintJSON` helper (single source of truth).
- `internal/config/accessors.go` — `ConfigGet` / `ConfigSet` for the config CLI.
- `internal/services/project.go` — exported `StableProjectID`.
- `internal/services/eval.go` — per-metric panic recovery; only persist a
  complete report.
- `internal/services/team.go` — ok-checked type assertions on `agent`/`role`
  and on scope entries; plus `team_test.go` for malformed-properties coverage.

## Weaknesses fixed

1. **Empty `internal/cli/` package.** Created `internal/cli/cli.go` with the
   shared `PrintJSON` helper; `report.go` and `doctor.go` now delegate to it.
2. **Duplicate `printJSON`.** Removed the `doctor.go` copy; both callers use
   `cli.PrintJSON`.
3. **`configForCommand` incomplete.** Now lists every read-only command and
   understands `index run|status` vs `index watch`. Test expanded.
4. **`doctor`'s `checkConfig` was a stub.** Replaced with a real storage-path
   check; added a `storage` check too.
5. **`runSession` aggregated before dispatch.** `list`/`flush` no longer pay
   for a full usage summary.
6. **`eval` persisted partial reports.** Per-metric panic recovery; only a
   complete, marshalled report is written.
7. **Unsafe type assertions.** Every `x, _ := n.Properties["k"].(string)`
   across `analysis.go`, `calls.go`, `context.go`, `indexing.go`,
   `memory_compact.go` converted to the safe `strProp` helper; the unchecked
   `scopeSet[sc.(string)]` in `team.go` now ok-checks too.
8. **No CLI regression tests.** Added `cmd/codergag/commands_test.go` covering
   every new command and subcommand.
9. **No malformed-properties test for TeamService.** Added
   `internal/services/team_test.go` with nil/missing `agent` properties and a
   non-string scope entry.
10. **`web.go` still reaches straight to the graph in HTTP handlers.** Left as
    a documented architectural debt (see `AGENTS.md`); it is read-only and
    local-only.

## Verification

```bash
go build ./...   # clean
go vet ./...     # clean
go test ./...    # 17 packages, 0 failures
```