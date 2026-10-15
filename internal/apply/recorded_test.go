package apply

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/jira/jiratest"
)

// The recordings in testdata/ts and testdata/jsctest2 were made with
// jsc-record --dry-run against the sandbox and scrubbed with jsc-scrub: an
// export of the space (spec.yaml) and every response a dry run of it reads.

func recorded(t *testing.T, dir string) ([]jiratest.Route, []byte) {
	t.Helper()
	routes, err := jiratest.LoadRoutes(dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dir + "/spec.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return routes, data
}

func runRecorded(t *testing.T, dir, key string, routes []jiratest.Route, data []byte) (string, *jiratest.Server, error) {
	t.Helper()
	srv := jiratest.New(t, dir, routes)
	var out bytes.Buffer
	err := DryRun(context.Background(), srv.Client(), Options{SpecFile: "spec.yaml", Spec: data, SpaceKey: key}, &out)
	return strings.ReplaceAll(out.String(), srv.URL(), "https://example.atlassian.net"), srv, err
}

var spaces = []struct{ dir, key string }{{"testdata/ts", "TS"}, {"testdata/jsctest2", "JSCTEST2"}}

// TestOwnExportMatches is guarantee R1 for apply: a dry run of a space
// against its own export finds everything as it is.
func TestOwnExportMatches(t *testing.T) {
	for _, sp := range spaces {
		t.Run(sp.key, func(t *testing.T) {
			routes, data := recorded(t, sp.dir)
			got, srv, err := runRecorded(t, sp.dir, sp.key, routes, data)
			if err != nil {
				t.Errorf("dry run: %v", err)
			}
			for _, marker := range []string{"+ create", "~ update", "✗", "■ blocked", "? manual"} {
				if strings.Contains(got, marker) {
					t.Errorf("own export has %q items:\n%s", marker, got)
				}
			}
			jiratest.CheckDocumented(t, "../../docs/api/openapi", srv.Requests())
			if sp.key == "TS" {
				checkGolden(t, got, "ts-own-export.golden.txt")
			}
		})
	}
}

// TestEveryRecordedReadStopsTheRun fails, one at a time, each request a dry
// run of a recorded space makes.
func TestEveryRecordedReadStopsTheRun(t *testing.T) {
	for _, sp := range spaces {
		routes, data := recorded(t, sp.dir)
		_, srv, _ := runRecorded(t, sp.dir, sp.key, routes, data)
		unused := srv.Unused()
		for i, r := range routes {
			if slices.ContainsFunc(unused, func(u jiratest.Route) bool {
				return u.Method == r.Method && u.Path == r.Path && u.Query.Encode() == r.Query.Encode() && u.Body == r.Body
			}) {
				continue // read by the export only
			}
			t.Run(sp.key+" "+r.Method+" "+r.Path+" "+r.Query.Encode()+" "+r.Body, func(t *testing.T) {
				t.Parallel()
				broken := slices.Clone(routes)
				broken[i] = jiratest.Route{Method: r.Method, Path: r.Path, Query: r.Query, Body: r.Body, Status: http.StatusInternalServerError}
				got, _, err := runRecorded(t, sp.dir, sp.key, broken, data)
				var stopped *Stopped
				if !errors.As(err, &stopped) || !strings.Contains(got, "Jira answered   500") || !strings.Contains(got, "Nothing was changed.") {
					t.Errorf("err = %v; output:\n%s", err, got)
				}
			})
		}
	}
}
