package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnsafeProjectRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if !unsafeProjectRoot(home) {
		t.Fatal("home directory must be unsafe")
	}
	if !unsafeProjectRoot(string(filepath.Separator)) {
		t.Fatal("filesystem root must be unsafe")
	}
	if unsafeProjectRoot(filepath.Join(home, "proj")) {
		t.Fatal("subdirectory of home must be allowed")
	}
}

func TestDetectorSkipsHomeAsProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// home looks like a project (manifest present) but must not be registered.
	if err := os.WriteFile(filepath.Join(home, "Makefile"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(home, "work", "app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "go.mod"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".cache", "junk"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".cache", "junk", "go.mod"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	found := NewProjectDetector([]string{home}, 3, nil).Detect()
	if len(found) != 1 {
		t.Fatalf("want 1 project, got %d: %+v", len(found), found)
	}
	want, _ := filepath.EvalSymlinks(proj)
	if found[0].Root != want {
		t.Fatalf("got root %q, want %q", found[0].Root, want)
	}
}
