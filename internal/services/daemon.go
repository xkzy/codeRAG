package services

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type DaemonConfig struct {
	ProjectDetectInterval time.Duration
	IndexInterval         time.Duration
	IndexOnChange         bool
	MaxBackgroundJobs     int
	MaxWatchDirs          int
	ProjectScanDepth      int
	IdleTimeout           time.Duration
	Roots                 []string
	IndexIncremental      bool
	IndexIgnore           []string
}

type Daemon struct {
	app      *Application
	cfg      DaemonConfig
	detector *ProjectDetector
	sem      chan struct{}

	startMu  sync.Mutex
	mu       sync.RWMutex
	running  bool
	ctx      context.Context
	cancel   context.CancelFunc
	projects map[string]*ProjectWorker
	jobs     atomic.Int64
}

func NewDaemon(app *Application, cfg DaemonConfig) *Daemon {
	if cfg.ProjectDetectInterval == 0 {
		cfg.ProjectDetectInterval = 30 * time.Second
	}
	if cfg.IndexInterval == 0 {
		cfg.IndexInterval = 5 * time.Minute
	}
	if cfg.MaxBackgroundJobs <= 0 {
		cfg.MaxBackgroundJobs = 4
	}
	if cfg.MaxWatchDirs <= 0 {
		cfg.MaxWatchDirs = 256
	}
	if cfg.ProjectScanDepth <= 0 {
		cfg.ProjectScanDepth = 3
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 30 * time.Minute
	}
	if len(cfg.Roots) == 0 {
		cfg.Roots = []string{"."}
	}
	if cfg.IndexIgnore == nil {
		cfg.IndexIgnore = []string{}
	}
	return &Daemon{
		app:      app,
		cfg:      cfg,
		sem:      make(chan struct{}, cfg.MaxBackgroundJobs),
		projects: map[string]*ProjectWorker{},
	}
}

func (d *Daemon) Start() {
	d.startMu.Lock()
	defer d.startMu.Unlock()
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.ctx = ctx
	d.cancel = cancel
	d.running = true
	d.detector = NewProjectDetector(d.cfg.Roots, d.cfg.ProjectScanDepth, d.app.Events)
	d.mu.Unlock()

	d.detectProjects(ctx)
	go d.detectLoop(ctx)
	go d.maintenanceLoop(ctx)
}

func (d *Daemon) Stop() {
	d.startMu.Lock()
	defer d.startMu.Unlock()
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return
	}
	cancel := d.cancel
	projects := make([]*ProjectWorker, 0, len(d.projects))
	for _, project := range d.projects {
		projects = append(projects, project)
	}
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	for _, project := range projects {
		project.Stop()
	}
	d.mu.Lock()
	d.running = false
	d.detector = nil
	d.mu.Unlock()
}

func (d *Daemon) Status() map[string]any {
	d.mu.RLock()
	defer d.mu.RUnlock()
	queue := 0
	if d.app != nil && d.app.Events != nil {
		queue = d.app.Events.QueueLength()
	}
	active := 0
	for _, project := range d.projects {
		if project.IsActive() {
			active++
		}
	}
	return map[string]any{
		"running":         d.running,
		"projects":        len(d.projects),
		"active_agents":   active,
		"background_jobs": d.jobs.Load(),
		"max_workers":     d.cfg.MaxBackgroundJobs,
		"event_queue":     queue,
	}
}

func (d *Daemon) Projects() []*ProjectIdentity {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]*ProjectIdentity, 0, len(d.projects))
	for _, project := range d.projects {
		out = append(out, &ProjectIdentity{
			ID:   project.projectID,
			Root: project.root,
			Name: filepath.Base(project.root),
		})
	}
	return out
}

func (d *Daemon) detectLoop(ctx context.Context) {
	ticker := time.NewTicker(d.cfg.ProjectDetectInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.detectProjects(ctx)
		}
	}
}

func (d *Daemon) detectProjects(ctx context.Context) {
	d.mu.RLock()
	detector := d.detector
	d.mu.RUnlock()
	if detector == nil {
		return
	}
	for _, project := range detector.Detect() {
		select {
		case <-ctx.Done():
			return
		default:
			d.registerProject(project)
		}
	}
}

func (d *Daemon) registerProject(project *ProjectIdentity) {
	d.mu.Lock()
	if d.projects[project.ID] != nil {
		d.mu.Unlock()
		return
	}
	worker := NewProjectWorker(d.app, project.ID, project.Root, d.cfg, d.sem)
	worker.jobs = &d.jobs
	d.projects[project.ID] = worker
	d.mu.Unlock()
	worker.Start()
}

func (d *Daemon) maintenanceLoop(ctx context.Context) {
	ticker := time.NewTicker(d.cfg.IndexInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.maintenance()
		}
	}
}

func (d *Daemon) maintenance() {
	d.mu.RLock()
	projects := make([]*ProjectWorker, 0, len(d.projects))
	for _, project := range d.projects {
		projects = append(projects, project)
	}
	d.mu.RUnlock()
	for _, project := range projects {
		project.RunMaintenance()
	}
}

