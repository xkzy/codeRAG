package mcp

import (
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
)

// Response shaping keeps tool output small for LLM callers. Graph rows carry
// bookkeeping (hashes, timestamps, repeated identifiers) that an agent never
// needs to pick a result; shaping drops it, factors out the common path prefix,
// trims long text to a snippet around the query, and can cap a page to a token
// budget. detail=full returns rows untouched.

const (
	detailCompact = "compact"
	detailFull    = "full"

	snippetLen     = 240
	snippetLeading = 80
)

// noisyFields never help an agent choose or act on a result.
var noisyFields = map[string]bool{
	"created_at": true, "updated_at": true, "project_id": true, "source_hash": true, "hash": true,
	"parser_version": true, "last_indexed_at": true, "calls": true, "entity_ids": true,
	"member_ids": true, "type_relations": true, "compacted_into": true, "line_end": true,
	// location and fingerprint detail: derivable from stable_id or via resolve_reference
	"rel_path": true, "start_byte": true, "end_byte": true, "start_col": true, "end_col": true,
	"content_hash": true, "commit": true, "qualified_name": true, "imports": true, "refs": true,
}

// shapedTools return lists of graph rows that are worth shaping.
var shapedTools = map[string]bool{
	"get_function": true, "get_callers": true, "get_callees": true, "get_references": true,
	"get_dependents": true, "get_dependencies": true, "impact_analysis": true,
	"find_related_code": true, "find_similar_functions": true, "get_type_hierarchy": true,
	"prepare_context": true, "explain_context": true, "find_circular_deps": true,
}

func isShaped(tool string) bool { return paginatedTools[tool] || shapedTools[tool] }

// approxTokens estimates LLM tokens from JSON size (about 4 bytes per token).
func approxTokens(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return int(math.Ceil(float64(len(b)) / 4))
}

func shapeRow(row map[string]any, terms []string) map[string]any {
	out := make(map[string]any, len(row))
	path, _ := row["path"].(string)
	name, _ := row["name"].(string)
	owner, _ := row["owner"].(string)
	for k, v := range row {
		if noisyFields[k] {
			continue
		}
		switch x := v.(type) {
		case nil:
			continue
		case string:
			if x == "" {
				continue
			}
			if len(x) > snippetLen {
				v = snippet(x, terms)
			}
		case bool:
			if !x { // false flags are the default; only true ones are signal
				continue
			}
		case []any:
			if len(x) == 0 {
				continue
			}
		case []string:
			if len(x) == 0 {
				continue
			}
		case float64:
			v = math.Round(x*1000) / 1000
		}
		out[k] = v
	}
	// qualified_name repeats path + owner + name for code entities.
	if q, ok := row["qualified_name"].(string); ok && path != "" && name != "" {
		derived := path + ":" + name
		if owner != "" {
			derived = path + ":" + owner + "." + name
		}
		if q == derived {
			delete(out, "qualified_name")
		}
	}
	if ls, ok := row["line_start"]; ok {
		delete(out, "line_start")
		out["line"] = ls
		if le, ok := row["line_end"]; ok && le != ls {
			out["line_end"] = le
		}
	}
	return out
}

// snippet returns a window of text around the first query term (or the start).
func snippet(text string, terms []string) string {
	lower := strings.ToLower(text)
	at := -1
	for _, t := range terms {
		if i := strings.Index(lower, t); i >= 0 && (at < 0 || i < at) {
			at = i
		}
	}
	start := 0
	if at > snippetLeading {
		start = at - snippetLeading
	}
	end := start + snippetLen
	if end > len(text) {
		end = len(text)
	}
	s := strings.TrimSpace(text[start:end])
	if start > 0 {
		s = "…" + s
	}
	if end < len(text) {
		s += "…"
	}
	return s
}

func queryTerms(args map[string]any) []string {
	var terms []string
	for _, key := range []string{"query", "name", "question", "function_name"} {
		for _, t := range strings.Fields(strings.ToLower(getString(args, key))) {
			terms = append(terms, strings.Trim(t, `.,;:"'()`))
		}
	}
	return terms
}

// commonDir returns the longest directory prefix shared by all paths.
func commonDir(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	base := filepath.Dir(paths[0])
	for _, p := range paths[1:] {
		for base != "." && base != "/" && !strings.HasPrefix(p, base+string(filepath.Separator)) {
			base = filepath.Dir(base)
		}
	}
	if base == "." || base == "/" || base == "" {
		return ""
	}
	return base
}

// shapeRows shapes a list of rows and factors the shared path prefix into one
// returned base, so each row carries a short relative path.
func shapeRows(rows []map[string]any, terms []string) ([]map[string]any, string) {
	out := make([]map[string]any, len(rows))
	var paths []string
	for i, r := range rows {
		out[i] = shapeRow(r, terms)
		if p, ok := out[i]["path"].(string); ok && filepath.IsAbs(p) {
			paths = append(paths, p)
		}
	}
	base := ""
	if len(paths) == len(rows) && len(rows) > 0 {
		base = commonDir(paths)
	}
	if base != "" {
		for _, r := range out {
			p := r["path"].(string)
			r["path"] = strings.TrimPrefix(p, base+string(filepath.Separator))
		}
	}
	return out, base
}

// applyBudget keeps the longest prefix of rows whose estimated size fits maxTokens
// (always at least one row so the caller makes progress).
func applyBudget(rows []map[string]any, maxTokens int) ([]map[string]any, bool) {
	if maxTokens <= 0 {
		return rows, false
	}
	used := 0
	for i, r := range rows {
		used += approxTokens(r)
		if used > maxTokens && i > 0 {
			return rows[:i], true
		}
	}
	return rows, false
}

// shapeResult shapes every list of rows in a tool result in place.
func shapeResult(result map[string]any, args map[string]any) {
	terms := queryTerms(args)
	for key, val := range result {
		rows, ok := val.([]map[string]any)
		if !ok {
			continue
		}
		shaped, base := shapeRows(rows, terms)
		result[key] = shaped
		if base != "" {
			result["path_base"] = base
		}
	}
}
