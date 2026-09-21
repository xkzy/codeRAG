package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codergag/internal/services"
)

// runInject writes a compact codebase map into a file (default CLAUDE.md) so an
// agent that has not yet connected to the MCP server still has a project
// summary. The map is a small Markdown document, not the raw graph.
//
// Uses block markers (<!-- codergag:begin --> … <!-- codergag:end -->) so the
// operation is idempotent and preserves any existing content in the target file,
// mirroring cctx's inject behavior.
func runInject(app *services.Application, args []string) int {
	fs := flag.NewFlagSet("inject", flag.ExitOnError)
	project := fs.String("project", "", "project id (defaults to the first indexed project)")
	out := fs.String("file", "CLAUDE.md", "output file path")
	fs.Parse(args)

	defer app.Graph.Close()

	pid := *project
	if pid == "" {
		p, err := firstProject(app)
		if err != nil {
			fmt.Fprintln(os.Stderr, "inject:", err)
			return 2
		}
		pid = p
	}
	if pid == "" {
		fmt.Fprintln(os.Stderr, "inject: no project indexed; use -project ID")
		return 2
	}

	var b strings.Builder
	b.WriteString("## Codebase map (managed by codergag — do not edit)\n\n")
	b.WriteString(fmt.Sprintf("**Project:** `%s`\n\n", pid))

	files, _ := app.Graph.FindNodes("SourceFile", map[string]any{"project_id": pid})
	b.WriteString(fmt.Sprintf("- %d source files indexed.\n", len(files)))

	funcs, _ := app.Graph.FindNodes("Function", map[string]any{"project_id": pid})
	b.WriteString(fmt.Sprintf("- %d functions.\n", len(funcs)))

	classes, _ := app.Graph.FindNodes("Class", map[string]any{"project_id": pid})
	structs, _ := app.Graph.FindNodes("Struct", map[string]any{"project_id": pid})
	b.WriteString(fmt.Sprintf("- %d classes, %d structs.\n", len(classes), len(structs)))

	b.WriteString("\n### Notes\n\n")
	b.WriteString("- Run `codergag status` for health and usage.\n")
	b.WriteString("- Run `codergag eval -project " + pid + "` for a quality score.\n")
	b.WriteString("- The live knowledge graph is served by the `codergag` MCP server.\n")

	block := injectBegin + "\n" + b.String() + injectEnd + "\n"

	existing := ""
	if data, err := os.ReadFile(*out); err == nil {
		existing = string(data)
	}
	var next string
	if idx := strings.Index(existing, injectBegin); idx >= 0 {
		end := strings.Index(existing[idx:], injectEnd)
		if end >= 0 {
			next = existing[:idx] + block + existing[idx+end+len(injectEnd):]
		} else {
			next = existing + block
		}
	} else {
		next = existing
		if len(strings.TrimSpace(next)) > 0 {
			next = strings.TrimRight(next, " \t\n") + "\n\n" + block
		} else {
			next = block
		}
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "inject:", err)
		return 1
	}
	if err := os.WriteFile(*out, []byte(next), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "inject:", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", *out, len(next))
	return 0
}

// firstProject returns the id of the first indexed project, or "" if none.
func firstProject(app *services.Application) (string, error) {
	nodes, err := app.Graph.FindNodes("Project", nil)
	if err != nil {
		return "", err
	}
	if len(nodes) == 0 {
		return "", nil
	}
	return services.StrProp(nodes[0], "id"), nil
}

const injectBegin = "<!-- codergag:begin -->"
const injectEnd = "<!-- codergag:end -->"
