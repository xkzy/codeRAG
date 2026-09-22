# codeRAG Architecture Report - Phase 1 Understanding

## Current Architecture Overview

```
MCP/CLI -> Application Services -> GraphRepository -> Storage (gob)
                    |
              Cache Manager
                    |
              Persistent Knowledge
              (graph nodes + cache entries)
```

## Core Components

### 1. Graph Layer (`internal/graph/`)
- **GraphRepository interface** - Abstract storage interface
- **PersistentGraphRepository** - Gob-backed persistent storage with in-memory cache (LRU)
- **MemoryGraphRepository** - In-memory implementation for tests
- **Schema Version**: 2 with migration support
- **Key methods**: UpsertNode, GetNode, FindNodes, Link, Neighbors, QueryReadonly, Save, Close

### 2. Domain Models (`internal/models/domain.go`)
- **Node** - ID, Kind, Properties (map[string]any)
- **Edge** - ID, Kind, FromID, ToID, Properties
- **KnowledgeState** - FACT, OBSERVATION, INFERENCE, HYPOTHESIS, CONFIRMED
- **EquivalenceStatus** - UNKNOWN, SUSPECTED, PARTIAL, VALIDATED, CONTRADICTED

### 3. Services Layer (`internal/services/`)
- **ReverseEngineeringService** - Binary import, function mapping, hypothesis recording, validation
- **CacheManager** - Multi-level cache (L0 exact, L1 semantic, L2 tool, L3 analysis)
- **ContextStore** - Durable project context (decisions, conventions, tasks, notes)
- **SecurityService** - CWE-based vulnerability scanning
- **DocumentService** - Document indexing (PDF, DOCX, CSV, etc.)

### 4. Reverse Engineering Domain (`internal/reverse/`)
- **NormalizedBinary/Function/BasicBlock/Instruction** - Normalized binary analysis output
- **RuntimeTrace** - Dynamic execution traces
- **Hypothesis/EvidenceRef** - Competing hypotheses with evidence
- **BehavioralEquivalence** - Binary vs source comparison results

### 5. MCP Tools (`internal/mcp/tools.go`) - 80+ tools registered
Key reverse engineering tools:
- `index_binary` - Import normalized binary data
- `record_re_hypothesis` - Record competing hypotheses
- `record_evidence` / `get_evidence` - Evidence recording
- `record_runtime_trace` - Runtime trace recording
- `record_behavioral_equivalence` - Binary vs source validation
- `map_binary_function` / `map_source_function` - Cross-mapping
- `record_port` / `record_validation` - Porting tracking

### 6. Cache System (`internal/cache/`)
- **CacheManager** - Multi-level cache with TTL, invalidation
- **SemanticCache** - Vector-based semantic similarity (BM25 + RRF)
- **CacheEntry** - Stores tool results with git/binary fingerprint invalidation
- **Freshness states**: VALID, STALE, INVALID

### 7. Binary Adapters (`adapters/`)
- Ghidra, IDA Pro, Binary Ninja, objdump/readelf, GDB, LLDB
- Convert vendor-specific output to normalized format

### 8. Privacy (`internal/privacy/`)
- **Policy** - FULL, MINIMAL, MASKED, STRUCTURAL, ABSTRACT, LOCAL_ONLY modes
- **Firewall** - Redacts before LLM transmission
- **Pseudonymizer** - Stable local pseudonyms for identifiers

## Current Binary RE Capabilities

### What Exists:
✅ Binary node with stable ID (binary_id, hash, tool)
✅ BinaryFunction with address, name, size, calls, basic blocks, instructions
✅ BasicBlock with instructions
✅ Instruction with mnemonic, operands, data/code refs
✅ String, BinaryData nodes
✅ CALLS, BRANCHES_TO, REFERENCES_DATA edges
✅ DecompilerOutput linked to functions
✅ Hypothesis with EvidenceFor/EvidenceAgainst
✅ BehavioralEquivalence with test cases and differences
✅ RuntimeTrace with instructions, memory reads/writes
✅ Porting tracking (EQUIVALENT_TO, PORTED_TO, VALIDATED_BY)

### What's Missing (per kk spec):
❌ Module, Section, Global, Import, Export, Symbol, Type, Variable, Register, MemoryLocation, Constant
❌ CallSite, Jump, DataFlow, ControlFlow, ExternalAPI, Artifact node types
❌ FLOWS_TO, READS, WRITES, USES, DEFINES, POINTS_TO, TAKES_ADDRESS_OF, RETURNS, PASSES_TO, DERIVES_FROM, DEPENDS_ON, IMPLEMENTS, WRAPS, ALIASES, RESEMBLES relationships
❌ Function recovery pipeline with confidence/evidence/status layers
❌ CFG, Call Graph, Data Flow Graph, Use-Def/Def-Use chains as cached artifacts
❌ Incremental invalidation based on graph dependencies
❌ Intermediate representation for porting (semantic, not pseudocode)
❌ Porting layer with semantic mappings (integer width, signedness, overflow, etc.)
❌ prepare_reverse_engineering_context() operation
❌ Background worker for progressive graph refinement
❌ Priority-based analysis scheduling
❌ Repeated exploration detection -> ContextPackage caching

## Performance Baseline Needed
Per spec, need benchmarks for:
1. Binary indexing time
2. Incremental analysis time
3. Function lookup latency
4. Graph traversal latency
5. Context generation latency
6. Cache hit/miss rates
7. LLM context size
8. Repeated exploration reduction
9. Memory/disk usage
10. Background CPU usage

## Implementation Plan Phases

### Phase 3 - Decouple (Priority)
Separate: INDEXING | GRAPH RESOLUTION | DEEP ANALYSIS | CONTEXT GENERATION

Current: Single ImportBinary call does everything
Target: 
- Cheap extraction → persistent facts
- Background graph resolution
- On-demand deeper analysis
- Cached analysis artifacts

### Phase 4 - Binary Entities
Add missing node/edge types to domain model and repository

### Phase 5 - Lazy Analysis
Implement demand-driven analysis with artifact caching keyed by:
- binary_hash
- function_hash  
- analysis_version
- architecture
- compiler_assumptions

### Phase 6 - Context Engine
New MCP tool: `prepare_reverse_engineering_context(target, task, token_budget)`

### Phase 7 - Porting Model
Add semantic mapping nodes for source→target constructs

### Phase 8 - Verification
Differential verification workflow with mismatch tracking

### Phase 9 - Background Worker
Job queue for progressive graph improvement

### Phase 10 - Re-benchmark
Compare against Phase 2 baselines