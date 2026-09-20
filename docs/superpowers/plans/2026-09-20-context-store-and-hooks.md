# Context Store and Hook Injection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `codergag ctx` record CLI and Claude Code `SessionStart`/`UserPromptSubmit` hooks that inject a token-budgeted context digest, ported conceptually from agentctx.

**Architecture:** Records are the existing graph `Memory` nodes (`services.MemoryService`), with `kind` in decision|convention|task|note and a new `pinned` property. A new `internal/ctxstore` package wraps them and builds digests. `cmd/codergag` gets `ctx` and `hook` commands, and `setup --hooks` / `uninstall` merge and remove tagged entries in `~/.claude/settings.json`.

**Tech Stack:** Go 1.25, stdlib only, existing `internal/graph`, `internal/services`, `internal/search`.

**Spec:** `docs/superpowers/specs/2026-09-20-context-store-and-hooks-design.md`

## Global Constraints

- No new dependencies; `go.mod` unchanged.
- No new database: reuse `Memory` graph nodes (identity is `project_id` + `title`, so adding a record with an existing title in the same project updates it).
- `codergag hook` must always exit 0 and never block a session; errors go to stderr.
- Token estimate is `(len(s)+3)/4` everywhere (`ctxstore.EstimateTokens`).
- Default budgets: session digest 1500 tokens, prompt digest 500 tokens.
- Hook entries written to settings are identified by the substring `codergag hook` in the command; foreign hooks are never touched.
- State lives under `$CODERAG_HOME` if set, else `~/.codergag`.
- agentctx is Elastic License 2.0: do not copy its source.
- Verification at the end: `go build ./...`, `go vet ./...`, `go test ./... -count=1`.

## File Structure

- Create `internal/ctxstore/store.go`: `Record`, `Store`, `Add/Get/List/Search/SetPinned/Delete/Reset`, `ProjectForDir`.
- Create `internal/ctxstore/digest.go`: `EstimateTokens`, `SessionDigest`, `PromptDigest`, `Profile`, seen-state helpers.
- Create `internal/ctxstore/store_test.go`, `internal/ctxstore/digest_test.go`.
- Modify `internal/config/config.go`: `ContextConfig` and `Config.Context`.
- Create `cmd/codergag/ctx.go` + `ctx_test.go`: `ctx` subcommands.
- Create `cmd/codergag/hook.go` + `hook_test.go`: `hook` command.
- Create `cmd/codergag/hooks_settings.go` + `hooks_settings_test.go`: settings.json merge/remove.
- Modify `cmd/codergag/main.go` (dispatch, usage, daemon-disable list, `activeCfg`), `setup.go` (`-hooks`), `uninstall.go`, `main_test.go`.
- Modify `README.md` and the `instructionsMD` text in `cmd/codergag/register.go`.

---

### Task 1: Record store over Memory nodes

**Files:**
- Create: `internal/ctxstore/store.go`
- Test: `internal/ctxstore/store_test.go`

**Interfaces:**
- Produces:
  - `type Record struct { ID, Kind, Title, Body, Source string; Pinned bool; CreatedAt string; Score float64 }`
  - `var Kinds = []string{"decision","convention","task","note"}`; `func ValidKind(k string) bool`
  - `func New(app *services.Application) *Store`
  - `func (s *Store) Add(project string, r Record) (Record, error)`
  - `func (s *Store) Get(id string) (Record, error)`
  - `func (s *Store) List(project, kind string) ([]Record, error)` (newest first, archived skipped, `kind==""` means all)
  - `func (s *Store) Search(project, query string, limit int) ([]Record, error)`
  - `func (s *Store) SetPinned(id string, pinned bool) error`
  - `func (s *Store) Delete(id string) error`
  - `func (s *Store) Reset(project string) (int, error)`
  - `func ProjectForDir(app *services.Application, dir string) (string, error)` (longest matching Project `path` ancestor of `dir`; `""` if none)

- [ ] **Step 1: Write the failing test**

```go
// internal/ctxstore/store_test.go
package ctxstore

import (
	"testing"

	"codergag/internal/services"
)

func newStore(t *testing.T) (*Store, *services.Application) {
	t.Helper()
	app := services.ApplicationInMemory()
	return New(app), app
}

func TestAddListSearchGetDelete(t *testing.T) {
	s, _ := newStore(t)
	a, err := s.Add("p", Record{Kind: "decision", Title: "Use SQLite", Body: "single binary, no server", Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.Kind != "decision" || !a.Pinned || a.Source != "manual" {
		t.Fatalf("unexpected record: %+v", a)
	}
	if _, err := s.Add("p", Record{Title: "Logging", Body: "use slog"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("other", Record{Title: "Elsewhere", Body: "other project"}); err != nil {
		t.Fatal(err)
	}

	all, _ := s.List("p", "")
	if len(all) != 2 {
		t.Fatalf("want 2 records in p, got %d", len(all))
	}
	if dec, _ := s.List("p", "decision"); len(dec) != 1 || dec[0].Title != "Use SQLite" {
		t.Fatalf("kind filter failed: %+v", dec)
	}
	hits, _ := s.Search("p", "sqlite", 5)
	if len(hits) != 1 || hits[0].ID != a.ID || hits[0].Body == "" {
		t.Fatalf("search failed: %+v", hits)
	}
	got, err := s.Get(a.ID)
	if err != nil || got.Title != "Use SQLite" {
		t.Fatalf("get failed: %+v %v", got, err)
	}
	if err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(a.ID); err == nil {
		t.Fatal("deleted record must not be found")
	}
}

func TestAddValidation(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Add("p", Record{Title: "  "}); err == nil {
		t.Fatal("empty title must fail")
	}
	if _, err := s.Add("p", Record{Title: "x", Kind: "bogus"}); err == nil {
		t.Fatal("invalid kind must fail")
	}
}

func TestSetPinnedAndReset(t *testing.T) {
	s, _ := newStore(t)
	r, _ := s.Add("p", Record{Title: "a", Body: "b"})
	if err := s.SetPinned(r.ID, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(r.ID); !got.Pinned {
		t.Fatal("record should be pinned")
	}
	s.Add("q", Record{Title: "keep", Body: "other project"})
	n, err := s.Reset("p")
	if err != nil || n != 1 {
		t.Fatalf("reset removed %d, err %v", n, err)
	}
	if left, _ := s.List("q", ""); len(left) != 1 {
		t.Fatal("reset must not touch other projects")
	}
}

func TestProjectForDir(t *testing.T) {
	_, app := newStore(t)
	app.Graph.UpsertNode("Project", map[string]any{"id": "outer"}, map[string]any{"project_id": "outer", "path": "/w/outer"})
	app.Graph.UpsertNode("Project", map[string]any{"id": "inner"}, map[string]any{"project_id": "inner", "path": "/w/outer/inner"})
	cases := map[string]string{
		"/w/outer":           "outer",
		"/w/outer/inner/sub": "inner",
		"/w/outer2":          "",
		"/elsewhere":         "",
	}
	for dir, want := range cases {
		got, err := ProjectForDir(app, dir)
		if err != nil || got != want {
			t.Errorf("ProjectForDir(%q) = %q, %v; want %q", dir, got, err, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ctxstore/ -count=1`
