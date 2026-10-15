package export

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jerryjvl/jira-space-config/internal/jira/jiratest"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

var update = flag.Bool("update", false, "rewrite golden files")

// baseRoutes serve the responses recorded from a fresh company-managed space
// created from Jira's Kanban template (key TS, id 10002), scrubbed.
var baseRoutes = []jiratest.Route{
	{Method: http.MethodPost, Path: "/gateway/api/automation/public/jira/00000000-0000-0000-0000-000000000000/rest/v1/rule/summary",
		Body: `{"limit":100,"scope":"ari:cloud:jira:00000000-0000-0000-0000-000000000000:project/10002"}`, File: "automation_rules.json"},
	{Path: "/rest/agile/1.0/board", Query: url.Values{"projectKeyOrId": {"TS"}}, File: "boards.json"},
	{Path: "/rest/agile/1.0/board/3/configuration", File: "board_3_configuration.json"},
	{Path: "/rest/api/3/filter/10002", File: "filter_10002.json"},
	{Path: "/rest/api/3/filter/search", Query: url.Values{"projectId": {"10002"}}, File: "filter_search.json"},
	{Path: "/_edge/tenant_info", File: "tenant_info.json"},
	{Path: "/rest/api/3/project/TS", File: "project.json"},
	{Path: "/rest/api/3/issuetypescheme/project", Query: url.Values{"projectId": {"10002"}}, File: "issuetypescheme_project.json"},
	{Path: "/rest/api/3/issuetypescheme/mapping", Query: url.Values{"issueTypeSchemeId": {"10146"}}, File: "issuetypescheme_mapping.json"},
	{Path: "/rest/api/3/issuetype/project", Query: url.Values{"projectId": {"10002"}}, File: "issuetype_project.json"},
	{Path: "/rest/api/3/project/TS/statuses", File: "project_statuses.json"},
	{Path: "/rest/api/3/statuses", Query: url.Values{"id": {"10008", "10009", "3", "10010"}}, File: "statuses_byid.json"},
	{Path: "/rest/api/3/resolution/search", File: "resolution_search.json"},
	{Path: "/rest/api/3/priorityscheme", File: "priorityscheme.json"},
	{Path: "/rest/api/3/priorityscheme/10124/priorities", File: "priorityscheme_priorities.json"},

	// Slice 2: screens.
	{Path: "/rest/api/3/issuetypescreenscheme/project", Query: url.Values{"projectId": {"10002"}}, File: "itss_project.json"},
	{Path: "/rest/api/3/issuetypescreenscheme/mapping", Query: url.Values{"issueTypeScreenSchemeId": {"10002"}}, File: "itss_mapping.json"},
	{Path: "/rest/api/3/screenscheme", Query: url.Values{"id": {"10012", "10013", "10014"}}, File: "screenscheme.json"},
	{Path: "/rest/api/3/screens", Query: url.Values{"id": {"10012", "10013", "10014"}}, File: "screens.json"},
	{Path: "/rest/api/3/screens/10012/tabs", File: "tabs_10012.json"},
	{Path: "/rest/api/3/screens/10013/tabs", File: "tabs_10013.json"},
	{Path: "/rest/api/3/screens/10014/tabs", File: "tabs_10014.json"},
	{Path: "/rest/api/3/screens/10012/tabs/10015/fields", File: "tabfields_10012_10015.json"},
	{Path: "/rest/api/3/screens/10013/tabs/10016/fields", File: "tabfields_10013_10016.json"},
	{Path: "/rest/api/3/screens/10014/tabs/10017/fields", File: "tabfields_10014_10017.json"},

	// Slice 2: fields, from the field scheme (the site uses field schemes).
	{Path: "/rest/api/3/field", File: "field.json"},
	{Path: "/rest/api/3/config/fieldschemes/projects", Query: url.Values{"projectId": {"10002"}}, File: "fieldschemes_projects.json"},
	{Path: "/rest/api/3/config/fieldschemes/1", File: "fieldscheme_1.json"},
	{Path: "/rest/api/3/config/fieldschemes/1/fields", File: "fieldscheme_1_fields.json"},
	{Path: "/rest/api/3/field/search", Query: url.Values{"type": {"custom"}, "expand": {"searcherKey,isLocked"}}, File: "field_search.json"},

	// Slice 3: workflows. The bulk read is the one documented read-only POST used.
	{Path: "/rest/api/3/workflowscheme/project", Query: url.Values{"projectId": {"10002"}}, File: "workflowscheme_project.json"},
	{Method: http.MethodPost, Path: "/rest/api/3/workflows", Body: `{"workflowNames":["Software Simplified Workflow for Project TS"]}`, File: "workflows_read.json"},

	// Slice 5: roles, permissions, notifications, issue security, components,
	// versions, properties. TS has no issue security scheme: Jira answers 404.
	{Path: "/rest/api/3/project/TS/role", File: "project_role.json"},
	{Path: "/rest/api/3/role", File: "role.json"},
	{Path: "/rest/api/3/project/TS/role/10002", File: "project_role_10002.json"},
	{Path: "/rest/api/3/project/TS/role/10003", File: "project_role_10003.json"},
	{Path: "/rest/api/3/project/TS/role/10014", File: "project_role_10014.json"},
	{Path: "/rest/api/3/project/TS/permissionscheme", Query: url.Values{"expand": {"all"}}, File: "permissionscheme.json"},
	{Path: "/rest/api/3/project/TS/notificationscheme", Query: url.Values{"expand": {"all"}}, File: "notificationscheme.json"},
	{Path: "/rest/api/3/project/TS/issuesecuritylevelscheme", File: "issuesecuritylevelscheme.json", Status: http.StatusNotFound},
	{Path: "/rest/api/3/project/TS/component", File: "component.json"},
	{Path: "/rest/api/3/project/TS/version", File: "version.json"},
	{Path: "/rest/api/3/project/TS/properties", File: "properties.json"},
	{Path: "/rest/api/3/project/TS/properties/jira-slack-integration-projectLeadNotified", File: "property_jira-slack-integration-projectLeadNotified.json"},
	{Path: "/rest/api/3/project/TS/properties/jswSelectedBoardType", File: "property_jswSelectedBoardType.json"},
	{Path: "/rest/api/3/project/TS/properties/project.created.via.v2", File: "property_project.created.via.v2.json"},
}

