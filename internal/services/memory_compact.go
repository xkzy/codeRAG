package services

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"codergag/internal/graph"
	"codergag/internal/models"
)

const (
	// autoCompactThreshold is the number of active, entity-linked memories that triggers compaction on Store.
	autoCompactThreshold = 50
	minEntityNameLen     = 4
	maxLinksPerName      = 5
	compactedKind        = "compacted"
)

var identRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

func strSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func isArchived(n *models.Node) bool {
	b, _ := n.Properties["archived"].(bool)
	return b
}

// linkEntities connects a memory to the code entities (functions, classes,
// structs) it mentions, so its knowledge is reachable through graph traversal.
func (s *MemoryService) entityIndex(projectID string) (map[string][]*models.Node, error) {
	index := map[string][]*models.Node{}
	for _, kind := range []string{"Function", "Class", "Struct"} {
		nodes, err := s.graph.FindNodes(kind, map[string]any{"project_id": projectID})
		if err != nil {
			return nil, err
		}
		for _, n := range nodes {
			if name, _ := n.Properties["name"].(string); len(name) >= minEntityNameLen {
				index[name] = append(index[name], n)
			}
		}
	}
	return index, nil
}

func (s *MemoryService) findEntities(projectID, text string) ([]string, []string, error) {
	index, err := s.entityIndex(projectID)
	if err != nil {
		return nil, nil, err
	}
	return entitiesFromIndex(text, index)
}

func (s *MemoryService) linkEntities(projectID string, mem *models.Node, text string) ([]string, []string, error) {
	index, err := s.entityIndex(projectID)
	if err != nil {
		return nil, nil, err
	}
	return s.linkEntitiesFromIndex(mem.ID, text, index)
}

func entitiesFromIndex(text string, index map[string][]*models.Node) ([]string, []string, error) {
	linked := map[string]bool{}
	seenName := map[string]bool{}
	var ids, names []string
	for _, tok := range identRe.FindAllString(text, -1) {
		if seenName[tok] {
			continue
		}
		seenName[tok] = true
		targets := index[tok]
		if len(targets) > maxLinksPerName {
			targets = targets[:maxLinksPerName]
		}
		for _, t := range targets {
			ids = append(ids, t.ID)
			names = append(names, tok)
			linked[t.ID] = true
		}
	}
	return ids, names, nil
}

func (s *MemoryService) linkEntitiesFromIndex(memID, text string, index map[string][]*models.Node) ([]string, []string, error) {
	linked := map[string]bool{}
	if nbrs, err := s.graph.Neighbors(memID, "MENTIONS", graph.DirOut); err == nil {
		for _, en := range nbrs {
			linked[en.Node.ID] = true
		}
	}
	seenName := map[string]bool{}
	var linkedIDs, linkedNames []string
	for _, tok := range identRe.FindAllString(text, -1) {
		if seenName[tok] {
			continue
		}
		seenName[tok] = true
		targets := index[tok]
		if len(targets) > maxLinksPerName {
			targets = targets[:maxLinksPerName]
		}
		for _, t := range targets {
			linkedIDs = append(linkedIDs, t.ID)
			linkedNames = append(linkedNames, tok)
			if !linked[t.ID] {
				if _, err := s.graph.Link("MENTIONS", memID, t.ID, map[string]any{"source": "auto"}); err == nil {
					linked[t.ID] = true
				}
			}
		}
	}
	return linkedIDs, linkedNames, nil
}

func normalizeLine(l string) string {
	return strings.Join(strings.Fields(strings.ToLower(l)), " ")
}

