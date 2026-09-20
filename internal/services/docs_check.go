package services

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"codergag/internal/models"
)

var (
	backtickRe = regexp.MustCompile("`([^`\n]+)`")
	callRefRe  = regexp.MustCompile(`\b([A-Za-z_]\w{2,})\(`)
	camelRe    = regexp.MustCompile(`\b[A-Z][a-z0-9]+(?:[A-Z][a-z0-9]+)+\b`)
	snakeRe    = regexp.MustCompile(`\b[a-z][a-z0-9]*(?:_[a-z0-9]+)+\b`)
	fileRefRe  = regexp.MustCompile(`^[\w./-]+\.(?:go|py|rs|c|h|cc|cpp|hpp|java|js|jsx|ts|tsx|md|yaml|yml|json|toml)$`)
	leadIdent  = regexp.MustCompile(`^[A-Za-z_][\w.]*`)
)

// notSymbols are words that appear in backticks but are language or tool
// vocabulary, not references into the project.
var notSymbols = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`true false nil null none self this super if else for while return break continue
		func function def class struct enum trait impl import package module const var let type interface async await
		main init new delete print println len cap make append panic error string int bool byte void map list dict set
		json yaml toml sql http https url uri api cli mcp git go npm pip cargo make docker todo fixme readme license
		id ids ok err ctx args argv env stdin stdout stderr`) {
		m[w] = true
	}
	return m
}()

// docCodeRefs returns the identifiers and file paths in a section that look
// like references to code. strong references are backticked names that look like
// code (underscore, capital, or a call) and `Name(` calls: only these may be
// reported as missing. weak references are unquoted CamelCase or snake_case
// words: they can confirm a symbol but never raise a warning, because prose is
// full of product names such as JavaScript. Shell commands are ignored.
func docCodeRefs(text string) (strong, weak, files []string) {
	seen := map[string]bool{}
	seenF := map[string]bool{}
	add := func(dst *[]string, s string) {
		if len(s) >= 3 && !notSymbols[strings.ToLower(s)] && !seen[s] {
			seen[s] = true
			*dst = append(*dst, s)
		}
	}
	for _, m := range backtickRe.FindAllStringSubmatch(text, -1) {
		tok := strings.TrimSpace(m[1])
		if strings.ContainsAny(tok, " \t") {
			continue
		}
		if fileRefRe.MatchString(tok) {
			if !seenF[tok] {
				seenF[tok] = true
				files = append(files, tok)
			}
			continue
		}
		lead := leadIdent.FindString(tok)
		if lead == "" {
			continue
		}
		parts := strings.Split(lead, ".")
		name := parts[len(parts)-1]
		if name == strings.ToUpper(name) { // FACT, MAX_LEN, ENV_VARS: constants, not indexed symbols
			add(&weak, name)
			continue
		}
		codeLike := strings.Contains(name, "_") || strings.ContainsAny(name, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") || strings.Contains(tok, "(")
		if codeLike {
			add(&strong, name)
		} else {
			add(&weak, name)
		}
	}
	for _, m := range callRefRe.FindAllStringSubmatch(text, -1) {
		add(&strong, m[1])
	}
	for _, w := range camelRe.FindAllString(text, -1) {
		add(&weak, w)
	}
	for _, w := range snakeRe.FindAllString(text, -1) {
		add(&weak, w)
	}
	return strong, weak, files
}

// editDistance is a small Levenshtein for "did you mean" suggestions.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

type symbolIndex struct {
	names map[string][]string // symbol name -> defining file paths
	files map[string]bool     // indexed file paths
}

// SetKnownNames registers vocabulary that documents may mention but that is not
// in the code graph, such as MCP tool names and their parameters.
func (s *DocumentService) SetKnownNames(names []string) {
	s.known = map[string]bool{}
	for _, n := range names {
		s.known[n] = true
	}
}

func (s *DocumentService) symbolIndex(projectID string) symbolIndex {
	ix := symbolIndex{names: map[string][]string{}, files: map[string]bool{}}
	for n := range s.known {
		ix.names[n] = nil // known, but defined in no file
	}
	for _, kind := range []string{"Function", "Class", "Struct"} {
		nodes, _ := s.graph.FindNodes(kind, map[string]any{"project_id": projectID})
		for _, n := range nodes {
			ix.names[strProp(n, "name")] = append(ix.names[strProp(n, "name")], strProp(n, "path"))
		}
	}
	files, _ := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID})
	for _, f := range files {
		ix.files[strProp(f, "path")] = true
	}
	return ix
}

