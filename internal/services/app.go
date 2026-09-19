package services

import (
	"codergag/internal/config"
	"codergag/internal/graph"
)

type Application struct {
	Graph     graph.GraphRepository
	Index     *CodeIndexService
	Code      *CodeGraphService
	Evidence  *EvidenceService
	Reverse   *ReverseEngineeringService
	Analysis  *AnalysisService
	Memory    *MemoryService
	Documents *DocumentService
	Git       *GitService
}

func NewApplication(g graph.GraphRepository) *Application {
	return &Application{
		Graph:     g,
		Index:     NewCodeIndexService(g),
		Code:      NewCodeGraphService(g),
		Evidence:  NewEvidenceService(g),
		Reverse:   NewReverseEngineeringService(g),
		Analysis:  NewAnalysisService(g),
		Memory:    NewMemoryService(g),
		Documents: NewDocumentService(g),
		Git:       NewGitService(g),
	}
}

func ApplicationFromConfig(cfg *config.Config) (*Application, error) {
	var g graph.GraphRepository
	if cfg.Storage == "sqlite" || cfg.Storage == "" {
		dbPath := cfg.Database.Path
		if dbPath == "" {
			dbPath = ".codergag.db"
		}
		repo, err := graph.NewPersistentRepository(dbPath)
		if err != nil {
			return nil, err
		}
		g = repo
	} else {
		g = graph.NewMemoryGraphRepository()
	}
	return NewApplication(g), nil
}

func ApplicationInMemory() *Application {
	return NewApplication(graph.NewMemoryGraphRepository())
}
