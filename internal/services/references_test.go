package services

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"codergag/internal/graph"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, src := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func indexTree(t *testing.T, files map[string]string) (*Application, string) {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, files)
	app := ApplicationInMemory()
	if _, err := app.Index.IndexRepository("p", dir, true, nil, false); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(dir)
	return app, real
}

func usesOf(t *testing.T, app *Application, fn string) []string {
	t.Helper()
	nodes, _ := app.Graph.FindNodes("Function", map[string]any{"project_id": "p", "name": fn})
	var out []string
	for _, n := range nodes {
		nbrs, _ := app.Graph.Neighbors(n.ID, "USES", graph.DirOut)
		for _, en := range nbrs {
			out = append(out, strProp(en.Node, "name"))
		}
	}
	sort.Strings(out)
	return out
}

func TestReferenceResolverLinksTypeUsesAcrossLanguages(t *testing.T) {
	app, _ := indexTree(t, map[string]string{
		"types.go":  "package x\ntype Widget struct{}\ntype Gadget struct{}\ntype Unused struct{}\n",
		"use.go":    "package x\nfunc Make() *Widget { g := Gadget{}; _ = g; return &Widget{} }\nfunc (w *Widget) Own() { _ = w }\n",
		"w.py":      "class Widget:\n    pass\n\ndef build(x: Widget):\n    return Widget()\n\ndef unrelated():\n    return 1\n",
		"w.ts":      "class Box {}\nfunction mk() { return new Box(); }\n",
		"Reg.java":  "class Registry { static void get() {} }\nclass App { void run() { Registry.get(); } }\n",
		"lib.rs":    "struct Cell {}\nfn build_cell() { let c = Cell::new(); }\n",
		"c.c":       "struct node { int v; };\nvoid f(void) { struct node *n; }\n",
		"shadow.go": "package x\nfunc Widget2() {}\nfunc CallsFunc() { Widget2() }\n",
	})
	cases := map[string][]string{
		"Make":      {"Gadget", "Widget"}, // composite literal + return type, deduplicated
		"Own":       nil,                  // a method never "uses" its own receiver type
		"build":     {"Widget"},           // Python annotation and Widget() constructor call
		"mk":        {"Box"},              // new Box()
		"run":       {"Registry"},         // Registry.get() qualifier
		"f":         {"node"},             // C struct type use
		"unrelated": nil,
		"CallsFunc": nil, // calls a function, not a type
	}
	for fn, want := range cases {
		got := usesOf(t, app, fn)
		if len(want) == 0 && len(got) == 0 {
			continue
		}
		// Widget exists as a Go struct and a Python class: references stay within their language.
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s uses %v, want %v", fn, got, want)
		}
	}
	// Rust: Cell::new() references the type Cell.
	if got := usesOf(t, app, "build_cell"); strings.Join(got, ",") != "Cell" {
		t.Errorf("build_cell uses %v, want Cell", got)
	}
}

func TestReferenceEdgesAreMaintained(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.go": "package x\ntype T struct{}\nfunc F() *T { return nil }\n"})
	app := ApplicationInMemory()
	app.Index.IndexRepository("p", dir, true, nil, false)
	if got := usesOf(t, app, "F"); len(got) != 1 {
		t.Fatalf("setup: %v", got)
	}
	writeTree(t, dir, map[string]string{"a.go": "package x\ntype T struct{}\nfunc F() int { return 1 }\n"})
	app.Index.IndexRepository("p", dir, true, nil, false)
	if got := usesOf(t, app, "F"); len(got) != 0 {
		t.Fatalf("stale USES edge survived: %v", got)
	}
}

func TestImportExtraction(t *testing.T) {
	cases := []struct {
		suffix, src string
		want        []string
	}{
		{".go", "package x\nimport \"fmt\"\nimport (\n\t\"os\"\n\tg \"example.com/a/graph\"\n)\n", []string{"fmt", "os", "example.com/a/graph"}},
		{".py", "import os, a.b as c\nfrom x.y import z\nfrom . import q\nfrom ..pkg import r\n", []string{"os", "a.b", "x.y", "x.y.z", ".", ".q", "..pkg", "..pkg.r"}},
		{".ts", "import a from './a';\nimport { b } from \"../b\";\nexport * from './c';\nimport 'side';\n", []string{"./a", "../b", "./c", "side"}},
		{".java", "package p;\nimport a.b.C;\nimport static a.b.D.m;\nimport a.*;\n", []string{"a.b.C", "a.b.D.m", "a.*"}},
		{".c", "#include <stdio.h>\n#include \"util/x.h\"\n", []string{"stdio.h", "util/x.h"}},
		{".rs", "mod inner;\nmod inline { fn f() {} }\n", []string{"inner"}},
	}
	for _, c := range cases {
		got, ok := extractImportsTreeSitter(c.src, c.suffix)
		if !ok || strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s imports = %v, want %v", c.suffix, got, c.want)
		}
	}
}

