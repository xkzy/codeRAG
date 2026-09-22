package services

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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

// FileIndexState tracks the index state of a single file for fast incremental indexing.
type FileIndexState struct {
	ProjectID     string    `json:"project_id"`
	Path          string    `json:"path"`
	FileID        string    `json:"file_id"`
	ContentHash   string    `json:"content_hash"`
	Size          int64     `json:"size"`
	Mtime         time.Time `json:"mtime"`
	ParserVersion string    `json:"parser_version"`
	Language      string    `json:"language"`
	IndexedAt     time.Time `json:"indexed_at"`
	IndexStatus   string    `json:"index_status"` // UNSEEN, DISCOVERED, METADATA_READY, STRUCTURE_READY, RELATIONSHIPS_READY, ANALYZED, STALE
}

// ParseArtifact caches the result of parsing a file to avoid re-parsing.
type ParseArtifact struct {
	FileID        string         `json:"file_id"`
	ContentHash   string         `json:"content_hash"`
	ParserVersion string         `json:"parser_version"`
	Language      string         `json:"language"`
	Functions     []FuncInfo     `json:"functions"`
	Types         []TypeInfo     `json:"types"`
	Imports       []string       `json:"imports"`
	TypeRelations []TypeRelation `json:"type_relations"`
	Generated     bool           `json:"generated"`
	ParseTime     time.Time      `json:"parse_time"`
}

// FuncInfo represents a parsed function for the artifact.
type FuncInfo struct {
	Name          string   `json:"name"`
	Owner         string   `json:"owner"`
	Params        int      `json:"params"`
	Start         int      `json:"start"`
	End           int      `json:"end"`
	StartByte     int      `json:"start_byte"`
	EndByte       int      `json:"end_byte"`
	StartCol      int      `json:"start_col"`
	EndCol        int      `json:"end_col"`
	Calls         []string `json:"calls"`
	Refs          []string `json:"refs"`
	QualifiedName string   `json:"qualified_name"`
	ContentHash   string   `json:"content_hash"`
}

// TypeInfo represents a parsed type for the artifact.
type TypeInfo struct {
	Kind          string   `json:"kind"`
	Name          string   `json:"name"`
	Start         int      `json:"start"`
	End           int      `json:"end"`
	StartByte     int      `json:"start_byte"`
	EndByte       int      `json:"end_byte"`
	StartCol      int      `json:"start_col"`
	EndCol        int      `json:"end_col"`
	Refs          []string `json:"refs"`
	TypeKind      string   `json:"type_kind"`
	ContentHash   string   `json:"content_hash"`
	QualifiedName string   `json:"qualified_name"`
}

// TypeRelation represents an inheritance/implementation relation.
type TypeRelation struct {
	Sub   string `json:"sub"`
	Rel   string `json:"rel"`
	Super string `json:"super"`
}

// GraphDelta represents a batched set of graph changes.
type GraphDelta struct {
	AddNodes    []NodeDelta `json:"add_nodes"`
	UpdateNodes []NodeDelta `json:"update_nodes"`
	DeleteNodes []string    `json:"delete_nodes"`
	AddEdges    []EdgeDelta `json:"add_edges"`
	DeleteEdges []string    `json:"delete_edges"`
}

// NodeDelta represents a node addition or update.
type NodeDelta struct {
	Kind       string         `json:"kind"`
	Key        map[string]any `json:"key"`
	Properties map[string]any `json:"properties"`
}

// EdgeDelta represents an edge addition.
type EdgeDelta struct {
	Kind       string         `json:"kind"`
	FromKey    map[string]any `json:"from_key"`
	ToKey      map[string]any `json:"to_key"`
	Properties map[string]any `json:"properties"`
}

// DirtySet tracks pending changes for coalesced incremental indexing.
type DirtySet struct {
	mu               sync.Mutex
	ChangedFiles     map[string]bool   `json:"changed_files"`
	DeletedFiles     map[string]bool   `json:"deleted_files"`
	RenamedFiles     map[string]string `json:"renamed_files"` // old -> new
	AffectedSymbols  map[string]bool   `json:"affected_symbols"`
	InvalidatedCache map[string]bool   `json:"invalidated_cache"`
	Priority         map[string]int    `json:"priority"` // file -> priority
	LastEventTime    time.Time         `json:"last_event_time"`
}

