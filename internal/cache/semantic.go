package cache

import (
	"math"
	"sort"
	"strings"
	"sync"
)

type SemanticCache struct {
	mu      sync.RWMutex
	entries []*CacheEntry
	config  SemanticConfig
}

func NewSemanticCache(cfg SemanticConfig) *SemanticCache {
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 10000
	}
	return &SemanticCache{
		entries: make([]*CacheEntry, 0, cfg.MaxEntries),
		config:  cfg,
	}
}

func (s *SemanticCache) Index(entry *CacheEntry, query string) {
	if !s.config.Enabled || entry == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.Embedding == nil {
		entry.Embedding = embedQuery(query)
	}
	s.entries = append(s.entries, entry)
	if excess := len(s.entries) - s.config.MaxEntries; excess > 0 {
		copy(s.entries, s.entries[excess:])
		s.entries = s.entries[:s.config.MaxEntries]
	}
}

func (s *SemanticCache) Search(query string, projectID, repoID, commit, binaryHash string, maxResults int) *CacheEntry {
	if !s.config.Enabled {
		return nil
	}
	target := embedQuery(query)
	s.mu.RLock()
	defer s.mu.RUnlock()

	type scored struct {
		entry *CacheEntry
		score float64
	}
	var results []scored
	for _, entry := range s.entries {
		if entry.ProjectID != projectID {
			continue
		}
		if entry.RepositoryID != "" && entry.RepositoryID != repoID {
			continue
		}
		if entry.GitCommit != "" && entry.GitCommit != commit {
			continue
		}
		if entry.BinaryHash != "" && entry.BinaryHash != binaryHash {
			continue
		}
		if len(entry.Embedding) == 0 {
			continue
		}
		sim := cosineSimilarity(target, entry.Embedding)
		if sim >= s.config.Threshold {
			results = append(results, scored{entry, sim})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})
	if len(results) == 0 {
		return nil
	}
	return results[0].entry
}

func (s *SemanticCache) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make([]*CacheEntry, 0)
}

func embedQuery(query string) []float64 {
	tokens := tokenize(query)
	vocab := make(map[string]float64)
	for _, t := range tokens {
		vocab[t]++
	}
	magnitude := 0.0
	for _, v := range vocab {
		magnitude += v * v
	}
	mag := math.Sqrt(magnitude)
	if mag == 0 {
		return []float64{}
	}
	for k := range vocab {
		vocab[k] = vocab[k] / mag
	}
	result := make([]float64, 0, len(vocab))
	keys := make([]string, 0, len(vocab))
	for k := range vocab {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		result = append(result, vocab[k])
	}
	return result
}

func tokenize(text string) []string {
	lower := strings.ToLower(text)
	fields := strings.FieldsFunc(lower, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_')
	})
	stopwords := map[string]bool{
		"the": true, "a": true, "an": true, "of": true, "to": true, "in": true, "for": true,
		"on": true, "with": true, "at": true, "by": true, "from": true,
		"is": true, "are": true, "was": true, "were": true, "this": true, "that": true,
		"these": true, "those": true, "and": true, "or": true, "but": true,
		"what": true, "which": true, "how": true, "why": true, "when": true, "where": true,
		"who": true, "whom": true, "whose": true,
		"does": true, "do": true, "did": true, "has": true, "have": true, "had": true,
		"be": true, "been": true, "being": true,
		"show": true, "get": true, "find": true, "list": true, "give": true, "tell": true,
		"explain": true, "describe": true,
	}
	var tokens []string
	for _, f := range fields {
		if len(f) > 2 && !stopwords[f] {
			tokens = append(tokens, f)
		}
	}
	return tokens
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}
	magA, magB := 0.0, 0.0
	for _, v := range a {
		magA += v * v
	}
	for _, v := range b {
		magB += v * v
	}
	if magA == 0 || magB == 0 {
		return 0.0
	}
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	dot := 0.0
	for i := 0; i < minLen; i++ {
		dot += a[i] * b[i]
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}

type JobRegistry struct {
	mu   sync.RWMutex
	jobs map[string]*Job
}

func NewJobRegistry() *JobRegistry {
	return &JobRegistry{
		jobs: make(map[string]*Job),
	}
}

func (j *JobRegistry) Acquire(cacheKey string) *Job {
	j.mu.Lock()
	defer j.mu.Unlock()
	if existing, ok := j.jobs[cacheKey]; ok {
		return existing
	}
	job := &Job{
		Done: make(chan struct{}),
	}
	j.jobs[cacheKey] = job
	return job
}

func (j *JobRegistry) Release(cacheKey string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	delete(j.jobs, cacheKey)
}

func (j *JobRegistry) Get(cacheKey string) *Job {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.jobs[cacheKey]
}
