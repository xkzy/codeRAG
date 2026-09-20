package main

import (
	"flag"
	"fmt"
	"os"

	"codergag/internal/services"
)

// runSetup is a cctx-compatibility shim. cctx `setup` installs Ollama, pulls a
// model, registers MCP, and runs an initial index. codeRAG has none of those
// pieces — it is a single binary + SQLite. So `setup` performs the subset that
// makes sense: it prints the MCP registration snippet and points at the
// `index` command, rather than silently pretending to install Ollama.
func runSetup(app *services.Application, args []string) int {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	yes := fs.Bool("yes", false, "non-interactive")
	model := fs.String("model", "phi3.5", "model name (ignored; codeRAG has no local LLM)")
	fs.Parse(args)
	_ = model
	_ = yes

	defer app.Graph.Close()

	fmt.Fprintln(os.Stderr, "codeRAG setup")
	fmt.Fprintln(os.Stderr, "  codeRAG is a single binary + SQLite; there is no Ollama/model to install.")
	fmt.Fprintln(os.Stderr, "  The agent's own model is used; codeRAG is the MCP server.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  To register with Claude Code, add to ~/.claude/config.json:")
	fmt.Fprintln(os.Stderr, `    "mcpServers": { "codergag": { "command": "codergag", "args": ["serve"] } }`)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  To index the current directory now: codergag index run .")
	return 0
}