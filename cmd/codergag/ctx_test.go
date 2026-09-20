package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"codergag/internal/services"
)

func runCtxT(t *testing.T, app *services.Application, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := ctxRun(app, args, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func TestCtxAddSearchShowExportStatusReset(t *testing.T) {
	t.Setenv("CODERAG_HOME", t.TempDir())
	app := services.ApplicationInMemory()

	code, out, errs := runCtxT(t, app, "", "add", "-project", "p", "-kind", "decision", "-title", "Use SQLite", "-pin", "single", "binary")
	if code != 0 {
		t.Fatalf("add failed: %d %s", code, errs)
	}
	var added map[string]any
	if err := json.Unmarshal([]byte(out), &added); err != nil || added["id"] == "" {
		t.Fatalf("add must print the record as JSON: %q %v", out, err)
	}
	id := added["id"].(string)

	if code, _, errs := runCtxT(t, app, "from stdin body", "add", "-project", "p", "-title", "Stdin note"); code != 0 {
		t.Fatalf("stdin add failed: %s", errs)
	}

	_, out, _ = runCtxT(t, app, "", "search", "-project", "p", "-json", "sqlite")
	if !strings.Contains(out, "Use SQLite") {
		t.Fatalf("search output: %s", out)
	}
	_, out, _ = runCtxT(t, app, "", "show", id)
	if !strings.Contains(out, "single binary") {
		t.Fatalf("show output: %s", out)
	}
	_, out, _ = runCtxT(t, app, "", "export", "-project", "p")
	if !strings.Contains(out, "## decision") || !strings.Contains(out, "### Use SQLite") {
		t.Fatalf("export output: %s", out)
	}
	_, out, _ = runCtxT(t, app, "", "status", "-project", "p", "-json")
	var st map[string]any
	if err := json.Unmarshal([]byte(out), &st); err != nil || st["total"].(float64) != 2 || st["digest_tokens"].(float64) <= 0 {
		t.Fatalf("status output: %s (%v)", out, err)
	}

	if code, _, _ := runCtxT(t, app, "n\n", "reset", "-project", "p"); code == 0 {
		t.Fatal("reset without confirmation must not succeed")
	}
	if code, _, _ := runCtxT(t, app, "", "reset", "-project", "p", "-yes"); code != 0 {
		t.Fatal("reset -yes must succeed")
	}
	_, out, _ = runCtxT(t, app, "", "status", "-project", "p", "-json")
	json.Unmarshal([]byte(out), &st)
	if st["total"].(float64) != 0 {
		t.Fatalf("reset must clear records: %s", out)
	}
}

func TestCtxProfile(t *testing.T) {
	t.Setenv("CODERAG_HOME", t.TempDir())
	app := services.ApplicationInMemory()
	if code, _, errs := runCtxT(t, app, "", "profile", "set", "style", "terse"); code != 0 {
		t.Fatal(errs)
	}
	_, out, _ := runCtxT(t, app, "", "profile")
	if !strings.Contains(out, "style") || !strings.Contains(out, "terse") {
		t.Fatalf("profile output: %s", out)
	}
	runCtxT(t, app, "", "profile", "clear")
	_, out, _ = runCtxT(t, app, "", "profile")
	if strings.Contains(out, "terse") {
		t.Fatalf("profile not cleared: %s", out)
	}
}

func TestCtxErrors(t *testing.T) {
	t.Setenv("CODERAG_HOME", t.TempDir())
	app := services.ApplicationInMemory()
	if code, _, _ := runCtxT(t, app, ""); code != 2 {
		t.Error("missing subcommand must exit 2")
	}
	if code, _, _ := runCtxT(t, app, "", "add", "-project", "p"); code == 0 {
		t.Error("add without title must fail")
	}
	if code, _, _ := runCtxT(t, app, "", "show", "nope"); code == 0 {
		t.Error("show of unknown id must fail")
	}
}
