package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"codergag/internal/cli"
)

// runUninstall removes codeRAG-installed components. It mirrors cctx's
// `--keep-models` flag but operates on codeRAG's own artifacts: the config
// file, the database, the instructions, the global CLAUDE.md block, and the
// installed binary (if it was installed via install.sh).
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

	if changed, err := updateSettingsFile(defaultSettingsPath(), true); err != nil {
		fmt.Fprintln(os.Stderr, "uninstall: hooks:", err)
	} else if changed {
		fmt.Fprintf(os.Stderr, "removed codergag hooks from %s\n", defaultSettingsPath())
		removed++
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
	if removedBlock := removeImportBlock(home + "/.claude/CLAUDE.md"); removedBlock {
		fmt.Fprintf(os.Stderr, "removed instructions block from %s/.claude/CLAUDE.md\n", home)
		removed++
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

// removeImportBlock removes the codergag instructions block from ~/.claude/CLAUDE.md,
// mirroring cctx's unregisterGlobalInstructions. If the block was the only content,
// the file is deleted. Returns true if the block was found and removed.
func removeImportBlock(claudeMD string) bool {
	b, err := os.ReadFile(claudeMD)
	if err != nil {
		return false
	}
	existing := string(b)
	idx := strings.Index(existing, claudeBegin)
	if idx < 0 {
		return false
	}
	end := strings.Index(existing[idx:], claudeEnd)
	if end < 0 {
		return false
	}
	before := existing[:idx]
	after := existing[idx+end+len(claudeEnd):]
	next := strings.TrimSpace(before + after)
	if next == "" {
		if err := os.Remove(claudeMD); err != nil {
			return false
		}
		return true
	}
	if err := os.WriteFile(claudeMD, []byte(next+"\n"), 0o644); err != nil {
		return false
	}
	return true
}
