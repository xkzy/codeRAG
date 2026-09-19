package config

import (
	"os"
	"path/filepath"

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

type Config struct {
	Database DatabaseConfig  `yaml:"database"`
	Projects []ProjectConfig `yaml:"projects"`
	Indexing IndexingConfig  `yaml:"indexing"`
	Storage  string          `yaml:"storage,omitempty"`
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
