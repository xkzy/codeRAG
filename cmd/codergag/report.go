package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"codergag/internal/services"
)

func printJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func runStatus(app *services.Application, args []string) int {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "machine-readable output")
	fs.Parse(args)
	defer app.Graph.Close()
	st, err := app.Status()
	if err != nil {
		fmt.Fprintln(os.Stderr, "status:", err)
		return 1
	}
	if *asJSON {
		printJSON(os.Stdout, st)
	} else {
		writeStatus(os.Stdout, st)
	}
	return 0
}

func writeStatus(w io.Writer, st map[string]any) {
	fmt.Fprintln(w, "codeRAG status")
	if projects, _ := st["projects"].([]map[string]any); len(projects) == 0 {
		fmt.Fprintln(w, "\nNo projects indexed yet. Index one with the index_repository tool.")
	} else {
		for _, p := range projects {
			fmt.Fprintf(w, "\nProject %v\n", p["project_id"])
			fmt.Fprintf(w, "  code      %v files (%v generated), %v functions, %v classes, %v structs\n",
				p["files"], p["generated_files"], p["functions"], p["classes"], p["structs"])
			fmt.Fprintf(w, "  indexed   last %v", orDash(p["last_indexed_at"]))
			if n, _ := p["files_on_old_parser"].(int); n > 0 {
				fmt.Fprintf(w, "  ! %d files on an old parser version: re-index", n)
			}
			fmt.Fprintln(w)
			fmt.Fprintf(w, "  memory    %v active, %v archived, %v summaries; docs %v sections\n",
				p["memories_active"], p["memories_archived"], p["memory_summaries"], p["doc_sections"])
			if e, ok := p["last_eval"].(map[string]any); ok {
				fmt.Fprintf(w, "  eval      %v/100 (%v) at %v\n", e["overall"], e["grade"], e["ran_at"])
			} else {
				fmt.Fprintln(w, "  eval      never run: codergag eval -project "+fmt.Sprint(p["project_id"]))
			}
		}
	}
	if u, ok := st["usage"].(map[string]any); ok {
		fmt.Fprintf(w, "\nUsage\n  %v tool calls in %v sessions, %v errors (%v%%), avg %v ms\n",
			u["tool_calls"], u["sessions"], u["errors"], u["error_rate_pct"], u["avg_ms"])
		fmt.Fprintf(w, "  tokens    ~%v returned, ~%v saved by compact responses (%v%%), %v pages cut by budget\n",
			u["tokens_returned_est"], u["tokens_saved_est"], u["token_savings_pct"], u["pages_cut_by_budget"])
		if tools, ok := u["top_tools"].(map[string]any); ok {
			names := make([]string, 0, len(tools))
			for n := range tools {
				names = append(names, n)
			}
			sort.Slice(names, func(i, j int) bool {
				return tools[names[i]].(map[string]any)["calls"].(int) > tools[names[j]].(map[string]any)["calls"].(int)
			})
			for _, n := range names {
				t := tools[n].(map[string]any)
				fmt.Fprintf(w, "    %-22s %5v calls  %v errors  %v ms\n", n, t["calls"], t["errors"], t["avg_ms"])
			}
		}
	}
	if m, ok := st["last_maintenance"].(map[string]any); ok {
		fmt.Fprintf(w, "\nLast maintenance %v: %v summaries updated, %v memories archived, %v cache entries purged, %v usage sessions rolled up\n",
			m["ran_at"], m["memory_summaries_updated"], m["memories_archived"], m["cache_entries_purged"], m["usage_sessions_rolled_up"])
	} else {
		fmt.Fprintln(w, "\nMaintenance has not run yet (scheduled while the server is running, or: codergag maintenance).")
	}
}

func runEval(app *services.Application, args []string) int {
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	project := fs.String("project", "", "project id (required)")
	asJSON := fs.Bool("json", false, "machine-readable output")
	fs.Parse(args)
	defer app.Graph.Close()
	if *project == "" {
		fmt.Fprintln(os.Stderr, "eval: -project is required")
		return 2
	}
	rep, err := app.Eval(*project)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		return 1
	}
	if *asJSON {
		printJSON(os.Stdout, rep)
	} else {
		writeEval(os.Stdout, rep)
	}
	return 0
}

func writeEval(w io.Writer, rep *services.EvalReport) {
	fmt.Fprintf(w, "codeRAG eval for %s: %d/100 (%s)\n\n", rep.ProjectID, rep.Overall, strings.ToUpper(rep.Grade))
	for _, m := range rep.Metrics {
		if m.NA {
			fmt.Fprintf(w, "  %-17s  n/a   %s\n", m.Name, m.Detail)
			continue
		}
		fmt.Fprintf(w, "  %-17s %3.0f%%   %s\n", m.Name, m.Score*100, m.Detail)
	}
	if len(rep.Advice) > 0 {
		fmt.Fprintln(w, "\nTo improve:")
		for _, a := range rep.Advice {
			fmt.Fprintln(w, "  -", a)
		}
	}
}

func runMaintenance(app *services.Application) int {
	defer app.Graph.Close()
	rep, err := app.RunMaintenance()
	if err != nil {
		fmt.Fprintln(os.Stderr, "maintenance:", err)
		return 1
	}
	printJSON(os.Stdout, rep)
	return 0
}

func orDash(v any) any {
	if s, _ := v.(string); s == "" {
		return "-"
	}
	return v
}
