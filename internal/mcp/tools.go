package mcp

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"codergag/internal/ids"
	"codergag/internal/models"
	"codergag/internal/services"
)

type ToolHandler func(args map[string]any) (map[string]any, error)

type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Handler     ToolHandler    `json:"-"`
}

type ToolRegistry struct {
	app     *services.Application
	schemas []*ToolSpec

	mu        sync.Mutex // serializes tool calls with scheduled maintenance
	usage     *services.UsageRecorder
	lastFlush time.Time
	// per-call accounting, valid while mu is held
	curRaw, curShaped int
	curTruncated      bool
}

// usageFlushEvery bounds how often usage counters are written to the graph, so
// read-only calls do not force a file rewrite each time.
var usageFlushEvery = 30 * time.Second

func NewToolRegistry(app *services.Application) *ToolRegistry {
	reg := &ToolRegistry{app: app, usage: services.NewUsageRecorder(app.Graph), lastFlush: time.Now()}
	reg.registerAll()
	reg.app.Documents.SetKnownNames(reg.vocabulary())
	return reg
}

func (r *ToolRegistry) registerAll() {
	baseProps := func(props map[string]any) map[string]any {
		result := map[string]any{
			"project_id": map[string]any{
				"type":        "string",
				"description": "Required isolation boundary for the project.",
			},
		}
		for k, v := range props {
			result[k] = v
		}
		return result
	}

	requiredBase := []string{"project_id"}

	r.register("index_repository", "CodeGraph semantic operation: index repository", baseProps(map[string]any{
		"path":        map[string]any{"type": "string", "description": "Repository root path to index."},
		"incremental": map[string]any{"type": "boolean", "description": "Whether to skip unchanged files."},
		"ignore":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Additional directories to ignore."},
	}), append(append([]string{}, requiredBase...), "path"), r.handleIndexRepository)

	r.register("index_file", "CodeGraph semantic operation: index file", baseProps(map[string]any{
		"path": map[string]any{"type": "string", "description": "File path to index."},
	}), append(requiredBase, "path"), r.handleIndexFile)

	r.register("find_symbol", "CodeGraph semantic operation: find symbol", baseProps(map[string]any{
		"query": map[string]any{"type": "string", "description": "Search query for symbol name."},
		"limit": map[string]any{"type": "integer", "description": "Maximum results."},
	}), append(requiredBase, "query"), r.handleFindSymbol)

	r.register("find_function", "CodeGraph semantic operation: find function", baseProps(map[string]any{
		"query": map[string]any{"type": "string", "description": "Search query for function name."},
		"limit": map[string]any{"type": "integer", "description": "Maximum results."},
	}), append(requiredBase, "query"), r.handleFindFunction)

	r.register("find_class", "CodeGraph semantic operation: find class", baseProps(map[string]any{
		"query": map[string]any{"type": "string", "description": "Search query for class name."},
		"limit": map[string]any{"type": "integer", "description": "Maximum results."},
	}), append(requiredBase, "query"), r.handleFindClass)

	r.register("find_struct", "CodeGraph semantic operation: find struct", baseProps(map[string]any{
		"query": map[string]any{"type": "string", "description": "Search query for struct name."},
		"limit": map[string]any{"type": "integer", "description": "Maximum results."},
	}), append(requiredBase, "query"), r.handleFindStruct)

	r.register("find_string", "CodeGraph semantic operation: find string", baseProps(map[string]any{
		"query": map[string]any{"type": "string", "description": "Search query for string content."},
		"limit": map[string]any{"type": "integer", "description": "Maximum results."},
	}), append(requiredBase, "query"), r.handleFindString)

	r.register("get_function", "CodeGraph semantic operation: get function", baseProps(map[string]any{
		"name": map[string]any{"type": "string", "description": "Function name to look up."},
	}), append(requiredBase, "name"), r.handleGetFunction)

	r.register("get_callers", "CodeGraph semantic operation: get callers", baseProps(map[string]any{
		"function_id": map[string]any{"type": "string", "description": "ID of the function to get callers for."},
	}), append(requiredBase, "function_id"), r.handleGetCallers)

	r.register("get_callees", "CodeGraph semantic operation: get callees", baseProps(map[string]any{
		"function_id": map[string]any{"type": "string", "description": "ID of the function to get callees for."},
	}), append(requiredBase, "function_id"), r.handleGetCallees)

	r.register("get_references", "CodeGraph semantic operation: get references (evidence links and code that uses a class/struct)", baseProps(map[string]any{
		"node_id": map[string]any{"type": "string", "description": "ID of the node to get references for."},
	}), append(requiredBase, "node_id"), r.handleGetReferences)

	r.register("get_dependents", "CodeGraph semantic operation: get dependents (files whose imports resolve to this file, plus importers of a module)", baseProps(map[string]any{
		"node_id": map[string]any{"type": "string", "description": "ID of the node to get dependents for."},
	}), append(requiredBase, "node_id"), r.handleGetDependents)

	r.register("get_dependencies", "CodeGraph semantic operation: get dependencies (project files this file imports, plus imported modules)", baseProps(map[string]any{
		"node_id": map[string]any{"type": "string", "description": "ID of the node to get dependencies for."},
	}), append(requiredBase, "node_id"), r.handleGetDependencies)

	r.register("trace_call_path", "CodeGraph semantic operation: trace call path", baseProps(map[string]any{
		"source_id": map[string]any{"type": "string", "description": "Source function ID."},
		"target_id": map[string]any{"type": "string", "description": "Target function ID."},
		"max_depth": map[string]any{"type": "integer", "description": "Maximum traversal depth."},
	}), append(requiredBase, "source_id", "target_id"), r.handleTraceCallPath)

	r.register("trace_data_flow", "CodeGraph semantic operation: trace data flow", baseProps(map[string]any{
		"source_id": map[string]any{"type": "string", "description": "Source function ID."},
		"target_id": map[string]any{"type": "string", "description": "Target function ID."},
		"max_depth": map[string]any{"type": "integer", "description": "Maximum traversal depth."},
	}), append(requiredBase, "source_id", "target_id"), r.handleTraceDataFlow)

	r.register("impact_analysis", "CodeGraph semantic operation: impact analysis", baseProps(map[string]any{
		"node_id": map[string]any{"type": "string", "description": "ID of the node to analyze impact for."},
		"depth":   map[string]any{"type": "integer", "description": "Traversal depth."},
		"limit":   map[string]any{"type": "integer", "description": "Maximum results."},
	}), append(requiredBase, "node_id"), r.handleImpactAnalysis)

	r.register("find_related_code", "CodeGraph semantic operation: find related code", baseProps(map[string]any{
		"node_id": map[string]any{"type": "string", "description": "ID of the node to find related code for."},
	}), append(requiredBase, "node_id"), r.handleFindRelatedCode)

	r.register("find_similar_functions", "CodeGraph semantic operation: find similar functions", baseProps(map[string]any{
		"function_id": map[string]any{"type": "string", "description": "ID of the function to find similar functions for."},
		"limit":       map[string]any{"type": "integer", "description": "Maximum results."},
	}), append(requiredBase, "function_id"), r.handleFindSimilarFunctions)

	r.register("get_type_hierarchy", "CodeGraph semantic operation: supertypes and subtypes via EXTENDS/IMPLEMENTS edges", baseProps(map[string]any{
		"node_id":   map[string]any{"type": "string", "description": "Class or struct node ID."},
		"direction": map[string]any{"type": "string", "enum": []string{"up", "down", "both"}, "description": "up = supertypes, down = subtypes (default both)."},
		"depth":     map[string]any{"type": "integer", "description": "Maximum levels to traverse (default 3)."},
	}), append(requiredBase, "node_id"), r.handleGetTypeHierarchy)

	agentProp := map[string]any{"type": "string", "description": "Your agent/model name; recorded for handoff provenance."}
	r.register("create_task", "Start a persistent task (working memory outside your context window). References must be resolvable stable IDs.", baseProps(map[string]any{
		"goal":        map[string]any{"type": "string"},
		"mode":        map[string]any{"type": "string", "enum": []string{"explore", "implement", "reverse_engineer", "validate", "autonomous"}},
		"agent":       agentProp,
		"constraints": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"references":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Stable IDs the task is about."},
	}), append(append([]string{}, requiredBase...), "goal"), r.handleCreateTask)

	r.register("get_task_state", "Read a task's full persistent state", baseProps(map[string]any{
		"task_id": map[string]any{"type": "string"},
	}), append(append([]string{}, requiredBase...), "task_id"), r.handleGetTaskState)

	r.register("update_task_state", "Apply a patch to a task atomically: state transition, plan/steps, facts (FACT needs provenance), unknowns, hypotheses (evidence-gated), tool results, failures, validation, next_action. Pass expected_version to detect concurrent edits.", baseProps(map[string]any{
		"task_id": map[string]any{"type": "string"},
		"agent":   agentProp,
		"patch":   map[string]any{"type": "object", "description": "Fields: expected_version, state, state_reason, current_step, add_plan, complete_steps, add_facts[{text,kind,source}], add_unknowns, resolve_unknowns, add_constraints, add_references, add_evidence, add_tool_results[{tool,summary}], add_failures, clear_failures, add_validation[{name,status,detail}], hypotheses[{id,claim,subject_id,status,confidence,evidence_for,evidence_against}], next_action."},
	}), append(append([]string{}, requiredBase...), "task_id", "patch"), r.handleUpdateTaskState)

	r.register("resume_task", "Continue a task without the previous conversation: returns its state, the live validity of every reference (moving it to STALE if any is outdated) and a runtime checklist. Records the handoff.", baseProps(map[string]any{
		"task_id": map[string]any{"type": "string"},
		"agent":   agentProp,
	}), append(append([]string{}, requiredBase...), "task_id"), r.handleResumeTask)

	r.register("list_tasks", "List tasks in the project, newest first", baseProps(map[string]any{
		"state": map[string]any{"type": "string"},
		"limit": map[string]any{"type": "integer"},
	}), requiredBase, r.handleListTasks)

	r.register("resolve_edges", "Re-run the edge resolvers (calls, inheritance, type uses, imports->files) and report edge totals", baseProps(map[string]any{}), requiredBase, r.handleResolveReferences)

	r.register("resolve_references", "Deprecated alias of resolve_edges (not the same as resolve_reference)", baseProps(map[string]any{}), requiredBase, r.handleResolveReferences)

	refID := map[string]any{"type": "string", "description": "Stable ID, e.g. func:src/cat21.c:parse_cat21, class:..., struct:..., file:..., binfunc:<binary>:<address>."}
	r.register("resolve_reference", "Resolve a stable ID to its current source/binary location (span, hash, revision). INVALID_REFERENCE returns verified near matches; REFERENCE_STALE means the code changed since indexing.", baseProps(map[string]any{
		"id": refID,
	}), append(append([]string{}, requiredBase...), "id"), r.handleResolveReference)

	r.register("verify_reference", "Check a remembered reference is still safe to modify: VALID, REFERENCE_STALE or INVALID_REFERENCE. Pass the content_hash/revision you saw when reading.", baseProps(map[string]any{
		"id":                    refID,
		"expected_content_hash": map[string]any{"type": "string", "description": "content_hash from when you read the code."},
		"expected_revision":     map[string]any{"type": "string", "description": "git revision you read it at."},
	}), append(append([]string{}, requiredBase...), "id"), r.handleVerifyReference)

	r.register("resolve_source_span", "Fingerprint a file line range as it is now and name the innermost indexed symbol containing it", baseProps(map[string]any{
		"file":       map[string]any{"type": "string", "description": "Repo-relative (or absolute) path."},
		"start_line": map[string]any{"type": "integer"},
		"end_line":   map[string]any{"type": "integer"},
	}), append(append([]string{}, requiredBase...), "file", "start_line"), r.handleResolveSourceSpan)

	r.register("resolve_symbol", "Structural symbol lookup by exact name or Owner.name (never similarity); returns stable IDs, or verified near matches for an unknown name", baseProps(map[string]any{
		"name": map[string]any{"type": "string"},
	}), append(append([]string{}, requiredBase...), "name"), r.handleResolveSymbol)

	r.register("resolve_instruction", "Resolve an insn:<binary>:<address> ID (instruction-level data must have been imported by an adapter)", baseProps(map[string]any{
		"id": map[string]any{"type": "string"},
	}), append(append([]string{}, requiredBase...), "id"), r.handleResolveInstruction)

	r.register("resolve_basic_block", "Resolve a bb:<binary>:<function>:<n> ID (basic-block data must have been imported by an adapter)", baseProps(map[string]any{
		"id": map[string]any{"type": "string"},
	}), append(append([]string{}, requiredBase...), "id"), r.handleResolveBasicBlock)

	r.register("resolve_slice", "Do slice/offset arithmetic deterministically: offset, length or exclusive end, element size, optional base length. Reports out-of-range and off-by-one problems.", baseProps(map[string]any{
		"base":              map[string]any{"type": "string", "description": "Buffer/variable name or ID."},
		"offset":            map[string]any{"type": "integer"},
		"length":            map[string]any{"type": "integer"},
		"end":               map[string]any{"type": "integer", "description": "Exclusive end index."},
		"element_size":      map[string]any{"type": "integer", "description": "Bytes per element (default 1)."},
		"element_type":      map[string]any{"type": "string"},
		"base_length":       map[string]any{"type": "integer", "description": "Known element count of base, for bounds checking."},
		"source_reference":  map[string]any{"type": "string", "description": "Stable ID of the code producing the slice."},
		"source_expression": map[string]any{"type": "string"},
	}), append(append([]string{}, requiredBase...), "base", "offset"), r.handleResolveSlice)

	r.register("server_status", "codeRAG health: graph size, per-project index/memory status, tool usage, token savings, last maintenance and eval", map[string]any{}, []string{}, r.handleServerStatus)

	r.register("list_languages", "List every language the indexer supports with file extensions and the extraction engine (tree-sitter, regex or label-scanner for assembly)", map[string]any{}, []string{}, r.handleListLanguages)

	r.register("run_eval", "codeRAG self-evaluation: retrieval quality, call/inheritance resolution, index freshness and memory retention with a 0-100 score", baseProps(map[string]any{}), requiredBase, r.handleRunEval)

	r.register("run_benchmark", "Deterministic runtime benchmark: reference accuracy, invalid-reference rejection, slice arithmetic, context tokens vs naive top-K baseline", baseProps(map[string]any{
		"num_queries":      map[string]any{"type": "integer", "description": "Number of queries for context benchmark (default 10)."},
		"max_tokens":       map[string]any{"type": "integer", "description": "Token budget for context compiler (default 2000)."},
		"include_baseline": map[string]any{"type": "boolean", "description": "Include context vs baseline comparison (default true)."},
	}), requiredBase, r.handleRunBenchmark)

	r.register("run_maintenance", "Run the optimization pass now: compact memories, re-resolve edges, purge stale cache, roll up usage", map[string]any{}, []string{}, r.handleRunMaintenance)

	r.register("get_subsystem", "CodeGraph semantic operation: get subsystem", baseProps(map[string]any{
		"name":  map[string]any{"type": "string", "description": "Subsystem name to search for."},
		"limit": map[string]any{"type": "integer", "description": "Maximum results."},
	}), append(requiredBase, "name"), r.handleGetSubsystem)

	r.register("get_architecture", "CodeGraph semantic operation: get architecture", baseProps(map[string]any{}), requiredBase, r.handleGetArchitecture)

	r.register("search_code_graph", "CodeGraph semantic operation: search code graph", baseProps(map[string]any{
		"query":             map[string]any{"type": "string", "description": "Search query."},
		"limit":             map[string]any{"type": "integer", "description": "Maximum results."},
		"include_generated": map[string]any{"type": "boolean", "description": "Also return machine-generated code (hidden by default)."},
	}), append(requiredBase, "query"), r.handleSearchCodeGraph)

	r.register("get_hypotheses", "CodeGraph semantic operation: get hypotheses", baseProps(map[string]any{
		"subject_id": map[string]any{"type": "string", "description": "Subject node ID."},
	}), append(requiredBase, "subject_id"), r.handleGetHypotheses)

	r.register("get_evidence", "CodeGraph semantic operation: get evidence", baseProps(map[string]any{
		"subject_id": map[string]any{"type": "string", "description": "Subject node ID."},
	}), append(requiredBase, "subject_id"), r.handleGetEvidence)

	r.register("record_evidence", "CodeGraph semantic operation: record evidence", baseProps(map[string]any{
		"subject_id":             map[string]any{"type": "string", "description": "Subject node ID."},
		"description":            map[string]any{"type": "string", "description": "Evidence description."},
		"confidence":             map[string]any{"type": "number", "description": "Confidence between 0 and 1."},
		"kind":                   map[string]any{"type": "string", "description": "Evidence kind."},
		"agent":                  map[string]any{"type": "string", "description": "Agent name."},
		"method":                 map[string]any{"type": "string", "description": "Recording method."},
		"supports_hypothesis_id": map[string]any{"type": "string", "description": "Hypothesis ID to support."},
	}), append(requiredBase, "subject_id", "description"), r.handleRecordEvidence)

	r.register("record_observation", "CodeGraph semantic operation: record observation", baseProps(map[string]any{
		"subject_id":  map[string]any{"type": "string", "description": "Subject node ID."},
		"description": map[string]any{"type": "string", "description": "Observation description."},
		"confidence":  map[string]any{"type": "number", "description": "Confidence between 0 and 1."},
		"agent":       map[string]any{"type": "string", "description": "Agent name."},
		"method":      map[string]any{"type": "string", "description": "Recording method."},
	}), append(requiredBase, "subject_id", "description"), r.handleRecordObservation)

	r.register("record_runtime_trace", "Reverse engineering: record runtime execution trace from debugger", baseProps(map[string]any{
		"trace": map[string]any{"type": "object", "description": "Runtime trace (binary_id, function_addr, trace_id, timestamp, instructions, memory_reads, memory_writes)."},
	}), append(requiredBase, "trace"), r.handleRecordRuntimeTrace)

	r.register("record_re_hypothesis", "Reverse engineering: record competing hypothesis with evidence", baseProps(map[string]any{
		"hypothesis": map[string]any{"type": "object", "description": "Hypothesis (id, subject_id, claim, confidence, status, analyst, evidence_for, evidence_against)."},
	}), append(requiredBase, "hypothesis"), r.handleRecordHypothesis)

	r.register("record_behavioral_equivalence", "Reverse engineering: record behavioral equivalence comparison result", baseProps(map[string]any{
		"equivalence": map[string]any{"type": "object", "description": "Equivalence (binary_function_id, source_function_id, equivalence_status, confidence, method, analyst, test_cases, differences)."},
	}), append(requiredBase, "equivalence"), r.handleRecordBehavioralEquivalence)

	r.register("index_binary", "CodeGraph semantic operation: index binary", baseProps(map[string]any{
		"binary": map[string]any{"type": "object", "description": "Normalized binary data (binary_id, path, sha256, tool, functions)."},
	}), append(requiredBase, "binary"), r.handleIndexBinary)

	r.register("map_binary_function", "CodeGraph semantic operation: map binary function to source", baseProps(map[string]any{
		"binary_function_id": map[string]any{"type": "string", "description": "Binary function node ID."},
		"source_function_id": map[string]any{"type": "string", "description": "Source function node ID."},
		"confidence":         map[string]any{"type": "number", "description": "Confidence between 0 and 1."},
		"method":             map[string]any{"type": "string", "description": "Mapping method."},
	}), append(requiredBase, "binary_function_id", "source_function_id", "confidence"), r.handleMapBinaryFunction)

	r.register("map_source_function", "CodeGraph semantic operation: map source function to binary", baseProps(map[string]any{
		"binary_function_id": map[string]any{"type": "string", "description": "Binary function node ID."},
		"source_function_id": map[string]any{"type": "string", "description": "Source function node ID."},
		"confidence":         map[string]any{"type": "number", "description": "Confidence between 0 and 1."},
		"method":             map[string]any{"type": "string", "description": "Mapping method."},
	}), append(requiredBase, "binary_function_id", "source_function_id", "confidence"), r.handleMapBinaryFunction)

	r.register("record_port", "CodeGraph semantic operation: record port", baseProps(map[string]any{
		"binary_function_id": map[string]any{"type": "string", "description": "Binary function node ID."},
		"implementation_id":  map[string]any{"type": "string", "description": "Implementation node ID."},
		"language":           map[string]any{"type": "string", "description": "Implementation language."},
		"confidence":         map[string]any{"type": "number", "description": "Confidence between 0 and 1."},
	}), append(requiredBase, "binary_function_id", "implementation_id", "language", "confidence"), r.handleRecordPort)

	r.register("record_validation", "CodeGraph semantic operation: record validation", baseProps(map[string]any{
		"binary_function_id": map[string]any{"type": "string", "description": "Binary function node ID."},
		"implementation_id":  map[string]any{"type": "string", "description": "Implementation node ID."},
		"test_name":          map[string]any{"type": "string", "description": "Test name."},
		"status":             map[string]any{"type": "string", "description": "Test status (passed/failed)."},
		"confidence":         map[string]any{"type": "number", "description": "Confidence between 0 and 1."},
		"method":             map[string]any{"type": "string", "description": "Validation method."},
	}), append(requiredBase, "binary_function_id", "implementation_id", "test_name", "status", "confidence"), r.handleRecordValidation)

	r.register("rename_symbol", "CodeGraph semantic operation: rename symbol", baseProps(map[string]any{
		"symbol_id": map[string]any{"type": "string", "description": "Symbol node ID."},
		"name":      map[string]any{"type": "string", "description": "New name."},
		"agent":     map[string]any{"type": "string", "description": "Agent performing the rename."},
	}), append(requiredBase, "symbol_id", "name"), r.handleRenameSymbol)

	r.register("update_symbol", "CodeGraph semantic operation: update symbol properties", baseProps(map[string]any{
		"symbol_id":  map[string]any{"type": "string", "description": "Symbol node ID."},
		"properties": map[string]any{"type": "object", "description": "Properties to set."},
	}), append(requiredBase, "symbol_id", "properties"), r.handleUpdateSymbol)

	r.register("query_graph", "CodeGraph semantic operation: query graph (read-only, expert only)", baseProps(map[string]any{
		"query":  map[string]any{"type": "string", "description": "Read-only SELECT or MATCH query with :project_id filter."},
		"params": map[string]any{"type": "object", "description": "Query parameters."},
	}), append(requiredBase, "query"), r.handleQueryGraph)

	r.register("analyze_complexity", "CodeGraph semantic operation: analyze complexity", baseProps(map[string]any{
		"function_id": map[string]any{"type": "string", "description": "Function node ID."},
	}), append(requiredBase, "function_id"), r.handleAnalyzeComplexity)

	r.register("find_circular_deps", "CodeGraph semantic operation: find circular dependencies", baseProps(map[string]any{
		"limit": map[string]any{"type": "integer", "description": "Maximum results."},
	}), requiredBase, r.handleFindCircularDeps)

	r.register("find_hot_paths", "CodeGraph semantic operation: find hot paths", baseProps(map[string]any{
		"limit": map[string]any{"type": "integer", "description": "Maximum results."},
	}), requiredBase, r.handleFindHotPaths)

	r.register("find_dead_imports", "CodeGraph semantic operation: find dead imports", baseProps(map[string]any{
		"limit": map[string]any{"type": "integer", "description": "Maximum results."},
	}), requiredBase, r.handleFindDeadImports)

	r.register("get_module_summary", "CodeGraph semantic operation: get module summary", baseProps(map[string]any{
		"query": map[string]any{"type": "string", "description": "Path filter query."},
		"limit": map[string]any{"type": "integer", "description": "Maximum results."},
	}), requiredBase, r.handleGetModuleSummary)

	r.register("find_by_signature", "CodeGraph semantic operation: find by signature", baseProps(map[string]any{
		"parameter_count": map[string]any{"type": "integer", "description": "Parameter count filter."},
		"language":        map[string]any{"type": "string", "description": "Language filter."},
		"limit":           map[string]any{"type": "integer", "description": "Maximum results."},
	}), requiredBase, r.handleFindBySignature)

	r.register("find_entry_points", "CodeGraph semantic operation: find entry points", baseProps(map[string]any{
		"limit": map[string]any{"type": "integer", "description": "Maximum results."},
	}), requiredBase, r.handleFindEntryPoints)

	r.register("find_related_tests", "CodeGraph semantic operation: find related tests", baseProps(map[string]any{
		"function_name": map[string]any{"type": "string", "description": "Function name to find tests for."},
		"limit":         map[string]any{"type": "integer", "description": "Maximum results."},
	}), append(requiredBase, "function_name"), r.handleFindRelatedTests)

	r.register("memory_store", "CodeGraph semantic operation: store memory", baseProps(map[string]any{
		"title":   map[string]any{"type": "string", "description": "Memory title."},
		"content": map[string]any{"type": "string", "description": "Memory content."},
		"kind":    map[string]any{"type": "string", "description": "Memory kind."},
		"agent":   map[string]any{"type": "string", "description": "Agent name."},
		"source":  map[string]any{"type": "string", "description": "Source of the memory."},
	}), append(requiredBase, "title", "content"), r.handleMemoryStore)

	r.register("memory_get", "CodeGraph semantic operation: get memory", baseProps(map[string]any{
		"title": map[string]any{"type": "string", "description": "Memory title."},
	}), append(requiredBase, "title"), r.handleMemoryGet)

	r.register("memory_search", "CodeGraph semantic operation: search memory", baseProps(map[string]any{
		"query":            map[string]any{"type": "string", "description": "Search query."},
		"limit":            map[string]any{"type": "integer", "description": "Maximum results."},
		"include_archived": map[string]any{"type": "boolean", "description": "Also search originals already folded into compacted summaries."},
	}), append(requiredBase, "query"), r.handleMemorySearch)

	r.register("memory_compact", "Memory: fold memories about the same code entity into lossless graph-linked summaries (also runs automatically)", baseProps(map[string]any{
		"min_group": map[string]any{"type": "integer", "description": "Minimum memories per entity to compact (default 2)."},
	}), requiredBase, r.handleMemoryCompact)

	r.register("index_markdown", "CodeGraph semantic operation: index markdown", baseProps(map[string]any{
		"path": map[string]any{"type": "string", "description": "Markdown file path."},
	}), append(requiredBase, "path"), r.handleIndexMarkdown)

	r.register("search_docs", "CodeGraph semantic operation: search docs", baseProps(map[string]any{
		"query": map[string]any{"type": "string", "description": "Search query."},
		"limit": map[string]any{"type": "integer", "description": "Maximum results."},
	}), append(requiredBase, "query"), r.handleSearchDocs)

	r.register("list_doc_sources", "CodeGraph semantic operation: list doc sources", baseProps(map[string]any{}), requiredBase, r.handleListDocSources)

	r.register("remove_doc_source", "CodeGraph semantic operation: remove doc source", baseProps(map[string]any{
		"document_id": map[string]any{"type": "string", "description": "Document ID to remove."},
	}), append(requiredBase, "document_id"), r.handleRemoveDocSource)

	r.register("verify_design", "CodeGraph semantic operation: verify design", baseProps(map[string]any{
		"document_id": map[string]any{"type": "string", "description": "Document ID."},
	}), append(requiredBase, "document_id"), r.handleVerifyDesign)

	r.register("generate_architecture_report", "Generate a compact Markdown architecture overview: modules, dependency layers and cycles, entry points, most-called functions, core types. Read this instead of exploring the tree.", baseProps(map[string]any{
		"depth": map[string]any{"type": "integer", "description": "Directory levels that form a module (default 2)."},
		"limit": map[string]any{"type": "integer", "description": "Rows per section (default 10)."},
	}), requiredBase, r.handleArchitectureReport)

	r.register("check_docs", "Stale-document warnings: doc references to symbols/files that no longer exist (with rename hints), code changed since the doc, missing doc files", baseProps(map[string]any{
		"limit": map[string]any{"type": "integer", "description": "Maximum documents / references listed."},
	}), requiredBase, r.handleCheckDocs)

	r.register("pr_context", "CodeGraph semantic operation: PR context", baseProps(map[string]any{
		"base":  map[string]any{"type": "string", "description": "Base ref for diff."},
		"limit": map[string]any{"type": "integer", "description": "Maximum changed files."},
	}), requiredBase, r.handlePrContext)

	r.register("review_suggestions", "Git: per-author line ownership for changed files (git blame), sorted by lines owned", baseProps(map[string]any{
		"files": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Repo-relative file paths to blame (default: changed files since last commit)."},
		"limit": map[string]any{"type": "integer", "description": "Maximum reviewers."},
	}), requiredBase, r.handleReviewSuggestions)

	r.register("audit_security", "Security audit: scan project for vulnerabilities (CWE patterns)", baseProps(map[string]any{
		"path":  map[string]any{"type": "string", "description": "Project root to audit."},
		"cache": map[string]any{"type": "boolean", "description": "Use cache to skip re-scan."},
		"agent": map[string]any{"type": "string", "description": "Agent performing the audit."},
	}), append(requiredBase, "path"), r.handleAuditSecurity)

	r.register("find_vulnerabilities", "Security: find findings by CWE", baseProps(map[string]any{
		"cwe":   map[string]any{"type": "string", "description": "CWE identifier (e.g. CWE-78)."},
		"limit": map[string]any{"type": "integer", "description": "Maximum results."},
	}), append(requiredBase, "cwe"), r.handleFindVulnerabilities)

	r.register("get_findings", "Security: get all security findings for project", baseProps(map[string]any{
		"limit": map[string]any{"type": "integer", "description": "Maximum results."},
	}), requiredBase, r.handleGetFindings)

	r.register("get_cwe_details", "Security: get CWE details", baseProps(map[string]any{
		"cwe": map[string]any{"type": "string", "description": "CWE identifier."},
	}), append(requiredBase, "cwe"), r.handleGetCWEDetails)

	r.register("get_security_metrics", "Security: get security metrics and findings summary", baseProps(map[string]any{}), requiredBase, r.handleGetSecurityMetrics)

	r.register("team_create", "Multi-agent: create a team with agent personas", baseProps(map[string]any{
		"name":        map[string]any{"type": "string", "description": "Team name."},
		"members":     map[string]any{"type": "array", "description": "Team members with role and agent.", "items": map[string]any{"type": "object"}},
		"owner_agent": map[string]any{"type": "string", "description": "Agent that created the team."},
	}), append(requiredBase, "name"), r.handleTeamCreate)

	r.register("list_teams", "Multi-agent: list teams in project", baseProps(map[string]any{}), requiredBase, r.handleListTeams)

	r.register("list_agents", "Multi-agent: list agents in a team or project", baseProps(map[string]any{
		"team_id": map[string]any{"type": "string", "description": "Team ID filter."},
	}), requiredBase, r.handleListAgents)

	r.register("list_roles", "Multi-agent: list built-in agent roles", baseProps(map[string]any{}), requiredBase, r.handleListRoles)

	r.register("assign_task", "Multi-agent: assign a task to a team", baseProps(map[string]any{
		"team_id":     map[string]any{"type": "string", "description": "Team ID."},
		"title":       map[string]any{"type": "string", "description": "Task title."},
		"description": map[string]any{"type": "string", "description": "Task description."},
		"role":        map[string]any{"type": "string", "description": "Role to assign."},
		"assignee":    map[string]any{"type": "string", "description": "Agent assignee."},
		"priority":    map[string]any{"type": "string", "description": "Priority (low/medium/high)."},
		"due_date":    map[string]any{"type": "string", "description": "Due date (ISO format)."},
	}), append(requiredBase, "team_id", "title", "description", "role"), r.handleAssignTask)

	r.register("update_task", "Multi-agent: update task status", baseProps(map[string]any{
		"task_id": map[string]any{"type": "string", "description": "Task ID."},
		"status":  map[string]any{"type": "string", "description": "New status."},
	}), append(requiredBase, "task_id", "status"), r.handleUpdateTask)

	r.register("get_tasks", "Multi-agent: get tasks for project/team", baseProps(map[string]any{
		"team_id": map[string]any{"type": "string", "description": "Team ID filter."},
		"status":  map[string]any{"type": "string", "description": "Status filter."},
		"limit":   map[string]any{"type": "integer", "description": "Maximum results."},
	}), requiredBase, r.handleGetTasks)

	r.register("share_knowledge", "Multi-agent: share knowledge between agents", baseProps(map[string]any{
		"from_agent": map[string]any{"type": "string", "description": "Source agent."},
		"to_agent":   map[string]any{"type": "string", "description": "Target agent."},
		"to_role":    map[string]any{"type": "string", "description": "Target role."},
		"title":      map[string]any{"type": "string", "description": "Knowledge title."},
		"content":    map[string]any{"type": "string", "description": "Knowledge content."},
		"scopes":     map[string]any{"type": "array", "description": "Knowledge scope tags.", "items": map[string]any{"type": "string"}},
	}), append(requiredBase, "from_agent", "title", "content"), r.handleShareKnowledge)

	r.register("get_team_context", "Multi-agent: assemble team context from graph and cache", baseProps(map[string]any{
		"team_id": map[string]any{"type": "string", "description": "Team ID."},
		"scopes":  map[string]any{"type": "array", "description": "Knowledge scopes to include.", "items": map[string]any{"type": "string"}},
		"limit":   map[string]any{"type": "integer", "description": "Maximum items per category."},
	}), append(requiredBase, "team_id"), r.handleGetTeamContext)

	r.register("cache_lookup", "Cache: search exact and semantic caches", baseProps(map[string]any{
		"query":     map[string]any{"type": "string", "description": "Search query."},
		"tool_name": map[string]any{"type": "string", "description": "Tool name to look up."},
		"arguments": map[string]any{"type": "object", "description": "Normalized arguments."},
		"limit":     map[string]any{"type": "integer", "description": "Maximum results."},
	}), append(requiredBase, "query"), r.handleCacheLookup)

	r.register("cache_store", "Cache: persist a reusable result", baseProps(map[string]any{
		"tool_name":   map[string]any{"type": "string", "description": "Tool name."},
		"arguments":   map[string]any{"type": "object", "description": "Normalized arguments."},
		"result":      map[string]any{"type": "object", "description": "Result to cache."},
		"confidence":  map[string]any{"type": "number", "description": "Confidence score."},
		"agent":       map[string]any{"type": "string", "description": "Agent name."},
		"git_commit":  map[string]any{"type": "string", "description": "Git commit hash."},
		"binary_hash": map[string]any{"type": "string", "description": "Binary hash."},
	}), append(requiredBase, "tool_name", "arguments", "result"), r.handleCacheStore)

	r.register("cache_invalidate", "Cache: invalidate cached entries", baseProps(map[string]any{
		"cache_key":   map[string]any{"type": "string", "description": "Specific cache key."},
		"cache_id":    map[string]any{"type": "string", "description": "Specific cache entry ID."},
		"tool_name":   map[string]any{"type": "string", "description": "Tool name to invalidate."},
		"commit":      map[string]any{"type": "string", "description": "Git commit hash."},
		"file":        map[string]any{"type": "string", "description": "File path."},
		"binary_hash": map[string]any{"type": "string", "description": "Binary hash."},
		"artifact":    map[string]any{"type": "string", "description": "Artifact identifier."},
	}), requiredBase, r.handleCacheInvalidate)

	r.register("cache_stats", "Cache: show cache statistics", baseProps(map[string]any{}), requiredBase, r.handleCacheStats)

	r.register("cache_flush", "Cache: flush all cache entries for a project", baseProps(map[string]any{
		"project_id": map[string]any{"type": "string", "description": "Project ID."},
	}), requiredBase, r.handleCacheFlush)

	r.register("cache_config", "Cache: show current cache configuration", baseProps(map[string]any{}), requiredBase, r.handleCacheConfig)

	r.register("cache_explain", "Cache: explain why a cached result was returned", baseProps(map[string]any{
		"cache_id":  map[string]any{"type": "string", "description": "Cache entry ID."},
		"cache_key": map[string]any{"type": "string", "description": "Cache key."},
	}), requiredBase, r.handleCacheExplain)

	r.register("get_cached_analysis", "Cache: retrieve cached analysis artifact", baseProps(map[string]any{
		"artifact_type": map[string]any{"type": "string", "description": "Artifact type."},
		"artifact_id":   map[string]any{"type": "string", "description": "Artifact ID."},
	}), requiredBase, r.handleGetCachedAnalysis)

	r.register("refresh_analysis", "Cache: force refresh of analysis", baseProps(map[string]any{
		"tool_name": map[string]any{"type": "string", "description": "Tool to refresh."},
		"arguments": map[string]any{"type": "object", "description": "Arguments for the tool."},
	}), append(requiredBase, "tool_name"), r.handleRefreshAnalysis)

	r.register("ensure_fresh", "Cache: ensure cache entry is fresh, invalidate if stale", baseProps(map[string]any{
		"cache_key":   map[string]any{"type": "string", "description": "Cache key to check."},
		"git_commit":  map[string]any{"type": "string", "description": "Expected git commit."},
		"binary_hash": map[string]any{"type": "string", "description": "Expected binary hash."},
	}), append(requiredBase, "cache_key"), r.handleEnsureFresh)

	r.register("prepare_context", "ContextCompiler: assemble ranked, provenance-tagged context from symbols, memory, docs, task and team knowledge. level 0=raw, 1=structured, 2=compressed, 3=ranked, 4=diff-aware, 5=provenance.", baseProps(map[string]any{
		"question":   map[string]any{"type": "string", "description": "User question or intent."},
		"task_id":    map[string]any{"type": "string", "description": "Task ID to pull task facts, plan and unknowns."},
		"team_id":    map[string]any{"type": "string", "description": "Team ID to pull team knowledge."},
		"scopes":     map[string]any{"type": "array", "description": "Knowledge scopes.", "items": map[string]any{"type": "string"}},
		"limit":      map[string]any{"type": "integer", "description": "Max items per section (default 10)."},
		"max_tokens": map[string]any{"type": "integer", "description": "Token budget; response is trimmed to fit."},
		"level":      map[string]any{"type": "integer", "description": "Context level 0-5 (default 1)."},
	}), append(requiredBase, "question"), r.handlePrepareContext)

	r.register("explain_context", "ContextCompiler: same as prepare_context at level=5 with a full provenance explanation of how each fact was sourced. Use when you need to audit or debug the context assembly.", baseProps(map[string]any{
		"question":   map[string]any{"type": "string", "description": "User question or intent."},
		"task_id":    map[string]any{"type": "string", "description": "Task ID to pull task facts."},
		"team_id":    map[string]any{"type": "string", "description": "Team ID to pull team knowledge."},
		"scopes":     map[string]any{"type": "array", "description": "Knowledge scopes.", "items": map[string]any{"type": "string"}},
		"limit":      map[string]any{"type": "integer", "description": "Max items per section (default 10)."},
		"max_tokens": map[string]any{"type": "integer", "description": "Token budget."},
	}), append(requiredBase, "question"), r.handleExplainContext)

	r.register("run_verification", "Verification runner: execute an allowlisted build/test/analyze command in the project directory. Never runs arbitrary shell; only allowlisted commands and sub-verbs are permitted. Returns structured output and parsed findings.", baseProps(map[string]any{
		"command":       map[string]any{"type": "string", "description": "Executable name, e.g. 'go', 'cargo', 'pytest'. Must be in the allowlist."},
		"args":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Arguments to pass, e.g. ['test', './...']."},
		"dir":           map[string]any{"type": "string", "description": "Working directory (defaults to project root)."},
		"env":           map[string]any{"type": "object", "description": "Extra environment variables (key-value pairs)."},
		"record_result": map[string]any{"type": "boolean", "description": "Persist the result as a VerificationRun node for provenance (default false)."},
		"agent":         map[string]any{"type": "string", "description": "Agent name for provenance."},
	}), append(requiredBase, "command"), r.handleRunVerification)

	r.register("list_verification_runs", "Verification runner: list recent verification runs recorded in the graph", baseProps(map[string]any{
		"limit": map[string]any{"type": "integer", "description": "Maximum results (default 20)."},
	}), requiredBase, r.handleListVerificationRuns)

	r.register("list_allowed_verifications", "Verification runner: show which commands and sub-verbs are allowed", map[string]any{}, []string{}, r.handleListAllowedVerifications)

	r.register("sanitize_context", "Privacy: apply the project privacy policy to outbound context. Returns whether transmission is allowed, the sanitized text, redactions, disclosure level and reconstruction risk.", baseProps(map[string]any{
		"content":    map[string]any{"type": "string", "description": "Context text to sanitize."},
		"mode":       map[string]any{"type": "string", "description": "Override privacy mode (FULL, MINIMAL, MASKED, STRUCTURAL, ABSTRACT, LOCAL_ONLY)."},
		"destination": map[string]any{"type": "string", "description": "Destination label for audit."},
	}), append(requiredBase, "content"), r.handleSanitizeContext)

	r.register("redact_content", "Privacy: redact secrets, forbidden paths and identifiers from content.", baseProps(map[string]any{
		"content": map[string]any{"type": "string", "description": "Content to redact."},
	}), append(requiredBase, "content"), r.handleRedactContent)

	r.register("pseudonymize_symbol", "Privacy: generate a stable local pseudonym for an identifier.", baseProps(map[string]any{
		"name": map[string]any{"type": "string", "description": "Identifier name."},
		"kind": map[string]any{"type": "string", "description": "Pseudonym kind (FUNC, CLASS, STRUCT, VAR, FIELD, MODULE, TYPE, UNKNOWN)."},
	}), append(requiredBase, "name"), r.handlePseudonymizeSymbol)

	r.register("audit_transmission", "Privacy: audit an outbound transmission against the project policy.", baseProps(map[string]any{
		"content":    map[string]any{"type": "string", "description": "Content being transmitted."},
		"destination": map[string]any{"type": "string", "description": "Destination label."},
	}), append(requiredBase, "content", "destination"), r.handleAuditTransmission)

	r.register("privacy_policy", "Privacy: get or set the active privacy policy for a project.", baseProps(map[string]any{
		"mode":                 map[string]any{"type": "string", "description": "Privacy mode to set."},
		"allow_exact_source":   map[string]any{"type": "boolean"},
		"allow_strings":        map[string]any{"type": "boolean"},
		"max_source_bytes":     map[string]any{"type": "integer"},
		"max_context_tokens":   map[string]any{"type": "integer"},
		"allowed_identifiers":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"forbidden_identifiers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"allowed_paths":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"forbidden_paths":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	}), requiredBase, r.handlePrivacyPolicy)
}

// paginatedTools return a single ranked or filtered list and accept offset/limit.
// Call fetches offset+limit+1 rows so it can report has_more without a total count.
var paginatedTools = map[string]bool{
	"find_symbol": true, "find_function": true, "find_class": true, "find_struct": true,
	"find_string": true, "search_code_graph": true, "memory_search": true, "search_docs": true,
	"find_vulnerabilities": true, "get_findings": true, "find_hot_paths": true,
	"find_dead_imports": true, "find_by_signature": true, "find_entry_points": true,
	"find_related_tests": true, "get_tasks": true,
}

// globalTools operate on the whole installation and take no project_id.
var globalTools = map[string]bool{"server_status": true, "run_maintenance": true, "list_languages": true}

const maxFetch = 1000

func (r *ToolRegistry) register(name, description string, props map[string]any, required []string, handler ToolHandler) {
	if isShaped(name) {
		props["detail"] = map[string]any{"type": "string", "enum": []string{detailCompact, detailFull},
			"description": "compact (default) drops bookkeeping fields and trims long text; full returns raw rows."}
		props["max_tokens"] = map[string]any{"type": "integer",
			"description": "Approximate token budget for the returned rows; the page is cut to fit and next_offset continues it."}
	}
	if paginatedTools[name] {
		props["offset"] = map[string]any{"type": "integer", "description": "Rows to skip for paging (default 0). Use next_offset from the previous page."}
	}
	r.schemas = append(r.schemas, &ToolSpec{
		Name:        name,
		Description: description,
		InputSchema: map[string]any{
			"type":       "object",
			"properties": props,
			"required":   required,
		},
		Handler: handler,
	})
}

func (r *ToolRegistry) Definitions() []ToolDefinition {
	result := make([]ToolDefinition, len(r.schemas))
	for i, spec := range r.schemas {
		result[i] = ToolDefinition{
			Name:        spec.Name,
			Description: spec.Description,
			InputSchema: spec.InputSchema,
		}
	}
	return result
}

func (r *ToolRegistry) handler(name string) *ToolSpec {
	for _, s := range r.schemas {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// syncedGraph is implemented by repositories shared through a file. Call
// refreshes before running a tool, so changes from other processes are visible,
// and saves afterwards, so a killed server loses nothing.
type syncedGraph interface {
	Refresh() error
	Save() error
}

func (r *ToolRegistry) Call(name string, args map[string]any) (map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sg, shared := r.app.Graph.(syncedGraph)
	if shared {
		if err := sg.Refresh(); err != nil {
			fmt.Fprintln(os.Stderr, "graph refresh:", err)
		}
	}
	r.curRaw, r.curShaped, r.curTruncated = 0, 0, false
	start := time.Now()
	result, err := r.call(name, args)
	r.usage.Record(name, time.Since(start), err, r.curRaw, r.curShaped, r.curTruncated)
	if time.Since(r.lastFlush) >= usageFlushEvery {
		r.flushUsageLocked()
	}
	if shared {
		if serr := sg.Save(); serr != nil {
			fmt.Fprintln(os.Stderr, "graph save:", serr)
			if err == nil {
				return nil, fmt.Errorf("result computed but could not be saved: %w", serr)
			}
		}
	}
	return result, err
}

func (r *ToolRegistry) call(name string, args map[string]any) (map[string]any, error) {
	spec := r.handler(name)
	if spec == nil {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}

	if !globalTools[name] {
		projectID := getString(args, "project_id")
		if projectID == "" {
			return nil, fmt.Errorf("project_id is required")
		}
		var err error
		if args, err = r.resolveStableRefs(projectID, args); err != nil {
			return nil, err
		}
		if err := r.validateCrossProjectRefs(projectID, args); err != nil {
			return nil, err
		}
	}

	if !paginatedTools[name] {
		res, err := spec.Handler(args)
		if err == nil && shapedTools[name] && getString(args, "detail") != detailFull {
			r.curRaw = approxTokens(res)
			shapeResult(res, args)
			r.curShaped = approxTokens(res)
			res["approx_tokens"] = r.curShaped
		}
		return res, err
	}
	return r.callPaginated(spec, args)
}

func (r *ToolRegistry) callPaginated(spec *ToolSpec, args map[string]any) (map[string]any, error) {
	offset := getInt(args, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	limit := limitOf(args)
	if offset+limit+1 > maxFetch {
		return nil, fmt.Errorf("offset+limit exceeds %d; narrow the query instead of paging that deep", maxFetch-1)
	}
	fetch := make(map[string]any, len(args)+1)
	for k, v := range args {
		fetch[k] = v
	}
	fetch["limit"] = offset + limit + 1
	fetch["_fetch"] = true
	result, err := spec.Handler(fetch)
	if err != nil {
		return nil, err
	}
	for key, val := range result {
		list, ok := val.([]map[string]any)
		if !ok {
			continue
		}
		hasMore := len(list) > offset+limit
		if offset > len(list) {
			offset = len(list)
		}
		end := offset + limit
		if end > len(list) {
			end = len(list)
		}
		page := list[offset:end]
		r.curRaw = approxTokens(page)
		if getString(args, "detail") != detailFull {
			shaped, base := shapeRows(page, queryTerms(args))
			page = shaped
			if base != "" {
				result["path_base"] = base
			}
		}
		if kept, cut := applyBudget(page, getInt(args, "max_tokens", 0)); cut {
			page = kept
			end = offset + len(kept)
			hasMore = true
			result["truncated"] = "token budget"
			r.curTruncated = true
		}
		result[key] = page
		result["count"] = len(page)
		result["offset"] = offset
		result["has_more"] = hasMore
		if hasMore {
			result["next_offset"] = end
		}
		r.curShaped = approxTokens(page)
		result["approx_tokens"] = r.curShaped
		break
	}
	return result, nil
}

func (r *ToolRegistry) validateCrossProjectRefs(projectID string, args map[string]any) error {
	for key, value := range args {
		if strings.HasSuffix(key, "_id") && key != "project_id" {
			if nodeID, ok := value.(string); ok && nodeID != "" {
				if node, err := r.app.Graph.GetNode(nodeID); err == nil && node != nil {
					if npid, ok := node.Properties["project_id"].(string); ok && npid != projectID {
						return fmt.Errorf("%s belongs to a different project", key)
					}
				}
			}
		}
	}
	return nil
}

func getString(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt(args map[string]any, key string, defaultVal int) int {
	if v, ok := args[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case float64:
			return int(val)
		case string:
			if i, err := strconv.Atoi(val); err == nil {
				return i
			}
		}
	}
	return defaultVal
}

func getBool(args map[string]any, key string, defaultVal bool) bool {
	if v, ok := args[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
}

func getFloat(args map[string]any, key string, defaultVal float64) float64 {
	if v, ok := args[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		case string:
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				return f
			}
		}
	}
	return defaultVal
}

func getStringSlice(args map[string]any, key string) []string {
	if v, ok := args[key]; ok {
		if arr, ok := v.([]any); ok {
			result := make([]string, len(arr))
			for i, item := range arr {
				if s, ok := item.(string); ok {
					result[i] = s
				} else {
					result[i] = fmt.Sprintf("%v", item)
				}
			}
			return result
		}
	}
	return nil
}

func optsFromArgs(args map[string]any, ignore ...string) map[string]any {
	ignoreSet := make(map[string]bool)
	for _, k := range ignore {
		ignoreSet[k] = true
	}
	result := make(map[string]any)
	for k, v := range args {
		if !ignoreSet[k] {
			result[k] = v
		}
	}
	return result
}

func (r *ToolRegistry) resolveProjectID(args map[string]any) (string, *models.Node, error) {
	projectID := getString(args, "project_id")
	if projectID == "" {
		return "", nil, fmt.Errorf("project_id is required")
	}
	projects, err := r.app.Graph.FindNodes("Project", map[string]any{"id": projectID})
	if err != nil || len(projects) == 0 {
		return projectID, nil, fmt.Errorf("project must be indexed first")
	}
	return projectID, projects[0], nil
}

func (r *ToolRegistry) application() *services.Application {
	return r.app
}

// vocabulary is every tool name and parameter name, so documentation that
// mentions them is not reported as referring to missing code.
func (r *ToolRegistry) vocabulary() []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, spec := range r.schemas {
		add(spec.Name)
		if props, ok := spec.InputSchema["properties"].(map[string]any); ok {
			for k := range props {
				add(k)
			}
		}
	}
	for _, k := range []string{"has_more", "next_offset", "approx_tokens", "path_base", "detail", "max_tokens", "offset"} {
		add(k) // response fields
	}
	return out
}

// stableRefArgs are node-reference parameters. A value that looks like a stable
// ID is resolved to its node before the tool runs; an unknown one is refused with
// verified corrections instead of being executed.
var stableRefArgs = map[string]bool{
	"node_id": true, "function_id": true, "source_id": true, "target_id": true, "subject_id": true,
	"symbol_id": true, "binary_function_id": true, "source_function_id": true, "implementation_id": true,
}

func (r *ToolRegistry) resolveStableRefs(projectID string, args map[string]any) (map[string]any, error) {
	var out map[string]any
	for key, v := range args {
		if !stableRefArgs[key] {
			continue
		}
		id, ok := v.(string)
		if !ok || !ids.Looks(id) {
			continue
		}
		n, cands := r.app.Refs.Lookup(projectID, id)
		if n == nil {
			msg := fmt.Sprintf("%s: %s=%q does not exist in project %s", services.RefInvalid, key, id, projectID)
			if len(cands) > 0 {
				msg += "; did you mean: " + strings.Join(cands, ", ")
			}
			return nil, fmt.Errorf("%s", msg)
		}
		if out == nil {
			out = make(map[string]any, len(args))
			for k, val := range args {
				out[k] = val
			}
		}
		out[key] = n.ID
	}
	if out == nil {
		return args, nil
	}
	return out, nil
}
