package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codergag/internal/services"
)

// TestRunDoctorSmoke runs doctor against an in-memory graph and checks that
// it returns 0 (no failed checks) and emits JSON.
func TestRunDoctorSmoke(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	dir := t.TempDir()
	// write a trivial go file so the storage-path check has something to look at
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// runDoctor parses its own flag set; pass --json so we get machine output.
	code := runDoctor(app, []string{"--json"})
	if code != 0 {
		t.Fatalf("doctor returned %d, want 0", code)
	}
}

// TestRunSessionListEmpty verifies `session list` against an empty graph
// returns 0 and prints a count of zero.
func TestRunSessionListEmpty(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	code := runSession(app, []string{"list", "--json"})
	if code != 0 {
		t.Fatalf("session list returned %d, want 0", code)
	}
}

// TestRunSessionFlushEmpty verifies flush on an empty graph is a no-op that
// exits 0.
func TestRunSessionFlushEmpty(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	code := runSession(app, []string{"flush", "--json"})
	if code != 0 {
		t.Fatalf("session flush returned %d, want 0", code)
	}
}

// TestRunSessionUnknownSubcommand returns 2 for an unknown subcommand.
func TestRunSessionUnknownSubcommand(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	code := runSession(app, []string{"bogus"})
	if code != 2 {
		t.Fatalf("session bogus returned %d, want 2", code)
	}
}

// TestRunInjectWritesFile verifies inject writes a codebase map and exits 0.
func TestRunInjectWritesFile(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	// Register a project so inject has something to describe.
	if _, err := app.Graph.UpsertNode("Project", map[string]any{"id": "project-test", "path": "."},
		map[string]any{}); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "CLAUDE.md")
	code := runInject(app, []string{"--project", "project-test", "--file", out})
	if code != 0 {
		t.Fatalf("inject returned %d, want 0", code)
	}
	b, err := os.ReadFile(out)
	if err != nil || len(b) == 0 {
		t.Fatalf("inject did not write %s: err=%v", out, err)
	}
}

// TestRunInjectPreservesExistingContent verifies inject uses block markers to
// merge into an existing CLAUDE.md without destroying prior content.
func TestRunInjectPreservesExistingContent(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	if _, err := app.Graph.UpsertNode("Project", map[string]any{"id": "p1", "path": "."},
		map[string]any{}); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "CLAUDE.md")
	if err := os.WriteFile(out, []byte("# My Project\n\nSome notes here.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code := runInject(app, []string{"--project", "p1", "--file", out})
	if code != 0 {
		t.Fatalf("inject returned %d, want 0", code)
	}
	b, _ := os.ReadFile(out)
	content := string(b)
	if !strings.Contains(content, "# My Project") {
		t.Fatal("inject should preserve existing content")
	}
	if !strings.Contains(strings.ToLower(content), "codebase map") {
		t.Fatal("inject should add its managed block")
	}

	// Run again — should be idempotent (no duplicate blocks).
	code = runInject(app, []string{"--project", "p1", "--file", out})
	if code != 0 {
		t.Fatalf("second inject returned %d, want 0", code)
	}
	b2, _ := os.ReadFile(out)
	content2 := string(b2)
	occurrences := strings.Count(content2, "<!-- codergag:begin -->")
	if occurrences != 1 {
		t.Fatalf("expected 1 block marker, got %d", occurrences)
	}
}

// TestRunRegisterWritesInstructions verifies register-instructions writes the
// instructions file and exits 0.
func TestRunRegisterWritesInstructions(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	out := filepath.Join(t.TempDir(), "instructions.md")
	code := runRegisterInstructions(app, []string{"--file", out})
	if code != 0 {
		t.Fatalf("register-instructions returned %d, want 0", code)
	}
	if info, err := os.Stat(out); err != nil || info.Size() == 0 {
		t.Fatalf("register-instructions did not write %s: err=%v", out, err)
	}
}

// TestRunRegisterInjectsCLAUDEMD verifies register-instructions injects an
// @import block into ~/.claude/CLAUDE.md (mirroring cctx's behavior).
func TestRunRegisterInjectsCLAUDEMD(t *testing.T) {
	home := t.TempDir()
	os.Setenv("HOME", home)
	defer os.Unsetenv("HOME")

	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	out := filepath.Join(home, ".codergag", "instructions.md")
	code := runRegisterInstructions(app, []string{"--file", out})
	if code != 0 {
		t.Fatalf("register-instructions returned %d, want 0", code)
	}

	claudeMD := filepath.Join(home, ".claude", "CLAUDE.md")
	b, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("CLAUDE.md not written: %v", err)
	}
	content := string(b)
	if !strings.Contains(content, "<!-- codergag:instructions:begin -->") {
		t.Fatal("CLAUDE.md should contain codergag begin marker")
	}
	if !strings.Contains(content, "@~/.codergag/instructions.md") {
		t.Fatal("CLAUDE.md should contain the @import line")
	}

	// Run again — should be idempotent (one block, not two).
	code = runRegisterInstructions(app, []string{"--file", out})
	if code != 0 {
		t.Fatalf("second register-instructions returned %d, want 0", code)
	}
	b2, _ := os.ReadFile(claudeMD)
	if strings.Count(string(b2), "<!-- codergag:instructions:begin -->") != 1 {
		t.Fatal("CLAUDE.md should have exactly one block after re-run")
	}
}

