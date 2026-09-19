package services

import (
	"codergag/internal/cache"
	"codergag/internal/config"
	"codergag/internal/graph"
)

type Application struct {
	Graph     graph.GraphRepository
	Cache     *cache.CacheManager
	Index     *CodeIndexService
	Code      *CodeGraphService
	Evidence  *EvidenceService
	Reverse   *ReverseEngineeringService
	Analysis  *AnalysisService
	Memory    *MemoryService
	Documents *DocumentService
	Git       *GitService
	Security  *SecurityAuditService
	Team      *TeamService
	Refs      *ReferenceResolver
	Tasks     *TaskService
	Context   *ContextCompiler
	Verify    *VerificationRunService
	Privacy   *PrivacyService
}

func NewApplication(g graph.GraphRepository) *Application {
	repoKind := "Project"
	_ = repoKind
	sec := NewSecurityAuditService(g)
	refs := NewReferenceResolver(g)
	ev := NewEvidenceService(g)
	app := &Application{
		Graph:     g,
		Index:     NewCodeIndexService(g),
		Code:      NewCodeGraphService(g),
		Evidence:  ev,
		Reverse:   NewReverseEngineeringService(g),
		Analysis:  NewAnalysisService(g),
		Memory:    NewMemoryService(g),
		Documents: NewDocumentService(g),
		Git:       NewGitService(g),
		Security:  sec,
		Team:      NewTeamService(g),
		Refs:      refs,
		Tasks:     NewTaskService(g, refs, ev),
		Verify:    NewVerificationRunService(DefaultVerificationConfig(), g),
		Privacy:   NewPrivacyService(g),
	}
	app.Context = NewContextCompiler(app)
	return app
}

func NewApplicationWithCache(g graph.GraphRepository, cm *cache.CacheManager) *Application {
	app := NewApplication(g)
	app.Cache = cm
	app.Security.SetCache(cm)
	app.Privacy.SetCache(cm)
	return app
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
	var cm *cache.CacheManager
	if cfg.Cache.Enabled {
		cm = cache.NewCacheManager(g, cfg.Cache)
	}
	app := NewApplicationWithCache(g, cm)
	// Wire verification config from the yaml config (field-by-field to avoid import cycle).
	vc := cfg.Verification
	app.Verify = NewVerificationRunService(VerificationConfig{
		Enabled:            vc.Enabled,
		AllowedCommands:    vc.AllowedCommands,
		AllowedSubcommands: vc.AllowedSubcommands,
		TimeoutSeconds:     vc.TimeoutSeconds,
		MaxOutputBytes:     vc.MaxOutputBytes,
	}, g)
	return app, nil
}

func ApplicationInMemory() *Application {
	return NewApplication(graph.NewMemoryGraphRepository())
}