// IndexingMetrics tracks detailed performance metrics.
type IndexingMetrics struct {
	mu                sync.Mutex
	IndexTotalMs      int64            `json:"index_total_ms"`
	FileDiscoveryMs   int64            `json:"file_discovery_ms"`
	HashMs            int64            `json:"hash_ms"`
	ParseMs           int64            `json:"parse_ms"`
	GraphUpdateMs     int64            `json:"graph_update_ms"`
	StorageWriteMs    int64            `json:"storage_write_ms"`
	RelationshipMs    int64            `json:"relationship_ms"`
	AnalysisMs        int64            `json:"analysis_ms"`
	CacheLookupMs     int64            `json:"cache_lookup_ms"`
	CacheHitRate      float64          `json:"cache_hit_rate"`
	FilesScanned      int              `json:"files_scanned"`
	FilesParsed       int              `json:"files_parsed"`
	FilesSkipped      int              `json:"files_skipped"`
	NodesAdded        int              `json:"nodes_added"`
	NodesUpdated      int              `json:"nodes_updated"`
	NodesDeleted      int              `json:"nodes_deleted"`
	EdgesAdded        int              `json:"edges_added"`
	EdgesDeleted      int              `json:"edges_deleted"`
	QueueDepth        int              `json:"queue_depth"`
	WorkerUtilization float64          `json:"worker_utilization"`
	StageTimings      map[string]int64 `json:"stage_timings"`
}

type CodeIndexService struct {
	graph              graph.GraphRepository
	cache              *cache.CacheManager
	parserVersion      string
	progressMap        map[string]*IndexProgress
	progressMu         sync.Mutex
	maxWorkers         int
	resolverCache      *resolverCache
	parseArtifactCache *ParseArtifactCache
	dirtySet           *DirtySet
	metrics            *IndexingMetrics
	dependencyGraph    *DependencyGraph
}

// ParseArtifactCache provides thread-safe caching of parse artifacts.
type ParseArtifactCache struct {
	mu        sync.RWMutex
	artifacts map[string]*ParseArtifact
	maxSize   int
}

// DependencyGraph tracks file-to-symbol dependencies for invalidation.
type DependencyGraph struct {
	mu              sync.RWMutex
	fileToSymbols   map[string]map[string]bool // file -> symbol IDs
	symbolToFiles   map[string]map[string]bool // symbol -> file IDs
	symbolToSymbols map[string]map[string]bool // symbol -> dependent symbol IDs (CALLS, USES, etc.)
}

type resolverCache struct {
	mu            sync.Mutex
	projectID     string
	functions     []*models.Node
	functionsByID map[string]*models.Node
	byName        map[string][]string
	byQualified   map[string][]string
	pathOf        map[string]string
	typesByName   map[string][]string
	typePaths     map[string]string
	funcNames     map[string]bool
	ownerUses     map[string]map[string]bool
	fnOwner       map[string]string
	fnRefs        map[string][]string
	commitCache   map[string]struct {
		sha string
		at  time.Time
	}
}

func (s *CodeIndexService) getCache(projectID string) *resolverCache {
	s.resolverCache.mu.Lock()
	defer s.resolverCache.mu.Unlock()
	if s.resolverCache.projectID != projectID {
		s.resolverCache = &resolverCache{projectID: projectID}
	}
	return s.resolverCache
}

func (s *CodeIndexService) invalidateCache(projectID string) {
	s.resolverCache.mu.Lock()
	defer s.resolverCache.mu.Unlock()
	if s.resolverCache.projectID == projectID {
		s.resolverCache = &resolverCache{}
	}
}

func NewCodeIndexService(g graph.GraphRepository) *CodeIndexService {
	return &CodeIndexService{
		graph:         g,
		parserVersion: "treesitter-v7",
		resolverCache: &resolverCache{},
		parseArtifactCache: &ParseArtifactCache{
			artifacts: make(map[string]*ParseArtifact),
			maxSize:   10000,
		},
		dirtySet: &DirtySet{
			ChangedFiles:     make(map[string]bool),
			DeletedFiles:     make(map[string]bool),
			RenamedFiles:     make(map[string]string),
			AffectedSymbols:  make(map[string]bool),
			InvalidatedCache: make(map[string]bool),
			Priority:         make(map[string]int),
		},
		metrics: &IndexingMetrics{
			StageTimings: make(map[string]int64),
		},
		dependencyGraph: &DependencyGraph{
			fileToSymbols:   make(map[string]map[string]bool),
			symbolToFiles:   make(map[string]map[string]bool),
			symbolToSymbols: make(map[string]map[string]bool),
		},
	}
}

func (s *CodeIndexService) ParserVersion() string { return s.parserVersion }

// ParseArtifactCache methods
func (c *ParseArtifactCache) Get(key string) (*ParseArtifact, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	artifact, ok := c.artifacts[key]
	return artifact, ok
}

func (c *ParseArtifactCache) Set(key string, artifact *ParseArtifact) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.artifacts) >= c.maxSize {
		// Simple eviction: remove oldest 10%
		count := 0
		for k := range c.artifacts {
			delete(c.artifacts, k)
			count++
			if count >= c.maxSize/10 {
				break
			}
		}
	}
	c.artifacts[key] = artifact
}

func (c *ParseArtifactCache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.artifacts, key)
}

