# codeRAG implementation plan

## Completed foundation

- Self-contained Go MCP server with stdio JSON-RPC framing. In-memory graph for tests; gob-persisted graph for production (no external database dependency).
- Incremental indexing for Python, C/C++, Rust, Go, Java, JavaScript, and TypeScript via regex-based parsers.
- Symbols, types, imports, calls, impact, signatures, entry points, complexity, hot paths, and dependency checks.
- Normalized binary import, source/binary mapping, porting, validation, evidence, hypotheses, observations, and provenance.
- Persistent memory, Markdown document search/design verification, read-only Git/PR context.
- Multi-level persistent cache (L0 exact, L1 semantic, L2 tool result, L3 analysis artifact) with cache key generation, invalidation, and metrics.
- Concurrent job deduplication to prevent duplicate expensive analysis across agents.

## Completed milestones

### Milestone 5: Replaceable binary analysis adapters (DONE)
- 6 adapters in `adapters/`: Ghidra, IDA Pro, Binary Ninja, objdump/readelf, GDB, LLDB
- Common `Adapter` interface + registry in `internal/reverse/adapter.go`
- JSON export from each tool's headless/scripted API; text parsing for objdump
- All with unit tests

### Milestone 6: Binary structure & analysis (DONE)
- Extended `NormalizedBinary` types: `DataReferences`, `CodeReferences`, `RuntimeTrace`, `Hypothesis`, `BehavioralEquivalence` in `internal/reverse/normalized.go`
- `ReverseEngineeringService` additions: `RecordRuntimeTrace`, `RecordHypothesis`, `RecordBehavioralEquivalence`, enhanced `ImportBinary` with cross-reference edges
- New MCP tools: `record_runtime_trace`, `record_re_hypothesis`, `record_behavioral_equivalence`, `index_binary`
- `handleIndexBinary` parses cross-refs and basic blocks

### Milestone 7: Editor shells (DONE)
- `adapters/lsp/adapter.go` - LSP client interface with VSCodeClient and JetBrainsClient
- `adapters/vscode/adapter.go` - VS Code extension shell
- `adapters/jetbrains/adapter.go` - JetBrains plugin shell
- All with tests

### Language coverage: 50+ languages incl. assembly (DONE)
- `internal/services/langs.go`: table-driven registry (`langSpec`) of regex extractors for functions, types, imports and calls; 9 languages keep tree-sitter (`treesitter.go`).
- Added: C#, Kotlin, Swift, Scala, Ruby, Crystal, PHP, Lua, Perl, R, Julia, Dart, Haskell, OCaml, F#, Elixir, Erlang, Clojure, Groovy, Shell, PowerShell, Objective-C, Zig, Nim, D, Fortran, COBOL, Pascal, Ada, Solidity, Verilog/SystemVerilog, VHDL, SQL, Protobuf, Lisp/Elisp, Scheme/Racket, Tcl, CUDA/GLSL/HLSL, Vue, Svelte, JSX.
- Assembly (`langs_asm.go`; GAS, NASM, MASM, ARM, AArch64, RISC-V, MIPS): functions are recovered from labels (global, `.type @function`, `PROC`, or call targets); local, jump and data labels are ignored; `call/bl/jal/tail` and tail-jumps become CALLS. Asm shares a call-resolution group with C so asm<->C calls resolve.
- MCP tool `list_languages` reports every language, extension and engine.
- Regex languages have no inheritance or data-flow edges; upgrading one to tree-sitter means adding its grammar to `languageFor` and its node types to the funcs/types tables.

### Phase 5: Privacy / Context Firewall (DONE)
- `internal/privacy/types.go` - PrivacyMode, DisclosureLevel, ReconstructionRisk, PseudonymKind, ContentKind
- `internal/privacy/policy.go` - PrivacyPolicy with project-scoped config, validation, per-mode defaults
- `internal/privacy/classifier.go` - ContentClassifier with secret detection (API keys, passwords, tokens, private keys, AWS, JWT, connection strings), path detection, comment classification, string literal detection
- `internal/privacy/redactor.go` - Redactor applying policy-based redaction
- `internal/privacy/pseudonymizer.go` - Stable local pseudonyms (FUNC_1842, TYPE_17, VAR_91) with local-only mapping
- `internal/privacy/firewall.go` - PrivacyFirewall: sanitize_context, redact, pseudonymize, abstract, enforce_policy, calculate_disclosure_level, detect_reconstruction_risk, validate_context, audit_transmission
- `internal/services/privacy.go` - PrivacyService exposing firewall to MCP layer
- Integrated with `ContextCompiler` via `PrivacyContext` in `ContextRequest` - `applyPrivacy` sanitizes assembled text before return
- MCP tools added: `sanitize_context`, `redact_content`, `pseudonymize_symbol`, `audit_transmission`, `privacy_policy`
- Cache integration: `StorePrivacyPolicy`/`GetPrivacyPolicy`, `StorePseudonymSnapshot`/`GetPseudonymSnapshot` in `CacheManager`
- Tests: 22 privacy tests, 3 service tests, 2 MCP tool tests

