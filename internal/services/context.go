package services

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"codergag/internal/privacy"
)

// ContextLevel controls how aggressively the compiler compresses context.
// Higher levels fit more provenance and structure into a tighter token budget.
//
//	0 – raw: unfiltered graph rows, no shaping
//	1 – structured: noisy fields removed, sections clearly labelled
//	2 – compressed: text snippets trimmed, duplicate paths factored out
//	3 – ranked: BM25-scored, lowest-relevance items dropped first
//	4 – diff-aware: changed functions / files surfaced first
//	5 – provenance: every fact annotated with its source stable-ID
const (
	CtxLevelRaw        = 0
	CtxLevelStructured = 1
	CtxLevelCompressed = 2
	CtxLevelRanked     = 3
	CtxLevelDiffAware  = 4
	CtxLevelProvenance = 5
)

// ContextItem is one provenance-tagged entry in a compiled context block.
type ContextItem struct {
	// Kind is "symbol", "memory", "doc", "task_fact", "team", "hypothesis".
	Kind string `json:"kind"`
	// ID is the stable graph ID where applicable.
	ID string `json:"id,omitempty"`
	// Text is the human-readable excerpt that the agent will read.
	Text string `json:"text"`
	// Source is a human-readable label ("BM25 search", "memory", "task:…", etc.)
	Source string `json:"source,omitempty"`
	// Confidence is [0,1] when known; 0 means "not applicable".
	Confidence float64 `json:"confidence,omitempty"`
	// Provenance is the stable ID of the node that produced this fact (for level 5).
	Provenance string `json:"provenance,omitempty"`
	// Score is the relevance score used to rank this item (for level 3+).
	Score float64 `json:"score,omitempty"`
}

// ContextBlock groups items by section.
type ContextBlock struct {
	Section string        `json:"section"`
	Items   []ContextItem `json:"items"`
}

// CompiledContext is the output of ContextCompiler.Compile.
type CompiledContext struct {
	Question     string         `json:"question"`
	Level        int            `json:"level"`
	ApproxTokens int            `json:"approx_tokens"`
	Truncated    bool           `json:"truncated,omitempty"`
	Blocks       []ContextBlock `json:"blocks"`
	// Checklist is a runtime checklist for agents (populated at level 1+).
	Checklist []string `json:"checklist,omitempty"`
	// Explanation describes how the context was assembled (explain_context).
	Explanation string `json:"explanation,omitempty"`
}

// ContextRequest parameterises a Compile call.
type ContextRequest struct {
	ProjectID string
	Question  string
	TaskID    string // optional: pull task facts and plan
	TeamID    string // optional: pull team knowledge
	Scopes    []string
	Limit     int // max items per section (default 10)
	MaxTokens int // 0 = no budget cap
	Level     int // 0-5, default 1
	Explain   bool
	Privacy   *PrivacyContext // optional: apply privacy firewall to outbound text
}

// PrivacyContext controls how the compiled context is sanitized before use.
type PrivacyContext struct {
	Mode      string   // privacy mode override (empty = use project policy)
	Forbidden []string // identifiers that must never appear
	Allowed   []string // identifiers explicitly permitted
	MaxTokens int      // token budget for the sanitized text
}

// ContextCompiler assembles multi-source context for an agent question.
type ContextCompiler struct {
	app *Application
}

// NewContextCompiler creates a ContextCompiler backed by the given Application.
func NewContextCompiler(app *Application) *ContextCompiler {
	return &ContextCompiler{app: app}
}