func (ix symbolIndex) fileExists(ref, docDir, root string) bool {
	for _, base := range []string{docDir, root} {
		if base == "" {
			continue
		}
		p := filepath.Clean(filepath.Join(base, ref))
		if ix.files[p] {
			return true
		}
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	for p := range ix.files { // bare name such as `indexing.go`
		if p == ref || strings.HasSuffix(p, "/"+ref) {
			return true
		}
	}
	return false
}

func (ix symbolIndex) suggest(name string) string {
	low := strings.ToLower(name)
	best, bestD := "", 1<<30
	for known := range ix.names {
		if strings.ToLower(known) == low {
			return known
		}
		if len(known) >= 6 && len(name) >= 6 {
			if d := editDistance(low, strings.ToLower(known)); d < bestD || (d == bestD && known < best) {
				best, bestD = known, d
			}
		}
	}
	// A rename hint must be a near-miss: under 20% of the name changed.
	if best != "" && bestD*5 < len(name) {
		return best
	}
	return ""
}

const maxNewerCode = 5

// CheckDocs warns about documentation that has drifted from the code: names and
// files it mentions that no longer exist (with a rename suggestion when a close
// match exists), code it describes that changed after the document did, and
// documents whose source file is gone. Only documents with warnings are listed.
func (s *DocumentService) CheckDocs(projectID string, limit int) (map[string]any, error) {
	if limit <= 0 {
		limit = 20
	}
	docs, err := s.graph.FindNodes("Document", map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	sort.Slice(docs, func(i, j int) bool { return strProp(docs[i], "path") < strProp(docs[j], "path") })
	root := ""
	if ps, _ := s.graph.FindNodes("Project", map[string]any{"id": projectID}); len(ps) > 0 {
		root = strProp(ps[0], "path")
	}
	ix := s.symbolIndex(projectID)

	var report []map[string]any
	totalMissing, totalNewer, totalGone := 0, 0, 0
	for _, doc := range docs {
		path := strProp(doc, "path")
		entry := map[string]any{"document": relPath(root, path)}
		warnings := 0

		info, statErr := os.Stat(path)
		if statErr != nil {
			entry["source_missing"] = true
			totalGone++
			warnings++
		}

		sections, _ := s.graphSections(doc)
		var missing []map[string]any
		newer := map[string][]string{}
		seen := map[string]bool{}
		for _, sec := range sections {
			text := strProp(sec, "heading") + "\n" + strProp(sec, "content")
			strong, weak, files := docCodeRefs(text)
			for _, id := range append(append([]string{}, strong...), weak...) {
				if seen[id] {
					continue
				}
				seen[id] = true
				isStrong := false
				for _, st := range strong {
					if st == id {
						isStrong = true
						break
					}
				}
				if defs, ok := ix.names[id]; ok {
					if statErr == nil {
						for _, def := range defs {
							if fi, err := os.Stat(def); err == nil && fi.ModTime().After(info.ModTime().Add(time.Second)) {
								newer[relPath(root, def)] = append(newer[relPath(root, def)], id)
							}
						}
					}
					continue
				}
				if !isStrong {
					continue // plain prose: cannot be called missing
				}
				m := map[string]any{"identifier": id, "section": strProp(sec, "heading")}
				if sug := ix.suggest(id); sug != "" {
					m["did_you_mean"] = sug
				}
				missing = append(missing, m)
			}
			for _, f := range files {
				if seen[f] {
					continue
				}
				seen[f] = true
				if !ix.fileExists(f, filepath.Dir(path), root) {
					missing = append(missing, map[string]any{"identifier": f, "section": strProp(sec, "heading"), "kind": "file"})
				}
			}
		}
		if len(missing) > 0 {
			totalMissing += len(missing)
			warnings += len(missing)
			entry["missing_references"] = capRows(missing, limit)
			if len(missing) > limit {
				entry["missing_references_total"] = len(missing)
			}
		}
		if len(newer) > 0 {
			files := make([]string, 0, len(newer))
			for f := range newer {
				files = append(files, f)
			}
			sort.Strings(files)
			var rows []map[string]any
			for _, f := range capRows(files, maxNewerCode) {
				syms := uniqueStrings(newer[f], "")
				sort.Strings(syms)
				rows = append(rows, map[string]any{"file": f, "documented_symbols": syms})
			}
			entry["code_changed_since_doc"] = rows
			totalNewer += len(files)
			warnings += len(files)
		}
		if warnings > 0 {
			entry["warnings"] = warnings
			report = append(report, entry)
		}
	}
	sort.SliceStable(report, func(i, j int) bool { return mapIntProp(report[i], "warnings") > mapIntProp(report[j], "warnings") })
	return map[string]any{
		"documents_checked":     len(docs),
		"documents_with_issues": len(report),
		"missing_references":    totalMissing,
		"stale_by_code_change":  totalNewer,
		"source_missing":        totalGone,
		"documents":             capRows(report, limit),
	}, nil
}

func (s *DocumentService) graphSections(doc *models.Node) ([]*models.Node, error) {
	nbrs, err := s.graph.Neighbors(doc.ID, "CONTAINS", "out")
	if err != nil {
		return nil, err
	}
	var out []*models.Node
	for _, en := range nbrs {
		if en.Node.Kind == "DocumentSection" {
			out = append(out, en.Node)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := out[i].Properties["line_start"].(int)
		b, _ := out[j].Properties["line_start"].(int)
		return a < b
	})
	return out, nil
}