// legacyRoutes replace the field-scheme routes with the legacy field
// configuration data recorded from the same space, and make the field-scheme
// API answer 404 as it does on sites without field schemes.
// tsRoutes are every route for the space as it really is (field schemes).
var tsRoutes = append(slices.Clone(baseRoutes), contextRoutes()...)

var legacyRoutes = append(slices.DeleteFunc(slices.Clone(tsRoutes), func(r jiratest.Route) bool {
	return strings.HasPrefix(r.Path, "/rest/api/3/config/fieldschemes")
}),
	jiratest.Route{Path: "/rest/api/3/config/fieldschemes/projects", Status: http.StatusNotFound},
	jiratest.Route{Path: "/rest/api/3/fieldconfigurationscheme/project", Query: url.Values{"projectId": {"10002"}}, File: "fcs_project.json"},
	jiratest.Route{Path: "/rest/api/3/fieldconfiguration", Query: url.Values{"isDefault": {"true"}}, File: "fieldconfiguration.json"},
	jiratest.Route{Path: "/rest/api/3/fieldconfiguration/10000/fields", File: "fieldconfiguration_10000_fields.json"},
)

func fixedNow() time.Time { return time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC) }

func exportTS(t *testing.T) (*spec.Spec, *jiratest.Server) {
	t.Helper()
	return exportWith(t, tsRoutes)
}

func exportWith(t *testing.T, routes []jiratest.Route) (*spec.Spec, *jiratest.Server) {
	t.Helper()
	return exportFrom(t, "testdata/ts", "TS", routes)
}

func exportFrom(t *testing.T, dir, key string, routes []jiratest.Route) (*spec.Spec, *jiratest.Server) {
	t.Helper()
	srv := jiratest.New(t, dir, routes)
	s, err := Space(context.Background(), srv.Client(), key, Options{ToolVersion: "test", Now: fixedNow})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	return s, srv
}

// TestExportTSGolden compares the export of the recorded space with the
// golden files, after replacing the fake server's random address: once
// through field schemes (as the site really is) and once through the legacy
// field configurations recorded from the same space.
func TestExportTSGolden(t *testing.T) {
	t.Run("field schemes", func(t *testing.T) { checkGolden(t, tsRoutes, "ts.golden.yaml") })
	t.Run("legacy", func(t *testing.T) { checkGolden(t, legacyRoutes, "ts-legacy.golden.yaml") })
}

