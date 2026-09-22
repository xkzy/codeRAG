package services

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"codergag/internal/graph"
)

// SmartRead returns a structured skeleton of a file: language, size, symbols
// (functions/classes/structs with line ranges), imports and exports.
// It is the code-context equivalent of cctx-mcp's smart_read: read the
// structure instead of reading the whole file.
func (a *Application) SmartRead(projectID, file string) (map[string]any, error) {
	projects, err := a.Graph.FindNodes("Project", map[string]any{"id": projectID})
	if err != nil || len(projects) == 0 {
		return nil, fmt.Errorf("project %q not found", projectID)
	}
	root := strProp(projects[0], "path")
	if root == "" {
		return nil, fmt.Errorf("project %q has no root path", projectID)
	}

	absFile := file
	if !filepath.IsAbs(file) {
		absFile = filepath.Join(root, file)
	}
	absFile, _ = filepath.Abs(absFile)

	files, err := a.Graph.FindNodes("SourceFile", map[string]any{"project_id": projectID, "path": absFile})
	if err != nil || len(files) == 0 {
		rel, _ := filepath.Rel(root, absFile)
		if rel == "" {
			rel = absFile
		}
		return nil, fmt.Errorf("file %s is not indexed in project %q", rel, projectID)
	}
	f := files[0]

	rel, _ := filepath.Rel(root, strProp(f, "path"))
	if rel == "" {
		rel = strProp(f, "path")
	}

	size := 0
	if data, err := os.ReadFile(strProp(f, "path")); err == nil {
		size = len(data)
	}

	symbols := make([]map[string]any, 0)
	for _, kind := range []string{"Function", "Class", "Struct", "Module", "Interface", "Enum", "Trait"} {
		snodes, _ := a.Graph.FindNodes(kind, map[string]any{"project_id": projectID, "path": absFile})
		for _, n := range snodes {
			sym := map[string]any{
				"kind": kind,
				"name": strProp(n, "name"),
				"id":   n.ID,
			}
			ls, _ := n.Properties["line_start"].(int)
			if ls != 0 {
				sym["line_start"] = ls
			}
			le, _ := n.Properties["line_end"].(int)
			if le != 0 && le != ls {
				sym["line_end"] = le
			}
			if sig := strProp(n, "signature"); sig != "" {
				sym["signature"] = sig
			}
			symbols = append(symbols, sym)
		}
	}
	sort.Slice(symbols, func(i, j int) bool {
		a, b := symbols[i]["line_start"].(int), symbols[j]["line_start"].(int)
		if a != b {
			return a < b
		}
		return symbols[i]["name"].(string) < symbols[j]["name"].(string)
	})

	imports := make([]map[string]any, 0)
	deps, _ := a.Graph.Neighbors(f.ID, "DEPENDS_ON", graph.DirOut)
	for _, en := range deps {
		imp := map[string]any{
			"path": relPath(root, strProp(en.Node, "path")),
			"id":   en.Node.ID,
		}
		if st, ok := en.Node.Properties["language"].(string); ok {
			imp["language"] = st
		}
		imports = append(imports, imp)
	}
	sort.Slice(imports, func(i, j int) bool {
		return imports[i]["path"].(string) < imports[j]["path"].(string)
	})

	exports := make([]map[string]any, 0)
	defined, _ := a.Graph.Neighbors(f.ID, "DEFINES", graph.DirIn)
	for _, en := range defined {
		exports = append(exports, map[string]any{
			"kind": en.Node.Kind,
			"name": strProp(en.Node, "name"),
			"id":   en.Node.ID,
		})
	}
	sort.Slice(exports, func(i, j int) bool {
		a, b := exports[i]["kind"].(string), exports[j]["kind"].(string)
		if a != b {
			return a < b
		}
		return exports[i]["name"].(string) < exports[j]["name"].(string)
	})

	return map[string]any{
		"project_id": projectID,
		"file":       rel,
		"absolute":   strProp(f, "path"),
		"language":   strProp(f, "language"),
		"size_bytes": size,
		"line_count": countLines(strProp(f, "path")),
		"generated":  f.Properties["generated"],
		"symbols":    symbols,
		"imports":    imports,
		"exports":    exports,
	}, nil
}

// AnalyzeProject returns a compact JSON overview of a project: file counts by
// language, symbol counts by kind, generated/stale stats.
// It complements generate_architecture_report (which returns Markdown) with a
// structured summary suitable for programmatic consumption.
func (a *Application) AnalyzeProject(projectID string) (map[string]any, error) {
	projects, err := a.Graph.FindNodes("Project", map[string]any{"id": projectID})
	if err != nil || len(projects) == 0 {
		return nil, fmt.Errorf("project %q not found", projectID)
	}
	root := strProp(projects[0], "path")

	files, _ := a.Graph.FindNodes("SourceFile", map[string]any{"project_id": projectID})
	langs := map[string]int{}
	generated, stale := 0, 0
	totalLines := 0
	for _, f := range files {
		lang := strProp(f, "language")
		if lang != "" {
			langs[lang]++
		}
		if g, _ := f.Properties["generated"].(bool); g {
			generated++
		}
		if strProp(f, "parser_version") != a.Index.ParserVersion() {
			stale++
		}
		if l, ok := f.Properties["line_count"].(int); ok {
			totalLines += l
		} else if data, err := os.ReadFile(strProp(f, "path")); err == nil {
			totalLines += strings.Count(string(data), "\n") + 1
		}
	}

	symbolKinds := []string{"Function", "Class", "Struct", "Interface", "Enum", "Trait", "Module", "Macro", "Method"}
	symbolsByKind := map[string]int{}
	for _, kind := range symbolKinds {
		nodes, _ := a.Graph.FindNodes(kind, map[string]any{"project_id": projectID})
		if len(nodes) > 0 {
			symbolsByKind[kind] = len(nodes)
		}
	}
	totalSymbols := 0
	for _, c := range symbolsByKind {
		totalSymbols += c
	}

	dirs := map[string]bool{}
	for _, f := range files {
		rel, _ := filepath.Rel(root, strProp(f, "path"))
		dir := filepath.ToSlash(filepath.Dir(rel))
		for dir != "" && dir != "." {
			dirs[dir] = true
			dir = filepath.ToSlash(filepath.Dir(dir))
		}
	}

	sortByCount := func(m map[string]int) []map[string]any {
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
		out := make([]map[string]any, len(rows))
		for i, r := range rows {
			out[i] = map[string]any{"name": r.k, "count": r.v}
		}
		return out
	}

	return map[string]any{
		"project_id":    projectID,
		"root":          root,
		"files":         len(files),
		"languages":     langs,
		"language_rows": sortByCount(langs),
		"total_lines":   totalLines,
		"symbols":       symbolsByKind,
		"total_symbols": totalSymbols,
		"directories":   len(dirs),
		"generated":     generated,
		"stale":         stale,
	}, nil
}

func countLines(path string) int {
	if path == "" {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "\n") + 1
}
