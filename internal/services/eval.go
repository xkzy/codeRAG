package services

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"codergag/internal/models"
	"codergag/internal/search"
)

// Metric is one measured aspect of quality. Score is 0..1; N/A metrics carry
// no score and are left out of the overall grade.
type Metric struct {
	Name   string  `json:"name"`
	Score  float64 `json:"score"`
	Weight float64 `json:"weight"`
	NA     bool    `json:"na,omitempty"`
	Detail string  `json:"detail"`
}

// EvalReport is a self-assessment of how well the graph serves an agent.
type EvalReport struct {
	ProjectID string   `json:"project_id"`
	Overall   int      `json:"overall"` // 0..100
	Grade     string   `json:"grade"`   // good | ok | poor | no-data
	Metrics   []Metric `json:"metrics"`
	Advice    []string `json:"advice"`
	RanAt     string   `json:"ran_at"`
}

func gradeOf(score int) string {
	switch {
	case score >= 80:
		return "good"
	case score >= 60:
		return "ok"
	}
	return "poor"
}

// evalSampleSize bounds how many symbols the retrieval check queries.
const evalSampleSize = 60

// Eval measures the project without any external ground truth: retrieval
// quality is checked with queries derived from the project's own symbols,
// resolvers are checked against what they should have found, freshness against
// the files on disk, and compaction against the text it folded away.
func (a *Application) Eval(projectID string) (*EvalReport, error) {
	if nodes, _ := a.Graph.FindNodes("Project", map[string]any{"id": projectID}); len(nodes) == 0 {
		return nil, fmt.Errorf("project %q has not been indexed", projectID)
	}
	rep := &EvalReport{ProjectID: projectID, RanAt: time.Now().UTC().Format(time.RFC3339)}
	rep.Metrics = []Metric{
		a.evalRetrieval(projectID),
		a.evalCallResolution(projectID),
		a.evalInheritance(projectID),
		a.evalFreshness(projectID),
		a.evalMemoryRetention(projectID),
	}
	var sum, weight float64
	for _, m := range rep.Metrics {
		if m.NA {
			continue
		}
		sum += m.Score * m.Weight
		weight += m.Weight
		if m.Score < 0.6 {
			rep.Advice = append(rep.Advice, adviceFor(m.Name))
		}
	}
	if weight == 0 {
		rep.Grade = "no-data"
	} else {
		rep.Overall = int(sum/weight*100 + 0.5)
		rep.Grade = gradeOf(rep.Overall)
	}
	blob, _ := json.Marshal(rep)
	a.Graph.UpsertNode("EvalRun", map[string]any{"project_id": projectID, "name": "latest"},
		map[string]any{"report": string(blob), "overall": rep.Overall, "ran_at": rep.RanAt})
	return rep, nil
}

func adviceFor(metric string) string {
	switch metric {
	case "retrieval":
		return "Search often misses known symbols: re-index, and check for many same-named or generated symbols."
	case "call_resolution":
		return "Many in-project calls have no edge: re-run indexing; names defined in >3 places are skipped as ambiguous."
	case "inheritance":
		return "Declared base types are not linked: re-index so ResolveInheritance runs."
	case "freshness":
		return "Indexed files changed on disk: run index_repository (incremental) to refresh."
	case "memory_retention":
		return "A compacted summary is missing lines from its originals: use include_archived to read the originals."
	}
	return ""
}

func nameWords(name string) string {
	return strings.Join(search.Tokenize(name)[1:], " ")
}

