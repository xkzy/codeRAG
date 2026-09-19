package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"codergag/internal/graph"
	"codergag/internal/models"
)

const (
	SchemaVersion      = "v1"
	AnalysisVersion    = "v1"
	CacheStatusHit     = "CACHE_HIT"
	CacheStatusMiss    = "CACHE_MISS"
	CacheStatusPartial = "CACHE_PARTIAL"
	CacheStatusStale   = "STALE"
	CacheStatusInvalid = "INVALID"

	FreshnessValid   = "VALID"
	FreshnessStale   = "STALE"
	FreshnessInvalid = "INVALID"
)

type CacheConfig struct {
	Enabled      bool               `yaml:"enabled" xml:"enabled"`
	Exact        LevelConfig        `yaml:"exact" xml:"exact"`
	Semantic     SemanticConfig     `yaml:"semantic" xml:"semantic"`
	Tool         LevelConfig        `yaml:"tool" xml:"tool"`
	Analysis     LevelConfig        `yaml:"analysis" xml:"analysis"`
	LLM          LLMCacheConfig     `yaml:"llm" xml:"llm"`
	TTL          TTLConfig          `yaml:"ttl" xml:"ttl"`
	Invalidation InvalidationConfig `yaml:"invalidation" xml:"invalidation"`
	Concurrency  ConcurrencyConfig  `yaml:"concurrency" xml:"concurrency"`
	Privacy      PrivacyConfig      `yaml:"privacy" xml:"privacy"`
}

type LevelConfig struct {
	Enabled bool `yaml:"enabled" xml:"enabled"`
}

type SemanticConfig struct {
	Enabled    bool    `yaml:"enabled" xml:"enabled"`
	Threshold  float64 `yaml:"threshold" xml:"threshold"`
	MaxResults int     `yaml:"max_results" xml:"max_results"`
}

type LLMCacheConfig struct {
	Enabled        bool `yaml:"enabled" xml:"enabled"`
	CacheResponses bool `yaml:"cache_responses" xml:"cache_responses"`
}

type TTLConfig struct {
	Enabled bool `yaml:"enabled" xml:"enabled"`
	Seconds int  `yaml:"seconds" xml:"seconds"`
}

type InvalidationConfig struct {
	Git         bool `yaml:"git" xml:"git"`
	BinaryHash  bool `yaml:"binary_hash" xml:"binary_hash"`
	Dependency  bool `yaml:"dependency" xml:"dependency"`
	ToolVersion bool `yaml:"tool_version" xml:"tool_version"`
}

type ConcurrencyConfig struct {
	PreventDuplicateWork bool `yaml:"prevent_duplicate_work" xml:"prevent_duplicate_work"`
}

type PrivacyConfig struct {
	CacheLLMResponses  bool `yaml:"cache_llm_responses" xml:"cache_llm_responses"`
	CacheSourceContent bool `yaml:"cache_source_content" xml:"cache_source_content"`
	CacheBinaryContent bool `yaml:"cache_binary_content" xml:"cache_binary_content"`
	CacheToolResults   bool `yaml:"cache_tool_results" xml:"cache_tool_results"`
}

func DefaultConfig() CacheConfig {
	return CacheConfig{
		Enabled: true,
		Exact:   LevelConfig{Enabled: true},
		Semantic: SemanticConfig{
			Enabled:    true,
			Threshold:  0.92,
			MaxResults: 5,
		},
		Tool:     LevelConfig{Enabled: true},
		Analysis: LevelConfig{Enabled: true},
		LLM:      LLMCacheConfig{Enabled: true, CacheResponses: true},
		TTL:      TTLConfig{Enabled: false, Seconds: 86400},
		Invalidation: InvalidationConfig{
			Git:         true,
			BinaryHash:  true,
			Dependency:  true,
			ToolVersion: true,
		},
		Concurrency: ConcurrencyConfig{PreventDuplicateWork: true},
		Privacy: PrivacyConfig{
			CacheLLMResponses:  true,
			CacheSourceContent: false,
			CacheBinaryContent: false,
			CacheToolResults:   true,
		},
	}
}