func checkGolden(t *testing.T, routes []jiratest.Route, name string) {
	s, srv := exportWith(t, routes)
	for _, r := range srv.Unused() {
		t.Errorf("fixture never requested: %s %s", r.Method, r.Path)
	}
	out, err := spec.Write(s)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.ReplaceAll(string(out), srv.URL(), "https://example.atlassian.net")
	golden := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("export differs from %s (go test ./internal/export -update to accept):\n%s", golden, got)
	}
}

// TestExportIsValidAndStable: the export passes the tool's own validation,
// and its canonical text is a fixpoint (guarantee R2).
func TestExportIsValidAndStable(t *testing.T) {
	s, _ := exportTS(t)
	if diags := spec.Validate(s); diags.HasErrors() {
		t.Fatalf("export does not validate:\n%v", diags)
	}
	first, err := spec.Write(s)
	if err != nil {
		t.Fatal(err)
	}
	reparsed, diags := spec.Parse(first)
	if diags.HasErrors() {
		t.Fatalf("export does not parse:\n%v", diags)
	}
	second, err := spec.Write(reparsed)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("parsing and rewriting the export changed it")
	}
}

// TestExportOnlyReads: every request was a GET or one of the documented
// read-only POSTs (bulk workflow read, context lookup), every request had a
// fixture and every fixture was requested.
func TestExportOnlyReads(t *testing.T) {
	_, srv := exportTS(t)
	for _, r := range srv.Requests() {
		readPost := r == "POST /rest/api/3/workflows" ||
			regexp.MustCompile(`^POST /rest/api/3/field/customfield_\d+/context/mapping(\?startAt=\d+)?$`).MatchString(r) ||
			regexp.MustCompile(`^POST /gateway/api/automation/public/jira/[^/]+/rest/v1/rule/summary$`).MatchString(r)
		if !strings.HasPrefix(r, "GET ") && !readPost {
			t.Errorf("request other than GET or a documented read-only POST: %s", r)
		}
	}
	for _, r := range srv.Unused() {
		t.Errorf("fixture never requested: %s %s", r.Method, r.Path)
	}
	if n := len(srv.Requests()); n != len(tsRoutes) {
		t.Errorf("export made %d requests, want %d", n, len(tsRoutes))
	}
}

// TestFixturesAreScrubbed guards against committing identifying data
// recorded from a real site.
func TestFixturesAreScrubbed(t *testing.T) {
	host := regexp.MustCompile(`[a-z0-9-]+\.atlassian\.net`)
	email := regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	account := regexp.MustCompile(`\d{6}(:|%3[Aa])[0-9a-f-]{36}`)                // %3A: in a URL
	fakeAccount := regexp.MustCompile(`^000000:00000000-0000-0000-0000-\d{12}$`) // numbered by the scrub script
	var files []string
	for _, pattern := range []string{"testdata/*/*.json", "../apply/testdata/*/*.json", "../apply/testdata/*/*.yaml"} {
		found, err := filepath.Glob(pattern)
		if err != nil || len(found) == 0 {
			t.Fatalf("no fixtures match %s: %v", pattern, err)
		}
		files = append(files, found...)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range host.FindAllString(string(data), -1) {
			if h != "example.atlassian.net" {
				t.Errorf("%s contains site %q", f, h)
			}
		}
		for _, e := range email.FindAllString(string(data), -1) {
			if !strings.HasSuffix(e, "@example.com") {
				t.Errorf("%s contains email %q", f, e)
			}
		}
		for _, a := range account.FindAllString(string(data), -1) {
			if !fakeAccount.MatchString(strings.Replace(strings.Replace(a, "%3A", ":", 1), "%3a", ":", 1)) {
				t.Errorf("%s contains account ID %q", f, a)
			}
		}
	}
}

// mappingBody is the context lookup export sends: the space and each of its
// issue types, in scheme order.
const mappingBody = `{"mappings":[{"projectId":"10002","issueTypeId":"10012"},{"projectId":"10002","issueTypeId":"10000"},` +
	`{"projectId":"10002","issueTypeId":"10005"},{"projectId":"10002","issueTypeId":"10013"},{"projectId":"10002","issueTypeId":"10014"}]}`

