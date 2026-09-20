package services

import "testing"

func TestCompactBlocksNormalizesAndDeduplicatesEvidence(t *testing.T) {
	blocks := []ContextBlock{
		{Section: "memory", Items: []ContextItem{
			{Kind: "memory", Text: "short read\n at offset 0"},
			{Kind: "memory", Text: "short read at offset 0"},
		}},
		{Section: "docs", Items: []ContextItem{
			{Kind: "doc", Text: "short read at offset 0"},
		}},
	}

	got := compactBlocks(blocks)
	if len(got) != 2 || len(got[0].Items) != 1 || len(got[1].Items) != 1 {
		t.Fatalf("unexpected compacted blocks: %+v", got)
	}
	if got[0].Items[0].Text != "short read at offset 0" {
		t.Fatalf("whitespace was not normalized: %q", got[0].Items[0].Text)
	}
}

func TestCompactBlocksDropsEmptyItems(t *testing.T) {
	got := compactBlocks([]ContextBlock{{Section: "logs", Items: []ContextItem{{Kind: "memory", Text: " \n\t"}}}})
	if len(got) != 0 {
		t.Fatalf("expected empty block to be removed: %+v", got)
	}
}

func TestSmallProfileAppliesSafeDefaults(t *testing.T) {
	app := ApplicationInMemory()
	ctx, err := app.Context.Compile(ContextRequest{ProjectID: "small", Question: "log", Profile: "small"})
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Level != CtxLevelCompressed {
		t.Fatalf("level = %d, want compressed", ctx.Level)
	}
}
