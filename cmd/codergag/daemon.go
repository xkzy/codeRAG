package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"codergag/internal/cli"
	"codergag/internal/services"
)

// runDaemon manages the background indexing daemon. It is a thin wrapper over
// services.Application.Daemon so the CLI can start/stop/restart it without
// going through the MCP server.
func runDaemon(app *services.Application, args []string) int {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	sub := fs.Name()
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs.Parse(args)

	switch sub {
	case "start":
		if app.Daemon == nil {
			fmt.Fprintln(os.Stderr, "daemon: daemon not configured")
			return 1
		}
		app.Daemon.Start()
		fmt.Fprintln(os.Stderr, "daemon started")
		return 0
	case "stop":
		if app.Daemon == nil {
			fmt.Fprintln(os.Stderr, "daemon: daemon not configured")
			return 1
		}
		app.Daemon.Stop()
		fmt.Fprintln(os.Stderr, "daemon stopped")
		return 0
	case "restart":
		if app.Daemon == nil {
			fmt.Fprintln(os.Stderr, "daemon: daemon not configured")
			return 1
		}
		app.Daemon.Stop()
		app.Daemon.Start()
		fmt.Fprintln(os.Stderr, "daemon restarted")
		return 0
	case "status":
		st := daemonStatus(app)
		cli.PrintJSON(os.Stdout, st)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown daemon subcommand %q\n\n", sub)
		fmt.Fprintln(os.Stderr, "Usage: codergag daemon [start|stop|restart|status]")
		return 2
	}
}

func daemonStatus(app *services.Application) map[string]any {
	if app.Daemon == nil {
		return map[string]any{"running": false, "reason": "daemon not configured"}
	}
	st := app.Daemon.Status()
	st["running"] = true
	return st
}