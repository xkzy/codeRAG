package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"codergag/internal/cli"
)

// runUninstall removes cctx-installed components. It mirrors cctx's
// `--keep-models` flag but operates on codeRAG's own artifacts: the config
// file, the database, and the installed binary (if it was installed via
// install.sh).
func runUninstall(args []string) int {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	keepModels := fs.Bool("keep-models", false, "preserve downloaded models (no-op for codeRAG)")
	keepDB := fs.Bool("keep-db", false, "preserve the codeRAG database")
	fs.Parse(args)
	_ = keepModels

	removed := 0
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "."
	}

	candidates := []string{
		home + "/.codergag.yaml",
		home + "/.codergag/instructions.md",
		".codergag.db",
	}
	for _, p := range candidates {
		if *keepDB && (strings.HasSuffix(p, ".db") || strings.Contains(p, ".codergag")) {
			continue
		}
		if err := os.Remove(p); err == nil {
			fmt.Fprintf(os.Stderr, "removed %s\n", p)
			removed++
		}
	}

	// Also try the install.sh-managed locations.
	for _, p := range []string{
		"/usr/local/bin/codergag",
		"/usr/local/bin/cctx",
		home + "/.codergag",
	} {
		if info, err := os.Lstat(p); err == nil && !info.IsDir() {
			if err := os.Remove(p); err == nil {
				fmt.Fprintf(os.Stderr, "removed %s\n", p)
				removed++
			}
		}
	}

	cli.PrintJSON(os.Stdout, map[string]any{"removed": removed, "keep_db": *keepDB})
	if removed == 0 {
		fmt.Fprintln(os.Stderr, "nothing to remove")
		return 1
	}
	return 0
}