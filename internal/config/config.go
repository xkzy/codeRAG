package config

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codergag/internal/cache"
	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

type DatabaseConfig struct {
	Host     string `yaml:"host" xml:"host"`
	Port     int    `yaml:"port" xml:"port"`
	Database string `yaml:"database" xml:"database"`
	User     string `yaml:"user" xml:"user"`
	Password string `yaml:"password" xml:"password"`
	Path     string `yaml:"path,omitempty" xml:"path,omitempty"`
}

type ProjectConfig struct {
	ID   string `yaml:"id" xml:"project"`
	Name string `yaml:"name" xml:"name"`
	Path string `yaml:"path" xml:"path"`
}

type IndexingConfig struct {
	Incremental  bool     `yaml:"incremental" xml:"incremental"`
	Ignore       []string `yaml:"ignore" xml:"ignore"`
	SkipGraphify bool     `yaml:"skip_graphify" xml:"skip_graphify"`
}

type GraphConfig struct {
	CacheNodes int `yaml:"cache_nodes" xml:"cache_nodes"`
	CacheEdges int `yaml:"cache_edges" xml:"cache_edges"`
}

type VerificationConfig struct {
	// Enabled globally enables the runner. Off by default for safety.
	Enabled bool `yaml:"enabled" xml:"enabled"`
	// AllowedCommands is the set of executable names that may be invoked.
	AllowedCommands []string `yaml:"allowed_commands" xml:"allowed_commands"`
	// AllowedSubcommands restricts which sub-verbs a binary may use.
	// Key is the binary name; value is the list of allowed first arguments.
	AllowedSubcommands map[string][]string `yaml:"allowed_subcommands" xml:"allowed_subcommands"`
	// TimeoutSeconds caps a single run. Default 120.
	TimeoutSeconds int `yaml:"timeout_seconds" xml:"timeout_seconds"`
	// MaxOutputBytes caps captured stdout+stderr. Default 64 KB.
	MaxOutputBytes int `yaml:"max_output_bytes" xml:"max_output_bytes"`
}

// WatchConfig controls automatic indexing and the live terminal dashboard.
type WatchConfig struct {
	Enabled          bool     `yaml:"enabled" xml:"enabled"`
	Interval         string   `yaml:"interval" xml:"interval"`
	IndexInterval    string   `yaml:"index_interval" xml:"index_interval"`
	Debounce         string   `yaml:"debounce" xml:"debounce"`
	IndexOnChange    bool     `yaml:"index_on_change" xml:"index_on_change"`
	MaxWorkers       int      `yaml:"max_workers" xml:"max_workers"`
	QueueSize        int      `yaml:"queue_size" xml:"queue_size"`
	MaxWatchDirs     int      `yaml:"max_watch_dirs" xml:"max_watch_dirs"`
	MaxDirtyFiles    int      `yaml:"max_dirty_files" xml:"max_dirty_files"`
	ProjectScanDepth int      `yaml:"project_scan_depth" xml:"project_scan_depth"`
	ProjectRoots     []string `yaml:"project_roots" xml:"project_roots"`
}

func (w WatchConfig) IntervalDuration() time.Duration {
	d, err := time.ParseDuration(w.Interval)
	if err != nil || d <= 0 {
		return 2 * time.Second
	}
	return d
}

func (w WatchConfig) IndexIntervalDuration() time.Duration {
	d, err := time.ParseDuration(w.IndexInterval)
	if err != nil || d <= 0 {
		return 5 * time.Minute
	}
	return d
}

func (w WatchConfig) DebounceDuration() time.Duration {
	d, err := time.ParseDuration(w.Debounce)
	if err != nil || d < 0 {
		return 200 * time.Millisecond
	}
	return d
}

// ContextConfig sets the token budgets for hook-injected context digests.
// Zero values fall back to the defaults.
type ContextConfig struct {
	SessionBudget int `yaml:"session_budget,omitempty" xml:"session_budget,omitempty"`
	PromptBudget  int `yaml:"prompt_budget,omitempty" xml:"prompt_budget,omitempty"`
}

func (c ContextConfig) Session() int {
	if c.SessionBudget > 0 {
		return c.SessionBudget
	}
	return 1500
}

func (c ContextConfig) Prompt() int {
	if c.PromptBudget > 0 {
		return c.PromptBudget
	}
	return 500
}