### Phase 6: Evaluation & correctness — Deterministic Runtime Benchmark (DONE)
- `Application.RunBenchmark` in `internal/services/eval.go` with four sub-benchmarks:
  - Reference accuracy: resolves all stable IDs in project, measures VALID/STALE/INVALID rates
  - Invalid-reference rejection: generates mutated/bogus IDs, verifies rejection rate and false positive/negative rates
  - Slice arithmetic: 8 test cases covering normal slices, out-of-bounds, negative offsets, off-by-one errors
  - Context tokens vs naive top-K baseline: compares ContextCompiler (ranked, token-budgeted) vs raw BM25 top-K search
- New MCP tool: `run_benchmark` with configurable `num_queries`, `max_tokens`, `include_baseline`
- Returns weighted overall score 0-100, stores `BenchmarkRun` in graph
- Test: `TestRunBenchmark` in `internal/mcp/tools_test.go`

### Phase 7: Binary structure — `resolve_instruction` / `resolve_basic_block` (DONE)
- `BasicBlock` / `Instruction` nodes exist in normalized adapter format (Milestone 6)
- `ReferenceResolver.Resolve` returns `RefValid` for Binary, BasicBlock, Instruction kinds
- `internal/ids/ids.go` — `BasicBlockID` and `InstructionID` stable ID constructors
- New MCP tools: `resolve_instruction`, `resolve_basic_block` with project validation
- Test: `TestResolveInstructionBasicBlock` in `internal/mcp/tools_test.go`

## Operations

- `codergag status` / `codergag eval -project ID` report health, usage, token savings and a 0-100 quality score; `run_eval`, `server_status` and `run_maintenance` expose the same to agents.
- The server runs a maintenance pass every 15 minutes (`CODERAG_MAINTENANCE_INTERVAL`, 0 disables): lossless memory compaction, edge re-resolution, stale cache purge, usage roll-up.
- List/search tools return compact rows by default (`detail=full` for raw), support `offset`, and honor `max_tokens`.

## Cognitive runtime (small-LLM capability layer)

Model-agnostic: no LLM is called anywhere; the runtime supplies memory, structure, verification.

- Phase 1 done: stable IDs (`func:`, `class:`, `struct:`, `file:`, `binfunc:`, `binary:`; repo-relative, property `stable_id`), exact SourceSpan/BinarySpan, symbols updated in place on re-index (node IDs and attached edges survive), `resolve_reference`, `verify_reference` (VALID / REFERENCE_STALE / INVALID_REFERENCE with corrections; relocates shifted symbols), `resolve_source_span`, `resolve_symbol`, `resolve_slice`, central rejection of hallucinated IDs in every `*_id` argument, schema v2 migration. Edge resolver renamed `resolve_edges`.
- Phase 2 done: persistent `TaskState` (`create_task`, `get_task_state`, `update_task_state`, `resume_task`, `list_tasks`), task and hypothesis state machines, FACT requires provenance, hypotheses evidence-gated, optimistic concurrency, agent provenance, STALE on resume.
- Phase 3 next: ContextCompiler (levels 0-5, budgets, provenance-tagged facts, structural vs semantic retrieval, runtime checklist, `explain_context`).
- Phase 4: verification runner (config-allowlisted build/test/analyze, no arbitrary shell), mode gating.
- Phase 5: compiler-aware cache (CACHE_HIT/PARTIAL/MISS/STALE), dependency-fingerprint invalidation — DONE: `CheckCache` multi-level lookup with status enums, semantic cache fallback.
- Phase 6: deterministic runtime benchmark (reference accuracy, invalid-reference rejection, slice arithmetic, context tokens vs a naive top-K baseline) — DONE in `Application.RunBenchmark` / `run_benchmark` tool.
- Binary structure: `BasicBlock` / `Instruction` nodes — DONE.

Every milestone requires project-isolation tests, provenance checks, bounded MCP responses, and documentation updates.