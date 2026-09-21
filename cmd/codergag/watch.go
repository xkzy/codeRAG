package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"codergag/internal/services"
)

// runWatch starts the daemon and renders a live status line to stderr.
// Nothing is written to stdout, so it never interferes with MCP framing.
func runWatch(app *services.Application, args []string) int {
	interval := time.Second
	if len(args) > 1 && args[0] == "--interval" {
		d, err := time.ParseDuration(args[1])
		if err != nil || d <= 0 {
			fmt.Fprintln(os.Stderr, "watch: invalid --interval")
			return 2
		}
		interval = d
	}
	if app.Daemon == nil {
		fmt.Fprintln(os.Stderr, "watch: daemon not configured (enable watch in config)")
		return 1
	}
	app.Daemon.Start()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	start := time.Now()
	interactive := isTerminal(os.Stderr)
	for {
		select {
		case <-sig:
			app.Daemon.Stop()
			app.Events.Stop()
			if err := app.Graph.Close(); err != nil {
				fmt.Fprintln(os.Stderr, "close:", err)
			}
			fmt.Fprintln(os.Stderr)
			return 0
		case <-ticker.C:
			line := renderWatchLine(app, time.Since(start))
			if interactive {
				fmt.Fprintf(os.Stderr, "\r\033[K%s", line)
			} else {
				fmt.Fprintln(os.Stderr, line)
			}
		}
	}
}

func renderWatchLine(app *services.Application, uptime time.Duration) string {
	st := app.Daemon.Status()
	m := app.Events.Metrics()
	rate := 0.0
	if s := uptime.Seconds(); s > 0 {
		rate = float64(m["events_processed"]) / s
	}
	cacheInfo := "cache n/a"
	if app.Cache != nil {
		cs := app.Cache.Stats()
		hits := cs.ExactHits + cs.SemanticHits + cs.ToolHits + cs.AnalysisHits + cs.GraphHits + cs.PartialHits
		if total := hits + cs.CacheMisses; total > 0 {
			cacheInfo = fmt.Sprintf("cache %.0f%% hit, ~%d tok saved", 100*float64(hits)/float64(total), cs.TokensSaved)
		} else {
			cacheInfo = "cache idle"
		}
	}
	return fmt.Sprintf("codergag watch | up %s | projects %v | active %v | jobs %v/%v | queue %v | events %d (%.1f/s) dropped %d errors %d | "+cacheInfo,
		uptime.Truncate(time.Second), st["projects"], st["active_agents"], st["background_jobs"], st["max_workers"],
		st["event_queue"], m["events_seen"], rate, m["events_dropped"], m["worker_errors"])
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
