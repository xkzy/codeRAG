package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"codergag/internal/config"
	"codergag/internal/mcp"
	"codergag/internal/services"
)

const usage = `codergag - shared code knowledge graph for LLM agents

Usage:
  codergag [serve]                 run the MCP server on stdio (default)
  codergag watch [--interval 1s]   live daemon status on stderr (indexes as files change)
  codergag http [--addr :8080]     local agent/memory dashboard
  codergag status [--json]         health, usage and token-savings report
  codergag eval -project ID [--json]   score retrieval, resolution, freshness, memory
  codergag maintenance             run the optimization pass once

Environment:
  CODERAG_CONFIG                   path to config yaml
  CODERAG_MAINTENANCE_INTERVAL     scheduled optimization period (default 15m, 0 disables)
`

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
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
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