// Compile assembles a ranked, provenance-tagged context for the given request.
func (c *ContextCompiler) Compile(req ContextRequest) (*CompiledContext, error) {
	if req.Limit <= 0 {
		req.Limit = 10
	}
	if req.Level < CtxLevelRaw || req.Level > CtxLevelProvenance {
		req.Level = CtxLevelStructured
	}

	var blocks []ContextBlock
	var notes []string

	// --- Section 1: symbols (BM25 search) ---
	if syms, err := c.app.Code.Search(req.ProjectID, req.Question, req.Limit, false); err == nil && len(syms) > 0 {
		items := c.symbolItems(syms, req)
		blocks = append(blocks, ContextBlock{Section: "symbols", Items: items})
		notes = append(notes, fmt.Sprintf("BM25 search over code graph returned %d symbols", len(items)))
	}

	// --- Section 2: memory ---
	if mems, err := c.app.Memory.Search(req.ProjectID, req.Question, req.Limit); err == nil && len(mems) > 0 {
		items := c.memoryItems(mems, req)
		blocks = append(blocks, ContextBlock{Section: "memory", Items: items})
		notes = append(notes, fmt.Sprintf("%d memory entries matched", len(items)))
	}

	// --- Section 3: docs ---
	if docs, err := c.app.Documents.Search(req.ProjectID, req.Question, req.Limit); err == nil && len(docs) > 0 {
		items := c.docItems(docs, req)
		blocks = append(blocks, ContextBlock{Section: "docs", Items: items})
		notes = append(notes, fmt.Sprintf("%d documentation sections matched", len(items)))
	}

	// --- Section 4: task facts (if task_id given) ---
	if req.TaskID != "" {
		if ts, err := c.app.Tasks.Get(req.ProjectID, req.TaskID); err == nil {
			items := c.taskItems(ts, req)
			if len(items) > 0 {
				blocks = append(blocks, ContextBlock{Section: "task", Items: items})
				notes = append(notes, fmt.Sprintf("task %s (%s) contributed %d facts/steps", req.TaskID, ts.State, len(items)))
			}
		}
	}

	// --- Section 5: team knowledge (if team_id given) ---
	if req.TeamID != "" {
		if tc, err := c.app.Team.GetTeamContext(req.ProjectID, req.TeamID, req.Scopes, req.Limit); err == nil {
			items := c.teamItems(tc, req)
			if len(items) > 0 {
				blocks = append(blocks, ContextBlock{Section: "team", Items: items})
				notes = append(notes, fmt.Sprintf("team context contributed %d items", len(items)))
			}
		}
	}

	// --- Level 3: rank across all blocks by Score ---
	if req.Level >= CtxLevelRanked {
		blocks = c.rankBlocks(blocks, req.Question, req.Limit)
	}

	// --- Level 4: diff-aware: surface changed files/functions first ---
	if req.Level >= CtxLevelDiffAware {
		blocks = c.diffAware(req.ProjectID, req.Question, blocks)
	}

	// --- Level 5: annotate provenance on every item ---
	if req.Level >= CtxLevelProvenance {
		c.annotateProvenance(blocks)
	}

	// --- Token budget: trim blocks until we fit ---
	truncated := false
	if req.MaxTokens > 0 {
		blocks, truncated = c.applyBudget(blocks, req.MaxTokens)
	}

	// --- Checklist (level 1+) ---
	var checklist []string
	if req.Level >= CtxLevelStructured {
		checklist = c.buildChecklist(blocks, req)
	}

	ctx := &CompiledContext{
		Question:     req.Question,
		Level:        req.Level,
		ApproxTokens: approxContextTokens(blocks),
		Truncated:    truncated,
		Blocks:       blocks,
		Checklist:    checklist,
	}

	if req.Explain || req.Level >= CtxLevelProvenance {
		ctx.Explanation = c.buildExplanation(notes, req, ctx)
	}

	// Apply privacy firewall to the assembled text when requested.
	if req.Privacy != nil {
		ctx = c.applyPrivacy(req.ProjectID, ctx, req.Privacy)
	}

	return ctx, nil
}

