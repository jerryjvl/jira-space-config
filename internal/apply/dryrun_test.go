package apply

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/jira/jiratest"
	"github.com/jerryjvl/jira-space-config/internal/steps"
)

var update = flag.Bool("update", false, "rewrite golden files")

// siteRoutes serve testdata/site: a site shaped from the OpenAPI response
// schemas, with the objects the test specs look for.
func siteRoutes() []jiratest.Route {
	routes := []jiratest.Route{
		{Path: "/_edge/tenant_info", File: "tenant_info.json"},
		{Path: "/rest/api/3/myself", File: "myself.json"},
		{Path: "/rest/api/3/serverInfo", File: "serverInfo.json"},
		{Path: "/rest/api/3/instance/license", File: "license.json"},
		{Path: "/rest/api/3/mypermissions", File: "mypermissions.json"},
		{Path: "/rest/api/3/project/VULN", File: "project.json"},
		{Method: http.MethodPost, Path: "/rest/api/3/search/approximate-count", Body: `{"jql":"project = 10042"}`, File: "count.json"},
		{Path: "/rest/api/3/role", File: "role.json"},
		{Path: "/rest/api/3/group/bulk", File: "group_bulk.json"},
		{Path: "/rest/api/3/resolution/search", File: "resolution_search.json"},
		{Path: "/rest/api/3/priority/search", File: "priority_search.json"},
		{Path: "/rest/api/3/issueLinkType", File: "issueLinkType.json"},
		{Path: "/rest/api/3/issuetype", File: "issuetype.json"},
		{Path: "/rest/api/3/field", File: "field.json"},
		{Path: "/rest/api/3/field/search", File: "field_search.json"},
		{Path: "/rest/api/3/statuses", Query: url.Values{"id": {"1"}}, File: "statuses_by_id.json"},
	}
	for _, name := range []string{"Open", "In Triage", "Closed", "Blocked", "in review", "Done", "Nowhere"} {
		routes = append(routes, jiratest.Route{Path: "/rest/api/3/statuses/search",
			Query: url.Values{"searchString": {name}}, File: "statuses_" + name + ".json"})
	}
	for _, name := range []string{"VULN Triage Screen", "Default Screen", "VULN Old Screen", "VULN Bad Screen"} {
		routes = append(routes, jiratest.Route{Path: "/rest/api/3/screens", Query: url.Values{"queryString": {name}}, File: "screens_" + name + ".json"})
	}
	for _, name := range []string{"VULN Workflow", "VULN New Workflow", "VULN Broken"} {
		routes = append(routes, jiratest.Route{Path: "/rest/api/3/workflows/search", Query: url.Values{"queryString": {name}}, File: "workflows_" + name + ".json"})
	}
	routes = append(routes,
		jiratest.Route{Path: "/rest/api/3/project/VULN/permissionscheme", Query: url.Values{"expand": {"all"}}, File: "permissionscheme.json"},
		jiratest.Route{Path: "/rest/api/3/permissionscheme", File: "permissionschemes.json"},
		jiratest.Route{Path: "/rest/api/3/screens", Query: url.Values{"id": {"1", "10100"}}, File: "screens_by_id.json"},
		jiratest.Route{Path: "/rest/api/3/screens/1/tabs", File: "screen_1_tabs.json"},
		jiratest.Route{Path: "/rest/api/3/screens/1/tabs/10000/fields", File: "screen_1_tab_10000_fields.json"},
		jiratest.Route{Path: "/rest/api/3/screens/10100/tabs", File: "screen_10100_tabs.json"},
		jiratest.Route{Path: "/rest/api/3/screens/10100/tabs/10101/fields", File: "screen_10100_tab_10101_fields.json"},
		jiratest.Route{Method: http.MethodPost, Path: "/rest/api/3/workflows", Body: `{"workflowNames":["VULN Workflow"]}`, File: "workflows_read.json"},
		jiratest.Route{Method: http.MethodPost, Path: "/rest/api/3/workflows", Body: `{"workflowIds":["wf-1"]}`, File: "workflows_read.json"},
		jiratest.Route{Path: "/rest/api/3/workflow/wf-1/projectUsages", File: "workflow_wf-1_usages.json"},
	)
	return routes
}

