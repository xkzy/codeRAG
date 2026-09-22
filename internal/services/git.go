package services

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"codergag/internal/graph"
	"codergag/internal/models"
)

type GitService struct {
	graph    graph.GraphRepository
	code     *CodeGraphService
	analysis *AnalysisService
}

func NewGitService(g graph.GraphRepository) *GitService {
	return &GitService{graph: g, code: NewCodeGraphService(g), analysis: NewAnalysisService(g)}
}

// git runs a fixed, read-only git subcommand in root.
func git(root string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

type lineRange struct{ start, end int }

var hunkRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// parseHunks maps each changed file to the new-side line ranges of its hunks.
// A pure deletion (0 lines) is recorded as the line where it happened.
func parseHunks(diff string) map[string][]lineRange {
	out := map[string][]lineRange{}
	var file string
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ b/"):
			file = strings.TrimPrefix(line, "+++ b/")
		case strings.HasPrefix(line, "+++ /dev/null"):
			file = ""
		case strings.HasPrefix(line, "@@") && file != "":
			m := hunkRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			start, _ := strconv.Atoi(m[1])
			count := 1
			if m[2] != "" {
				count, _ = strconv.Atoi(m[2])
			}
			end := start + count - 1
			if count == 0 {
				end = start
			}
			out[file] = append(out[file], lineRange{start, end})
		}
	}
	return out
}

func overlaps(ranges []lineRange, start, end int) bool {
	for _, r := range ranges {
		if r.start <= end && start <= r.end {
			return true
		}
	}
	return false
}

// resolveBase returns the ref a PR would be compared against. An empty base
// tries the usual default branches.
func resolveBase(root, base string) (string, error) {
	candidates := []string{base}
	if base == "" {
		candidates = []string{"origin/main", "origin/master", "main", "master", "HEAD~1"}
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := git(root, "rev-parse", "--verify", "--quiet", c+"^{commit}"); err == nil {
			return c, nil
		}
	}
	return "", &ServiceError{Message: fmt.Sprintf("git base ref %q not found", base)}
}

const (
	highFanIn       = 10
	highImpact      = 25
	maxHintItems    = 5
	maxDocsToReview = 8
)

