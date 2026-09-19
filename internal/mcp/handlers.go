package mcp

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"codergag/internal/cache"
	"codergag/internal/graph"
	"codergag/internal/models"
	"codergag/internal/persona"
	"codergag/internal/privacy"
	"codergag/internal/reverse"
	"codergag/internal/services"
)

const defaultLimit = 20

func limitOf(args map[string]any) int {
	n := getInt(args, "limit", defaultLimit)
	if n <= 0 {
		return defaultLimit
	}
	max := 200
	if internal, _ := args["_fetch"].(bool); internal { // set only by callPaginated
		max = maxFetch
	}
	if n > max {
		return max
	}
	return n
}

func rows(key string, items []map[string]any) map[string]any {
	if items == nil {
		items = []map[string]any{}
	}
	return map[string]any{key: items, "count": len(items)}
}

// ---- indexing ----

func (r *ToolRegistry) handleIndexRepository(args map[string]any) (map[string]any, error) {
	return r.app.Index.IndexRepository(getString(args, "project_id"), getString(args, "path"),
		getBool(args, "incremental", true), getStringSlice(args, "ignore"))
}

func (r *ToolRegistry) handleIndexFile(args map[string]any) (map[string]any, error) {
	pid, project, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}
	path, err := filepath.Abs(getString(args, "path"))
	if err != nil {
		return nil, err
	}
	res, err := r.app.Index.IndexFile(project.ID, pid, path, true)
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": res.Path, "changed": res.Changed, "functions": res.Functions}, nil
}

// ---- symbol lookup ----

func (r *ToolRegistry) find(args map[string]any, kinds ...string) (map[string]any, error) {
	limit := limitOf(args)
	var all []map[string]any
	for _, kind := range kinds {
		found, err := r.app.Code.Find(getString(args, "project_id"), kind, getString(args, "query"), limit-len(all))
		if err != nil {
			return nil, err
		}
		all = append(all, found...)
		if len(all) >= limit {
			break
		}
	}
	return rows("results", all), nil
}

func (r *ToolRegistry) handleFindSymbol(args map[string]any) (map[string]any, error) {
	return r.find(args, "Function", "Class", "Struct", "Module")
}
func (r *ToolRegistry) handleFindFunction(args map[string]any) (map[string]any, error) {
	return r.find(args, "Function")
}
func (r *ToolRegistry) handleFindClass(args map[string]any) (map[string]any, error) {
	return r.find(args, "Class")
}
func (r *ToolRegistry) handleFindStruct(args map[string]any) (map[string]any, error) {
	return r.find(args, "Struct")
}
func (r *ToolRegistry) handleFindString(args map[string]any) (map[string]any, error) {
	return r.find(args, "String")
}
func (r *ToolRegistry) handleSearchCodeGraph(args map[string]any) (map[string]any, error) {
	res, err := r.app.Code.Search(getString(args, "project_id"), getString(args, "query"), limitOf(args), getBool(args, "include_generated", false))
	if err != nil {
		return nil, err
	}
	return rows("results", res), nil
}

func (r *ToolRegistry) handleGetFunction(args map[string]any) (map[string]any, error) {
	fn, err := r.app.Code.Function(getString(args, "project_id"), getString(args, "name"))
	if err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, fmt.Errorf("function not found: %s", getString(args, "name"))
	}
	id, _ := fn["id"].(string)
	callers, _ := r.app.Code.Related(id, "CALLS", string(graph.DirIn))
	callees, _ := r.app.Code.Related(id, "CALLS", string(graph.DirOut))
	return map[string]any{"function": fn, "callers": callers, "callees": callees}, nil
}

func (r *ToolRegistry) related(args map[string]any, key, edge string, dir graph.Direction) (map[string]any, error) {
	items, err := r.app.Code.Related(getString(args, key), edge, string(dir))
	if err != nil {
		return nil, err
	}
	return rows("results", items), nil
}

func (r *ToolRegistry) handleGetCallers(args map[string]any) (map[string]any, error) {
	return r.related(args, "function_id", "CALLS", graph.DirIn)
}
func (r *ToolRegistry) handleGetCallees(args map[string]any) (map[string]any, error) {
	return r.related(args, "function_id", "CALLS", graph.DirOut)
}

// relatedAny merges neighbors over several edge kinds, tagging each row with its relationship.
func (r *ToolRegistry) relatedAny(args map[string]any, key string, dir graph.Direction, kinds ...string) (map[string]any, error) {
	var all []map[string]any
	for _, kind := range kinds {
		items, err := r.app.Code.Related(getString(args, key), kind, string(dir))
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
	}
	return rows("results", all), nil
}

// get_references: evidence links plus code that uses the type (USES).
func (r *ToolRegistry) handleGetReferences(args map[string]any) (map[string]any, error) {
	return r.relatedAny(args, "node_id", graph.DirBoth, "REFERENCES", "USES")
}

// get_dependents/dependencies: file-level DEPENDS_ON (resolved imports) plus name-level IMPORTS.
func (r *ToolRegistry) handleGetDependents(args map[string]any) (map[string]any, error) {
	return r.relatedAny(args, "node_id", graph.DirIn, "DEPENDS_ON", "IMPORTS")
}
func (r *ToolRegistry) handleGetDependencies(args map[string]any) (map[string]any, error) {
	return r.relatedAny(args, "node_id", graph.DirOut, "DEPENDS_ON", "IMPORTS")
}

func (r *ToolRegistry) handleResolveReferences(args map[string]any) (map[string]any, error) {
	if _, _, err := r.resolveProjectID(args); err != nil {
		return nil, err
	}
	return r.app.Index.ResolveAll(getString(args, "project_id")), nil
}