Expected: FAIL (package does not compile: `New`, `Record`, ... undefined)

- [ ] **Step 3: Write minimal implementation**

```go
// internal/ctxstore/store.go
package ctxstore

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"codergag/internal/graph"
	"codergag/internal/models"
	"codergag/internal/services"
)

// Kinds are the record kinds `ctx add` accepts. Memories created through the
// MCP tools may carry other kinds; they are listed and searched unchanged.
var Kinds = []string{"decision", "convention", "task", "note"}

func ValidKind(k string) bool {
	for _, v := range Kinds {
		if v == k {
			return true
		}
	}
	return false
}

type Record struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	Source    string  `json:"source"`
	Pinned    bool    `json:"pinned"`
	CreatedAt string  `json:"created_at"`
	Score     float64 `json:"score,omitempty"`
}

// Store is a thin layer over the graph's Memory nodes.
type Store struct {
	g   graph.GraphRepository
	mem *services.MemoryService
}

func New(app *services.Application) *Store {
	return &Store{g: app.Graph, mem: app.Memory}
}

func fromNode(n *models.Node) Record {
	pinned, _ := n.Properties["pinned"].(bool)
	return Record{
		ID:        n.ID,
		Kind:      services.StrProp(n, "kind"),
		Title:     services.StrProp(n, "title"),
		Body:      services.StrProp(n, "content"),
		Source:    services.StrProp(n, "source"),
		Pinned:    pinned,
		CreatedAt: services.StrProp(n, "created_at"),
	}
}

func fromMap(m map[string]any) Record {
	str := func(k string) string { s, _ := m[k].(string); return s }
	pinned, _ := m["pinned"].(bool)
	score, _ := m["score"].(float64)
	return Record{
		ID: str("id"), Kind: str("kind"), Title: str("title"), Body: str("content"),
		Source: str("source"), Pinned: pinned, CreatedAt: str("created_at"), Score: score,
	}
}

func archived(n *models.Node) bool {
	b, _ := n.Properties["archived"].(bool)
	return b
}

// Add stores a record. An existing record with the same title in the same
// project is updated (Memory identity is project_id + title).
func (s *Store) Add(project string, r Record) (Record, error) {
	r.Title = strings.TrimSpace(r.Title)
	if r.Title == "" {
		return Record{}, errors.New("title is required")
	}
	if r.Kind == "" {
		r.Kind = "note"
	}
	if !ValidKind(r.Kind) {
		return Record{}, fmt.Errorf("invalid kind %q (want one of %s)", r.Kind, strings.Join(Kinds, ", "))
	}
	if r.Source == "" {
		r.Source = "manual"
	}
	res, err := s.mem.Store(project, r.Title, r.Body, map[string]any{
		"kind": r.Kind, "source": r.Source, "auto_compact": false,
	})
	if err != nil {
		return Record{}, err
	}
	id, _ := res["id"].(string)
	if r.Pinned {
		if err := s.SetPinned(id, true); err != nil {
			return Record{}, err
		}
	}
	return s.Get(id)
}

func (s *Store) Get(id string) (Record, error) {
	n, err := s.g.GetNode(id)
	if err != nil {
		return Record{}, err
	}
	if n == nil || n.Kind != "Memory" {
		return Record{}, graph.ErrNotFound
	}
	return fromNode(n), nil
}

func (s *Store) List(project, kind string) ([]Record, error) {
	nodes, err := s.g.FindNodes("Memory", map[string]any{"project_id": project})
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, n := range nodes {
		if archived(n) {
			continue
		}
		r := fromNode(n)
		if kind != "" && r.Kind != kind {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func (s *Store) Search(project, query string, limit int) ([]Record, error) {
	rows, err := s.mem.Search(project, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(rows))
	for _, m := range rows {
		out = append(out, fromMap(m))
	}
	return out, nil
}

func (s *Store) SetPinned(id string, pinned bool) error {
	n, err := s.g.GetNode(id)
	if err != nil {
		return err
	}
	if n == nil || n.Kind != "Memory" {
		return graph.ErrNotFound
	}
	_, err = s.g.UpsertNode("Memory", map[string]any{
		"project_id": n.Properties["project_id"],
		"title":      n.Properties["title"],
	}, map[string]any{"pinned": pinned})
	return err
}

func (s *Store) Delete(id string) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	return s.g.RemoveNodes([]string{id})
}

// Reset deletes every Memory node of the project (including archived ones and
// those created through the MCP tools) and returns how many were removed.
func (s *Store) Reset(project string) (int, error) {
	nodes, err := s.g.FindNodes("Memory", map[string]any{"project_id": project})
	if err != nil {
		return 0, err
	}
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return len(ids), s.g.RemoveNodes(ids)
}

// ProjectForDir returns the id of the indexed project whose path is the
// longest ancestor of (or equal to) dir, or "" when none matches.
func ProjectForDir(app *services.Application, dir string) (string, error) {
	nodes, err := app.Graph.FindNodes("Project", nil)
	if err != nil {
		return "", err
	}
	dir = filepath.Clean(dir)
	best, bestLen := "", -1
	for _, n := range nodes {
		p := filepath.Clean(services.StrProp(n, "path"))
		if p == "." || p == "" {
			continue
		}
		if dir == p || strings.HasPrefix(dir, p+string(filepath.Separator)) {
			if len(p) > bestLen {
				best, bestLen = services.StrProp(n, "id"), len(p)
			}
		}
	}
	return best, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ctxstore/ -count=1`