// DirtySet methods
func (d *DirtySet) AddChanged(file string, priority int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ChangedFiles[file] = true
	d.DeletedFiles[file] = false
	d.Priority[file] = priority
	d.LastEventTime = time.Now()
}

func (d *DirtySet) AddDeleted(file string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.DeletedFiles[file] = true
	d.ChangedFiles[file] = false
	d.LastEventTime = time.Now()
}

func (d *DirtySet) AddRenamed(oldPath, newPath string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.RenamedFiles[oldPath] = newPath
	d.DeletedFiles[oldPath] = true
	d.ChangedFiles[newPath] = true
	d.LastEventTime = time.Now()
}

func (d *DirtySet) GetPending() (changed, deleted, renamed map[string]bool, priority map[string]int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	changed = make(map[string]bool, len(d.ChangedFiles))
	for k, v := range d.ChangedFiles {
		changed[k] = v
	}
	deleted = make(map[string]bool, len(d.DeletedFiles))
	for k, v := range d.DeletedFiles {
		deleted[k] = v
	}
	renamed = make(map[string]bool)
	for k := range d.RenamedFiles {
		renamed[k] = true
	}
	priority = make(map[string]int, len(d.Priority))
	for k, v := range d.Priority {
		priority[k] = v
	}
	// Clear after getting
	d.ChangedFiles = make(map[string]bool)
	d.DeletedFiles = make(map[string]bool)
	d.RenamedFiles = make(map[string]string)
	d.Priority = make(map[string]int)
	return
}

func (d *DirtySet) InvalidateSymbol(symbol string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.AffectedSymbols[symbol] = true
}

func (d *DirtySet) InvalidateCache(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.InvalidatedCache[key] = true
}

func (d *DirtySet) Size() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.ChangedFiles) + len(d.DeletedFiles) + len(d.RenamedFiles)
}

// DependencyGraph methods
func (dg *DependencyGraph) AddFileSymbol(file, symbol string) {
	dg.mu.Lock()
	defer dg.mu.Unlock()
	if dg.fileToSymbols[file] == nil {
		dg.fileToSymbols[file] = make(map[string]bool)
	}
	dg.fileToSymbols[file][symbol] = true
	if dg.symbolToFiles[symbol] == nil {
		dg.symbolToFiles[symbol] = make(map[string]bool)
	}
	dg.symbolToFiles[symbol][file] = true
}

func (dg *DependencyGraph) AddSymbolDependency(from, to string) {
	dg.mu.Lock()
	defer dg.mu.Unlock()
	if dg.symbolToSymbols[from] == nil {
		dg.symbolToSymbols[from] = make(map[string]bool)
	}
	dg.symbolToSymbols[from][to] = true
}

func (dg *DependencyGraph) GetDependentSymbols(symbol string) []string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()
	var deps []string
	if depsMap, ok := dg.symbolToSymbols[symbol]; ok {
		for dep := range depsMap {
			deps = append(deps, dep)
		}
	}
	return deps
}

func (dg *DependencyGraph) GetFilesForSymbol(symbol string) []string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()
	var files []string
	if filesMap, ok := dg.symbolToFiles[symbol]; ok {
		for f := range filesMap {
			files = append(files, f)
		}
	}
	return files
}

func (dg *DependencyGraph) GetSymbolsForFile(file string) []string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()
	var symbols []string
	if symMap, ok := dg.fileToSymbols[file]; ok {
		for s := range symMap {
			symbols = append(symbols, s)
		}
	}
	return symbols
}

func (dg *DependencyGraph) InvalidateFile(file string) []string {
	dg.mu.Lock()
	defer dg.mu.Unlock()
	symbols := dg.fileToSymbols[file]
	var affected []string
	for s := range symbols {
		affected = append(affected, s)
		// Remove reverse mapping
		delete(dg.symbolToFiles[s], file)
		// Also find dependent symbols
		if deps, ok := dg.symbolToSymbols[s]; ok {
			for dep := range deps {
				affected = append(affected, dep)
			}
		}
	}
	delete(dg.fileToSymbols, file)
	return affected
}

// IndexProgress is a snapshot of a long-running index pass. It is updated
// incrementally by the walk and read by the `index_progress` MCP tool so an
// agent can report on an indexing operation that takes longer than one turn.
type IndexProgress struct {
	mu         sync.Mutex
	ProjectID  string `json:"project_id"`
	Root       string `json:"root"`
	Phase      string `json:"phase"`       // "walk", "resolve", "graphify", "done"
	FilesTotal int    `json:"files_total"` // estimated total, 0 until the walk finishes
	FilesDone  int    `json:"files_done"`
	Functions  int    `json:"functions"`
	StartedAt  string `json:"started_at"`
	UpdatedAt  string `json:"updated_at"`
	Finished   bool   `json:"finished"`
	Truncated  bool   `json:"truncated"`
	Error      string `json:"error,omitempty"`
}

