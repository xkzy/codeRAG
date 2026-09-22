package services

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxGoPackageFiles = 20
	sourceImport      = "import-resolver"
)

// ResolveReferences links each Function, and each class or struct through its
// fields, to the classes and structs it refers to
// without calling: type annotations, composite literals, `new X()`, static
// access such as `Type.member`, `Type::new`, and constructor calls like `Foo()`
// that name a class rather than a function. Edges are USES edges; they are
// added and removed to match the code, like CALLS.
func (s *CodeIndexService) ResolveReferences(projectID string) error {
	fns, err := s.graph.FindNodes("Function", map[string]any{"project_id": projectID})
	if err != nil {
		return err
	}
	typesByName := map[string][]string{}
	pathOf := map[string]string{}
	for _, kind := range []string{"Class", "Struct"} {
		nodes, err := s.graph.FindNodes(kind, map[string]any{"project_id": projectID})
		if err != nil {
			return err
		}
		for _, n := range nodes {
			typesByName[strProp(n, "name")] = append(typesByName[strProp(n, "name")], n.ID)
			pathOf[n.ID] = strProp(n, "path")
		}
	}
	funcNames := map[string]bool{}
	for _, f := range fns {
		funcNames[strProp(f, "name")] = true
	}
	// Classes and structs use the types their fields mention.
	for _, kind := range []string{"Class", "Struct"} {
		nodes, _ := s.graph.FindNodes(kind, map[string]any{"project_id": projectID})
		for _, n := range nodes {
			desired := map[string]float64{}
			for _, name := range strSlice(n.Properties["refs"]) {
				targets, conf := pickTargets(typesByName[name], pathOf, strProp(n, "path"))
				for _, id := range targets {
					if id != n.ID && conf > desired[id] {
						desired[id] = conf
					}
				}
			}
			s.syncEdgesAs(n.ID, "USES", "tree-sitter", desired)
		}
	}
	for _, f := range fns {
		if len(f.Properties) == 0 {
			continue
		}
		owner := strProp(f, "owner")
		names := append([]string{}, strSlice(f.Properties["refs"])...)
		for _, c := range strSlice(f.Properties["calls"]) {
			if !funcNames[c] { // `Foo()` where Foo is a class, not a function
				names = append(names, c)
			}
		}
		desired := map[string]float64{}
		for _, name := range names {
			if name == owner {
				continue // methods constantly mention their own type
			}
			targets, conf := pickTargets(typesByName[name], pathOf, strProp(f, "path"))
			for _, id := range targets {
				if conf > desired[id] {
					desired[id] = conf
				}
			}
		}
		s.syncEdgesAs(f.ID, "USES", "tree-sitter", desired)
	}
	return nil
}

// pathIndex answers "which indexed file does this import mean?" without
// scanning every file per import.
type pathIndex struct {
	root     string // project root
	goModule string // module path from the root go.mod, if any
	idByPath map[string]string
	byBase   map[string][]string // base name -> paths
	goDirs   map[string][]string // dir -> non-test .go files
}

var goModuleRe = regexp.MustCompile(`(?m)^\s*module\s+(\S+)`)

func newPathIndex(root string, files map[string]string) *pathIndex {
	ix := &pathIndex{root: root, idByPath: files, byBase: map[string][]string{}, goDirs: map[string][]string{}}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		base := filepath.Base(p)
		ix.byBase[base] = append(ix.byBase[base], p)
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			ix.goDirs[filepath.Dir(p)] = append(ix.goDirs[filepath.Dir(p)], p)
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil {
		if m := goModuleRe.FindSubmatch(data); m != nil {
			ix.goModule = strings.Trim(string(m[1]), `"`)
		}
	}
	return ix
}

func (ix *pathIndex) exact(p string) []string {
	if _, ok := ix.idByPath[p]; ok {
		return []string{p}
	}
	return nil
}

// uniqueSuffix returns the single indexed file named base whose path ends with
// "/"+tail. Ambiguity yields nothing rather than a guess.
func (ix *pathIndex) uniqueSuffix(base, tail string) []string {
	var hits []string
	for _, p := range ix.byBase[base] {
		if strings.HasSuffix(p, "/"+tail) {
			hits = append(hits, p)
		}
	}
	if len(hits) == 1 {
		return hits
	}
	return nil
}

var scriptExts = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}