func (r *ToolRegistry) handleResolveInstruction(args map[string]any) (map[string]any, error) {
	id := getString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}
	projectID, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}
	res := r.app.Refs.Resolve(projectID, id)
	m, err := structToMap(res)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (r *ToolRegistry) handleResolveBasicBlock(args map[string]any) (map[string]any, error) {
	id := getString(args, "id")
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}
	projectID, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}
	res := r.app.Refs.Resolve(projectID, id)
	m, err := structToMap(res)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (r *ToolRegistry) handleTraceCallPath(args map[string]any) (map[string]any, error) {
	return r.app.Code.Trace(getString(args, "source_id"), getString(args, "target_id"), getInt(args, "max_depth", 8))
}

func (r *ToolRegistry) handleTraceDataFlow(args map[string]any) (map[string]any, error) {
	res, err := r.app.Code.TraceDataFlow(getString(args, "source_id"), getString(args, "target_id"), getInt(args, "max_depth", 8), true)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (r *ToolRegistry) handleImpactAnalysis(args map[string]any) (map[string]any, error) {
	return r.app.Code.Impact(getString(args, "node_id"), getInt(args, "depth", 3), limitOf(args))
}

func (r *ToolRegistry) handleFindRelatedCode(args map[string]any) (map[string]any, error) {
	id := getString(args, "node_id")
	var all []map[string]any
	for _, dir := range []graph.Direction{graph.DirOut, graph.DirIn} {
		items, err := r.app.Code.Related(id, "CALLS", string(dir))
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
	}
	return rows("results", all), nil
}

func (r *ToolRegistry) handleFindSimilarFunctions(args map[string]any) (map[string]any, error) {
	pid := getString(args, "project_id")
	node, err := r.app.Graph.GetNode(getString(args, "function_id"))
	if err != nil {
		return nil, err
	}
	pc, _ := node.Properties["parameter_count"].(int)
	lang, _ := node.Properties["language"].(string)
	similar, err := r.app.Analysis.Signature(pid, &pc, lang, limitOf(args)+1)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for _, s := range similar {
		if s["id"] != node.ID {
			out = append(out, s)
		}
	}
	return rows("results", out), nil
}

func (r *ToolRegistry) handleGetTypeHierarchy(args map[string]any) (map[string]any, error) {
	return r.app.Code.Hierarchy(getString(args, "node_id"), getString(args, "direction"), getInt(args, "depth", 3))
}

func (r *ToolRegistry) handleGetSubsystem(args map[string]any) (map[string]any, error) {
	return r.app.Analysis.ModuleSummary(getString(args, "project_id"), getString(args, "name"), limitOf(args))
}

func (r *ToolRegistry) handleGetArchitecture(args map[string]any) (map[string]any, error) {
	pid := getString(args, "project_id")
	summary, err := r.app.Analysis.ModuleSummary(pid, "", limitOf(args))
	if err != nil {
		return nil, err
	}
	entries, _ := r.app.Analysis.EntryPoints(pid, 10)
	cycles, _ := r.app.Analysis.CircularDependencies(pid, 10)
	return map[string]any{"modules": summary, "entry_points": entries, "circular_dependencies": cycles}, nil
}

// ---- symbol mutation ----

func (r *ToolRegistry) ownedNode(pid, id string) (*models.Node, error) {
	n, err := r.app.Graph.GetNode(id)
	if err != nil || n == nil || n.Properties["project_id"] != pid {
		return nil, fmt.Errorf("symbol is absent or belongs to another project")
	}
	return n, nil
}

func (r *ToolRegistry) handleRenameSymbol(args map[string]any) (map[string]any, error) {
	n, err := r.ownedNode(getString(args, "project_id"), getString(args, "symbol_id"))
	if err != nil {
		return nil, err
	}
	old, _ := n.Properties["name"].(string)
	n.SetProperty("name", getString(args, "name"))
	n.SetProperty("previous_name", old)
	n.SetProperty("renamed_by", getString(args, "agent"))
	if _, err := r.app.Graph.UpsertNode(n.Kind, map[string]any{"id": n.ID, "project_id": n.Properties["project_id"]}, n.Properties); err != nil {
		return nil, err
	}
	return map[string]any{"symbol_id": n.ID, "name": getString(args, "name"), "previous_name": old}, nil
}

func (r *ToolRegistry) handleUpdateSymbol(args map[string]any) (map[string]any, error) {
	n, err := r.ownedNode(getString(args, "project_id"), getString(args, "symbol_id"))
	if err != nil {
		return nil, err
	}
	props, _ := args["properties"].(map[string]any)
	for k, v := range props {
		switch k {
		case "id", "project_id", "created_at", "updated_at":
			continue
		}
		n.SetProperty(k, v)
	}
	if _, err := r.app.Graph.UpsertNode(n.Kind, map[string]any{"id": n.ID, "project_id": n.Properties["project_id"]}, n.Properties); err != nil {
		return nil, err
	}
	return map[string]any{"symbol_id": n.ID, "updated": len(props)}, nil
}

func (r *ToolRegistry) handleQueryGraph(args map[string]any) (map[string]any, error) {
	q := strings.TrimSpace(getString(args, "query"))
	up := strings.ToUpper(q)
	if !(strings.HasPrefix(up, "SELECT") || strings.HasPrefix(up, "MATCH")) {
		return nil, fmt.Errorf("only read-only SELECT or MATCH queries are allowed")
	}
	if !strings.Contains(q, ":project_id") {
		return nil, fmt.Errorf("query must filter on :project_id")
	}
	params, _ := args["params"].(map[string]any)
	if params == nil {
		params = map[string]any{}
	}
	params["project_id"] = getString(args, "project_id")
	res, err := r.app.Graph.QueryReadonly(q, params)
	if err != nil {
		return nil, err
	}
	return rows("rows", res), nil
}

// ---- analysis ----

func (r *ToolRegistry) handleAnalyzeComplexity(args map[string]any) (map[string]any, error) {
	return r.app.Analysis.Complexity(getString(args, "project_id"), getString(args, "function_id"))
}

func (r *ToolRegistry) handleFindCircularDeps(args map[string]any) (map[string]any, error) {
	cycles, err := r.app.Analysis.CircularDependencies(getString(args, "project_id"), limitOf(args))
	if err != nil {
		return nil, err
	}
	return map[string]any{"cycles": cycles, "count": len(cycles)}, nil
}

func (r *ToolRegistry) handleFindHotPaths(args map[string]any) (map[string]any, error) {
	res, err := r.app.Analysis.HotPaths(getString(args, "project_id"), limitOf(args))
	if err != nil {
		return nil, err
	}
	return rows("results", res), nil
}

func (r *ToolRegistry) handleFindDeadImports(args map[string]any) (map[string]any, error) {
	res, err := r.app.Analysis.DeadImports(getString(args, "project_id"), limitOf(args))
	if err != nil {
		return nil, err
	}
	return rows("results", res), nil
}

func (r *ToolRegistry) handleGetModuleSummary(args map[string]any) (map[string]any, error) {
	return r.app.Analysis.ModuleSummary(getString(args, "project_id"), getString(args, "query"), limitOf(args))
}

func (r *ToolRegistry) handleFindBySignature(args map[string]any) (map[string]any, error) {
	var pc *int
	if _, ok := args["parameter_count"]; ok {
		n := getInt(args, "parameter_count", 0)
		pc = &n
	}
	res, err := r.app.Analysis.Signature(getString(args, "project_id"), pc, getString(args, "language"), limitOf(args))
	if err != nil {
		return nil, err
	}
	return rows("results", res), nil
}

func (r *ToolRegistry) handleFindEntryPoints(args map[string]any) (map[string]any, error) {
	res, err := r.app.Analysis.EntryPoints(getString(args, "project_id"), limitOf(args))
	if err != nil {
		return nil, err
	}
	return rows("results", res), nil
}

func (r *ToolRegistry) handleFindRelatedTests(args map[string]any) (map[string]any, error) {
	res, err := r.app.Analysis.RelatedTests(getString(args, "project_id"), getString(args, "function_name"), limitOf(args))
	if err != nil {
		return nil, err
	}
	return rows("results", res), nil
}

// ---- evidence / reverse engineering ----

func (r *ToolRegistry) handleGetHypotheses(args map[string]any) (map[string]any, error) {
	res, err := r.app.Evidence.GetHypotheses(getString(args, "subject_id"))
	if err != nil {
		return nil, err
	}
	return rows("hypotheses", res), nil
}

func (r *ToolRegistry) handleGetEvidence(args map[string]any) (map[string]any, error) {
	res, err := r.app.Evidence.GetEvidence(getString(args, "subject_id"))
	if err != nil {
		return nil, err
	}
	return rows("evidence", res), nil
}

func (r *ToolRegistry) handleRecordEvidence(args map[string]any) (map[string]any, error) {
	return r.app.Evidence.RecordEvidence(getString(args, "project_id"), getString(args, "subject_id"),
		getString(args, "description"), optsFromArgs(args, "project_id", "subject_id", "description"))
}

func (r *ToolRegistry) handleRecordObservation(args map[string]any) (map[string]any, error) {
	opts := optsFromArgs(args, "project_id", "subject_id", "description")
	opts["kind"] = "observation"
	if _, ok := opts["confidence"]; !ok {
		opts["confidence"] = 1.0
	}
	return r.app.Evidence.RecordEvidence(getString(args, "project_id"), getString(args, "subject_id"),
		getString(args, "description"), opts)
}

func (r *ToolRegistry) handleRecordRuntimeTrace(args map[string]any) (map[string]any, error) {
	raw, _ := args["trace"].(map[string]any)
	if raw == nil {
		return nil, fmt.Errorf("trace object is required")
	}
	trace := reverse.RuntimeTrace{
		BinaryID:     getString(raw, "binary_id"),
		FunctionAddr: getString(raw, "function_addr"),
		TraceID:      getString(raw, "trace_id"),
		Timestamp:    int64(getInt(raw, "timestamp", 0)),
	}
	instrs, _ := raw["instructions"].([]any)
	for _, i := range instrs {
		m, ok := i.(map[string]any)
		if !ok {
			continue
		}
		regs := make(map[string]string)
		if r, ok := m["registers"].(map[string]any); ok {
			for k, v := range r {
				regs[k] = fmt.Sprintf("%v", v)
			}
		}
		trace.Instructions = append(trace.Instructions, reverse.TraceInstruction{
			Address:   getString(m, "address"),
			Mnemonic:  getString(m, "mnemonic"),
			Operands:  getString(m, "operands"),
			Registers: regs,
		})
	}
	reads, _ := raw["memory_reads"].([]any)
	for _, rd := range reads {
		m, ok := rd.(map[string]any)
		if !ok {
			continue
		}
		trace.MemoryReads = append(trace.MemoryReads, reverse.TraceMemoryAccess{
			Address: getString(m, "address"),
			Size:    getInt(m, "size", 0),
			Value:   getString(m, "value"),
		})
	}
	writes, _ := raw["memory_writes"].([]any)
	for _, wr := range writes {
		m, ok := wr.(map[string]any)
		if !ok {
			continue
		}
		trace.MemoryWrites = append(trace.MemoryWrites, reverse.TraceMemoryAccess{
			Address: getString(m, "address"),
			Size:    getInt(m, "size", 0),
			Value:   getString(m, "value"),
		})
	}
	return r.app.Reverse.RecordRuntimeTrace(getString(args, "project_id"), trace)
}

func (r *ToolRegistry) handleRecordHypothesis(args map[string]any) (map[string]any, error) {
	raw, _ := args["hypothesis"].(map[string]any)
	if raw == nil {
		return nil, fmt.Errorf("hypothesis object is required")
	}
	hyp := reverse.Hypothesis{
		ID:         getString(raw, "id"),
		SubjectID:  getString(raw, "subject_id"),
		Claim:      getString(raw, "claim"),
		Confidence: getFloat(raw, "confidence", 0),
		Status:     getString(raw, "status"),
		Analyst:    getString(raw, "analyst"),
		CreatedAt:  int64(getInt(raw, "created_at", 0)),
		UpdatedAt:  int64(getInt(raw, "updated_at", 0)),
	}
	evFor, _ := raw["evidence_for"].([]any)
	for _, e := range evFor {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		hyp.EvidenceFor = append(hyp.EvidenceFor, reverse.EvidenceRef{
			ID:          getString(m, "id"),
			Description: getString(m, "description"),
			Confidence:  getFloat(m, "confidence", 0),
			Kind:        getString(m, "kind"),
		})
	}
	evAgainst, _ := raw["evidence_against"].([]any)
	for _, e := range evAgainst {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		hyp.EvidenceAgainst = append(hyp.EvidenceAgainst, reverse.EvidenceRef{
			ID:          getString(m, "id"),
			Description: getString(m, "description"),
			Confidence:  getFloat(m, "confidence", 0),
			Kind:        getString(m, "kind"),
		})
	}
	return r.app.Reverse.RecordHypothesis(getString(args, "project_id"), hyp)
}

func (r *ToolRegistry) handleRecordBehavioralEquivalence(args map[string]any) (map[string]any, error) {
	raw, _ := args["equivalence"].(map[string]any)
	if raw == nil {
		return nil, fmt.Errorf("equivalence object is required")
	}
	equiv := reverse.BehavioralEquivalence{
		BinaryFunctionID:  getString(raw, "binary_function_id"),
		SourceFunctionID:  getString(raw, "source_function_id"),
		EquivalenceStatus: getString(raw, "equivalence_status"),
		Confidence:        getFloat(raw, "confidence", 0),
		Method:            getString(raw, "method"),
		Analyst:           getString(raw, "analyst"),
		CreatedAt:         int64(getInt(raw, "created_at", 0)),
	}
	testCases, _ := raw["test_cases"].([]any)
	for _, tc := range testCases {
		m, ok := tc.(map[string]any)
		if !ok {
			continue
		}
		equiv.TestCases = append(equiv.TestCases, reverse.EquivalenceTestCase{
			Input:     m["input"].(map[string]any),
			BinaryOut: m["binary_out"].(map[string]any),
			SourceOut: m["source_out"].(map[string]any),
			Match:     getBool(m, "match", false),
		})
	}
	diffs, _ := raw["differences"].([]any)
	for _, d := range diffs {
		m, ok := d.(map[string]any)
		if !ok {
			continue
		}
		equiv.Differences = append(equiv.Differences, reverse.EquivalenceDifference{
			Type:        getString(m, "type"),
			Description: getString(m, "description"),
			Severity:    getString(m, "severity"),
		})
	}
	return r.app.Reverse.RecordBehavioralEquivalence(getString(args, "project_id"), equiv)
}

func (r *ToolRegistry) handleIndexBinary(args map[string]any) (map[string]any, error) {
	raw, _ := args["binary"].(map[string]any)
	if raw == nil {
		return nil, fmt.Errorf("binary object is required")
	}
	bin := reverse.NormalizedBinary{
		BinaryID: getString(raw, "binary_id"),
		Path:     getString(raw, "path"),
		Sha256:   getString(raw, "sha256"),
		Tool:     getString(raw, "tool"),
	}
	if bin.BinaryID == "" {
		return nil, fmt.Errorf("binary.binary_id is required")
	}
	fns, _ := raw["functions"].([]any)
	for _, f := range fns {
		m, ok := f.(map[string]any)
		if !ok {
			continue
		}
		fn := reverse.NormalizedFunction{
			Address:          getString(m, "address"),
			Name:             getString(m, "name"),
			Calls:            getStringSlice(m, "calls"),
			Strings:          getStringSlice(m, "strings"),
			DecompilerOutput: getString(m, "decompiler_output"),
			DataReferences:   parseDataRefs(getInterfaceSlice(m, "data_references")),
			CodeReferences:   parseCodeRefs(getInterfaceSlice(m, "code_references")),
			BasicBlocks:      parseBasicBlocks(getInterfaceSlice(m, "basic_blocks")),
		}
		if _, ok := m["size"]; ok {
			n := getInt(m, "size", 0)
			fn.Size = &n
		}
		bin.Functions = append(bin.Functions, fn)
	}
	return r.app.Reverse.ImportBinary(getString(args, "project_id"), bin)
}

func parseDataRefs(arr []any) []reverse.NormalizedDataRef {
	var refs []reverse.NormalizedDataRef
	for _, r := range arr {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		refs = append(refs, reverse.NormalizedDataRef{
			FromAddress: getString(m, "from_address"),
			ToAddress:   getString(m, "to_address"),
			Type:        getString(m, "type"),
			Size:        getInt(m, "size", 0),
		})
	}
	return refs
}

func parseCodeRefs(arr []any) []reverse.NormalizedCodeRef {
	var refs []reverse.NormalizedCodeRef
	for _, r := range arr {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		refs = append(refs, reverse.NormalizedCodeRef{
			FromAddress: getString(m, "from_address"),
			ToAddress:   getString(m, "to_address"),
			Type:        getString(m, "type"),
		})
	}
	return refs
}

func parseBasicBlocks(arr []any) []reverse.NormalizedBasicBlock {
	var bbs []reverse.NormalizedBasicBlock
	for _, b := range arr {
		m, ok := b.(map[string]any)
		if !ok {
			continue
		}
		bb := reverse.NormalizedBasicBlock{
			Address: getString(m, "address"),
		}
		insns, _ := m["instructions"].([]any)
		for _, i := range insns {
			im, ok := i.(map[string]any)
			if !ok {
				continue
			}
			insn := reverse.NormalizedInstruction{
				Address:  getString(im, "address"),
				Mnemonic: getString(im, "mnemonic"),
				Operands: getString(im, "operands"),
				DataRefs: parseDataRefs(getInterfaceSlice(im, "data_refs")),
				CodeRefs: parseCodeRefs(getInterfaceSlice(im, "code_refs")),
			}
			bb.Instruction = append(bb.Instruction, insn)
		}
		bbs = append(bbs, bb)
	}
	return bbs
}

func getInterfaceSlice(m map[string]any, key string) []any {
	if v, ok := m[key]; ok {
		if arr, ok := v.([]any); ok {
			return arr
		}
	}
	return nil
}

func (r *ToolRegistry) handleMapBinaryFunction(args map[string]any) (map[string]any, error) {
	return r.app.Reverse.MapSourceFunction(getString(args, "binary_function_id"), getString(args, "source_function_id"),
		getFloat(args, "confidence", 0), getString(args, "method"))
}

func (r *ToolRegistry) handleRecordPort(args map[string]any) (map[string]any, error) {
	return r.app.Reverse.RecordPort(getString(args, "binary_function_id"), getString(args, "implementation_id"),
		getString(args, "language"), getFloat(args, "confidence", 0))
}

func (r *ToolRegistry) handleRecordValidation(args map[string]any) (map[string]any, error) {
	return r.app.Reverse.RecordValidation(getString(args, "binary_function_id"), getString(args, "implementation_id"),
		getString(args, "test_name"), getString(args, "status"), getFloat(args, "confidence", 0), getString(args, "method"))
}

// ---- memory / documents / git ----

func (r *ToolRegistry) handleMemoryStore(args map[string]any) (map[string]any, error) {
	return r.app.Memory.Store(getString(args, "project_id"), getString(args, "title"), getString(args, "content"),
		optsFromArgs(args, "project_id", "title", "content"))
}

func (r *ToolRegistry) handleMemoryGet(args map[string]any) (map[string]any, error) {
	return r.app.Memory.Get(getString(args, "project_id"), getString(args, "title"))
}

func (r *ToolRegistry) handleMemorySearch(args map[string]any) (map[string]any, error) {
	res, err := r.app.Memory.SearchAll(getString(args, "project_id"), getString(args, "query"), limitOf(args), getBool(args, "include_archived", false))
	if err != nil {
		return nil, err
	}
	return rows("results", res), nil
}

func (r *ToolRegistry) handleMemoryCompact(args map[string]any) (map[string]any, error) {
	return r.app.Memory.Compact(getString(args, "project_id"), getInt(args, "min_group", 2))
}

func (r *ToolRegistry) handleIndexMarkdown(args map[string]any) (map[string]any, error) {
	return r.app.Documents.IndexMarkdown(getString(args, "project_id"), getString(args, "path"))
}

func (r *ToolRegistry) handleIndexDocument(args map[string]any) (map[string]any, error) {
	return r.app.Documents.IndexDocument(getString(args, "project_id"), getString(args, "path"))
}

func (r *ToolRegistry) handleSearchDocs(args map[string]any) (map[string]any, error) {
	res, err := r.app.Documents.Search(getString(args, "project_id"), getString(args, "query"), limitOf(args))
	if err != nil {
		return nil, err
	}
	return rows("results", res), nil
}

func (r *ToolRegistry) handleListDocSources(args map[string]any) (map[string]any, error) {
	res, err := r.app.Documents.ListSources(getString(args, "project_id"))
	if err != nil {
		return nil, err
	}
	return rows("sources", res), nil
}

func (r *ToolRegistry) handleRemoveDocSource(args map[string]any) (map[string]any, error) {
	return r.app.Documents.RemoveSource(getString(args, "project_id"), getString(args, "document_id"))
}

func (r *ToolRegistry) handleVerifyDesign(args map[string]any) (map[string]any, error) {
	return r.app.Documents.VerifyDesign(getString(args, "project_id"), getString(args, "document_id"))
}

func (r *ToolRegistry) handleArchitectureReport(args map[string]any) (map[string]any, error) {
	return r.app.ArchitectureReport(getString(args, "project_id"), getInt(args, "depth", 0), getInt(args, "limit", 0))
}

func (r *ToolRegistry) handleCheckDocs(args map[string]any) (map[string]any, error) {
	return r.app.Documents.CheckDocs(getString(args, "project_id"), limitOf(args))
}

func (r *ToolRegistry) handlePrContext(args map[string]any) (map[string]any, error) {
	return r.app.Git.PrContext(getString(args, "project_id"), getString(args, "base"), limitOf(args))
}

func (r *ToolRegistry) handleReviewSuggestions(args map[string]any) (map[string]any, error) {
	return map[string]any{"reviewers": r.app.Git.ReviewSuggestions(getString(args, "project_id"),
		getStringSlice(args, "files"), limitOf(args))}, nil
}

// ---- security ----

func (r *ToolRegistry) handleAuditSecurity(args map[string]any) (map[string]any, error) {
	return r.app.Security.AuditProject(getString(args, "project_id"), getString(args, "path"),
		getBool(args, "cache", false), getString(args, "agent"))
}

func (r *ToolRegistry) handleFindVulnerabilities(args map[string]any) (map[string]any, error) {
	res, err := r.app.Security.FindByCWE(getString(args, "project_id"), getString(args, "cwe"), limitOf(args))
	if err != nil {
		return nil, err
	}
	return rows("findings", res), nil
}

func (r *ToolRegistry) handleGetFindings(args map[string]any) (map[string]any, error) {
	res, err := r.app.Security.GetFindings(getString(args, "project_id"), limitOf(args))
	if err != nil {
		return nil, err
	}
	return rows("findings", res), nil
}

func (r *ToolRegistry) handleGetCWEDetails(args map[string]any) (map[string]any, error) {
	return r.app.Security.GetCWEDetails(getString(args, "cwe"))
}

func (r *ToolRegistry) handleGetSecurityMetrics(args map[string]any) (map[string]any, error) {
	return r.app.Security.GetMetrics(getString(args, "project_id"))
}

// ---- team ----

func mapSlice(v any) []map[string]any {
	arr, _ := v.([]any)
	var out []map[string]any
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func (r *ToolRegistry) handleTeamCreate(args map[string]any) (map[string]any, error) {
	return r.app.Team.CreateTeam(getString(args, "project_id"), getString(args, "name"),
		getString(args, "owner_agent"), mapSlice(args["members"]))
}

func (r *ToolRegistry) handleListTeams(args map[string]any) (map[string]any, error) {
	res, err := r.app.Team.ListTeams(getString(args, "project_id"))
	if err != nil {
		return nil, err
	}
	return rows("teams", res), nil
}

func (r *ToolRegistry) handleListAgents(args map[string]any) (map[string]any, error) {
	res, err := r.app.Team.ListAgents(getString(args, "project_id"), getString(args, "team_id"))
	if err != nil {
		return nil, err
	}
	return rows("agents", res), nil
}

func (r *ToolRegistry) handleListRoles(args map[string]any) (map[string]any, error) {
	return rows("roles", r.app.Team.ListRoles()), nil
}

func (r *ToolRegistry) handleAssignTask(args map[string]any) (map[string]any, error) {
	role := persona.Role(getString(args, "role"))
	if _, ok := persona.BuiltInRoles[role]; !ok {
		return nil, fmt.Errorf("unknown role: %s", role)
	}
	return r.app.Team.AssignTask(getString(args, "project_id"), getString(args, "team_id"), getString(args, "title"),
		getString(args, "description"), role, getString(args, "assignee"), getString(args, "priority"), getString(args, "due_date"))
}

func (r *ToolRegistry) handleUpdateTask(args map[string]any) (map[string]any, error) {
	return r.app.Team.UpdateTask(getString(args, "project_id"), getString(args, "task_id"), getString(args, "status"))
}

func (r *ToolRegistry) handleGetTasks(args map[string]any) (map[string]any, error) {
	res, err := r.app.Team.GetTasks(getString(args, "project_id"), getString(args, "team_id"), getString(args, "status"))
	if err != nil {
		return nil, err
	}
	if lim := limitOf(args); len(res) > lim {
		res = res[:lim]
	}
	return rows("tasks", res), nil
}

func (r *ToolRegistry) handleShareKnowledge(args map[string]any) (map[string]any, error) {
	return r.app.Team.ShareKnowledge(getString(args, "project_id"), getString(args, "from_agent"), getString(args, "to_agent"),
		getString(args, "to_role"), getString(args, "title"), getString(args, "content"), getStringSlice(args, "scopes"))
}

func (r *ToolRegistry) handleGetTeamContext(args map[string]any) (map[string]any, error) {
	return r.app.Team.GetTeamContext(getString(args, "project_id"), getString(args, "team_id"),
		getStringSlice(args, "scopes"), limitOf(args))
}

// ---- cache ----

func (r *ToolRegistry) cacheManager() (*cache.CacheManager, error) {
	if r.app.Cache == nil {
		return nil, fmt.Errorf("cache is disabled")
	}
	return r.app.Cache, nil
}

func (r *ToolRegistry) handleCacheLookup(args map[string]any) (map[string]any, error) {
	cm, err := r.cacheManager()
	if err != nil {
		return nil, err
	}
	tool := getString(args, "tool_name")
	if tool == "" {
		return nil, fmt.Errorf("tool_name is required for lookup")
	}
	callArgs, _ := args["arguments"].(map[string]any)
	res, err := cm.CheckCache(getString(args, "project_id"), "", getString(args, "git_commit"),
		getString(args, "binary_hash"), tool, callArgs)
	if err != nil {
		return nil, err
	}
	return structToMap(res)
}

func (r *ToolRegistry) handleCacheStore(args map[string]any) (map[string]any, error) {
	cm, err := r.cacheManager()
	if err != nil {
		return nil, err
	}
	callArgs, _ := args["arguments"].(map[string]any)
	result, _ := args["result"].(map[string]any)
	if err := cm.StoreExactCache(getString(args, "project_id"), "", getString(args, "git_commit"),
		getString(args, "binary_hash"), getString(args, "tool_name"), callArgs, result,
		getFloat(args, "confidence", 1), getString(args, "agent")); err != nil {
		return nil, err
	}
	return map[string]any{"status": "stored"}, nil
}

func (r *ToolRegistry) invalidate(args map[string]any) error {
	cm, err := r.cacheManager()
	if err != nil {
		return err
	}
	return cm.Invalidate(cache.InvalidateArgs{
		ProjectID:  getString(args, "project_id"),
		CacheKey:   getString(args, "cache_key"),
		CacheID:    getString(args, "cache_id"),
		ToolName:   getString(args, "tool_name"),
		Commit:     getString(args, "commit"),
		BinaryHash: getString(args, "binary_hash"),
		File:       getString(args, "file"),
		Artifact:   getString(args, "artifact"),
	})
}

func (r *ToolRegistry) handleCacheInvalidate(args map[string]any) (map[string]any, error) {
	if err := r.invalidate(args); err != nil {
		return nil, err
	}
	return map[string]any{"status": "invalidated"}, nil
}

func (r *ToolRegistry) handleCacheStats(args map[string]any) (map[string]any, error) {
	cm, err := r.cacheManager()
	if err != nil {
		return nil, err
	}
	return map[string]any{"stats": cm.Stats()}, nil
}

func (r *ToolRegistry) handleCacheFlush(args map[string]any) (map[string]any, error) {
	cm, err := r.cacheManager()
	if err != nil {
		return nil, err
	}
	n, err := cm.Flush(getString(args, "project_id"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "flushed", "entries_removed": n}, nil
}

func (r *ToolRegistry) handleCacheConfig(args map[string]any) (map[string]any, error) {
	cm, err := r.cacheManager()
	if err != nil {
		return nil, err
	}
	cfg := cm.Config()
	return map[string]any{"config": map[string]any{
		"enabled":     cfg.Enabled,
		"exact":       cfg.Exact,
		"semantic":    cfg.Semantic,
		"tool":        cfg.Tool,
		"analysis":    cfg.Analysis,
		"ttl_enabled": cfg.TTL.Enabled,
		"ttl_seconds": cfg.TTL.Seconds,
	}}, nil
}

func (r *ToolRegistry) cacheNodes(args map[string]any) ([]map[string]any, error) {
	filters := map[string]any{"project_id": getString(args, "project_id")}
	if k := getString(args, "cache_key"); k != "" {
		filters["cache_key"] = k
	}
	if id := getString(args, "cache_id"); id != "" {
		filters["id"] = id
	}
	nodes, err := r.app.Graph.FindNodes("CacheEntry", filters)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, services.Present(n))
	}
	return out, nil
}

func (r *ToolRegistry) handleCacheExplain(args map[string]any) (map[string]any, error) {
	entries, err := r.cacheNodes(args)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no matching cache entry")
	}
	e := entries[0]
	return map[string]any{
		"entry":       e,
		"explanation": fmt.Sprintf("tool %v cached at %v with freshness %v", e["tool_name"], e["created_at"], e["freshness"]),
	}, nil
}