type CacheEntry struct {
	CacheKey        string         `json:"cache_key"`
	ToolName        string         `json:"tool_name"`
	Arguments       map[string]any `json:"arguments"`
	Result          map[string]any `json:"result"`
	ProjectID       string         `json:"project_id"`
	RepositoryID    string         `json:"repository_id,omitempty"`
	GitCommit       string         `json:"git_commit,omitempty"`
	BinaryHash      string         `json:"binary_hash,omitempty"`
	ParserVersion   string         `json:"parser_version,omitempty"`
	AnalysisVersion string         `json:"analysis_version,omitempty"`
	SchemaVersion   string         `json:"schema_version,omitempty"`
	CreatedAt       string         `json:"created_at"`
	UpdatedAt       string         `json:"updated_at"`
	Confidence      float64        `json:"confidence"`
	Model           string         `json:"model,omitempty"`
	ModelVersion    string         `json:"model_version,omitempty"`
	Agent           string         `json:"agent,omitempty"`
	EvidenceRefs    []string       `json:"evidence_refs,omitempty"`
	GraphRefs       []string       `json:"graph_refs,omitempty"`
	Freshness       string         `json:"freshness"`
	Embedding       []float64      `json:"embedding,omitempty"`
}

type CacheStats struct {
	ExactHits        int     `json:"exact_hits"`
	SemanticHits     int     `json:"semantic_hits"`
	ToolHits         int     `json:"tool_hits"`
	AnalysisHits     int     `json:"analysis_hits"`
	GraphHits        int     `json:"graph_hits"`
	PartialHits      int     `json:"partial_hits"`
	CacheMisses      int     `json:"cache_misses"`
	StaleHits        int     `json:"stale_hits"`
	Invalidations    int     `json:"invalidations"`
	LLMCallsSaved    int     `json:"llm_calls_saved"`
	ToolCallsSaved   int     `json:"tool_calls_saved"`
	TokensSaved      int     `json:"estimated_tokens_saved"`
	TimeSavedSeconds float64 `json:"analysis_time_saved_seconds"`
}

type LookupResult struct {
	Status     string         `json:"cache_status"`
	Result     map[string]any `json:"result,omitempty"`
	Entry      *CacheEntry    `json:"entry,omitempty"`
	Confidence float64        `json:"confidence,omitempty"`
	Evidence   []string       `json:"evidence,omitempty"`
	GraphRefs  []string       `json:"graph_references,omitempty"`
	Reused     []string       `json:"reused,omitempty"`
	Missing    []string       `json:"missing,omitempty"`
}

type CacheKeyInput struct {
	ProjectID       string
	RepositoryID    string
	GitCommit       string
	BinaryHash      string
	ToolName        string
	Arguments       map[string]any
	ParserVersion   string
	SchemaVersion   string
	AnalysisVersion string
}

func GenerateCacheKey(input CacheKeyInput) string {
	normalized := normalizeArgs(input.Arguments)
	hashInput := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s|%s",
		input.ProjectID,
		input.RepositoryID,
		input.GitCommit,
		input.BinaryHash,
		input.ToolName,
		normalized,
		input.ParserVersion,
		input.SchemaVersion,
		input.AnalysisVersion,
		SchemaVersion,
	)
	h := sha256.Sum256([]byte(hashInput))
	return fmt.Sprintf("%x", h[:])
}

func normalizeArgs(args map[string]any) string {
	if args == nil {
		return "{}"
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == "project_id" {
			continue
		}
		v := args[k]
		bs, _ := json.Marshal(v)
		parts = append(parts, fmt.Sprintf("%s=%s", k, string(bs)))
	}
	return strings.Join(parts, "&")
}

func NowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

type CacheManager struct {
	graph         graph.GraphRepository
	config        CacheConfig
	mu            sync.RWMutex
	stats         CacheStats
	jobs          map[string]*Job
	semanticCache *SemanticCache
}

type Job struct {
	mu        sync.Mutex
	Claimed   bool
	Completed bool
	Result    map[string]any
	Error     string
	Done      chan struct{}
}

func NewCacheManager(g graph.GraphRepository, cfg CacheConfig) *CacheManager {
	cm := &CacheManager{
		graph:  g,
		config: cfg,
		jobs:   make(map[string]*Job),
	}
	if cfg.Semantic.Enabled {
		cm.semanticCache = NewSemanticCache(cfg.Semantic)
	}
	return cm
}

func (c *CacheManager) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats
}

func (c *CacheManager) IncrementStat(field string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch field {
	case "exact_hits":
		c.stats.ExactHits++
	case "semantic_hits":
		c.stats.SemanticHits++
	case "tool_hits":
		c.stats.ToolHits++
	case "analysis_hits":
		c.stats.AnalysisHits++
	case "graph_hits":
		c.stats.GraphHits++
	case "partial_hits":
		c.stats.PartialHits++
	case "cache_misses":
		c.stats.CacheMisses++
	case "stale_hits":
		c.stats.StaleHits++
	case "invalidations":
		c.stats.Invalidations++
	case "llm_calls_saved":
		c.stats.LLMCallsSaved++
	case "tool_calls_saved":
		c.stats.ToolCallsSaved++
	}
}