// resolveImport maps one import target, as written in file, to indexed files.
func (ix *pathIndex) resolveImport(file, target string) []string {
	dir := filepath.Dir(file)
	switch strings.ToLower(filepath.Ext(file)) {
	case ".py":
		if strings.HasPrefix(target, ".") { // relative: from .pkg import x
			rel := strings.TrimLeft(target, ".")
			up := len(target) - len(rel) - 1
			base := dir
			for i := 0; i < up; i++ {
				base = filepath.Dir(base)
			}
			joined := filepath.Join(base, strings.ReplaceAll(rel, ".", "/"))
			if rel == "" {
				return ix.exact(filepath.Join(base, "__init__.py"))
			}
			return append(ix.exact(joined+".py"), ix.exact(filepath.Join(joined, "__init__.py"))...)
		}
		rel := strings.ReplaceAll(target, ".", "/")
		if hits := ix.uniqueSuffix(filepath.Base(rel)+".py", rel+".py"); hits != nil {
			return hits
		}
		return ix.uniqueSuffix("__init__.py", rel+"/__init__.py")
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs":
		if !strings.HasPrefix(target, ".") {
			return nil // bare package name: external
		}
		base := filepath.Clean(filepath.Join(dir, target))
		for _, e := range []string{".js", ".jsx", ".mjs", ".cjs"} { // TS imports may name the compiled extension
			base = strings.TrimSuffix(base, e)
		}
		for _, e := range scriptExts {
			if hit := ix.exact(base + e); hit != nil {
				return hit
			}
		}
		for _, e := range scriptExts {
			if hit := ix.exact(filepath.Join(base, "index"+e)); hit != nil {
				return hit
			}
		}
	case ".java":
		if strings.HasSuffix(target, ".") || strings.HasSuffix(target, "*") {
			return nil // wildcard import
		}
		rel := strings.ReplaceAll(target, ".", "/") + ".java"
		return ix.uniqueSuffix(filepath.Base(rel), rel)
	case ".go":
		if ix.goModule != "" && (target == ix.goModule || strings.HasPrefix(target, ix.goModule+"/")) {
			// An import inside this module maps to an exact directory.
			hits := ix.goDirs[filepath.Join(ix.root, strings.TrimPrefix(strings.TrimPrefix(target, ix.goModule), "/"))]
			if len(hits) > maxGoPackageFiles {
				hits = hits[:maxGoPackageFiles]
			}
			return hits
		}
		segs := strings.Split(target, "/")
		n := 2
		if len(segs) < 2 {
			n = 1
		}
		tail := strings.Join(segs[len(segs)-n:], "/")
		var hits []string
		for d, files := range ix.goDirs {
			if strings.HasSuffix(d, "/"+tail) {
				hits = append(hits, files...)
			}
		}
		if len(hits) > maxGoPackageFiles {
			hits = hits[:maxGoPackageFiles]
		}
		sort.Strings(hits)
		return hits
	case ".c", ".h", ".cc", ".cpp", ".hpp":
		if hit := ix.exact(filepath.Clean(filepath.Join(dir, target))); hit != nil {
			return hit
		}
		return ix.uniqueSuffix(filepath.Base(target), target)
	case ".rs":
		return append(ix.exact(filepath.Join(dir, target+".rs")), ix.exact(filepath.Join(dir, target, "mod.rs"))...)
	}
	return nil
}

// ResolveImports links each SourceFile to the project files it imports with
// DEPENDS_ON edges (external packages resolve to nothing). This gives real
// file-level dependencies, unlike the name-only IMPORTS edges to Module nodes.
func (s *CodeIndexService) ResolveImports(projectID string) error {
	files, err := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID})
	if err != nil {
		return err
	}
	idByPath := make(map[string]string, len(files))
	for _, f := range files {
		idByPath[strProp(f, "path")] = f.ID
	}
	root := ""
	if ps, _ := s.graph.FindNodes("Project", map[string]any{"id": projectID}); len(ps) > 0 {
		root = strProp(ps[0], "path")
	}
	ix := newPathIndex(root, idByPath)
	for _, f := range files {
		path := strProp(f, "path")
		desired := map[string]float64{}
		for _, target := range strSlice(f.Properties["imports"]) {
			for _, hit := range ix.resolveImport(path, target) {
				if id := idByPath[hit]; id != f.ID {
					desired[id] = 0.9
				}
			}
		}
		s.syncEdgesAs(f.ID, "DEPENDS_ON", sourceImport, desired)
	}
	return nil
}

func isExported(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

// ResolveReferencesWithIndex resolves references using a pre-built index.
func (s *CodeIndexService) ResolveReferencesWithIndex(projectID string, idx *ResolverIndex) error {
	typesByName := idx.TypesByName
	pathOf := idx.TypePaths
	funcNames := idx.FuncNames

	// Classes and structs use the types their fields mention.
	for _, n := range idx.ClassStructs {
		id := n.ID
		desired := map[string]float64{}
		for _, name := range strSlice(n.Properties["refs"]) {
			targets, conf := pickTargets(typesByName[name], pathOf, idx.TypePaths[id])
			for _, tid := range targets {
				if tid != id && conf > desired[tid] {
					desired[tid] = conf
				}
			}
		}
		s.syncEdgesAs(id, "USES", "tree-sitter", desired)
	}
	for _, f := range idx.Functions {
		if len(f.Properties) == 0 {
			continue
		}
		owner := strProp(f, "owner")
		names := append([]string{}, strSlice(f.Properties["refs"])...)
		for _, c := range strSlice(f.Properties["calls"]) {
			if !funcNames[c] {
				names = append(names, c)
			}
		}
		desired := map[string]float64{}
		for _, name := range names {
			if name == owner {
				continue
			}
			targets, conf := pickTargets(typesByName[name], pathOf, strProp(f, "path"))
			for _, id := range targets {
				if conf > desired[id] {
					desired[id] = conf
				}
			}
		}
		s.syncEdgesAs(f.ID, "USES", "tree-sitter", desired)
	}
	return nil
}

// ResolveImportsWithIndex resolves imports using a pre-built index.
func (s *CodeIndexService) ResolveImportsWithIndex(projectID string, idx *ResolverIndex) error {
	files, err := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID})
	if err != nil {
		return err
	}
	idByPath := make(map[string]string, len(files))
	for _, f := range files {
		idByPath[strProp(f, "path")] = f.ID
	}
	root := ""
	if ps, _ := s.graph.FindNodes("Project", map[string]any{"id": projectID}); len(ps) > 0 {
		root = strProp(ps[0], "path")
	}
	ix := newPathIndex(root, idByPath)
	for _, f := range files {
		path := strProp(f, "path")
		desired := map[string]float64{}
		for _, target := range strSlice(f.Properties["imports"]) {
			for _, hit := range ix.resolveImport(path, target) {
				if id := idByPath[hit]; id != f.ID {
					desired[id] = 0.9
				}
			}
		}
		s.syncEdgesAs(f.ID, "DEPENDS_ON", sourceImport, desired)
	}
	return nil
}
