package services

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type ObserverConfig struct {
	Debounce   time.Duration
	Ignore     []string
	MaxPending int
}

type debounceEntry struct {
	op fsnotify.Op
	at time.Time
}

type Observer struct {
	root       string
	projectID  string
	engine     *EventEngine
	maxDirs    int
	debounce   time.Duration
	ignore     []string
	maxPending int

	mu       sync.Mutex
	watcher  *fsnotify.Watcher
	watched  map[string]bool
	pending  map[string]debounceEntry
	stopCh   chan struct{}
	doneCh   chan struct{}
	stopOnce sync.Once
	started  bool
}

func NewObserver(root, projectID string, engine *EventEngine, maxDirs int) *Observer {
	return NewObserverWithConfig(root, projectID, engine, ObserverConfig{
		Debounce:   500 * time.Millisecond,
		MaxPending: 4096,
		Ignore:     nil,
	}, maxDirs)
}

func NewObserverWithConfig(root, projectID string, engine *EventEngine, cfg ObserverConfig, maxDirs int) *Observer {
	if maxDirs <= 0 {
		maxDirs = 256
	}
	if cfg.Debounce < 0 {
		cfg.Debounce = 500 * time.Millisecond
	}
	if cfg.Debounce == 0 {
		cfg.Debounce = 500 * time.Millisecond
	}
	if cfg.MaxPending <= 0 {
		cfg.MaxPending = 4096
	}
	return &Observer{
		root:       root,
		projectID:  projectID,
		engine:     engine,
		maxDirs:    maxDirs,
		debounce:   cfg.Debounce,
		ignore:     append([]string(nil), cfg.Ignore...),
		maxPending: cfg.MaxPending,
		watched:    make(map[string]bool),
		pending:    make(map[string]debounceEntry),
	}
}

func (o *Observer) Start() error {
	o.mu.Lock()
	if o.started {
		o.mu.Unlock()
		return nil
	}
	root, err := filepath.Abs(o.root)
	if err != nil {
		o.mu.Unlock()
		return err
	}
	if evaluated, err := filepath.EvalSymlinks(root); err == nil {
		root = evaluated
	}
	o.root = root
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		o.mu.Unlock()
		return err
	}
	if err := o.watchDirLocked(watcher, root); err != nil {
		_ = watcher.Close()
		o.mu.Unlock()
		return err
	}
	o.watcher = watcher
	o.stopCh = make(chan struct{})
	o.doneCh = make(chan struct{})
	o.started = true
	o.mu.Unlock()
	go o.loop()
	return nil
}

func (o *Observer) Stop() {
	o.stopOnce.Do(func() {
		o.mu.Lock()
		if o.stopCh != nil {
			close(o.stopCh)
		}
		watcher := o.watcher
		done := o.doneCh
		started := o.started
		o.mu.Unlock()
		if watcher != nil {
			_ = watcher.Close()
		}
		if done != nil {
			<-done
		}
		// Wait for loop to finish if it was started
		if started {
			<-done
		}
	})
}

func (o *Observer) WatchedDirs() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.watched)
}

func (o *Observer) loop() {
	defer func() {
		o.mu.Lock()
		close(o.doneCh)
		o.mu.Unlock()
	}()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-o.stopCh:
			o.flush(false, time.Now())
			return
		case event, ok := <-o.watcher.Events:
			if !ok {
				o.flush(false, time.Now())
				return
			}
			o.handleEvent(event)
		case <-o.watcher.Errors:
			o.flush(false, time.Now())
			return
		case now := <-ticker.C:
			o.flush(true, now)
		}
	}
}

func (o *Observer) handleEvent(event fsnotify.Event) {
	if isIgnoredPath(o.root, event.Name, o.ignore) {
		return
	}
	o.mu.Lock()
	entry, exists := o.pending[event.Name]
	entry.op |= event.Op
	entry.at = time.Now()
	var evictPath string
	var evictOp fsnotify.Op
	evicted := false
	if !exists && len(o.pending) >= o.maxPending {
		// Overflow: emit the oldest pending change now (ties broken by path)
		// rather than silently dropping it.
		var oldest time.Time
		for path, e := range o.pending {
			if !evicted || e.at.Before(oldest) || (e.at.Equal(oldest) && path < evictPath) {
				evictPath, evictOp, oldest, evicted = path, e.op, e.at, true
			}
		}
		delete(o.pending, evictPath)
	}
	o.pending[event.Name] = entry
	o.mu.Unlock()
	if evicted {
		o.emitForPath(evictPath, evictOp)
	}
}

