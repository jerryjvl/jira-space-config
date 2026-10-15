package steps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/jira/jiratest"
	"github.com/jerryjvl/jira-space-config/internal/reader"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// The synthetic site of the apply tests, for turning names into IDs.
func testSite(t *testing.T) (*reader.Reader, *Env) {
	t.Helper()
	page := func(_, file string) jiratest.Route { return jiratest.Route{File: "../../apply/testdata/site/" + file} }
	routes := []jiratest.Route{}
	add := func(path, file string, query url.Values) {
		r := page("", file)
		r.Path, r.Query = path, query
		routes = append(routes, r)
	}
	add("/rest/api/3/field", "field.json", nil)
	add("/rest/api/3/field/search", "field_search.json", nil)
	add("/rest/api/3/statuses/search", "statuses_Open.json", url.Values{"searchString": {"Open"}})
	add("/rest/api/3/statuses/search", "statuses_Nowhere.json", url.Values{"searchString": {"Nowhere"}})
	add("/rest/api/3/statuses/search", "statuses_Done.json", url.Values{"searchString": {"Done"}})
	add("/rest/api/3/role", "role.json", nil)
	add("/rest/api/3/group/bulk", "group_bulk.json", nil)
	add("/rest/api/3/resolution/search", "resolution_search.json", nil)
	add("/rest/api/3/issuetype", "issuetype.json", nil)
	add("/rest/api/3/screens", "screens_VULN Triage Screen.json", url.Values{"queryString": {"Missing Screen"}})
	add("/rest/api/3/screens", "screens_Default Screen.json", url.Values{"queryString": {"Default Screen"}})
	add("/rest/api/3/priority/search", "priority_search.json", nil)
	add("/_edge/tenant_info", "tenant_info.json", nil)
	add("/rest/api/3/permissionscheme", "permissionschemes.json", nil)
	routes = append(routes,
		jiratest.Route{Path: "/rest/api/3/priorityscheme", Query: url.Values{"schemeName": {"Nothing"}}, File: "empty_page.json"},
		jiratest.Route{Path: "/rest/api/3/fieldconfigurationscheme", File: "fieldconfigurationscheme.json"},
		jiratest.Route{Path: "/rest/api/3/fieldconfigurationscheme/mapping", File: "empty_page.json"},
		jiratest.Route{Path: "/rest/api/3/fieldconfiguration", File: "empty_page.json"},
		jiratest.Route{Path: "/rest/api/3/statuses", Query: url.Values{"id": {"99"}}, File: "empty_list.json"},
		jiratest.Route{Path: "/rest/api/3/events", File: "events.json"},
		jiratest.Route{Path: "/rest/api/3/filter/search", Query: url.Values{"filterName": {"Nowhere"}}, File: "empty_page.json"},
		jiratest.Route{Path: "/rest/api/3/workflows/search", Query: url.Values{"queryString": {"VULN Workflow"}}, File: "../../apply/testdata/site/workflows_VULN Workflow.json"},
		jiratest.Route{Path: "/rest/api/3/workflows/search", Query: url.Values{"queryString": {"Nowhere"}}, File: "empty_page.json"},
		jiratest.Route{Method: http.MethodPost, Path: "/rest/api/3/workflows", Body: `{"workflowNames":["Gone","VULN Workflow"]}`, File: "../../apply/testdata/site/workflows_read.json"})
	srv := jiratest.New(t, "testdata", routes)
	r := reader.New(context.Background(), srv.Client())
	s := &spec.Spec{Space: &spec.Space{Key: "VULN"}, CustomFields: []spec.CustomField{
		{GlobalObject: spec.GlobalObject{Name: "Target", Meta: spec.Meta{ID: "customfield_10051"}}, Type: "text"}}}
	return r, &Env{R: r, Spec: s, P: &reader.Project{ID: "10042", Key: "VULN"}}
}

