package services

import (
	"os"
	"path/filepath"
	"strings"

	"codergag/internal/cache"
	"codergag/internal/config"
	"codergag/internal/graph"
)

type Application struct {
	Graph     graph.GraphRepository
	Cache     *cache.CacheManager
	Events    *EventEngine
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
	Daemon    *Daemon
}

// IndexProgress returns the most recent index-progress snapshot for a project.
func (a *Application) IndexProgress(projectID string) *IndexProgress {
	if a.Index == nil {
		return nil
	}
	return a.Index.Progress(projectID)
}

func NewApplication(g graph.GraphRepository) *Application {
	repoKind := "Project"
	_ = repoKind
	sec := NewSecurityAuditService(g)
	refs := NewReferenceResolver(g)
	ev := NewEvidenceService(g)
	app := &Application{
		Graph:     g,
		Events:    NewEventEngine(1024),
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
		if dbPath == "~" || strings.HasPrefix(dbPath, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				dbPath = filepath.Join(home, strings.TrimPrefix(dbPath, "~"))
			}
		}
		repo, err := graph.NewPersistentRepositoryWithCache(dbPath, cfg.Graph.CacheNodes, cfg.Graph.CacheEdges)
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
	app.Events = NewEventEngine(cfg.Watch.QueueSize)
	app.Daemon = NewDaemon(app, daemonConfigFromConfig(*cfg))
	if cfg.Watch.Enabled {
		app.Daemon.Start()
	}
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

func daemonConfigFromConfig(cfg config.Config) DaemonConfig {
	roots := append([]string(nil), cfg.Watch.ProjectRoots...)
	if len(roots) == 0 {
		for _, project := range cfg.Projects {
			if project.Path != "" {
				roots = append(roots, project.Path)
			}
		}
	}
	if len(roots) == 0 {
		roots = []string{"."}
	}
	return DaemonConfig{
		ProjectDetectInterval: cfg.Watch.IntervalDuration(),
		IndexInterval:         cfg.Watch.IndexIntervalDuration(),
		IndexOnChange:         cfg.Watch.IndexOnChange,
		MaxBackgroundJobs:     cfg.Watch.MaxWorkers,
		MaxWatchDirs:          cfg.Watch.MaxWatchDirs,
		MaxDirtyFiles:         cfg.Watch.MaxDirtyFiles,
		ProjectScanDepth:      cfg.Watch.ProjectScanDepth,
		Roots:                 roots,
		IndexIncremental:      cfg.Indexing.Incremental,
		IndexIgnore:           cfg.Indexing.Ignore,
		SkipGraphify:          cfg.Indexing.SkipGraphify,
	}
}

func ApplicationInMemory() *Application {
	return NewApplication(graph.NewMemoryGraphRepository())
}