Expected: PASS. If `GetNode` on a removed id returns `(nil, nil)` instead of an error, `Get` already returns `graph.ErrNotFound` via the nil check; if it panics on a missing id, fix by treating the error as not-found.

- [ ] **Step 5: Commit**

```bash
git add internal/ctxstore/store.go internal/ctxstore/store_test.go
git commit -m "feat(ctxstore): record store over Memory nodes"
```

---

### Task 2: Digests, profile, per-session seen state, budgets

**Files:**
- Create: `internal/ctxstore/digest.go`
- Test: `internal/ctxstore/digest_test.go`
- Modify: `internal/config/config.go` (add `ContextConfig`, field `Context` on `Config`)
- Test: `internal/config/config_test.go` (append one test)

**Interfaces:**
- Consumes: `Store.List`, `Store.Search`, `Record` (Task 1).
- Produces:
  - `func EstimateTokens(s string) int`
  - `type Profile map[string]string`; `func LoadProfile(path string) (Profile, error)` (missing file → empty, nil); `func (p Profile) Save(path string) error`
  - `func (s *Store) SessionDigest(project string, budget int, profile Profile, codebase string) (string, error)`
  - `func (s *Store) PromptDigest(project, prompt string, budget int, seen map[string]bool) (text string, ids []string, err error)`
  - `func LoadSeen(dir, sessionID string) map[string]bool`; `func SaveSeen(dir, sessionID string, seen map[string]bool) error`
  - `config.ContextConfig{SessionBudget, PromptBudget int}` with methods `Session() int` (default 1500) and `Prompt() int` (default 500); `Config.Context ContextConfig` (yaml `context`).

- [ ] **Step 1: Write the failing tests**

```go
// internal/ctxstore/digest_test.go
package ctxstore

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	if EstimateTokens("") != 0 || EstimateTokens("abcd") != 1 || EstimateTokens("abcde") != 2 {
		t.Fatal("estimate must be ceil(len/4)")
	}
}

func TestSessionDigestOrderAndBudget(t *testing.T) {
	s, _ := newStore(t)
	s.Add("p", Record{Kind: "note", Title: "old note", Body: "n"})
	s.Add("p", Record{Kind: "decision", Title: "Use SQLite", Body: "single binary"})
	s.Add("p", Record{Kind: "convention", Title: "Pinned rule", Body: "always wrap errors", Pinned: true})

	out, err := s.SessionDigest("p", 1500, Profile{"style": "terse"}, "Codebase: 3 files")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"style: terse", "Pinned rule", "Use SQLite", "old note", "Codebase: 3 files"} {
		if !strings.Contains(out, want) {
			t.Errorf("digest missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "Pinned rule") > strings.Index(out, "Use SQLite") {
		t.Error("pinned records must come before decisions")
	}
	if strings.Index(out, "Use SQLite") > strings.Index(out, "old note") {
		t.Error("decisions must come before notes")
	}

	small, _ := s.SessionDigest("p", 30, nil, "")
	if EstimateTokens(small) > 30 {
		t.Errorf("digest exceeds budget: %d tokens", EstimateTokens(small))
	}
	if !strings.Contains(small, "Pinned rule") {
		t.Error("highest-priority record must survive a tight budget")
	}
}

func TestSessionDigestEmptyWhenNothingToSay(t *testing.T) {
	s, _ := newStore(t)
	out, err := s.SessionDigest("p", 1500, nil, "")
	if err != nil || out != "" {
		t.Fatalf("want empty digest, got %q, %v", out, err)
	}
}

func TestPromptDigestSkipsSeenAndPinned(t *testing.T) {
	s, _ := newStore(t)
	a, _ := s.Add("p", Record{Title: "packet decoder", Body: "splits the stream"})
	s.Add("p", Record{Title: "packet framing", Body: "length prefixed"})
	s.Add("p", Record{Title: "packet pinned", Body: "already in session digest", Pinned: true})

	text, ids, err := s.PromptDigest("p", "packet", 500, map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || strings.Contains(text, "packet pinned") {
		t.Fatalf("pinned records must be skipped: ids=%v\n%s", ids, text)
	}
	text2, ids2, _ := s.PromptDigest("p", "packet", 500, map[string]bool{a.ID: true})
	if len(ids2) != 1 || strings.Contains(text2, "packet decoder") {
		t.Fatalf("seen records must be skipped: %v\n%s", ids2, text2)
	}
	if txt, ids3, _ := s.PromptDigest("p", "zzzznomatch", 500, nil); txt != "" || len(ids3) != 0 {
		t.Fatalf("no match must yield empty digest, got %q", txt)
	}
}

func TestSeenStateRoundTripAndSanitizing(t *testing.T) {
	dir := t.TempDir()
	if got := LoadSeen(dir, "s1"); len(got) != 0 {
		t.Fatal("missing state must load empty")
	}
	if err := SaveSeen(dir, "../../evil", map[string]bool{"a": true}); err != nil {
		t.Fatal(err)
	}
	if !LoadSeen(dir, "../../evil")["a"] {
		t.Fatal("round trip failed")
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "hook-state", "*.json")); len(matches) != 1 {
		t.Fatalf("state must stay inside hook-state/, got %v", matches)
	}
}

func TestProfileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	if p, err := LoadProfile(path); err != nil || len(p) != 0 {
		t.Fatalf("missing profile must be empty: %v %v", p, err)
	}
	if err := (Profile{"editor": "vim"}).Save(path); err != nil {
		t.Fatal(err)
	}
	if p, _ := LoadProfile(path); p["editor"] != "vim" {
		t.Fatalf("round trip failed: %v", p)
	}
}
```

