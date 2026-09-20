package services

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"codergag/internal/cache"
	"codergag/internal/graph"
	"codergag/internal/ids"
	"codergag/internal/models"
)

var (
	supportedExts = map[string]bool{
		".py": true, ".c": true, ".h": true, ".cc": true, ".cpp": true, ".hpp": true,
		".rs": true, ".js": true, ".ts": true, ".tsx": true, ".java": true, ".go": true,
	}
	ignoreDirs = map[string]bool{
		".git": true, ".codegraph": true, "node_modules": true, "build": true,
		"dist": true, "target": true, ".venv": true, "vendor": true,
	}

	genericFuncRe = regexp.MustCompile(`(?:^|\n)\s*(?:pub\s+|static\s+|async\s+|def\s+)?(?:[\w:<>,~*&\[\]\s]+\s+)?([A-Za-z_]\w*)\s*\(([^;{}]*)\)\s*(?:->\s*[\w:<>]+)?\s*(?:\{|:)`)
	pyFuncRe      = regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+([A-Za-z_]\w*)\s*\(([^)]*)\)`)
	jsFuncRe      = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s+)?\(([^)]*)\)\s*=>|^\s*(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_$][\w$]*)\s*\(([^)]*)\)`)
	goFuncRe      = regexp.MustCompile(`(?m)^\s*func\s+(?:\([^)]*\)\s+)?([A-Za-z_]\w*)\s*\(([^)]*)\)`)
	rustFuncRe    = regexp.MustCompile(`(?m)^\s*(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?fn\s+([A-Za-z_]\w*)\s*(?:<[^>]+>)?\s*\(([^)]*)\)`)
	javaFuncRe    = regexp.MustCompile(`(?m)^\s*(?:public|private|protected|static|final|synchronized|abstract|native|\s)+[\w<>\[\], ?]+\s+([A-Za-z_]\w*)\s*\(([^)]*)\)\s*(?:throws[^\{]+)?\{`)
	typeRe        = regexp.MustCompile(`(?m)^\s*(?:class|struct|enum|interface)\s+([A-Za-z_]\w*)`)
	importRe      = regexp.MustCompile(`(?m)^\s*(?:from\s+([\w.]+)\s+import|import\s+([\w./-]+)|#include\s*[<"]([^>"]+))`)
	callRe        = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`)
)

type IndexedFile struct {
	Path      string
	Changed   bool
	Functions int
}

type FileChange struct {
	Path    string
	Deleted bool
}

type CodeIndexService struct {
	graph         graph.GraphRepository
	cache         *cache.CacheManager
	parserVersion string
}

func (s *CodeIndexService) SetCache(cm *cache.CacheManager) {
	s.cache = cm
}

func NewCodeIndexService(g graph.GraphRepository) *CodeIndexService {
	return &CodeIndexService{graph: g, parserVersion: "treesitter-v7"}
}

func (s *CodeIndexService) ParserVersion() string { return s.parserVersion }

// MaxIndexFiles bounds how many source files one full index pass will visit,
// so a mis-rooted project (e.g. a huge monorepo or $HOME) cannot exhaust memory.
var MaxIndexFiles = 50000

func (s *CodeIndexService) IndexRepository(projectID, root string, incremental bool, ignore []string) (map[string]any, error) {
	path, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("not a repository directory: %s", root)
	}
	path = info

	project, err := s.graph.UpsertNode("Project", map[string]any{"id": projectID}, map[string]any{
		"project_id": projectID, "path": path,
	})
	if err != nil {
		return nil, err
	}

	excluded := make(map[string]bool)
	for k, v := range ignoreDirs {
		excluded[k] = v
	}
	for _, ig := range ignore {
		excluded[ig] = true
	}

	var results []*IndexedFile
	discovered := make(map[string]bool)
	truncated := false
	walkErr := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != path && excluded[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if !supportedExts[ext] {
			return nil
		}
		rel, _ := filepath.Rel(path, p)
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			if excluded[part] {
				return nil
			}
		}
		if len(discovered) >= MaxIndexFiles {
			truncated = true
			return filepath.SkipAll
		}
		discovered[p] = true
		indexed, err := s.indexFile(project.ID, projectID, p, incremental)
		if err == nil {
			results = append(results, indexed)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	existingFiles, _ := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID})
	var deletedFiles []*models.Node
	for _, nf := range existingFiles {
		nfPath, _ := nf.Properties["path"].(string)
		if !truncated && strings.HasPrefix(nfPath, path) && !discovered[nfPath] {
			deletedFiles = append(deletedFiles, nf)
		}
	}
	var deleted []string
	for _, file := range deletedFiles {
		deleted = append(deleted, file.ID)
	}
	for _, file := range deletedFiles {
		neighbors, _ := s.graph.Neighbors(file.ID, "DEFINES", graph.DirOut)
		for _, en := range neighbors {
			deleted = append(deleted, en.Node.ID)
		}
	}
	if len(deleted) > 0 {
		s.graph.RemoveNodes(deleted)
	}

	s.resolveGraph(projectID)

	// Run graphify as part of indexing - builds the knowledge graph
	// automatically so the repository is always searchable. Incremental passes
	// reuse the latest stored run to avoid rebuilding and retaining full graphs.
	changedCount := 0
	for _, r := range results {
		if r.Changed {
			changedCount++
		}
	}
	prior, _ := s.graph.FindNodes("GraphifyRun", map[string]any{"project_id": projectID})
	skipGraphify := incremental && len(prior) > 0
	var graphErr error
	var graphOut *Graph
	if !skipGraphify {
		graphOut, graphErr = NewGraphify(path, false, false).Run()
	}
	if graph := graphOut; !skipGraphify && graphErr == nil {
		data, _ := json.Marshal(graph)
		s.graph.UpsertNode("GraphifyRun", map[string]any{
			"project_id": projectID,
		}, map[string]any{
			"graph_json":  string(data),
			"nodes":       len(graph.Nodes),
			"edges":       len(graph.Edges),
			"communities": len(graph.Communities),
			"ran_at":      time.Now().UTC().Format(time.RFC3339),
		})
	}

	funcs := 0
	changed := 0
	for _, r := range results {
		funcs += r.Functions
		if r.Changed {
			changed++
		}
	}
	return map[string]any{
		"project_id":     projectID,
		"files_seen":     len(results),
		"files_changed":  changed,
		"files_deleted":  len(deletedFiles),
		"truncated":      truncated,
		"functions":      funcs,
		"parser_version": s.parserVersion,
	}, nil
}

// IndexFile indexes one file and resolves call edges for the project.
func (s *CodeIndexService) IndexFile(projectNodeID, projectID, filePath string, incremental bool) (*IndexedFile, error) {
	res, err := s.indexFile(projectNodeID, projectID, filePath, incremental)
	if err == nil && res.Changed {
		s.resolveGraph(projectID)
	}
	return res, err
}

// IndexFiles indexes specific files incrementally. Useful for change-based indexing.
// Handles deleted files by removing them from the graph.
func (s *CodeIndexService) IndexFiles(projectID, root string, files []string, incremental bool, ignore []string) (map[string]any, error) {
	path, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("not a repository directory: %s", root)
	}
	path = info

	project, err := s.graph.UpsertNode("Project", map[string]any{"id": projectID}, map[string]any{
		"project_id": projectID, "path": path,
	})
	if err != nil {
		return nil, err
	}

	var results []*IndexedFile
	var failed []string
	deleted := 0
	for _, f := range files {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			s.removeFile(projectID, f)
			deleted++
			continue
		}
		indexed, err := s.indexFile(project.ID, projectID, f, incremental)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", f, err))
			continue
		}
		results = append(results, indexed)
	}

	funcs := 0
	changed := 0
	for _, r := range results {
		funcs += r.Functions
		if r.Changed {
			changed++
		}
	}
	if changed > 0 || deleted > 0 {
		s.resolveGraph(projectID)
	}
	return map[string]any{
		"project_id":     projectID,
		"files_seen":     len(results),
		"files_changed":  changed,
		"files_deleted":  deleted,
		"files_failed":   failed,
		"functions":      funcs,
		"parser_version": s.parserVersion,
	}, errors.Join(errorsFromStrings(failed)...)
}

// removeFile removes a file and its associated symbols from the graph.
func (s *CodeIndexService) removeFile(projectID, filePath string) {
	existing, _ := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID, "path": filePath})
	if len(existing) == 0 {
		return
	}
	fileNode := existing[0]
	neighbors, _ := s.graph.Neighbors(fileNode.ID, "DEFINES", graph.DirOut)
	deleted := []string{fileNode.ID}
	for _, en := range neighbors {
		deleted = append(deleted, en.Node.ID)
	}
	if len(deleted) > 0 {
		s.graph.RemoveNodes(deleted)
	}
}

func (s *CodeIndexService) indexFile(projectNodeID, projectID, filePath string, incremental bool) (*IndexedFile, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxIndexFileBytes {
		return &IndexedFile{Path: filePath}, nil
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	if len(content) > maxIndexFileBytes {
		return &IndexedFile{Path: filePath}, nil
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(content))
	text := string(content)
	generated := isGenerated(filePath, text)
	root := ""
	if pn, err := s.graph.GetNode(projectNodeID); err == nil {
		root = strProp(pn, "path")
	}
	rel := ids.Rel(root, filePath)
	commit := s.headCommit(root)

	existing, _ := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID, "path": filePath})
	if incremental && len(existing) > 0 {
		existingHash, _ := existing[0].Properties["hash"].(string)
		existingParser, _ := existing[0].Properties["parser_version"].(string)
		if existingHash == digest && existingParser == s.parserVersion {
			return &IndexedFile{Path: filePath, Changed: false, Functions: 0}, nil
		}
	}

	// Symbols are updated in place, not deleted and recreated, so their node IDs
	// (and every edge attached to them: memory links, evidence, C->Rust mappings)
	// survive re-indexing. Only symbols that no longer exist are removed.
	previous := map[string]bool{}
	keep := map[string]bool{}
	if len(existing) > 0 {
		neighbors, _ := s.graph.Neighbors(existing[0].ID, "DEFINES", graph.DirOut)
		for _, en := range neighbors {
			previous[en.Node.ID] = true
		}
	}

	language := strings.TrimPrefix(filepath.Ext(filePath), ".")
	source, err := s.graph.UpsertNode("SourceFile", map[string]any{
		"project_id": projectID, "path": filePath,
	}, map[string]any{
		"hash":            digest,
		"parser_version":  s.parserVersion,
		"language":        language,
		"generated":       generated,
		"rel_path":        rel,
		"stable_id":       ids.FileID(rel),
		"commit":          commit,
		"last_indexed_at": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	s.graph.Link("CONTAINS", projectNodeID, source.ID, nil)

	funcInfos := mergeFuncInfos(extractFunctionInfos(text, filepath.Ext(filePath)))
	for _, fi := range funcInfos {
		fn, err := s.graph.UpsertNode("Function", map[string]any{
			"project_id":     projectID,
			"qualified_name": fmt.Sprintf("%s:%s", rel, fi.qualifiedName()),
		}, map[string]any{
			"stable_id":       ids.Symbol(ids.Func, rel, fi.qualifiedName()),
			"rel_path":        rel,
			"start_byte":      fi.span.startByte,
			"end_byte":        fi.span.endByte,
			"start_col":       fi.span.startCol,
			"end_col":         fi.span.endCol,
			"content_hash":    spanHash(content, fi.span),
			"commit":          commit,
			"name":            fi.name,
			"owner":           fi.owner,
			"path":            filePath,
			"line_start":      fi.start,
			"line_end":        fi.end,
			"parameter_count": fi.params,
			"language":        language,
			"source_hash":     digest,
			"generated":       generated,
			"calls":           toAny(uniqueStrings(fi.calls, fi.name)),
			"refs":            toAny(uniqueStrings(fi.refs, fi.name)),
		})
		if err == nil {
			keep[fn.ID] = true
			s.graph.Link("DEFINES", source.ID, fn.ID, nil)
		}
	}

	for _, tm := range extractTypes(text, filepath.Ext(filePath)) {
		node, err := s.graph.UpsertNode(tm.kind, map[string]any{
			"project_id":     projectID,
			"qualified_name": fmt.Sprintf("%s:%s", rel, tm.name),
		}, map[string]any{
			"stable_id":    ids.Symbol(strings.ToLower(tm.kind), rel, tm.name),
			"rel_path":     rel,
			"start_byte":   tm.span.startByte,
			"end_byte":     tm.span.endByte,
			"start_col":    tm.span.startCol,
			"end_col":      tm.span.endCol,
			"content_hash": spanHash(content, tm.span),
			"commit":       commit,
			"name":         tm.name,
			"path":         filePath,
			"line_start":   tm.start,
			"line_end":     tm.end,
			"refs":         toAny(tm.refs),
			"type_kind":    tm.typeKind,
			"language":     language,
			"source_hash":  digest,
			"generated":    generated,
		})
		if err == nil {
			keep[node.ID] = true
			s.graph.Link("DEFINES", source.ID, node.ID, nil)
		}
	}

	var relStrs []any
	if rels, ok := extractRelationsTreeSitter(text, filepath.Ext(filePath)); ok {
		for _, r := range rels {
			relStrs = append(relStrs, encodeRelation(r))
		}
	}
	imports := uniqueStrings(extractImports(text, filepath.Ext(filePath)), "")
	s.graph.UpsertNode("SourceFile", map[string]any{"project_id": projectID, "path": filePath},
		map[string]any{"type_relations": relStrs, "imports": toAny(imports)})

	for _, target := range imports {
		dep, err := s.graph.UpsertNode("Module", map[string]any{
			"project_id": projectID, "name": target,
		}, map[string]any{"name": target})
		if err == nil {
			s.graph.Link("IMPORTS", source.ID, dep.ID, nil)
		}
	}

	var vanished []string
	for id := range previous {
		if !keep[id] {
			vanished = append(vanished, id)
		}
	}
	if len(vanished) > 0 {
		s.graph.RemoveNodes(vanished)
	}

	return &IndexedFile{Path: filePath, Changed: true, Functions: len(funcInfos)}, nil
}

type functionMatch struct {
	name   string
	params string
	start  int
}

func extractFunctions(content, suffix string) []functionMatch {
	var re *regexp.Regexp
	switch suffix {
	case ".py":
		re = pyFuncRe
	case ".rs":
		re = rustFuncRe
	case ".go":
		re = goFuncRe
	case ".js", ".ts", ".tsx":
		re = jsFuncRe
	case ".java":
		re = javaFuncRe
	default:
		re = genericFuncRe
	}
	var matches []functionMatch
	idxMatches := re.FindAllStringSubmatchIndex(content, -1)
	for _, m := range idxMatches {
		if suffix == ".js" || suffix == ".ts" || suffix == ".tsx" {
			name := ""
			params := ""
			if m[2] >= 0 {
				name = content[m[2]:m[3]]
			}
			if m[4] >= 0 {
				params = content[m[4]:m[5]]
			}
			if name == "" && m[6] >= 0 {
				name = content[m[6]:m[7]]
			}
			if params == "" && m[8] >= 0 {
				params = content[m[8]:m[9]]
			}
			matches = append(matches, functionMatch{
				name:   name,
				params: params,
				start:  strings.Count(content[:m[0]], "\n") + 1,
			})
			continue
		}
		name := content[m[2]:m[3]]
		params := content[m[4]:m[5]]
		matches = append(matches, functionMatch{
			name:   name,
			params: params,
			start:  strings.Count(content[:m[0]], "\n") + 1,
		})
	}
	return matches
}

func countParams(params string) int {
	count := 0
	for _, p := range strings.Split(params, ",") {
		if strings.TrimSpace(p) != "" {
			count++
		}
	}
	return count
}

func (s *CodeIndexService) resolveGraph(projectID string) {
	s.ResolveCalls(projectID)
	s.ResolveInheritance(projectID)
	s.ResolveReferences(projectID)
	s.ResolveImports(projectID)
	s.ResolveDataFlow(projectID)
}

// ResolveAll re-runs every resolver for a project and reports graph-wide edge
// totals by kind (the graph does not count edges per project).
func (s *CodeIndexService) ResolveAll(projectID string) map[string]any {
	s.resolveGraph(projectID)
	out := map[string]any{"project_id": projectID}
	if c, ok := s.graph.(interface {
		Counts() (map[string]int, map[string]int)
	}); ok {
		_, edges := c.Counts()
		for _, kind := range []string{"CALLS", "EXTENDS", "IMPLEMENTS", "USES", "DEPENDS_ON"} {
			out[strings.ToLower(kind)] = edges[kind]
		}
	}
	return out
}

// spanHash fingerprints the exact bytes of a definition so a later read can tell
// whether the code under a reference has changed.
func spanHash(content []byte, sp span) string {
	if sp.startByte < 0 || sp.endByte > len(content) || sp.startByte > sp.endByte {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(content[sp.startByte:sp.endByte]))
}

var (
	commitMu    sync.Mutex
	commitCache = map[string]struct {
		sha string
		at  time.Time
	}{}
	commitCacheTTL        = 2 * time.Second
	maxCommitCacheEntries = 256
)

// headCommit returns the checkout's HEAD commit (best effort, cached briefly so
// indexing thousands of files does not fork git per file).
func (s *CodeIndexService) headCommit(root string) string {
	if root == "" {
		return ""
	}
	commitMu.Lock()
	defer commitMu.Unlock()
	pruneCommitCache(time.Now())
	if c, ok := commitCache[root]; ok && time.Since(c.at) < commitCacheTTL {
		return c.sha
	}
	sha, _ := git(root, "rev-parse", "HEAD")
	commitCache[root] = struct {
		sha string
		at  time.Time
	}{sha, time.Now()}
	pruneCommitCache(time.Now())
	return sha
}

func pruneCommitCache(now time.Time) {
	for root, cached := range commitCache {
		if now.Sub(cached.at) >= commitCacheTTL {
			delete(commitCache, root)
		}
	}
	if len(commitCache) <= maxCommitCacheEntries {
		return
	}
	for len(commitCache) > maxCommitCacheEntries {
		var oldestRoot string
		var oldest time.Time
		for root, cached := range commitCache {
			if oldestRoot == "" || cached.at.Before(oldest) {
				oldestRoot, oldest = root, cached.at
			}
		}
		delete(commitCache, oldestRoot)
	}
}

func errorsFromStrings(msgs []string) []error {
	errs := make([]error, 0, len(msgs))
	for _, m := range msgs {
		errs = append(errs, errors.New(m))
	}
	return errs
}
