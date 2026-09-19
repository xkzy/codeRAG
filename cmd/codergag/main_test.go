package main

import (
	"testing"

	"codergag/internal/config"
)

func TestConfigForCommandDisablesDaemonForReadOnlyCommands(t *testing.T) {
	for _, cmd := range []string{"http", "status", "eval", "maintenance"} {
		if configForCommand(cmd, config.Default()).Watch.Enabled {
			t.Errorf("%s must not start the indexing daemon", cmd)
		}
	}
	for _, cmd := range []string{"serve", "watch"} {
		if !configForCommand(cmd, config.Default()).Watch.Enabled {
			t.Errorf("%s must keep the daemon enabled", cmd)
		}
	}
}