```go
// append to internal/config/config_test.go
func TestContextBudgetDefaults(t *testing.T) {
	c := Default().Context
	if c.Session() != 1500 || c.Prompt() != 500 {
		t.Fatalf("defaults: %d %d", c.Session(), c.Prompt())
	}
	p := writeConfig(t, "context:\n  session_budget: 900\n  prompt_budget: 200\n", ".yaml")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Context.Session() != 900 || cfg.Context.Prompt() != 200 {
		t.Fatalf("overrides not applied: %+v", cfg.Context)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ctxstore/ ./internal/config/ -count=1`
Expected: FAIL (undefined: `EstimateTokens`, `Context`, ...)

- [ ] **Step 3: Write minimal implementation**

In `internal/config/config.go`, add next to `GraphConfig` and add the field `Context ContextConfig \`yaml:"context,omitempty" xml:"context,omitempty"\`` as the last field of `Config`:

```go
// ContextConfig sets the token budgets for hook-injected context digests.
// Zero values fall back to the defaults.
type ContextConfig struct {
	SessionBudget int `yaml:"session_budget,omitempty" xml:"session_budget,omitempty"`
	PromptBudget  int `yaml:"prompt_budget,omitempty" xml:"prompt_budget,omitempty"`
}

func (c ContextConfig) Session() int {
	if c.SessionBudget > 0 {
		return c.SessionBudget
	}
	return 1500
}

func (c ContextConfig) Prompt() int {
	if c.PromptBudget > 0 {
		return c.PromptBudget
	}
	return 500
}
```

```go
// internal/ctxstore/digest.go
package ctxstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// EstimateTokens is the shared rough token estimate (ceil(len/4)).
func EstimateTokens(s string) int { return (len(s) + 3) / 4 }

// Profile holds global developer preferences.
type Profile map[string]string

func LoadProfile(path string) (Profile, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Profile{}, nil
	}
	if err != nil {
		return nil, err
	}
	p := Profile{}
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return p, nil
}

func (p Profile) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func recordLine(r Record) string {
	body := strings.Join(strings.Fields(r.Body), " ")
	if rs := []rune(body); len(rs) > 200 {
		body = string(rs[:200]) + "..."
	}
	line := "- [" + r.Kind + "] " + r.Title
	if body != "" {
		line += ": " + body
	}
	return line + "\n"
}

// pack appends lines in order while the running estimate stays within budget,
// skipping any line that does not fit. It returns "" when no line fit.
func pack(header string, budget int, lines []string) (string, []int) {
	used := EstimateTokens(header)
	var b strings.Builder
	b.WriteString(header)
	var included []int
	for i, l := range lines {
		if c := EstimateTokens(l); used+c <= budget {
			b.WriteString(l)
			used += c
			included = append(included, i)
		}
	}
	if len(included) == 0 {
		return "", nil
	}
	return b.String(), included
}

// SessionDigest builds the SessionStart digest: profile preferences, pinned
// records, decisions and conventions, other records, then a codebase line,
// each group newest first, trimmed to budget tokens.
func (s *Store) SessionDigest(project string, budget int, profile Profile, codebase string) (string, error) {
	recs, err := s.List(project, "")
	if err != nil {
		return "", err
	}
	var lines []string
	keys := make([]string, 0, len(profile))
	for k := range profile {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, "- preference "+k+": "+profile[k]+"\n")
	}
	group := func(pred func(Record) bool) {
		for _, r := range recs {
			if pred(r) {
				lines = append(lines, recordLine(r))
			}
		}
	}
	group(func(r Record) bool { return r.Pinned })
	group(func(r Record) bool { return !r.Pinned && (r.Kind == "decision" || r.Kind == "convention") })
	group(func(r Record) bool { return !r.Pinned && r.Kind != "decision" && r.Kind != "convention" })
	if codebase != "" {
		lines = append(lines, codebase+"\n")
	}
	out, _ := pack("Project context (codergag):\n", budget, lines)
	return out, nil
}

// PromptDigest returns records relevant to prompt that fit budget, skipping
// pinned records (already in the session digest) and ids in seen. ids lists
// the records included so the caller can mark them seen.
func (s *Store) PromptDigest(project, prompt string, budget int, seen map[string]bool) (string, []string, error) {
	hits, err := s.Search(project, prompt, 10)
	if err != nil {
		return "", nil, err
	}
	var cand []Record
	var lines []string
	for _, r := range hits {
		if r.Pinned || seen[r.ID] {
			continue
		}
		cand = append(cand, r)
		lines = append(lines, recordLine(r))
	}
	out, idx := pack("Relevant project context (codergag):\n", budget, lines)
	ids := make([]string, 0, len(idx))
	for _, i := range idx {
		ids = append(ids, cand[i].ID)
	}
	return out, ids, nil
}

func seenPath(dir, sessionID string) string {
	clean := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, sessionID)
	if clean == "" {
		clean = "default"
	}
	return filepath.Join(dir, "hook-state", clean+".json")
}

func LoadSeen(dir, sessionID string) map[string]bool {
	seen := map[string]bool{}
	b, err := os.ReadFile(seenPath(dir, sessionID))
	if err != nil {
		return seen
	}
	var ids []string
	if json.Unmarshal(b, &ids) != nil {
		return seen
	}
	for _, id := range ids {
		seen[id] = true
	}
	return seen
}

func SaveSeen(dir, sessionID string, seen map[string]bool) error {
	p := seenPath(dir, sessionID)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	b, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ctxstore/ ./internal/config/ -count=1`
