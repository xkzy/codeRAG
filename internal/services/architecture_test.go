package services

import (
	"strings"
	"testing"
)

func archSample(t *testing.T) *Application {
	t.Helper()
	app, _ := indexTree(t, map[string]string{
		"go.mod":               "module example.com/m\n",
		"cmd/app/main.go":      "package main\nimport \"example.com/m/internal/svc\"\nfunc main() { svc.Run() }\n",
		"internal/svc/svc.go":  "package svc\nimport \"example.com/m/internal/store\"\nfunc Run() { store.Get() }\nfunc Other() { store.Get() }\ntype Service struct{ S store.Store }\n",
		"internal/store/st.go": "package store\nfunc Get() {}\ntype Store struct{}\n",
		// x and y import each other: a dependency cycle
		"internal/x/x.go": "package x\nimport \"example.com/m/internal/y\"\nfunc X() { y.Y() }\n",
		"internal/y/y.go": "package y\nimport \"example.com/m/internal/x\"\nfunc Y() { x.X() }\n",
		"gen/api.pb.go":   "package gen\nfunc G() {}\n",
	})
	return app
}

func TestArchitectureReportShowsModulesLayersCyclesAndHotspots(t *testing.T) {
	app := archSample(t)
	rep, err := app.ArchitectureReport("p", 3, 10)
	if err != nil {
		t.Fatal(err)
	}
	md := rep["markdown"].(string)
	for _, want := range []string{
		"# Architecture: p",
		"`internal/svc`", "`internal/store`", "`cmd/app`",
		"depends on `internal/store`",
		"## Layers (foundation first)",
		"internal/x ⇄ internal/y",
		"## Dependency cycles",
		"## Entry points", "`main`",
		"`Get` — called by 2",
		"## Core types", "`Store` — referenced by 1",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report should contain %q\n%s", want, md)
		}
	}
	if rep["cycles"] != 1 {
		t.Errorf("cycles = %v, want 1", rep["cycles"])
	}
	// Layering: store (foundation) is listed before svc, and svc before cmd/app.
	layers := md[strings.Index(md, "## Layers"):]
	iStore, iSvc, iCmd := strings.Index(layers, "internal/store"), strings.Index(layers, "internal/svc"), strings.Index(layers, "cmd/app")
	if !(iStore < iSvc && iSvc < iCmd) {
		t.Errorf("layers out of order (store %d, svc %d, cmd %d):\n%s", iStore, iSvc, iCmd, layers)
	}
	if !strings.Contains(md, "Warnings") || !strings.Contains(md, "cycle") {
		t.Errorf("cycle should be a warning:\n%s", md)
	}
}

func TestArchitectureReportIsBoundedAndValidatesProject(t *testing.T) {
	app := archSample(t)
	small, _ := app.ArchitectureReport("p", 3, 2)
	if n := strings.Count(small["markdown"].(string), "\n- `"); n > 2*6 {
		t.Errorf("limit=2 should cap each section: %d bullet lines", n)
	}
	if !strings.Contains(small["markdown"].(string), "more modules") {
		t.Errorf("truncated module list should say so:\n%s", small["markdown"])
	}
	if small["approx_tokens"].(int) > 600 {
		t.Errorf("report should stay compact: %v tokens", small["approx_tokens"])
	}
	if _, err := app.ArchitectureReport("nope", 2, 5); err == nil {
		t.Error("unindexed project must error")
	}
}

func TestSCCsFindsOnlyRealCycles(t *testing.T) {
	adj := map[string]map[string]int{"a": {"b": 1}, "b": {"c": 1}, "c": {"a": 1, "d": 1}, "d": {}, "e": {"e": 1}}
	var multi []string
	for _, c := range sccs([]string{"a", "b", "c", "d", "e"}, adj) {
		if len(c) > 1 {
			multi = append(multi, strings.Join(c, ","))
		}
	}
	if len(multi) != 1 || multi[0] != "a,b,c" {
		t.Fatalf("cycles = %v, want [a,b,c]", multi)
	}
}