type Config struct {
	Database     DatabaseConfig     `yaml:"database" xml:"database"`
	Projects     []ProjectConfig    `yaml:"projects" xml:"projects"`
	Indexing     IndexingConfig     `yaml:"indexing" xml:"indexing"`
	Watch        WatchConfig        `yaml:"watch" xml:"watch"`
	Storage      string             `yaml:"storage,omitempty" xml:"storage,omitempty"`
	Cache        cache.CacheConfig  `yaml:"cache,omitempty" xml:"cache,omitempty"`
	Verification VerificationConfig `yaml:"verification,omitempty" xml:"verification,omitempty"`
	Graph        GraphConfig        `yaml:"graph,omitempty" xml:"graph,omitempty"`
	Context      ContextConfig      `yaml:"context,omitempty" xml:"context,omitempty"`
}

func Default() Config {
	return Config{
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     2480,
			Database: "codegraph",
			User:     "admin",
			Password: "admin",
		},
		Projects: []ProjectConfig{},
		Indexing: IndexingConfig{
			Incremental:  true,
			Ignore:       []string{".git", "build", "node_modules"},
			SkipGraphify: false,
		},
		Watch: WatchConfig{
			Enabled:          true,
			Interval:         "30s",
			Debounce:         "500ms",
			IndexOnChange:    true,
			MaxWorkers:       4,
			QueueSize:        1024,
			MaxWatchDirs:     256,
			MaxDirtyFiles:    10000,
			ProjectScanDepth: 3,
			ProjectRoots:     []string{"."},
		},
		Storage: "sqlite",
		Cache:   cache.DefaultConfig(),
		Graph: GraphConfig{
			CacheNodes: 1024,
			CacheEdges: 1024,
		},
	}
}

func Load(path string) (Config, error) {
	if path == "" {
		path = os.Getenv("CODEGRAPH_CONFIG")
		if path == "" {
			path = filepath.Join(os.Getenv("HOME"), ".codergag.yaml")
		}
	}
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		if err := json.Unmarshal(data, &cfg); err != nil {
			return Default(), err
		}
	case ".toml":
		if err := parseTOML(data, &cfg); err != nil {
			return Default(), err
		}
	case ".xml", ".config":
		if err := parseXML(data, &cfg); err != nil {
			return Default(), err
		}
	case ".yaml", ".yml", "":
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Default(), err
		}
	default:
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Default(), err
		}
	}
	if cfg.Database.Password == "" {
		cfg.Database.Password = os.Getenv("CODEGRAPH_DB_PASSWORD")
	}
	if cfg.Projects == nil {
		cfg.Projects = []ProjectConfig{}
	}
	if cfg.Indexing.Ignore == nil {
		cfg.Indexing.Ignore = []string{}
	}
	if cfg.Watch.Interval == "" {
		cfg.Watch.Interval = "30s"
	}
	if cfg.Watch.Debounce == "" {
		cfg.Watch.Debounce = "500ms"
	}
	if cfg.Watch.MaxWorkers <= 0 {
		cfg.Watch.MaxWorkers = 4
	}
	if cfg.Watch.QueueSize <= 0 {
		cfg.Watch.QueueSize = 1024
	}
	if cfg.Watch.MaxWatchDirs <= 0 {
		cfg.Watch.MaxWatchDirs = 256
	}
	if cfg.Watch.MaxDirtyFiles <= 0 {
		cfg.Watch.MaxDirtyFiles = 10000
	}
	if cfg.Watch.ProjectScanDepth <= 0 {
		cfg.Watch.ProjectScanDepth = 3
	}
	if cfg.Watch.ProjectRoots == nil {
		cfg.Watch.ProjectRoots = []string{"."}
	}
	if cfg.Storage == "" {
		cfg.Storage = "sqlite"
	}
	if cfg.Cache.Semantic.Threshold == 0 {
		cfg.Cache.Semantic.Threshold = 0.92
	}
	if cfg.Cache.Semantic.MaxResults == 0 {
		cfg.Cache.Semantic.MaxResults = 5
	}
	if cfg.Cache.Semantic.MaxEntries <= 0 {
		cfg.Cache.Semantic.MaxEntries = 10000
	}
	if cfg.Cache.Exact.MaxEntries <= 0 {
		cfg.Cache.Exact.MaxEntries = 10000
	}
	if cfg.Cache.TTL.Seconds == 0 {
		cfg.Cache.TTL.Seconds = 86400
	}
	if cfg.Graph.CacheNodes <= 0 {
		cfg.Graph.CacheNodes = 1024
	}
	if cfg.Graph.CacheEdges <= 0 {
		cfg.Graph.CacheEdges = 1024
	}
	return cfg, nil
}

func parseTOML(data []byte, cfg *Config) error {
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return err
	}
	jsonData, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonData, cfg)
}

func parseXML(data []byte, cfg *Config) error {
	return xml.Unmarshal(data, cfg)
}

func (c *Config) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	return enc.Encode(c)
}
