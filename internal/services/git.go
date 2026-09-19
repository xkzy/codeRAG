package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"codergag/internal/graph"
)

type GitService struct {
	graph graph.GraphRepository
	code  *CodeGraphService
}

func NewGitService(g graph.GraphRepository) *GitService {
	return &GitService{graph: g, code: NewCodeGraphService(g)}
}

func (s *GitService) PrContext(projectID, base string, limit int) (map[string]any, error) {
	projects, err := s.graph.FindNodes("Project", map[string]any{"id": projectID})
	if err != nil || len(projects) == 0 {
		return nil, &ServiceError{Message: "project not found"}
	}
	projectPath, _ := projects[0].Properties["path"].(string)
	root, err := filepath.Abs(projectPath)
	if err != nil || !dirExists(root) {
		return nil, &ServiceError{Message: "project path is unavailable"}
	}

	result := exec.Command("git", "-C", root, "diff", "--name-status", base, "--")
	out, err := result.Output()
	if err != nil {
		if base == "HEAD~1" {
			result = exec.Command("git", "-C", root, "diff", "--name-status", "HEAD", "--")
			out, err = result.Output()
		}
		if err != nil {
			return nil, &ServiceError{Message: "git diff failed"}
		}
	}

	type change struct {
		Status string `json:"status"`
		Path   string `json:"path"`
	}
	var changes []change
	pathSet := make(map[string]bool)
	for i, line := range strings.Split(string(out), "\n") {
		if i >= limit {
			break
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) >= 2 {
			path := fields[len(fields)-1]
			changes = append(changes, change{Status: fields[0], Path: path})
			pathSet[path] = true
		}
	}

	var affected []map[string]any
	fns, _ := s.graph.FindNodes("Function", map[string]any{"project_id": projectID})
	for _, fn := range fns {
		fnPath, _ := fn.Properties["path"].(string)
		rel := strings.TrimPrefix(fnPath, root+string(filepath.Separator))
		if pathSet[rel] {
			imp, _ := s.code.Impact(fn.ID, 2, 20)
			affected = append(affected, map[string]any{
				"function": Present(fn),
				"impact":   imp,
			})
		}
	}

	risk := "low"
	if len(affected) > 20 {
		risk = "high"
	} else if len(affected) > 0 {
		risk = "medium"
	}

	return map[string]any{
		"project_id":        projectID,
		"base":              base,
		"changed_files":     changes,
		"affected_functions": affected,
		"risk":              risk,
	}, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
