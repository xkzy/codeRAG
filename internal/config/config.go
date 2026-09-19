package config

import (
	"os"
	"path/filepath"

	"codergag/internal/cache"
	"gopkg.in/yaml.v3"
)

type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Database string `yaml:"database"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Path     string `yaml:"path,omitempty"`
}

type ProjectConfig struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

type IndexingConfig struct {
	Incremental bool     `yaml:"incremental"`
	Ignore      []string `yaml:"ignore"`
}

type VerificationConfig struct {
	// Enabled globally enables the runner. Off by default for safety.
	Enabled bool `yaml:"enabled"`
	// AllowedCommands is the set of executable names that may be invoked.
	AllowedCommands []string `yaml:"allowed_commands"`
	// AllowedSubcommands restricts which sub-verbs a binary may use.
	// Key is the binary name; value is the list of allowed first arguments.
	AllowedSubcommands map[string][]string `yaml:"allowed_subcommands"`
	// TimeoutSeconds caps a single run. Default 120.
	TimeoutSeconds int `yaml:"timeout_seconds"`
	// MaxOutputBytes caps captured stdout+stderr. Default 64 KB.
	MaxOutputBytes int `yaml:"max_output_bytes"`
}

type Config struct {
	Database     DatabaseConfig    `yaml:"database"`
	Projects     []ProjectConfig   `yaml:"projects"`
	Indexing     IndexingConfig    `yaml:"indexing"`
	Storage      string            `yaml:"storage,omitempty"`
	Cache        cache.CacheConfig `yaml:"cache,omitempty"`
	Verification VerificationConfig `yaml:"verification,omitempty"`
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
			Incremental: true,
			Ignore:      []string{".git", "build", "node_modules"},
		},
		Storage: "sqlite",
		Cache:   cache.DefaultConfig(),
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
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Default(), err
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
	if cfg.Storage == "" {
		cfg.Storage = "sqlite"
	}
	if cfg.Cache.Semantic.Threshold == 0 {
		cfg.Cache.Semantic.Threshold = 0.92
	}
	if cfg.Cache.Semantic.MaxResults == 0 {
		cfg.Cache.Semantic.MaxResults = 5
	}
	if cfg.Cache.TTL.Seconds == 0 {
		cfg.Cache.TTL.Seconds = 86400
	}
	return cfg, nil
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
