package services

import (
	"sort"
	"sync"
	"time"

	"codergag/internal/graph"
)

type HotContext struct {
	Files      []*HotFile
	Symbols    []*HotSymbol
	LastUpdated time.Time
	AccessCount int
}

type HotFile struct {
	ID           string `json:"id"`
	Path         string `json:"path"`
	Language     string `json:"language,omitempty"`
	Hash         string `json:"hash,omitempty"`
	LastAccessed string `json:"last_accessed"`
	AccessCount  int    `json:"access_count"`
	IsStale      bool   `json:"is_stale,omitempty"`
}

type HotSymbol struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	FileID       string `json:"file_id"`
	FilePath     string `json:"file_path"`
	LineStart    int    `json:"line_start"`
	LineEnd      int    `json:"line_end"`
	Signature    string `json:"signature,omitempty"`
	LastAccessed string `json:"last_accessed"`
	AccessCount  int    `json:"access_count"`
	Callers      []string `json:"callers,omitempty"`
	Callees      []string `json:"callees,omitempty"`
	IsStale      bool    `json:"is_stale,omitempty"`
}

type HotContextCache struct {
	mu        sync.RWMutex
	projectID string
	hot       *HotContext
	graph     graph.GraphRepository
	maxItems  int
}

func NewHotContextCache(projectID string, g graph.GraphRepository) *HotContextCache {
	return &HotContextCache{
		projectID: projectID,
		hot:       &HotContext{},
		graph:     g,
		maxItems:  100,
	}
}

func (hc *HotContextCache) AddFile(path, language, hash string) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)

	for _, f := range hc.hot.Files {
		if f.Path == path {
			f.AccessCount++
			f.LastAccessed = now
			f.Hash = hash
			return
		}
	}

	hc.hot.Files = append(hc.hot.Files, &HotFile{
		ID:           "file_" + hash[:min(8, len(hash))],
		Path:         path,
		Language:     language,
		Hash:         hash,
		LastAccessed: now,
		AccessCount:  1,
	})
	hc.trimFiles()
	hc.hot.LastUpdated = time.Now()
	hc.hot.AccessCount++
}

func (hc *HotContextCache) AddSymbol(id, name, kind, fileID, filePath string, lineStart, lineEnd int, signature string) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)

	for _, s := range hc.hot.Symbols {
		if s.ID == id {
			s.AccessCount++
			s.LastAccessed = now
			return
		}
	}

	hc.hot.Symbols = append(hc.hot.Symbols, &HotSymbol{
		ID:           id,
		Name:         name,
		Kind:         kind,
		FileID:       fileID,
		FilePath:     filePath,
		LineStart:    lineStart,
		LineEnd:      lineEnd,
		Signature:    signature,
		LastAccessed: now,
		AccessCount:  1,
	})
	hc.trimSymbols()
	hc.hot.LastUpdated = time.Now()
	hc.hot.AccessCount++
}

func (hc *HotContextCache) GetFilesByAccess() []*HotFile {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	files := make([]*HotFile, len(hc.hot.Files))
	copy(files, hc.hot.Files)

	sort.Slice(files, func(i, j int) bool {
		return files[i].AccessCount > files[j].AccessCount
	})
	return files
}

func (hc *HotContextCache) GetSymbolsByAccess() []*HotSymbol {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	symbols := make([]*HotSymbol, len(hc.hot.Symbols))
	copy(symbols, hc.hot.Symbols)

	sort.Slice(symbols, func(i, j int) bool {
		return symbols[i].AccessCount > symbols[j].AccessCount
	})
	return symbols
}

func (hc *HotContextCache) GetHotContext() *HotContext {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return &HotContext{
		Files:        hc.hot.Files,
		Symbols:      hc.hot.Symbols,
		LastUpdated:  hc.hot.LastUpdated,
		AccessCount: hc.hot.AccessCount,
	}
}

func (hc *HotContextCache) InvalidateStale(staleThreshold time.Duration) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-staleThreshold)

	for _, f := range hc.hot.Files {
		lastAcc, _ := time.Parse(time.RFC3339, f.LastAccessed)
		if lastAcc.Before(cutoff) {
			f.IsStale = true
		}
	}

	for _, s := range hc.hot.Symbols {
		lastAcc, _ := time.Parse(time.RFC3339, s.LastAccessed)
		if lastAcc.Before(cutoff) {
			s.IsStale = true
		}
	}
	hc.hot.LastUpdated = now
}

func (hc *HotContextCache) RefreshFromGraph() error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	nodes, err := hc.graph.FindNodes("SourceFile", map[string]any{"project_id": hc.projectID})
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)

	for _, n := range nodes {
		path := strProp(n, "path")
		hash := strProp(n, "hash")
		lang := strProp(n, "language")

		existing := false
		for _, f := range hc.hot.Files {
			if f.Path == path {
				existing = true
				if f.Hash != hash {
					f.IsStale = true
				}
				break
			}
		}

		if !existing {
			hc.hot.Files = append(hc.hot.Files, &HotFile{
				ID:           n.ID,
				Path:         path,
				Language:     lang,
				Hash:         hash,
				LastAccessed: now,
				AccessCount:  0,
			})
		}
	}
	hc.hot.LastUpdated = time.Now()
	return nil
}

func (hc *HotContextCache) MergeFrom(snapshot *ProjectSnapshot) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if snapshot == nil {
		return
	}

	if snapshot.FrequentlyAccessed != nil {
		for _, fa := range snapshot.FrequentlyAccessed {
			if fa.EntityKind == "file" {
				hc.hot.Files = append(hc.hot.Files, &HotFile{
					ID:           fa.EntityID,
					Path:         fa.EntityID,
					AccessCount:  fa.Count,
					LastAccessed: fa.LastSeen,
				})
			}
		}
	}

	if snapshot.RecentErrors != nil {
		for _, re := range snapshot.RecentErrors {
			hc.hot.Symbols = append(hc.hot.Symbols, &HotSymbol{
				ID:   re.ID,
				Name: re.Message,
			})
		}
	}
	hc.hot.LastUpdated = time.Now()
}

func (hc *HotContextCache) trimFiles() {
	max := hc.maxItems
	if len(hc.hot.Files) <= max {
		return
	}
	sort.Slice(hc.hot.Files, func(i, j int) bool {
		return hc.hot.Files[i].AccessCount > hc.hot.Files[j].AccessCount
	})
	hc.hot.Files = hc.hot.Files[:max]
}

func (hc *HotContextCache) trimSymbols() {
	max := hc.maxItems
	if len(hc.hot.Symbols) <= max {
		return
	}
	sort.Slice(hc.hot.Symbols, func(i, j int) bool {
		return hc.hot.Symbols[i].AccessCount > hc.hot.Symbols[j].AccessCount
	})
	hc.hot.Symbols = hc.hot.Symbols[:max]
}