func (r *ToolRegistry) handleGetCachedAnalysis(args map[string]any) (map[string]any, error) {
	filters := map[string]any{"project_id": getString(args, "project_id")}
	if t := getString(args, "artifact_type"); t != "" {
		filters["artifact_type"] = t
	}
	if id := getString(args, "artifact_id"); id != "" {
		filters["id"] = id
	}
	nodes, err := r.app.Graph.FindNodes("AnalysisArtifact", filters)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, services.Present(n))
	}
	return rows("artifacts", out), nil
}

func (r *ToolRegistry) handleRefreshAnalysis(args map[string]any) (map[string]any, error) {
	if err := r.invalidate(map[string]any{
		"project_id": getString(args, "project_id"),
		"tool_name":  getString(args, "tool_name"),
	}); err != nil {
		return nil, err
	}
	return map[string]any{"status": "invalidated", "note": "rerun the tool to repopulate the cache"}, nil
}

func (r *ToolRegistry) handleEnsureFresh(args map[string]any) (map[string]any, error) {
	entries, err := r.cacheNodes(args)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return map[string]any{"fresh": false, "reason": "missing"}, nil
	}
	e := entries[0]
	stale := ""
	if c := getString(args, "git_commit"); c != "" && e["git_commit"] != c {
		stale = "git commit changed"
	} else if h := getString(args, "binary_hash"); h != "" && e["binary_hash"] != h {
		stale = "binary hash changed"
	}
	if stale != "" {
		if err := r.invalidate(map[string]any{"project_id": getString(args, "project_id"), "cache_key": getString(args, "cache_key")}); err != nil {
			return nil, err
		}
		return map[string]any{"fresh": false, "reason": stale, "invalidated": true}, nil
	}
	return map[string]any{"fresh": true}, nil
}