func (c *CacheManager) CheckExactCache(projectID, repoID, commit, binaryHash, toolName string, args map[string]any) (*CacheEntry, error) {
	if !c.config.Enabled || !c.config.Exact.Enabled {
		return nil, nil
	}
	cacheKey := GenerateCacheKey(CacheKeyInput{
		ProjectID:       projectID,
		RepositoryID:    repoID,
		GitCommit:       commit,
		BinaryHash:      binaryHash,
		ToolName:        toolName,
		Arguments:       args,
		ParserVersion:   cacheParserVersion,
		SchemaVersion:   SchemaVersion,
		AnalysisVersion: AnalysisVersion,
	})
	entries, err := c.graph.FindNodes("CacheEntry", map[string]any{
		"project_id": projectID,
		"cache_key":  cacheKey,
		"tool_name":  toolName,
	})
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	entry := unmarshalCacheEntry(entries[0])
	if c.isFresh(entry) {
		return entry, nil
	}
	c.IncrementStat("stale_hits")
	return &CacheEntry{Freshness: FreshnessStale}, nil
}

func (c *CacheManager) StoreExactCache(projectID, repoID, commit, binaryHash, toolName string, args map[string]any, result map[string]any, confidence float64, agent string) error {
	if !c.config.Enabled || !c.config.Exact.Enabled {
		return nil
	}
	cacheKey := GenerateCacheKey(CacheKeyInput{
		ProjectID:       projectID,
		RepositoryID:    repoID,
		GitCommit:       commit,
		BinaryHash:      binaryHash,
		ToolName:        toolName,
		Arguments:       args,
		ParserVersion:   cacheParserVersion,
		SchemaVersion:   SchemaVersion,
		AnalysisVersion: AnalysisVersion,
	})
	now := NowISO()
	_, err := c.graph.UpsertNode("CacheEntry", map[string]any{
		"project_id": projectID,
		"cache_key":  cacheKey,
		"tool_name":  toolName,
	}, map[string]any{
		"arguments":        args,
		"result":           result,
		"repository_id":    repoID,
		"git_commit":       commit,
		"binary_hash":      binaryHash,
		"parser_version":   cacheParserVersion,
		"schema_version":   SchemaVersion,
		"analysis_version": AnalysisVersion,
		"created_at":       now,
		"updated_at":       now,
		"confidence":       confidence,
		"agent":            agent,
		"freshness":        FreshnessValid,
	})
	return err
}

func (c *CacheManager) Invalidate(args InvalidateArgs) error {
	filters := map[string]any{"project_id": args.ProjectID}
	var kind string
	if args.CacheKey != "" {
		kind = "CacheEntry"
		filters["cache_key"] = args.CacheKey
	}
	if args.CacheID != "" {
		kind = "CacheEntry"
		filters["id"] = args.CacheID
	}
	if args.ToolName != "" {
		kind = "CacheEntry"
		filters["tool_name"] = args.ToolName
	}
	if kind == "" {
		kind = "CacheEntry"
	}
	entries, err := c.graph.FindNodes(kind, filters)
	if err != nil {
		return err
	}
	var ids []string
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	if len(ids) > 0 {
		if err := c.graph.RemoveNodes(ids); err != nil {
			return err
		}
		c.mu.Lock()
		c.stats.Invalidations += len(ids)
		c.mu.Unlock()
	}
	return nil
}

func (c *CacheManager) InvalidateByCommit(projectID, commit string) error {
	entries, err := c.graph.FindNodes("CacheEntry", map[string]any{
		"project_id": projectID,
		"git_commit": commit,
	})
	if err != nil {
		return err
	}
	var ids []string
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	if len(ids) > 0 {
		if err := c.graph.RemoveNodes(ids); err != nil {
			return err
		}
		c.mu.Lock()
		c.stats.Invalidations += len(ids)
		c.mu.Unlock()
	}
	return nil
}

func (c *CacheManager) InvalidateByBinaryHash(projectID, binaryHash string) error {
	entries, err := c.graph.FindNodes("CacheEntry", map[string]any{
		"project_id":  projectID,
		"binary_hash": binaryHash,
	})
	if err != nil {
		return err
	}
	var ids []string
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	if len(ids) > 0 {
		if err := c.graph.RemoveNodes(ids); err != nil {
			return err
		}
		c.mu.Lock()
		c.stats.Invalidations += len(ids)
		c.mu.Unlock()
	}
	return nil
}

