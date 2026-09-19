package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"codergag/internal/services"
)

func TestMultiLanguageIndexing(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"boot.S": ".text\n.globl _start\n_start:\n\tcall main\n\tret\n",
		"main.c": "int main() { return helper(); }\nint helper() { return 0; }\n",
		"App.kt": "fun start() { stop() }\nfun stop() {}\n",
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := NewToolRegistry(services.ApplicationInMemory())
	res, err := reg.Call("index_repository", map[string]any{"project_id": "ml", "path": dir})
	if err != nil {
		t.Fatal(err)
	}
	if res["files_seen"].(int) != 3 {
		t.Fatalf("files_seen = %v, want 3", res["files_seen"])
	}
	for _, name := range []string{"_start", "main", "start", "stop"} {
		r, err := reg.Call("find_function", map[string]any{"project_id": "ml", "query": name})
		if err != nil || r["count"].(int) < 1 {
			t.Errorf("function %s not indexed: %v %v", name, r, err)
		}
	}
	// asm _start calls C main across languages
	found, err := reg.Call("find_function", map[string]any{"project_id": "ml", "query": "main"})
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := found["results"].([]map[string]any)
	if len(rows) == 0 {
		t.Fatalf("main not found: %v", found)
	}
	callers, err := reg.Call("get_callers", map[string]any{"project_id": "ml", "function_id": rows[0]["id"]})
	if err != nil {
		t.Fatal(err)
	}
	if callers["count"].(int) < 1 {
		t.Errorf("asm -> C call edge missing: %v", callers)
	}

	langs, err := reg.Call("list_languages", map[string]any{})
	if err != nil || langs["count"].(int) < 40 {
		t.Errorf("list_languages: %v %v", langs, err)
	}
}

func TestGoCallsPlan9Assembly(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"sum.go":      "package x\n\nfunc Sum(a []byte) int { return blockAVX2(a) }\n\nfunc blockAVX2(a []byte) int\n",
		"sum_amd64.s": "#include \"textflag.h\"\nTEXT ·blockAVX2(SB), NOSPLIT, $0-32\n\tCALL ·helper(SB)\n\tRET\nTEXT ·helper(SB), NOSPLIT, $0\n\tRET\n",
	}
	for n, s := range files {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := NewToolRegistry(services.ApplicationInMemory())
	if _, err := reg.Call("index_repository", map[string]any{"project_id": "p9", "path": dir}); err != nil {
		t.Fatal(err)
	}
	find := func(name string) []map[string]any {
		r, err := reg.Call("find_function", map[string]any{"project_id": "p9", "query": name})
		if err != nil {
			t.Fatal(err)
		}
		rows, _ := r["results"].([]map[string]any)
		return rows
	}
	// helper is only called from assembly; blockAVX2 (asm body) is called from Go.
	for _, name := range []string{"helper", "blockAVX2"} {
		rows := find(name)
		if len(rows) == 0 {
			t.Fatalf("%s not indexed", name)
		}
		linked := false
		for _, row := range rows {
			c, err := reg.Call("get_callers", map[string]any{"project_id": "p9", "function_id": row["id"]})
			if err == nil && c["count"].(int) > 0 {
				linked = true
			}
		}
		if !linked {
			t.Errorf("%s has no callers", name)
		}
	}
}
