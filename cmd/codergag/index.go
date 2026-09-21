package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"codergag/internal/cli"
	"codergag/internal/services"
)

// runIndex manages the codebase semantic index. It wraps
// CodeIndexService.IndexRepository so an agent can trigger a (re)index from the
// CLI without going through the MCP server.
func runIndex(app *services.Application, args []string) int {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	sub := fs.Name()
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	incremental := fs.Bool("incremental", true, "skip unchanged files")
	force := fs.Bool("force", false, "re-index all files even if unchanged")
	path := fs.String("path", "", "project path (defaults to current directory or first positional arg)")
	ignore := fs.String("ignore", ".git,build,node_modules", "comma-separated ignore list")
	skipGraphify := fs.Bool("skip-graphify", false, "skip the graphify community pass")
	fs.Parse(args)

	defer app.Graph.Close()

	switch sub {
	case "run":
		root := "."
		if *path != "" {
			root = *path
		} else if fs.NArg() > 0 {
			root = fs.Arg(0)
		}
		pid := services.StableProjectID(root)
		ign := splitList(*ignore)
		incr := *incremental
		if *force {
			incr = false
		}
		res, err := app.Index.IndexRepository(pid, root, incr, ign, *skipGraphify)
		if err != nil {
			fmt.Fprintln(os.Stderr, "index:", err)
			return 1
		}
		cli.PrintJSON(os.Stdout, res)
		return 0
	case "status":
		projects := []*services.ProjectIdentity{}
		if app.Daemon != nil {
			projects = app.Daemon.Projects()
		}
		out := make([]map[string]any, 0, len(projects))
		for _, p := range projects {
			out = append(out, map[string]any{
				"project_id": p.ID,
				"root":       p.Root,
				"progress":   app.Index.Progress(p.ID),
			})
		}
		cli.PrintJSON(os.Stdout, map[string]any{"projects": out})
		return 0
	case "watch":
		// The watch daemon is the same as `codergag watch`; delegate so the
		// user gets one consistent behavior.
		return runWatch(app, fs.Args())
	default:
		fmt.Fprintf(os.Stderr, "unknown index subcommand %q\n\n", sub)
		fmt.Fprintln(os.Stderr, "Usage: codergag index [run|status|watch] [root]")
		return 2
	}
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	out := make([]string, 0, len(s))
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}