// TestRunDaemonStatus reports the daemon state.
func TestRunDaemonStatus(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	code := runDaemon(app, []string{"status"})
	if code != 0 {
		t.Fatalf("daemon status returned %d, want 0", code)
	}
}

// TestRunModelList returns 0 and an empty model list.
func TestRunModelList(t *testing.T) {
	code := runModel([]string{"list"})
	if code != 0 {
		t.Fatalf("model list returned %d, want 0", code)
	}
}

// TestRunConfigShow loads the default config and prints it.
func TestRunConfigShow(t *testing.T) {
	os.Setenv("CODERAG_CONFIG", filepath.Join(t.TempDir(), "nonexistent.yaml"))
	defer os.Unsetenv("CODERAG_CONFIG")
	code := runConfig([]string{"show"})
	if code != 0 {
		t.Fatalf("config show returned %d, want 0", code)
	}
}

// TestRunConfigGetSet round-trips a value through the config file.
func TestRunConfigGetSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codergag.yaml")
	os.Setenv("CODERAG_CONFIG", path)
	defer os.Unsetenv("CODERAG_CONFIG")

	if code := runConfig([]string{"set", "watch.interval", "45s"}); code != 0 {
		t.Fatalf("config set returned %d, want 0", code)
	}
	if code := runConfig([]string{"get", "watch.interval"}); code != 0 {
		t.Fatalf("config get returned %d, want 0", code)
	}
}

// TestRunUninstallNoOp returns 1 when there is nothing to remove.
func TestRunUninstallNoOp(t *testing.T) {
	home := t.TempDir()
	os.Setenv("HOME", home)
	defer os.Unsetenv("HOME")
	os.Setenv("CODERAG_CONFIG", filepath.Join(home, "codergag.yaml"))
	defer os.Unsetenv("CODERAG_CONFIG")

	code := runUninstall([]string{})
	if code != 1 {
		t.Fatalf("uninstall returned %d, want 1 (nothing to remove)", code)
	}
}

