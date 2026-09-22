package services

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"codergag/internal/graph"
	"codergag/internal/models"
)

type ProjectSnapshot struct {
	ProjectID    string           `json:"project_id"`
	Name        string           `json:"name"`
	RootPath    string           `json:"root_path"`
	GitCommit   string           `json:"git_commit"`
	GitBranch   string           `json:"git_branch"`
	IndexVersion int             `json:"index_version"`
	CreatedAt   string           `json:"created_at"`
	UpdatedAt   string           `json:"updated_at"`

	RepositoryStructure *RepositoryStructure `json:"repository_structure,omitempty"`
	Modules           []*ModuleSummary   `json:"modules,omitempty"`
	EntryPoints       []*EntryPoint     `json:"entry_points,omitempty"`
	Dependencies       []*Dependency      `json:"dependencies,omitempty"`
	ImportantFiles    []*FileNode       `json:"important_files,omitempty"`
	HotSymbols        []*SymbolNode     `json:"hot_symbols,omitempty"`
	FrequentlyAccessed []*AccessNode     `json:"frequently_accessed,omitempty"`
	RecentErrors      []*ErrorNode      `json:"recent_errors,omitempty"`
	BuildConfig      *BuildConfig      `json:"build_config,omitempty"`
	TestConfig       *TestConfig       `json:"test_config,omitempty"`
}

type RepositoryStructure struct {
	Root        string   `json:"root"`
	Directories []string `json:"directories,omitempty"`
	Submodules  []string `json:"submodules,omitempty"`
	Depth       int      `json:"depth"`
}

type ModuleSummary struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Files      int    `json:"files"`
	Functions  int    `json:"functions"`
	Types      int    `json:"types"`
	Dependency string `json:"dependency,omitempty"`
	IsCycle    bool   `json:"is_cycle,omitempty"`
}

type EntryPoint struct {
	Path        string `json:"path"`
	Symbol      string `json:"symbol,omitempty"`
	Kind        string `json:"kind"`
	Description string `json:"description,omitempty"`
}

type Dependency struct {
	FromModule string `json:"from_module"`
	ToModule   string `json:"to_module"`
	Kind       string `json:"kind"`
}

type FileNode struct {
	Path     string `json:"path"`
	Language string `json:"language,omitempty"`
	Size     int    `json:"size"`
	Hash     string `json:"hash,omitempty"`
	AccessCount int `json:"access_count"`
	LastAccessed string `json:"last_accessed"`
}

type SymbolNode struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	AccessCount int  `json:"access_count"`
	LastAccessed string `json:"last_accessed"`
	Callers   []string `json:"callers,omitempty"`
	Callees   []string `json:"callees,omitempty"`
}

type AccessNode struct {
	EntityID   string `json:"entity_id"`
	EntityKind string `json:"entity_kind"`
	Count     int    `json:"count"`
	LastSeen  string `json:"last_seen"`
}

type ErrorNode struct {
	ID        string `json:"id"`
	Message   string `json:"message"`
	Source    string `json:"source"`
	EntityID  string `json:"entity_id,omitempty"`
	Timestamp string `json:"timestamp"`
	Severity  string `json:"severity"`
}

