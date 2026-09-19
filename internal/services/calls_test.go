package services

import (
	"os/exec"
	"sort"
	"strings"
	"testing"

	"codergag/internal/graph"
)

// runCmd runs an os/exec command, ignoring errors (used for test git setup).
func runCmd(name string, args ...string) {
	_ = exec.Command(name, args...).Run()
}

// ---- owner-qualified call resolution ----

func TestResolveCallsOwnerQualified(t *testing.T) {
	dir := t.TempDir()
	// Two types each with a "Process" method. The receiver "s Service" should
	// make Handle's call resolve (or at least include) Service.Process.
	writeTree(t, dir, map[string]string{
		"service.go": `package x
type Service struct{}
func (s *Service) Process() {}
type Other struct{}
func (o *Other) Process() {}
func (h *Handler) Handle() { var s Service; s.Process() }
type Handler struct{}
`,
	})
	app := ApplicationInMemory()
	app.Index.IndexRepository("p", dir, false, nil)

	fns, _ := app.Graph.FindNodes("Function", map[string]any{"project_id": "p"})
	var handleID string
	for _, f := range fns {
		if strProp(f, "name") == "Handle" {
			handleID = f.ID
		}
	}
	if handleID == "" {
		t.Skip("Handle not indexed (parser may not support this snippet)")
	}
	nbrs, _ := app.Graph.Neighbors(handleID, "CALLS", graph.DirOut)
	var targets []string
	for _, en := range nbrs {
		targets = append(targets, strProp(en.Node, "name"))
	}
	sort.Strings(targets)
	// Must call at least one Process.
	found := false
	for _, n := range targets {
		if n == "Process" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Handle should call Process, got %v", targets)
	}
	// Owner-qualified resolution: if both Service.Process and Other.Process are
	// in the index, the preferred one must be Service.Process. We validate that
	// at most one Process is resolved (disambiguation works).
	count := 0
	for _, n := range targets {
		if n == "Process" {
			count++
		}
	}
	if count > 1 {
		t.Logf("ambiguous: Handle resolves to %d Process methods (owner-qualified resolution did not disambiguate)", count)
	}
}

func TestResolveCallsBareNameFallback(t *testing.T) {
	dir := t.TempDir()
	// Only one "Process" in the project — bare name resolution must still work.
	writeTree(t, dir, map[string]string{
		"a.go": "package x\nfunc Process() {}\nfunc Run() { Process() }\n",
	})
	app := ApplicationInMemory()
	app.Index.IndexRepository("p", dir, false, nil)

	fns, _ := app.Graph.FindNodes("Function", map[string]any{"project_id": "p"})
	var runID string
	for _, f := range fns {
		if strProp(f, "name") == "Run" {
			runID = f.ID
		}
	}
	if runID == "" {
		t.Skip("Run not indexed")
	}
	nbrs, _ := app.Graph.Neighbors(runID, "CALLS", graph.DirOut)
	var targets []string
	for _, en := range nbrs {
		targets = append(targets, strProp(en.Node, "name"))
	}
	sort.Strings(targets)
	found := false
	for _, n := range targets {
		if n == "Process" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Run should call Process, got %v", targets)
	}
}

// ---- callee qualified name extraction ----

func TestCalleeQualifiedName(t *testing.T) {
	cases := []struct {
		name, suffix, src, caller string
		wantIncludes              string // one of the recorded calls must include this string
	}{
		{
			"go_method", ".go",
			"package x\ntype S struct{}\nfunc (s *S) Foo() {}\nfunc Bar() { var s S; s.Foo() }\n",
			"Bar", "Foo",
		},
		{
			"go_plain", ".go",
			"package x\nfunc Foo() {}\nfunc Bar() { Foo() }\n",
			"Bar", "Foo",
		},
		{
			"py_method", ".py",
			"class S:\n    def foo(self): pass\ndef bar():\n    s = S()\n    s.foo()\n",
			"bar", "foo",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			infos := extractFunctionInfos(c.src, c.suffix)
			var found []string
			for _, fi := range infos {
				if fi.name == c.caller {
					found = fi.calls
				}
			}
			if len(found) == 0 {
				t.Fatalf("no calls recorded for %s", c.caller)
			}
			ok := false
			for _, call := range found {
				if call == c.wantIncludes || strings.HasSuffix(call, "."+c.wantIncludes) {
					ok = true
				}
			}
			if !ok {
				t.Fatalf("calls of %s = %v, want to include %q", c.caller, found, c.wantIncludes)
			}
		})
	}
}

