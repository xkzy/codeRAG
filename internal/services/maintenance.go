package services

import (
	"encoding/json"
	"time"

	"codergag/internal/cache"
)

const keepUsageSessions = 20

// projectIDs lists indexed projects (the internal system project is excluded).
func (a *Application) projectIDs() []string {
	nodes, _ := a.Graph.FindNodes("Project", nil)
	var ids []string
	seen := map[string]bool{}
	for _, n := range nodes {
		id := strProp(n, "id")
		if id != "" && id != systemProject && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// RunMaintenance is the scheduled optimization pass. Every step is idempotent
// and lossless: memories are folded into summaries (originals archived), call
// and inheritance edges are re-resolved, stale cache entries are purged and old
// usage sessions are rolled up. The result is stored so status can show it.
func (a *Application) RunMaintenance() (map[string]any, error) {
	start := time.Now()
	report := map[string]any{}
	compacted, archived := 0, 0
	for _, pid := range a.projectIDs() {
		if stats, err := a.Memory.Compact(pid, 2); err == nil {
			compacted += stats["summaries_updated"].(int)
			archived += stats["memories_archived"].(int)
		}
		a.Index.resolveGraph(pid)
	}
	report["memory_summaries_updated"] = compacted
	report["memories_archived"] = archived
	report["projects"] = len(a.projectIDs())

	purged := 0
	if entries, err := a.Graph.FindNodes("CacheEntry", nil); err == nil {
		var ids []string
		for _, e := range entries {
			if f := strProp(e, "freshness"); f == cache.FreshnessStale || f == cache.FreshnessInvalid {
				ids = append(ids, e.ID)
			}
		}
		if len(ids) > 0 && a.Graph.RemoveNodes(ids) == nil {
			purged = len(ids)
		}
	}
	report["cache_entries_purged"] = purged
	if a.Cache != nil {
		if exactPurged, err := a.Cache.PurgeExact(a.Cache.ExactLimit()); err == nil {
			report["cache_entries_purged"] = purged + exactPurged
		}
	}

	rolled, _ := rollUpUsage(a.Graph, keepUsageSessions)
	report["usage_sessions_rolled_up"] = rolled

	report["duration_ms"] = float64(time.Since(start).Microseconds()) / 1000
	report["ran_at"] = time.Now().UTC().Format(time.RFC3339)
	blob, _ := json.Marshal(report)
	_, err := a.Graph.UpsertNode("MaintenanceRun", map[string]any{"project_id": systemProject, "name": "latest"},
		map[string]any{"report": string(blob), "ran_at": report["ran_at"]})
	return report, err
}

func (a *Application) lastMaintenance() map[string]any {
	nodes, _ := a.Graph.FindNodes("MaintenanceRun", map[string]any{"project_id": systemProject, "name": "latest"})
	if len(nodes) == 0 {
		return nil
	}
	var m map[string]any
	if json.Unmarshal([]byte(strProp(nodes[0], "report")), &m) != nil {
		return nil
	}
	return m
}
