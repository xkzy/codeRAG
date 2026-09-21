---
task_id: reveng-toolkit-v1
project_id: codergag
mode: implement
state: completed
created_by: klio-auto/free
created_at: 2026-09-21
---

# Reverse Engineering Toolkit Development Tracker

## Goal
Build a token-efficient reverse engineering toolkit for codeRAG: RetDec adapter, programming workflow skills, anti-loop/hallucination interceptor, and token-runaway prevention.

## Plan
1. Add test coverage for internal/graph memory repository
2. Add test coverage for internal/mcp handler edge cases  
3. Add CLI helper tests for cmd/codergag
4. Add edge case tests for adapters (gdb, lldb, lsp)
5. Add branch path coverage tests for internal/services/analysis.go
6. Create RetDec adapter (adapters/retdec/)
7. Create reverse-engineer skill (skills/reverse-engineer/SKILL.md)
8. Create programming workflow skills (code-review, test-generation, refactoring, security-audit)
9. Create auto-install script for adapter tools (scripts/setup_tools.sh)
10. Create anti-loop/hallucination interceptor (internal/services/anti_loop.go)
11. Add decompiler output truncation to ImportBinary

## Completed
- [x] Step 1: internal/graph/memory_test.go (MemoryRepository coverage improved)
- [x] Step 2: internal/graph/persistent_cache_test.go (PersistentRepository tests)
- [x] Step 3: internal/mcp/handlers_extra_test.go (40+ handler edge cases)
- [x] Step 4: internal/services/analysis_test.go (analysis.go 0% → 99.1%)
- [x] Step 5: internal/services/anti_loop.go + anti_loop_test.go (AntiLoopDetector with 10 tests)
- [x] Step 6: cmd/codergag/helpers_test.go (CLI helpers 100%)
- [x] Step 7: adapters/gdb/edge_test.go (GDB adapter edge cases)
- [x] Step 8: adapters/lldb/edge_test.go (LLDB adapter edge cases)
- [x] Step 9: adapters/lsp/lsp_extra_test.go (LSP adapter 9.0% → 93.3%)
- [x] Step 10: adapters/retdec/adapter.go + adapter_test.go (6 tests)
- [x] Step 11: skills/reverse-engineer/SKILL.md + scripts/run_retdec.sh + references/retdec_api.md
- [x] Step 12: skills/code-review/SKILL.md
- [x] Step 13: skills/test-generation/SKILL.md
- [x] Step 14: skills/refactoring/SKILL.md
- [x] Step 15: skills/security-audit/SKILL.md
- [x] Step 16: scripts/setup_tools.sh (auto-install RetDec, GDB, LLDB, Ghidra, objdump)
- [x] Step 17: Token-runaway prevention - decompiler output truncation (MaxDecompilerOutputLen=50000, MaxInstructionFieldLen=500)
- [x] Step 18: Anti-loop MCP tool registered as codergag_check_interception
- [x] Step 19: internal/services/reverse_test.go (reverse.go 0% → 90.5%)
- [x] Step 20: internal/mcp/handlers.go + tools.go (check_interception handler/tool)

## Results
- analysis.go coverage: 0% → 99.1% (217/217 statements)
- reverse.go coverage: 0% → 90.5% (114/126 statements)
- internal/services overall: 72.7% → 74.3%
- adapters/retdec coverage: 100% (new package)
- All tests pass across 17 packages

## Files Created
- internal/services/analysis_test.go
- internal/services/anti_loop.go
- internal/services/anti_loop_test.go
- adapters/retdec/adapter.go
- adapters/retdec/adapter_test.go
- skills/reverse-engineer/SKILL.md
- skills/reverse-engineer/scripts/run_retdec.sh
- skills/reverse-engineer/references/retdec_api.md
- skills/code-review/SKILL.md
- skills/test-generation/SKILL.md
- skills/refactoring/SKILL.md
- skills/security-audit/SKILL.md
- scripts/setup_tools.sh
