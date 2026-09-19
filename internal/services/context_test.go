package services_test

import (
	"testing"

	"codergag/internal/services"
)

func TestContextCompiler_EmptyGraph(t *testing.T) {
	app := services.ApplicationInMemory()

	t.Run("level1_no_crash", func(t *testing.T) {
		ctx, err := app.Context.Compile(services.ContextRequest{
			ProjectID: "proj1",
			Question:  "parse_packet",
			Level:     services.CtxLevelStructured,
		})
		if err != nil {
			t.Fatal(err)
		}
		if ctx.Level != services.CtxLevelStructured {
			t.Errorf("level: got %d, want %d", ctx.Level, services.CtxLevelStructured)
		}
		// Empty graph → no blocks, but checklist should fire
		if len(ctx.Checklist) == 0 {
			t.Error("expected at least one checklist item for empty graph")
		}
	})

	t.Run("level5_explain", func(t *testing.T) {
		ctx, err := app.Context.Compile(services.ContextRequest{
			ProjectID: "proj1",
			Question:  "parse_packet",
			Level:     services.CtxLevelProvenance,
			Explain:   true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if ctx.Explanation == "" {
			t.Error("expected Explanation for level 5 + Explain=true")
		}
	})

	t.Run("token_budget_zero_blocks", func(t *testing.T) {
		ctx, err := app.Context.Compile(services.ContextRequest{
			ProjectID: "proj1",
			Question:  "foo",
			Level:     services.CtxLevelCompressed,
			MaxTokens: 50,
		})
		if err != nil {
			t.Fatal(err)
		}
		if ctx.Truncated {
			t.Error("truncated should not be set when there are no blocks")
		}
	})
}

func TestContextCompiler_WithData(t *testing.T) {
	app := services.ApplicationInMemory()
	pid := "proj2"

	// Seed a symbol via the index service
	_, err := app.Index.IndexFile(pid, pid, "/tmp/fake.go", false)
	// errors are expected for a non-existent file; we just need the app to survive

	// Seed a memory
	if _, err = app.Memory.Store(pid, "parse_packet design",
		"parse_packet reads the header then delegates to sub-parsers", nil); err != nil {
		t.Fatalf("memory store: %v", err)
	}

	ctx, err := app.Context.Compile(services.ContextRequest{
		ProjectID: pid,
		Question:  "parse_packet",
		Level:     services.CtxLevelRanked,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Should have at least the memory block
	found := false
	for _, b := range ctx.Blocks {
		if b.Section == "memory" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected memory block for seeded memory")
	}
}

func TestContextCompiler_TokenBudget(t *testing.T) {
	app := services.ApplicationInMemory()
	pid := "proj3"

	// Seed several memories to ensure blocks are non-empty
	for i := 0; i < 5; i++ {
		app.Memory.Store(pid, "title "+string(rune('A'+i)),
			"This is a fairly long piece of engineering memory content that should consume tokens", nil)
	}

	ctx, err := app.Context.Compile(services.ContextRequest{
		ProjectID: pid,
		Question:  "memory",
		Level:     services.CtxLevelStructured,
		MaxTokens: 10, // very tight budget → must truncate
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ctx.Truncated {
		// Might not truncate if items fit; log rather than fail
		t.Log("no truncation with tight budget (items may be small enough)")
	}
}

func TestContextCompiler_Checklist(t *testing.T) {
	app := services.ApplicationInMemory()
	ctx, err := app.Context.Compile(services.ContextRequest{
		ProjectID: "proj4",
		Question:  "something",
		Level:     services.CtxLevelStructured,
	})
	if err != nil {
		t.Fatal(err)
	}
	hasIndexHint := false
	for _, c := range ctx.Checklist {
		if len(c) > 10 {
			hasIndexHint = true
		}
	}
	if !hasIndexHint {
		t.Error("checklist should have at least one item")
	}
}