func (c *CacheManager) isFresh(entry *CacheEntry) bool {
	if entry.Freshness == FreshnessInvalid {
		return false
	}
	if entry.Freshness == FreshnessStale {
		return false
	}
	if c.config.TTL.Enabled {
		t, err := time.Parse(time.RFC3339, entry.UpdatedAt)
		if err == nil {
			if time.Since(t).Seconds() > float64(c.config.TTL.Seconds) {
				return false
			}
		}
	}
	return true
}

func unmarshalCacheEntry(node *models.Node) *CacheEntry {
	entry := &CacheEntry{
		CacheKey:        getString(node.Properties, "cache_key"),
		ToolName:        getString(node.Properties, "tool_name"),
		ProjectID:       getString(node.Properties, "project_id"),
		RepositoryID:    getString(node.Properties, "repository_id"),
		GitCommit:       getString(node.Properties, "git_commit"),
		BinaryHash:      getString(node.Properties, "binary_hash"),
		ParserVersion:   getString(node.Properties, "parser_version"),
		AnalysisVersion: getString(node.Properties, "analysis_version"),
		SchemaVersion:   getString(node.Properties, "schema_version"),
		CreatedAt:       getString(node.Properties, "created_at"),
		UpdatedAt:       getString(node.Properties, "updated_at"),
		Agent:           getString(node.Properties, "agent"),
		Freshness:       getString(node.Properties, "freshness"),
	}
	if args, ok := node.Properties["arguments"].(map[string]any); ok {
		entry.Arguments = args
	}
	if result, ok := node.Properties["result"].(map[string]any); ok {
		entry.Result = result
	}
	if conf, ok := node.Properties["confidence"].(float64); ok {
		entry.Confidence = conf
	}
	if emb, ok := node.Properties["embedding"].([]float64); ok {
		entry.Embedding = emb
	}
	return entry
}

func getString(props map[string]any, key string) string {
	if v, ok := props[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Flush removes all cache entries for a project.
func (c *CacheManager) Flush(projectID string) (int, error) {
	entries, err := c.graph.FindNodes("CacheEntry", map[string]any{"project_id": projectID})
	if err != nil {
		return 0, err
	}
	var ids []string
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	if len(ids) > 0 {
		if err := c.graph.RemoveNodes(ids); err != nil {
			return 0, err
		}
		c.mu.Lock()
		c.stats.Invalidations += len(ids)
		c.mu.Unlock()
	}
	return len(ids), nil
}

// PrivacyStateKey is the tool name used for privacy state cache entries.
const PrivacyStateKey = "__privacy_state__"

// PrivacyPolicyKey is the tool name used for privacy policy cache entries.
const PrivacyPolicyKey = "__privacy_policy__"

// PseudonymSnapshotKey is the tool name used for pseudonym snapshot cache entries.
const PseudonymSnapshotKey = "__pseudonym_snapshot__"

// StorePrivacyPolicy stores the privacy policy for a project in the cache.
func (c *CacheManager) StorePrivacyPolicy(projectID string, policyJSON string) error {
	if !c.config.Enabled {
		return nil
	}
	now := NowISO()
	_, err := c.graph.UpsertNode("CacheEntry", map[string]any{
		"project_id": projectID,
		"cache_key":  PrivacyPolicyKey,
		"tool_name":  PrivacyPolicyKey,
	}, map[string]any{
		"arguments":        map[string]any{},
		"result":           map[string]any{"policy": policyJSON},
		"repository_id":    "",
		"git_commit":       "",
		"binary_hash":      "",
		"parser_version":   cacheParserVersion,
		"schema_version":   SchemaVersion,
		"analysis_version": AnalysisVersion,
		"created_at":       now,
		"updated_at":       now,
		"confidence":       1.0,
		"agent":            "privacy_service",
		"freshness":        FreshnessValid,
	})
	return err
}

// GetPrivacyPolicy retrieves the privacy policy for a project from the cache.
func (c *CacheManager) GetPrivacyPolicy(projectID string) (string, error) {
	if !c.config.Enabled {
		return "", nil
	}
	entries, err := c.graph.FindNodes("CacheEntry", map[string]any{
		"project_id": projectID,
		"cache_key":  PrivacyPolicyKey,
		"tool_name":  PrivacyPolicyKey,
	})
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", nil
	}
	entry := unmarshalCacheEntry(entries[0])
	if result, ok := entry.Result["policy"].(string); ok {
		return result, nil
	}
	return "", nil
}

// StorePseudonymSnapshot stores the pseudonym snapshot for a project in the cache.
func (c *CacheManager) StorePseudonymSnapshot(projectID string, snapshotJSON string) error {
	if !c.config.Enabled {
		return nil
	}
	now := NowISO()
	_, err := c.graph.UpsertNode("CacheEntry", map[string]any{
		"project_id": projectID,
		"cache_key":  PseudonymSnapshotKey,
		"tool_name":  PseudonymSnapshotKey,
	}, map[string]any{
		"arguments":        map[string]any{},
		"result":           map[string]any{"snapshot": snapshotJSON},
		"repository_id":    "",
		"git_commit":       "",
		"binary_hash":      "",
		"parser_version":   cacheParserVersion,
		"schema_version":   SchemaVersion,
		"analysis_version": AnalysisVersion,
		"created_at":       now,
		"updated_at":       now,
		"confidence":       1.0,
		"agent":            "privacy_service",
		"freshness":        FreshnessValid,
	})
	return err
}

// GetPseudonymSnapshot retrieves the pseudonym snapshot for a project from the cache.
func (c *CacheManager) GetPseudonymSnapshot(projectID string) (string, error) {
	if !c.config.Enabled {
		return "", nil
	}
	entries, err := c.graph.FindNodes("CacheEntry", map[string]any{
		"project_id": projectID,
		"cache_key":  PseudonymSnapshotKey,
		"tool_name":  PseudonymSnapshotKey,
	})
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", nil
	}
	entry := unmarshalCacheEntry(entries[0])
	if result, ok := entry.Result["snapshot"].(string); ok {
		return result, nil
	}
	return "", nil
}