func (r *ToolRegistry) compiledContext(args map[string]any, level int, explain bool) (map[string]any, error) {
	req := services.ContextRequest{
		ProjectID: getString(args, "project_id"),
		Question:  getString(args, "question"),
		TaskID:    getString(args, "task_id"),
		TeamID:    getString(args, "team_id"),
		Scopes:    getStringSlice(args, "scopes"),
		Limit:     limitOf(args),
		MaxTokens: getInt(args, "max_tokens", 0),
		Level:     level,
		Explain:   explain,
	}
	if lvl, ok := args["level"]; ok && level != services.CtxLevelProvenance {
		if n, ok := lvl.(float64); ok {
			req.Level = int(n)
		}
	}
	ctx, err := r.app.Context.Compile(req)
	if err != nil {
		return nil, err
	}
	return structToMap(ctx)
}

func (r *ToolRegistry) handlePrepareContext(args map[string]any) (map[string]any, error) {
	return r.compiledContext(args, services.CtxLevelStructured, false)
}

func (r *ToolRegistry) handleExplainContext(args map[string]any) (map[string]any, error) {
	return r.compiledContext(args, services.CtxLevelProvenance, true)
}

// ---- verification runner ----

func (r *ToolRegistry) handleRunVerification(args map[string]any) (map[string]any, error) {
	envRaw, _ := args["env"].(map[string]any)
	env := make(map[string]string, len(envRaw))
	for k, v := range envRaw {
		env[k] = fmt.Sprintf("%v", v)
	}
	req := services.VerifyCommandRequest{
		ProjectID:    getString(args, "project_id"),
		Command:      getString(args, "command"),
		Args:         getStringSlice(args, "args"),
		Dir:          getString(args, "dir"),
		Env:          env,
		RecordResult: getBool(args, "record_result", false),
		Agent:        getString(args, "agent"),
	}
	res, err := r.app.Verify.Run(req)
	if err != nil {
		return nil, err
	}
	return structToMap(res)
}