// applyPrivacy sanitizes the compiled context text with the project firewall.
func (c *ContextCompiler) applyPrivacy(projectID string, ctx *CompiledContext, pc *PrivacyContext) *CompiledContext {
	svc := c.app.Privacy
	policy := svc.Policy(projectID)
	if pc.Mode != "" {
		if parsed, err := privacy.ParseMode(pc.Mode); err == nil {
			policy.Mode = parsed
		}
	}
	if pc.MaxTokens > 0 {
		policy.MaxContextTokens = pc.MaxTokens
	}
	if pc.Forbidden != nil {
		policy.ForbiddenIdentifiers = pc.Forbidden
	}
	if pc.Allowed != nil {
		policy.AllowedIdentifiers = pc.Allowed
	}
	// Request overrides apply to this call only; the stored policy is unchanged.
	firewall := svc.FirewallWithPolicy(policy)

	// Sanitize the rendered text of every block.
	for i := range ctx.Blocks {
		block := &ctx.Blocks[i]
		for j := range block.Items {
			item := &block.Items[j]
			result := firewall.SanitizeContext(item.Text)
			item.Text = result.Sanitized
			if !result.Allowed {
				item.Text = "***BLOCKED***"
			}
		}
	}
	ctx.ApproxTokens = approxContextTokens(ctx.Blocks)
	return ctx
}

// --- item builders ---

func (c *ContextCompiler) symbolItems(syms []map[string]any, req ContextRequest) []ContextItem {
	items := make([]ContextItem, 0, len(syms))
	for i, s := range syms {
		id, _ := s["id"].(string)
		name, _ := s["name"].(string)
		kind, _ := s["kind"].(string)
		path, _ := s["path"].(string)
		line, _ := s["line"].(float64)
		text := buildSymbolText(name, kind, path, int(line), req.Level)
		item := ContextItem{
			Kind:   "symbol",
			ID:     id,
			Text:   text,
			Source: "BM25 search",
			Score:  float64(len(syms)-i) / float64(len(syms)), // rank proxy
		}
		if req.Level >= CtxLevelProvenance {
			item.Provenance = id
		}
		items = append(items, item)
	}
	return items
}

func (c *ContextCompiler) memoryItems(mems []map[string]any, req ContextRequest) []ContextItem {
	items := make([]ContextItem, 0, len(mems))
	for _, m := range mems {
		id, _ := m["id"].(string)
		title, _ := m["title"].(string)
		content, _ := m["content"].(string)
		if req.Level >= CtxLevelCompressed {
			content = snippet(content, req.Question, 200)
		}
		items = append(items, ContextItem{
			Kind:       "memory",
			ID:         id,
			Text:       fmt.Sprintf("[%s] %s", title, content),
			Source:     "memory",
			Provenance: id,
		})
	}
	return items
}

func (c *ContextCompiler) docItems(docs []map[string]any, req ContextRequest) []ContextItem {
	items := make([]ContextItem, 0, len(docs))
	for _, d := range docs {
		id, _ := d["id"].(string)
		title, _ := d["title"].(string)
		content, _ := d["content"].(string)
		if req.Level >= CtxLevelCompressed {
			content = snippet(content, req.Question, 200)
		}
		items = append(items, ContextItem{
			Kind:       "doc",
			ID:         id,
			Text:       fmt.Sprintf("[%s] %s", title, content),
			Source:     "docs",
			Provenance: id,
		})
	}
	return items
}

func (c *ContextCompiler) taskItems(ts *TaskState, req ContextRequest) []ContextItem {
	var items []ContextItem
	// goal + state
	items = append(items, ContextItem{
		Kind:   "task_fact",
		ID:     ts.TaskID,
		Text:   fmt.Sprintf("Goal: %s | State: %s | Step: %s", ts.GoalSummary(), ts.State, ts.CurrentStep),
		Source: fmt.Sprintf("task:%s", ts.TaskID),
	})
	// plan items (pending)
	for _, step := range ts.Pending {
		items = append(items, ContextItem{
			Kind:   "task_fact",
			Text:   fmt.Sprintf("Pending: %s", step),
			Source: fmt.Sprintf("task:%s/plan", ts.TaskID),
		})
	}
	// known facts
	for _, f := range ts.KnownFacts {
		src := f.Source
		if src == "" {
			src = fmt.Sprintf("task:%s/facts", ts.TaskID)
		}
		item := ContextItem{
			Kind:       "task_fact",
			Text:       fmt.Sprintf("[%s] %s", f.Kind, f.Text),
			Source:     src,
			Confidence: 1.0,
		}
		if req.Level >= CtxLevelProvenance {
			item.Provenance = src
		}
		items = append(items, item)
	}
	// unknowns
	for _, u := range ts.Unknowns {
		items = append(items, ContextItem{
			Kind:   "task_fact",
			Text:   fmt.Sprintf("Unknown: %s", u),
			Source: fmt.Sprintf("task:%s/unknowns", ts.TaskID),
		})
	}
	// next action
	if ts.NextAction != "" {
		items = append(items, ContextItem{
			Kind:   "task_fact",
			Text:   fmt.Sprintf("Next: %s", ts.NextAction),
			Source: fmt.Sprintf("task:%s/next", ts.TaskID),
		})
	}
	return items
}

