package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"codergag/internal/cli"
)

// runModel is a cctx-compatibility shim. cctx manages a local Ollama model for
// LLM-backed compression; codeRAG has no local LLM — it is an MCP server that
// delegates to whatever model the agent uses. So `model` reports the (empty)
// model inventory and supports `set`/`pull`/`remove` as no-ops that explain the
// difference, rather than silently pretending to manage models.
func runModel(args []string) int {
	fs := flag.NewFlagSet("model", flag.ExitOnError)
	sub := fs.Name()
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs.Parse(args)

	switch sub {
	case "list":
		cli.PrintJSON(os.Stdout, map[string]any{
			"models": []any{},
			"note":   "codeRAG does not run a local LLM; it is an MCP server. The agent's own model is used.",
		})
		return 0
	case "set":
		fmt.Fprintf(os.Stderr, "model set: codeRAG has no local model to set (it is an MCP server, not an LLM app)\n")
		return 0
	case "pull":
		fmt.Fprintf(os.Stderr, "model pull: codeRAG has no local model to pull\n")
		return 0
	case "remove":
		fmt.Fprintf(os.Stderr, "model remove: codeRAG has no local model to remove\n")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown model subcommand %q\n\n", sub)
		fmt.Fprintln(os.Stderr, "Usage: codergag model [list|set|pull|remove]")
		return 2
	}
}