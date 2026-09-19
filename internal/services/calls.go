package services

import (
	"path/filepath"
	"strings"

	"codergag/internal/graph"
)

const (
	confSameFile    = 0.9
	confUniqueName  = 0.8
	confAmbiguous   = 0.5
	maxAmbiguousFan = 3
)

// ResolveCalls makes the CALLS edges match the call names recorded on each
// Function: missing edges are created and tree-sitter edges whose call no
// longer exists are removed. It is idempotent and order-independent, so callers
// indexed before their callees are still linked once both exist.
//
// Resolution priority:
//  1. "Owner.method" — exact owner+name match (type-resolved method call)
//  2. Same-file by bare name
//  3. Project-wide unique bare name
//  4. Ambiguous (up to maxAmbiguousFan targets)
func (s *CodeIndexService) ResolveCalls(projectID string) error {
	fns, err := s.graph.FindNodes("Function", map[string]any{"project_id": projectID})
	if err != nil {
		return err
	}
	// Index by bare name and by owner-qualified name.
	byName := map[string][]string{}
	byQualified := map[string][]string{} // "Owner.method" -> []id
	pathOf := map[string]string{}
	for _, f := range fns {
		name, _ := f.Properties["name"].(string)
		owner, _ := f.Properties["owner"].(string)
		if name == "" {
			continue
		}
		byName[name] = append(byName[name], f.ID)
		if owner != "" {
			byQualified[owner+"."+name] = append(byQualified[owner+"."+name], f.ID)
		}
		pathOf[f.ID], _ = f.Properties["path"].(string)
	}
	for _, caller := range fns {
		desired := map[string]float64{}
		for _, call := range strSlice(caller.Properties["calls"]) {
			var targets []string
			var conf float64
			if strings.Contains(call, ".") {
				// Owner-qualified: e.g. "Service.Process"
				if ids := byQualified[call]; len(ids) > 0 {
					targets, conf = pickTargets(ids, pathOf, pathOf[caller.ID])
					if len(targets) == 0 {
						// Fall back: try bare name
						bare := call[strings.LastIndex(call, ".")+1:]
						targets, conf = pickTargets(byName[bare], pathOf, pathOf[caller.ID])
						conf *= 0.8 // penalise: owner didn't match
					}
				} else {
					// Qualified name not in index: try bare name
					bare := call[strings.LastIndex(call, ".")+1:]
					targets, conf = pickTargets(byName[bare], pathOf, pathOf[caller.ID])
				}
			} else {
				targets, conf = pickTargets(byName[call], pathOf, pathOf[caller.ID])
			}
			for _, id := range targets {
				if id != caller.ID && conf > desired[id] {
					desired[id] = conf
				}
			}
		}
		s.syncEdges(caller.ID, "CALLS", desired)
	}
	return nil
}

// syncEdges makes the tree-sitter-derived edges of one kind leaving fromID equal
// to desired (target ID -> confidence). Edges from other sources are untouched.
func (s *CodeIndexService) syncEdges(fromID, kind string, desired map[string]float64) {
	s.syncEdgesAs(fromID, kind, "tree-sitter", desired)
}

// syncEdgesAs is syncEdges for edges stamped with the given source.
func (s *CodeIndexService) syncEdgesAs(fromID, kind, source string, desired map[string]float64) {
	have := map[string]bool{}
	var stale []string
	if nbrs, err := s.graph.Neighbors(fromID, kind, graph.DirOut); err == nil {
		for _, en := range nbrs {
			if src, _ := en.Edge.Properties["source"].(string); src != source {
				have[en.Node.ID] = true
				continue
			}
			if _, want := desired[en.Node.ID]; !want || have[en.Node.ID] {
				stale = append(stale, en.Edge.ID)
				continue
			}
			have[en.Node.ID] = true
		}
	}
	if len(stale) > 0 {
		s.graph.RemoveEdges(stale)
	}
	for id, conf := range desired {
		if !have[id] {
			s.graph.Link(kind, fromID, id, map[string]any{"source": source, "confidence": conf})
		}
	}
}

// ResolveDataFlow creates DATA_FLOW edges between functions where data
// of an owned type flows from a callee back to its caller. When function
// A calls B and A (or A's owner type) USES the type that owns B,
// B's return/produced data flows into A. Edges are DATA_FLOW B->A.
func (s *CodeIndexService) ResolveDataFlow(projectID string) error {
	fns, err := s.graph.FindNodes("Function", map[string]any{"project_id": projectID})
	if err != nil {
		return err
	}
	fnOwner := map[string]string{}
	fnRefs := map[string][]string{}
	for _, f := range fns {
		fnOwner[f.ID] = strProp(f, "owner")
		fnRefs[f.ID] = strSlice(f.Properties["refs"])
	}
	// ownerUses: which types does each owner struct/class reference (from its own refs).
	ownerUses := map[string]map[string]bool{}
	for _, kind := range []string{"Class", "Struct"} {
		nodes, _ := s.graph.FindNodes(kind, map[string]any{"project_id": projectID})
		for _, n := range nodes {
			name := strProp(n, "name")
			refs := strSlice(n.Properties["refs"])
			if ownerUses[name] == nil {
				ownerUses[name] = map[string]bool{}
			}
			for _, r := range refs {
				ownerUses[name][r] = true
			}
		}
	}
	for _, caller := range fns {
		callerID := caller.ID
		nbrs, _ := s.graph.Neighbors(callerID, "CALLS", graph.DirOut)
		for _, en := range nbrs {
			callee := en.Node.ID
			calleeOwner := fnOwner[callee]
			if calleeOwner == "" {
				continue
			}
			callerUses := false
			if fnOwner[callerID] == calleeOwner {
				callerUses = true
			}
			if !callerUses {
				for _, r := range fnRefs[callerID] {
					if r == calleeOwner {
						callerUses = true
						break
					}
				}
			}
			if !callerUses {
				callerOwner := fnOwner[callerID]
				if ou, ok := ownerUses[callerOwner]; ok {
					if ou[calleeOwner] {
						callerUses = true
					}
				}
			}
			if !callerUses {
				continue
			}
			existing, _ := s.graph.Neighbors(callee, "DATA_FLOW", graph.DirOut)
			dup := false
			for _, e := range existing {
				if e.Node.ID == callerID {
					dup = true
					break
				}
			}
			if !dup {
				s.graph.Link("DATA_FLOW", callee, callerID, map[string]any{"source": "type-directed"})
			}
		}
	}
	return nil
}

