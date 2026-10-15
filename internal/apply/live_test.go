package apply

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/export"
	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/jira/jiratest"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

const createInTriage = `{"scope":{"type":"GLOBAL"},"statuses":[{"description":"[managed-by: jira-space-config]","name":"In Triage","statusCategory":"IN_PROGRESS"}]}`

// liveRun runs a live apply against the synthetic site with scripted
// answers, and returns the output, the error and the server.
func liveRun(t *testing.T, routes []jiratest.Route, specFile, answers string) (string, *jiratest.Server, string, error) {
	t.Helper()
	srv := jiratest.New(t, "testdata/site", routes)
	data, err := os.ReadFile(specFile)
	if err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	var out bytes.Buffer
	err = Live(context.Background(), LiveOptions{
		Options: Options{SpecFile: specFile, Spec: data, SpaceKey: "VULN"},
		Connect: srv.Options(), In: strings.NewReader(answers),
		StateDir: state, Output: filepath.Join(state, "applied.yaml"), ToolVersion: "test",
	}, &out)
	u, _ := url.Parse(srv.URL())
	got := strings.ReplaceAll(out.String(), srv.URL(), "https://example.atlassian.net")
	return strings.ReplaceAll(got, u.Host, "example.atlassian.net"), srv, state, err
}

// The synthetic site cannot answer a whole export: the resulting spec is a
// stand-in.
func TestMain(m *testing.M) {
	exportSpace = func(context.Context, *jira.Client, string, export.Options) (*spec.Spec, error) {
		return &spec.Spec{APIVersion: spec.APIVersion, Kind: spec.Kind, Metadata: spec.Metadata{Name: "stand-in"}, Space: &spec.Space{Key: "VULN"}}, nil
	}
	os.Exit(m.Run())
}

func withCreate(status int) []jiratest.Route {
	r := jiratest.Route{Method: http.MethodPost, Path: "/rest/api/3/statuses", Body: createInTriage, File: "created_status.json"}
	if status != 0 {
		r = jiratest.Route{Method: http.MethodPost, Path: "/rest/api/3/statuses", Body: createInTriage, Status: status}
	}
	return append(siteRoutes(), r)
}

func posted(srv *jiratest.Server) int {
	n := 0
	for _, r := range srv.Requests() {
		if strings.HasPrefix(r, "POST /rest/api/3/statuses") {
			n++
		}
	}
	return n
}

func TestLiveCreatesAStatus(t *testing.T) {
	got, srv, state, err := liveRun(t, withCreate(0), "testdata/clean.yaml", "p\nVULN\n")
	if err != nil {
		t.Fatalf("live: %v\n%s", err, got)
	}
	for _, w := range []string{"LIVE · changes will be made, one confirmed step at a time",
		"Step 4 will create 1 status, site-wide: every space on the site shares them.",
		"VULN on example.atlassian.net · [p] proceed   [r] re-evaluate   [a] abort", "Type the space key, VULN, to go on.",
		"+ In Triage  created (id 10500)", "Step 4 done · 1 change", `Created        1 · status "In Triage"`} {
		if !strings.Contains(got, w) {
			t.Errorf("output lacks %q:\n%s", w, got)
		}
	}
	if n := posted(srv); n != 1 {
		t.Errorf("%d status creations sent, want 1", n)
	}
	logs, _ := filepath.Glob(filepath.Join(state, "runs", "*-VULN.jsonl"))
	if len(logs) != 1 {
		t.Fatalf("audit logs: %v", logs)
	}
	log, _ := os.ReadFile(logs[0])
	for _, w := range []string{`"event":"intent"`, `"event":"send","method":"POST","path":"/rest/api/3/statuses"`, `"event":"done"`, `"id":"10500"`} {
		if !strings.Contains(string(log), w) {
			t.Errorf("audit log lacks %s:\n%s", w, log)
		}
	}
	if strings.Contains(string(log), "test-token") {
		t.Error("the audit log contains the token")
	}
	if locks, _ := filepath.Glob(filepath.Join(state, "locks", "*")); len(locks) != 0 {
		t.Errorf("lock not released: %v", locks)
	}
}

func TestLiveChangesNothingWithoutConsent(t *testing.T) {
	for name, answers := range map[string]string{"abort": "a\n", "wrong space key": "p\nOTHER\n", "end of input": ""} {
		t.Run(name, func(t *testing.T) {
			got, srv, _, err := liveRun(t, withCreate(0), "testdata/clean.yaml", answers)
			if err == nil || !strings.Contains(got, "Aborted at step 4") || !strings.Contains(got, "Nothing was changed.") {
				t.Errorf("err = %v\n%s", err, got)
			}
			if n := posted(srv); n != 0 {
				t.Errorf("%d writes sent", n)
			}
		})
	}
}

func TestLiveStopsWhenAWriteFails(t *testing.T) {
	got, _, _, err := liveRun(t, withCreate(http.StatusBadRequest), "testdata/clean.yaml", "p\nVULN\n")
	for _, w := range []string{`✗ Could not create status "In Triage"`, "Jira answered   400 Bad Request", "Nothing more was sent in this step", "Stopped at step 4 of 28."} {
		if !strings.Contains(got, w) {
			t.Errorf("output lacks %q:\n%s", w, got)
		}
	}
	if err == nil {
		t.Error("a failed write must fail the run")
	}
}

