package services

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"codergag/internal/graph"
	"codergag/internal/models"
)

// VerificationConfig controls which commands the runner is allowed to execute.
// The zero value disables everything. Populated from config.VerificationConfig by
// ApplicationFromConfig. The two types are structurally identical so a field-by-field
// copy is used to avoid an import cycle.
type VerificationConfig struct {
	// Enabled globally enables the runner. Off by default.
	Enabled bool
	// AllowedCommands is the set of executable names that may be invoked.
	AllowedCommands []string
	// AllowedSubcommands restricts which sub-verbs a binary may use.
	AllowedSubcommands map[string][]string
	// TimeoutSeconds caps a single run. Default 120.
	TimeoutSeconds int
	// MaxOutputBytes caps captured stdout+stderr. Default 64 KB.
	MaxOutputBytes int
}

func DefaultVerificationConfig() VerificationConfig {
	return VerificationConfig{
		Enabled: false,
		AllowedCommands: []string{
			"go", "cargo", "python3", "python", "pytest", "npm", "npx",
			"gradle", "mvn", "make", "cmake",
		},
		AllowedSubcommands: map[string][]string{
			"go":      {"build", "test", "vet", "fmt", "generate"},
			"cargo":   {"build", "test", "check", "clippy", "fmt"},
			"python":  {"-m"},
			"python3": {"-m"},
			"npm":     {"run", "test"},
			"npx":     {"jest", "mocha"},
			"make":    {}, // any target
			"cmake":   {"--build"},
		},
		TimeoutSeconds: 120,
		MaxOutputBytes: 64 * 1024,
	}
}

// VerificationStatus is the outcome of a verification run.
type VerificationStatus string

const (
	VerifPass    VerificationStatus = "PASS"
	VerifFail    VerificationStatus = "FAIL"
	VerifError   VerificationStatus = "ERROR"
	VerifDenied  VerificationStatus = "DENIED"
	VerifTimeout VerificationStatus = "TIMEOUT"
)

// VerificationResult holds one run's outcome plus parsed annotations.
type VerificationResult struct {
	Command  string             `json:"command"`
	Args     []string           `json:"args"`
	Dir      string             `json:"dir,omitempty"`
	Status   VerificationStatus `json:"status"`
	ExitCode int                `json:"exit_code"`
	Duration string             `json:"duration"`
	Output   string             `json:"output,omitempty"`
	Stderr   string             `json:"stderr,omitempty"`
	// Findings are structured diagnostics extracted from the output.
	Findings []VerificationFinding `json:"findings,omitempty"`
	// RecordedID is the graph node ID if the result was persisted.
	RecordedID string `json:"recorded_id,omitempty"`
}

// VerificationFinding is a single diagnostic (error / warning) extracted from output.
type VerificationFinding struct {
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Col     int    `json:"col,omitempty"`
	Kind    string `json:"kind"` // "error" | "warning" | "info"
	Message string `json:"message"`
	Tool    string `json:"tool"`
}

// VerificationRunner runs allowlisted build/test/analyze commands.
type VerificationRunner struct {
	cfg   VerificationConfig
	graph graph.GraphRepository
}

func NewVerificationRunner(cfg VerificationConfig, g graph.GraphRepository) *VerificationRunner {
	return &VerificationRunner{cfg: cfg, graph: g}
}

// VerifyRequest specifies what to run.
type VerifyCommandRequest struct {
	ProjectID string
	// Command is the executable name (e.g. "go"). Must be in AllowedCommands.
	Command string
	// Args are the arguments (e.g. ["test", "./..."]).
	Args []string
	// Dir is the working directory. Defaults to the project root.
	Dir string
	// Env is extra environment variables (merged with the current env).
	Env map[string]string
	// RecordResult persists the result as a VerificationRun node in the graph.
	RecordResult bool
	// Agent is the agent name for provenance.
	Agent string
}

