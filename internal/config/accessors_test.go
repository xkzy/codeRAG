package config

import (
	"testing"
)

func TestConfigGetAllSupportedKeys(t *testing.T) {
	cfg := Default()
	keys := []string{
		"storage",
		"cache.enabled",
		"cache.exact.max_entries",
		"cache.semantic.threshold",
		"cache.semantic.max_entries",
		"cache.ttl.enabled",
		"cache.ttl.seconds",
		"indexing.incremental",
		"indexing.skip_graphify",
		"watch.enabled",
		"watch.interval",
		"watch.index_interval",
		"watch.debounce",
		"watch.index_on_change",
		"watch.max_workers",
		"watch.queue_size",
		"watch.max_watch_dirs",
		"watch.max_dirty_files",
		"watch.project_scan_depth",
		"watch.project_roots",
		"graph.cache_nodes",
		"graph.cache_edges",
		"verification.enabled",
		"verification.timeout_seconds",
		"verification.max_output_bytes",
	}
	for _, k := range keys {
		_, err := ConfigGet(cfg, k)
		if err != nil {
			t.Errorf("ConfigGet(%q) unexpected error: %v", k, err)
		}
	}
}

func TestConfigGetKnownValues(t *testing.T) {
	cfg := Default()

	cases := []struct {
		key  string
		want string
	}{
		{"storage", "sqlite"},
		{"indexing.incremental", "true"},
		{"indexing.skip_graphify", "false"},
		{"watch.enabled", "true"},
		{"watch.interval", "30s"},
		{"watch.debounce", "500ms"},
		{"watch.index_on_change", "true"},
		{"watch.max_workers", "4"},
		{"watch.queue_size", "1024"},
		{"watch.max_watch_dirs", "256"},
		{"watch.max_dirty_files", "10000"},
		{"watch.project_scan_depth", "3"},
		{"graph.cache_nodes", "1024"},
		{"graph.cache_edges", "1024"},
	}
	for _, tc := range cases {
		got, err := ConfigGet(cfg, tc.key)
		if err != nil {
			t.Errorf("ConfigGet(%q): %v", tc.key, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ConfigGet(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestConfigGetCSVField(t *testing.T) {
	cfg := Default()
	cfg.Watch.ProjectRoots = []string{"/a", "/b"}
	got, err := ConfigGet(cfg, "watch.project_roots")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/a,/b" {
		t.Fatalf("want '/a,/b', got %q", got)
	}
}

func TestConfigGetUnknownKey(t *testing.T) {
	cfg := Default()
	_, err := ConfigGet(cfg, "nonexistent.key")
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestConfigSetAndGetRoundTrip(t *testing.T) {
	cfg := Default()
	cases := []struct {
		key string
		val string
	}{
		{"storage", "postgres"},
		{"cache.enabled", "true"},
		{"cache.exact.max_entries", "500"},
		{"cache.semantic.threshold", "0.75"},
		{"cache.semantic.max_entries", "5000"},
		{"cache.ttl.enabled", "true"},
		{"cache.ttl.seconds", "7200"},
		{"indexing.incremental", "false"},
		{"indexing.skip_graphify", "true"},
		{"watch.enabled", "false"},
		{"watch.interval", "5s"},
		{"watch.index_interval", "30s"},
		{"watch.debounce", "200ms"},
		{"watch.index_on_change", "false"},
		{"watch.max_workers", "8"},
		{"watch.queue_size", "200"},
		{"watch.max_watch_dirs", "50"},
		{"watch.max_dirty_files", "500"},
		{"watch.project_scan_depth", "4"},
		{"graph.cache_nodes", "2048"},
		{"graph.cache_edges", "2048"},
		{"verification.enabled", "true"},
		{"verification.timeout_seconds", "60"},
		{"verification.max_output_bytes", "65536"},
	}
	for _, tc := range cases {
		if err := ConfigSet(&cfg, tc.key, tc.val); err != nil {
			t.Errorf("ConfigSet(%q, %q): %v", tc.key, tc.val, err)
			continue
		}
		got, err := ConfigGet(cfg, tc.key)
		if err != nil {
			t.Errorf("ConfigGet(%q) after set: %v", tc.key, err)
			continue
		}
		if got != tc.val {
			t.Errorf("round-trip(%q): got %q, want %q", tc.key, got, tc.val)
		}
	}
}

func TestConfigSetCSVField(t *testing.T) {
	cfg := Default()
	if err := ConfigSet(&cfg, "watch.project_roots", "/x,/y"); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Watch.ProjectRoots) != 2 || cfg.Watch.ProjectRoots[0] != "/x" {
		t.Fatalf("splitCSV failed: %v", cfg.Watch.ProjectRoots)
	}
}

func TestConfigSetEmptyCSV(t *testing.T) {
	cfg := Default()
	if err := ConfigSet(&cfg, "watch.project_roots", ""); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Watch.ProjectRoots) != 0 {
		t.Fatalf("empty CSV should yield empty slice, got %v", cfg.Watch.ProjectRoots)
	}
}

func TestConfigSetUnknownKey(t *testing.T) {
	cfg := Default()
	if err := ConfigSet(&cfg, "no.such.key", "val"); err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestConfigSetInvalidBool(t *testing.T) {
	cfg := Default()
	if err := ConfigSet(&cfg, "cache.enabled", "notabool"); err == nil {
		t.Fatal("expected error for invalid bool")
	}
}

func TestConfigSetInvalidInt(t *testing.T) {
	cfg := Default()
	if err := ConfigSet(&cfg, "cache.exact.max_entries", "notanint"); err == nil {
		t.Fatal("expected error for invalid int")
	}
}

func TestConfigSetInvalidFloat(t *testing.T) {
	cfg := Default()
	if err := ConfigSet(&cfg, "cache.semantic.threshold", "notafloat"); err == nil {
		t.Fatal("expected error for invalid float")
	}
}

func TestDurationHelpers(t *testing.T) {
	cfg := Default()
	// Default has "30s" interval and "500ms" debounce
	if d := cfg.Watch.IntervalDuration(); d.Seconds() != 30 {
		t.Fatalf("IntervalDuration: got %v", d)
	}
	if d := cfg.Watch.DebounceDuration(); d.Milliseconds() != 500 {
		t.Fatalf("DebounceDuration: got %v", d)
	}
	// IndexInterval may be empty by default; test explicit set
	cfg.Watch.IndexInterval = "60s"
	if d := cfg.Watch.IndexIntervalDuration(); d.Seconds() != 60 {
		t.Fatalf("IndexIntervalDuration: got %v", d)
	}
}

func TestConfigSaveAndLoad(t *testing.T) {
	path := t.TempDir() + "/saved.yaml"
	cfg := Default()
	cfg.Storage = "postgres"
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	cfg2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Storage != "postgres" {
		t.Fatalf("Save/Load round trip failed: %s", cfg2.Storage)
	}
}

func TestLoadUnknownExtensionFallsBackToYAML(t *testing.T) {
	p := writeConfig(t, "storage: postgres\n", ".conf")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage != "postgres" {
		t.Fatalf("unknown ext fallback: %s", cfg.Storage)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	p := writeConfig(t, ":\tinvalid: yaml: :\n", ".yaml")
	_, err := Load(p)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	p := writeConfig(t, "{not valid json}", ".json")
	_, err := Load(p)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