// snapshot returns a copy of the progress state safe for concurrent readers.
func (p *IndexProgress) snapshot() IndexProgress {
	p.mu.Lock()
	defer p.mu.Unlock()
	return IndexProgress{
		ProjectID: p.ProjectID, Root: p.Root, Phase: p.Phase,
		FilesTotal: p.FilesTotal, FilesDone: p.FilesDone, Functions: p.Functions,
		StartedAt: p.StartedAt, UpdatedAt: p.UpdatedAt, Finished: p.Finished,
		Truncated: p.Truncated, Error: p.Error,
	}
}

func (p *IndexProgress) set(phase string, filesDone, functions int, truncated bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Phase = phase
	p.FilesDone = filesDone
	p.Functions = functions
	p.Truncated = truncated
	p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		p.Error = err.Error()
	}
	if phase == "done" {
		p.Finished = true
	}
}

// progressFor returns the progress tracker for a project, creating one if it
// does not exist. Trackers are retained for 24h so a late `index_progress`
// call can still report the final totals.
func (s *CodeIndexService) progressFor(projectID, root string) *IndexProgress {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	if s.progressMap == nil {
		s.progressMap = map[string]*IndexProgress{}
	}
	if p, ok := s.progressMap[projectID]; ok {
		// reuse the existing tracker for a new pass
		p.mu.Lock()
		p.Finished = false
		p.Error = ""
		p.Phase = "walk"
		p.FilesTotal = 0
		p.FilesDone = 0
		p.Functions = 0
		p.Truncated = false
		p.StartedAt = time.Now().UTC().Format(time.RFC3339)
		p.UpdatedAt = p.StartedAt
		p.mu.Unlock()
		return p
	}
	p := &IndexProgress{ProjectID: projectID, Root: root, Phase: "walk",
		StartedAt: time.Now().UTC().Format(time.RFC3339)}
	p.UpdatedAt = p.StartedAt
	s.progressMap[projectID] = p
	return p
}

// Progress returns the most recent index-progress snapshot for a project, or
// nil if no indexing has run for it.
func (s *CodeIndexService) Progress(projectID string) *IndexProgress {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	if s.progressMap == nil {
		return nil
	}
	p := s.progressMap[projectID]
	if p == nil {
		return nil
	}
	snap := p.snapshot()
	return &snap
}

func (s *CodeIndexService) SetCache(cm *cache.CacheManager) {
	s.cache = cm
}

// SetMaxWorkers sets the maximum number of parallel workers for indexing.
// 0 means use the default (CPU cores).
func (s *CodeIndexService) SetMaxWorkers(n int) {
	s.maxWorkers = n
}

// MaxIndexFiles bounds how many source files one full index pass will visit,
// so a mis-rooted project (e.g. a huge monorepo or $HOME) cannot exhaust memory.
var MaxIndexFiles = 50000