func replace(routes []jiratest.Route, path string, r jiratest.Route) []jiratest.Route {
	for i := range routes {
		if routes[i].Path == path {
			r.Path = path
			routes[i] = r
		}
	}
	return routes
}

func runDry(t *testing.T, routes []jiratest.Route, specFile string, opts Options) (string, error) {
	t.Helper()
	srv := jiratest.New(t, "testdata/site", routes)
	data, err := os.ReadFile(specFile)
	if err != nil {
		t.Fatal(err)
	}
	opts.SpecFile, opts.Spec = specFile, data
	if opts.SpaceKey == "" {
		opts.SpaceKey = "VULN"
	}
	var out bytes.Buffer
	err = DryRun(context.Background(), srv.Client(), opts, &out)
	jiratest.CheckDocumented(t, "../../docs/api/openapi", srv.Requests())
	return strings.ReplaceAll(out.String(), srv.URL(), "https://example.atlassian.net"), err
}

func checkGolden(t *testing.T, got, golden string) {
	t.Helper()
	path := "testdata/" + golden
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s (go test ./internal/apply -update to accept):\n%s", path, got)
	}
}

// TestDryRun covers every outcome of the site-wide steps in one plan.
func TestDryRun(t *testing.T) {
	got, err := runDry(t, siteRoutes(), "testdata/vuln.yaml", Options{ShowRequests: true})
	var stopped *Stopped
	if !errors.As(err, &stopped) || stopped.Summary != "14 items to fix before the spec can be applied" {
		t.Errorf("err = %v", err)
	}
	checkGolden(t, got, "dryrun.golden.txt")
}

func TestDryRunOutcomes(t *testing.T) {
	failing := func(path string, status int) []jiratest.Route {
		return replace(siteRoutes(), path, jiratest.Route{Status: status})
	}
	tests := []struct {
		name    string
		routes  []jiratest.Route
		spec    string
		opts    Options
		wantErr string   // "" for success
		want    []string // in the output
		absent  []string // not in the output
	}{
		{"clean first apply", siteRoutes(), "testdata/clean.yaml", Options{}, "",
			[]string{"First apply: the spec has no binding", "+ create   In Triage", "Needs fixing   0", "Nothing was changed."},
			[]string{"POST /rest/api/3/statuses"}},
		{"spec errors stop before any request", nil, "testdata/invalid.yaml", Options{}, "testdata/invalid.yaml has 1 error",
			[]string{"✗ The spec has 1 error", "testdata/invalid.yaml:5:", "must be one of reuse, ensure", "Stopped at step 1 of 28. Nothing was changed."},
			[]string{"Site "}},
		{"another space", siteRoutes(), "testdata/clean.yaml", Options{SpaceKey: "OTHER"}, "the spec is for another space",
			[]string{"✗ The spec is for space VULN, not OTHER"}, nil},
		{"another site", siteRoutes(), "testdata/other-site.yaml", Options{}, "the spec is bound to another site",
			[]string{"✗ The spec was written for another site", "cloud ID cloud-2", "run with --ignore-ids"}, nil},
		{"another site with --ignore-ids", siteRoutes(), "testdata/other-site.yaml", Options{IgnoreIDs: true}, "",
			[]string{"Overrides  --ignore-ids", "✓ exists   Open  matches (id 1)"}, nil},
		{"team-managed space", replace(siteRoutes(), "/rest/api/3/project/VULN", jiratest.Route{File: "project_team.json"}),
			"testdata/clean.yaml", Options{}, "the space is team-managed",
			[]string{"✗ Space VULN is team-managed", "Jira cannot convert a space"}, nil},
		{"credentials refused", failing("/_edge/tenant_info", http.StatusUnauthorized), "testdata/clean.yaml", Options{}, "preflight failed",
			[]string{"✗ Could not check the account and the space", "Jira answered   401 Unauthorized", "JIRA_API_TOKEN"}, nil}, //nolint:misspell // HTTP's status text
		{"space not found", failing("/rest/api/3/project/VULN", http.StatusNotFound), "testdata/clean.yaml", Options{}, "preflight failed",
			[]string{`While           space "VULN" was not found, or this account cannot see it`}, nil},
		{"work items cannot be counted", replace(siteRoutes(), "/rest/api/3/search/approximate-count",
			jiratest.Route{Method: http.MethodPost, Body: `{"jql":"project = 10042"}`, Status: http.StatusBadRequest}),
			"testdata/clean.yaml", Options{}, "preflight failed",
			[]string{"✗ Could not count the space's work items", "While           counting the space's work items"}, nil},
		{"a read fails during a step", failing("/rest/api/3/statuses/search", http.StatusInternalServerError), "testdata/clean.yaml", Options{},
			"stopped at step 4",
			[]string{"✗ Could not evaluate step 4 of 28 · Statuses", "Jira answered   500 Internal Server Error",
				`While           looking up statuses named "Open"`, "Stopped at step 4 of 28. Nothing was changed."},
			[]string{"Summary"}},
		{"Administer Jira without space permissions", replace(siteRoutes(), "/rest/api/3/mypermissions", jiratest.Route{File: "mypermissions_admin_only.json"}),
			"testdata/clean.yaml", Options{}, "",
			[]string{"(through Administer Jira)", "12 work items that this account can", "(it cannot browse the space, so there may be more)"}, nil},
		{"no Administer Jira", replace(siteRoutes(), "/rest/api/3/mypermissions", jiratest.Route{File: "mypermissions_no_admin.json"}),
			"testdata/clean.yaml", Options{}, "1 item to fix before the spec can be applied",
			[]string{"Administer Jira ✗", "✗ error    In Triage  missing, and this account cannot create it",
				"Creating a status needs the Administer Jira"}, nil},
		{"a managed workflow change that removes, allowed", siteRoutes(), "testdata/vuln.yaml", Options{ShowRequests: true, AllowDelete: []string{"workflows"}},
			"13 items to fix before the spec can be applied",
			[]string{"Overrides  --allow-delete=workflows", "~ update   VULN Workflow", "used by this space alone; it is changed to match",
				"(removing):", "POST /rest/api/3/workflows/update/validation (read-only check)"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := runDry(t, tc.routes, tc.spec, tc.opts)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("err = %v", err)
			case tc.wantErr != "" && (err == nil || err.Error() != tc.wantErr):
				t.Errorf("err = %v, want %q", err, tc.wantErr)
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("output lacks %q:\n%s", w, got)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(got, a) {
					t.Errorf("output has %q:\n%s", a, got)
				}
			}
		})
	}
}

