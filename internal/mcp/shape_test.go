package mcp

import (
	"codergag/internal/services"
	"encoding/json"
	"strings"
	"testing"
)

func bytesOf(v any) int {
	b, _ := json.Marshal(v)
	return len(b)
}

func TestCompactResponsesAreMuchSmallerAndKeepWhatMatters(t *testing.T) {
	reg := indexedFunctions(t, 20)
	args := func(detail string) map[string]any {
		return map[string]any{"project_id": "p", "query": "handler", "limit": 20, "detail": detail}
	}
	full, err := reg.Call("find_function", args(detailFull))
	if err != nil {
		t.Fatal(err)
	}
	compact, err := reg.Call("find_function", args(detailCompact))
	if err != nil {
		t.Fatal(err)
	}
	fb, cb := bytesOf(full), bytesOf(compact)
	if cb*100 > fb*45 {
		t.Fatalf("compact should be under 45%% of full: %d vs %d bytes", cb, fb)
	}
	t.Logf("full=%dB compact=%dB (%.0f%% smaller)", fb, cb, 100*(1-float64(cb)/float64(fb)))

	rows := compact["results"].([]map[string]any)
	first := rows[0]
	for _, keep := range []string{"id", "name", "kind", "path", "line"} {
		if _, ok := first[keep]; !ok {
			t.Fatalf("compact row lost %q: %v", keep, first)
		}
	}
	for _, drop := range []string{"created_at", "source_hash", "project_id", "calls", "qualified_name", "generated", "owner"} {
		if _, ok := first[drop]; ok {
			t.Fatalf("compact row should not carry %q: %v", drop, first)
		}
	}
	base, _ := compact["path_base"].(string)
	if base == "" || strings.HasPrefix(first["path"].(string), "/") {
		t.Fatalf("paths should be relative to a shared path_base: base=%q path=%v", base, first["path"])
	}
	if _, ok := full["path_base"]; ok {
		t.Fatal("detail=full must return raw rows")
	}
	if fullRow := full["results"].([]map[string]any)[0]; fullRow["source_hash"] == nil {
		t.Fatal("detail=full must keep bookkeeping fields")
	}
}

func TestLongTextIsTrimmedToSnippetAroundQuery(t *testing.T) {
	reg := NewToolRegistry(newApp())
	body := strings.Repeat("filler words go here. ", 60) + "The retry backoff uses jitter. " + strings.Repeat("more filler. ", 60)
	if _, err := reg.Call("memory_store", map[string]any{"project_id": "p", "title": "notes", "content": body, "auto_compact": false}); err != nil {
		t.Fatal(err)
	}
	res, err := reg.Call("memory_search", map[string]any{"project_id": "p", "query": "jitter"})
	if err != nil {
		t.Fatal(err)
	}
	content := res["results"].([]map[string]any)[0]["content"].(string)
	if len(content) > snippetLen+8 || !strings.Contains(content, "jitter") {
		t.Fatalf("snippet should be short and contain the match: %d bytes %q", len(content), content)
	}
	full, _ := reg.Call("memory_get", map[string]any{"project_id": "p", "title": "notes"})
	if len(full["content"].(string)) != len(body) {
		t.Fatal("memory_get must still return the full text")
	}
}

func TestTokenBudgetCutsPageAndNextOffsetContinuesWithoutGaps(t *testing.T) {
	reg := indexedFunctions(t, 40)
	seen := map[string]bool{}
	offset, pages := 0, 0
	for {
		res, err := reg.Call("find_function", map[string]any{
			"project_id": "p", "query": "handler", "limit": 40, "offset": offset, "max_tokens": 120})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		rows := res["results"].([]map[string]any)
		if len(rows) == 0 {
			t.Fatal("a page must always make progress")
		}
		if tok := res["approx_tokens"].(int); tok > 120+approxTokens(rows[0]) {
			t.Fatalf("page exceeds budget: %d tokens", tok)
		}
		for _, r := range rows {
			if seen[r["name"].(string)] {
				t.Fatalf("%v repeated across budgeted pages", r["name"])
			}
			seen[r["name"].(string)] = true
		}
		if res["has_more"] != true {
			break
		}
		if res["truncated"] == nil && len(rows) < 40 && res["next_offset"].(int) != offset+len(rows) {
			t.Fatalf("next_offset must follow the rows actually returned: %v", res)
		}
		offset = res["next_offset"].(int)
		if pages > 100 {
			t.Fatal("did not terminate")
		}
	}
	if len(seen) != 40 || pages < 2 {
		t.Fatalf("expected all 40 rows over several pages, got %d rows in %d pages", len(seen), pages)
	}
}

func TestGetFunctionListsAreShapedButFunctionIsNot(t *testing.T) {
	reg := indexedFunctions(t, 3)
	res, err := reg.Call("get_function", map[string]any{"project_id": "p", "name": "Handler000"})
	if err != nil {
		t.Fatal(err)
	}
	if res["function"].(map[string]any)["source_hash"] == nil {
		t.Fatal("the requested function itself stays complete")
	}
	if res["approx_tokens"] == nil {
		t.Fatal("shaped responses report approx_tokens")
	}
}

func newApp() *services.Application { return services.ApplicationInMemory() }
