package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"codergag/internal/ctxstore"
	"codergag/internal/services"
)

func hookApp(t *testing.T) *services.Application {
	t.Helper()
	t.Setenv("CODERAG_HOME", t.TempDir())
	app := services.ApplicationInMemory()
	app.Graph.UpsertNode("Project", map[string]any{"id": "p"}, map[string]any{"project_id": "p", "path": "/w/p"})
	st := ctxstore.New(app)
	st.Add("p", ctxstore.Record{Kind: "decision", Title: "Use SQLite", Body: "single binary", Pinned: true})
	st.Add("p", ctxstore.Record{Title: "packet decoder", Body: "splits the stream"})
	return app
}

func runHookT(t *testing.T, app *services.Application, event, stdin string) (int, map[string]any, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := hookRun(app, event, strings.NewReader(stdin), &out, &errb)
	var parsed map[string]any
	if out.Len() > 0 {
		if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
			t.Fatalf("hook stdout must be valid JSON: %q (%v)", out.String(), err)
		}
	}
	return code, parsed, errb.String()
}

func additional(t *testing.T, parsed map[string]any, event string) string {
	t.Helper()
	hso, _ := parsed["hookSpecificOutput"].(map[string]any)
	if hso["hookEventName"] != event {
		t.Fatalf("hookEventName = %v, want %s", hso["hookEventName"], event)
	}
	s, _ := hso["additionalContext"].(string)
	return s
}

func TestHookSessionStartInjectsDigest(t *testing.T) {
	app := hookApp(t)
	code, out, _ := runHookT(t, app, "SessionStart", `{"session_id":"s1","cwd":"/w/p/sub"}`)
	if code != 0 {
		t.Fatal("hook must exit 0")
	}
	if txt := additional(t, out, "SessionStart"); !strings.Contains(txt, "Use SQLite") {
		t.Fatalf("digest missing pinned record: %s", txt)
	}
}

func TestHookUserPromptSubmitDedupesWithinSession(t *testing.T) {
	app := hookApp(t)
	in := `{"session_id":"s1","cwd":"/w/p","prompt":"packet"}`
	_, out, _ := runHookT(t, app, "UserPromptSubmit", in)
	if txt := additional(t, out, "UserPromptSubmit"); !strings.Contains(txt, "packet decoder") {
		t.Fatalf("expected relevant record: %s", txt)
	}
	_, out2, _ := runHookT(t, app, "UserPromptSubmit", in)
	if out2 != nil {
		t.Fatalf("second identical prompt must add nothing, got %v", out2)
	}
	_, out3, _ := runHookT(t, app, "UserPromptSubmit", `{"session_id":"s2","cwd":"/w/p","prompt":"packet"}`)
	if out3 == nil {
		t.Fatal("a new session must get the record again")
	}
}

func TestHookNeverFails(t *testing.T) {
	app := hookApp(t)
	for _, tc := range []struct{ event, stdin string }{
		{"SessionStart", "not json"},
		{"SessionStart", ""},
		{"SessionStart", `{"cwd":"/not/indexed"}`},
		{"UserPromptSubmit", `{"cwd":"/w/p"}`},
		{"PostToolUse", `{"cwd":"/w/p"}`},
		{"Stop", `{}`},
		{"Bogus", `{}`},
	} {
		code, out, _ := runHookT(t, app, tc.event, tc.stdin)
		if code != 0 || out != nil {
			t.Errorf("%s %q: code=%d out=%v; want silent exit 0", tc.event, tc.stdin, code, out)
		}
	}
}
