package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codergag/internal/graph"
)

func benchIndexTree(b *testing.B, files map[string]string) *Application {
	dir := b.TempDir()
	for rel, src := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	app := ApplicationInMemory()
	if _, err := app.Index.IndexRepository("p", dir, true, nil, false); err != nil {
		b.Fatal(err)
	}
	return app
}

// largeGraphFiles returns a project with enough functions that an O(N) clone
// is measurably slower than an O(candidate-set) indexed lookup.
func largeGraphFiles() map[string]string {
	files := map[string]string{"go.mod": "module example.com/m\n"}
	var b strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&b, "func Func%d() { Func%d() }\n", i, (i+1)%400)
	}
	files["pkg/big.go"] = "package pkg\n" + b.String()
	return files
}

func BenchmarkSmartRead(b *testing.B) {
	app := benchIndexTree(b, map[string]string{
		"go.mod":               "module example.com/m\n",
		"cmd/app/main.go":      "package main\nfunc main() {}\n",
		"internal/svc/svc.go":  "package svc\nfunc Run() { store.Get() }\nfunc Other() { store.Get() }\ntype Service struct{ S store.Store }\n",
		"internal/store/st.go": "package store\nfunc Get() {}\ntype Store struct{}\n",
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.SmartRead("p", "internal/svc/svc.go")
	}
}

func BenchmarkAnalyzeProject(b *testing.B) {
	app := benchIndexTree(b, map[string]string{
		"go.mod":               "module example.com/m\n",
		"cmd/app/main.go":      "package main\nfunc main() {}\n",
		"internal/svc/svc.go":  "package svc\nfunc Run() {}\ntype Service struct{}\n",
		"internal/store/st.go": "package store\nfunc Get() {}\ntype Store struct{}\n",
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.AnalyzeProject("p")
	}
}

func BenchmarkProjectProfile(b *testing.B) {
	app := benchIndexTree(b, map[string]string{
		"go.mod":               "module example.com/m\n",
		"cmd/app/main.go":      "package main\nfunc main() {}\n",
		"internal/svc/svc.go":  "package svc\nfunc Run() {}\ntype Service struct{}\n",
		"internal/store/st.go": "package store\nfunc Get() {}\ntype Store struct{}\n",
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.ProjectProfile("p")
	}
}

func BenchmarkSupersedeEvidence(b *testing.B) {
	app := benchIndexTree(b, map[string]string{
		"main.go": "package main\nfunc main() {}\n",
	})
	fns, _ := app.Graph.FindNodes("Function", map[string]any{"project_id": "p"})
	if len(fns) == 0 {
		b.Fatal("no functions")
	}
	ev, _ := app.Evidence.RecordEvidence("p", fns[0].ID, "original finding", nil)
	evID := ev["id"].(string)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.Evidence.SupersedeEvidence("p", evID, "updated finding", "rationale", "agent")
		_, _ = app.Evidence.RecordEvidence("p", fns[0].ID, "original finding", nil)
	}
}

func BenchmarkCompactChangeIntelligence(b *testing.B) {
	app := benchIndexTree(b, map[string]string{
		"main.go": "package main\nfunc main() {}\n",
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.Git.CompactChangeIntelligence("p", "")
	}
}

// BenchmarkFind_Index measures the hot entity-lookup path (find_function).
func BenchmarkFind_Index(b *testing.B) {
	app := benchIndexTree(b, largeGraphFiles())
	// Warm the project index so the timed path measures the hot lookup, not
	// the one-time index build.
	_, _ = app.Code.Find("p", "Function", "Func200", 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.Code.Find("p", "Function", "Func200", 20)
	}
}

// BenchmarkFind_Authoritative measures the same lookup against the raw
// repository, to prove the indexed path is faster on the hot path.
func BenchmarkFind_Authoritative(b *testing.B) {
	app := benchIndexTree(b, largeGraphFiles())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nodes, err := app.Graph.FindNodes("Function", map[string]any{"project_id": "p"})
		if err != nil {
			b.Fatal(err)
		}
		for _, n := range nodes {
			if strProp(n, "name") == "Func200" {
				break
			}
		}
	}
}

// BenchmarkFunction_Index measures the typed function lookup (find_function by name).
func BenchmarkFunction_Index(b *testing.B) {
	app := benchIndexTree(b, largeGraphFiles())
	_, _ = app.Code.Function("p", "Func200")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.Code.Function("p", "Func200")
	}
}

// BenchmarkRelated_Index measures the callers/callees relationship lookup.
func BenchmarkRelated_Index(b *testing.B) {
	app := benchIndexTree(b, largeGraphFiles())
	fns, _ := app.Graph.FindNodes("Function", map[string]any{"project_id": "p"})
	var runID string
	for _, n := range fns {
		if strProp(n, "name") == "Func200" {
			runID = n.ID
		}
	}
	if runID == "" {
		b.Skip("Func200 not found")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.Code.Related(runID, "CALLS", string(graph.DirOut))
	}
}