func TestLiveRefusesWhileAnotherRunHoldsTheLock(t *testing.T) {
	srv := jiratest.New(t, "testdata/site", withCreate(0))
	data, _ := os.ReadFile("testdata/clean.yaml")
	state := t.TempDir()
	u, _ := url.Parse(srv.URL())
	if err := os.MkdirAll(filepath.Join(state, "locks"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(state, "locks", u.Host+"-VULN.lock"), "process 1, since then\n")
	var out bytes.Buffer
	err := Live(context.Background(), LiveOptions{Options: Options{SpecFile: "clean.yaml", Spec: data, SpaceKey: "VULN"},
		Connect: srv.Options(), In: strings.NewReader("p\nVULN\n"), StateDir: state, Output: filepath.Join(state, "a.yaml")}, &out)
	if err == nil || !strings.Contains(out.String(), "Another live run is applying to VULN") {
		t.Errorf("err = %v\n%s", err, out.String())
	}
}

// TestLiveStopsUsingANotificationScheme: a spec with notifications: null takes
// the space's scheme away, as Jira does it: the scheme ID -1.
func TestLiveStopsUsingANotificationScheme(t *testing.T) {
	got, srv, _, err := liveRun(t, append(siteRoutes(),
		jiratest.Route{Path: "/rest/api/3/project/VULN/notificationscheme", File: "notificationscheme.json"},
		jiratest.Route{Method: http.MethodPut, Path: "/rest/api/3/project/VULN", Body: `{"notificationScheme":-1}`, File: "project.json"}),
		"testdata/no-notifications.yaml", "p\nVULN\np\n")
	if err != nil || !strings.Contains(got, "Step 25 done") || !slices.Contains(srv.Requests(), "PUT /rest/api/3/project/VULN") {
		t.Errorf("err = %v, requests %v\n%s", err, srv.Requests(), got)
	}
}

func TestLiveAsksForTheCountOfManySiteWideCreations(t *testing.T) {
	routes := siteRoutes()
	for _, n := range []string{"S1", "S2", "S3", "S4", "S5", "S6"} {
		routes = append(routes, jiratest.Route{Path: "/rest/api/3/statuses/search", Query: url.Values{"searchString": {n}}, File: "statuses_Blocked.json"})
	}
	got, srv, _, _ := liveRun(t, routes, "testdata/many.yaml", "p\nVULN\n5\n")
	if !strings.Contains(got, "This step creates 6 site-wide objects, more than a space usually needs. Type 6 to create them.") {
		t.Errorf("no count asked:\n%s", got)
	}
	if n := posted(srv); n != 0 {
		t.Errorf("%d writes sent after a wrong count", n)
	}
}

func TestStateDir(t *testing.T) {
	if d, _ := StateDir(func(k string) string { return map[string]string{"XDG_STATE_HOME": "/s"}[k] }); d != "/s/jira-space-config" {
		t.Errorf("with XDG_STATE_HOME: %s", d)
	}
	t.Setenv("HOME", "")
	if _, err := StateDir(func(string) string { return "" }); err == nil || !strings.Contains(err.Error(), "use --log-dir") {
		t.Errorf("without a home directory: %v", err)
	}
}

func TestLiveStopsWhenJiraDoesNotSayWhatItCreated(t *testing.T) {
	writeFile(t, "testdata/site/created_nothing.json", "[]\n")
	t.Cleanup(func() { _ = os.Remove("testdata/site/created_nothing.json") })
	routes := append(siteRoutes(), jiratest.Route{Method: http.MethodPost, Path: "/rest/api/3/statuses", Body: createInTriage, File: "created_nothing.json"})
	got, _, _, err := liveRun(t, routes, "testdata/clean.yaml", "p\nVULN\n")
	if err == nil || !strings.Contains(got, "the answer does not say which status was created") {
		t.Errorf("err = %v\n%s", err, got)
	}
}

// TestLiveReportsAPartlyMadeItem: a screen is made in several writes; when a
// later one fails, the summary says the screen was made in part, not that
// nothing changed.
func TestLiveReportsAPartlyMadeItem(t *testing.T) {
	routes := append(siteRoutes(),
		jiratest.Route{Method: http.MethodPost, Path: "/rest/api/3/screens", Body: `{"description":"[managed-by: jira-space-config]","name":"VULN Triage Screen"}`, File: "screen_created.json"},
		jiratest.Route{Path: "/rest/api/3/screens/10500/tabs", File: "screen_10500_new_tabs.json"},
		jiratest.Route{Method: http.MethodPut, Path: "/rest/api/3/screens/10500/tabs/1", Body: `{"name":"Triage"}`, Status: http.StatusInternalServerError})
	got, _, _, err := liveRun(t, routes, "testdata/new-screen.yaml", "p\nVULN\n")
	if err == nil {
		t.Fatal("a failed write must fail the run")
	}
	for _, w := range []string{"Partly changed 1 ·", `screen "VULN Triage Screen" (1 write made)`, "Running again is safe"} {
		if !strings.Contains(got, w) {
			t.Errorf("output lacks %q:\n%s", w, got)
		}
	}
	if strings.Contains(got, "Nothing was changed.") {
		t.Errorf("says nothing changed after a write went through:\n%s", got)
	}
}