// TestProblemLayout fixes the shape every problem has.
func TestProblemLayout(t *testing.T) {
	got, _ := runDry(t, replace(siteRoutes(), "/rest/api/3/statuses/search", jiratest.Route{Status: http.StatusForbidden}),
		"testdata/clean.yaml", Options{})
	checkGolden(t, got[strings.Index(got, "✗"):], "problem.golden.txt")
}

// TestEveryFailedReadStopsTheRun fails each request of a dry run in turn:
// the run must stop, say it changed nothing, and name the request.
func TestEveryFailedReadStopsTheRun(t *testing.T) {
	for i, r := range siteRoutes() {
		t.Run(r.Path+" "+r.Query.Encode(), func(t *testing.T) {
			routes := siteRoutes()
			routes[i] = jiratest.Route{Method: r.Method, Path: r.Path, Query: r.Query, Body: r.Body, Status: http.StatusInternalServerError}
			got, err := runDry(t, routes, "testdata/vuln.yaml", Options{ShowRequests: true})
			var stopped *Stopped
			if !errors.As(err, &stopped) {
				t.Fatalf("err = %v, want the run stopped", err)
			}
			method := r.Method
			if method == "" {
				method = http.MethodGet
			}
			for _, want := range []string{"Request         " + method + " " + r.Path, "Jira answered   500", "Nothing was changed."} {
				if !strings.Contains(got, want) {
					t.Errorf("output lacks %q:\n%s", want, got)
				}
			}
		})
	}
}

