package ctxstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// EstimateTokens is the shared rough token estimate (ceil(len/4)). It is used
// by both `ctx status` and the hooks so the numbers always agree.
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