package reader

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/jira/jiratest"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// usageRoutes serve the apply tests' site: screen 10100 is in a screen
// scheme of space 10042 and on a transition of workflow wf-9, which space
// 10088 uses.
func usageRoutes() []jiratest.Route {
	return []jiratest.Route{
		{Path: "/rest/api/3/screenscheme", File: "screenscheme_all.json"},
		{Path: "/rest/api/3/issuetypescreenscheme/mapping", File: "itss_mapping_all.json"},
		{Path: "/rest/api/3/issuetypescreenscheme/10300/project", File: "itss_project_10300.json"},
		{Path: "/rest/api/3/workflows/search", Query: url.Values{"isActive": {"true"}}, File: "workflows_active.json"},
		{Path: "/rest/api/3/workflow/wf-9/projectUsages", File: "workflow_wf-9_usages.json"},
	}
}

func TestScreenUsers(t *testing.T) {
	srv := jiratest.New(t, "../apply/testdata/site", usageRoutes())
	got, err := New(context.Background(), srv.Client()).ScreenUsers("10100")
	if err != nil || !slices.Equal(got, []string{"10042", "10088"}) {
		t.Errorf("ScreenUsers = %v, %v", got, err)
	}
	jiratest.CheckDocumented(t, "../../docs/api/openapi", srv.Requests())
}

