package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, content string, ext string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config"+ext)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadYAML(t *testing.T) {
	p := writeConfig(t, "database:\n  host: db.example.com\n  port: 2481\nstorage: sqlite\n", ".yaml")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Host != "db.example.com" {
		t.Fatalf("host: %s", cfg.Database.Host)
	}
	if cfg.Database.Port != 2481 {
		t.Fatalf("port: %d", cfg.Database.Port)
	}
	if cfg.Storage != "sqlite" {
		t.Fatalf("storage: %s", cfg.Storage)
	}
}

func TestLoadJSON(t *testing.T) {
	p := writeConfig(t, `{"database":{"host":"db.example.com","port":2481},"storage":"sqlite"}`, ".json")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Host != "db.example.com" {
		t.Fatalf("host: %s", cfg.Database.Host)
	}
	if cfg.Database.Port != 2481 {
		t.Fatalf("port: %d", cfg.Database.Port)
	}
}

func TestLoadTOML(t *testing.T) {
	p := writeConfig(t, "[[projects]]\nid = \"p\"\nname = \"proj\"\npath = \"/tmp/p\"\n", ".toml")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Projects) != 1 || cfg.Projects[0].ID != "p" {
		t.Fatalf("projects: %v", cfg.Projects)
	}
}

func TestLoadXML(t *testing.T) {
	p := writeConfig(t, "<?xml version=\"1.0\"?>\n<config>\n  <database><host>db.example.com</host><port>2481</port></database>\n  <storage>sqlite</storage>\n</config>\n", ".xml")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Host != "db.example.com" {
		t.Fatalf("host: %s", cfg.Database.Host)
	}
	if cfg.Database.Port != 2481 {
		t.Fatalf("port: %d", cfg.Database.Port)
	}
	if cfg.Storage != "sqlite" {
		t.Fatalf("storage: %s", cfg.Storage)
	}
}

func TestLoadDefaultsWhenMissing(t *testing.T) {
	p := writeConfig(t, "", ".yaml")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage != "sqlite" {
		t.Fatalf("storage default: %s", cfg.Storage)
	}
	if cfg.Cache.Semantic.Threshold != 0.92 {
		t.Fatalf("threshold default: %v", cfg.Cache.Semantic.Threshold)
	}
}

func TestPartialWatchKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte("watch:\n  enabled: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Watch.IndexOnChange {
		t.Fatal("index_on_change default lost")
	}
}