func (s *CodeIndexService) IndexRepository(projectID, root string, incremental bool, ignore []string, skipGraphify bool) (map[string]any, error) {
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

	progress := s.progressFor(projectID, path)

	// Fast change detection: use git diff if available, otherwise walk
	files, changedFiles, truncated, walkErr := s.discoverFiles(path, incremental, excluded, projectID)
	if walkErr != nil {
		return nil, walkErr
	}

	progress.FilesTotal = len(files)
	progress.set("walk", 0, 0, truncated, nil)

	// Parallel index files using worker pool
	var results []*IndexedFile
	if incremental && len(changedFiles) > 0 && !truncated {
		// Incremental: only index changed files
		results = s.indexFilesParallel(project.ID, projectID, changedFiles, incremental, progress)
	} else if !incremental {
		// Full re-index
		results = s.indexFilesParallel(project.ID, projectID, files, incremental, progress)
	} else {
		// All files unchanged, nothing to index
		results = s.indexFilesParallel(project.ID, projectID, files, incremental, progress)
	}

	// Invalidate resolver cache since graph has changed
	s.invalidateCache(projectID)

	// Handle deleted files (files that existed in graph but not on disk)
	existingFiles, _ := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID})
	filesSet := make(map[string]bool, len(files))
	for _, f := range files {
		filesSet[f] = true
	}
	var deletedFiles []*models.Node
	for _, nf := range existingFiles {
		nfPath := strProp(nf, "path")
		if !truncated && strings.HasPrefix(nfPath, path) && !filesSet[nfPath] {
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

	// Count changed files
	changedCount := 0
	for _, r := range results {
		if r.Changed {
			changedCount++
		}
	}

	// Only run resolveGraph if there are changes or forced full index
	shouldResolve := !incremental || changedCount > 0 || len(deletedFiles) > 0
	if shouldResolve {
		progress.set("resolve", len(results), sumFuncs(results), truncated, nil)
		s.resolveGraph(projectID)
	}

	prior, _ := s.graph.FindNodes("GraphifyRun", map[string]any{"project_id": projectID})
	skipGraphifyInternal := incremental && len(prior) > 0
	if skipGraphify {
		skipGraphifyInternal = true
	}
	var graphErr error
	var graphOut *Graph
	if !skipGraphifyInternal {
		progress.set("graphify", len(results), sumFuncs(results), truncated, nil)
		graphOut, graphErr = NewGraphify(path, false, false).Run()
	}
	if graph := graphOut; !skipGraphifyInternal && graphErr == nil {
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
	progress.set("done", len(results), funcs, truncated, nil)
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

// discoverFiles performs fast filesystem discovery with optional git-aware change detection.
// Returns all files (for full index), changed files (for incremental), truncation flag, and error.
func (s *CodeIndexService) discoverFiles(root string, incremental bool, excluded map[string]bool, projectID string) (allFiles, changedFiles []string, truncated bool, err error) {
	if incremental {
		// Try git-based change detection first
		if changed, errGit := s.gitChangedFiles(root); errGit == nil && len(changed) > 0 {
			return changed, changed, false, nil
		}
	}

	var files []string
	var mtimes []time.Time
	walkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && excluded[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if !supportedExts[ext] {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			if excluded[part] {
				return nil
			}
		}
		if len(files) >= MaxIndexFiles {
			truncated = true
			return filepath.SkipAll
		}
		// For incremental, use fast mtime check
		if incremental {
			info, err := d.Info()
			if err == nil {
				mtimes = append(mtimes, info.ModTime())
			}
		}
		files = append(files, p)
		return nil
	})
	if walkErr != nil {
		return nil, nil, false, walkErr
	}

	// If incremental, filter to only changed files using content hash
	if incremental {
		existingFiles, _ := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID})
		existingMap := make(map[string]*models.Node, len(existingFiles))
		for _, nf := range existingFiles {
			existingMap[strProp(nf, "path")] = nf
		}

		for _, f := range files {
			// Fast mtime check first
			info, err := os.Stat(f)
			if err != nil {
				continue
			}

			existing, exists := existingMap[f]
			if !exists {
				// New file
				changedFiles = append(changedFiles, f)
				continue
			}

			// Compare mtime
			if existingMTimes, ok := existing.Properties["mtime"].(string); ok {
				if existingMTimes == info.ModTime().Format(time.RFC3339Nano) {
					// Mtime unchanged, likely content unchanged
					continue
				}
			}

			// Mtime changed, verify with content hash
			content, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			if len(content) > maxIndexFileBytes {
				continue
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(content))
			existingHash, _ := existing.Properties["hash"].(string)
			existingParser, _ := existing.Properties["parser_version"].(string)
			if existingHash != digest || existingParser != s.parserVersion {
				changedFiles = append(changedFiles, f)
			}
		}
	}

	return files, changedFiles, truncated, nil
}

// gitChangedFiles returns files changed since last git commit, or files that are in the git index.
func (s *CodeIndexService) gitChangedFiles(root string) ([]string, error) {
	// Check if this is a git repo
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return nil, err
	}
	// Get changed files since last commit
	out, err := git(root, "diff", "--name-only")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fullPath := filepath.Join(root, line)
		ext := strings.ToLower(filepath.Ext(line))
		if supportedExts[ext] && !filepath.IsAbs(line) {
			files = append(files, fullPath)
		}
	}
	if len(files) == 0 {
		// Also check untracked files
		out, err = git(root, "ls-files", "--others", "--exclude-standard")
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			fullPath := filepath.Join(root, line)
			ext := strings.ToLower(filepath.Ext(line))
			if supportedExts[ext] {
				files = append(files, fullPath)
			}
		}
	}
	return files, nil
}

// hasFileChanged checks if a file has changed since last indexing using content hash.
func (s *CodeIndexService) hasFileChanged(projectID, filePath, root string) bool {
	_, err := os.Stat(filePath)
	if err != nil {
		return false
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return false
	}
	if len(content) > maxIndexFileBytes {
		return false
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(content))
	existing, _ := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID, "path": filePath})
	if len(existing) > 0 {
		existingHash, _ := existing[0].Properties["hash"].(string)
		existingParser, _ := existing[0].Properties["parser_version"].(string)
		return existingHash != digest || existingParser != s.parserVersion
	}
	return true // New file
}