func contextMappingRoute(field string) jiratest.Route {
	return jiratest.Route{Method: http.MethodPost, Path: "/rest/api/3/field/" + field + "/context/mapping",
		Body: mappingBody, File: "ctxmap_" + field + ".json"}
}

// contextRoutes are the slice 4 routes, one set per recorded field.
func contextRoutes() []jiratest.Route {
	var routes []jiratest.Route
	files, _ := filepath.Glob("testdata/ts/ctxmap_*.json")
	for _, f := range files {
		field := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "ctxmap_"), ".json")
		routes = append(routes, contextMappingRoute(field))
		var mapped struct {
			Values []struct{ ContextID string } `json:"values"`
		}
		data, _ := os.ReadFile(f)
		_ = json.Unmarshal(data, &mapped)
		ctx := mapped.Values[0].ContextID
		base := "/rest/api/3/field/" + field + "/context"
		for prefix, path := range map[string]string{
			"defaults_": base + "/defaultValues", "options_": base + "/" + ctx + "/option",
			"context_": base, "projectmapping_": base + "/projectmapping", "legacydefaults_": base + "/defaultValue",
		} {
			if _, err := os.Stat("testdata/ts/" + prefix + field + ".json"); err == nil {
				routes = append(routes, jiratest.Route{Path: path, File: prefix + field + ".json"})
			}
		}
	}
	return routes
}

// TestDefaultsNotSupportedIsNoDefault: Jira answers 400 "not supported" for
// default values of some fields; that means no default, not a failure.
func TestDefaultsNotSupportedIsNoDefault(t *testing.T) {
	routes := slices.Clone(tsRoutes)
	for i, r := range routes {
		if r.Path == "/rest/api/3/field/customfield_10004/context/defaultValues" {
			routes[i] = jiratest.Route{Path: r.Path, File: "../defaults_not_supported.json", Status: http.StatusBadRequest}
		}
	}
	s, _ := exportWith(t, routes)
	for _, set := range s.Space.FieldContexts {
		if set.Field == "Impact" && set.Contexts[0].Default.Set {
			t.Error("a not-supported answer produced a default")
		}
	}
}

// TestExportJSCTEST2 exports the second test space (a classic workflow
// alongside the simplified one, transition screens, events) from its
// recording, which jsc-record made with a manifest of every request.
func TestExportJSCTEST2(t *testing.T) {
	routes, err := jiratest.LoadRoutes("testdata/jsctest2")
	if err != nil {
		t.Fatal(err)
	}
	s, srv := exportFrom(t, "testdata/jsctest2", "JSCTEST2", routes)
	for _, r := range srv.Unused() {
		t.Errorf("fixture never requested: %s %s", r.Method, r.Path)
	}
	if diags := spec.Validate(s); diags.HasErrors() {
		t.Fatalf("export does not validate:\n%v", diags)
	}
	out, err := spec.Write(s)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.ReplaceAll(string(out), srv.URL(), "https://example.atlassian.net")
	golden := "testdata/jsctest2.golden.yaml"
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("export differs from %s (go test ./internal/export -update to accept)", golden)
	}
}

// TestDefaultsFallBackOutsideEarlyAccess: on sites outside Atlassian's early
// access programme the newer defaults endpoint answers 403; export then reads
// the deprecated one and still finds the default.
func TestDefaultsFallBackOutsideEarlyAccess(t *testing.T) {
	routes, err := jiratest.LoadRoutes("testdata/jsctest2")
	if err != nil {
		t.Fatal(err)
	}
	const field = "/rest/api/3/field/customfield_10043/context"
	for i, r := range routes {
		if r.Path == field+"/defaultValues" {
			routes[i] = jiratest.Route{Path: r.Path, File: "../defaults_early_access.json", Status: http.StatusForbidden}
		}
	}
	routes = append(routes, jiratest.Route{Path: field + "/defaultValue", File: "../defaults_legacy_10043.json"})
	s, _ := exportFrom(t, "testdata/jsctest2", "JSCTEST2", routes)
	for _, set := range s.Space.FieldContexts {
		if set.Field == "JSCTEST2 Severity" {
			if d := set.Contexts[0].Default; !d.Set || d.V.Str != "3 – Medium" {
				t.Errorf("default after fallback = %+v, want 3 – Medium", d)
			}
			return
		}
	}
	t.Error("JSCTEST2 Severity has no contexts in the export")
}
