package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"codergag/internal/config"
	"codergag/internal/ctxstore"
	"codergag/internal/services"
)

var activeCfg = config.Default()

func stateDir() string {
	if d := os.Getenv("CODERAG_HOME"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".codergag")
}

func profilePath() string { return filepath.Join(stateDir(), "profile.json") }

func resolveProject(app *services.Application, flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if cwd, err := os.Getwd(); err == nil {
		if id, err := ctxstore.ProjectForDir(app, cwd); err == nil && id != "" {
			return id, nil
		}
	}
	id, err := firstProject(app)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.New("no project indexed; use -project ID")
	}
	return id, nil
}

func runCtx(app *services.Application, args []string) int {
	defer app.Graph.Close()
	return ctxRun(app, args, os.Stdin, os.Stdout, os.Stderr)
}

const ctxUsage = "usage: codergag ctx <add|search|show|export|status|profile|reset> [flags]\n"

func ctxRun(app *services.Application, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, ctxUsage)
		return 2
	}
	sub, rest := args[0], args[1:]
	store := ctxstore.New(app)
	fail := func(err error) int { fmt.Fprintln(stderr, "ctx:", err); return 1 }
	newFS := func() (*flag.FlagSet, *string) {
		fs := flag.NewFlagSet("ctx "+sub, flag.ContinueOnError)
		fs.SetOutput(stderr)
		return fs, fs.String("project", "", "project id (default: detected)")
	}

	switch sub {
	case "add":
		fs, project := newFS()
		kind := fs.String("kind", "note", "decision|convention|task|note")
		title := fs.String("title", "", "record title (required)")
		pin := fs.Bool("pin", false, "always include in the session digest")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		body := strings.Join(fs.Args(), " ")
		if body == "" {
			if b, err := io.ReadAll(stdin); err == nil {
				body = strings.TrimSpace(string(b))
			}
		}
		pid, err := resolveProject(app, *project)
		if err != nil {
			return fail(err)
		}
		rec, err := store.Add(pid, ctxstore.Record{Kind: *kind, Title: *title, Body: body, Pinned: *pin})
		if err != nil {
			return fail(err)
		}
		printJSON(stdout, rec)
		return 0

	case "search":
		fs, project := newFS()
		asJSON := fs.Bool("json", false, "machine-readable output")
		limit := fs.Int("limit", 10, "max results")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		pid, err := resolveProject(app, *project)
		if err != nil {
			return fail(err)
		}
		recs, err := store.Search(pid, strings.Join(fs.Args(), " "), *limit)
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			printJSON(stdout, map[string]any{"records": recs, "count": len(recs)})
			return 0
		}
		for _, r := range recs {
			fmt.Fprintf(stdout, "%s  [%s] %s\n", r.ID, r.Kind, r.Title)
		}
		return 0

	case "show":
		if len(rest) != 1 {
			fmt.Fprintln(stderr, "usage: codergag ctx show <id>")
			return 2
		}
		r, err := store.Get(rest[0])
		if err != nil {
			return fail(fmt.Errorf("record %q: %w", rest[0], err))
		}
		fmt.Fprintf(stdout, "id:      %s\nkind:    %s\ntitle:   %s\nsource:  %s\npinned:  %v\ncreated: %s\n\n%s\n",
			r.ID, r.Kind, r.Title, r.Source, r.Pinned, r.CreatedAt, r.Body)
		return 0

	case "export":
		fs, project := newFS()
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		pid, err := resolveProject(app, *project)
		if err != nil {
			return fail(err)
		}
		recs, err := store.List(pid, "")
		if err != nil {
			return fail(err)
		}
		byKind := map[string][]ctxstore.Record{}
		for _, r := range recs {
			byKind[r.Kind] = append(byKind[r.Kind], r)
		}
		kinds := make([]string, 0, len(byKind))
		for k := range byKind {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		fmt.Fprintf(stdout, "# Context: %s\n", pid)
		for _, k := range kinds {
			fmt.Fprintf(stdout, "\n## %s\n", k)
			for _, r := range byKind[k] {
				fmt.Fprintf(stdout, "\n### %s\n\n%s\n", r.Title, r.Body)
			}
		}
		return 0

	case "status":
		fs, project := newFS()
		asJSON := fs.Bool("json", false, "machine-readable output")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		pid, err := resolveProject(app, *project)
		if err != nil {
			return fail(err)
		}
		recs, err := store.List(pid, "")
		if err != nil {
			return fail(err)
		}
		counts := map[string]int{}
		for _, r := range recs {
			counts[r.Kind]++
		}
		prof, _ := ctxstore.LoadProfile(profilePath())
		digest, err := store.SessionDigest(pid, activeCfg.Context.Session(), prof, "")
		if err != nil {
			return fail(err)
		}
		tokens := ctxstore.EstimateTokens(digest)
		if *asJSON {
			printJSON(stdout, map[string]any{"project": pid, "total": len(recs), "by_kind": counts,
				"digest_tokens": tokens, "session_budget": activeCfg.Context.Session()})
			return 0
		}
		fmt.Fprintf(stdout, "project: %s\nrecords: %d %v\nsession digest: ~%d / %d tokens\n",
			pid, len(recs), counts, tokens, activeCfg.Context.Session())
		return 0

	case "profile":
		p, err := ctxstore.LoadProfile(profilePath())
		if err != nil {
			return fail(err)
		}
		switch {
		case len(rest) == 0:
			keys := make([]string, 0, len(p))
			for k := range p {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(stdout, "%s: %s\n", k, p[k])
			}
		case rest[0] == "set" && len(rest) >= 3:
			p[rest[1]] = strings.Join(rest[2:], " ")
			if err := p.Save(profilePath()); err != nil {
				return fail(err)
			}
		case rest[0] == "clear":
			if err := (ctxstore.Profile{}).Save(profilePath()); err != nil {
				return fail(err)
			}
		default:
			fmt.Fprintln(stderr, "usage: codergag ctx profile [set <key> <value>|clear]")
			return 2
		}
		return 0

	case "reset":
		fs, project := newFS()
		yes := fs.Bool("yes", false, "skip the confirmation prompt")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		pid, err := resolveProject(app, *project)
		if err != nil {
			return fail(err)
		}
		if !*yes {
			fmt.Fprintf(stderr, "delete ALL memory records of project %s? [y/N] ", pid)
			line, _ := bufio.NewReader(stdin).ReadString('\n')
			if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
				fmt.Fprintln(stderr, "aborted")
				return 1
			}
		}
		n, err := store.Reset(pid)
		if err != nil {
			return fail(err)
		}
		printJSON(stdout, map[string]any{"project": pid, "removed": n})
		return 0
	}
	fmt.Fprintf(stderr, "ctx: unknown subcommand %q\n%s", sub, ctxUsage)
	return 2
}