func (r *ToolRegistry) handleListVerificationRuns(args map[string]any) (map[string]any, error) {
	runs, err := r.app.Verify.ListRuns(getString(args, "project_id"), limitOf(args))
	if err != nil {
		return nil, err
	}
	return rows("runs", runs), nil
}

func (r *ToolRegistry) handleListAllowedVerifications(_ map[string]any) (map[string]any, error) {
	tools := r.app.Verify.AllowedTools()
	return map[string]any{"allowed": tools, "note": "Add commands to the verification.allowed_commands config to expand the allowlist."}, nil
}

func (r *ToolRegistry) privacyService() *services.PrivacyService {
	return r.app.Privacy
}

func (r *ToolRegistry) handleSanitizeContext(args map[string]any) (map[string]any, error) {
	content := getString(args, "content")
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	projectID := getString(args, "project_id")
	svc := r.privacyService()
	policy := svc.Policy(projectID)
	if mode := getString(args, "mode"); mode != "" {
		parsed, err := privacy.ParseMode(mode)
		if err != nil {
			return nil, err
		}
		policy.Mode = parsed // per-request override; the stored policy is unchanged
	}
	fw := svc.FirewallWithPolicy(policy)
	if dest := getString(args, "destination"); dest != "" {
		return structToMap(fw.AuditTransmission(dest, content))
	}
	return structToMap(fw.SanitizeContext(content))
}