Expected: PASS. If the tight-budget assertion fails, reduce the fixture body sizes rather than the budget: the header alone costs 7 tokens and the pinned line must fit within 30.

- [ ] **Step 5: Commit**

```bash
git add internal/ctxstore internal/config
git commit -m "feat(ctxstore): budgeted session/prompt digests, profile, seen state"
```

---

### Task 3: `codergag ctx` CLI

**Files:**
- Create: `cmd/codergag/ctx.go`
- Test: `cmd/codergag/ctx_test.go`
- Modify: `cmd/codergag/main.go` (add `activeCfg`, `case "ctx"`, usage line, add `"ctx"` to the daemon-disabled list)
- Modify: `cmd/codergag/main_test.go` (add `"ctx"` and `"hook"` to the read-only command list in `TestConfigForCommandDisablesDaemonForReadOnlyCommands`)

**Interfaces:**
- Consumes: `ctxstore.New`, `Store` methods, `Profile`, `SessionDigest`, `EstimateTokens`, `ProjectForDir` (Tasks 1-2); `config.ContextConfig` (Task 2).
- Produces:
  - `var activeCfg config.Config` (set in `main` before dispatch; commands read budgets via `activeCfg.Context`)
  - `func stateDir() string` (`$CODERAG_HOME` else `~/.codergag`)
  - `func resolveProject(app *services.Application, flagVal string) (string, error)` (flag → cwd match via `ProjectForDir` → `firstProject`; error when none)
  - `func ctxRun(app *services.Application, args []string, stdin io.Reader, stdout, stderr io.Writer) int`
  - `func runCtx(app *services.Application, args []string) int` (closes the graph, wires real stdio)

- [ ] **Step 1: Write the failing test**

```go
// cmd/codergag/ctx_test.go
package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"codergag/internal/services"
)

func runCtxT(t *testing.T, app *services.Application, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := ctxRun(app, args, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func TestCtxAddSearchShowExportStatusReset(t *testing.T) {
	t.Setenv("CODERAG_HOME", t.TempDir())
	app := services.ApplicationInMemory()

	code, out, errs := runCtxT(t, app, "", "add", "-project", "p", "-kind", "decision", "-title", "Use SQLite", "-pin", "single", "binary")
	if code != 0 {
		t.Fatalf("add failed: %d %s", code, errs)
	}
	var added map[string]any
	if err := json.Unmarshal([]byte(out), &added); err != nil || added["id"] == "" {
		t.Fatalf("add must print the record as JSON: %q %v", out, err)
	}
	id := added["id"].(string)

	// body from stdin when no positional args
	if code, _, errs := runCtxT(t, app, "from stdin body", "add", "-project", "p", "-title", "Stdin note"); code != 0 {
		t.Fatalf("stdin add failed: %s", errs)
	}

	_, out, _ = runCtxT(t, app, "", "search", "-project", "p", "-json", "sqlite")
	if !strings.Contains(out, "Use SQLite") {
		t.Fatalf("search output: %s", out)
	}
	_, out, _ = runCtxT(t, app, "", "show", id)
	if !strings.Contains(out, "single binary") {
		t.Fatalf("show output: %s", out)
	}
	_, out, _ = runCtxT(t, app, "", "export", "-project", "p")
	if !strings.Contains(out, "## decision") || !strings.Contains(out, "### Use SQLite") {
		t.Fatalf("export output: %s", out)
	}
	_, out, _ = runCtxT(t, app, "", "status", "-project", "p", "-json")
	var st map[string]any
	if err := json.Unmarshal([]byte(out), &st); err != nil || st["total"].(float64) != 2 || st["digest_tokens"].(float64) <= 0 {
		t.Fatalf("status output: %s (%v)", out, err)
	}

	if code, _, _ := runCtxT(t, app, "n\n", "reset", "-project", "p"); code == 0 {
		t.Fatal("reset without confirmation must not succeed")
	}
	if code, _, _ := runCtxT(t, app, "", "reset", "-project", "p", "-yes"); code != 0 {
		t.Fatal("reset -yes must succeed")
	}
	_, out, _ = runCtxT(t, app, "", "status", "-project", "p", "-json")
	json.Unmarshal([]byte(out), &st)
	if st["total"].(float64) != 0 {
		t.Fatalf("reset must clear records: %s", out)
	}
}

func TestCtxProfile(t *testing.T) {
	t.Setenv("CODERAG_HOME", t.TempDir())
	app := services.ApplicationInMemory()
	if code, _, errs := runCtxT(t, app, "", "profile", "set", "style", "terse"); code != 0 {
		t.Fatal(errs)
	}
	_, out, _ := runCtxT(t, app, "", "profile")
	if !strings.Contains(out, "style") || !strings.Contains(out, "terse") {
		t.Fatalf("profile output: %s", out)
	}
	runCtxT(t, app, "", "profile", "clear")
	_, out, _ = runCtxT(t, app, "", "profile")
	if strings.Contains(out, "terse") {
		t.Fatalf("profile not cleared: %s", out)
	}
}

func TestCtxErrors(t *testing.T) {
	t.Setenv("CODERAG_HOME", t.TempDir())
	app := services.ApplicationInMemory()
	if code, _, _ := runCtxT(t, app, ""); code != 2 {
		t.Error("missing subcommand must exit 2")
	}
	if code, _, _ := runCtxT(t, app, "", "add", "-project", "p"); code == 0 {
		t.Error("add without title must fail")
	}
	if code, _, _ := runCtxT(t, app, "", "show", "nope"); code == 0 {
		t.Error("show of unknown id must fail")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/codergag/ -run TestCtx -count=1`
