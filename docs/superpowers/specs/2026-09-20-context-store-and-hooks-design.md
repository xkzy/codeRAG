# Context store and hook injection (agentctx port, cycle 1)

Ports the record store, CLI extras and hook injection ideas from agentctx into codeRAG, reimplemented in Go. agentctx is Elastic License 2.0, so no source is copied.

Out of scope (later cycles): PostToolUse capture and Stop/SessionEnd extraction (#3), CLAUDE.md drift sync (#4).

## 1. Record store: `internal/contextstore`

- Records are the existing graph `Memory` nodes (`services.MemoryService`), stored through the existing persistent graph repository. No new node type and no new database.
- Fields: `id`, `project_id`, `kind` (decision | convention | task | note), `title`, `content`, `source` (manual | hook | extraction), `pinned` (bool, new property), `created_at`. Identity is `project_id` + `title`.
- API: `Add`, `Get`, `List(filter)`, `Search(query, limit)`, `Pin`, `Delete`, `Reset(project)`.
- Search reuses the existing BM25 search over title and body.
- No dependency on `cmd/`; unit-testable against the in-memory repository.

## 2. CLI: `codergag ctx <sub>`

| Sub | Behavior |
|---|---|
| `add` | Create a record (`-kind`, `-title`, `-pin`; body from args or stdin) |
| `search <q>` | Ranked matches, `-json` supported |
| `show <id>` | Print one full record |
| `export` | Render records as Markdown grouped by kind |
| `status` | Record counts by kind, plus the token cost of the current SessionStart digest |
| `profile` | Show/edit/clear global preferences in `~/.codergag/profile.json` |
| `reset` | Delete the current project's records; requires confirmation or `-yes` |

## 3. Hook injection: `codergag hook <event>`

- Reads hook JSON from stdin; writes `{"hookSpecificOutput":{"hookEventName":...,"additionalContext":...}}` to stdout.
- Read-only and fast. Any error is logged to stderr and the command exits 0, so a session is never blocked.
- `SessionStart`: digest within a token budget (default 1500, config `context.session_budget`): pinned records, recent decisions, a one-line codebase summary (file and function counts), and profile preferences.
- `UserPromptSubmit`: top-k search hits for the prompt within a smaller budget (default 500, `context.prompt_budget`). Records already injected in this session are skipped (tracked by session id in a small state file under `~/.codergag/`).
- Token estimate: `len(text)/4`, shared helper so `ctx status` and the hooks agree.
- Unknown events (`PostToolUse`, `Stop`, ...) are accepted and no-op, reserving the dispatcher for cycle 2.

## 4. Registration

- `codergag setup --hooks` merges `SessionStart` and `UserPromptSubmit` entries into `~/.claude/settings.json`, each tagged with a marker in the command (`codergag hook`) so they can be found again.
- Idempotent; foreign hooks are preserved.
- `uninstall` removes only tagged entries.

## 5. Testing

- Store: add/search/list/reset on the in-memory repo.
- Budget trimming and per-session dedupe: table tests.
- Hook I/O: golden JSON in and out, including malformed stdin (must exit 0).
- Settings merge: round trip preserves foreign hooks; second run changes nothing; uninstall removes only ours.
- Verification: `go build ./...`, `go vet ./...`, `go test ./... -count=1`.
