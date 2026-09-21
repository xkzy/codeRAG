package services

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codergag/internal/security"
)

func TestMapSemgrepToCWE(t *testing.T) {
	cases := []struct {
		checkID   string
		expectCWE string
	}{
		{"python.sql-injection.bad-tensor", "CWE-89"},
		{"go.command-injection", "CWE-78"},
		{"javascript.xss.xss-detector", "CWE-79"},
		{"go.path-traversal", "CWE-22"},
		{"python.pickle.deserialization", "CWE-502"},
		{"go.buffer-overflow", "CWE-125"},
		{"javascript.csrf", "CWE-352"},
		{"python.md5", "CWE-327"},
		{"go.random.hardcoded-secret", "CWE-798"},
		{"unknown-rule", "CWE-20"},
	}
	for _, tc := range cases {
		got := mapSemgrepToCWE(tc.checkID)
		if got != tc.expectCWE {
			t.Errorf("checkID %q: expected %s, got %s", tc.checkID, tc.expectCWE, got)
		}
	}
}

func TestSemgrepSeverityToConfidence(t *testing.T) {
	cases := []struct {
		sev   string
		expect float64
	}{
		{"error", 0.9},
		{"warning", 0.6},
		{"info", 0.3},
		{"unknown", 0.5},
	}
	for _, tc := range cases {
		got := semgrepSeverityToConfidence(tc.sev)
		if got != tc.expect {
			t.Errorf("severity %q: expected %f, got %f", tc.sev, tc.expect, got)
		}
	}
}

func TestAuditProjectSemgrep_NoSemgrepInstalled(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "main.go")
	os.WriteFile(p, []byte(`package main
import "os"
func main() { os.Exit(0) }
`), 0o644)

	app := ApplicationInMemory()
	result, err := app.Security.AuditProjectSemgrep("p", dir, "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if result["method"] != "regex" && result["method"] != "semgrep" {
		// Falls back to regex if semgrep not installed
		t.Logf("method: %v", result["method"])
	}
	if result["project_id"] != "p" {
		t.Errorf("expected project p, got %v", result["project_id"])
	}
}

func TestAuditProjectSemgrep_NotDir(t *testing.T) {
	app := ApplicationInMemory()
	_, err := app.Security.AuditProjectSemgrep("p", "/nonexistent/path", "test-agent")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestExportPatternsJSON(t *testing.T) {
	data, err := security.ExportPatternsJSON()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty JSON")
	}
	// Verify it's valid JSON
	var patterns []security.SerializablePattern
	if err := json.Unmarshal(data, &patterns); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(patterns) < 50 {
		t.Errorf("expected at least 50 patterns, got %d", len(patterns))
	}
}

func TestUpdatePatternsOnline_Fallback(t *testing.T) {
	// This test verifies that if the pattern server is unreachable,
	// we don't crash - the built-in patterns remain.
	original := security.Patterns
	defer func() { security.Patterns = original }()

	count, err := security.UpdatePatternsOnlineWithClient(&http.Client{
		Timeout: 1 * time.Second,
	})
	// If the server is unreachable, we expect an error
	// but the patterns should be unchanged
	if err != nil && count == 0 {
		// Server unreachable is expected in CI
		t.Logf("server unreachable (expected): %v", err)
		// Patterns should still be available
		if len(security.Patterns) == 0 {
			t.Error("patterns should remain after failed update")
		}
	}
}

func TestLoadCachedPatterns(t *testing.T) {
	// First export patterns
	data, err := security.ExportPatternsJSON()
	if err != nil {
		t.Fatal(err)
	}

	// Write to cache
	cacheDir := t.TempDir()
	security.PatternCacheDir = cacheDir
	cachePath := cacheDir + "/patterns.json"
	os.WriteFile(cachePath, data, 0o644)

	// Load cached
	patterns, err := security.LoadCachedPatterns()
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != len(security.Patterns) {
		t.Errorf("expected %d cached patterns, got %d", len(security.Patterns), len(patterns))
	}
}

func TestPatternSourceConstant(t *testing.T) {
	if security.PatternSource == "" {
		t.Error("PatternSource should not be empty")
	}
	if !strings.HasPrefix(security.PatternSource, "https://") {
		t.Error("PatternSource should be an HTTPS URL")
	}
}

func TestPatternsUpdateTime(t *testing.T) {
	// Before any update, time should be zero
	when := security.PatternsUpdateTime()
	if !when.IsZero() {
		t.Logf("time not zero before update: %v", when)
	}
}

func TestShouldUpdate(t *testing.T) {
	// Fresh state has never been fetched, so should update
	security.PatternUpdateMu.Lock()
	security.SetPatternUpdateTime(time.Time{})
	security.PatternUpdateMu.Unlock()

	if !security.ShouldUpdate() {
		t.Error("should update when never fetched")
	}

	// After a recent fetch, should not update
	security.PatternUpdateMu.Lock()
	security.SetPatternUpdateTime(time.Now())
	security.PatternUpdateMu.Unlock()

	if security.ShouldUpdate() {
		t.Error("should not update after recent fetch")
	}

	// After an old fetch, should update
	security.PatternUpdateMu.Lock()
	security.SetPatternUpdateTime(time.Now().Add(-48 * time.Hour))
	security.PatternUpdateMu.Unlock()

	if !security.ShouldUpdate() {
		t.Error("should update when cache is stale")
	}
}

func TestCheckPatternUpdate_Stale(t *testing.T) {
	original := security.Patterns
	defer func() { security.Patterns = original }()

	// Force stale state
	security.PatternUpdateMu.Lock()
	security.SetPatternUpdateTime(time.Now().Add(-48 * time.Hour))
	security.PatternUpdateMu.Unlock()

	// This may succeed or fail depending on network; either way should not panic
	security.CheckPatternUpdate()
}

func TestCheckPatternUpdate_Fresh(t *testing.T) {
	// With fresh patterns, CheckPatternUpdate should return early
	security.PatternUpdateMu.Lock()
	security.SetPatternUpdateTime(time.Now())
	security.PatternUpdateMu.Unlock()

	// Should not attempt to fetch
	result := security.CheckPatternUpdate()
	if result {
		t.Error("should not return true when cache is fresh")
	}
}

func TestStartAutoUpdater(t *testing.T) {
	// Start with a very short interval to test it doesn't panic
	// We just verify it starts without error
	security.StartAutoUpdater(1 * time.Millisecond)
	// Give the goroutine a moment to run
	time.Sleep(50 * time.Millisecond)
}
