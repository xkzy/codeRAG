# agentctx / cctx port roadmap

Each cycle gets its own spec (brainstorming), plan, and implementation. Order is fixed; later cycles build on the `codergag hook` dispatcher and `internal/ctxstore` from cycle 1.

| Cycle | Scope | Source | Status |
|---|---|---|---|
| 1 | Record store, `ctx` CLI, `SessionStart`/`UserPromptSubmit` hook injection, `setup --hooks` | agentctx | DONE (implemented, all tests pass) |
| 2 | Tool-output compression: `PostToolUse` hook shrinking large Bash/Read/Grep/ls/test/web output (deterministic rules first) | cctx `compressor/*` | Not started |
| 3 | Session checkpoints: `Stop`/`SessionEnd` capture, consolidation, knowledge extraction; deterministic capture with optional LLM | cctx `memory/*` + agentctx session-end extraction | Not started |
| 4 | Local LLM summarizer: Ollama-backed summarizer and real `model`/`daemon` commands. Reverses the "no local LLM" decision in `PLAN_CLI.md`; needs its own design review | cctx `summarizer/*`, `ollama/*` | Not started |
| 5 | CLAUDE.md drift `sync` | agentctx | Not started |

Notes:
- agentctx is Elastic License 2.0; port ideas, do not copy source.
- cctx behavior (not CLI surface) is what remains; command parity is already in `PLAN_CLI.md`.