// Run executes the command if it passes the allowlist, records the result, and
// returns structured output.
func (r *VerificationRunner) Run(req VerifyCommandRequest) (*VerificationResult, error) {
	if !r.cfg.Enabled {
		return &VerificationResult{
			Command: req.Command, Args: req.Args,
			Status: VerifDenied,
			Output: "Verification runner is disabled. Set verification.enabled=true in config.",
		}, nil
	}

	// Check the allowlist
	if err := r.checkAllowlist(req.Command, req.Args); err != nil {
		return &VerificationResult{
			Command: req.Command, Args: req.Args,
			Status: VerifDenied,
			Output: err.Error(),
		}, nil
	}

	// Resolve working directory
	dir := req.Dir
	if dir == "" {
		root, err := r.projectRoot(req.ProjectID)
		if err != nil {
			return nil, err
		}
		dir = root
	}

	timeout := time.Duration(r.cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, req.Command, req.Args...)
	cmd.Dir = dir
	cmd.Env = r.buildEnv(req.Env)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	dur := time.Since(start)

	maxOut := r.cfg.MaxOutputBytes
	if maxOut <= 0 {
		maxOut = 64 * 1024
	}

	outStr := truncateOutput(stdout.String(), maxOut/2)
	errStr := truncateOutput(stderr.String(), maxOut/2)

	status := VerifPass
	exitCode := 0
	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			status = VerifTimeout
		} else {
			status = VerifFail
		}
		if ee, ok := runErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			status = VerifError
		}
	}

	findings := parseFindings(outStr+"\n"+errStr, req.Command)

	res := &VerificationResult{
		Command:  req.Command,
		Args:     req.Args,
		Dir:      dir,
		Status:   status,
		ExitCode: exitCode,
		Duration: dur.Round(time.Millisecond).String(),
		Output:   outStr,
		Stderr:   errStr,
		Findings: findings,
	}

	if req.RecordResult {
		id, err := r.record(req.ProjectID, req.Agent, res)
		if err == nil {
			res.RecordedID = id
		}
	}

	return res, nil
}

// --- allowlist ---