func (c *ContextCompiler) teamItems(tc map[string]any, req ContextRequest) []ContextItem {
	var items []ContextItem
	// knowledge entries from the team context
	if knowledge, ok := tc["knowledge"].([]map[string]any); ok {
		for _, k := range knowledge {
			title, _ := k["title"].(string)
			content, _ := k["content"].(string)
			id, _ := k["id"].(string)
			if req.Level >= CtxLevelCompressed {
				content = snippet(content, req.Question, 160)
			}
			items = append(items, ContextItem{
				Kind:       "team",
				ID:         id,
				Text:       fmt.Sprintf("[team] %s: %s", title, content),
				Source:     "team_knowledge",
				Provenance: id,
			})
		}
	}
	return items
}

// --- level 3: ranking across blocks ---

func (c *ContextCompiler) rankBlocks(blocks []ContextBlock, question string, limit int) []ContextBlock {
	// Collect every item with its block index, score globally, then rebuild the
	// blocks in their original section order keeping the top `limit` items
	// overall. This preserves section structure while still dropping the
	// least-relevant items across all sections.
	type scored struct {
		block int
		item  ContextItem
	}
	var all []scored
	for bi, b := range blocks {
		for _, item := range b.Items {
			if item.Score == 0 {
				item.Score = 0.1
			}
			all = append(all, scored{bi, item})
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].item.Score > all[j].item.Score
	})
	// Keep the top `limit` items (or everything if fewer), remembering which
	// block each kept item came from so sections are rebuilt in order.
	if limit <= 0 {
		limit = len(all)
	}
	kept := make(map[int][]ContextItem, len(blocks))
	for i, s := range all {
		if i >= limit {
			break
		}
		kept[s.block] = append(kept[s.block], s.item)
	}
	var out []ContextBlock
	for i, b := range blocks {
		if items, ok := kept[i]; ok {
			out = append(out, ContextBlock{Section: b.Section, Items: items})
		}
	}
	return out
}

// --- level 4: diff-aware ---

func (c *ContextCompiler) diffAware(projectID, question string, blocks []ContextBlock) []ContextBlock {
	// Pull recently-changed files from Git if available.
	changed, err := c.app.Git.RecentlyChanged(projectID, 20)
	if err != nil || len(changed) == 0 {
		return blocks
	}
	changedSet := map[string]bool{}
	for _, f := range changed {
		changedSet[f] = true
	}
	// Boost score of items whose path appears in the changed set.
	for bi := range blocks {
		for ii := range blocks[bi].Items {
			item := &blocks[bi].Items[ii]
			// Heuristic: path appears in the text or in a path property
			for f := range changedSet {
				if strings.Contains(item.Text, f) {
					item.Score += 0.5
					item.Source += " [changed]"
					break
				}
			}
		}
	}
	return blocks
}

// --- level 5: provenance annotation ---

func (c *ContextCompiler) annotateProvenance(blocks []ContextBlock) {
	for bi := range blocks {
		for ii := range blocks[bi].Items {
			item := &blocks[bi].Items[ii]
			if item.Provenance == "" && item.ID != "" {
				item.Provenance = item.ID
			}
			// Resolve the provenance ID to a human label.
			if item.Provenance != "" && item.ID != "" {
				if node, err := c.app.Graph.GetNode(item.ID); err == nil && node != nil {
					if path, ok := node.Properties["path"].(string); ok && path != "" {
						item.Provenance = fmt.Sprintf("%s@%s", item.ID, path)
					}
				}
			}
		}
	}
}

