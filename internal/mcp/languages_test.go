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