func (a *Application) evalRetrieval(pid string) Metric {
	m := Metric{Name: "retrieval", Weight: 0.4}
	fns, _ := a.Graph.FindNodes("Function", map[string]any{"project_id": pid})
	count := map[string]int{}
	for _, f := range fns {
		count[strProp(f, "name")]++
	}
	var candidates []*models.Node
	for _, f := range fns {
		name := strProp(f, "name")
		if count[name] == 1 && len(search.Tokenize(name)) > 1 { // unique, multi-word name
			candidates = append(candidates, f)
		}
	}
	if len(candidates) == 0 {
		m.NA, m.Detail = true, "no uniquely named multi-word functions to query"
		return m
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	stride := 1
	if len(candidates) > evalSampleSize {
		stride = len(candidates) / evalSampleSize
	}
	var n, at1, at5, at10 int
	var rr float64
	for i := 0; i < len(candidates) && n < evalSampleSize; i += stride {
		target := candidates[i]
		query := nameWords(strProp(target, "name"))
		res, err := a.Code.Search(pid, query, 10, true)
		if err != nil {
			continue
		}
		n++
		for rank, row := range res {
			if row["id"] == target.ID {
				rr += 1 / float64(rank+1)
				if rank == 0 {
					at1++
				}
				if rank < 5 {
					at5++
				}
				at10++
				break
			}
		}
	}
	if n == 0 {
		m.NA, m.Detail = true, "no queries could be run"
		return m
	}
	m.Score = rr / float64(n)
	m.Detail = fmt.Sprintf("%d symbol-name queries: MRR %.2f, hit@1 %d%%, hit@5 %d%%, hit@10 %d%%",
		n, m.Score, 100*at1/n, 100*at5/n, 100*at10/n)
	return m
}

func (a *Application) evalCallResolution(pid string) Metric {
	m := Metric{Name: "call_resolution", Weight: 0.2}
	fns, _ := a.Graph.FindNodes("Function", map[string]any{"project_id": pid})
	byName := map[string]int{}
	for _, f := range fns {
		byName[strProp(f, "name")]++
	}
	var expected, found, external, total int
	for _, f := range fns {
		calls := strSlice(f.Properties["calls"])
		if len(calls) == 0 {
			continue
		}
		linked := map[string]bool{}
		if nbrs, err := a.Graph.Neighbors(f.ID, "CALLS", "out"); err == nil {
			for _, en := range nbrs {
				linked[strProp(en.Node, "name")] = true
			}
		}
		for _, c := range calls {
			total++
			switch cnt := byName[c]; {
			case cnt == 0:
				external++
			case cnt <= maxAmbiguousFan: // resolver promises an edge here
				expected++
				if linked[c] {
					found++
				}
			}
		}
	}
	if expected == 0 {
		m.NA, m.Detail = true, "no resolvable in-project calls"
		return m
	}
	m.Score = float64(found) / float64(expected)
	m.Detail = fmt.Sprintf("%d/%d resolvable in-project calls linked; %d of %d call names are external", found, expected, external, total)
	return m
}

func (a *Application) evalInheritance(pid string) Metric {
	m := Metric{Name: "inheritance", Weight: 0.1}
	files, _ := a.Graph.FindNodes("SourceFile", map[string]any{"project_id": pid})
	types := map[string]int{}
	for _, kind := range []string{"Class", "Struct"} {
		ns, _ := a.Graph.FindNodes(kind, map[string]any{"project_id": pid})
		for _, n := range ns {
			types[strProp(n, "name")]++
		}
	}
	var expected, found int
	for _, f := range files {
		for _, enc := range strSlice(f.Properties["type_relations"]) {
			r, ok := decodeRelation(enc)
			if !ok || types[r.sub] == 0 || types[r.super] == 0 || types[r.super] > maxAmbiguousFan {
				continue
			}
			expected++
			subs, _ := a.Graph.FindNodes("Class", map[string]any{"project_id": pid, "name": r.sub})
			more, _ := a.Graph.FindNodes("Struct", map[string]any{"project_id": pid, "name": r.sub})
			for _, s := range append(subs, more...) {
				nbrs, _ := a.Graph.Neighbors(s.ID, r.rel, "out")
				hit := false
				for _, en := range nbrs {
					if strProp(en.Node, "name") == r.super {
						hit = true
					}
				}
				if hit {
					found++
					break
				}
			}
		}
	}
	if expected == 0 {
		m.NA, m.Detail = true, "no in-project inheritance relations"
		return m
	}
	m.Score = float64(found) / float64(expected)
	m.Detail = fmt.Sprintf("%d/%d in-project relations have an edge", found, expected)
	return m
}

const maxFreshnessCheck = 500

func (a *Application) evalFreshness(pid string) Metric {
	m := Metric{Name: "freshness", Weight: 0.2}
	files, _ := a.Graph.FindNodes("SourceFile", map[string]any{"project_id": pid})
	sort.Slice(files, func(i, j int) bool { return files[i].ID < files[j].ID })
	if len(files) > maxFreshnessCheck {
		files = files[:maxFreshnessCheck]
	}
	if len(files) == 0 {
		m.NA, m.Detail = true, "no indexed files"
		return m
	}
	var fresh, changed, missing, oversized int
	for _, f := range files {
		data, err := os.ReadFile(strProp(f, "path"))
		switch {
		case err != nil:
			missing++
		case len(data) > maxIndexFileBytes:
			oversized++
		case fmt.Sprintf("%x", sha256.Sum256(data)) == strProp(f, "hash"):
			fresh++
		default:
			changed++
		}
	}
	checked := len(files) - oversized
	if checked == 0 {
		m.NA, m.Detail = true, "only oversized files"
		return m
	}
	m.Score = float64(fresh) / float64(checked)
	m.Detail = fmt.Sprintf("%d/%d indexed files match disk (%d changed, %d missing)", fresh, checked, changed, missing)
	return m
}

func (a *Application) evalMemoryRetention(pid string) Metric {
	m := Metric{Name: "memory_retention", Weight: 0.1}
	mems, _ := a.Graph.FindNodes("Memory", map[string]any{"project_id": pid})
	byID := map[string]*models.Node{}
	for _, n := range mems {
		byID[n.ID] = n
	}
	var lines, kept, summaries int
	for _, sm := range mems {
		if sm.Properties["kind"] != compactedKind {
			continue
		}
		summaries++
		have := map[string]bool{}
		for _, l := range strings.Split(strProp(sm, "content"), "\n") {
			have[normalizeLine(l)] = true
		}
		for _, id := range strSlice(sm.Properties["member_ids"]) {
			orig := byID[id]
			if orig == nil {
				continue
			}
			for _, l := range strings.Split(strProp(orig, "content"), "\n") {
				if n := normalizeLine(l); n != "" {
					lines++
					if have[n] {
						kept++
					}
				}
			}
		}
	}
	if lines == 0 {
		m.NA, m.Detail = true, "no compacted memories yet"
		return m
	}
	m.Score = float64(kept) / float64(lines)
	m.Detail = fmt.Sprintf("%d/%d original lines present in %d summaries", kept, lines, summaries)
	return m
}

// Status is a read-only health snapshot of the whole installation.
func (a *Application) Status() (map[string]any, error) {
	out := map[string]any{}
	if c, ok := a.Graph.(interface {
		Counts() (map[string]int, map[string]int)
	}); ok {
		nodes, edges := c.Counts()
		delete(nodes, "UsageSession")
		out["graph"] = map[string]any{"nodes_by_kind": nodes, "edges_by_kind": edges}
	}
	var projects []map[string]any
	for _, pid := range a.projectIDs() {
		projects = append(projects, a.projectStatus(pid))
	}
	out["projects"] = projects

	usage, err := SummarizeUsage(a.Graph)
	if err != nil {
		return nil, err
	}
	tools := map[string]any{}
	for _, name := range usage.TopTools(8) {
		u := usage.ByTool[name]
		tools[name] = map[string]any{
			"calls": u.Calls, "errors": u.Errors, "avg_ms": round1(u.TotalMs / float64(max1(u.Calls))),
		}
	}
	raw := usage.Total.RawTokens
	saved := usage.TokensSaved()
	pct := 0.0
	if raw > 0 {
		pct = round1(100 * float64(saved) / float64(raw))
	}
	out["usage"] = map[string]any{
		"sessions": usage.Sessions, "tool_calls": usage.Total.Calls, "errors": usage.Total.Errors,
		"error_rate_pct":      round1(100 * float64(usage.Total.Errors) / float64(max1(usage.Total.Calls))),
		"avg_ms":              round1(usage.Total.TotalMs / float64(max1(usage.Total.Calls))),
		"tokens_returned_est": usage.Total.ShapedTokens, "tokens_saved_est": saved, "token_savings_pct": pct,
		"pages_cut_by_budget": usage.Total.Truncated, "top_tools": tools,
	}
	if a.Cache != nil {
		out["cache"] = a.Cache.Stats()
	}
	if m := a.lastMaintenance(); m != nil {
		out["last_maintenance"] = m
	}
	return out, nil
}

func (a *Application) projectStatus(pid string) map[string]any {
	count := func(kind string) int {
		n, _ := a.Graph.FindNodes(kind, map[string]any{"project_id": pid})
		return len(n)
	}
	files, _ := a.Graph.FindNodes("SourceFile", map[string]any{"project_id": pid})
	generated, stale := 0, 0
	newest := ""
	for _, f := range files {
		if g, _ := f.Properties["generated"].(bool); g {
			generated++
		}
		if strProp(f, "parser_version") != a.Index.ParserVersion() {
			stale++
		}
		if t := strProp(f, "last_indexed_at"); t > newest {
			newest = t
		}
	}
	mems, _ := a.Graph.FindNodes("Memory", map[string]any{"project_id": pid})
	active, archived, summaries := 0, 0, 0
	for _, m := range mems {
		switch {
		case m.Properties["kind"] == compactedKind:
			summaries++
		case isArchived(m):
			archived++
		default:
			active++
		}
	}
	st := map[string]any{
		"project_id": pid, "files": len(files), "generated_files": generated,
		"functions": count("Function"), "classes": count("Class"), "structs": count("Struct"),
		"last_indexed_at": newest, "files_on_old_parser": stale,
		"memories_active": active, "memories_archived": archived, "memory_summaries": summaries,
		"doc_sections": count("DocumentSection"),
	}
	if nodes, _ := a.Graph.FindNodes("EvalRun", map[string]any{"project_id": pid, "name": "latest"}); len(nodes) > 0 {
		var r EvalReport
		if json.Unmarshal([]byte(strProp(nodes[0], "report")), &r) == nil {
			st["last_eval"] = map[string]any{"overall": r.Overall, "grade": r.Grade, "ran_at": r.RanAt}
		}
	}
	return st
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// ============================================================
// Benchmark - deterministic runtime benchmark for correctness
// ============================================================

// BenchmarkResult contains the results of a benchmark run.
type BenchmarkResult struct {
	ProjectID    string             `json:"project_id"`
	RanAt        string             `json:"ran_at"`
	Reference    ReferenceBenchmark `json:"reference_accuracy"`
	InvalidRef   InvalidRefBenchmark `json:"invalid_reference_rejection"`
	Slice        SliceBenchmark     `json:"slice_arithmetic"`
	Context      ContextBenchmark   `json:"context_tokens_vs_baseline"`
	OverallScore int                `json:"overall_score"` // 0-100
}

// ReferenceBenchmark measures reference resolution accuracy.
type ReferenceBenchmark struct {
	Total      int     `json:"total"`
	Valid      int     `json:"valid"`
	Stale      int     `json:"stale"`
	Invalid    int     `json:"invalid"`
	Accuracy   float64 `json:"accuracy"` // valid / total
	Detail     string  `json:"detail"`
}

// InvalidRefBenchmark measures rejection of invalid references.
type InvalidRefBenchmark struct {
	TotalRejected   int     `json:"total_rejected"`
	TotalTested     int     `json:"total_tested"`
	RejectionRate   float64 `json:"rejection_rate"`
	FalsePositives  int     `json:"false_positives"` // valid IDs incorrectly rejected
	FalseNegatives  int     `json:"false_negatives"` // invalid IDs not rejected
	Detail          string  `json:"detail"`
}

// SliceBenchmark measures slice arithmetic correctness.
type SliceBenchmark struct {
	TotalTested     int     `json:"total_tested"`
	Correct         int     `json:"correct"`
	Accuracy        float64 `json:"accuracy"`
	Detail          string  `json:"detail"`
}

// ContextBenchmark compares ContextCompiler vs naive top-K baseline.
type ContextBenchmark struct {
	QueriesTested      int     `json:"queries_tested"`
	AvgTokensCompiler  float64 `json:"avg_tokens_compiler"`
	AvgTokensBaseline  float64 `json:"avg_tokens_baseline"`
	TokenReductionPct  float64 `json:"token_reduction_pct"`
	QualityScore       float64 `json:"quality_score"` // how much relevant content preserved
	Detail             string  `json:"detail"`
}

// BenchmarkConfig controls benchmark behavior.
type BenchmarkConfig struct {
	NumQueries      int // number of queries for context benchmark (default 10)
	MaxTokens       int // token budget for context compiler (default 2000)
	IncludeBaseline bool // whether to run baseline comparison
}

// DefaultBenchmarkConfig returns sensible defaults.
func DefaultBenchmarkConfig() BenchmarkConfig {
	return BenchmarkConfig{
		NumQueries:      10,
		MaxTokens:       2000,
		IncludeBaseline: true,
	}
}

// RunBenchmark executes the deterministic runtime benchmark.
func (a *Application) RunBenchmark(projectID string, cfg BenchmarkConfig) (*BenchmarkResult, error) {
	if nodes, _ := a.Graph.FindNodes("Project", map[string]any{"id": projectID}); len(nodes) == 0 {
		return nil, fmt.Errorf("project %q has not been indexed", projectID)
	}

	result := &BenchmarkResult{
		ProjectID: projectID,
		RanAt:     time.Now().UTC().Format(time.RFC3339),
	}

	// 1. Reference accuracy
	result.Reference = a.benchmarkReferenceAccuracy(projectID)

	// 2. Invalid reference rejection
	result.InvalidRef = a.benchmarkInvalidReferenceRejection(projectID)

	// 3. Slice arithmetic
	result.Slice = a.benchmarkSliceArithmetic(projectID)

	// 4. Context tokens vs baseline
	if cfg.IncludeBaseline {
		result.Context = a.benchmarkContextTokens(projectID, cfg.NumQueries, cfg.MaxTokens)
	}

	// Calculate overall score (weighted average)
	weights := map[string]float64{
		"reference":    0.3,
		"invalid_ref":  0.2,
		"slice":        0.2,
		"context":      0.3,
	}
	var sum, totalWeight float64
	sum += result.Reference.Accuracy * weights["reference"]
	totalWeight += weights["reference"]
	sum += result.InvalidRef.RejectionRate * weights["invalid_ref"]
	totalWeight += weights["invalid_ref"]
	sum += result.Slice.Accuracy * weights["slice"]
	totalWeight += weights["slice"]
	if cfg.IncludeBaseline {
		sum += result.Context.QualityScore * weights["context"]
		totalWeight += weights["context"]
	}
	if totalWeight > 0 {
		result.OverallScore = int(sum / totalWeight * 100 + 0.5)
	}

	// Store in graph
	blob, _ := json.Marshal(result)
	a.Graph.UpsertNode("BenchmarkRun", map[string]any{"project_id": projectID, "name": "latest"},
		map[string]any{"report": string(blob), "overall": result.OverallScore, "ran_at": result.RanAt})

	return result, nil
}

// benchmarkReferenceAccuracy tests resolve_reference and verify_reference.
func (a *Application) benchmarkReferenceAccuracy(projectID string) ReferenceBenchmark {
	b := ReferenceBenchmark{}
	// Get all stable IDs in the project
	fns, _ := a.Graph.FindNodes("Function", map[string]any{"project_id": projectID})
	classes, _ := a.Graph.FindNodes("Class", map[string]any{"project_id": projectID})
	structs, _ := a.Graph.FindNodes("Struct", map[string]any{"project_id": projectID})
	files, _ := a.Graph.FindNodes("SourceFile", map[string]any{"project_id": projectID})

	allNodes := append(fns, classes...)
	allNodes = append(allNodes, structs...)
	allNodes = append(allNodes, files...)

	b.Total = len(allNodes)
	for _, n := range allNodes {
		stableID := strProp(n, "stable_id")
		if stableID == "" {
			continue
		}
		res := a.Refs.Resolve(projectID, stableID)
		switch res.Status {
		case RefValid:
			b.Valid++
		case RefStale:
			b.Stale++
		case RefInvalid:
			b.Invalid++
		}
		// Also test verify_reference with correct hash
		if res.Status == RefValid && res.Source != nil && res.Source.ContentHash != "" {
			v := a.Refs.Verify(projectID, VerifyRequest{
				ID: stableID,
				ExpectedContentHash: res.Source.ContentHash,
			})
			if v.Status != RefValid {
				b.Invalid++ // verify should pass for correct hash
			}
		}
	}
	if b.Total > 0 {
		b.Accuracy = float64(b.Valid) / float64(b.Total)
	}
	b.Detail = fmt.Sprintf("%d symbols: %d valid, %d stale, %d invalid", b.Total, b.Valid, b.Stale, b.Invalid)
	return b
}

// benchmarkInvalidReferenceRejection tests that invalid IDs are properly rejected.
func (a *Application) benchmarkInvalidReferenceRejection(projectID string) InvalidRefBenchmark {
	b := InvalidRefBenchmark{}
	// Get valid IDs first
	fns, _ := a.Graph.FindNodes("Function", map[string]any{"project_id": projectID})
	var validIDs []string
	for _, f := range fns {
		if sid := strProp(f, "stable_id"); sid != "" {
			validIDs = append(validIDs, sid)
		}
	}

	// Test 1: Valid IDs should NOT be rejected (false positive check)
	for _, id := range validIDs {
		b.TotalTested++
		res := a.Refs.Resolve(projectID, id)
		if res.Status == RefInvalid {
			b.FalsePositives++
		}
	}

	// Test 2: Invalid IDs SHOULD be rejected
	// Generate invalid IDs by mutating valid ones
	invalidIDs := []string{}
	for _, id := range validIDs {
		// Mutate the last character
		if len(id) > 0 {
			mutated := id[:len(id)-1] + "X"
			if mutated != id {
				invalidIDs = append(invalidIDs, mutated)
			}
		}
		// Add completely bogus IDs
		invalidIDs = append(invalidIDs, "func:fake:notexist")
		invalidIDs = append(invalidIDs, "class:nope:missing")
		invalidIDs = append(invalidIDs, "struct:doesnot:exist")
	}

	// Limit to reasonable number
	if len(invalidIDs) > 50 {
		invalidIDs = invalidIDs[:50]
	}

	for _, id := range invalidIDs {
		b.TotalTested++
		res := a.Refs.Resolve(projectID, id)
		if res.Status == RefInvalid {
			b.TotalRejected++
		} else {
			b.FalseNegatives++
		}
	}

	if b.TotalTested > 0 {
		b.RejectionRate = float64(b.TotalRejected) / float64(b.TotalTested)
	}
	b.Detail = fmt.Sprintf("Tested %d IDs: %d valid (fp: %d), %d invalid (fn: %d)",
		b.TotalTested, len(validIDs), b.FalsePositives, len(invalidIDs), b.FalseNegatives)
	return b
}

// benchmarkSliceArithmetic tests resolve_slice correctness.
func (a *Application) benchmarkSliceArithmetic(projectID string) SliceBenchmark {
	b := SliceBenchmark{}

	// Test cases: (base, offset, length, base_length, expected_valid, expected_end)
	testCases := []struct {
		base         string
		offset       int
		length       int
		baseLength   int
		expectValid  bool
		expectEnd    int
		description  string
	}{
		{"buf", 0, 10, 20, true, 10, "normal slice"},
		{"buf", 5, 5, 20, true, 10, "mid slice"},
		{"buf", 15, 10, 20, false, 25, "out of bounds"},
		{"buf", -1, 10, 20, false, 9, "negative offset"},
		{"buf", 0, 20, 20, true, 20, "full slice"},
		{"buf", 0, 21, 20, false, 21, "exceeds base"},
		{"data", 100, 50, 100, false, 150, "offset at end"},
		{"arr", 0, 5, 100, true, 5, "small slice large base"},
	}

	for _, tc := range testCases {
		b.TotalTested++
		req := SliceRequest{
			Base:       tc.base,
			Offset:     tc.offset,
			Length:     &tc.length,
			BaseLength: &tc.baseLength,
			ElementSize: 1,
		}
		res := ComputeSlice(req)

		if res.Valid == tc.expectValid && res.End == tc.expectEnd {
			b.Correct++
		}
	}

	if b.TotalTested > 0 {
		b.Accuracy = float64(b.Correct) / float64(b.TotalTested)
	}
	b.Detail = fmt.Sprintf("%d/%d slice tests passed", b.Correct, b.TotalTested)
	return b
}

// benchmarkContextTokens compares ContextCompiler vs naive top-K.
func (a *Application) benchmarkContextTokens(projectID string, numQueries, maxTokens int) ContextBenchmark {
	b := ContextBenchmark{QueriesTested: numQueries}

	// Get some function names to use as queries
	fns, _ := a.Graph.FindNodes("Function", map[string]any{"project_id": projectID})
	if len(fns) == 0 {
		b.Detail = "no functions to query"
		return b
	}

	var compilerTokens, baselineTokens float64
	var qualitySum float64

	for i := 0; i < numQueries && i < len(fns); i++ {
		query := strProp(fns[i], "name")

		// Naive baseline: top-K BM25 search (limit=10, no filtering)
		baselineRes, err := a.Code.Search(projectID, query, 10, false)
		if err != nil {
			continue
		}
		baselineTokens += float64(len(baselineRes)) * 50 // rough estimate: ~50 tokens per result

		// ContextCompiler with token budget
		ctx, err := a.Context.Compile(ContextRequest{
			ProjectID: projectID,
			Question:  query,
			Level:     CtxLevelRanked,
			MaxTokens: maxTokens,
			Limit:     10,
		})
		if err != nil {
			continue
		}
		compilerTokens += float64(ctx.ApproxTokens)

		// Quality: how many baseline results are in the compiled context?
		baselineNames := make(map[string]bool)
		for _, r := range baselineRes {
			if name, ok := r["name"].(string); ok {
				baselineNames[name] = true
			}
		}
		contextNames := make(map[string]bool)
		for _, block := range ctx.Blocks {
			for _, item := range block.Items {
				if item.Kind == "symbol" {
					// Extract name from text like "func ParsePacket"
					parts := strings.Fields(item.Text)
					if len(parts) >= 2 {
						contextNames[parts[1]] = true
					}
				}
			}
		}
		overlap := 0
		for name := range baselineNames {
			if contextNames[name] {
				overlap++
			}
		}
		if len(baselineNames) > 0 {
			qualitySum += float64(overlap) / float64(len(baselineNames))
		}
	}

	b.QueriesTested = numQueries
	if numQueries > 0 {
		b.AvgTokensCompiler = compilerTokens / float64(numQueries)
		b.AvgTokensBaseline = baselineTokens / float64(numQueries)
		b.QualityScore = qualitySum / float64(numQueries)
	}
	if b.AvgTokensBaseline > 0 {
		b.TokenReductionPct = 100 * (b.AvgTokensBaseline - b.AvgTokensCompiler) / b.AvgTokensBaseline
	}
	b.Detail = fmt.Sprintf("Compiler: %.0f tokens, Baseline: %.0f tokens, Reduction: %.1f%%, Quality: %.2f",
		b.AvgTokensCompiler, b.AvgTokensBaseline, b.TokenReductionPct, b.QualityScore)
	return b
}
