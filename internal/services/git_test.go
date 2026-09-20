package services

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func hasHint(rep map[string]any, substr string) bool {
	for _, h := range rep["reviewer_hints"].([]string) {
		if strings.Contains(h, substr) {
			return true
		}
	}
	return false
}

func fnNames(rep map[string]any) []string {
	var out []string
	for _, f := range rep["changed_functions"].([]map[string]any) {
		out = append(out, f["name"].(string))
	}
	return out
}

func TestPrContextIsBranchAwareAndLineAccurate(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	var a strings.Builder
	a.WriteString("package x\n\nfunc Hot() int { return 1 }\n\nfunc Cold() int { return 2 }\n\nfunc Tested() int { return 3 }\n")
	var callers strings.Builder
	callers.WriteString("package x\n")
	for i := 0; i < 11; i++ {
		fmt.Fprintf(&callers, "func Caller%02d() { Hot() }\n", i)
	}
	writeTree(t, dir, map[string]string{
		"a.go":       a.String(),
		"callers.go": callers.String(),
		"a_test.go":  "package x\nfunc TestTested() { Tested() }\n",
		"gone.go":    "package x\nfunc Gone() {}\n",
		"NOTES.md":   "# Notes\nThe Hot function is the entry.\n",
	})
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")

	gitRun(t, dir, "checkout", "-b", "feature")
	// Change only Hot and Tested; Cold stays untouched. Add a new function; delete a file.
	writeTree(t, dir, map[string]string{
		"a.go": "package x\n\nfunc Hot() int { return 100 }\n\nfunc Cold() int { return 2 }\n\nfunc Tested() int { return 30 }\n\nfunc Fresh() int { return 4 }\n",
	})
	gitRun(t, dir, "rm", "-q", "gone.go")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "feature work")
	// Main moves on after the branch point.
	gitRun(t, dir, "checkout", "main")
	writeTree(t, dir, map[string]string{"other.txt": "x\n"})
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "main moves on")
	gitRun(t, dir, "checkout", "feature")

	app := ApplicationInMemory()
	if _, err := app.Index.IndexRepository("p", dir, true, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Documents.IndexMarkdown("p", dir+"/NOTES.md"); err != nil {
		t.Fatal(err)
	}
	rep, err := app.Git.PrContext("p", "main", 20)
	if err != nil {
		t.Fatal(err)
	}

	if rep["branch"] != "feature" || rep["commits_ahead"] != 1 || rep["base_moved_on"] != 1 {
		t.Fatalf("branch info: %v", rep)
	}
	got := strings.Join(fnNames(rep), ",")
	for _, want := range []string{"Hot", "Tested", "Fresh"} {
		if !strings.Contains(got, want) {
			t.Errorf("changed functions %q should include %s", got, want)
		}
	}
	if strings.Contains(got, "Cold") || strings.Contains(got, "Caller") {
		t.Errorf("untouched functions in a changed file must not be reported: %q", got)
	}
	if strings.Contains(fmt.Sprint(rep["changed_files"]), "other.txt") {
		t.Error("changes made on main after the branch point must not appear (merge-base diff)")
	}
	if !hasHint(rep, "Heavily called") || !hasHint(rep, "Hot (11 callers)") {
		t.Errorf("fan-in hint missing: %v", rep["reviewer_hints"])
	}
	if !hasHint(rep, "no related test") || !hasHint(rep, "Fresh") || hasHint(rep, "Tested,") {
		t.Errorf("untested hint should name Hot and Fresh but not Tested: %v", rep["reviewer_hints"])
	}
	for _, h := range rep["reviewer_hints"].([]string) {
		if strings.Contains(h, "no related test") && strings.Contains(h, "Tested") && !strings.Contains(h, "TestTested") {
			t.Errorf("Tested has a test and must not be flagged: %s", h)
		}
	}
	if !hasHint(rep, "1 file(s) deleted") || !hasHint(rep, "Base has 1 newer commit") {
		t.Errorf("deletion / rebase hints missing: %v", rep["reviewer_hints"])
	}
	docs := rep["docs_to_review"].([]map[string]any)
	if len(docs) != 1 || docs[0]["document"] != "NOTES.md" {
		t.Errorf("NOTES.md mentions Hot and should be flagged: %v", docs)
	}
	if rep["risk"] == "low" {
		t.Errorf("11 callers and untested changes should not be low risk: %v", rep["risk"])
	}
	if hasHint(rep, "Index is stale") {
		t.Errorf("index was built after the changes; it must not be stale: %v", rep["reviewer_hints"])
	}

	// Uncommitted edits make the index stale for that file and the report says so.
	writeTree(t, dir, map[string]string{"callers.go": "package x\nfunc Caller00() { Hot(); Hot() }\n"})
	rep, _ = app.Git.PrContext("p", "main", 20)
	if !hasHint(rep, "Index is stale") || !hasHint(rep, "callers.go") {
		t.Errorf("stale-index hint expected for edited file: %v", rep["reviewer_hints"])
	}
}

func TestPrContextErrorsAreExplicit(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.go": "package x\nfunc A() {}\n"})
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")
	app := ApplicationInMemory()
	app.Index.IndexRepository("p", dir, true, nil, false)
	if _, err := app.Git.PrContext("p", "no-such-ref", 10); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown base must be reported: %v", err)
	}
	if _, err := app.Git.PrContext("missing", "main", 10); err == nil {
		t.Fatal("unindexed project must error")
	}
}

func TestParseHunks(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n+++ b/a.go\n@@ -3 +3 @@\n@@ -10,2 +12,3 @@\n@@ -20,4 +25,0 @@\n+++ /dev/null\n@@ -1,3 +0,0 @@\n"
	got := parseHunks(diff)["a.go"]
	want := []lineRange{{3, 3}, {12, 14}, {25, 25}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("hunks = %v, want %v", got, want)
	}
	if !overlaps(got, 13, 20) || overlaps(got, 4, 11) {
		t.Fatal("overlap check wrong")
	}
}

func TestIsTestPathIgnoresParentDirectories(t *testing.T) {
	yes := []string{"a_test.go", "pkg/x_test.go", "test_thing.py", "src/app.spec.ts", "tests/helper.py", "src/__tests__/a.js", "FooTest.java"}
	no := []string{"src/contest.go", "latest/main.go", "pkg/attestation.go", "main.go"}
	for _, p := range yes {
		if !isTestPath(p) {
			t.Errorf("%s should be a test path", p)
		}
	}
	for _, p := range no {
		if isTestPath(p) {
			t.Errorf("%s should not be a test path", p)
		}
	}
}

func TestReviewSuggestions(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.go": "package x\nfunc A() {}\n"})
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")
	app := ApplicationInMemory()
	app.Index.IndexRepository("p", dir, true, nil, false)
	// Unknown project returns nil.
	if got := app.Git.ReviewSuggestions("missing", nil, 5); got != nil {
		t.Fatalf("expected nil for unknown project, got %v", got)
	}
	// Blame a specific file in the project.
	out := app.Git.ReviewSuggestions("p", []string{"a.go"}, 5)
	if len(out) == 0 {
		t.Skip("git blame returned no output in this environment")
	}
	if out[0]["name"] != "t" {
		t.Errorf("expected reviewer t, got %v", out[0])
	}
	// Limit is respected.
	out2 := app.Git.ReviewSuggestions("p", []string{"a.go"}, 1)
	if len(out2) > 1 {
		t.Errorf("limit 1 should yield at most 1 reviewer, got %d", len(out2))
	}
}
