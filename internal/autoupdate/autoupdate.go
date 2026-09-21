package autoupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"codergag/internal/selfupdate"
)

const (
	DefaultCheckInterval = 24 * time.Hour
	DefaultCacheFile     = "autoupdate_cache.json"
)

type Config struct {
	Enabled        bool
	CheckInterval  time.Duration
	Repo           string
	CacheDir       string
	CurrentVersion string
	BinaryPath     string
	NotifyOnly     bool // If true, only notify; don't auto-install
}

type CacheEntry struct {
	LastCheck     time.Time `json:"last_check"`
	LatestVersion string    `json:"latest_version"`
	ReleaseNotes  string    `json:"release_notes"`
	DownloadURL   string    `json:"download_url"`
	AssetName     string    `json:"asset_name"`
	AssetSize     int64     `json:"asset_size"`
}

type Notifier func(info *selfupdate.UpdateInfo)

type AutoUpdater struct {
	cfg       Config
	cachePath string
	mu        sync.Mutex
	notifier  Notifier
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

func NewAutoUpdater(cfg Config) *AutoUpdater {
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = DefaultCheckInterval
	}
	if cfg.CacheDir == "" {
		home, _ := os.UserHomeDir()
		cfg.CacheDir = filepath.Join(home, ".codergag")
	}
	if cfg.BinaryPath == "" {
		bp, _ := selfupdate.GetCurrentBinaryPath()
		cfg.BinaryPath = bp
	}
	
	cachePath := filepath.Join(cfg.CacheDir, DefaultCacheFile)
	
	return &AutoUpdater{
		cfg:       cfg,
		cachePath: cachePath,
		stopCh:    make(chan struct{}),
	}
}

func (a *AutoUpdater) SetNotifier(fn Notifier) {
	a.notifier = fn
}

func (a *AutoUpdater) Start() {
	if !a.cfg.Enabled {
		return
	}
	
	a.wg.Add(1)
	go a.run()
}

func (a *AutoUpdater) Stop() {
	close(a.stopCh)
	a.wg.Wait()
}

func (a *AutoUpdater) run() {
	defer a.wg.Done()
	
	// Initial check after short delay
	select {
	case <-time.After(10 * time.Second):
		a.checkAndNotify()
	case <-a.stopCh:
		return
	}
	
	ticker := time.NewTicker(a.cfg.CheckInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			a.checkAndNotify()
		case <-a.stopCh:
			return
		}
	}
}

func (a *AutoUpdater) checkAndNotify() {
	a.mu.Lock()
	defer a.mu.Unlock()
	
	// Load cache to avoid excessive API calls
	cache := a.loadCache()
	if cache != nil && time.Since(cache.LastCheck) < a.cfg.CheckInterval {
		// Use cached info if recent
		if cache.LatestVersion != "" && cache.LatestVersion != a.cfg.CurrentVersion {
			a.notifyFromCache(cache)
		}
		return
	}
	
	// Check for update
	info, err := selfupdate.GetUpdateInfo(a.cfg.CurrentVersion, a.cfg.Repo)
	if err != nil {
		return // Silently ignore errors
	}
	
	if info == nil {
		// Update cache with current version
		a.saveCache(&CacheEntry{
			LastCheck:     time.Now(),
			LatestVersion: a.cfg.CurrentVersion,
		})
		return
	}
	
	// Save to cache
	a.saveCache(&CacheEntry{
		LastCheck:     time.Now(),
		LatestVersion: info.LatestVersion,
		ReleaseNotes:  info.ReleaseNotes,
		DownloadURL:   info.DownloadURL,
		AssetName:     info.Asset.Name,
		AssetSize:     info.Asset.Size,
	})
	
	a.notifyUser(info)
}

func (a *AutoUpdater) notifyFromCache(cache *CacheEntry) {
	if a.notifier != nil {
		info := &selfupdate.UpdateInfo{
			CurrentVersion: a.cfg.CurrentVersion,
			LatestVersion:  cache.LatestVersion,
			ReleaseNotes:   cache.ReleaseNotes,
			DownloadURL:    cache.DownloadURL,
			Asset: &selfupdate.Asset{
				Name: cache.AssetName,
				Size: cache.AssetSize,
			},
		}
		a.notifier(info)
		return
	}
	
	// Default: print to stderr
	fmt.Fprintf(os.Stderr, "\n\033[33mUpdate available: %s -> %s\033[0m\n", a.cfg.CurrentVersion, cache.LatestVersion)
	fmt.Fprintf(os.Stderr, "Run '%s update' to install\n\n", filepath.Base(a.cfg.BinaryPath))
}

func (a *AutoUpdater) notifyUser(info *selfupdate.UpdateInfo) {
	if a.notifier != nil {
		a.notifier(info)
		return
	}
	
	fmt.Fprintf(os.Stderr, "\n\033[33mUpdate available: %s -> %s\033[0m\n", info.CurrentVersion, info.LatestVersion)
	if info.ReleaseNotes != "" {
		fmt.Fprintf(os.Stderr, "Release notes:\n%s\n", truncate(info.ReleaseNotes, 500))
	}
	fmt.Fprintf(os.Stderr, "Run '%s update' to install\n\n", filepath.Base(a.cfg.BinaryPath))
}

func (a *AutoUpdater) loadCache() *CacheEntry {
	data, err := os.ReadFile(a.cachePath)
	if err != nil {
		return nil
	}
	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil
	}
	return &entry
}

func (a *AutoUpdater) saveCache(entry *CacheEntry) {
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(a.cachePath), 0755)
	os.WriteFile(a.cachePath, data, 0644)
}

func (a *AutoUpdater) ForceCheck() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.checkAndNotify()
}

func (a *AutoUpdater) CheckNow(ctx context.Context) (*selfupdate.UpdateInfo, error) {
	return selfupdate.GetUpdateInfo(a.cfg.CurrentVersion, a.cfg.Repo)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}