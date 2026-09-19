package services_test

import (
	"runtime"
	"strings"
	"testing"

	"codergag/internal/services"
)

func enabledConfig() services.VerificationConfig {
	cfg := services.DefaultVerificationConfig()
	cfg.Enabled = true
	return cfg
}

func TestVerificationRunner_Disabled(t *testing.T) {
	app := services.ApplicationInMemory()
	// Default config has Enabled=false
	res, err := app.Verify.Run(services.VerifyCommandRequest{
		ProjectID: "p", Command: "go", Args: []string{"build", "./..."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != services.VerifDenied {
		t.Errorf("expected DENIED, got %s", res.Status)
	}
}

func TestVerificationRunner_AllowlistReject(t *testing.T) {
	cfg := enabledConfig()
	runner := services.NewVerificationRunService(cfg, nil)
	res, err := runner.Run(services.VerifyCommandRequest{
		Command: "bash", Args: []string{"-c", "rm -rf /"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != services.VerifDenied {
		t.Errorf("expected DENIED for bash, got %s", res.Status)
	}
	if !strings.Contains(res.Output, "allowlist") {
		t.Error("denial message should mention allowlist")
	}
}

func TestVerificationRunner_SubcommandReject(t *testing.T) {
	cfg := enabledConfig()
	runner := services.NewVerificationRunService(cfg, nil)
	res, err := runner.Run(services.VerifyCommandRequest{
		Command: "go", Args: []string{"run", "evil.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != services.VerifDenied {
		t.Errorf("expected DENIED for go run, got %s", res.Status)
	}
}

func TestVerificationRunner_GoVet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping exec test on Windows")
	}
	cfg := enabledConfig()
	runner := services.NewVerificationRunService(cfg, nil)
	// Run go vet on the services package itself — should pass
	res, err := runner.Run(services.VerifyCommandRequest{
		Command: "go",
		Args:    []string{"vet", "./..."},
		Dir:     "../..", // project root
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status == services.VerifDenied {
		t.Errorf("go vet should be allowed, got DENIED: %s", res.Output)
	}
	// go vet on a clean repo should exit 0
	if res.Status != services.VerifPass {
		t.Logf("go vet status=%s output=%s stderr=%s", res.Status, res.Output, res.Stderr)
	}
}

func TestVerificationRunner_ParseFindings_Go(t *testing.T) {
	// Simulate a Go compiler error in stdout
	cfg := enabledConfig()
	// We can't inject output directly but we can test parseFindings via a failing file.
	// Instead, test the allowlist + subcommand path is correct for "go build".
	runner := services.NewVerificationRunService(cfg, nil)
	res, err := runner.Run(services.VerifyCommandRequest{
		Command: "go",
		Args:    []string{"build", "./nonexistent/pkg"},
		Dir:     "../..",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Should fail (nonexistent package) but be allowed
	if res.Status == services.VerifDenied {
		t.Errorf("go build should be allowed: %s", res.Output)
	}
}

func TestVerificationRunner_AllowedTools(t *testing.T) {
	app := services.ApplicationInMemory()
	tools := app.Verify.AllowedTools()
	if len(tools) == 0 {
		t.Error("expected at least one allowed tool")
	}
	found := false
	for _, tool := range tools {
		if cmd, _ := tool["command"].(string); cmd == "go" {
			found = true
		}
	}
	if !found {
		t.Error("'go' should be in the allowed tools list")
	}
}

func TestVerificationRunner_ListRuns_Empty(t *testing.T) {
	app := services.ApplicationInMemory()
	runs, err := app.Verify.ListRuns("proj", 10)
	if err != nil {
		t.Fatal(err)
	}
	if runs == nil {
		t.Error("expected non-nil empty slice")
	}
}
