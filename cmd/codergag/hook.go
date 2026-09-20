package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"codergag/internal/ctxstore"
	"codergag/internal/services"
)

type hookInput struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	Prompt    string `json:"prompt"`
}

func runHook(app *services.Application, args []string) int {
	defer app.Graph.Close()
	event := ""
	if len(args) > 0 {
		event = args[0]
	}
	return hookRun(app, event, os.Stdin, os.Stdout, os.Stderr)
}

func hookRun(app *services.Application, event string, stdin io.Reader, stdout, stderr io.Writer) int {
	if event != "SessionStart" && event != "UserPromptSubmit" {
		return 0
	}
	var in hookInput
	if b, err := io.ReadAll(stdin); err == nil && len(b) > 0 {
		if err := json.Unmarshal(b, &in); err != nil {
			fmt.Fprintln(stderr, "hook: bad input:", err)
			return 0
		}
	}
	cwd := in.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	project, err := ctxstore.ProjectForDir(app, cwd)
	if err != nil || project == "" {
		return 0
	}
	store := ctxstore.New(app)

	var text string
	switch event {
	case "SessionStart":
		prof, _ := ctxstore.LoadProfile(profilePath())
		text, err = store.SessionDigest(project, activeCfg.Context.Session(), prof, codebaseLine(app, project))
	case "UserPromptSubmit":
		if in.Prompt == "" {
			return 0
		}
		seen := ctxstore.LoadSeen(stateDir(), in.SessionID)
		var ids []string
		text, ids, err = store.PromptDigest(project, in.Prompt, activeCfg.Context.Prompt(), seen)
		if err == nil && len(ids) > 0 {
			for _, id := range ids {
				seen[id] = true
			}
			if serr := ctxstore.SaveSeen(stateDir(), in.SessionID, seen); serr != nil {
				fmt.Fprintln(stderr, "hook: save state:", serr)
			}
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "hook:", err)
		return 0
	}
	if text == "" {
		return 0
	}
	json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":     event,
		"additionalContext": text,
	}})
	return 0
}

func codebaseLine(app *services.Application, project string) string {
	files, _ := app.Graph.FindNodes("SourceFile", map[string]any{"project_id": project})
	if len(files) == 0 {
		return ""
	}
	funcs, _ := app.Graph.FindNodes("Function", map[string]any{"project_id": project})
	return fmt.Sprintf("Codebase: %d source files, %d functions indexed (codergag MCP tools available).", len(files), len(funcs))
}
