package services

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"codergag/internal/graph"
	"codergag/internal/models"
)

const defaultModuleDepth = 2

type edgeLister interface {
	EdgesOfKind(kind string) []graph.EdgeRef
}

// moduleOf buckets a file into a module: its directory, cut to depth segments.
func moduleOf(rel string, depth int) string {
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." || dir == "" {
		return "."
	}
	parts := strings.Split(dir, "/")
	if len(parts) > depth {
		parts = parts[:depth]
	}
	return strings.Join(parts, "/")
}

type moduleStats struct {
	name                      string
	files, funcs, types, gens int
}

// sccs returns the strongly connected components of a module graph (Tarjan).
func sccs(nodes []string, adj map[string]map[string]int) [][]string {
	index, low := map[string]int{}, map[string]int{}
	on := map[string]bool{}
	var stack []string
	var out [][]string
	next := 0
	var visit func(v string)
	visit = func(v string) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		on[v] = true
		var succ []string
		for w := range adj[v] {
			succ = append(succ, w)
		}
		sort.Strings(succ)
		for _, w := range succ {
			if _, seen := index[w]; !seen {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if on[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] == index[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				on[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			sort.Strings(comp)
			out = append(out, comp)
		}
	}
	for _, n := range nodes {
		if _, seen := index[n]; !seen {
			visit(n)
		}
	}
	return out
}

// ArchitectureReport builds a compact Markdown overview from the graph alone:
// modules with sizes and dependencies, dependency layers, cycles, entry points,
// the most called functions and most referenced types. It is meant to be read
// by an agent in place of exploring the tree file by file.
func (a *Application) ArchitectureReport(projectID string, depth, limit int) (map[string]any, error) {
	ps, _ := a.Graph.FindNodes("Project", map[string]any{"id": projectID})
	if len(ps) == 0 {
		return nil, fmt.Errorf("project %q has not been indexed", projectID)
	}
	if depth <= 0 {
		depth = defaultModuleDepth
	}
	if limit <= 0 {
		limit = 10
	}
	root := strProp(ps[0], "path")
	rel := func(p string) string { return relPath(root, p) }

	files, _ := a.Graph.FindNodes("SourceFile", map[string]any{"project_id": projectID})
	if len(files) == 0 {
		return nil, fmt.Errorf("project %q has no indexed files", projectID)
	}
	mods := map[string]*moduleStats{}
	modOfNode := map[string]string{} // node id -> module, for files, functions and types
	modOfPath := map[string]string{}
	langs := map[string]int{}
	generated, stale := 0, 0
	stat := func(name string) *moduleStats {
		if mods[name] == nil {
			mods[name] = &moduleStats{name: name}
		}
		return mods[name]
	}
	for _, f := range files {
		m := moduleOf(rel(strProp(f, "path")), depth)
		modOfNode[f.ID] = m
		modOfPath[strProp(f, "path")] = m
		st := stat(m)
		st.files++
		langs[strProp(f, "language")]++
		if g, _ := f.Properties["generated"].(bool); g {
			st.gens++
			generated++
		}
		if strProp(f, "parser_version") != a.Index.ParserVersion() {
			stale++
		}
	}
	byKind := map[string][]*models.Node{}
	for _, kind := range []string{"Function", "Class", "Struct"} {
		nodes, _ := a.Graph.FindNodes(kind, map[string]any{"project_id": projectID})
		byKind[kind] = nodes
		for _, n := range nodes {
			m, ok := modOfPath[strProp(n, "path")]
			if !ok {
				continue
			}
			modOfNode[n.ID] = m
			if kind == "Function" {
				stat(m).funcs++
			} else {
				stat(m).types++
			}
		}
	}

	edges := func(kind string) []graph.EdgeRef {
		if el, ok := a.Graph.(edgeLister); ok {
			return el.EdgesOfKind(kind)
		}
		return nil
	}

	// Module dependency graph from resolved imports.
	adj := map[string]map[string]int{}
	fanIn := map[string]map[string]int{}
	for _, e := range edges("DEPENDS_ON") {
		from, to := modOfNode[e.From], modOfNode[e.To]
		if from == "" || to == "" || from == to {
			continue
		}
		if adj[from] == nil {
			adj[from] = map[string]int{}
		}
		adj[from][to]++
		if fanIn[to] == nil {
			fanIn[to] = map[string]int{}
		}
		fanIn[to][from]++
	}
	names := make([]string, 0, len(mods))
	for n := range mods {
		names = append(names, n)
	}
	sort.Strings(names)

	// Layers: condense cycles, then level = longest dependency chain below a module.
	comps := sccs(names, adj)
	compOf := map[string]int{}
	for i, c := range comps {
		for _, m := range c {
			compOf[m] = i
		}
	}
	level := make([]int, len(comps))
	memo := map[int]int{}
	var resolve func(i int) int // the condensed graph is a DAG, so plain recursion terminates
	resolve = func(i int) int {
		if v, ok := memo[i]; ok {
			return v
		}
		best := 0
		for _, m := range comps[i] {
			for dep := range adj[m] {
				if j := compOf[dep]; j != i {
					best = max(best, resolve(j)+1)
				}
			}
		}
		memo[i] = best
		return best
	}
	maxLevel := 0
	for i := range comps {
		level[i] = resolve(i)
		maxLevel = max(maxLevel, level[i])
	}

	var sb strings.Builder
	totalFns, totalTypes := len(byKind["Function"]), len(byKind["Class"])+len(byKind["Struct"])
	fmt.Fprintf(&sb, "# Architecture: %s\n%d files, %d functions, %d types in %d modules · %s\n",
		projectID, len(files), totalFns, totalTypes, len(mods), langMix(langs, len(files)))

	// Modules, largest first.
	sort.Slice(names, func(i, j int) bool {
		a, b := mods[names[i]], mods[names[j]]
		if a.funcs != b.funcs {
			return a.funcs > b.funcs
		}
		return a.name < b.name
	})
	sb.WriteString("\n## Modules\n")
	for _, n := range capRows(names, limit) {
		m := mods[n]
		fmt.Fprintf(&sb, "- `%s` — %d files, %d fns, %d types", n, m.files, m.funcs, m.types)
		if d := topEdges(adj[n], 4); d != "" {
			fmt.Fprintf(&sb, " · depends on %s", d)
		}
		if u := topEdges(fanIn[n], 4); u != "" {
			fmt.Fprintf(&sb, " · used by %s", u)
		}
		sb.WriteString("\n")
	}
	if len(names) > limit {
		fmt.Fprintf(&sb, "- … %d more modules (raise limit)\n", len(names)-limit)
	}

	if len(adj) > 0 {
		sb.WriteString("\n## Layers (foundation first)\n")
		byLevel := map[int][]string{}
		for i, c := range comps {
			byLevel[level[i]] = append(byLevel[level[i]], strings.Join(c, " ⇄ "))
		}
		for l := 0; l <= maxLevel; l++ {
			sort.Strings(byLevel[l])
			fmt.Fprintf(&sb, "%d. %s\n", l, joinMax(byLevel[l], limit))
		}
	}

	var cycles []string
	for _, c := range comps {
		if len(c) > 1 {
			cycles = append(cycles, strings.Join(c, " ⇄ "))
		}
	}
	sort.Strings(cycles)
	if len(cycles) > 0 {
		sb.WriteString("\n## Dependency cycles\n")
		for _, c := range capRows(cycles, limit) {
			fmt.Fprintf(&sb, "- %s\n", c)
		}
	}

	if eps, _ := a.Analysis.EntryPoints(projectID, limit); len(eps) > 0 {
		sb.WriteString("\n## Entry points\n")
		for _, e := range eps {
			fmt.Fprintf(&sb, "- `%v` (%s:%v)\n", e["name"], rel(fmt.Sprint(e["path"])), e["line_start"])
		}
	}

	nodeByID := map[string]*models.Node{}
	for _, kind := range []string{"Function", "Class", "Struct"} {
		for _, n := range byKind[kind] {
			nodeByID[n.ID] = n
		}
	}
	rank := func(counts map[string]int, n int, noun string) []string {
		ids := make([]string, 0, len(counts))
		for id := range counts {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			if counts[ids[i]] != counts[ids[j]] {
				return counts[ids[i]] > counts[ids[j]]
			}
			return ids[i] < ids[j]
		})
		var out []string
		for _, id := range capRows(ids, n) {
			if node := nodeByID[id]; node != nil {
				out = append(out, fmt.Sprintf("`%s` — %s %d (%s)", qualifiedLabel(node), noun, counts[id], rel(strProp(node, "path"))))
			}
		}
		return out
	}
	called := map[string]int{}
	for _, e := range edges("CALLS") {
		called[e.To]++
	}
	if top := rank(called, limit, "called by"); len(top) > 0 {
		sb.WriteString("\n## Most called (distinct callers)\n")
		for _, t := range top {
			fmt.Fprintf(&sb, "- %s\n", t)
		}
	}
	referenced := map[string]int{}
	for _, kind := range []string{"USES", "EXTENDS", "IMPLEMENTS"} {
		for _, e := range edges(kind) {
			if n := nodeByID[e.To]; n != nil && n.Kind != "Function" {
				referenced[e.To]++
			}
		}
	}
	if top := rank(referenced, limit, "referenced by"); len(top) > 0 {
		sb.WriteString("\n## Core types (most referenced)\n")
		for _, t := range top {
			fmt.Fprintf(&sb, "- %s\n", t)
		}
	}

	var warnings []string
	if len(cycles) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d module dependency cycle(s)", len(cycles)))
	}
	if generated > 0 && generated*100/len(files) >= 30 {
		warnings = append(warnings, fmt.Sprintf("%d%% of files are generated", generated*100/len(files)))
	}
	if stale > 0 {
		warnings = append(warnings, fmt.Sprintf("%d file(s) indexed by an old parser version; re-index", stale))
	}
	if len(adj) == 0 && len(files) > 1 {
		warnings = append(warnings, "no cross-module imports resolved; dependency sections are empty")
	}
	if len(warnings) > 0 {
		sb.WriteString("\n## Warnings\n")
		for _, w := range warnings {
			fmt.Fprintf(&sb, "- %s\n", w)
		}
	}

	md := sb.String()
	return map[string]any{
		"markdown": md, "modules": len(mods), "cycles": len(cycles), "layers": maxLevel + 1,
		"approx_tokens": approxTokensOf(md),
	}, nil
}

func approxTokensOf(s string) int { return (len(s) + 3) / 4 }

func langMix(langs map[string]int, total int) string {
	type kv struct {
		k string
		v int
	}
	var rows []kv
	for k, v := range langs {
		rows = append(rows, kv{k, v})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].v != rows[j].v {
			return rows[i].v > rows[j].v
		}
		return rows[i].k < rows[j].k
	})
	var parts []string
	for _, r := range capRows(rows, 4) {
		parts = append(parts, fmt.Sprintf("%s %d%%", r.k, r.v*100/max(total, 1)))
	}
	return strings.Join(parts, ", ")
}

// topEdges renders the heaviest neighbors as "a (12), b (3)".
func topEdges(m map[string]int, n int) string {
	type kv struct {
		k string
		v int
	}
	var rows []kv
	for k, v := range m {
		rows = append(rows, kv{k, v})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].v != rows[j].v {
			return rows[i].v > rows[j].v
		}
		return rows[i].k < rows[j].k
	})
	var parts []string
	for _, r := range capRows(rows, n) {
		parts = append(parts, fmt.Sprintf("`%s` (%d)", r.k, r.v))
	}
	return strings.Join(parts, ", ")
}