// TestRunUninstallCleansCLAUDEMD verifies uninstall removes the instructions
// block from ~/.claude/CLAUDE.md that register-instructions created.
func TestRunUninstallCleansCLAUDEMD(t *testing.T) {
	home := t.TempDir()
	os.Setenv("HOME", home)
	defer os.Unsetenv("HOME")
	os.Setenv("CODERAG_CONFIG", filepath.Join(home, "codergag.yaml"))
	defer os.Unsetenv("CODERAG_CONFIG")

	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	// Register first (creates CLAUDE.md block + instructions file)
	out := filepath.Join(home, ".codergag", "instructions.md")
	if code := runRegisterInstructions(app, []string{"--file", out}); code != 0 {
		t.Fatalf("register-instructions returned %d, want 0", code)
	}
	claudeMD := filepath.Join(home, ".claude", "CLAUDE.md")
	if b, _ := os.ReadFile(claudeMD); !strings.Contains(string(b), "<!-- codergag:instructions:begin -->") {
		t.Fatal("CLAUDE.md should have block before uninstall")
	}

	// Uninstall should remove both the file and the CLAUDE.md block
	code := runUninstall([]string{})
	if code != 0 {
		t.Fatalf("uninstall returned %d, want 0", code)
	}
	// CLAUDE.md should be deleted (it only had the codergag block)
	if _, err := os.Stat(claudeMD); !os.IsNotExist(err) {
		t.Fatal("CLAUDE.md should be removed after uninstall")
	}
}

// TestRunSetupShim exits 0 and prints the registration snippet.
func TestRunSetupShim(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	code := runSetup(app, []string{"--yes"})
	if code != 0 {
		t.Fatalf("setup returned %d, want 0", code)
	}
}

// TestRunIndexStatus reports the indexed projects.
func TestRunIndexStatus(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	code := runIndex(app, []string{"status"})
	if code != 0 {
		t.Fatalf("index status returned %d, want 0", code)
	}
}

// TestRunIndexUnknownSubcommand returns 2.
func TestRunIndexUnknownSubcommand(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	code := runIndex(app, []string{"bogus"})
	if code != 2 {
		t.Fatalf("index bogus returned %d, want 2", code)
	}
}

// TestRunSessionFlushBySessionID verifies flush only removes the named session.
func TestRunSessionFlushBySessionID(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	for _, id := range []string{"sess-a", "sess-b", "sess-c"} {
		if _, err := app.Graph.UpsertNode("UsageSession",
			map[string]any{"project_id": services.SystemProject, "session_id": id},
			map[string]any{"started_at": id, "usage": `{"x":1}`}); err != nil {
			t.Fatal(err)
		}
	}
	// Flush only sess-b
	code := runSession(app, []string{"flush", "--session-id", "sess-b", "--json"})
	if code != 0 {
		t.Fatalf("flush --session-id returned %d, want 0", code)
	}
	remaining, _ := app.Graph.FindNodes("UsageSession", map[string]any{"project_id": services.SystemProject})
	for _, n := range remaining {
		if services.StrProp(n, "session_id") == "sess-b" {
			t.Fatal("sess-b should have been flushed")
		}
	}
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining sessions, got %d", len(remaining))
	}
}

// TestRunSessionExportMD verifies the --format md and --out flags work together.
func TestRunSessionExportMD(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	if _, err := app.Graph.UpsertNode("UsageSession",
		map[string]any{"project_id": services.SystemProject, "session_id": "test-session"},
		map[string]any{"started_at": "test-session", "usage": `{"find_function":{"calls":5,"errors":1,"total_ms":250,"raw_tokens":100,"shaped_tokens":50}}`}); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "export.md")
	code := runSession(app, []string{"export", "--session-id", "test-session", "--format", "md", "--out", out})
	if code != 0 {
		t.Fatalf("export md returned %d, want 0", code)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	if !strings.Contains(content, "codeRAG session test-session") {
		t.Fatalf("export md missing session header: %s", content)
	}
	if !strings.Contains(content, "find_function") {
		t.Fatal("export md should contain tool name")
	}
}

// TestRunSessionExportJSONFormat verifies the default json output still works.
func TestRunSessionExportJSONFormat(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	code := runSession(app, []string{"export"})
	if code != 0 {
		t.Fatalf("export json returned %d, want 0", code)
	}
}

// TestRunSessionExportBadFormat returns 2 for unsupported formats.
func TestRunSessionExportBadFormat(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	code := runSession(app, []string{"export", "--format", "yaml"})
	if code != 2 {
		t.Fatalf("export bad format returned %d, want 2", code)
	}
}
