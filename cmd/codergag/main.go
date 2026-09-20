package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"codergag/internal/config"
	"codergag/internal/mcp"
	"codergag/internal/services"
)

const usage = "codergag - shared code knowledge graph for LLM agents\n\n" +
	"Usage:\n" +
	"  codergag [serve]                 run the MCP server on stdio (default)\n" +
	"  codergag setup [--yes]           first-time setup: write config, register MCP\n" +
	"  codergag daemon [start|stop|restart|status]   manage the indexing daemon\n" +
	"  codergag model [list|set|pull|remove]         manage local models (compat shim)\n" +
	"  codergag index [run|status|watch] [root]      manage the codebase semantic index\n" +
	"  codergag watch [--interval 1s]   live daemon status on stderr (indexes as files change)\n" +
	"  codergag http [--addr :8080]     local agent/memory dashboard\n" +
	"  codergag status [--json]         health, usage and token-savings report\n" +
	"  codergag eval -project ID [--json]   score retrieval, resolution, freshness, memory\n" +
	"  codergag maintenance             run the optimization pass once\n" +
	"  codergag session [list|stats|flush|export] [--json]   inspect recorded tool usage\n" +
	"  codergag inject [--project ID] [--file CLAUDE.md]   write a compact codebase map into a file\n" +
	"  codergag register-instructions [--agent claude|all]   write tool instructions and register the MCP server\n" +
	"  codergag doctor [--json]          health checks: graph, cache, daemon, config, storage\n" +
	"  codergag config [show|get|set]   manage configuration\n" +
	"  codergag uninstall [--keep-db]   remove codeRAG components\n" +
	"  codergag mcp                     start as MCP server (internal; use serve)\n" +
	"\n" +
	"Environment:\n" +
	"  CODERAG_CONFIG                   path to config yaml\n" +
	"  CODERAG_HTTP_ADDR                also serve the dashboard from serve (e.g. 127.0.0.1:8080)\n" +
	"  CODERAG_MAINTENANCE_INTERVAL     scheduled optimization period (default 15m, 0 disables)\n"

func main() {
	cmd, args := "serve", os.Args[1:]
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	if cmd == "help" || cmd == "-h" || cmd == "--help" {
		fmt.Print(usage)
		return
	}

	cfg := config.Default()
	if path := os.Getenv("CODERAG_CONFIG"); path != "" {
		loaded, err := config.Load(path)
		if err != nil {
			fail("config:", err)
		}
		cfg = loaded
	}
	cfg = configForCommand(cmd, args, cfg)
	applyMemoryLimit()
	app, err := services.ApplicationFromConfig(&cfg)
	if err != nil {
		fail("init:", err)
	}

	switch cmd {
	case "serve":
		serve(app)
	case "http":
		runHTTP(app, args)
	case "watch":
		os.Exit(runWatch(app, args))
	case "status":
		os.Exit(runStatus(app, args))
	case "eval":
		os.Exit(runEval(app, args))
	case "maintenance":
		os.Exit(runMaintenance(app))
	case "session":
		os.Exit(runSession(app, args))
	case "inject":
		os.Exit(runInject(app, args))
	case "register-instructions":
		os.Exit(runRegisterInstructions(app, args))
	case "doctor":
		os.Exit(runDoctor(app, args))
	case "setup":
		os.Exit(runSetup(app, args))
	case "daemon":
		os.Exit(runDaemon(app, args))
	case "model":
		os.Exit(runModel(args))
	case "index":
		os.Exit(runIndex(app, args))
	case "config":
		os.Exit(runConfig(args))
	case "uninstall":
		os.Exit(runUninstall(args))
	case "mcp":
		serve(app)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
}

// configForCommand disables the background indexing daemon for read-only
// commands. Otherwise `http` (often started from an arbitrary cwd such as
// $HOME) would scan and index the default watch root "." and exhaust memory.
// Every command that only reads the graph or writes a file must be listed
// here; `serve`, `watch`, and `index watch` intentionally keep the daemon.
func configForCommand(cmd string, args []string, cfg config.Config) config.Config {
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
	}
	// Only `index watch` keeps the daemon; every other index subcommand
	// (run/status) is a one-shot read or write that must not start a
	// background scanner from an arbitrary cwd.
	if cmd == "index" && sub != "watch" {
		cfg.Watch.Enabled = false
		return cfg
	}
	switch cmd {
	case "serve", "watch":
		// keep the daemon
	case "http", "status", "eval", "maintenance",
		"session", "inject", "register-instructions", "doctor",
		"setup", "daemon", "model", "config", "uninstall":
		cfg.Watch.Enabled = false
	}
	return cfg
}

func fail(msg string, err error) {
	fmt.Fprintln(os.Stderr, msg, err)
	os.Exit(1)
}

func serve(app *services.Application) {
	reg := mcp.NewToolRegistry(app)
	interval := 15 * time.Minute
	if v := os.Getenv("CODERAG_MAINTENANCE_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			if v == "0" {
				d = 0
			} else {
				fail("CODERAG_MAINTENANCE_INTERVAL:", err)
			}
		}
		interval = d
	}
	stopMaintenance := reg.StartMaintenance(interval)

	// Optional dashboard. Several serve processes may share one port; the first
	// wins and the rest continue without it.
	if addr := os.Getenv("CODERAG_HTTP_ADDR"); addr != "" {
		if bound, stop, err := services.NewHTTPServer(app, addr).StartBackground(); err != nil {
			fmt.Fprintln(os.Stderr, "dashboard not started:", err)
		} else {
			fmt.Fprintf(os.Stderr, "dashboard at http://%s\n", bound)
			defer stop()
		}
	}

	shutdown := func() {
		stopMaintenance()
		if app.Daemon != nil {
			app.Daemon.Stop()
		}
		if app.Events != nil {
			app.Events.Stop()
		}
		reg.FlushUsage()
		if err := app.Graph.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "close:", err)
		}
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		shutdown()
		os.Exit(0)
	}()

	err := mcp.NewServer(reg).Run()
	shutdown()
	if err != nil {
		fail("server:", err)
	}
}

func runHTTP(app *services.Application, args []string) {
	addr := ":8080"
	if len(args) > 0 {
		addr = args[0]
	}
	srv := services.NewHTTPServer(app, addr)
	fmt.Fprintf(os.Stderr, "agent view at http://%s\n", addr)
	if err := srv.ListenAndServe(); err != nil {
		fail("http:", err)
	}
}

// applyMemoryLimit sets a soft Go heap limit so the GC works harder before the
// process grows without bound. CODERAG_MEMORY_LIMIT_MB overrides the 1 GiB
// default; 0 disables the limit.
func applyMemoryLimit() {
	mb := 1024
	if v := os.Getenv("CODERAG_MEMORY_LIMIT_MB"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			fail("CODERAG_MEMORY_LIMIT_MB:", fmt.Errorf("invalid value %q", v))
		}
		mb = n
	}
	if mb > 0 {
		debug.SetMemoryLimit(int64(mb) << 20)
	}
}