func TestResolverNamesToIDs(t *testing.T) {
	r, env := testSite(t)
	x := resolver{r, env.Spec}
	ok := []struct {
		name string
		got  func() (string, error)
		want string
	}{
		{"system field", func() (string, error) { return x.field("Summary") }, "summary"},
		{"ambiguous field the spec gives an id for", func() (string, error) { return x.field("Target") }, "customfield_10051"},
		{"field by id", func() (string, error) { return x.field("id:customfield_1") }, "customfield_1"},
		{"status", func() (string, error) { return x.ref(spec.RefStatus, "Open") }, "1"},
		{"role", func() (string, error) { return x.ref(spec.RefRole, "Administrators") }, "10002"},
		{"group", func() (string, error) { return x.ref(spec.RefGroup, "team-alpha") }, "g-1"},
		{"user", func() (string, error) { return x.ref(spec.RefUser, "acc-1") }, "acc-1"},
		{"resolution", func() (string, error) { return x.resolution("Fixed") }, "10000"},
	}
	for _, tc := range ok {
		if got, err := tc.got(); err != nil || got != tc.want {
			t.Errorf("%s: %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
	bad := []struct {
		name string
		err  func() error
		want string
	}{
		{"unknown field", func() error { _, err := x.field("Nothing"); return err }, `no field on the site is named "Nothing"`},
		{"ambiguous field", func() error {
			_, err := resolver{r, &spec.Spec{}}.field("Target")
			return err
		}, `2 fields on the site are named "Target"`},
		{"unknown status", func() error { _, err := x.status("Nowhere"); return err }, `no status on the site is named "Nowhere"`},
		{"unknown status id", func() error { _, err := x.status("id:99"); return err }, "no status has id 99"},
		{"unknown role", func() error { _, err := x.role("Nobody"); return err }, `no role type on the site is named "Nobody"`},
		{"unknown group", func() error { _, err := x.group("nobody"); return err }, `no group on the site is named "nobody"`},
		{"unknown resolution", func() error { _, err := x.resolution("Gone"); return err }, `no resolution on the site is named "Gone"`},
	}
	for _, tc := range bad {
		if err := tc.err(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v; want %q", tc.name, err, tc.want)
		}
	}
}

func TestRuleToJira(t *testing.T) {
	r, env := testSite(t)
	x := resolver{r, env.Spec}
	got, err := ruleToJira(x, spec.ListConditions, spec.Rule{Type: "restrict_to", Params: []spec.Param{
		{Key: "roles", Kind: spec.KindList, List: []string{"Administrators"}}, {Key: "extra", Kind: spec.KindMap, Map: []spec.KV{{Key: "future", Value: "1"}}}}})
	if err != nil || got.RuleKey != "system:restrict-issue-transition" || got.Parameters["roleIds"] != "10002" || got.Parameters["future"] != "1" {
		t.Errorf("restrict_to: %+v, %v", got, err)
	}
	opaque, err := ruleToJira(x, spec.ListPostFunctions, spec.Rule{Type: spec.RuleOpaque, Params: []spec.Param{
		{Key: "key", Str: "forge:x"}, {Key: "parameters", Map: []spec.KV{{Key: "a", Value: "b"}}}}})
	if err != nil || opaque.RuleKey != "forge:x" || opaque.Parameters["a"] != "b" {
		t.Errorf("opaque: %+v, %v", opaque, err)
	}
	for name, tc := range map[string]struct {
		rule spec.Rule
		want string
	}{
		"unknown type":      {spec.Rule{Type: "nothing"}, `no conditions rule is of type "nothing"`},
		"unknown parameter": {spec.Rule{Type: "restrict_to", Params: []spec.Param{{Key: "colour"}}}, `rule restrict_to has no parameter "colour"`},
		"unknown role":      {spec.Rule{Type: "restrict_to", Params: []spec.Param{{Key: "roles", List: []string{"Nobody"}}}}, "no role type"},
	} {
		if _, err := ruleToJira(x, spec.ListConditions, tc.rule); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestDefaultValues(t *testing.T) {
	options := map[string]string{"High": "1", "Host": "2", "Host/Linux": "3"}
	for typ, v := range map[string]spec.Value{"select": {Str: "High"}, "multiselect": {IsList: true, List: []string{"High"}},
		"cascading": {IsList: true, List: []string{"Host", "Linux"}}, "text": {Str: "x"}, "number": {Str: "2.5"},
		"url": {Str: "https://x"}, "labels": {IsList: true, List: []string{"a"}}} {
		if _, err := defaultValue("c", v, options, typ); err != nil {
			t.Errorf("%s: %v", typ, err)
		}
	}
	if _, err := defaultValue("c", spec.Value{Str: "Low"}, options, "select"); err == nil || !strings.Contains(err.Error(), `the default "Low" is not one of the options`) {
		t.Errorf("not an option: %v", err)
	}
	if _, err := defaultValue("c", spec.Value{Str: "x"}, options, "group"); err == nil || !strings.Contains(err.Error(), "the format cannot set a default for a field of type") {
		t.Errorf("unsupported type: %v", err)
	}
	for typ, tc := range map[string]struct {
		v    spec.Value
		want string
	}{
		"date":      {spec.Value{Str: "current"}, `{"contextId":"c","type":"datepicker","useCurrent":true}`},
		"datetime":  {spec.Value{Str: "2026-01-01T09:00"}, `{"contextId":"c","dateTime":"2026-01-01T09:00","type":"datetimepicker"}`},
		"user":      {spec.Value{Str: "user:acc-1"}, `{"accountId":"acc-1","contextId":"c","type":"single.user.select","userFilter":{"enabled":false}}`},
		"multiuser": {spec.Value{IsList: true, List: []string{"user:acc-1"}}, `{"accountIds":["acc-1"],"contextId":"c","type":"multi.user.select"}`},
	} {
		d, err := defaultValue("c", tc.v, options, typ)
		if got, _ := json.Marshal(d); err != nil || string(got) != tc.want {
			t.Errorf("%s: %s, %v", typ, got, err)
		}
	}
	if d, err := noDefault("c", "user"); err != nil || d["type"] != "single.user.select" || d["userFilter"] == nil {
		t.Errorf("no user default: %v, %v", d, err)
	}
	if _, err := noDefault("c", "group"); err == nil || !strings.Contains(err.Error(), `of type "group" away is not supported`) {
		t.Errorf("no group default: %v", err)
	}
}

// answers is a Sender that answers by path, for writes that read Jira's
// answer.
type answers map[string]string

func (a answers) Send(_ context.Context, method, path string, _ url.Values, _, out any) error {
	text, ok := a[method+" "+path]
	if !ok {
		text = "{}"
	}
	return decode(text, out)
}

func TestWorkflowWrites(t *testing.T) {
	r, env := testSite(t)
	x := resolver{r, env.Spec}
	w := spec.Workflow{Name: "W", Statuses: []spec.WorkflowStatus{{Name: "Open"}},
		Transitions: []spec.Transition{{Name: "Create", Type: spec.TransitionInitial, To: "Open"}}}
	for name, tc := range map[string]struct {
		w      spec.Workflow
		answer answers
		want   string
	}{
		"Jira's validation finds a problem": {w, answers{"POST /rest/api/3/workflows/create/validation": `{"errors":[{"level":"ERROR","message":"bad"}]}`},
			"validating the workflow, Jira found: bad"},
		"an answer without the workflow": {w, answers{}, "the answer does not say which workflow was created"},
		"an unknown screen": {withTransition(w, spec.Transition{Name: "T", Type: spec.TransitionGlobal, To: "Open", Screen: spec.Some("Missing Screen")}),
			answers{}, `no screen on the site is named "Missing Screen"`},
		"an unknown event": {withTransition(w, spec.Transition{Name: "T", Type: spec.TransitionGlobal, To: "Open", Event: spec.Some("Gone")}),
			answers{}, `no event on the site is named "Gone"`},
	} {
		if _, err := createWorkflow(env, x, tc.w)(context.Background(), tc.answer); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func withTransition(w spec.Workflow, t spec.Transition) spec.Workflow {
	w.Transitions = append(append([]spec.Transition{}, w.Transitions...), t)
	return w
}

func TestContextsForUnknownIssueTypes(t *testing.T) {
	r, env := testSite(t)
	do := createContexts(env, resolver{r, env.Spec}, "Summary", []spec.FieldContext{{IssueTypes: []string{"Nothing"}}})
	if _, err := do(context.Background(), answers{}); err == nil || !strings.Contains(err.Error(), `no issue type on the site is named "Nothing"`) {
		t.Errorf("err = %v", err)
	}

}

func decode(text string, out any) error {
	if out == nil {
		return nil
	}
	return json.Unmarshal([]byte(text), out)
}

// TestWorkflowCreateKeepsStatusDescriptions: Jira updates the statuses a
// workflow create lists, so each must carry its description (seen live:
// leaving it out cleared it).
func TestWorkflowCreateKeepsStatusDescriptions(t *testing.T) {
	r, env := testSite(t)
	payload, err := workflowRequest(env, resolver{r, env.Spec}, spec.Workflow{Name: "W", Statuses: []spec.WorkflowStatus{{Name: "Open"}},
		Transitions: []spec.Transition{{Name: "Create", Type: spec.TransitionInitial, To: "Open"}}})
	if err != nil {
		t.Fatal(err)
	}
	statuses := payload["statuses"].([]map[string]string)
	if d, ok := statuses[0]["description"]; !ok || d != "" {
		t.Errorf("status sent without its description: %v", statuses[0])
	}
	managed := jiraDescription(spec.GlobalObject{Mode: spec.ModeEnsure, Description: spec.Some("Being assessed")})
	if managed != "Being assessed [managed-by: jira-space-config]" {
		t.Errorf("a status the tool made is sent as %q", managed)
	}
}
