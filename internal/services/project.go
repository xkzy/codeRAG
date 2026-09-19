package services

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type ProjectIdentity struct {
	ID       string
	Root     string
	Name     string
	Commit   string
	Detected bool
}

type ProjectDetector struct {
	mu       sync.RWMutex
	projects map[string]*ProjectIdentity
	roots    []string
	depth    int
	engine   *EventEngine
}

func NewProjectDetector(roots []string, depth int, engine *EventEngine) *ProjectDetector {
	if len(roots) == 0 {
		roots = []string{"."}
	}
	if depth < 0 {
		depth = 3
	}
	return &ProjectDetector{
		projects: map[string]*ProjectIdentity{},
		roots:    append([]string(nil), roots...),
		depth:    depth,
		engine:   engine,
	}
}

func (d *ProjectDetector) Detect() []*ProjectIdentity {
	var found []*ProjectIdentity
	for _, root := range d.roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if evaluated, err := filepath.EvalSymlinks(abs); err == nil {
			abs = evaluated
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			continue
		}
		d.scan(abs, 0, &found)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Root < found[j].Root })
	return found
}

func (d *ProjectDetector) scan(dir string, depth int, found *[]*ProjectIdentity) {
	if depth > d.depth || isIgnoreDir(filepath.Base(dir)) {
		return
	}
	if isProjectRoot(dir) {
		d.register(dir, found)
		return
	}
	if depth == d.depth {
		return
	}
	entries, err := safeReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || isIgnoreDir(entry.Name()) || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		d.scan(filepath.Join(dir, entry.Name()), depth+1, found)
	}
}

func (d *ProjectDetector) register(root string, found *[]*ProjectIdentity) {
	clean, err := canonicalProjectRoot(root)
	if err != nil {
		return
	}
	id := stableProjectID(clean)
	d.mu.Lock()
	existing := d.projects[id]
	if existing != nil {
		if commit, err := gitRootCommit(clean); err == nil {
			existing.Commit = commit
		}
		d.mu.Unlock()
		return
	}
	identity := &ProjectIdentity{ID: id, Root: clean, Name: filepath.Base(clean), Detected: true}
	if commit, err := gitRootCommit(clean); err == nil {
		identity.Commit = commit
	}
	d.projects[id] = identity
	d.mu.Unlock()
	if found != nil {
		*found = append(*found, identity)
	}
	if d.engine != nil {
		d.engine.Emit(Event{Kind: ProjectDetected, ProjectID: id, Payload: map[string]any{"root": clean}})
	}
}

func (d *ProjectDetector) Projects() []*ProjectIdentity {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]*ProjectIdentity, 0, len(d.projects))
	for _, project := range d.projects {
		out = append(out, &ProjectIdentity{ID: project.ID, Root: project.Root, Name: project.Name, Commit: project.Commit, Detected: project.Detected})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Root < out[j].Root })
	return out
}

func (d *ProjectDetector) Get(root string) *ProjectIdentity {
	clean, err := canonicalProjectRoot(root)
	if err != nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if project := d.projects[stableProjectID(clean)]; project != nil {
		return cloneProject(project)
	}
	var best *ProjectIdentity
	for _, project := range d.projects {
		if pathWithin(project.Root, clean) && (best == nil || len(project.Root) > len(best.Root)) {
			best = cloneProject(project)
		}
	}
	return best
}

func canonicalProjectRoot(root string) (string, error) {
	clean, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if evaluated, err := filepath.EvalSymlinks(clean); err == nil {
		clean = evaluated
	}
	if gitRoot, err := gitRootPath(clean); err == nil && gitRoot != "" {
		clean = gitRoot
	}
	info, err := os.Stat(clean)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		clean = filepath.Dir(clean)
	}
	return filepath.Clean(clean), nil
}

func gitRootPath(root string) (string, error) {
	out, err := git(root, "rev-parse", "--show-toplevel")
	if err != nil || out == "" {
		return "", err
	}
	return filepath.Abs(out)
}

func gitRootCommit(root string) (string, error) {
	return git(root, "rev-parse", "HEAD")
}

func isProjectRoot(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return true
	}
	for _, name := range []string{"go.mod", "package.json", "Cargo.toml", "pyproject.toml", "pom.xml", "build.gradle", "build.gradle.kts", "Makefile"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

func stableProjectID(root string) string {
	clean, err := canonicalProjectRoot(root)
	if err == nil {
		root = clean
	}
	return projectID(root)
}

func projectID(root string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return fmt.Sprintf("project-%x", sum[:8])
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func cloneProject(project *ProjectIdentity) *ProjectIdentity {
	if project == nil {
		return nil
	}
	return &ProjectIdentity{ID: project.ID, Root: project.Root, Name: project.Name, Commit: project.Commit, Detected: project.Detected}
}