// TestUsageReadsFail: a usage read that fails is an error, never "unused".
func TestUsageReadsFail(t *testing.T) {
	for _, r := range usageRoutes() {
		t.Run(r.Path, func(t *testing.T) {
			routes := usageRoutes()
			for i := range routes {
				if routes[i].Path == r.Path {
					routes[i].File, routes[i].Status = "", http.StatusInternalServerError
				}
			}
			srv := jiratest.New(t, "../apply/testdata/site", routes)
			_, err := New(context.Background(), srv.Client()).ScreenUsers("10100")
			if err == nil || !strings.Contains(err.Error(), "checking which") {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestSchemeUsers(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("its.json", `{"values":[{"id":"20","projects":{"isLast":false,"values":[{"id":"10042"}]}}]}`)
	write("none.json", `{"values":[]}`)
	write("projects.json", `{"isLast":true,"values":[{"id":"10042"},{"id":"10077"}]}`)
	srv := jiratest.New(t, dir, []jiratest.Route{
		{Path: "/rest/api/3/issuetypescheme", Query: url.Values{"id": {"20"}}, File: "its.json"},
		{Path: "/rest/api/3/issuetypescheme", Query: url.Values{"id": {"21"}}, File: "none.json"},
		{Path: "/rest/api/3/issuetypescheme", Query: url.Values{"id": {"22"}}, Status: http.StatusInternalServerError},
		{Path: "/rest/api/3/config/fieldschemes/7/projects", File: "projects.json"},
		{Path: "/rest/api/3/issuetypescreenscheme/8/project", Status: http.StatusInternalServerError},
	})
	r := New(context.Background(), srv.Client())
	if got, err := r.IssueTypeSchemeUsers("20"); err != nil || !slices.Equal(got, []string{"10042", "more"}) {
		t.Errorf("more than a page: %v, %v", got, err)
	}
	if _, err := r.IssueTypeSchemeUsers("21"); err == nil || !strings.Contains(err.Error(), "Jira returned 0 schemes") {
		t.Errorf("no scheme: %v", err)
	}
	if _, err := r.IssueTypeSchemeUsers("22"); err == nil || !strings.Contains(err.Error(), "checking which spaces use issue type scheme 22") {
		t.Errorf("failed read: %v", err)
	}
	if got, err := r.FieldSchemeUsers("7"); err != nil || !slices.Equal(got, []string{"10042", "10077"}) {
		t.Errorf("field scheme: %v, %v", got, err)
	}
	if _, err := r.ScreenSchemeUsers("8"); err == nil || !strings.Contains(err.Error(), "checking which spaces use issue type screen scheme 8") {
		t.Errorf("failed read: %v", err)
	}
	jiratest.CheckDocumented(t, "../../docs/api/openapi", srv.Requests())
}

func TestMappedSchemeUsersFail(t *testing.T) {
	srv := jiratest.New(t, t.TempDir(), []jiratest.Route{{Path: "/rest/api/3/notificationscheme/project", Status: http.StatusInternalServerError}})
	if _, err := New(context.Background(), srv.Client()).NotificationSchemeUsers("4"); err == nil ||
		!strings.Contains(err.Error(), "checking which spaces use notification scheme 4") {
		t.Errorf("err = %v", err)
	}
}

func TestPermissionSchemeUsers(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{
		"projects.json": `{"isLast":true,"values":[{"id":"10042"},{"id":"10077"}]}`,
		"a.json":        `{"id":60}`, "b.json": `{"id":"61"}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	routes := []jiratest.Route{
		{Path: "/rest/api/3/project/search", File: "projects.json"},
		{Path: "/rest/api/3/project/10042/permissionscheme", File: "a.json"},
		{Path: "/rest/api/3/project/10077/permissionscheme", File: "b.json"},
	}
	srv := jiratest.New(t, dir, routes)
	if got, err := New(context.Background(), srv.Client()).PermissionSchemeUsers("60"); err != nil || !slices.Equal(got, []string{"10042"}) {
		t.Errorf("users: %v, %v", got, err)
	}
	jiratest.CheckDocumented(t, "../../docs/api/openapi", srv.Requests())
	for i, want := range []string{"listing the spaces to check", "checking which spaces use permission scheme 60"} {
		failing := slices.Clone(routes)
		failing[i] = jiratest.Route{Path: failing[i].Path, Status: http.StatusInternalServerError}
		srv := jiratest.New(t, dir, failing)
		if _, err := New(context.Background(), srv.Client()).PermissionSchemeUsers("60"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}
}

func TestFilterLookups(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "boards.json"), []byte(`{"isLast":true,"values":[{"location":{"projectId":10042}},{"location":{}}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := jiratest.New(t, dir, []jiratest.Route{
		{Path: "/rest/agile/1.0/board", Query: url.Values{"filterId": {"5"}}, File: "boards.json"},
		{Path: "/rest/agile/1.0/board", Query: url.Values{"filterId": {"6"}}, Status: http.StatusInternalServerError},
		{Path: "/rest/api/3/filter/search", Status: http.StatusInternalServerError},
	})
	r := New(context.Background(), srv.Client())
	if got, err := r.FilterUsers("5"); err != nil || !slices.Equal(got, []string{"10042", "a user's board"}) {
		t.Errorf("users: %v, %v", got, err)
	}
	if _, err := r.FilterUsers("6"); err == nil || !strings.Contains(err.Error(), "checking which boards show filter 6") {
		t.Errorf("failed read: %v", err)
	}
	if _, err := r.FindFilters(&Project{ID: "10042"}, "F"); err == nil || !strings.Contains(err.Error(), `looking up filters named "F"`) {
		t.Errorf("failed lookup: %v", err)
	}
}

// TestAutomationRules: the space's rules are read page by page, with their
// components' IDs left out; a rule that covers other spaces too is shared.
func TestAutomationRules(t *testing.T) {
	dir := t.TempDir()
	z := "00000000-0000-0000-0000-000000000000"
	base := "/gateway/api/automation/public/jira/" + z + "/rest/v1"
	scope := `"scope":"ari:cloud:jira:` + z + `:project/10042"`
	rule, err := os.ReadFile("testdata/automation_rule.json")
	if err != nil {
		t.Fatal(err)
	}
	pages := map[string]string{
		"page1.json":  `{"data":[{"uuid":"r1"}],"links":{"next":"?cursor=c2&limit=100"}}`,
		"page2.json":  `{"data":[{"uuid":"r2"}],"links":{"next":null}}`,
		"tenant.json": `{"cloudId":"` + z + `"}`,
		"r1.json":     strings.Replace(string(rule), `"connections": []`, `"connections": [{"id":"c1","connectionTargetKey":"k","container":{"id":"r"},"createdAt":1,"updatedAt":2}]`, 1),
		"r2.json":     strings.Replace(string(rule), `"ruleScopeARIs": [`, `"ruleScopeARIs": ["ari:cloud:jira:x:project/1", `, 1),
	}
	for name, text := range pages {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	routes := []jiratest.Route{
		{Path: "/_edge/tenant_info", File: "tenant.json"},
		{Method: http.MethodPost, Path: base + "/rule/summary", Body: `{"limit":100,` + scope + `}`, File: "page1.json"},
		{Method: http.MethodPost, Path: base + "/rule/summary", Body: `{"cursor":"c2","limit":100,` + scope + `}`, File: "page2.json"},
		{Path: base + "/rule/r1", File: "r1.json"},
		{Path: base + "/rule/r2", File: "r2.json"},
	}
	srv := jiratest.New(t, dir, routes)
	got, err := New(context.Background(), srv.Client()).AutomationRules(&Project{ID: "10042"})
	if err != nil || len(got) != 2 {
		t.Fatalf("%v, %v", got, err)
	}
	if got[0].Mode != spec.ModeManaged || got[0].Scope != "space" || got[0].Description.V != "Closes duplicates" || got[0].State != "disabled" {
		t.Errorf("rule 1: %+v", got[0])
	}
	if got[1].Mode != spec.ModeReuse || got[1].Scope != "shared" {
		t.Errorf("rule 2: %+v", got[1])
	}
	body, _ := json.Marshal(spec.JSONValue(got[0].Rule))
	if !strings.Contains(string(body), `"connectionTargetKey":"k"`) || strings.Contains(string(body), "createdAt") || strings.Contains(string(body), `"container"`) {
		t.Errorf("connections: %s", body)
	}
	if strings.Contains(string(body), `"parentId"`) || strings.Contains(string(body), `"id":"08db`) || !strings.Contains(string(body), `"type":"jira.issue.event.trigger:link"`) {
		t.Errorf("body: %s", body)
	}
	jiratest.CheckDocumented(t, "../../docs/api/openapi", srv.Requests())

	routes[3] = jiratest.Route{Path: base + "/rule/r1", Status: http.StatusInternalServerError}
	srv = jiratest.New(t, dir, routes)
	if _, err := New(context.Background(), srv.Client()).AutomationRules(&Project{ID: "10042"}); err == nil || !strings.Contains(err.Error(), "reading automation rule r1") {
		t.Errorf("failed read: %v", err)
	}
}

// TestBareContexts: a context with no options and no default is kept when it
// is the space's own (it limits the field to some issue types, and a run that
// did not see it would make it again), and left out when it is Jira's global
// default for the field.
func TestBareContexts(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"mapping.json":    `{"isLast":true,"values":[{"contextId":"50","issueTypeId":"7"}]}`,
		"defaults.json":   `{"isLast":true,"values":[]}`,
		"own.json":        `{"isLast":true,"values":[{"id":"50","name":"VULN Notes","isGlobalContext":false}]}`,
		"global.json":     `{"isLast":true,"values":[{"id":"50","name":"Default Configuration Scheme for Notes","isGlobalContext":true}]}`,
		"projectmap.json": `{"isLast":true,"values":[{"contextId":"50","projectId":"10"}]}`,
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read := func(contextFile string) []spec.FieldContext {
		routes := []jiratest.Route{
			{Method: http.MethodPost, Path: "/rest/api/3/field/customfield_1/context/mapping", Body: `{"mappings":[{"projectId":"10","issueTypeId":"7"}]}`, File: "mapping.json"},
			{Path: "/rest/api/3/field/customfield_1/context/defaultValues", File: "defaults.json"},
			{Path: "/rest/api/3/field/customfield_1/context", File: contextFile},
			{Path: "/rest/api/3/field/customfield_1/context/projectmapping", File: "projectmap.json"},
		}
		srv := jiratest.New(t, dir, routes)
		its := &IssueTypes{Space: []spec.SpaceIssueType{{Name: "Vulnerability"}}, ids: []string{"7"}}
		cf := spec.CustomField{GlobalObject: spec.GlobalObject{Name: "Notes", Meta: spec.Meta{ID: "customfield_1"}}, Type: "text"}
		sets, err := New(context.Background(), srv.Client()).FieldContexts(&Project{ID: "10", Key: "VULN"}, its, []spec.CustomField{cf})
		if err != nil {
			t.Fatal(err)
		}
		if len(sets) == 0 {
			return nil
		}
		return sets[0].Contexts
	}
	if own := read("own.json"); len(own) != 1 || own[0].Binding == nil || own[0].Binding.Scope != spec.ScopeSpace {
		t.Errorf("the space's own bare context: %+v", own)
	}
	if global := read("global.json"); len(global) != 0 {
		t.Errorf("a global bare context was kept: %+v", global)
	}
}
