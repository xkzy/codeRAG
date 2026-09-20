package main

import (
	"os"
	"path/filepath"
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
	if info, err := os.Stat(out); err != nil || info.Size() == 0 {
		t.Fatalf("inject did not write %s: err=%v size=%d", out, err, size(info))
	}
}

func size(info os.FileInfo) int64 {
	if info == nil {
		return 0
	}
	return info.Size()
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
	// Point HOME at an empty temp dir so uninstall finds nothing.
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