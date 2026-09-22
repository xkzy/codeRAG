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
	Graph             graph.GraphRepository
	Cache             *cache.CacheManager
	Events            *EventEngine
	Index             *CodeIndexService
	Code              *CodeGraphService
	Evidence          *EvidenceService
	Reverse           *ReverseEngineeringService
	BinaryAnalysis    *BinaryAnalysisService
	BinaryVerification *BinaryVerificationService
	Porting           *PortingService
	Analysis          *AnalysisService
	Memory            *MemoryService
	Documents         *DocumentService
	Git               *GitService
	Security          *SecurityAuditService
	Team              *TeamService
	Refs              *ReferenceResolver
	Tasks             *TaskService
	Context           *ContextCompiler
	Verify            *VerificationRunService
	Privacy           *PrivacyService
	Runtime           *RuntimeService
	Daemon            *Daemon
	AntiLoop          *AntiLoopDetector
	SmallModel        *SmallModelService
	StatusPanel       *StatusPanel
	Session           *SessionManager
	Snapshot          *SnapshotManager
}

// IndexProgress returns the most recent index-progress snapshot for a project.
func (a *Application) IndexProgress(projectID string) *IndexProgress {
	if a.Index == nil {
		return nil
	}
	return a.Index.Progress(projectID)
}

func (a *Application) ResolverFor(projectID string) *ReferenceResolver {
	if a == nil {
		return nil
	}
	if a.Code != nil {
		if idx := a.Code.IndexFor(projectID); idx != nil {
			return NewReferenceResolverWithIndex(a.Graph, idx)
		}
	}
	return a.Refs
}

func (a *Application) BuildProjectSnapshot(projectID string) (*ProjectSnapshot, error) {
	if a.Snapshot == nil {
		return nil, fmt.Errorf("snapshot manager not initialized")
	}
	return a.Snapshot.CreateSnapshot(projectID, a)
}

func NewApplication(g graph.GraphRepository) *Application {
	sec := NewSecurityAuditService(g)
	refs := NewReferenceResolver(g)
	ev := NewEvidenceService(g)
	app := &Application{
		Graph:              g,
		Events:             NewEventEngine(1024),
		Index:              NewCodeIndexService(g),
		Code:               NewCodeGraphService(g),
		Evidence:           ev,
		Reverse:            NewReverseEngineeringService(g),
		BinaryAnalysis:     NewBinaryAnalysisService(g),
		BinaryVerification: NewBinaryVerificationService(g),
		Porting:            NewPortingService(g),
		Analysis:           NewAnalysisService(g),
		Memory:             NewMemoryService(g),
		Documents:          NewDocumentService(g),
		Git:                NewGitService(g),
		Security:           sec,
		Team:               NewTeamService(g),
		Refs:               refs,
		Tasks:              NewTaskService(g, refs, ev),
		Verify:             NewVerificationRunService(DefaultVerificationConfig(), g),
		Privacy:            NewPrivacyService(g),
		Runtime:            NewRuntimeService(g),
	}
	app.StatusPanel = NewStatusPanel(app)
	app.Context = NewContextCompiler(app)
	app.AntiLoop = NewAntiLoopDetector(g)
	app.Snapshot = NewSnapshotManager(g)
	app.Session = NewSessionManager(g, app)
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
		repo, err := newSQLGraphRepository(context.Background(), dsn)
		if err != nil {
			return nil, err
		}
		g = repo

	case config.DatabaseMongo:
		dsn := cfg.Database.DSN
		if dsn == "" {
			dsn = buildMongoDSN(cfg.Database)
		}
		repo, err := newMongoGraphRepository(context.Background(), dsn, cfg.Projects[0].ID)
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
