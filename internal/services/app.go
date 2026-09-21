package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codergag/internal/autoupdate"
	"codergag/internal/cache"
	"codergag/internal/config"
	"codergag/internal/graph"
	"codergag/internal/security"
	"codergag/internal/selfupdate"
)

type Application struct {
	Graph     graph.GraphRepository
	Cache     *cache.CacheManager
	Events    *EventEngine
	Index     *CodeIndexService
	Code      *CodeGraphService
	Evidence  *EvidenceService
	Reverse   *ReverseEngineeringService
	Analysis   *AnalysisService
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
	AntiLoop  *AntiLoopDetector
	SmallModel *SmallModelService
}

// IndexProgress returns the most recent index-progress snapshot for a project.
func (a *Application) IndexProgress(projectID string) *IndexProgress {
	if a.Index == nil {
		return nil
	}
	return a.Index.Progress(projectID)
}

func NewApplication(g graph.GraphRepository) *Application {
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
	app.AntiLoop = NewAntiLoopDetector(g)
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
	dbType := cfg.Database.Type
	if dbType == "" {
		// Backward compat: Storage field was the old config key.
		if cfg.Storage == "sqlite" || cfg.Storage == "" {
			dbType = "sqlite"
		} else {
			dbType = "memory"
		}
	}

	switch dbType {
	case config.DatabaseSQLite, "":
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

	case config.DatabasePostgres:
		dsn := cfg.Database.DSN
		if dsn == "" {
			dsn = buildPostgresDSN(cfg.Database)
		}
		repo, err := graph.NewSQLGraphRepository(context.Background(), dsn)
		if err != nil {
			return nil, err
		}
		g = repo

	case config.DatabaseMongo:
		dsn := cfg.Database.DSN
		if dsn == "" {
			dsn = buildMongoDSN(cfg.Database)
		}
		repo, err := graph.NewMongoGraphRepository(context.Background(), dsn, cfg.Projects[0].ID)
		if err != nil {
			return nil, err
		}
		g = repo

	case config.DatabaseMemory:
		g = graph.NewMemoryGraphRepository()

	default:
		g = graph.NewMemoryGraphRepository()
	}

	var cm *cache.CacheManager
	if cfg.Cache.Enabled {
		cm = cache.NewCacheManager(g, cfg.Cache)
	}
	app := NewApplicationWithCache(g, cm)
	app.Events = NewEventEngine(cfg.Watch.QueueSize)
	app.Daemon = NewDaemon(app, daemonConfigFromConfig(*cfg))
	app.SmallModel = NewSmallModelService(cfg.LLM)
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

	// Start automatic pattern database updates
	if cfg.Security.AutoUpdate {
		interval := cfg.Security.UpdateIntervalDuration()
		if cfg.Security.PatternSource != "" {
			security.PatternSource = cfg.Security.PatternSource
		}
		security.PatternUpdateInterval = interval
		_ = security.CheckPatternUpdate()
		security.StartAutoUpdater(interval)
	}

	// Start binary auto-updater
	au := autoupdate.NewAutoUpdater(autoupdate.Config{
		Enabled:        true,
		CheckInterval:  24 * time.Hour,
		Repo:           "kilocode-org/codergag",
		CurrentVersion: cfg.Version,
		BinaryPath:     "",
		NotifyOnly:     true,
	})
	au.SetNotifier(func(info *selfupdate.UpdateInfo) {
		fmt.Fprintf(os.Stderr, "\n\033[33mUpdate available: %s -> %s\033[0m\n", info.CurrentVersion, info.LatestVersion)
		fmt.Fprintf(os.Stderr, "Run 'codergag update' to install\n\n")
	})
	au.Start()

	return app, nil
}

func buildPostgresDSN(cfg config.DatabaseConfig) string {
	if cfg.DSN != "" {
		return cfg.DSN
	}
	password := cfg.Password
	if password != "" {
		password = ":" + password + "@"
	}
	port := cfg.Port
	if port == 0 {
		port = 5432
	}
	return fmt.Sprintf("postgres://%s%s@%s:%d/%s?sslmode=disable",
		cfg.User, password, cfg.Host, port, cfg.Database)
}

func buildMongoDSN(cfg config.DatabaseConfig) string {
	if cfg.DSN != "" {
		return cfg.DSN
	}
	port := cfg.Port
	if port == 0 {
		port = 27017
	}
	auth := ""
	if cfg.User != "" {
		auth = cfg.User
		if cfg.Password != "" {
			auth += ":" + cfg.Password
		}
		auth += "@"
	}
	return fmt.Sprintf("mongodb://%s%s:%d/%s", auth, cfg.Host, port, cfg.Database)
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
