package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"codergag/internal/cli"
	"codergag/internal/services"
)

// runSession inspects the recorded tool-usage history. Sessions are persisted
// as UsageSession nodes by every codergag process, so this command lets an
// agent (or a human) review what has been done without running the MCP server.
func runSession(app *services.Application, args []string) int {
	fs := flag.NewFlagSet("session", flag.ExitOnError)
	sub := fs.Name()
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	asJSON := fs.Bool("json", false, "machine-readable output")
	fs.Parse(args)

	defer app.Graph.Close()

	switch sub {
	case "list":
		sessions, err := app.Graph.FindNodes("UsageSession", map[string]any{"project_id": services.SystemProject})
		if err != nil {
			fmt.Fprintln(os.Stderr, "session:", err)
			return 1
		}
		sort.SliceStable(sessions, func(i, j int) bool {
			return services.StrProp(sessions[i], "started_at") > services.StrProp(sessions[j], "started_at")
		})
		out := make([]map[string]any, 0, len(sessions))
		for _, s := range sessions {
			out = append(out, map[string]any{
				"session_id": s.Properties["session_id"],
				"started_at": s.Properties["started_at"],
				"calls":      len(services.DecodeUsage(services.StrProp(s, "usage"))),
			})
		}
		if *asJSON {
			cli.PrintJSON(os.Stdout, map[string]any{"sessions": out, "count": len(out)})
		} else {
			fmt.Fprintf(os.Stderr, "%d sessions recorded\n", len(out))
			for _, s := range out {
				fmt.Fprintf(os.Stderr, "  %s  %v  %v calls\n", s["started_at"], s["session_id"], s["calls"])
			}
		}
		return 0
	case "stats":
		sum, err := services.SummarizeUsage(app.Graph)
		if err != nil {
			fmt.Fprintln(os.Stderr, "session:", err)
			return 1
		}
		if *asJSON {
			cli.PrintJSON(os.Stdout, sum)
		} else {
			fmt.Fprintf(os.Stderr, "codeRAG usage stats\n")
			fmt.Fprintf(os.Stderr, "  sessions   %d\n", sum.Sessions)
			fmt.Fprintf(os.Stderr, "  tool calls %d\n", sum.Total.Calls)
			fmt.Fprintf(os.Stderr, "  errors     %d (%.2f%%)\n", sum.Total.Errors, errorRate(sum.Total))
			fmt.Fprintf(os.Stderr, "  avg ms     %.1f\n", avgMs(sum.Total))
			fmt.Fprintf(os.Stderr, "  tokens     ~%d returned, ~%d saved (%.1f%%)\n",
				sum.Total.ShapedTokens, sum.TokensSaved(), tokenSavingsPct(sum))
			names := sum.TopTools(10)
			for _, n := range names {
				u := sum.ByTool[n]
				fmt.Fprintf(os.Stderr, "    %-22s %5d calls  %d errors  %.1f ms\n",
					n, u.Calls, u.Errors, avgMs(*u))
			}
		}
		return 0
	case "flush":
		n, err := app.Graph.FindNodes("UsageSession", map[string]any{"project_id": services.SystemProject})
		if err != nil {
			fmt.Fprintln(os.Stderr, "session:", err)
			return 1
		}
		ids := make([]string, len(n))
		for i, nd := range n {
			ids[i] = nd.ID
		}
		removed := 0
		if len(ids) > 0 {
			if err := app.Graph.RemoveNodes(ids); err != nil {
				fmt.Fprintln(os.Stderr, "session:", err)
				return 1
			}
			removed = len(ids)
		}
		if *asJSON {
			cli.PrintJSON(os.Stdout, map[string]any{"flushed": removed})
		} else {
			fmt.Fprintf(os.Stderr, "flushed %d usage sessions\n", removed)
		}
		return 0
	case "export":
		sum, err := services.SummarizeUsage(app.Graph)
		if err != nil {
			fmt.Fprintln(os.Stderr, "session:", err)
			return 1
		}
		blob, err := json.Marshal(map[string]any{"summary": sum})
		if err != nil {
			fmt.Fprintln(os.Stderr, "session:", err)
			return 1
		}
		fmt.Print(string(blob))
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown session subcommand %q\n\n", sub)
		fmt.Fprintln(os.Stderr, "Usage: codergag session [list|stats|flush|export] [--json]")
		return 2
	}
}

func errorRate(u services.ToolUsage) float64 {
	if u.Calls == 0 {
		return 0
	}
	return 100 * float64(u.Errors) / float64(u.Calls)
}

func avgMs(u services.ToolUsage) float64 {
	if u.Calls == 0 {
		return 0
	}
	return u.TotalMs / float64(u.Calls)
}

func tokenSavingsPct(s services.UsageSummary) float64 {
	if s.Total.RawTokens == 0 {
		return 0
	}
	return 100 * float64(s.TokensSaved()) / float64(s.Total.RawTokens)
}