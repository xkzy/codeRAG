package main

import (
	"testing"

	"codergag/internal/config"
)

func TestConfigForCommandDisablesDaemonForReadOnlyCommands(t *testing.T) {
	for _, cmd := range []string{"http", "status", "eval", "maintenance",
		"session", "inject", "register-instructions", "doctor",
		"setup", "daemon", "model", "config", "uninstall"} {
		if configForCommand(cmd, nil, config.Default()).Watch.Enabled {
			t.Errorf("%s must not start the indexing daemon", cmd)
		}
	}
	for _, cmd := range []string{"serve", "watch"} {
		if !configForCommand(cmd, nil, config.Default()).Watch.Enabled {
			t.Errorf("%s must keep the daemon enabled", cmd)
		}
	}
	// `index watch` must keep the daemon (it is a live watcher), while
	// `index run` and `index status` must not.
	if !configForCommand("index", []string{"watch"}, config.Default()).Watch.Enabled {
		t.Errorf("index watch must keep the daemon enabled")
	}
	if configForCommand("index", []string{"run"}, config.Default()).Watch.Enabled {
		t.Errorf("index run must not start the daemon")
	}
	if configForCommand("index", []string{"status"}, config.Default()).Watch.Enabled {
		t.Errorf("index status must not start the daemon")
	}
}