type BuildConfig struct {
	BuildTool   string            `json:"build_tool,omitempty"`
	BuildFile   string            `json:"build_file,omitempty"`
	BuildArgs  []string          `json:"build_args,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
}

type TestConfig struct {
	TestTool   string   `json:"test_tool,omitempty"`
	TestFile   string   `json:"test_file,omitempty"`
	TestArgs   []string `json:"test_args,omitempty"`
	CoverageCmd string `json:"coverage_cmd,omitempty"`
}

type SnapshotManager struct {
	graph       graph.GraphRepository
	maxSnapshots int
}

func NewSnapshotManager(g graph.GraphRepository) *SnapshotManager {
	return &SnapshotManager{
		graph:       g,
		maxSnapshots: 10,
	}
}

func (sm *SnapshotManager) CreateSnapshot(projectID string, app *Application) (*ProjectSnapshot, error) {
	snapshot := &ProjectSnapshot{
		ProjectID:    projectID,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
		IndexVersion: 1,
	}

	if err := sm.populateProjectInfo(snapshot, projectID); err != nil {
		return nil, fmt.Errorf("failed to populate project info: %w", err)
	}

	if err := sm.populateRepositoryStructure(snapshot, projectID); err != nil {
		return nil, fmt.Errorf("failed to populate repository structure: %w", err)
	}

	if err := sm.populateModules(snapshot, projectID); err != nil {
		return nil, fmt.Errorf("failed to populate modules: %w", err)
	}

	if err := sm.populateEntryPoints(snapshot, projectID); err != nil {
		return nil, fmt.Errorf("failed to populate entry points: %w", err)
	}

	if err := sm.populateDependencies(snapshot, projectID); err != nil {
		return nil, fmt.Errorf("failed to populate dependencies: %w", err)
	}

	if err := sm.populateImportantFiles(snapshot, projectID); err != nil {
		return nil, fmt.Errorf("failed to populate important files: %w", err)
	}

	return snapshot, nil
}

func (sm *SnapshotManager) populateProjectInfo(snapshot *ProjectSnapshot, projectID string) error {
	projects, err := sm.graph.FindNodes("Project", map[string]any{"id": projectID})
	if err != nil || len(projects) == 0 {
		return fmt.Errorf("project not found")
	}

	p := projects[0]
	snapshot.Name = strProp(p, "name")
	snapshot.RootPath = strProp(p, "path")

	return nil
}

func (sm *SnapshotManager) populateRepositoryStructure(snapshot *ProjectSnapshot, projectID string) error {
	files, err := sm.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID})
	if err != nil {
		return err
	}

	dirSet := make(map[string]bool)
	for _, f := range files {
		path := strProp(f, "path")
		parts := strings.Split(path, "/")
		for i := 0; i < len(parts)-1; i++ {
			dir := strings.Join(parts[:i+1], "/")
			dirSet[dir] = true
		}
	}

	var dirs []string
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	snapshot.RepositoryStructure = &RepositoryStructure{
		Root:       snapshot.RootPath,
		Directories: dirs,
		Depth:      computeDirDepth(dirs),
	}

	return nil
}

func (sm *SnapshotManager) populateModules(snapshot *ProjectSnapshot, projectID string) error {
	modules, err := sm.graph.FindNodes("Module", map[string]any{"project_id": projectID})
	if err != nil {
		return err
	}

	var summaries []*ModuleSummary
	for _, m := range modules {
		summary := &ModuleSummary{
			Name: strProp(m, "name"),
			Path: strProp(m, "path"),
		}
		summaries = append(summaries, summary)
	}

	snapshot.Modules = summaries
	return nil
}

func (sm *SnapshotManager) populateEntryPoints(snapshot *ProjectSnapshot, projectID string) error {
	mainFiles, _ := sm.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID, "kind": "main"})
	testFiles, _ := sm.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID, "kind": "test"})

	var entryPoints []*EntryPoint
	for _, f := range mainFiles {
		entryPoints = append(entryPoints, &EntryPoint{
			Path: strProp(f, "path"),
			Kind: "main",
		})
	}
	for _, f := range testFiles {
		entryPoints = append(entryPoints, &EntryPoint{
			Path: strProp(f, "path"),
			Kind: "test",
		})
	}

	snapshot.EntryPoints = entryPoints
	return nil
}

func (sm *SnapshotManager) populateDependencies(snapshot *ProjectSnapshot, projectID string) error {
	deps, err := sm.graph.FindNodes("Dependency", map[string]any{"project_id": projectID})
	if err != nil {
		return nil
	}

	var dependencies []*Dependency
	for _, d := range deps {
		dependencies = append(dependencies, &Dependency{
			FromModule: strProp(d, "from_module"),
			ToModule:   strProp(d, "to_module"),
			Kind:       strProp(d, "kind"),
		})
	}

	snapshot.Dependencies = dependencies
	return nil
}

func (sm *SnapshotManager) populateImportantFiles(snapshot *ProjectSnapshot, projectID string) error {
	files, err := sm.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID})
	if err != nil {
		return err
	}

	var important []*FileNode
	buildFiles := []string{"Makefile", "CMakeLists.txt", "BUILD", "go.mod", "Cargo.toml", "package.json", "pom.xml", "build.gradle"}
	buildSet := make(map[string]bool)
	for _, bf := range buildFiles {
		buildSet[bf] = true
	}

	for _, f := range files {
		path := strProp(f, "path")
		isBuild := false
		for bf := range buildSet {
			if strings.Contains(path, bf) {
				isBuild = true
				break
			}
		}

		if isBuild {
			important = append(important, &FileNode{
				Path:     path,
				Language: strProp(f, "language"),
				Size:     intProp(f, "size"),
			})
		}
	}

	snapshot.ImportantFiles = important
	return nil
}

func (sm *SnapshotManager) SaveSnapshot(snapshot *ProjectSnapshot) error {
	snapshot.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("snapshot_%s_%s", snapshot.ProjectID, snapshot.GitCommit)

	_, err = sm.graph.UpsertNode("ProjectSnapshot", map[string]any{
		"project_id": snapshot.ProjectID,
		"key":        key,
	}, map[string]any{
		"snapshot":    string(data),
		"git_commit":  snapshot.GitCommit,
		"git_branch": snapshot.GitBranch,
		"updated_at": snapshot.UpdatedAt,
	})

	return err
}

func (sm *SnapshotManager) LoadLatestSnapshot(projectID string) (*ProjectSnapshot, error) {
	snapshots, err := sm.graph.FindNodes("ProjectSnapshot", map[string]any{"project_id": projectID})
	if err != nil || len(snapshots) == 0 {
		return nil, fmt.Errorf("no snapshots found for project %s", projectID)
	}

	var latest *models.Node
	for _, s := range snapshots {
		if latest == nil || strProp(s, "updated_at") > strProp(latest, "updated_at") {
			latest = s
		}
	}

	if latest == nil {
		return nil, fmt.Errorf("no snapshot found")
	}

	var snapshot ProjectSnapshot
	if err := json.Unmarshal([]byte(strProp(latest, "snapshot")), &snapshot); err != nil {
		return nil, err
	}

	return &snapshot, nil
}

func (sm *SnapshotManager) LoadSnapshotByCommit(projectID, commit string) (*ProjectSnapshot, error) {
	snapshots, err := sm.graph.FindNodes("ProjectSnapshot", map[string]any{
		"project_id": projectID,
		"git_commit": commit,
	})
	if err != nil || len(snapshots) == 0 {
		return nil, fmt.Errorf("no snapshot found for commit %s", commit)
	}

	var snapshot ProjectSnapshot
	if err := json.Unmarshal([]byte(strProp(snapshots[0], "snapshot")), &snapshot); err != nil {
		return nil, err
	}

	return &snapshot, nil
}

func computeDirDepth(dirs []string) int {
	maxDepth := 0
	for _, d := range dirs {
		depth := strings.Count(d, "/") + 1
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	return maxDepth
}