func (o *Observer) flush(tick bool, now time.Time) {
	o.mu.Lock()
	type pendingChange struct {
		path string
		op   fsnotify.Op
	}
	changes := make([]pendingChange, 0, len(o.pending))
	for path, entry := range o.pending {
		if !tick || now.Sub(entry.at) >= o.debounce {
			changes = append(changes, pendingChange{path: path, op: entry.op})
			delete(o.pending, path)
		}
	}
	o.mu.Unlock()
	for _, change := range changes {
		o.emitForPath(change.path, change.op)
	}
}

func (o *Observer) emitForPath(path string, op fsnotify.Op) {
	if isIgnoredPath(o.root, path, o.ignore) {
		return
	}
	info, err := osStat(path)
	if err != nil {
		o.emitFileEvent(path, deleteKind(op), op)
		return
	}
	if info.IsDir() {
		o.mu.Lock()
		watcher := o.watcher
		started := o.started
		o.mu.Unlock()
		if !started || watcher == nil {
			return
		}
		if op&(fsnotify.Create|fsnotify.Rename) != 0 {
			o.mu.Lock()
			_ = o.watchDirLocked(watcher, path)
			o.mu.Unlock()
		}
		if op&(fsnotify.Remove|fsnotify.Rename) != 0 {
			o.removeWatchedUnder(path)
		}
		return
	}
	o.emitFileEvent(path, fileKind(op), op)
}

func (o *Observer) emitFileEvent(path string, kind EventKind, op fsnotify.Op) {
	if kind == "" || !isCodeExt(filepath.Ext(path)) {
		return
	}
	if o.engine == nil {
		return
	}
	o.engine.Emit(Event{
		Kind:      kind,
		ProjectID: o.projectID,
		Payload: map[string]any{
			"file": path,
			"path": path,
			"op":   op.String(),
		},
	})
}

func (o *Observer) watchDirLocked(watcher *fsnotify.Watcher, dir string) error {
	clean, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if evaluated, err := filepath.EvalSymlinks(clean); err == nil {
		clean = evaluated
	}
	if o.watched[clean] {
		return nil
	}
	if len(o.watched) >= o.maxDirs {
		return nil
	}
	if err := watcher.Add(clean); err != nil {
		return err
	}
	o.watched[clean] = true
	entries, err := safeReadDir(clean)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || isIgnoreDir(entry.Name()) || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		child := filepath.Join(clean, entry.Name())
		if isIgnoredPath(o.root, child, o.ignore) {
			continue
		}
		if len(o.watched) >= o.maxDirs {
			return nil
		}
		if err := o.watchDirLocked(watcher, child); err != nil {
			return err
		}
	}
	return nil
}

func (o *Observer) removeWatchedUnder(root string) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return
	}
	o.mu.Lock()
	watcher := o.watcher
	paths := make([]string, 0, len(o.watched))
	for watched := range o.watched {
		if watched == absRoot || strings.HasPrefix(watched, absRoot+string(filepath.Separator)) {
			paths = append(paths, watched)
		}
	}
	for _, path := range paths {
		delete(o.watched, path)
	}
	o.mu.Unlock()
	if watcher != nil {
		for _, path := range paths {
			_ = watcher.Remove(path)
		}
	}
}

func deleteKind(op fsnotify.Op) EventKind {
	if op&fsnotify.Rename != 0 {
		return FileRenamed
	}
	if op&fsnotify.Remove != 0 {
		return FileDeleted
	}
	return ""
}

func fileKind(op fsnotify.Op) EventKind {
	switch {
	case op&fsnotify.Remove != 0:
		return FileDeleted
	case op&fsnotify.Rename != 0:
		return FileRenamed
	case op&fsnotify.Create != 0:
		return FileCreated
	case op&fsnotify.Write != 0, op&fsnotify.Chmod != 0:
		return FileModified
	default:
		return ""
	}
}

func isIgnoredPath(root, path string, ignore []string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, part := range parts {
		if isIgnoreDir(part) {
			return true
		}
		for _, pattern := range ignore {
			if pattern == "" {
				continue
			}
			if matched, _ := filepath.Match(pattern, part); matched {
				return true
			}
			if strings.Contains(pattern, "/") {
				if matched, _ := filepath.Match(pattern, filepath.ToSlash(rel)); matched {
					return true
				}
			}
		}
	}
	return false
}

func osStat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

func safeReadDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}

const osModeSymlink = os.ModeSymlink
