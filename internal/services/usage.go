package services

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"codergag/internal/graph"
)

const systemProject = "_system"

// SystemProject is the project id used for cross-process usage bookkeeping.
const SystemProject = systemProject

// ToolUsage accumulates how one tool has been used.
type ToolUsage struct {
	Calls        int     `json:"calls"`
	Errors       int     `json:"errors"`
	TotalMs      float64 `json:"total_ms"`
	RawTokens    int     `json:"raw_tokens"`    // estimated size before shaping
	ShapedTokens int     `json:"shaped_tokens"` // estimated size actually returned
	Truncated    int     `json:"truncated"`     // pages cut by a token budget
}

func (u *ToolUsage) add(o ToolUsage) {
	u.Calls += o.Calls
	u.Errors += o.Errors
	u.TotalMs += o.TotalMs
	u.RawTokens += o.RawTokens
	u.ShapedTokens += o.ShapedTokens
	u.Truncated += o.Truncated
}

// UsageRecorder counts tool calls for this process and persists them as one
// UsageSession node, so several processes sharing a graph merge without
// overwriting each other's counters.
type UsageRecorder struct {
	g         graph.GraphRepository
	sessionID string
	started   time.Time
	mu        sync.Mutex
	byTool    map[string]*ToolUsage
}

func NewUsageRecorder(g graph.GraphRepository) *UsageRecorder {
	return &UsageRecorder{
		g:         g,
		sessionID: fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano()),
		started:   time.Now().UTC(),
		byTool:    map[string]*ToolUsage{},
	}
}

func (u *UsageRecorder) Record(tool string, d time.Duration, callErr error, rawTokens, shapedTokens int, truncated bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	t := u.byTool[tool]
	if t == nil {
		t = &ToolUsage{}
		u.byTool[tool] = t
	}
	t.Calls++
	if callErr != nil {
		t.Errors++
	}
	t.TotalMs += float64(d.Microseconds()) / 1000
	t.RawTokens += rawTokens
	t.ShapedTokens += shapedTokens
	if truncated {
		t.Truncated++
	}
}

// Flush writes this session's counters to the graph.
func (u *UsageRecorder) Flush() error {
	u.mu.Lock()
	blob, err := json.Marshal(u.byTool)
	u.mu.Unlock()
	if err != nil {
		return err
	}
	_, err = u.g.UpsertNode("UsageSession", map[string]any{"project_id": systemProject, "session_id": u.sessionID},
		map[string]any{
			"started_at": u.started.Format(time.RFC3339),
			"usage":      string(blob),
		})
	return err
}

// UsageSummary is the total across every recorded session.
type UsageSummary struct {
	Sessions int                   `json:"sessions"`
	Total    ToolUsage             `json:"total"`
	ByTool   map[string]*ToolUsage `json:"by_tool"`
}

func decodeUsage(blob string) map[string]*ToolUsage {
	var m map[string]*ToolUsage
	if json.Unmarshal([]byte(blob), &m) != nil {
		return nil
	}
	return m
}

// SummarizeUsage adds up rolled-up totals and live sessions.
func SummarizeUsage(g graph.GraphRepository) (UsageSummary, error) {
	sum := UsageSummary{ByTool: map[string]*ToolUsage{}}
	for _, kind := range []string{"UsageTotal", "UsageSession"} {
		nodes, err := g.FindNodes(kind, map[string]any{"project_id": systemProject})
		if err != nil {
			return sum, err
		}
		for _, n := range nodes {
			if kind == "UsageSession" {
				sum.Sessions++
			}
			for tool, u := range decodeUsage(strProp(n, "usage")) {
				if sum.ByTool[tool] == nil {
					sum.ByTool[tool] = &ToolUsage{}
				}
				sum.ByTool[tool].add(*u)
				sum.Total.add(*u)
			}
		}
	}
	return sum, nil
}

// TopTools returns tool names by descending call count.
func (s UsageSummary) TopTools(n int) []string {
	names := make([]string, 0, len(s.ByTool))
	for k := range s.ByTool {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := s.ByTool[names[i]].Calls, s.ByTool[names[j]].Calls
		if a != b {
			return a > b
		}
		return names[i] < names[j]
	})
	if n > 0 && len(names) > n {
		names = names[:n]
	}
	return names
}

// TokensSaved is the estimated tokens not sent thanks to response shaping.
func (s UsageSummary) TokensSaved() int { return s.Total.RawTokens - s.Total.ShapedTokens }

// rollUpUsage folds all but the newest keep sessions into one UsageTotal node.
func rollUpUsage(g graph.GraphRepository, keep int) (int, error) {
	sessions, err := g.FindNodes("UsageSession", map[string]any{"project_id": systemProject})
	if err != nil || len(sessions) <= keep {
		return 0, err
	}
	sort.Slice(sessions, func(i, j int) bool { return strProp(sessions[i], "started_at") < strProp(sessions[j], "started_at") })
	old := sessions[:len(sessions)-keep]
	total := map[string]*ToolUsage{}
	if prev, _ := g.FindNodes("UsageTotal", map[string]any{"project_id": systemProject, "name": "all"}); len(prev) > 0 {
		total = decodeUsage(strProp(prev[0], "usage"))
		if total == nil {
			total = map[string]*ToolUsage{}
		}
	}
	var ids []string
	for _, n := range old {
		for tool, u := range decodeUsage(strProp(n, "usage")) {
			if total[tool] == nil {
				total[tool] = &ToolUsage{}
			}
			total[tool].add(*u)
		}
		ids = append(ids, n.ID)
	}
	blob, _ := json.Marshal(total)
	if _, err := g.UpsertNode("UsageTotal", map[string]any{"project_id": systemProject, "name": "all"},
		map[string]any{"usage": string(blob)}); err != nil {
		return 0, err
	}
	return len(ids), g.RemoveNodes(ids)
}
