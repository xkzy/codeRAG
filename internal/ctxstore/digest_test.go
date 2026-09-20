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