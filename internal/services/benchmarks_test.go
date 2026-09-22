package services

import (
	"os"
	"path/filepath"
	"testing"
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