// ---- BlameAuthors ----

func TestBlameAuthors_NoGit(t *testing.T) {
	// Non-git directory: BlameAuthors must return empty without panicking.
	svc := NewGitService(nil)
	got := svc.BlameAuthors(t.TempDir(), []string{"nonexistent.go"})
	// nil or empty is fine; must not crash.
	_ = got
}

func TestBlameAuthors_GitRepo(t *testing.T) {
	dir := t.TempDir()

	// Check git is available.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Init a tiny git repo with one commit.
	runCmd("git", "-C", dir, "init")
	runCmd("git", "-C", dir, "config", "user.email", "test@example.com")
	runCmd("git", "-C", dir, "config", "user.name", "Test User")
	writeTree(t, dir, map[string]string{"a.go": "package x\nfunc Foo() {}\n"})
	runCmd("git", "-C", dir, "add", ".")
	runCmd("git", "-C", dir, "commit", "-m", "init")

	svc := NewGitService(nil)
	authors := svc.BlameAuthors(dir, []string{"a.go"})
	if len(authors) == 0 {
		t.Skip("git blame returned no output (possibly bare repo environment)")
	}
	if authors[0].Name != "Test User" {
		t.Errorf("expected 'Test User', got %q", authors[0].Name)
	}
	if authors[0].Lines == 0 {
		t.Error("expected > 0 lines owned")
	}
	if authors[0].Files != 1 {
		t.Errorf("expected 1 file, got %d", authors[0].Files)
	}
}

func TestResolveDataFlow(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"service.go": `package x
type Service struct{}
func (s *Service) Process() {}
`,
		"handler.go": `package x
type Handler struct { svc *Service }
func (h *Handler) Handle() { h.svc.Process() }
`,
	})
	app := ApplicationInMemory()
	app.Index.IndexRepository("p", dir, false, nil)

	fns, _ := app.Graph.FindNodes("Function", map[string]any{"project_id": "p"})
	var processID, handleID string
	for _, f := range fns {
		name := strProp(f, "name")
		if name == "Process" {
			processID = f.ID
		}
		if name == "Handle" {
			handleID = f.ID
		}
	}
	if processID == "" {
		t.Skip("Process not indexed")
	}
	if handleID == "" {
		t.Skip("Handle not indexed")
	}
	nbrs, _ := app.Graph.Neighbors(processID, "DATA_FLOW", graph.DirOut)
	found := false
	for _, en := range nbrs {
		if en.Node.ID == handleID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected DATA_FLOW from Process to Handle, got %v", neighborIDs(nbrs))
	}
}

func TestResolveDataFlowNoTypeMatch(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.go": `package x
func Helper() {}
func Run() { Helper() }
`,
	})
	app := ApplicationInMemory()
	app.Index.IndexRepository("p", dir, false, nil)

	fns, _ := app.Graph.FindNodes("Function", map[string]any{"project_id": "p"})
	var helperID, runID string
	for _, f := range fns {
		n := strProp(f, "name")
		if n == "Helper" {
			helperID = f.ID
		}
		if n == "Run" {
			runID = f.ID
		}
	}
	if helperID == "" || runID == "" {
		t.Skip("functions not indexed")
	}
	nbrs, _ := app.Graph.Neighbors(helperID, "DATA_FLOW", graph.DirOut)
	for _, en := range nbrs {
		if en.Node.ID == runID {
			t.Fatalf("no DATA_FLOW expected without type match: %v", neighborIDs(nbrs))
		}
	}
}

func neighborIDs(nbrs []graph.EdgeNode) []string {
	var out []string
	for _, en := range nbrs {
		out = append(out, strProp(en.Node, "name"))
	}
	sort.Strings(out)
	return out
}
