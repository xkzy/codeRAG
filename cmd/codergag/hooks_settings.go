package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// hookMarker identifies hook commands written by codergag. Only entries whose
// command contains it are ever added, matched or removed.
const hookMarker = "codergag hook"

var managedHookEvents = []string{"SessionStart", "UserPromptSubmit"}

func defaultSettingsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".claude", "settings.json")
}

func isOurs(h any) bool {
	m, _ := h.(map[string]any)
	cmd, _ := m["command"].(string)
	return strings.Contains(cmd, hookMarker)
}

func entryHasOurs(entry any) bool {
	m, _ := entry.(map[string]any)
	inner, _ := m["hooks"].([]any)
	for _, h := range inner {
		if isOurs(h) {
			return true
		}
	}
	return false
}

// mergeHooks adds one tagged entry per managed event unless one exists.
func mergeHooks(settings map[string]any) bool {
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	changed := false
	for _, ev := range managedHookEvents {
		entries, _ := hooks[ev].([]any)
		have := false
		for _, e := range entries {
			if entryHasOurs(e) {
				have = true
				break
			}
		}
		if have {
			continue
		}
		hooks[ev] = append(entries, map[string]any{"hooks": []any{
			map[string]any{"type": "command", "command": hookMarker + " " + ev},
		}})
		changed = true
	}
	if changed {
		settings["hooks"] = hooks
	}
	return changed
}

// removeHooks strips tagged hook commands, dropping entries and event keys
// that become empty.
func removeHooks(settings map[string]any) bool {
	hooks, _ := settings["hooks"].(map[string]any)
	changed := false
	for ev, v := range hooks {
		entries, _ := v.([]any)
		var kept []any
		for _, e := range entries {
			m, _ := e.(map[string]any)
			inner, _ := m["hooks"].([]any)
			var keepInner []any
			for _, h := range inner {
				if isOurs(h) {
					changed = true
					continue
				}
				keepInner = append(keepInner, h)
			}
			if len(keepInner) == len(inner) {
				kept = append(kept, e)
			} else if len(keepInner) > 0 {
				m["hooks"] = keepInner
				kept = append(kept, m)
			}
		}
		if len(kept) == 0 && len(entries) > 0 {
			delete(hooks, ev)
		} else if len(kept) != len(entries) {
			hooks[ev] = kept
		}
	}
	if changed && len(hooks) == 0 {
		delete(settings, "hooks")
	}
	return changed
}

// updateSettingsFile adds (or with remove, strips) codergag's hooks in a
// Claude Code settings file. Unparseable files are never modified.
func updateSettingsFile(path string, remove bool) (bool, error) {
	settings := map[string]any{}
	mode := os.FileMode(0o600)
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if info, serr := os.Stat(path); serr == nil {
			mode = info.Mode().Perm()
		}
		if len(strings.TrimSpace(string(b))) > 0 {
			if err := json.Unmarshal(b, &settings); err != nil {
				return false, err
			}
		}
	case os.IsNotExist(err):
		if remove {
			return false, nil
		}
	default:
		return false, err
	}

	var changed bool
	if remove {
		changed = removeHooks(settings)
	} else {
		changed = mergeHooks(settings)
	}
	if !changed {
		return false, nil
	}
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*.json")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(out, '\n')); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return false, err
	}
	return true, os.Rename(tmp.Name(), path)
}
