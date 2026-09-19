package mcp

import (
	"testing"
	"time"

	"codergag/internal/services"
)

func TestStatusAndEvalToolsAndUsageRecording(t *testing.T) {
	reg := indexedFunctions(t, 6)
	for i := 0; i < 3; i++ {
		reg.Call("find_function", map[string]any{"project_id": "p", "query": "handler"})
	}
	reg.Call("find_function", map[string]any{"query": "handler"}) // error: no project_id

	st, err := reg.Call("server_status", map[string]any{}) // global tool: no project_id
	if err != nil {
		t.Fatal(err)
	}
	usage := st["usage"].(map[string]any)
	if usage["errors"] != 1 || usage["tool_calls"].(int) < 5 {
		t.Fatalf("usage not recorded: %v", usage)
	}
	if usage["tokens_saved_est"].(int) <= 0 {
		t.Fatalf("shaping savings should be counted: %v", usage)
	}

	ev, err := reg.Call("run_eval", map[string]any{"project_id": "p"})
	if err != nil {
		t.Fatal(err)
	}
	if ev["grade"] == nil || len(ev["metrics"].([]map[string]any)) != 5 {
		t.Fatalf("eval tool result: %v", ev)
	}
	st, _ = reg.Call("server_status", map[string]any{})
	proj := st["projects"].([]map[string]any)[0]
	if proj["last_eval"] == nil {
		t.Fatal("status should show the last eval after run_eval")
	}
}

func TestScheduledMaintenanceRunsAndSharesTheCallLock(t *testing.T) {
	app := services.ApplicationInMemory()
	reg := NewToolRegistry(app)
	reg.Call("memory_store", map[string]any{"project_id": "p", "title": "a", "content": "x", "auto_compact": false})
	stop := reg.StartMaintenance(10 * time.Millisecond)
	defer stop()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if nodes, _ := app.Graph.FindNodes("MaintenanceRun", nil); len(nodes) > 0 {
			// Tool calls keep working while the scheduler is ticking.
			for i := 0; i < 20; i++ {
				if _, err := reg.Call("memory_search", map[string]any{"project_id": "p", "query": "x"}); err != nil {
					t.Fatal(err)
				}
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("scheduled maintenance never ran")
}

func TestSchedulerDisabledWhenIntervalIsZero(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	stop := reg.StartMaintenance(0)
	stop()
}
