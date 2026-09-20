package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"codergag/internal/services"
)

// runRegisterInstructions writes the tool instructions into a file and prints
// the MCP registration snippet for the agent's config.
func runRegisterInstructions(app *services.Application, args []string) int {
	fs := flag.NewFlagSet("register-instructions", flag.ExitOnError)
	agent := fs.String("agent", "claude", "agent to register (claude|all)")
	out := fs.String("file", "", "instructions file path (default ~/.codergag/instructions.md)")
	fs.Parse(args)
	defer app.Graph.Close()

	path := *out
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "register-instructions:", err)
			return 1
		}
		path = home + "/.codergag/instructions.md"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "register-instructions:", err)
		return 1
	}
	if err := os.WriteFile(path, []byte(instructionsMD), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "register-instructions:", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", path)
	fmt.Fprintln(os.Stderr, "register the MCP server in your agent config:")
	fmt.Fprintln(os.Stderr, "  claude:   codergag serve")
	fmt.Fprintln(os.Stderr, "  opencode: codergag serve")
	fmt.Fprintln(os.Stderr, "  gemini:   codergag serve")
	if *agent == "all" {
		fmt.Fprintln(os.Stderr, "(applies to every agent that speaks MCP stdio)")
	}
	return 0
}

const instructionsMD = `# codeRAG instructions

codeRAG is a local code-knowledge graph served as an MCP server. Use it instead of
reading files or grepping when you need relationships between symbols.

## Start

- ` + "`codergag status`" + ` — health, usage, token savings
- ` + "`codergag eval -project ID`" + ` — quality score (0-100)

## Indexing

- ` + "`index_repository`" + ` — index a project root
- ` + "`index_files`" + ` — index specific changed files
- ` + "`index_progress`" + ` — live progress of a long-running index

## Search

- ` + "`search_code_graph`" + ` — BM25 keyword search
- ` + "`search_semantic`" + ` — BM25 + vector fusion
- ` + "`find_function`" + ` / ` + "`find_symbol`" + ` — exact/substring lookup

## Graph

- ` + "`get_callers`" + `, ` + "`get_callees`" + ` — call edges
- ` + "`impact_analysis`" + ` — what breaks if I change this
- ` + "`trace_call_path`" + `, ` + "`trace_data_flow`" + ` — traversal
- ` + "`query_graph`" + ` — read-only SELECT/MATCH (expert only)

## Memory

- ` + "`memory_store`" + ` / ` + "`memory_search`" + ` — persistent agent memory
- ` + "`memory_compact`" + ` — fold memories into summaries

## Privacy

- ` + "`sanitize_context`" + `, ` + "`redact_content`" + `, ` + "`pseudonymize_symbol`" + `
`