// Compact folds active memories that mention the same code entity into one
// summary node. Nothing is discarded: every unique line is kept in the summary
// and the original memories are archived and linked with SUMMARIZED_BY.
func (s *MemoryService) Compact(projectID string, minGroup int) (map[string]any, error) {
	if minGroup < 2 {
		minGroup = 2
	}
	nodes, err := s.graph.FindNodes("Memory", map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	summaries := map[string]*models.Node{} // entity id -> existing summary
	groups := map[string][]*models.Node{}
	entityName := map[string]string{}
	for _, n := range nodes {
		ids := strSlice(n.Properties["entity_ids"])
		if n.Properties["kind"] == compactedKind {
			for _, id := range ids {
				summaries[id] = n
			}
			continue
		}
		if isArchived(n) || len(ids) == 0 {
			continue
		}
		names := strSlice(n.Properties["entities"])
		key, keyName := ids[0], ""
		for i, id := range ids {
			nm := ""
			if i < len(names) {
				nm = names[i]
			}
			if i == 0 || nm < keyName || (nm == keyName && id < key) {
				key, keyName = id, nm
			}
		}
		entityName[key] = keyName
		groups[key] = append(groups[key], n)
	}

	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	archived, folded, charsBefore, charsAfter := 0, 0, 0, 0
	for _, key := range keys {
		members := groups[key]
		existing := summaries[key]
		if len(members) < minGroup && existing == nil {
			continue
		}
		sort.Slice(members, func(i, j int) bool {
			a, _ := members[i].Properties["created_at"].(string)
			b, _ := members[j].Properties["created_at"].(string)
			if a != b {
				return a < b
			}
			return members[i].ID < members[j].ID
		})

		seen := map[string]bool{}
		var body []string
		var memberIDs []string
		if existing != nil {
			text, _ := existing.Properties["content"].(string)
			body = append(body, text)
			for _, l := range strings.Split(text, "\n") {
				seen[normalizeLine(l)] = true
			}
			memberIDs = strSlice(existing.Properties["member_ids"])
		}
		for _, m := range members {
			title, _ := m.Properties["title"].(string)
			content, _ := m.Properties["content"].(string)
			agent, _ := m.Properties["agent_source"].(string)
			created, _ := m.Properties["created_at"].(string)
			charsBefore += len(title) + len(content)
			var unique []string
			for _, l := range strings.Split(content, "\n") {
				n := normalizeLine(l)
				if n == "" || seen[n] {
					continue
				}
				seen[n] = true
				unique = append(unique, strings.TrimRight(l, " \t"))
			}
			header := fmt.Sprintf("## %s (%s, %s)", title, agent, created)
			if len(unique) == 0 {
				unique = []string{"(content already covered by earlier memories)"}
			}
			body = append(body, header+"\n"+strings.Join(unique, "\n"))
			memberIDs = append(memberIDs, m.ID)
		}
		name := entityName[key]
		if name == "" && existing != nil {
			name = strings.TrimPrefix(fmt.Sprint(existing.Properties["title"]), "Compacted: ")
		}
		short := key
		if len(short) > 8 {
			short = short[:8]
		}
		content := strings.Join(body, "\n\n")
		summary, err := s.graph.UpsertNode("Memory", map[string]any{
			"project_id": projectID,
			"title":      fmt.Sprintf("Compacted: %s [%s]", name, short),
		}, map[string]any{
			"content":         content,
			"kind":            compactedKind,
			"agent_source":    "compactor",
			"source":          "compaction",
			"archived":        false,
			"entity_ids":      []any{key},
			"entities":        []any{name},
			"member_ids":      toAny(memberIDs),
			"compacted_count": len(memberIDs),
		})
		if err != nil {
			return nil, err
		}
		charsAfter += len(content)
		if existing == nil {
			s.graph.Link("MENTIONS", summary.ID, key, map[string]any{"source": "compaction"})
		}
		for _, m := range members {
			title, _ := m.Properties["title"].(string)
			if _, err := s.graph.UpsertNode("Memory", map[string]any{"project_id": projectID, "title": title},
				map[string]any{"archived": true, "compacted_into": summary.ID}); err != nil {
				return nil, err
			}
			s.graph.Link("SUMMARIZED_BY", m.ID, summary.ID, nil)
			archived++
		}
		folded++
	}
	return map[string]any{
		"summaries_updated": folded,
		"memories_archived": archived,
		"chars_before":      charsBefore,
		"chars_after":       charsAfter,
		"lossless":          true,
	}, nil
}

func toAny(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

func (s *MemoryService) activeLinkedCount(projectID string) int {
	nodes, _ := s.graph.FindNodes("Memory", map[string]any{"project_id": projectID})
	n := 0
	for _, m := range nodes {
		if m.Properties["kind"] != compactedKind && !isArchived(m) && len(strSlice(m.Properties["entity_ids"])) > 0 {
			n++
		}
	}
	return n
}
