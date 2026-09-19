package mcp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"codergag/internal/graph"
	"codergag/internal/services"
)

const helperEnv = "CODERAG_MP_HELPER"

// TestMultiProcessHelper is the body of each child process. It only runs when
// the parent test re-executes the test binary with helperEnv set.
func TestMultiProcessHelper(t *testing.T) {
	db := os.Getenv(helperEnv)
	if db == "" {
		t.Skip("helper process only")
	}
	id, _ := strconv.Atoi(os.Getenv("CODERAG_MP_ID"))
	repo, err := graph.NewPersistentRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewToolRegistry(services.NewApplication(repo))
	for i := 0; i < 15; i++ {
		_, err := reg.Call("memory_store", map[string]any{
			"project_id": "p", "title": fmt.Sprintf("proc%d-note%d", id, i),
			"content": "written concurrently", "auto_compact": false,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMultipleProcessesShareOneGraphFile(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	db := filepath.Join(t.TempDir(), "shared.gob")
	const procs = 4
	cmds := make([]*exec.Cmd, procs)
	outs := make([][]byte, procs)
	done := make(chan int, procs)
	for i := range cmds {
		cmds[i] = exec.Command(os.Args[0], "-test.run=^TestMultiProcessHelper$")
		cmds[i].Env = append(os.Environ(), helperEnv+"="+db, "CODERAG_MP_ID="+strconv.Itoa(i))
		i := i
		go func() {
			outs[i], _ = cmds[i].CombinedOutput()
			done <- i
		}()
	}
	for range cmds {
		<-done
	}
	for i, c := range cmds {
		if !c.ProcessState.Success() {
			t.Fatalf("process %d failed:\n%s", i, outs[i])
		}
	}

	repo, err := graph.NewPersistentRepository(db)
	if err != nil {
		t.Fatalf("graph file corrupted by concurrent writers: %v", err)
	}
	defer repo.Close()
	mems, _ := repo.FindNodes("Memory", map[string]any{"project_id": "p"})
	if len(mems) != procs*15 {
		t.Fatalf("expected %d memories from %d processes, got %d (lost or duplicated writes)", procs*15, procs, len(mems))
	}
	seen := map[string]bool{}
	for _, m := range mems {
		title := m.Properties["title"].(string)
		if seen[title] {
			t.Fatalf("duplicate memory %s", title)
		}
		seen[title] = true
	}
}