Expected: FAIL (undefined: `ctxRun`)

- [ ] **Step 3: Write minimal implementation**

```go
// cmd/codergag/ctx.go
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"codergag/internal/config"
	"codergag/internal/ctxstore"
	"codergag/internal/services"
)

// activeCfg is the effective config for the running command, set by main.
var activeCfg = config.Default()

// stateDir is where hook state and the profile live.
func stateDir() string {
	if d := os.Getenv("CODERAG_HOME"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".codergag")
}

func profilePath() string { return filepath.Join(stateDir(), "profile.json") }

// resolveProject picks the project: explicit flag, else the indexed project
// containing the working directory, else the first indexed project.
func resolveProject(app *services.Application, flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if cwd, err := os.Getwd(); err == nil {
		if id, err := ctxstore.ProjectForDir(app, cwd); err == nil && id != "" {
			return id, nil
		}
	}
	id, err := firstProject(app)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.New("no project indexed; use -project ID")
	}
	return id, nil
}

func runCtx(app *services.Application, args []string) int {
	defer app.Graph.Close()
	return ctxRun(app, args, os.Stdin, os.Stdout, os.Stderr)
}

const ctxUsage = "usage: codergag ctx <add|search|show|export|status|profile|reset> [flags]\n"

func ctxRun(app *services.Application, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, ctxUsage)
		return 2
	}
	sub, rest := args[0], args[1:]
	store := ctxstore.New(app)
	fail := func(err error) int { fmt.Fprintln(stderr, "ctx:", err); return 1 }
	newFS := func() (*flag.FlagSet, *string) {
		fs := flag.NewFlagSet("ctx "+sub, flag.ContinueOnError)
		fs.SetOutput(stderr)
		return fs, fs.String("project", "", "project id (default: detected)")
	}

	switch sub {
	case "add":
		fs, project := newFS()
		kind := fs.String("kind", "note", "decision|convention|task|note")
		title := fs.String("title", "", "record title (required)")
		pin := fs.Bool("pin", false, "always include in the session digest")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		body := strings.Join(fs.Args(), " ")
		if body == "" {
			if b, err := io.ReadAll(stdin); err == nil {
				body = strings.TrimSpace(string(b))
			}
		}
		pid, err := resolveProject(app, *project)
		if err != nil {
			return fail(err)
		}
		rec, err := store.Add(pid, ctxstore.Record{Kind: *kind, Title: *title, Body: body, Pinned: *pin})
		if err != nil {
			return fail(err)
		}
		writeJSON(stdout, rec)
		return 0

	case "search":
		fs, project := newFS()
		asJSON := fs.Bool("json", false, "machine-readable output")
		limit := fs.Int("limit", 10, "max results")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		pid, err := resolveProject(app, *project)
		if err != nil {
			return fail(err)
		}
		recs, err := store.Search(pid, strings.Join(fs.Args(), " "), *limit)
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			writeJSON(stdout, map[string]any{"records": recs, "count": len(recs)})
			return 0
		}
		for _, r := range recs {
			fmt.Fprintf(stdout, "%s  [%s] %s\n", r.ID, r.Kind, r.Title)
		}
		return 0

	case "show":
		if len(rest) != 1 {
			fmt.Fprintln(stderr, "usage: codergag ctx show <id>")
			return 2
		}
		r, err := store.Get(rest[0])
		if err != nil {
			return fail(fmt.Errorf("record %q: %w", rest[0], err))
		}
		fmt.Fprintf(stdout, "id:      %s\nkind:    %s\ntitle:   %s\nsource:  %s\npinned:  %v\ncreated: %s\n\n%s\n",
			r.ID, r.Kind, r.Title, r.Source, r.Pinned, r.CreatedAt, r.Body)
		return 0

	case "export":
		fs, project := newFS()
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		pid, err := resolveProject(app, *project)
		if err != nil {
			return fail(err)
		}
		recs, err := store.List(pid, "")
		if err != nil {
			return fail(err)
		}
		byKind := map[string][]ctxstore.Record{}
		for _, r := range recs {
			byKind[r.Kind] = append(byKind[r.Kind], r)
		}
		kinds := make([]string, 0, len(byKind))
		for k := range byKind {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		fmt.Fprintf(stdout, "# Context: %s\n", pid)
		for _, k := range kinds {
			fmt.Fprintf(stdout, "\n## %s\n", k)
			for _, r := range byKind[k] {
				fmt.Fprintf(stdout, "\n### %s\n\n%s\n", r.Title, r.Body)
			}
		}
		return 0

	case "status":
		fs, project := newFS()
		asJSON := fs.Bool("json", false, "machine-readable output")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		pid, err := resolveProject(app, *project)
		if err != nil {
			return fail(err)
		}
		recs, err := store.List(pid, "")
		if err != nil {
			return fail(err)
		}
		counts := map[string]int{}
		for _, r := range recs {
			counts[r.Kind]++
		}
		prof, _ := ctxstore.LoadProfile(profilePath())
		digest, err := store.SessionDigest(pid, activeCfg.Context.Session(), prof, "")
		if err != nil {
			return fail(err)
		}
		tokens := ctxstore.EstimateTokens(digest)
		if *asJSON {
			writeJSON(stdout, map[string]any{"project": pid, "total": len(recs), "by_kind": counts,
				"digest_tokens": tokens, "session_budget": activeCfg.Context.Session()})
			return 0
		}
		fmt.Fprintf(stdout, "project: %s\nrecords: %d %v\nsession digest: ~%d / %d tokens\n",
			pid, len(recs), counts, tokens, activeCfg.Context.Session())
		return 0

	case "profile":
		p, err := ctxstore.LoadProfile(profilePath())
		if err != nil {
			return fail(err)
		}
		switch {
		case len(rest) == 0:
			keys := make([]string, 0, len(p))
			for k := range p {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(stdout, "%s: %s\n", k, p[k])
			}
		case rest[0] == "set" && len(rest) >= 3:
			p[rest[1]] = strings.Join(rest[2:], " ")
			if err := p.Save(profilePath()); err != nil {
				return fail(err)
			}
		case rest[0] == "clear":
			if err := (ctxstore.Profile{}).Save(profilePath()); err != nil {
				return fail(err)
			}
		default:
			fmt.Fprintln(stderr, "usage: codergag ctx profile [set <key> <value>|clear]")
			return 2
		}
		return 0

	case "reset":
		fs, project := newFS()
		yes := fs.Bool("yes", false, "skip the confirmation prompt")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		pid, err := resolveProject(app, *project)
		if err != nil {
			return fail(err)
		}
		if !*yes {
			fmt.Fprintf(stderr, "delete ALL memory records of project %s? [y/N] ", pid)
			line, _ := bufio.NewReader(stdin).ReadString('\n')
			if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
				fmt.Fprintln(stderr, "aborted")
				return 1
			}
		}
		n, err := store.Reset(pid)
		if err != nil {
			return fail(err)
		}
		writeJSON(stdout, map[string]any{"project": pid, "removed": n})
		return 0
	}
	fmt.Fprintf(stderr, "ctx: unknown subcommand %q\n%s", sub, ctxUsage)
	return 2
}

func writeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}
```