// PrContext describes what a branch changes and what a reviewer should check.
// It diffs against the merge-base with base (so only this branch's work, plus
// uncommitted edits, counts), maps changed line ranges to the functions and
// types they touch, and reports blast radius, missing tests, docs that mention
// changed symbols, and whether the index is stale for the changed files.
func (s *GitService) PrContext(projectID, base string, limit int) (map[string]any, error) {
	projects, err := s.graph.FindNodes("Project", map[string]any{"id": projectID})
	if err != nil || len(projects) == 0 {
		return nil, &ServiceError{Message: "project not found"}
	}
	root, err := filepath.Abs(strProp(projects[0], "path"))
	if err != nil || !dirExists(root) {
		return nil, &ServiceError{Message: "project path is unavailable"}
	}
	if limit <= 0 {
		limit = 20
	}
	baseRef, err := resolveBase(root, base)
	if err != nil {
		return nil, err
	}
	mb, err := git(root, "merge-base", baseRef, "HEAD")
	if err != nil || mb == "" {
		mb = baseRef // unrelated histories: fall back to a direct diff
	}

	nameStatus, err := git(root, "diff", "--relative", "--name-status", mb, "--")
	if err != nil {
		return nil, &ServiceError{Message: "git diff failed"}
	}
	hunkText, _ := git(root, "diff", "--relative", "-U0", "--no-color", mb, "--")
	hunks := parseHunks(hunkText)

	type change struct {
		Status    string `json:"status"`
		Path      string `json:"path"`
		Generated bool   `json:"generated,omitempty"`
	}
	var changes []change
	wholeFile := map[string]bool{} // added or untracked: every symbol in it is new
	deleted := 0
	for _, line := range strings.Split(nameStatus, "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 2 {
			continue
		}
		path := f[len(f)-1]
		switch f[0][0] {
		case 'A':
			wholeFile[path] = true
		case 'D':
			deleted++
		}
		changes = append(changes, change{Status: f[0], Path: path})
	}
	if untracked, err := git(root, "ls-files", "--others", "--exclude-standard"); err == nil {
		for _, p := range strings.Split(untracked, "\n") {
			if p != "" {
				wholeFile[p] = true
				changes = append(changes, change{Status: "?", Path: p})
			}
		}
	}

	touched := func(n *models.Node) bool {
		rel, err := filepath.Rel(root, strProp(n, "path"))
		if err != nil {
			return false
		}
		if wholeFile[rel] {
			return true
		}
		ranges, ok := hunks[rel]
		if !ok {
			return false
		}
		start, _ := n.Properties["line_start"].(int)
		end, ok := n.Properties["line_end"].(int)
		if !ok || end < start {
			end = start
		}
		return overlaps(ranges, start, end)
	}

	result := map[string]any{"project_id": projectID, "base": baseRef, "merge_base": shortSHA(mb)}
	if b, err := git(root, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		result["branch"] = b
	}
	if n, err := git(root, "rev-list", "--count", mb+"..HEAD"); err == nil {
		result["commits_ahead"], _ = strconv.Atoi(n)
	}
	if n, err := git(root, "rev-list", "--count", "HEAD.."+baseRef); err == nil {
		result["base_moved_on"], _ = strconv.Atoi(n) // commits on base since the branch point
	}

	fns, _ := s.graph.FindNodes("Function", map[string]any{"project_id": projectID})
	var changedFns []map[string]any
	var hints []string
	var untested, fanIn, wide []string
	changedNames := map[string]bool{}
	generatedTouched := map[string]bool{}
	for _, fn := range fns {
		if !touched(fn) {
			continue
		}
		name := strProp(fn, "name")
		changedNames[name] = true
		callers, _ := s.graph.Neighbors(fn.ID, "CALLS", graph.DirIn)
		imp, _ := s.code.Impact(fn.ID, 2, 500)
		impacted, _ := imp["affected_count"].(int)
		tests, _ := s.analysis.RelatedTests(projectID, name, 3)
		gen, _ := fn.Properties["generated"].(bool)
		if gen {
			generatedTouched[strProp(fn, "path")] = true
		}
		label := qualifiedLabel(fn)
		if len(tests) == 0 && !strings.HasPrefix(strings.ToLower(name), "test") && !gen {
			untested = append(untested, label)
		}
		if len(callers) >= highFanIn {
			fanIn = append(fanIn, fmt.Sprintf("%s (%d callers)", label, len(callers)))
		}
		if impacted >= highImpact {
			wide = append(wide, fmt.Sprintf("%s (%d affected)", label, impacted))
		}
		changedFns = append(changedFns, map[string]any{
			"name": label, "path": relPath(root, strProp(fn, "path")), "line": fn.Properties["line_start"],
			"callers": len(callers), "impacted": impacted, "tests": len(tests),
		})
	}
	sort.Slice(changedFns, func(i, j int) bool {
		a, b := changedFns[i]["impacted"].(int), changedFns[j]["impacted"].(int)
		if a != b {
			return a > b
		}
		return changedFns[i]["name"].(string) < changedFns[j]["name"].(string)
	})

	var changedTypes []map[string]any
	var apiHints []string
	for _, kind := range []string{"Class", "Struct"} {
		nodes, _ := s.graph.FindNodes(kind, map[string]any{"project_id": projectID})
		for _, n := range nodes {
			if !touched(n) {
				continue
			}
			changedNames[strProp(n, "name")] = true
			users, _ := s.graph.Neighbors(n.ID, "USES", graph.DirIn)
			h, _ := s.code.Hierarchy(n.ID, "down", 3)
			subs, _ := h["subtypes"].([]map[string]any)
			changedTypes = append(changedTypes, map[string]any{
				"name": strProp(n, "name"), "kind": kind, "path": relPath(root, strProp(n, "path")),
				"line": n.Properties["line_start"], "used_by": len(users), "subtypes": len(subs),
			})
			if len(users)+len(subs) >= 3 {
				apiHints = append(apiHints, fmt.Sprintf("%s changed and is used by %d, extended by %d", strProp(n, "name"), len(users), len(subs)))
			}
		}
	}

	// Stale index: the graph may not reflect the files being reviewed.
	var stale []string
	files, _ := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID})
	for _, f := range files {
		rel := relPath(root, strProp(f, "path"))
		if ranges, ok := hunks[rel]; !ok && !wholeFile[rel] {
			_ = ranges
			continue
		}
		data, err := os.ReadFile(strProp(f, "path"))
		if err == nil && fmt.Sprintf("%x", sha256.Sum256(data)) != strProp(f, "hash") {
			stale = append(stale, rel)
		}
		if g, _ := f.Properties["generated"].(bool); g {
			generatedTouched[strProp(f, "path")] = true
		}
	}
	sort.Strings(stale)
	indexedPaths := map[string]bool{}
	for _, f := range files {
		indexedPaths[relPath(root, strProp(f, "path"))] = true
	}
	notIndexed := 0
	for i := range changes {
		if indexedPaths[changes[i].Path] {
			if f, _ := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID, "path": filepath.Join(root, changes[i].Path)}); len(f) > 0 {
				changes[i].Generated, _ = f[0].Properties["generated"].(bool)
			}
		} else if supportedExts[strings.ToLower(filepath.Ext(changes[i].Path))] && changes[i].Status[0] != 'D' {
			notIndexed++
		}
	}

	sort.Strings(untested)
	if len(stale) > 0 {
		hints = append(hints, fmt.Sprintf("Index is stale for %d changed file(s) (%s): symbol ranges and edges below may be wrong. Run index_repository first.", len(stale), joinMax(stale, maxHintItems)))
	}
	if notIndexed > 0 {
		hints = append(hints, fmt.Sprintf("%d changed source file(s) are not in the index yet; their symbols are not analysed.", notIndexed))
	}
	if len(generatedTouched) > 0 {
		paths := make([]string, 0, len(generatedTouched))
		for p := range generatedTouched {
			paths = append(paths, relPath(root, p))
		}
		sort.Strings(paths)
		hints = append(hints, "Generated code changed ("+joinMax(paths, maxHintItems)+"): change the generator or its input instead of editing the output.")
	}
	if len(wide) > 0 {
		hints = append(hints, "Wide blast radius: "+joinMax(wide, maxHintItems))
	}
	if len(fanIn) > 0 {
		hints = append(hints, "Heavily called functions changed: "+joinMax(fanIn, maxHintItems))
	}
	hints = append(hints, apiHints...)
	if len(untested) > 0 {
		hints = append(hints, fmt.Sprintf("%d changed function(s) have no related test: %s", len(untested), joinMax(untested, maxHintItems)))
	}
	if deleted > 0 {
		hints = append(hints, fmt.Sprintf("%d file(s) deleted: check for dangling references (find_dead_imports, get_references).", deleted))
	}
	if b, ok := result["base_moved_on"].(int); ok && b > 0 {
		hints = append(hints, fmt.Sprintf("Base has %d newer commit(s): rebase or merge before relying on this review.", b))
	}

	result["risk"] = riskLevel(len(changedFns), len(untested), len(fanIn), len(wide), len(stale))
	result["total_changed_files"] = len(changes)
	result["changed_files"] = capRows(changes, limit)
	result["changed_functions"] = capRows(changedFns, limit)
	result["changed_types"] = capRows(changedTypes, limit)
	result["reviewer_hints"] = hints
	if docs := s.docsMentioning(projectID, root, changedNames); len(docs) > 0 {
		result["docs_to_review"] = docs
	}
	if reviewers := s.suggestedReviewers(root, hunks, wholeFile, limit); len(reviewers) > 0 {
		result["suggested_reviewers"] = reviewers
	}
	return result, nil
}

