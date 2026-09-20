package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const foreign = `{
  "model": "opus",
  "hooks": {
    "SessionStart": [{"hooks": [{"type": "command", "command": "other-tool start"}]}],
    "Stop": [{"hooks": [{"type": "command", "command": "notify"}]}]
  }
}`

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestUpdateSettingsAddIsIdempotentAndPreservesForeign(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "settings.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(foreign), 0o600)

	changed, err := updateSettingsFile(path, false)
	if err != nil || !changed {
		t.Fatalf("first add: changed=%v err=%v", changed, err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Count(string(raw), hookMarker) != 2 {
		t.Fatalf("want exactly 2 tagged commands:\n%s", raw)
	}
	for _, keep := range []string{"other-tool start", "notify", `"model"`} {
		if !strings.Contains(string(raw), keep) {
			t.Errorf("foreign content %q lost", keep)
		}
	}
	changed, err = updateSettingsFile(path, false)
	if err != nil || changed {
		t.Fatalf("second add must be a no-op: changed=%v err=%v", changed, err)
	}
}

func TestUpdateSettingsCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude", "settings.json")
	if changed, err := updateSettingsFile(path, false); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	hooks := readJSON(t, path)["hooks"].(map[string]any)
	if len(hooks["SessionStart"].([]any)) != 1 || len(hooks["UserPromptSubmit"].([]any)) != 1 {
		t.Fatalf("unexpected hooks: %v", hooks)
	}
}

func TestUpdateSettingsRemoveOnlyOurs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(foreign), 0o600)
	updateSettingsFile(path, false)

	changed, err := updateSettingsFile(path, true)
	if err != nil || !changed {
		t.Fatalf("remove: changed=%v err=%v", changed, err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), hookMarker) {
		t.Fatalf("tagged hooks remain:\n%s", raw)
	}
	m := readJSON(t, path)
	hooks := m["hooks"].(map[string]any)
	if _, ok := hooks["UserPromptSubmit"]; ok {
		t.Error("emptied event key should be deleted")
	}
	if len(hooks["SessionStart"].([]any)) != 1 || len(hooks["Stop"].([]any)) != 1 || m["model"] != "opus" {
		t.Fatalf("foreign settings damaged: %v", m)
	}
	if changed, _ := updateSettingsFile(path, true); changed {
		t.Fatal("second remove must be a no-op")
	}
}

func TestUpdateSettingsRefusesInvalidJSONAndMissingRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte("{not json"), 0o600)
	if _, err := updateSettingsFile(path, false); err == nil {
		t.Fatal("invalid JSON must be an error")
	}
	if b, _ := os.ReadFile(path); string(b) != "{not json" {
		t.Fatal("invalid file must be left untouched")
	}
	if changed, err := updateSettingsFile(filepath.Join(t.TempDir(), "none.json"), true); err != nil || changed {
		t.Fatalf("remove on missing file: changed=%v err=%v", changed, err)
	}
}
