package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDocCodeRefsIgnoresProseAndCommands(t *testing.T) {
	text := "Run `go test ./...` then see `Indexer.ParsePacket()` and `main.go`.\nThe `true` flag and ParseFrame( ) matter; also read_stream and the word Hello.\nUse FooBar for it."
	strong, weak, files := docCodeRefs(text)
	idents := append(append([]string{}, strong...), weak...)
	got := strings.Join(idents, ",")
	for _, want := range []string{"ParsePacket", "ParseFrame", "read_stream", "FooBar"} {
		if !strings.Contains(got, want) {
			t.Errorf("identifiers %q should include %s", got, want)
		}
	}
	for _, bad := range []string{"test", "true", "Hello", "go"} {
		for _, id := range idents {
			if id == bad {
				t.Errorf("%q is not a code reference", bad)
			}
		}
	}
	if len(files) != 1 || files[0] != "main.go" {
		t.Errorf("files = %v, want [main.go]", files)
	}
}

func TestCheckDocsFindsDriftAndSuggestsRenames(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"code.go": "package x\nfunc ParseFrame() {}\nfunc EncodePacket() {}\nfunc read_stream() {}\ntype Decoder struct{}\n",
		"docs/design.md": "# Design\n" +
			"`ParseFrame` reads input; `Decoder` owns state.\n" +
			"The old `ParsePacket` decoded frames (renamed?). See `EncodePackt()` too.\n" +
			"Removed helper `NukeEverything` and file `gone.go`; keep `code.go`.\n" +
			"Build with `go build ./...` and read `docs/other.md`.\n",
		"docs/clean.md": "# Clean\nOnly mentions `Decoder` and `ParseFrame`.\n",
	})
	app := ApplicationInMemory()
	if _, err := app.Index.IndexRepository("p", dir, true, nil); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"docs/design.md", "docs/clean.md"} {
		if _, err := app.Documents.IndexMarkdown("p", filepath.Join(dir, d)); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := app.Documents.CheckDocs("p", 20)
	if err != nil {
		t.Fatal(err)
	}
	if rep["documents_checked"] != 2 || rep["documents_with_issues"] != 1 {
		t.Fatalf("only design.md should have issues: %v", rep)
	}
	doc := rep["documents"].([]map[string]any)[0]
	if doc["document"] != "docs/design.md" {
		t.Fatalf("wrong document: %v", doc["document"])
	}
	missing := map[string]string{}
	for _, m := range doc["missing_references"].([]map[string]any) {
		sug, _ := m["did_you_mean"].(string)
		missing[m["identifier"].(string)] = sug
	}
	if missing["EncodePackt"] != "EncodePacket" {
		t.Errorf("typo/rename should suggest EncodePacket: %v", missing)
	}
	for _, id := range []string{"ParsePacket", "NukeEverything", "gone.go", "docs/other.md"} {
		if _, ok := missing[id]; !ok {
			t.Errorf("%s should be reported missing: %v", id, missing)
		}
	}
	for _, ok := range []string{"ParseFrame", "Decoder", "code.go", "go"} {
		if _, bad := missing[ok]; bad {
			t.Errorf("%s exists (or is a command) and must not be reported: %v", ok, missing)
		}
	}
	if missing["NukeEverything"] != "" {
		t.Errorf("no close match exists for NukeEverything: %q", missing["NukeEverything"])
	}
}

func TestCheckDocsDetectsCodeNewerThanDocAndMissingSource(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"code.go": "package x\nfunc Widget() {}\n", "README.md": "# R\nUses `Widget`.\n"})
	app := ApplicationInMemory()
	app.Index.IndexRepository("p", dir, true, nil)
	doc := filepath.Join(dir, "README.md")
	app.Documents.IndexMarkdown("p", doc)

	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(doc, old, old) // the doc was last edited two days ago; code.go is fresh
	rep, _ := app.Documents.CheckDocs("p", 20)
	if rep["stale_by_code_change"] != 1 {
		t.Fatalf("code newer than doc must be flagged: %v", rep)
	}
	row := rep["documents"].([]map[string]any)[0]["code_changed_since_doc"].([]map[string]any)[0]
	if row["file"] != "code.go" || row["documented_symbols"].([]string)[0] != "Widget" {
		t.Fatalf("row: %v", row)
	}

	os.Remove(doc)
	rep, _ = app.Documents.CheckDocs("p", 20)
	if rep["source_missing"] != 1 {
		t.Fatalf("deleted doc file must be flagged: %v", rep)
	}
}

func TestVerifyDesignReportsUnverifiedReferences(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"code.go": "package x\nfunc RealThing() {}\n", "d.md": "# D\n`RealThing` and `FakeThing`.\n"})
	app := ApplicationInMemory()
	app.Index.IndexRepository("p", dir, true, nil)
	res, _ := app.Documents.IndexMarkdown("p", filepath.Join(dir, "d.md"))
	rep, err := app.Documents.VerifyDesign("p", res["document_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if rep["verified"] != 1 || rep["unverified"] != 1 {
		t.Fatalf("verify: %v", rep)
	}
	if _, err := app.Documents.VerifyDesign("other", res["document_id"].(string)); err == nil {
		t.Fatal("cross-project verify must be rejected")
	}
}

func TestProseNamesAreNotMissingButBackticksAre(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"code.go": "package x\nfunc ProjectIDs() {}\nfunc RealThing() {}\n",
		"d.md":    "# D\nWe support JavaScript and OpenCode users; `project_id` is a request field; call `find_function` first; `Vanished` is gone.\n",
	})
	app := ApplicationInMemory()
	app.Index.IndexRepository("p", dir, true, nil)
	app.Documents.SetKnownNames([]string{"find_function"})
	app.Documents.IndexMarkdown("p", filepath.Join(dir, "d.md"))
	rep, _ := app.Documents.CheckDocs("p", 20)
	got := map[string]string{}
	for _, d := range rep["documents"].([]map[string]any) {
		for _, m := range d["missing_references"].([]map[string]any) {
			sug, _ := m["did_you_mean"].(string)
			got[m["identifier"].(string)] = sug
		}
	}
	if _, bad := got["JavaScript"]; bad {
		t.Error("unquoted product names must never be reported")
	}
	if _, bad := got["find_function"]; bad {
		t.Error("registered vocabulary (tool names) is known")
	}
	if _, ok := got["Vanished"]; !ok {
		t.Errorf("a backticked missing name must be reported: %v", got)
	}
	if sug := got["project_id"]; sug != "" {
		t.Errorf("project_id is not a rename of ProjectIDs; a hint needs a near miss, got %q", sug)
	}
}

func TestGoImportsResolveExactlyThroughGoMod(t *testing.T) {
	app, root := indexTree(t, map[string]string{
		"go.mod":             "module example.com/proj\n\ngo 1.22\n",
		"main.go":            "package main\nimport (\n\t\"example.com/proj/util\"\n\t\"example.com/other/util\"\n)\nfunc main() {}\n",
		"util/util.go":       "package util\n",
		"vendor/x/util/u.go": "package util\n",
	})
	got := dependsOn(t, app, root, "main.go")
	if strings.Join(got, ",") != "util/util.go" {
		t.Fatalf("only the in-module import should resolve, exactly: %v", got)
	}
}