func capRows[T any](rows []T, limit int) []T {
	if len(rows) > limit {
		return rows[:limit]
	}
	return rows
}

func riskLevel(changedFns, untested, fanIn, wide, stale int) string {
	score := 0
	switch {
	case changedFns > 25:
		score += 2
	case changedFns > 0:
		score++
	}
	score += 2 * min(fanIn, 2)
	score += 2 * min(wide, 2)
	score += min(untested, 3)
	if stale > 0 {
		score++
	}
	switch {
	case score >= 6:
		return "high"
	case score >= 2:
		return "medium"
	}
	return "low"
}

func qualifiedLabel(n *models.Node) string {
	if owner := strProp(n, "owner"); owner != "" {
		return owner + "." + strProp(n, "name")
	}
	return strProp(n, "name")
}

func relPath(root, p string) string {
	if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}

func shortSHA(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

func joinMax(items []string, n int) string {
	if len(items) <= n {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:n], ", ") + fmt.Sprintf(" and %d more", len(items)-n)
}

// docsMentioning lists document sections that name a changed symbol, so the
// reviewer knows which prose may now be out of date.
func (s *GitService) docsMentioning(projectID, root string, names map[string]bool) []map[string]any {
	if len(names) == 0 {
		return nil
	}
	sections, _ := s.graph.FindNodes("DocumentSection", map[string]any{"project_id": projectID})
	sort.Slice(sections, func(i, j int) bool { return sections[i].ID < sections[j].ID })
	var out []map[string]any
	for _, sec := range sections {
		var hit []string
		seen := map[string]bool{}
		for _, id := range identRe.FindAllString(strProp(sec, "heading")+"\n"+strProp(sec, "content"), -1) {
			if names[id] && !seen[id] && len(id) >= 3 {
				seen[id] = true
				hit = append(hit, id)
			}
		}
		if len(hit) == 0 {
			continue
		}
		sort.Strings(hit)
		doc := ""
		if d, err := s.graph.GetNode(strProp(sec, "document_id")); err == nil {
			doc = relPath(root, strProp(d, "path"))
		}
		out = append(out, map[string]any{"document": doc, "section": strProp(sec, "heading"), "mentions": hit})
		if len(out) >= maxDocsToReview {
			break
		}
	}
	return out
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// AuthorStats holds blame line counts for one author on changed code.
type AuthorStats struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Lines int    `json:"lines_owned"`
	Files int    `json:"files"`
}