// indexFilesParallel indexes multiple files in parallel using a worker pool.
func (s *CodeIndexService) indexFilesParallel(projectNodeID, projectID string, files []string, incremental bool, progress *IndexProgress) []*IndexedFile {
	// Use number of CPU cores as default worker count
	workers := s.maxWorkers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if workers > len(files) {
		workers = len(files)
	}
	if workers < 1 {
		workers = 1
	}

	fileChan := make(chan string, len(files))
	resultChan := make(chan *IndexedFile, len(files))
	errChan := make(chan error, len(files))

	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for filePath := range fileChan {
				indexed, err := s.indexFile(projectNodeID, projectID, filePath, incremental)
				if err != nil {
					errChan <- err
					continue
				}
				if indexed != nil {
					resultChan <- indexed
				}
			}
		}()
	}

	// Send files to workers
	for _, f := range files {
		fileChan <- f
	}
	close(fileChan)

	// Close result channel when all workers done
	go func() {
		wg.Wait()
		close(resultChan)
		close(errChan)
	}()

	// Collect results
	var results []*IndexedFile
	done := 0
	for indexed := range resultChan {
		results = append(results, indexed)
		done++
		if done%10 == 0 || done == len(files) {
			progress.set("walk", done, sumFuncs(results), false, nil)
		}
	}

	// Check for errors (non-fatal, just log)
	select {
	case err := <-errChan:
		_ = err // errors are non-fatal, individual file failures are skipped
	default:
	}

	return results
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

	progress := s.progressFor(projectID, path)
	progress.set("walk", 0, 0, false, nil)

	var results []*IndexedFile
	var failed []string
	deleted := 0

	// Separate existing and deleted files
	var toIndex []string
	for _, f := range files {
		info, err := os.Stat(f)
		if os.IsNotExist(err) {
			s.removeFile(projectID, f)
			deleted++
			continue
		}
		if info.IsDir() {
			failed = append(failed, fmt.Sprintf("%s: is a directory", f))
			continue
		}
		toIndex = append(toIndex, f)
	}

	// Parallel index changed files
	if len(toIndex) > 0 {
		indexedResults := s.indexFilesParallel(project.ID, projectID, toIndex, incremental, progress)
		results = append(results, indexedResults...)
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
		progress.set("resolve", len(results), funcs, false, nil)
		s.resolveGraph(projectID)
	}
	progress.set("done", len(results), funcs, false, errors.Join(errorsFromStrings(failed)...))
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
	language := strings.TrimPrefix(filepath.Ext(filePath), ".")

	// Check parse artifact cache first
	cacheKey := fmt.Sprintf("%s:%s:%s", projectID, filePath, s.parserVersion)
	if artifact, ok := s.parseArtifactCache.Get(cacheKey); ok && incremental {
		if artifact.ContentHash == digest && artifact.ParserVersion == s.parserVersion {
			// Use cached parse artifact
			return s.applyParseArtifact(projectNodeID, projectID, filePath, rel, commit, language, generated, digest, artifact)
		}
	}

	existing, _ := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID, "path": filePath})
	if incremental && len(existing) > 0 {
		existingHash, _ := existing[0].Properties["hash"].(string)
		existingParser, _ := existing[0].Properties["parser_version"].(string)
		if existingHash == digest && existingParser == s.parserVersion {
			return &IndexedFile{Path: filePath, Changed: false, Functions: 0}, nil
		}
	}

	// Parse the file
	parseStart := time.Now()
	funcInfos := mergeFuncInfos(extractFunctionInfos(text, filepath.Ext(filePath)))
	typeInfos := extractTypes(text, filepath.Ext(filePath))
	imports := uniqueStrings(extractImports(text, filepath.Ext(filePath)), "")
	var typeRelations []TypeRelation
	if rels, ok := extractRelationsTreeSitter(text, filepath.Ext(filePath)); ok {
		for _, r := range rels {
			typeRelations = append(typeRelations, TypeRelation{Sub: r.sub, Rel: r.rel, Super: r.super})
		}
	}
	s.metrics.mu.Lock()
	s.metrics.ParseMs += time.Since(parseStart).Milliseconds()
	s.metrics.FilesParsed++
	s.metrics.mu.Unlock()

	// Create parse artifact for caching
	artifact := &ParseArtifact{
		FileID:        ids.FileID(rel),
		ContentHash:   digest,
		ParserVersion: s.parserVersion,
		Language:      language,
		Functions:     make([]FuncInfo, 0, len(funcInfos)),
		Types:         make([]TypeInfo, 0, len(typeInfos)),
		Imports:       imports,
		TypeRelations: typeRelations,
		Generated:     generated,
		ParseTime:     time.Now(),
	}

	for _, fi := range funcInfos {
		artifact.Functions = append(artifact.Functions, FuncInfo{
			Name:          fi.name,
			Owner:         fi.owner,
			Params:        fi.params,
			Start:         fi.start,
			End:           fi.end,
			StartByte:     fi.span.startByte,
			EndByte:       fi.span.endByte,
			StartCol:      fi.span.startCol,
			EndCol:        fi.span.endCol,
			Calls:         uniqueStrings(fi.calls, fi.name),
			Refs:          uniqueStrings(fi.refs, fi.name),
			QualifiedName: fi.qualifiedName(),
			ContentHash:   spanHash(content, fi.span),
		})
	}

	for _, tm := range typeInfos {
		artifact.Types = append(artifact.Types, TypeInfo{
			Kind:          tm.kind,
			Name:          tm.name,
			Start:         tm.start,
			End:           tm.end,
			StartByte:     tm.span.startByte,
			EndByte:       tm.span.endByte,
			StartCol:      tm.span.startCol,
			EndCol:        tm.span.endCol,
			Refs:          tm.refs,
			TypeKind:      tm.typeKind,
			ContentHash:   spanHash(content, tm.span),
			QualifiedName: fmt.Sprintf("%s:%s", rel, tm.name),
		})
	}

	// Cache the parse artifact
	s.parseArtifactCache.Set(cacheKey, artifact)

	// Apply to graph
	return s.applyParseArtifact(projectNodeID, projectID, filePath, rel, commit, language, generated, digest, artifact)
}