// --- token budget ---

func (c *ContextCompiler) applyBudget(blocks []ContextBlock, maxTokens int) ([]ContextBlock, bool) {
	used := 0
	truncated := false
	var out []ContextBlock
	for _, b := range blocks {
		var kept []ContextItem
		for _, item := range b.Items {
			cost := approxTextTokens(item.Text) + 20 // ~20 tokens overhead per item
			if used+cost > maxTokens {
				truncated = true
				break
			}
			kept = append(kept, item)
			used += cost
		}
		if len(kept) > 0 {
			out = append(out, ContextBlock{Section: b.Section, Items: kept})
		}
		if truncated {
			break
		}
	}
	return out, truncated
}

// --- checklist ---

func (c *ContextCompiler) buildChecklist(blocks []ContextBlock, req ContextRequest) []string {
	var checks []string
	hasSymbols := false
	hasTask := false
	for _, b := range blocks {
		switch b.Section {
		case "symbols":
			hasSymbols = true
		case "task":
			hasTask = true
		}
	}
	if !hasSymbols {
		checks = append(checks, "No indexed symbols matched — consider running index_repository first.")
	}
	if req.TaskID == "" {
		checks = append(checks, "No task_id supplied — create a task with create_task to persist progress.")
	} else if !hasTask {
		checks = append(checks, fmt.Sprintf("Task %s was not found or is empty — verify the task_id.", req.TaskID))
	}
	if req.Level < CtxLevelProvenance {
		checks = append(checks, "Use level=5 to get provenance annotations on every fact.")
	}
	return checks
}

// --- explanation ---

func (c *ContextCompiler) buildExplanation(notes []string, req ContextRequest, ctx *CompiledContext) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ContextCompiler level=%d assembled %d blocks (~%d tokens",
		req.Level, len(ctx.Blocks), ctx.ApproxTokens))
	if ctx.Truncated {
		sb.WriteString(", truncated to fit budget")
	}
	sb.WriteString(").\n")
	for i, note := range notes {
		sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, note))
	}
	sb.WriteString(fmt.Sprintf("Query: %q | Level: %d", req.Question, req.Level))
	return sb.String()
}

// --- helpers ---

// GoalSummary returns the first 80 chars of the goal.
func (ts *TaskState) GoalSummary() string {
	if len(ts.Goal) <= 80 {
		return ts.Goal
	}
	return ts.Goal[:77] + "..."
}

func buildSymbolText(name, kind, path string, line int, level int) string {
	if level >= CtxLevelCompressed && path != "" {
		if line > 0 {
			return fmt.Sprintf("%s %s (%s:%d)", kind, name, path, line)
		}
		return fmt.Sprintf("%s %s (%s)", kind, name, path)
	}
	return fmt.Sprintf("%s %s", kind, name)
}

// snippet returns a short excerpt of text centred on the first occurrence of
// any query term. Falls back to the first maxLen chars.
func snippet(text, query string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	low := strings.ToLower(text)
	for _, word := range strings.Fields(strings.ToLower(query)) {
		if i := strings.Index(low, word); i >= 0 {
			start := i - 40
			if start < 0 {
				start = 0
			}
			end := start + maxLen
			if end > len(text) {
				end = len(text)
			}
			return text[start:end]
		}
	}
	return text[:maxLen]
}

func approxTextTokens(s string) int {
	// ~4 bytes per token heuristic
	return (len(s) + 3) / 4
}

func approxContextTokens(blocks []ContextBlock) int {
	b, _ := json.Marshal(blocks)
	return (len(b) + 3) / 4
}

// RecentlyChanged returns a list of repo-relative paths modified in the last
// commit or working tree. Returns nil if git is unavailable.
func (s *GitService) RecentlyChanged(projectID string, limit int) ([]string, error) {
	ctx, err := s.PrContext(projectID, "HEAD~1", limit)
	if err != nil {
		return nil, err
	}
	files, _ := ctx["changed_files"].([]map[string]any)
	var out []string
	for _, f := range files {
		if p, ok := f["path"].(string); ok {
			out = append(out, p)
		}
	}
	return out, nil
}