func (r *ToolRegistry) handleRedactContent(args map[string]any) (map[string]any, error) {
	content := getString(args, "content")
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	return structToMap(r.privacyService().Redact(getString(args, "project_id"), content))
}

func (r *ToolRegistry) handlePseudonymizeSymbol(args map[string]any) (map[string]any, error) {
	name := getString(args, "name")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	kind := privacy.PseudonymKind(getString(args, "kind"))
	if kind == "" {
		kind = privacy.ClassifySymbol(name)
	}
	token, err := r.privacyService().Pseudonymize(getString(args, "project_id"), name, kind)
	if err != nil {
		return nil, err
	}
	return map[string]any{"name": name, "kind": string(kind), "pseudonym": token}, nil
}

func (r *ToolRegistry) handleAuditTransmission(args map[string]any) (map[string]any, error) {
	content := getString(args, "content")
	dest := getString(args, "destination")
	if content == "" || dest == "" {
		return nil, fmt.Errorf("content and destination are required")
	}
	return structToMap(r.privacyService().AuditTransmission(getString(args, "project_id"), dest, content))
}

func (r *ToolRegistry) handlePrivacyPolicy(args map[string]any) (map[string]any, error) {
	projectID := getString(args, "project_id")
	svc := r.privacyService()
	policy := svc.Policy(projectID)
	changed := false
	if mode := getString(args, "mode"); mode != "" {
		parsed, err := privacy.ParseMode(mode)
		if err != nil {
			return nil, err
		}
		policy.Mode = parsed
		changed = true
	}
	if v, ok := args["allow_exact_source"].(bool); ok {
		policy.AllowExactSource, changed = v, true
	}
	if v, ok := args["allow_strings"].(bool); ok {
		policy.AllowStrings, changed = v, true
	}
	if v := getInt(args, "max_source_bytes", -1); v >= 0 {
		policy.MaxSourceBytes, changed = v, true
	}
	if v := getInt(args, "max_context_tokens", -1); v >= 0 {
		policy.MaxContextTokens, changed = v, true
	}
	if v := getStringSlice(args, "allowed_identifiers"); v != nil {
		policy.AllowedIdentifiers, changed = v, true
	}
	if v := getStringSlice(args, "forbidden_identifiers"); v != nil {
		policy.ForbiddenIdentifiers, changed = v, true
	}
	if v := getStringSlice(args, "allowed_paths"); v != nil {
		policy.AllowedPaths, changed = v, true
	}
	if v := getStringSlice(args, "forbidden_paths"); v != nil {
		policy.ForbiddenPaths, changed = v, true
	}
	if changed {
		policy.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := svc.SetPolicy(policy); err != nil {
			return nil, err
		}
	}
	return structToMap(policy)
}

func (r *ToolRegistry) handleListLanguages(args map[string]any) (map[string]any, error) {
	langs := services.SupportedLanguages()
	return map[string]any{"count": len(langs), "languages": langs}, nil
}