// applyParseArtifact applies a parse artifact to the graph, creating/updating nodes.
func (s *CodeIndexService) applyParseArtifact(projectNodeID, projectID, filePath, rel, commit, language string, generated bool, digest string, artifact *ParseArtifact) (*IndexedFile, error) {
	// Track previous symbols for diff
	existing, _ := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID, "path": filePath})
	previous := map[string]bool{}
	keep := map[string]bool{}
	if len(existing) > 0 {
		neighbors, _ := s.graph.Neighbors(existing[0].ID, "DEFINES", graph.DirOut)
		for _, en := range neighbors {
			previous[en.Node.ID] = true
		}
	}

	// Upsert SourceFile
	fileInfo, _ := os.Stat(filePath)
	mtime := time.Now()
	if fileInfo != nil {
		mtime = fileInfo.ModTime()
	}
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
		"mtime":           mtime.UTC().Format(time.RFC3339Nano),
		"last_indexed_at": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	s.graph.Link("CONTAINS", projectNodeID, source.ID, nil)

	// Upsert Functions
	for _, fi := range artifact.Functions {
		fn, err := s.graph.UpsertNode("Function", map[string]any{
			"project_id":     projectID,
			"qualified_name": fmt.Sprintf("%s:%s", rel, fi.QualifiedName),
		}, map[string]any{
			"stable_id":       ids.Symbol(ids.Func, rel, fi.QualifiedName),
			"rel_path":        rel,
			"start_byte":      fi.StartByte,
			"end_byte":        fi.EndByte,
			"start_col":       fi.StartCol,
			"end_col":         fi.EndCol,
			"content_hash":    fi.ContentHash,
			"commit":          commit,
			"name":            fi.Name,
			"owner":           fi.Owner,
			"path":            filePath,
			"line_start":      fi.Start,
			"line_end":        fi.End,
			"parameter_count": fi.Params,
			"language":        language,
			"source_hash":     digest,
			"generated":       generated,
			"calls":           toAny(fi.Calls),
			"refs":            toAny(fi.Refs),
		})
		if err == nil {
			keep[fn.ID] = true
			s.graph.Link("DEFINES", source.ID, fn.ID, nil)
			// Track dependency
			s.dependencyGraph.AddFileSymbol(filePath, fn.ID)
			for _, call := range fi.Calls {
				s.dependencyGraph.AddSymbolDependency(fn.ID, call)
			}
			for _, ref := range fi.Refs {
				s.dependencyGraph.AddSymbolDependency(fn.ID, ref)
			}
		}
	}

	// Upsert Types
	for _, tm := range artifact.Types {
		node, err := s.graph.UpsertNode(tm.Kind, map[string]any{
			"project_id":     projectID,
			"qualified_name": tm.QualifiedName,
		}, map[string]any{
			"stable_id":    ids.Symbol(strings.ToLower(tm.Kind), rel, tm.Name),
			"rel_path":     rel,
			"start_byte":   tm.StartByte,
			"end_byte":     tm.EndByte,
			"start_col":    tm.StartCol,
			"end_col":      tm.EndCol,
			"content_hash": tm.ContentHash,
			"commit":       commit,
			"name":         tm.Name,
			"path":         filePath,
			"line_start":   tm.Start,
			"line_end":     tm.End,
			"refs":         toAny(tm.Refs),
			"type_kind":    tm.TypeKind,
			"language":     language,
			"source_hash":  digest,
			"generated":    generated,
		})
		if err == nil {
			keep[node.ID] = true
			s.graph.Link("DEFINES", source.ID, node.ID, nil)
			s.dependencyGraph.AddFileSymbol(filePath, node.ID)
		}
	}

	// Update SourceFile with relations and imports
	var relStrs []any
	for _, tr := range artifact.TypeRelations {
		relStrs = append(relStrs, encodeRelation(typeRelation{tr.Sub, tr.Rel, tr.Super}))
	}
	s.graph.UpsertNode("SourceFile", map[string]any{"project_id": projectID, "path": filePath},
		map[string]any{"type_relations": relStrs, "imports": toAny(artifact.Imports)})

	for _, target := range artifact.Imports {
		dep, err := s.graph.UpsertNode("Module", map[string]any{
			"project_id": projectID, "name": target,
		}, map[string]any{"name": target})
		if err == nil {
			s.graph.Link("IMPORTS", source.ID, dep.ID, nil)
		}
	}

	// Remove vanished symbols
	var vanished []string
	for id := range previous {
		if !keep[id] {
			vanished = append(vanished, id)
		}
	}
	if len(vanished) > 0 {
		s.graph.RemoveNodes(vanished)
		s.metrics.mu.Lock()
		s.metrics.NodesDeleted += len(vanished)
		s.metrics.mu.Unlock()
	}

	s.metrics.mu.Lock()
	s.metrics.NodesAdded += len(artifact.Functions) + len(artifact.Types)
	s.metrics.FilesScanned++
	s.metrics.mu.Unlock()

	return &IndexedFile{Path: filePath, Changed: true, Functions: len(artifact.Functions)}, nil
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
	// Build shared resolver index once
	idx := s.buildResolverIndex(projectID)

	s.ResolveCallsWithIndex(projectID, idx)

	s.ResolveInheritanceWithIndex(projectID, idx)

	s.ResolveReferencesWithIndex(projectID, idx)

	s.ResolveImportsWithIndex(projectID, idx)

	s.ResolveDataFlowWithIndex(projectID, idx)
}

