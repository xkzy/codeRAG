package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codergag/internal/cli"
	"codergag/internal/config"
)

// runConfig manages the codeRAG config file. It reads/edits the YAML at
// CODERAG_CONFIG (default ~/.codergag.yaml) without starting the server.
func runConfig(args []string) int {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	sub := fs.Name()
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs.Parse(args)

	path := os.Getenv("CODERAG_CONFIG")
	if path == "" {
		path = filepath.Join(homeDir(), ".codergag.yaml")
	}

	switch sub {
	case "show":
		cfg, err := config.Load(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "config:", err)
			return 1
		}
		cli.PrintJSON(os.Stdout, map[string]any{"path": path, "config": cfg})
		return 0
	case "get":
		if fs.NArg() < 1 {
			fmt.Fprintln(os.Stderr, "config get: key required")
			return 2
		}
		cfg, err := config.Load(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "config:", err)
			return 1
		}
		v, err := config.ConfigGet(cfg, fs.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, "config get:", err)
			return 1
		}
		cli.PrintJSON(os.Stdout, map[string]any{"key": fs.Arg(0), "value": v})
		return 0
	case "set":
		if fs.NArg() < 2 {
			fmt.Fprintln(os.Stderr, "config set: key value required")
			return 2
		}
		cfg, err := config.Load(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "config:", err)
			return 1
		}
		if err := config.ConfigSet(&cfg, fs.Arg(0), fs.Arg(1)); err != nil {
			fmt.Fprintln(os.Stderr, "config set:", err)
			return 1
		}
		if err := cfg.Save(path); err != nil {
			fmt.Fprintln(os.Stderr, "config:", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "set %s in %s\n", fs.Arg(0), path)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown config subcommand %q\n\n", sub)
		fmt.Fprintln(os.Stderr, "Usage: codergag config [show|get|set]")
		return 2
	}
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return "."
}