In `cmd/codergag/main.go`:
- After `cfg = configForCommand(cmd, args, cfg)` add `activeCfg = cfg`.
- In the `switch cmd` add `case "ctx": os.Exit(runCtx(app, args))`.
- Add to `usage`, after the `inject` line: `"  codergag ctx <add|search|show|export|status|profile|reset>   manage project context records\n" +`
- Add `"ctx"` to the read-only list in `configForCommand` (the `case "http", "status", ...` line).

In `cmd/codergag/main_test.go` add `"ctx"` to the command list in `TestConfigForCommandDisablesDaemonForReadOnlyCommands`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/codergag/ -count=1`
Expected: PASS (including the pre-existing tests)

- [ ] **Step 5: Commit**

```bash
git add cmd/codergag
git commit -m "feat(cli): add ctx command for context records"
```

---

### Task 4: `codergag hook` command

**Files:**
- Create: `cmd/codergag/hook.go`
- Test: `cmd/codergag/hook_test.go`
- Modify: `cmd/codergag/main.go` (`case "hook"`, usage line, add `"hook"` to the daemon-disabled list; the `main_test.go` list was already updated in Task 3)

**Interfaces:**
- Consumes: `ctxstore.*`, `activeCfg`, `stateDir`, `profilePath` (Task 3).
- Produces:
  - `func hookRun(app *services.Application, event string, stdin io.Reader, stdout, stderr io.Writer) int` (always returns 0)
  - `func runHook(app *services.Application, args []string) int`
  - Output shape: `{"hookSpecificOutput":{"hookEventName":"<event>","additionalContext":"<text>"}}`, written only when the digest is non-empty.

- [ ] **Step 1: Write the failing test**

```go
// cmd/codergag/hook_test.go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/codergag/ -run TestHook -count=1`
Expected: FAIL (undefined: `hookRun`)

- [ ] **Step 3: Write minimal implementation**

```go
// cmd/codergag/hook.go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"codergag/internal/ctxstore"
	"codergag/internal/services"
)

type hookInput struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	Prompt    string `json:"prompt"`
}

// runHook is the Claude Code hook entry point: `codergag hook <event>`.
func runHook(app *services.Application, args []string) int {
	defer app.Graph.Close()
	event := ""
	if len(args) > 0 {
		event = args[0]
	}
	return hookRun(app, event, os.Stdin, os.Stdout, os.Stderr)
}

// hookRun injects context for SessionStart and UserPromptSubmit and ignores
// every other event (reserved for capture in a later cycle). It never blocks
// the session: every failure is logged to stderr and the exit code is 0.
func hookRun(app *services.Application, event string, stdin io.Reader, stdout, stderr io.Writer) int {
	if event != "SessionStart" && event != "UserPromptSubmit" {
		return 0
	}
	var in hookInput
	if b, err := io.ReadAll(stdin); err == nil && len(b) > 0 {
		if err := json.Unmarshal(b, &in); err != nil {
			fmt.Fprintln(stderr, "hook: bad input:", err)
			return 0
		}
	}
	cwd := in.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	project, err := ctxstore.ProjectForDir(app, cwd)
	if err != nil || project == "" {
		return 0
	}
	store := ctxstore.New(app)

	var text string
	switch event {
	case "SessionStart":
		prof, _ := ctxstore.LoadProfile(profilePath())
		text, err = store.SessionDigest(project, activeCfg.Context.Session(), prof, codebaseLine(app, project))
	case "UserPromptSubmit":
		if in.Prompt == "" {
			return 0
		}
		seen := ctxstore.LoadSeen(stateDir(), in.SessionID)
		var ids []string
		text, ids, err = store.PromptDigest(project, in.Prompt, activeCfg.Context.Prompt(), seen)
		if err == nil && len(ids) > 0 {
			for _, id := range ids {
				seen[id] = true
			}
			if serr := ctxstore.SaveSeen(stateDir(), in.SessionID, seen); serr != nil {
				fmt.Fprintln(stderr, "hook: save state:", serr)
			}
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "hook:", err)
		return 0
	}
	if text == "" {
		return 0
	}
	json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":     event,
		"additionalContext": text,
	}})
	return 0
}

// codebaseLine is a one-line summary of the indexed project.
func codebaseLine(app *services.Application, project string) string {
	files, _ := app.Graph.FindNodes("SourceFile", map[string]any{"project_id": project})
	if len(files) == 0 {
		return ""
	}
	funcs, _ := app.Graph.FindNodes("Function", map[string]any{"project_id": project})
	return fmt.Sprintf("Codebase: %d source files, %d functions indexed (codergag MCP tools available).", len(files), len(funcs))
}
```