func dependsOn(t *testing.T, app *Application, root, rel string) []string {
	t.Helper()
	nodes, _ := app.Graph.FindNodes("SourceFile", map[string]any{"project_id": "p", "path": filepath.Join(root, rel)})
	if len(nodes) != 1 {
		t.Fatalf("file %s not indexed", rel)
	}
	nbrs, _ := app.Graph.Neighbors(nodes[0].ID, "DEPENDS_ON", graph.DirOut)
	var out []string
	for _, en := range nbrs {
		p, _ := filepath.Rel(root, strProp(en.Node, "path"))
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func TestImportsResolveToProjectFiles(t *testing.T) {
	app, root := indexTree(t, map[string]string{
		// Python: absolute, relative, package __init__, and an external import that must not resolve
		"py/pkg/__init__.py": "",
		"py/pkg/a.py":        "import os\nimport pkg.b\nfrom . import c\nfrom .d import x\nfrom pkg import sub\n",
		"py/pkg/b.py":        "",
		"py/pkg/c.py":        "",
		"py/pkg/d.py":        "x = 1\n",
		// TS: relative with compiled extension, extensionless, index file, bare package
		"ts/app.ts":       "import a from './lib/a.js';\nimport b from './lib/b';\nimport c from './lib';\nimport r from 'react';\n",
		"ts/lib/a.ts":     "export const a = 1;\n",
		"ts/lib/b.tsx":    "export const b = 1;\n",
		"ts/lib/index.ts": "export const c = 1;\n",
		// Java
		"j/com/acme/App.java":         "import com.acme.util.Helper;\nimport java.util.List;\nimport com.acme.util.*;\nclass App {}\n",
		"j/com/acme/util/Helper.java": "class Helper {}\n",
		// Go: package directory, external module ignored
		"g/cmd/main.go":               "package main\nimport (\n\t\"fmt\"\n\t\"example.com/proj/internal/store\"\n\t\"github.com/other/util\"\n)\nfunc main() {}\n",
		"g/internal/store/db.go":      "package store\n",
		"g/internal/store/kv.go":      "package store\n",
		"g/internal/store/db_test.go": "package store\n",
		// C includes: relative and unique suffix
		"c/main.c":       "#include \"local.h\"\n#include \"inc/shared.h\"\n#include <stdlib.h>\n",
		"c/local.h":      "",
		"c/inc/shared.h": "",
		// Rust mod
		"r/main.rs":       "mod util;\nmod nested;\nmod missing;\nfn main() {}\n",
		"r/util.rs":       "",
		"r/nested/mod.rs": "",
	})
	want := map[string][]string{
		"py/pkg/a.py":         {"py/pkg/__init__.py", "py/pkg/b.py", "py/pkg/c.py", "py/pkg/d.py"},
		"ts/app.ts":           {"ts/lib/a.ts", "ts/lib/b.tsx", "ts/lib/index.ts"},
		"j/com/acme/App.java": {"j/com/acme/util/Helper.java"},
		"g/cmd/main.go":       {"g/internal/store/db.go", "g/internal/store/kv.go"},
		"c/main.c":            {"c/inc/shared.h", "c/local.h"},
		"r/main.rs":           {"r/nested/mod.rs", "r/util.rs"},
	}
	for file, w := range want {
		got := dependsOn(t, app, root, file)
		if strings.Join(got, ",") != strings.Join(w, ",") {
			t.Errorf("%s DEPENDS_ON %v, want %v", file, got, w)
		}
	}
}

func TestImportEdgesFollowEditsAndFeedDependencyTools(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.ts": "import b from './b';\n", "b.ts": "export const b = 1;\n", "c.ts": "export const c = 1;\n"})
	app := ApplicationInMemory()
	app.Index.IndexRepository("p", dir, true, nil, false)
	real, _ := filepath.EvalSymlinks(dir)
	if got := dependsOn(t, app, real, "a.ts"); len(got) != 1 || got[0] != "b.ts" {
		t.Fatalf("setup: %v", got)
	}
	writeTree(t, dir, map[string]string{"a.ts": "import c from './c';\n"})
	app.Index.IndexRepository("p", dir, true, nil, false)
	if got := dependsOn(t, app, real, "a.ts"); len(got) != 1 || got[0] != "c.ts" {
		t.Fatalf("edge should move from b.ts to c.ts: %v", got)
	}
}