func langGroup(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if sp := specFor(ext); sp != nil {
		if sp.group != "" {
			return sp.group
		}
		return sp.name
	}
	switch ext {
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs":
		return "js"
	case ".c", ".h", ".cc", ".cpp", ".hpp", ".cxx", ".hxx", ".hh":
		return "c"
	case "":
		return ""
	default:
		return strings.ToLower(filepath.Ext(path))
	}
}

// asmLinkable are the groups whose functions are commonly implemented in or
// called from assembly (C ABI, Go's Plan 9 stubs, Rust extern "C").
var asmLinkable = map[string]bool{"c": true, ".go": true, ".rs": true}

// groupsCompatible reports whether a call may resolve across two language
// groups: only within a group, or between assembly and a native language.
func groupsCompatible(a, b string) bool {
	return a == b || (a == "asm" && asmLinkable[b]) || (b == "asm" && asmLinkable[a])
}

func pickTargets(candidates []string, pathOf map[string]string, callerPath string) ([]string, float64) {
	// A Go call never resolves to a Python function of the same name.
	group := langGroup(callerPath)
	filtered := candidates[:0:0]
	for _, id := range candidates {
		if groupsCompatible(langGroup(pathOf[id]), group) {
			filtered = append(filtered, id)
		}
	}
	candidates = filtered
	if len(candidates) == 0 {
		return nil, 0
	}
	var same []string
	for _, id := range candidates {
		if pathOf[id] == callerPath {
			same = append(same, id)
		}
	}
	if len(same) > 0 {
		return same, confSameFile
	}
	if len(candidates) == 1 {
		return candidates, confUniqueName
	}
	if len(candidates) <= maxAmbiguousFan {
		return candidates, confAmbiguous
	}
	return nil, 0
}

func encodeRelation(r typeRelation) string { return r.sub + "|" + r.rel + "|" + r.super }

func decodeRelation(s string) (typeRelation, bool) {
	p := strings.Split(s, "|")
	if len(p) != 3 {
		return typeRelation{}, false
	}
	return typeRelation{p[0], p[1], p[2]}, true
}

// ResolveInheritance makes EXTENDS / IMPLEMENTS edges between Class and Struct
// nodes match the relations recorded on each SourceFile, adding missing edges
// and removing tree-sitter edges that no file declares any more (for example a
// deleted Rust `impl` in another file). Like ResolveCalls it is idempotent and
// order-independent. Bases not defined in the project stay unresolved.
func (s *CodeIndexService) ResolveInheritance(projectID string) error {
	files, err := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID})
	if err != nil {
		return err
	}
	byName := map[string][]string{}
	pathOf := map[string]string{}
	var typeIDs []string
	for _, kind := range []string{"Class", "Struct"} {
		nodes, err := s.graph.FindNodes(kind, map[string]any{"project_id": projectID})
		if err != nil {
			return err
		}
		for _, n := range nodes {
			if name, _ := n.Properties["name"].(string); name != "" {
				byName[name] = append(byName[name], n.ID)
			}
			pathOf[n.ID], _ = n.Properties["path"].(string)
			typeIDs = append(typeIDs, n.ID)
		}
	}
	desired := map[string]map[string]map[string]float64{} // sub -> rel -> super -> confidence
	for _, f := range files {
		path, _ := f.Properties["path"].(string)
		for _, enc := range strSlice(f.Properties["type_relations"]) {
			r, ok := decodeRelation(enc)
			if !ok {
				continue
			}
			subs, _ := pickTargets(byName[r.sub], pathOf, path)
			supers, conf := pickTargets(byName[r.super], pathOf, path)
			for _, sub := range subs {
				for _, sup := range supers {
					if sub == sup {
						continue
					}
					if desired[sub] == nil {
						desired[sub] = map[string]map[string]float64{}
					}
					if desired[sub][r.rel] == nil {
						desired[sub][r.rel] = map[string]float64{}
					}
					if conf > desired[sub][r.rel][sup] {
						desired[sub][r.rel][sup] = conf
					}
				}
			}
		}
	}
	for _, id := range typeIDs {
		for _, rel := range []string{relExtends, relImplements} {
			s.syncEdges(id, rel, desired[id][rel])
		}
	}
	return nil
}