func (r *VerificationRunner) checkAllowlist(command string, args []string) error {
	// Strip path — only the base name is compared.
	base := filepath.Base(command)

	allowed := false
	for _, c := range r.cfg.AllowedCommands {
		if filepath.Base(c) == base {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("command %q is not in the allowlist (allowed: %s)",
			base, strings.Join(r.cfg.AllowedCommands, ", "))
	}

	// If subcommand restrictions are defined for this binary, check them.
	subs, hasSubs := r.cfg.AllowedSubcommands[base]
	if !hasSubs {
		return nil // no restriction on sub-verbs
	}
	if len(subs) == 0 {
		return nil // empty slice means any subcommand
	}
	if len(args) == 0 {
		return nil // no arg to check
	}
	firstArg := args[0]
	for _, s := range subs {
		if s == firstArg {
			return nil
		}
	}
	return fmt.Errorf("subcommand %q is not allowed for %q (allowed: %s)",
		firstArg, base, strings.Join(subs, ", "))
}

// --- project root ---

func (r *VerificationRunner) projectRoot(projectID string) (string, error) {
	nodes, err := r.graph.FindNodes("Project", map[string]any{"id": projectID})
	if err != nil || len(nodes) == 0 {
		return "", &ServiceError{Message: "project not found: " + projectID}
	}
	root := strProp(nodes[0], "path")
	if root == "" {
		return "", &ServiceError{Message: "project has no path set"}
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// --- env helpers ---

func (r *VerificationRunner) buildEnv(extra map[string]string) []string {
	base := os.Environ()
	if len(extra) == 0 {
		return base
	}
	// Start from current env and apply overrides.
	envMap := map[string]string{}
	for _, e := range base {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	for k, v := range extra {
		envMap[k] = v
	}
	result := make([]string, 0, len(envMap))
	for k, v := range envMap {
		result = append(result, k+"="+v)
	}
	sort.Strings(result)
	return result
}

// --- output trimming ---

func truncateOutput(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "\n... [truncated]"
}

// --- finding parsers ---

// findingPattern matches common compiler / test diagnostic formats:
//   - Go:    file.go:12:3: error: msg
//   - Rust:  error[E0xxx]: msg\n  --> src/lib.rs:12:3
//   - GCC/Clang: file.c:12:3: error: msg
//   - pytest: FAILED test_foo.py::TestBar::test_baz
var goDiagRe = regexp.MustCompile(`(?m)^([^:\n]+\.(?:go|py|rs|c|cpp|cc|java|ts|js)):(\d+):(?:(\d+):)?\s*(error|warning|note|info):\s*(.+)$`)
var goTestRe = regexp.MustCompile(`(?m)^--- (FAIL|PASS|SKIP): (\S+) \((.+)\)$`)
var pytestRe = regexp.MustCompile(`(?m)^(FAILED|PASSED|ERROR) (.+)$`)
var rustDiagRe = regexp.MustCompile(`(?m)^(error|warning)(?:\[[\w:]+\])?: (.+)\n\s*-->\s*([^:]+):(\d+):(\d+)`)

func parseFindings(output, tool string) []VerificationFinding {
	var out []VerificationFinding
	seen := map[string]bool{}

	add := func(f VerificationFinding) {
		key := fmt.Sprintf("%s:%d:%s", f.File, f.Line, f.Message)
		if seen[key] {
			return
		}
		seen[key] = true
		f.Tool = tool
		out = append(out, f)
	}

	// Generic file:line:col: kind: message
	for _, m := range goDiagRe.FindAllStringSubmatch(output, 50) {
		line := 0
		col := 0
		fmt.Sscanf(m[2], "%d", &line)
		fmt.Sscanf(m[3], "%d", &col)
		add(VerificationFinding{File: m[1], Line: line, Col: col, Kind: m[4], Message: strings.TrimSpace(m[5])})
	}

	// Go test results
	if tool == "go" || tool == "gotestsum" {
		for _, m := range goTestRe.FindAllStringSubmatch(output, 50) {
			kind := "info"
			if m[1] == "FAIL" {
				kind = "error"
			}
			add(VerificationFinding{Kind: kind, Message: fmt.Sprintf("%s %s (%s)", m[1], m[2], m[3])})
		}
	}

	// pytest
	if tool == "pytest" || tool == "python" || tool == "python3" {
		for _, m := range pytestRe.FindAllStringSubmatch(output, 50) {
			kind := "error"
			if m[1] == "PASSED" {
				kind = "info"
			}
			add(VerificationFinding{Kind: kind, Message: fmt.Sprintf("%s %s", m[1], m[2])})
		}
	}

	// Rust
	if tool == "cargo" || tool == "rustc" {
		for _, m := range rustDiagRe.FindAllStringSubmatch(output, 50) {
			line := 0
			col := 0
			fmt.Sscanf(m[4], "%d", &line)
			fmt.Sscanf(m[5], "%d", &col)
			add(VerificationFinding{File: m[3], Line: line, Col: col, Kind: m[1], Message: m[2]})
		}
	}

	return out
}

// --- graph recording ---

func (r *VerificationRunner) record(projectID, agent string, res *VerificationResult) (string, error) {
	props := map[string]any{
		"project_id":    projectID,
		"command":       res.Command,
		"args":          strings.Join(res.Args, " "),
		"status":        string(res.Status),
		"exit_code":     res.ExitCode,
		"duration":      res.Duration,
		"finding_count": len(res.Findings),
		"agent":         agent,
	}
	if len(res.Output) > 512 {
		props["output_snippet"] = res.Output[:512]
	} else {
		props["output_snippet"] = res.Output
	}
	node, err := r.graph.UpsertNode("VerificationRun", map[string]any{
		"project_id": projectID,
		"command":    res.Command,
		"args":       strings.Join(res.Args, " "),
	}, props)
	if err != nil {
		return "", err
	}
	return node.ID, nil
}

// --- service helpers used by MCP handlers ---

// VerificationRunService wraps the runner and exposes project-resolved helpers.
type VerificationRunService struct {
	runner *VerificationRunner
}

func NewVerificationRunService(cfg VerificationConfig, g graph.GraphRepository) *VerificationRunService {
	return &VerificationRunService{runner: NewVerificationRunner(cfg, g)}
}

func (s *VerificationRunService) Run(req VerifyCommandRequest) (*VerificationResult, error) {
	return s.runner.Run(req)
}

// AllowedTools returns the list of allowed commands and their sub-verbs for
// display to agents.
func (s *VerificationRunService) AllowedTools() []map[string]any {
	var out []map[string]any
	for _, cmd := range s.runner.cfg.AllowedCommands {
		row := map[string]any{"command": cmd}
		if subs, ok := s.runner.cfg.AllowedSubcommands[cmd]; ok && len(subs) > 0 {
			row["allowed_subcommands"] = subs
		}
		out = append(out, row)
	}
	return out
}

// ListRuns returns recent VerificationRun nodes for the project.
func (s *VerificationRunService) ListRuns(projectID string, limit int) ([]map[string]any, error) {
	nodes, err := s.runner.graph.FindNodes("VerificationRun", map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, presentVerificationRun(n))
	}
	// sort newest first by ID (UUIDs are time-ordered)
	sort.Slice(out, func(i, j int) bool {
		return fmt.Sprint(out[i]["id"]) > fmt.Sprint(out[j]["id"])
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func presentVerificationRun(n *models.Node) map[string]any {
	row := map[string]any{"id": n.ID}
	for k, v := range n.Properties {
		row[k] = v
	}
	return row
}