// BlameAuthors runs git blame on the given repo-relative paths and returns per-author
// line-ownership counts. It is O(files) git invocations; use on small sets only.
func (s *GitService) BlameAuthors(root string, files []string) []AuthorStats {
	type key struct{ name, email string }
	lines := map[key]int{}
	fileSet := map[key]map[string]bool{}

	for _, rel := range files {
		out, err := git(root, "blame", "--line-porcelain", "--", rel)
		if err != nil || out == "" {
			continue
		}
		var curName, curEmail string
		for _, line := range strings.Split(out, "\n") {
			switch {
			case strings.HasPrefix(line, "author "):
				curName = strings.TrimPrefix(line, "author ")
			case strings.HasPrefix(line, "author-mail "):
				curEmail = strings.Trim(strings.TrimPrefix(line, "author-mail "), "<>")
			case strings.HasPrefix(line, "\t"): // actual code line
				if curName == "" || curName == "Not Committed Yet" {
					continue
				}
				k := key{curName, curEmail}
				lines[k]++
				if fileSet[k] == nil {
					fileSet[k] = map[string]bool{}
				}
				fileSet[k][rel] = true
			}
		}
	}

	out := make([]AuthorStats, 0, len(lines))
	for k, n := range lines {
		out = append(out, AuthorStats{Name: k.name, Email: k.email, Lines: n, Files: len(fileSet[k])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Lines > out[j].Lines })
	return out
}

// ReviewSuggestions blames the given files (or all changed files in the
// project if none are specified) and returns per-author ownership,
// sorted by lines owned. Returns nil on non-git directories.
func (s *GitService) ReviewSuggestions(projectID string, files []string, limit int) []map[string]any {
	projects, err := s.graph.FindNodes("Project", map[string]any{"id": projectID})
	if err != nil || len(projects) == 0 {
		return nil
	}
	root, err := filepath.Abs(strProp(projects[0], "path"))
	if err != nil || !dirExists(root) {
		return nil
	}
	if limit <= 0 {
		limit = 5
	}
	authors := s.BlameAuthors(root, files)
	if len(authors) > limit {
		authors = authors[:limit]
	}
	out := make([]map[string]any, len(authors))
	for i, a := range authors {
		out[i] = map[string]any{
			"name":        a.Name,
			"email":       a.Email,
			"lines_owned": a.Lines,
			"files":       a.Files,
		}
	}
	return out
}

// Churn returns per-file change counts (commits, added, deleted lines)
// sorted by total activity descending. Uses git log --numstat for accuracy.
func (s *GitService) Churn(projectID, root string, limit int) (map[string]any, error) {
	projects, err := s.graph.FindNodes("Project", map[string]any{"id": projectID})
	if err == nil && len(projects) > 0 && root == "" {
		if p, e := filepath.Abs(strProp(projects[0], "path")); e == nil {
			root = p
		}
	}
	if root == "" {
		root = "."
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return nil, &ServiceError{Message: "not a git repository"}
	}
	if limit <= 0 {
		limit = 50
	}

	out, err := git(root, "log", "--numstat", "--pretty=format:", "--", ".")
	if err != nil {
		return map[string]any{
			"project_id": projectID,
			"root":       root,
			"file_count": 0,
			"files":      []map[string]any{},
		}, nil
	}

	type fileStats struct {
		path    string
		commits int
		added   int
		deleted int
	}
	stats := map[string]*fileStats{}
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		// numstat lines look like: "10 5 path/to/file.go" or "10 5 - (binary)"
		added, _ := strconv.Atoi(fields[0])
		deleted, _ := strconv.Atoi(fields[1])
		path := strings.Join(fields[2:], " ")
		if path == "-" {
			continue
		}
		// Normalize path
		if idx := strings.Index(path, "/"); idx >= 0 {
			path = strings.TrimPrefix(path, "")
		}
		fs, ok := stats[path]
		if !ok {
			fs = &fileStats{path: path}
			stats[path] = fs
		}
		fs.commits++
		fs.added += added
		fs.deleted += deleted
	}

	var files []map[string]any
	for _, fs := range stats {
		files = append(files, map[string]any{
			"path":       fs.path,
			"commits":    fs.commits,
			"added":      fs.added,
			"deleted":    fs.deleted,
			"total":      fs.added + fs.deleted,
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i]["total"].(int) > files[j]["total"].(int)
	})
	if len(files) > limit {
		files = files[:limit]
	}

	return map[string]any{
		"project_id":  projectID,
		"root":        root,
		"file_count":  len(files),
		"files":       files,
	}, nil
}