In `cmd/codergag/main.go` add `case "hook": os.Exit(runHook(app, args))`, the usage line `"  codergag hook <SessionStart|UserPromptSubmit>   Claude Code hook entry point (reads JSON on stdin)\n" +`, and add `"hook"` to the daemon-disabled list.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/codergag/ -count=1`
Expected: PASS (`main_test.go` now covers `ctx` and `hook` disabling the daemon)

- [ ] **Step 5: Commit**

```bash
git add cmd/codergag
git commit -m "feat(cli): add hook command injecting budgeted context"
```

---

### Task 5: Settings registration (`setup --hooks`, `uninstall`)

**Files:**
- Create: `cmd/codergag/hooks_settings.go`
- Test: `cmd/codergag/hooks_settings_test.go`
- Modify: `cmd/codergag/setup.go` (add `-hooks` and `-settings` flags)
- Modify: `cmd/codergag/uninstall.go` (remove tagged hooks)

**Interfaces:**
- Produces:
  - `const hookMarker = "codergag hook"`
  - `func mergeHooks(settings map[string]any) bool` (adds missing entries for `SessionStart`, `UserPromptSubmit`; reports whether it changed anything)
  - `func removeHooks(settings map[string]any) bool`
  - `func updateSettingsFile(path string, remove bool) (bool, error)` (atomic write; refuses to touch unparseable JSON)
  - `func defaultSettingsPath() string` (`~/.claude/settings.json`)

- [ ] **Step 1: Write the failing test**

```go
// cmd/codergag/hooks_settings_test.go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/codergag/ -run TestUpdateSettings -count=1`
Expected: FAIL (undefined: `updateSettingsFile`)

- [ ] **Step 3: Write minimal implementation**

```go
// cmd/codergag/hooks_settings.go
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
```

In `cmd/codergag/setup.go` add the flags before `fs.Parse(args)`:

```go
	hooks := fs.Bool("hooks", false, "register the SessionStart/UserPromptSubmit context hooks in Claude Code settings")
	settings := fs.String("settings", "", "Claude Code settings file (default ~/.claude/settings.json)")
```

and, before the final `return 0`:

```go
	if *hooks {
		path := *settings
		if path == "" {
			path = defaultSettingsPath()
		}
		changed, err := updateSettingsFile(path, false)
		if err != nil {
			fmt.Fprintln(os.Stderr, "setup: hooks:", err)
			return 1
		}
		if changed {
			fmt.Fprintf(os.Stderr, "  registered context hooks in %s\n", path)
		} else {
			fmt.Fprintf(os.Stderr, "  context hooks already registered in %s\n", path)
		}
	}
```

In `cmd/codergag/uninstall.go`, after the `removed := 0` / `home` block and before `candidates`, add:

```go
	if changed, err := updateSettingsFile(defaultSettingsPath(), true); err != nil {
		fmt.Fprintln(os.Stderr, "uninstall: hooks:", err)
	} else if changed {
		fmt.Fprintf(os.Stderr, "removed codergag hooks from %s\n", defaultSettingsPath())
		removed++
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/codergag/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/codergag
git commit -m "feat(setup): register and remove context hooks in Claude settings"
```

---

### Task 6: Docs, agent instructions, end-to-end check

**Files:**
- Modify: `README.md` (new "Context records and hooks" section)
- Modify: `cmd/codergag/register.go` (the `instructionsMD` "Memory" section)
- Modify: `docs/superpowers/specs/2026-09-20-context-store-and-hooks-design.md` (already updated to reflect `Memory` reuse)

- [ ] **Step 1: Document**

Add to `README.md`, after the paragraph listing MCP tools:

````markdown
## Context records and hooks

`codergag ctx` manages durable project context (decisions, conventions, tasks, notes) stored as graph memories:

```bash
codergag ctx add -kind decision -title "Use SQLite" -pin "single binary, no server"
codergag ctx search sqlite
codergag ctx status        # record counts and the token cost of the session digest
codergag ctx export        # Markdown, grouped by kind
codergag ctx profile set style terse
codergag ctx reset -yes
```

`codergag setup --hooks` registers Claude Code hooks (`SessionStart`, `UserPromptSubmit`) that inject a token-budgeted digest: pinned records, decisions, and the records relevant to each prompt. Budgets are `context.session_budget` (default 1500) and `context.prompt_budget` (default 500) tokens. `codergag uninstall` removes only the hooks it added.
````

In `cmd/codergag/register.go`, in `instructionsMD`, extend the `## Memory` list with:

```
- ` + "`codergag ctx add|search|show|export|status`" + ` — durable project records injected by the Claude Code hooks
```

- [ ] **Step 2: Run the full verification**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: all succeed with no failures.

- [ ] **Step 3: Manual smoke test against a scratch settings file**

Run:
```bash
export CODERAG_HOME=$(mktemp -d)
go run ./cmd/codergag setup --hooks --settings "$CODERAG_HOME/settings.json"
cat "$CODERAG_HOME/settings.json"
echo '{"cwd":"/nowhere"}' | go run ./cmd/codergag hook SessionStart; echo "exit=$?"
```
Expected: settings file contains two `codergag hook ...` commands; the hook prints nothing and prints `exit=0`.

- [ ] **Step 4: Commit**

```bash
git add README.md cmd/codergag/register.go docs
git commit -m "docs: document ctx records and hooks"
```