// ResolverIndex holds pre-built indexes for all resolvers to share.
type ResolverIndex struct {
	Functions     []*models.Node
	FunctionsByID map[string]*models.Node
	ByName        map[string][]string
	ByQualified   map[string][]string
	PathOf        map[string]string
	TypesByName   map[string][]string
	TypePaths     map[string]string
	TypeNames     map[string]string // type ID -> type name
	ClassStructs  []*models.Node    // Class and Struct nodes
	FuncNames     map[string]bool
	OwnerUses     map[string]map[string]bool
	FnOwner       map[string]string
	FnRefs        map[string][]string
}

func (s *CodeIndexService) buildResolverIndex(projectID string) *ResolverIndex {
	// Get all functions
	fns, _ := s.graph.FindNodes("Function", map[string]any{"project_id": projectID})

	// Build function indexes
	functionsByID := map[string]*models.Node{}
	byName := map[string][]string{}
	byQualified := map[string][]string{}
	pathOf := map[string]string{}
	fnOwner := map[string]string{}
	fnRefs := map[string][]string{}
	funcNames := map[string]bool{}

	for _, f := range fns {
		functionsByID[f.ID] = f
		name := strProp(f, "name")
		owner := strProp(f, "owner")
		if name != "" {
			byName[name] = append(byName[name], f.ID)
			funcNames[name] = true
			if owner != "" {
				byQualified[owner+"."+name] = append(byQualified[owner+"."+name], f.ID)
			}
		}
		pathOf[f.ID], _ = f.Properties["path"].(string)
		fnOwner[f.ID] = owner
		fnRefs[f.ID] = strSlice(f.Properties["refs"])
	}

	// Build type indexes
	typesByName := map[string][]string{}
	typePaths := map[string]string{}
	typeNames := map[string]string{}
	var classStructs []*models.Node
	for _, kind := range []string{"Class", "Struct"} {
		nodes, _ := s.graph.FindNodes(kind, map[string]any{"project_id": projectID})
		classStructs = append(classStructs, nodes...)
		for _, n := range nodes {
			name := strProp(n, "name")
			typesByName[name] = append(typesByName[name], n.ID)
			typePaths[n.ID] = strProp(n, "path")
			typeNames[n.ID] = name
		}
	}

	// Build ownerUses: which types does each owner struct/class reference
	ownerUses := map[string]map[string]bool{}
	for _, n := range classStructs {
		name := strProp(n, "name")
		refs := strSlice(n.Properties["refs"])
		if ownerUses[name] == nil {
			ownerUses[name] = map[string]bool{}
		}
		for _, r := range refs {
			ownerUses[name][r] = true
		}
	}

	return &ResolverIndex{
		Functions:     fns,
		FunctionsByID: functionsByID,
		ByName:        byName,
		ByQualified:   byQualified,
		PathOf:        pathOf,
		TypesByName:   typesByName,
		TypePaths:     typePaths,
		TypeNames:     typeNames,
		ClassStructs:  classStructs,
		FuncNames:     funcNames,
		OwnerUses:     ownerUses,
		FnOwner:       fnOwner,
		FnRefs:        fnRefs,
	}
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

// sumFuncs reports the total function count across a batch of indexed files.
func sumFuncs(results []*IndexedFile) int {
	n := 0
	for _, r := range results {
		n += r.Functions
	}
	return n
}