// suggestedReviewers blames the changed lines and returns the top authors
// sorted by ownership of the code being changed.
func (s *GitService) suggestedReviewers(root string, hunks map[string][]lineRange, wholeFiles map[string]bool, limit int) []map[string]any {
	// Collect the set of files to blame.
	var toBlame []string
	for f := range wholeFiles {
		toBlame = append(toBlame, f)
	}
	for f := range hunks {
		if !wholeFiles[f] {
			toBlame = append(toBlame, f)
		}
	}
	if len(toBlame) == 0 {
		return nil
	}
	// Cap the blame set — blaming hundreds of files is slow.
	sort.Strings(toBlame)
	if len(toBlame) > 20 {
		toBlame = toBlame[:20]
	}

	authors := s.BlameAuthors(root, toBlame)
	if limit <= 0 {
		limit = 5
	}
	if len(authors) > limit {
		authors = authors[:limit]
	}
	out := make([]map[string]any, len(authors))
	for i, a := range authors {
		out[i] = map[string]any{
			"name":        a.Name,
			"email":       a.Email,
			"lines_owned": a.Lines,
			"files":       a.Files,
		}
	}
	return out
}

// CompactChangeIntelligence returns a structured summary of working-tree
// changes (git status + git diff), mapping changed files to affected
// symbols. It replaces manual git diff + git status + symbol searching.
func (s *GitService) CompactChangeIntelligence(projectID, base string) (map[string]any, error) {
	projects, err := s.graph.FindNodes("Project", map[string]any{"id": projectID})
	if err != nil || len(projects) == 0 {
		return nil, &ServiceError{Message: "project not found"}
	}
	root, err := filepath.Abs(strProp(projects[0], "path"))
	if err != nil || !dirExists(root) {
		return nil, &ServiceError{Message: "project path is unavailable"}
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return nil, &ServiceError{Message: "not a git repository"}
	}

	statusOut, sErr := git(root, "status", "--porcelain")
	if sErr != nil {
		statusOut = ""
	}
	diffStat, _ := git(root, "diff", "--stat")
	diffStatHead, _ := git(root, "diff", "HEAD", "--stat")
	if base != "" {
		diffStat, _ = git(root, "diff", base, "--stat")
		diffStatHead, _ = git(root, "diff", base, "HEAD", "--stat")
	}

	type change struct {
		Status    string `json:"status"`
		Path      string `json:"path"`
		Staged    bool   `json:"staged"`
		Untracked bool   `json:"untracked"`
	}
	var changes []change
	if statusOut != "" {
		for _, line := range strings.Split(statusOut, "\n") {
			if len(line) < 4 {
				continue
			}
			staged := line[0] != ' ' && line[0] != '?'
			unstaged := line[1] != ' ' && line[1] != '?'
			if !staged && !unstaged {
				continue
			}
			path := strings.TrimSpace(line[3:])
			changes = append(changes, change{
				Status:    strings.Join([]string{string(line[0]), string(line[1])}, ""),
				Path:      path,
				Staged:    staged,
				Untracked: line[0] == '?',
			})
		}
	}

	projectPath := relPath(root, strProp(projects[0], "path"))
	if projectPath == "" {
		projectPath = "."
	}

	added, deleted, modified := 0, 0, 0
	for _, c := range changes {
		switch c.Status[0] {
		case 'A':
			added++
		case 'D':
			deleted++
		default:
			modified++
		}
	}

	wholeFile := map[string]bool{}
	for _, c := range changes {
		if c.Status[0] == 'A' || c.Untracked {
			wholeFile[c.Path] = true
		}
	}

	changedSymbols := make([]map[string]any, 0)
	filesChanged := make([]map[string]any, 0)
	for _, c := range changes {
		filesChanged = append(filesChanged, map[string]any{
			"path":    c.Path,
			"status":  c.Status,
			"staged":  c.Staged,
			"added":   c.Status[0] == 'A',
			"deleted": c.Status[0] == 'D',
		})
		nodes, _ := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID, "path": filepath.Join(root, c.Path)})
		for _, n := range nodes {
			if wholeFile[c.Path] {
				changedSymbols = append(changedSymbols, map[string]any{
					"file": relPath(root, strProp(n, "path")),
					"note": "whole file changed — all symbols may be affected",
				})
				continue
			}
			fns, _ := s.graph.FindNodes("Function", map[string]any{"project_id": projectID, "path": strProp(n, "path")})
			for _, fn := range fns {
				changedSymbols = append(changedSymbols, map[string]any{
					"file":   relPath(root, strProp(n, "path")),
					"kind":   "Function",
					"name":   strProp(fn, "name"),
					"line":   fn.Properties["line_start"],
					"status": c.Status,
				})
			}
			types, _ := s.graph.FindNodes("Class", map[string]any{"project_id": projectID, "path": strProp(n, "path")})
			if st, _ := s.graph.FindNodes("Struct", map[string]any{"project_id": projectID, "path": strProp(n, "path")}); len(st) > 0 {
				types = append(types, st...)
			}
			for _, t := range types {
				changedSymbols = append(changedSymbols, map[string]any{
					"file":   relPath(root, strProp(n, "path")),
					"kind":   t.Kind,
					"name":   strProp(t, "name"),
					"line":   t.Properties["line_start"],
					"status": c.Status,
				})
			}
		}
	}
	sort.Slice(changedSymbols, func(i, j int) bool {
		a, b := changedSymbols[i]["file"].(string), changedSymbols[j]["file"].(string)
		if a != b {
			return a < b
		}
		ai, aj := changedSymbols[i]["line"].(int), changedSymbols[j]["line"].(int)
		if ai != aj {
			return ai < aj
		}
		return changedSymbols[i]["name"].(string) < changedSymbols[j]["name"].(string)
	})

	risk := riskLevel(len(filesChanged), 0, 0, 0, 0)
	if modified > 10 {
		risk = "high"
	} else if modified > 3 {
		risk = "medium"
	}

	return map[string]any{
		"project_id":      projectID,
		"root":            projectPath,
		"changed_files":   filesChanged,
		"changed_symbols": changedSymbols,
		"file_count":      len(filesChanged),
		"symbol_count":    len(changedSymbols),
		"added_files":     added,
		"deleted_files":   deleted,
		"modified_files":  modified,
		"total_changes":   len(changes),
		"diff_stat":       diffStat,
		"diff_stat_head":  diffStatHead,
		"risk":            risk,
		"has_changes":     len(changes) > 0,
		"base":            base,
	}, nil
}