func (d *Daemon) AddProject(root string) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return
	}
	d.registerProject(&ProjectIdentity{
		ID:   stableProjectID(filepath.Clean(abs)),
		Root: filepath.Clean(abs),
		Name: filepath.Base(abs),
	})
}

type ProjectWorker struct {
	app       *Application
	projectID string
	root      string
	cfg       DaemonConfig
	sem       chan struct{}
	jobs      *atomic.Int64
	observer  *Observer

	stopMu    sync.Mutex
	stopCh    chan struct{}
	stopOnce  sync.Once
	mu        sync.RWMutex
	active    bool
	lastSeen  time.Time
	indexing  bool
	pending   bool
	lastError string
	dirty     map[string]bool
	timer     *time.Timer
}

func NewProjectWorker(app *Application, projectID, root string, cfg DaemonConfig, sem chan struct{}) *ProjectWorker {
	return &ProjectWorker{
		app:       app,
		projectID: projectID,
		root:      root,
		cfg:       cfg,
		sem:       sem,
		stopCh:    make(chan struct{}),
		dirty:     make(map[string]bool),
	}
}

func (p *ProjectWorker) Start() {
	if p.app != nil && p.app.Events != nil {
		p.app.Events.AddWorker(p)
	}
	if p.app != nil && p.app.Events != nil {
		observer := NewObserverWithConfig(p.root, p.projectID, p.app.Events, ObserverConfig{
			Debounce:   500 * time.Millisecond,
			MaxPending: 4096,
			Ignore:     p.cfg.IndexIgnore,
		}, p.cfg.MaxWatchDirs)
		p.observer = observer
		if err := observer.Start(); err != nil {
			p.setError(err)
		}
	}
	p.RunMaintenance()
}

func (p *ProjectWorker) Stop() {
	p.stopOnce.Do(func() {
		close(p.stopCh)
		p.mu.Lock()
		if p.timer != nil {
			p.timer.Stop()
			p.timer = nil
		}
		p.mu.Unlock()
		if p.observer != nil {
			p.observer.Stop()
		}
		if p.app != nil && p.app.Events != nil {
			p.app.Events.RemoveWorker(p)
		}
	})
}

func (p *ProjectWorker) OnEvent(event Event) {
	if event.ProjectID != "" && event.ProjectID != p.projectID {
		return
	}
	switch event.Kind {
	case FileCreated, FileModified, FileDeleted, FileRenamed:
		p.markActive()
		if p.cfg.IndexOnChange {
			// Track the changed file for incremental indexing
			if file, ok := event.Payload["file"].(string); ok && file != "" {
				p.mu.Lock()
				p.dirty[file] = true
				p.mu.Unlock()
			}
			p.RunMaintenance()
		}
	}
}

func (p *ProjectWorker) RunMaintenance() {
	p.mu.Lock()
	if p.indexing || p.pending {
		p.pending = true
		p.mu.Unlock()
		return
	}
	p.pending = true
	p.mu.Unlock()
	go p.runMaintenance()
}

func (p *ProjectWorker) runMaintenance() {
	for {
		p.mu.Lock()
		if !p.pending {
			p.mu.Unlock()
			return
		}
		// Collect dirty files before clearing
		var dirtyFiles []string
		if p.cfg.IndexIncremental && len(p.dirty) > 0 {
			for f := range p.dirty {
				dirtyFiles = append(dirtyFiles, f)
			}
			// Clear dirty set
			p.dirty = make(map[string]bool)
		}
		p.pending = false
		p.indexing = true
		p.mu.Unlock()

		select {
		case p.sem <- struct{}{}:
		case <-p.stopCh:
			p.mu.Lock()
			p.indexing = false
			p.mu.Unlock()
			return
		}
		if p.app != nil && p.app.Index != nil {
			var err error
			if len(dirtyFiles) > 0 {
				_, err = p.app.Index.IndexFiles(p.projectID, p.root, dirtyFiles, true, p.cfg.IndexIgnore)
			} else {
				_, err = p.app.Index.IndexRepository(p.projectID, p.root, p.cfg.IndexIncremental, p.cfg.IndexIgnore)
			}
			p.setError(err)
		}
		select {
		case <-p.sem:
		default:
		}

		p.mu.Lock()
		p.indexing = false
		again := p.pending
		p.pending = false
		p.mu.Unlock()
		if !again {
			return
		}
	}
}

func (p *ProjectWorker) restoreDirty(files []string) {
	p.mu.Lock()
	for _, f := range files {
		p.dirty[f] = true
	}
	p.mu.Unlock()
}

func (p *ProjectWorker) markActive() {
	p.mu.Lock()
	p.active = true
	p.lastSeen = time.Now()
	p.mu.Unlock()
}

func (p *ProjectWorker) IsActive() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.active {
		return false
	}
	return time.Since(p.lastSeen) < p.cfg.IdleTimeout
}

func (p *ProjectWorker) LastSeen() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastSeen
}

func (p *ProjectWorker) LastError() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastError
}

func (p *ProjectWorker) setError(err error) {
	p.mu.Lock()
	if err != nil {
		p.lastError = err.Error()
	} else {
		p.lastError = ""
	}
	p.mu.Unlock()
}