// CheckCache performs a multi-level cache lookup (L0 exact -> L1 semantic).
// Returns CACHE_HIT for exact match, CACHE_PARTIAL for semantic match, STALE for stale exact,
// CACHE_MISS if nothing found.
func (c *CacheManager) CheckCache(projectID, repoID, commit, binaryHash, toolName string, args map[string]any) (*LookupResult, error) {
	if !c.config.Enabled {
		return &LookupResult{Status: CacheStatusMiss}, nil
	}

	// L0: Exact cache
	exactEntry, err := c.CheckExactCache(projectID, repoID, commit, binaryHash, toolName, args)
	if err != nil {
		return nil, err
	}
	if exactEntry != nil {
		if exactEntry.Freshness == FreshnessStale {
			c.IncrementStat("stale_hits")
			return &LookupResult{
				Status: CacheStatusStale,
				Entry:  exactEntry,
			}, nil
		}
		c.IncrementStat("exact_hits")
		return &LookupResult{
			Status:     CacheStatusHit,
			Result:     exactEntry.Result,
			Entry:      exactEntry,
			Confidence: exactEntry.Confidence,
			Evidence:   exactEntry.EvidenceRefs,
			GraphRefs:  exactEntry.GraphRefs,
		}, nil
	}

	// L1: Semantic cache (if enabled and query provided)
	if c.config.Semantic.Enabled && c.semanticCache != nil {
		if query, ok := args["query"].(string); ok && query != "" {
			semanticEntry := c.semanticCache.Search(query, projectID, repoID, commit, binaryHash, c.config.Semantic.MaxResults)
			if semanticEntry != nil {
				c.IncrementStat("semantic_hits")
				return &LookupResult{
					Status:     CacheStatusPartial,
					Result:     semanticEntry.Result,
					Entry:      semanticEntry,
					Confidence: semanticEntry.Confidence,
					Evidence:   semanticEntry.EvidenceRefs,
					GraphRefs:  semanticEntry.GraphRefs,
				}, nil
			}
		}
	}

	// Cache miss
	c.IncrementStat("cache_misses")
	return &LookupResult{Status: CacheStatusMiss}, nil
}

// StoreCache stores a result in the exact cache (L0).
func (c *CacheManager) StoreCache(projectID, repoID, commit, binaryHash, toolName string, args map[string]any, result map[string]any, confidence float64, agent string) error {
	return c.StoreExactCache(projectID, repoID, commit, binaryHash, toolName, args, result, confidence, agent)
}

// Config returns the current cache configuration.
func (c *CacheManager) Config() CacheConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config
}

// SetTTL updates the TTL settings at runtime.
func (c *CacheManager) SetTTL(enabled bool, seconds int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.TTL.Enabled = enabled
	c.config.TTL.Seconds = seconds
}

type InvalidateArgs struct {
	ProjectID  string
	CacheKey   string
	CacheID    string
	ToolName   string
	Commit     string
	BinaryHash string
	File       string
	Artifact   string
}

const cacheParserVersion = "regex-v1"

type StoredResult struct {
	Result     map[string]any
	CacheEntry *CacheEntry
	FromCache  bool
	Stats      CacheStats
}
