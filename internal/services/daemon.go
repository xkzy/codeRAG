package services

import (
	"context"
	"os"
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
	MaxProjects           int
	MaxWatchDirs          int
	ProjectScanDepth      int
	IdleTimeout           time.Duration
	Roots                 []string
	IndexIncremental      bool
	IndexIgnore           []string
	MaxDirtyFiles         int
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
	if cfg.MaxProjects <= 0 {
		cfg.MaxProjects = 32
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
	if cfg.MaxDirtyFiles <= 0 {
		cfg.MaxDirtyFiles = 10000
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
	d.running = false
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	for _, project := range projects {
		project.Stop()
	}
	d.mu.Lock()
	d.detector = nil
	d.projects = map[string]*ProjectWorker{}
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
	d.pruneProjects()
	d.mu.RLock()
	detector := d.detector
	d.mu.RUnlock()
	if detector == nil {
		return
	}
	found := detector.Detect()
	active := make(map[string]bool, len(found))
	for _, project := range found {
		active[project.ID] = true
		select {
		case <-ctx.Done():
			return
		default:
			d.registerProject(project)
		}
	}
	detector.Prune(active)
}

func (d *Daemon) pruneProjects() {
	d.mu.RLock()
	projects := make([]*ProjectWorker, 0, len(d.projects))
	for _, project := range d.projects {
		projects = append(projects, project)
	}
	d.mu.RUnlock()
	var stale []*ProjectWorker
	for _, project := range projects {
		if _, err := os.Stat(project.root); os.IsNotExist(err) {
			stale = append(stale, project)
		}
	}
	if len(stale) == 0 {
		return
	}
	d.mu.Lock()
	for _, project := range stale {
		if d.projects[project.projectID] == project {
			delete(d.projects, project.projectID)
		}
	}
	d.mu.Unlock()
	for _, project := range stale {
		project.Stop()
	}
}

func (d *Daemon) registerProject(project *ProjectIdentity) {
	if info, err := os.Stat(project.Root); err != nil || !info.IsDir() {
		return
	}
	d.mu.Lock()
	if d.projects[project.ID] != nil || len(d.projects) >= d.cfg.MaxProjects {
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
	d.pruneProjects()
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

	stopMu        sync.Mutex
	stopCh        chan struct{}
	stopOnce      sync.Once
	wg            sync.WaitGroup
	stopped       bool
	mu            sync.RWMutex
	active        bool
	lastSeen      time.Time
	indexing      bool
	pending       bool
	dirtyOverflow bool
	lastError     string
	dirty         map[string]bool
	timer         *time.Timer
}

func NewProjectWorker(app *Application, projectID, root string, cfg DaemonConfig, sem chan struct{}) *ProjectWorker {
	if cfg.MaxDirtyFiles <= 0 {
		cfg.MaxDirtyFiles = 10000
	}
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
	p.mu.RLock()
	stopped := p.stopped
	p.mu.RUnlock()
	if stopped {
		return
	}
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
		p.mu.Lock()
		p.stopped = true
		if p.timer != nil {
			p.timer.Stop()
			p.timer = nil
		}
		p.mu.Unlock()
		close(p.stopCh)
		if p.observer != nil {
			p.observer.Stop()
		}
		if p.app != nil && p.app.Events != nil {
			p.app.Events.RemoveWorker(p)
		}
		p.wg.Wait()
	})
}

func (p *ProjectWorker) OnEvent(event Event) {
	p.mu.RLock()
	stopped := p.stopped
	p.mu.RUnlock()
	if stopped || event.ProjectID != "" && event.ProjectID != p.projectID {
		return
	}
	switch event.Kind {
	case FileCreated, FileModified, FileDeleted, FileRenamed:
		p.markActive()
		if p.cfg.IndexOnChange {
			if file, ok := event.Payload["file"].(string); ok && file != "" {
				p.mu.Lock()
				if len(p.dirty) >= p.cfg.MaxDirtyFiles {
					p.dirtyOverflow = true
					p.dirty = make(map[string]bool)
					p.mu.Unlock()
					p.RunMaintenance()
					return
				}
				p.dirty[file] = true
				p.mu.Unlock()
			}
			p.RunMaintenance()
		}
	}
}

func (p *ProjectWorker) RunMaintenance() {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	if p.indexing || p.pending {
		p.pending = true
		p.mu.Unlock()
		return
	}
	p.pending = true
	p.wg.Add(1)
	p.mu.Unlock()
	go func() {
		defer p.wg.Done()
		p.runMaintenance()
	}()
}

func (p *ProjectWorker) runMaintenance() {
	for {
		p.mu.Lock()
		if !p.pending {
			p.mu.Unlock()
			return
		}
		fullIndex := p.dirtyOverflow
		var dirtyFiles []string
		if fullIndex {
			p.dirtyOverflow = false
			p.dirty = make(map[string]bool)
		} else if p.cfg.IndexIncremental && len(p.dirty) > 0 {
			dirtyFiles = make([]string, 0, len(p.dirty))
			for f := range p.dirty {
				dirtyFiles = append(dirtyFiles, f)
			}
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
		if p.jobs != nil {
			p.jobs.Add(1)
		}
		var err error
		if p.app != nil && p.app.Index != nil {
			if fullIndex || len(dirtyFiles) == 0 {
				_, err = p.app.Index.IndexRepository(p.projectID, p.root, p.cfg.IndexIncremental, p.cfg.IndexIgnore)
			} else {
				_, err = p.app.Index.IndexFiles(p.projectID, p.root, dirtyFiles, true, p.cfg.IndexIgnore)
				if err != nil {
					p.restoreDirty(dirtyFiles)
				}
			}
		}
		if p.jobs != nil {
			p.jobs.Add(-1)
		}
		p.setError(err)
		p.saveGraph()
		<-p.sem

		p.mu.Lock()
		p.indexing = false
		again := p.pending && !p.stopped
		p.pending = false
		p.mu.Unlock()
		if !again {
			return
		}
	}
}

func (p *ProjectWorker) saveGraph() {
	if p.app == nil || p.app.Graph == nil {
		return
	}
	saver, ok := p.app.Graph.(interface{ Save() error })
	if !ok {
		return
	}
	if err := saver.Save(); err != nil {
		p.setError(err)
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
	if p.stopped || !p.active {
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
