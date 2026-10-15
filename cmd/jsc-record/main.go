// Command jsc-record is a development tool: it runs export against a real
// space and saves every response export receives, with a manifest of the
// requests (routes.json), for use as test fixtures. It uses the same
// read-only client and guard as the tool itself, so it cannot write to Jira.
//
// The recordings contain the site's real data. Scrub them before they go
// into the repository (account IDs, site name, cloud ID); see
// docs/api/README.md.
//
//	go run ./cmd/jsc-record --site https://example.atlassian.net --space KEY --out DIR [--dry-run]
//
// With --dry-run it then runs apply --dry-run with the export, so the
// recording also holds every response the dry run reads (and spec.yaml).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jerryjvl/jira-space-config/internal/apply"
	"github.com/jerryjvl/jira-space-config/internal/credentials"
	"github.com/jerryjvl/jira-space-config/internal/export"
	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/jira/jiratest"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run() error {
	site := flag.String("site", "", "Jira Cloud site (required)")
	space := flag.String("space", "", "space key (required)")
	out := flag.String("out", "", "empty directory for the recording (required)")
	dryRun := flag.Bool("dry-run", false, "after the export, record a dry run of apply with it (the export is saved as spec.yaml)")
	flag.Parse()
	if *site == "" || *space == "" || *out == "" {
		return errors.New("--site, --space and --out are required")
	}
	if entries, err := os.ReadDir(*out); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s is not empty; record into a new directory", *out)
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		return err
	}
	parsed, err := jira.ParseSite(*site)
	if err != nil {
		return err
	}
	creds, err := credentials.Load(os.Getenv)
	if err != nil {
		return err
	}
	rec := &jiratest.Recorder{Next: http.DefaultTransport, Dir: *out}
	ctx := context.Background()
	c, err := jira.New(ctx, jira.Options{Site: parsed, Credentials: creds, UserAgent: "jsc-record", Transport: rec})
	if err != nil {
		return err
	}
	s, exportErr := export.Space(ctx, c, *space, export.Options{ToolVersion: "record"})
	if exportErr == nil && *dryRun {
		data, err := spec.Write(s)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(*out, "spec.yaml"), data, 0o600); err != nil {
			return err
		}
		// The plan goes to standard error; it is not part of the recording.
		exportErr = apply.DryRun(ctx, c, apply.Options{SpecFile: "spec.yaml", Spec: data, SpaceKey: *space}, os.Stderr)
	}
	if err := rec.WriteManifest(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Recorded %d requests into %s (all read-only).\n", c.Requests(), *out)
	return exportErr
}
