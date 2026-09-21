package config

import (
	"testing"
	"time"
)

func TestDefaultHasSecurityEnabled(t *testing.T) {
	cfg := Default()
	if !cfg.Security.AutoUpdate {
		t.Error("auto_update should be true by default")
	}
	if cfg.Security.UpdateInterval != "24h" {
		t.Errorf("expected update interval 24h, got %q", cfg.Security.UpdateInterval)
	}
	if cfg.Security.PatternSource == "" {
		t.Error("pattern_source should be set by default")
	}
}

func TestSecurityConfigUpdateIntervalDuration(t *testing.T) {
	cases := []struct {
		input    string
		expected time.Duration
	}{
		{"24h", 24 * time.Hour},
		{"1h", time.Hour},
		{"", 24 * time.Hour},
		{"invalid", 24 * time.Hour},
		{"0s", 24 * time.Hour},
	}
	for _, c := range cases {
		s := SecurityConfig{UpdateInterval: c.input}
		got := s.UpdateIntervalDuration()
		if got != c.expected {
			t.Errorf("interval %q: expected %v, got %v", c.input, c.expected, got)
		}
	}
}
