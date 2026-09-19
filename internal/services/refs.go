package services

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"codergag/internal/graph"
	"codergag/internal/ids"
	"codergag/internal/models"
)

// Reference statuses. Only VALID references may be used to modify code.
const (
	RefValid   = "VALID"
	RefStale   = "REFERENCE_STALE"
	RefInvalid = "INVALID_REFERENCE"
)

// SourceSpan is an exact, versioned, fingerprinted source location. Identity is
// SymbolID/FileID; the rest are attributes that can be re-resolved at any time.
type SourceSpan struct {
	FileID      string `json:"file_id"`
	RevisionID  string `json:"revision_id,omitempty"`
	RelPath     string `json:"rel_path"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	StartColumn int    `json:"start_column"`
	EndColumn   int    `json:"end_column"`
	StartByte   int64  `json:"start_byte"`
	EndByte     int64  `json:"end_byte"`
	ContentHash string `json:"content_hash"`
	SymbolID    string `json:"symbol_id,omitempty"`
}

// BinarySpan locates a function in an imported binary.
type BinarySpan struct {
	BinaryID     string `json:"binary_id"`
	BinaryHash   string `json:"binary_hash,omitempty"`
	StartAddress string `json:"start_address"`
	EndAddress   string `json:"end_address,omitempty"`
	Size         int    `json:"size,omitempty"`
	Section      string `json:"section,omitempty"`
	FunctionID   string `json:"function_id"`
	Tool         string `json:"analysis_tool,omitempty"`
}

// Resolution is the answer to "what is this ID right now?".
type Resolution struct {
	ID          string      `json:"id"`
	Status      string      `json:"status"`
	Kind        string      `json:"kind,omitempty"`
	NodeID      string      `json:"node_id,omitempty"`
	Name        string      `json:"name,omitempty"`
	Source      *SourceSpan `json:"source,omitempty"`
	Binary      *BinarySpan `json:"binary,omitempty"`
	IndexedHash string      `json:"indexed_hash,omitempty"`
	CurrentHash string      `json:"current_hash,omitempty"`
	Revision    string      `json:"revision,omitempty"`
	Reason      string      `json:"reason,omitempty"`
	Candidates  []string    `json:"did_you_mean,omitempty"`
}

// ReferenceResolver turns stable IDs into current locations and decides whether a
// remembered reference is still safe to act on.
type ReferenceResolver struct {
	graph graph.GraphRepository
}

func NewReferenceResolver(g graph.GraphRepository) *ReferenceResolver {
	return &ReferenceResolver{graph: g}
}

var nodeKindOf = map[string]string{
	ids.Func: "Function", ids.Class: "Class", ids.Struct: "Struct", ids.File: "SourceFile",
	ids.BinFunc: "BinaryFunction", ids.Binary: "Binary", ids.BB: "BasicBlock", ids.Insn: "Instruction",
}

func (r *ReferenceResolver) projectRoot(projectID string) string {
	if ps, _ := r.graph.FindNodes("Project", map[string]any{"id": projectID}); len(ps) > 0 {
		return strProp(ps[0], "path")
	}
	return ""
}

// Lookup finds the node behind a stable ID. Unknown IDs return nil plus verified
// near matches so a hallucinated identifier is rejected with a correction.
func (r *ReferenceResolver) Lookup(projectID, id string) (*models.Node, []string) {
	kind, _, ok := ids.Parse(id)
	if !ok {
		return nil, nil
	}
	nk := nodeKindOf[kind]
	if nk == "" {
		return nil, nil
	}
	if n, _ := r.graph.FindNodes(nk, map[string]any{"project_id": projectID, "stable_id": id}); len(n) > 0 {
		return n[0], nil
	}
	return nil, r.suggest(projectID, nk, id)
}

const maxSuggestions = 3

func (r *ReferenceResolver) suggest(projectID, nodeKind, id string) []string {
	nodes, _ := r.graph.FindNodes(nodeKind, map[string]any{"project_id": projectID})
	type cand struct {
		id string
		d  int
	}
	var cands []cand
	for _, n := range nodes {
		sid := strProp(n, "stable_id")
		if sid == "" {
			continue
		}
		if d := editDistance(strings.ToLower(id), strings.ToLower(sid)); d*4 <= len(id) {
			cands = append(cands, cand{sid, d})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].d != cands[j].d {
			return cands[i].d < cands[j].d
		}
		return cands[i].id < cands[j].id
	})
	var out []string
	for _, c := range capRows(cands, maxSuggestions) {
		out = append(out, c.id)
	}
	return out
}

func intProp(n *models.Node, key string) int {
	v, _ := n.Properties[key].(int)
	return v
}

// sourceSpanOf projects a node's indexed properties into a SourceSpan.
func sourceSpanOf(n *models.Node) *SourceSpan {
	rel := strProp(n, "rel_path")
	return &SourceSpan{
		FileID: ids.FileID(rel), RevisionID: strProp(n, "commit"), RelPath: rel,
		StartLine: intProp(n, "line_start"), EndLine: intProp(n, "line_end"),
		StartColumn: intProp(n, "start_col"), EndColumn: intProp(n, "end_col"),
		StartByte: int64(intProp(n, "start_byte")), EndByte: int64(intProp(n, "end_byte")),
		ContentHash: strProp(n, "content_hash"), SymbolID: strProp(n, "stable_id"),
	}
}

// currentSpanHash hashes the bytes now on disk at the indexed byte range.
func currentSpanHash(path string, start, end int64) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if start < 0 || end > int64(len(data)) || start > end {
		return "", errors.New("indexed range is outside the file")
	}
	return fmt.Sprintf("%x", sha256.Sum256(data[start:end])), nil
}

// Resolve returns where an ID points now. For source symbols the indexed span is
// re-hashed against the file on disk, so a changed file yields REFERENCE_STALE
// instead of a silently wrong location.
func (r *ReferenceResolver) Resolve(projectID, id string) *Resolution {
	res := &Resolution{ID: id}
	kind, _, ok := ids.Parse(id)
	if !ok {
		res.Status, res.Reason = RefInvalid, "not a stable ID (expected kind:..., e.g. func:src/a.c:parse)"
		return res
	}
	res.Kind = kind
	n, cands := r.Lookup(projectID, id)
	if n == nil {
		res.Status, res.Candidates = RefInvalid, cands
		res.Reason = "no such object in project " + projectID
		if nodeKindOf[kind] == "" {
			res.Reason = "unsupported ID kind " + kind
		}
		return res
	}
	res.NodeID, res.Name = n.ID, strProp(n, "name")
	root := r.projectRoot(projectID)
	res.Revision, _ = git(root, "rev-parse", "HEAD")

	switch n.Kind {
	case "BinaryFunction":
		res.Binary = r.binarySpanOf(projectID, n)
		res.Status = RefValid
		return res
	case "Binary", "BasicBlock", "Instruction":
		res.Status = RefValid
		return res
	}

	res.Source = sourceSpanOf(n)
	res.IndexedHash = res.Source.ContentHash
	abs := filepath.Join(root, filepath.FromSlash(res.Source.RelPath))
	if n.Kind == "SourceFile" {
		data, err := os.ReadFile(abs)
		if err != nil {
			res.Status, res.Reason = RefStale, "file no longer exists on disk"
			return res
		}
		res.CurrentHash = fmt.Sprintf("%x", sha256.Sum256(data))
		res.IndexedHash = strProp(n, "hash")
		res.Status = RefValid
		if res.CurrentHash != res.IndexedHash {
			res.Status, res.Reason = RefStale, "file changed since it was indexed; re-index before relying on spans"
		}
		return res
	}
	if res.IndexedHash == "" {
		res.Status, res.Reason = RefStale, "no content fingerprint recorded (indexed by an older version); re-index"
		return res
	}
	cur, err := currentSpanHash(abs, res.Source.StartByte, res.Source.EndByte)
	if err == nil && cur == res.IndexedHash {
		res.CurrentHash = cur
		res.Status = RefValid
		return res
	}
	// The indexed byte range no longer matches. Edits elsewhere in the file shift
	// offsets without touching this symbol, so re-parse the file, find the symbol
	// again and compare its own content instead of assuming it changed.
	moved, ok := relocateSymbol(abs, n)
	if !ok {
		res.Status, res.Reason = RefStale, "the symbol is no longer present in the file, or the file cannot be read"
		return res
	}
	res.CurrentHash = moved.hash
	res.Source.StartLine, res.Source.EndLine = moved.startLine, moved.endLine
	res.Source.StartByte, res.Source.EndByte = int64(moved.sp.startByte), int64(moved.sp.endByte)
	res.Source.StartColumn, res.Source.EndColumn = moved.sp.startCol, moved.sp.endCol
	res.Source.ContentHash = moved.hash
	if moved.hash != res.IndexedHash {
		res.Status, res.Reason = RefStale, "the code of this symbol changed since it was indexed; re-index and resolve again"
		return res
	}
	res.Status = RefValid
	res.Reason = "symbol moved within the file (index is behind disk); the span above is current, re-index to refresh"
	return res
}

type relocated struct {
	sp                 span
	startLine, endLine int
	hash               string
}

// relocateSymbol re-extracts a symbol from the file as it is on disk now.
func relocateSymbol(abs string, n *models.Node) (relocated, bool) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return relocated{}, false
	}
	text, ext := string(data), strings.ToLower(filepath.Ext(abs))
	if n.Kind == "Function" {
		label := qualifiedLabel(n)
		for _, fi := range mergeFuncInfos(extractFunctionInfos(text, ext)) {
			if fi.qualifiedName() == label {
				return relocated{fi.span, fi.start, fi.end, spanHash(data, fi.span)}, true
			}
		}
		return relocated{}, false
	}
	for _, tm := range extractTypes(text, ext) {
		if tm.name == strProp(n, "name") && tm.kind == n.Kind {
			return relocated{tm.span, tm.start, tm.end, spanHash(data, tm.span)}, true
		}
	}
	return relocated{}, false
}

func (r *ReferenceResolver) binarySpanOf(projectID string, fn *models.Node) *BinarySpan {
	bs := &BinarySpan{
		BinaryID: strProp(fn, "binary_id"), StartAddress: strProp(fn, "address"),
		FunctionID: strProp(fn, "stable_id"), Section: strProp(fn, "section"), Tool: strProp(fn, "analysis_tool"),
	}
	bs.Size = intProp(fn, "size")
	if b, _ := r.graph.FindNodes("Binary", map[string]any{"project_id": projectID, "binary_id": bs.BinaryID}); len(b) > 0 {
		bs.BinaryHash = strProp(b[0], "hash")
	}
	if bs.Size > 0 {
		var start int64
		if _, err := fmt.Sscanf(strings.TrimPrefix(bs.StartAddress, "0x"), "%x", &start); err == nil {
			bs.EndAddress = fmt.Sprintf("0x%x", start+int64(bs.Size))
		}
	}
	return bs
}

// VerifyRequest carries what the caller believed when it read the code.
type VerifyRequest struct {
	ID                  string
	ExpectedContentHash string
	ExpectedRevision    string
}

// Verify decides whether a remembered reference may still be acted on. Disk is
// the authority: the reference is VALID only when the code at the span still has
// the fingerprint the caller saw (or, if none was given, the indexed one). A
// moved revision alone is fine when the file is unchanged between the revisions.
func (r *ReferenceResolver) Verify(projectID string, req VerifyRequest) *Resolution {
	res := r.Resolve(projectID, req.ID)
	if res.Status == RefInvalid {
		return res
	}
	warn := func(s string) {
		if res.Reason == "" {
			res.Reason = s
		} else {
			res.Reason += "; " + s
		}
	}
	if req.ExpectedContentHash != "" {
		if res.CurrentHash != "" && req.ExpectedContentHash != res.CurrentHash {
			res.Status = RefStale
			res.Reason = "content changed since you read it (expected " + shortHash(req.ExpectedContentHash) + ", now " + shortHash(res.CurrentHash) + ")"
			return res
		}
		if res.Status == RefStale && res.CurrentHash == req.ExpectedContentHash {
			res.Status, res.Reason = RefValid, "" // your view is current even though the index is behind
			warn("index is behind disk; re-index")
		}
	}
	if req.ExpectedRevision != "" && res.Revision != "" && !sameRevision(res.Revision, req.ExpectedRevision) {
		root := r.projectRoot(projectID)
		rel := ""
		if res.Source != nil {
			rel = res.Source.RelPath
		}
		if rel != "" && !fileChangedBetween(root, req.ExpectedRevision, rel) {
			warn("revision moved (" + shortSHA(req.ExpectedRevision) + " -> " + shortSHA(res.Revision) + ") but the file is unchanged")
		} else {
			res.Status = RefStale
			res.Reason = "revision moved from " + shortSHA(req.ExpectedRevision) + " to " + shortSHA(res.Revision) + " and the file changed (or cannot be compared)"
		}
	}
	return res
}

func sameRevision(a, b string) bool {
	return a == b || (len(a) >= 7 && len(b) >= 7 && (strings.HasPrefix(a, b) || strings.HasPrefix(b, a)))
}

func shortHash(h string) string {
	if len(h) > 10 {
		return h[:10]
	}
	return h
}

// fileChangedBetween reports whether rel differs between rev and the working
// tree. Anything git cannot answer counts as changed (fail closed).
func fileChangedBetween(root, rev, rel string) bool {
	cmd := exec.Command("git", "-C", root, "diff", "--quiet", rev, "--", rel)
	err := cmd.Run()
	if err == nil {
		return false
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return true
	}
	return true
}

// ResolveSourceSpan fingerprints a line range of a file as it is on disk now and
// names the innermost indexed symbol that contains it.
func (r *ReferenceResolver) ResolveSourceSpan(projectID, file string, startLine, endLine int) (*SourceSpan, string, error) {
	root := r.projectRoot(projectID)
	rel := ids.Rel(root, file)
	if !filepath.IsAbs(file) {
		rel = filepath.ToSlash(filepath.Clean(file))
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %s cannot be read (%v)", RefInvalid, rel, err)
	}
	if startLine < 1 {
		startLine = 1
	}
	if endLine < startLine {
		endLine = startLine
	}
	text := string(data)
	lines := strings.Split(text, "\n")
	if startLine > len(lines) {
		return nil, "", fmt.Errorf("%s: %s has %d lines, start_line %d is past the end", RefInvalid, rel, len(lines), startLine)
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	sp := lineSpan(text, startLine, endLine)
	// lineSpan's end excludes the final newline; recompute exactly for columns.
	span := &SourceSpan{
		FileID: ids.FileID(rel), RelPath: rel, StartLine: startLine, EndLine: endLine,
		StartByte: int64(sp.startByte), EndByte: int64(sp.endByte),
		EndColumn:   len(lines[endLine-1]),
		ContentHash: fmt.Sprintf("%x", sha256.Sum256(data[sp.startByte:sp.endByte])),
	}
	span.RevisionID, _ = git(root, "rev-parse", "HEAD")

	best, bestSize := "", 1<<30
	for _, kind := range []string{"Function", "Class", "Struct"} {
		nodes, _ := r.graph.FindNodes(kind, map[string]any{"project_id": projectID, "rel_path": rel})
		for _, n := range nodes {
			s, e := intProp(n, "line_start"), intProp(n, "line_end")
			if s <= startLine && endLine <= e && e-s < bestSize {
				best, bestSize = strProp(n, "stable_id"), e-s
			}
		}
	}
	span.SymbolID = best
	return span, best, nil
}

// SymbolMatch is one structural (exact-name) hit.
type SymbolMatch struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	Path string `json:"path"`
	Line int    `json:"line"`
}

// ResolveSymbol is structural lookup: exact name (or Owner.name), never
// similarity. An unknown name returns verified near matches, not a guess.
func (r *ReferenceResolver) ResolveSymbol(projectID, name string) ([]SymbolMatch, []string) {
	var out []SymbolMatch
	var names []string
	seen := map[string]bool{}
	for _, kind := range []string{"Function", "Class", "Struct"} {
		nodes, _ := r.graph.FindNodes(kind, map[string]any{"project_id": projectID})
		for _, n := range nodes {
			label := qualifiedLabel(n)
			if !seen[label] {
				seen[label] = true
				names = append(names, label)
			}
			if label == name || strProp(n, "name") == name {
				out = append(out, SymbolMatch{ID: strProp(n, "stable_id"), Kind: kind, Name: label,
					Path: strProp(n, "rel_path"), Line: intProp(n, "line_start")})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > 0 {
		return out, nil
	}
	sort.Strings(names)
	type cand struct {
		n string
		d int
	}
	var cs []cand
	for _, n := range names {
		if d := editDistance(strings.ToLower(name), strings.ToLower(n)); d*4 <= len(name) {
			cs = append(cs, cand{n, d})
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].d < cs[j].d })
	var sug []string
	for _, c := range capRows(cs, maxSuggestions) {
		sug = append(sug, c.n)
	}
	return nil, sug
}