// TestDryRunRequestsAreDocumented: every request a dry run makes, with its
// query parameters, is in the OpenAPI specs.
func TestDryRunRequestsAreDocumented(t *testing.T) {
	srv := jiratest.New(t, "testdata/site", siteRoutes())
	data, err := os.ReadFile("testdata/vuln.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	_ = DryRun(context.Background(), srv.Client(), Options{SpecFile: "vuln.yaml", Spec: data, SpaceKey: "VULN"}, &out)
	for _, r := range srv.Unused() {
		t.Errorf("fixture never requested: %s %s %v", r.Method, r.Path, r.Query)
	}
	jiratest.CheckDocumented(t, "../../docs/api/openapi", srv.Requests())
}

// TestColourOnlyReinforces: with colour, the marker text is unchanged and
// keeps its width; colour is never needed to read it.
func TestColourOnlyReinforces(t *testing.T) {
	plain, coloured := (&printer{}).marker(steps.Differs), (&printer{colour: true}).marker(steps.Differs)
	if coloured != "\x1b[31m✗ differs\x1b[0m  " || plain != "✗ differs  " {
		t.Errorf("plain %q, coloured %q", plain, coloured)
	}
}

// TestLongNames: a name too long for the name column gets a line of its
// own, and its summary starts under the column.
func TestLongNames(t *testing.T) {
	var out bytes.Buffer
	p := &printer{w: &out}
	p.step(&steps.Step{Number: 12, Title: "Workflows", Items: []steps.Item{
		{Kind: steps.Exists, Name: "Short", Summary: "matches (id 1)"},
		{Kind: steps.Exists, Name: "Software Simplified Workflow for Project TS", Summary: "matches (id 2)"},
	}}, false)
	want := "  ✓ exists   Short  matches (id 1)\n" +
		"  ✓ exists   Software Simplified Workflow for Project TS\n" +
		"                    matches (id 2)\n"
	if !strings.Contains(out.String(), want) {
		t.Errorf("got:\n%s", out.String())
	}
}

// managedScreenRoutes serve VULN Old Screen as a managed screen, with the
// reads that tell which spaces use it.
func managedScreenRoutes(itss, workflows string) []jiratest.Route {
	r := replace(siteRoutes(), "/rest/api/3/screens", jiratest.Route{Query: url.Values{"id": {"1", "10100"}}, File: "screens_by_id_managed.json"})
	r = append([]jiratest.Route{
		{Path: "/rest/api/3/screens", Query: url.Values{"queryString": {"VULN Old Screen"}}, File: "screens_VULN Old Screen.json"},
		{Path: "/rest/api/3/screens", Query: url.Values{"id": {"10100"}}, File: "screens_by_id_managed.json"},
	}, r...)
	return append(r,
		jiratest.Route{Path: "/rest/api/3/screenscheme", File: "screenscheme_all.json"},
		jiratest.Route{Path: "/rest/api/3/issuetypescreenscheme/mapping", File: "itss_mapping_all.json"},
		jiratest.Route{Path: "/rest/api/3/issuetypescreenscheme/10300/project", File: itss},
		jiratest.Route{Path: "/rest/api/3/workflows/search", Query: url.Values{"isActive": {"true"}}, File: workflows},
		jiratest.Route{Path: "/rest/api/3/workflow/wf-9/projectUsages", File: "workflow_wf-9_usages.json"})
}

// TestManagedScreenChanges: a managed screen that differs is changed when
// this space alone uses it, and never when another space does.
func TestManagedScreenChanges(t *testing.T) {
	routes := managedScreenRoutes
	for name, tc := range map[string]struct {
		routes []jiratest.Route
		want   []string
	}{
		"used by this space alone": {routes("itss_project_10300.json", "empty_page.json"), []string{
			"~ update   VULN Old Screen", "used by this space alone; it is changed to match", "(additive)", "Would change   1",
			"spec: Summary, Description", "POST /rest/api/3/screens/{screenId}/tabs/{tabId}/fields\n"}},
		"used by another space's screen scheme": {routes("itss_project_10300_shared.json", "empty_page.json"), []string{
			"✗ error    VULN Old Screen", "and another space uses", "(id 10077)", "is never changed"}},
		"used by another space's workflow": {routes("itss_project_10300.json", "workflows_active.json"), []string{
			"another space uses", "(id 10088)"}},
	} {
		t.Run(name, func(t *testing.T) {
			got, _ := runDry(t, tc.routes, "testdata/managed-screen.yaml", Options{ShowRequests: true})
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("output lacks %q:\n%s", w, got)
				}
			}
		})
	}
}
