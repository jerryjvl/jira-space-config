package export

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/jira/jiratest"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// TestJiraContractChecks edits one recorded response at a time into
// something Jira should not answer, or a case the recorded spaces lack, and
// requires the error or note export has for it. Each case is the only way
// the check it names can fire.
func TestJiraContractChecks(t *testing.T) {
	jsc, err := jiratest.LoadRoutes("testdata/jsctest2")
	if err != nil {
		t.Fatal(err)
	}
	ts := func() (string, string, []jiratest.Route) { return "testdata/ts", "TS", tsRoutes }
	legacy := func() (string, string, []jiratest.Route) { return "testdata/ts", "TS", legacyRoutes }
	j2 := func() (string, string, []jiratest.Route) { return "testdata/jsctest2", "JSCTEST2", jsc }
	tests := []struct {
		name    string
		set     func() (string, string, []jiratest.Route)
		file    string
		edit    func(v any) // changes the decoded response in place
		wantErr string      // the error export must stop with, or
		note    string      // the note it must add
	}{
		{"team-managed space", ts, "project.json", func(v any) { obj(v)["style"] = "next-gen" }, "is team-managed", ""},
		{"two issue type schemes", ts, "issuetypescheme_project.json", twice("values"), "expected one issue type scheme for space TS, Jira returned 2", ""},
		{"issue type without details", ts, "issuetype_project.json", func(v any) { arr(v)[0].(map[string]any)["id"] = "99999" }, "but Jira did not return its details", ""},
		{"unknown status category", ts, "project_statuses.json", func(v any) {
			obj(at(v, 0, "statuses", 0, "statusCategory"))["key"] = "blocked"
		}, "", `has status category "blocked", which the format has no name for`},
		{"translated status name", ts, "project_statuses.json", func(v any) {
			obj(at(v, 0, "statuses", 0))["untranslatedName"] = "Stored Backlog"
		}, "", `Status "Stored Backlog" is shown as "Backlog"`},
		{"a space status the statuses API does not return", ts, "statuses_byid.json", func(v any) { obj(arr(v)[0])["id"] = "99999" },
			`status "Backlog" is used by the space, but the statuses API does not return it`, ""},
		{"no priority scheme at all", ts, "priorityscheme.json", func(v any) { obj(v)["values"] = []any{} }, "no priority scheme applies to space TS", ""},
		{"two custom fields with one name", ts, "field_search.json", func(v any) {
			vals := arr(at(v, "values"))
			obj(vals[1])["name"] = obj(vals[0])["name"]
		}, "", "are both named"},
		{"field restricted to another space's issue type", ts, "fieldscheme_1_fields.json", func(v any) {
			obj(at(v, "values", 0))["restrictedToWorkTypes"] = []any{"10000", "99999"}
		}, "", "mentions issue type 99999, which is not in this space"},
		{"no default field configuration", legacy, "fieldconfiguration.json", func(v any) {
			obj(at(v, "values", 0))["isDefault"] = false
		}, "the space uses the default field configuration, but Jira did not return one", ""},
		{"two issue type screen schemes", ts, "itss_project.json", twice("values"), "expected one issue type screen scheme for space TS, Jira returned 2", ""},
		{"screen scheme missing", ts, "screenscheme.json", func(v any) { obj(at(v, "values", 0))["id"] = 99999 }, "uses screen scheme 10012, which Jira did not return", ""},
		{"screen scheme mapping for another space's issue type", ts, "itss_mapping.json", func(v any) {
			m := obj(v)
			m["values"] = append(arr(m["values"]), map[string]any{"issueTypeId": "99999", "issueTypeScreenSchemeId": "10002", "screenSchemeId": "10012"})
		}, "", "maps issue type 99999, which is not in this space"},
		{"screen missing", ts, "screens.json", func(v any) { m := obj(v); m["values"] = arr(m["values"])[1:] }, "is used by the space but Jira did not return it", ""},
		{"two workflow schemes", ts, "workflowscheme_project.json", twice("values"), "expected one workflow scheme for space TS, Jira returned 2", ""},
		{"workflow scheme mapping for another space's issue type", ts, "workflowscheme_project.json", func(v any) {
			obj(at(v, "values", 0, "workflowScheme"))["issueTypeMappings"] = map[string]any{"99999": "Other"}
		}, "", "maps issue type 99999, which is not in this space"},
		{"workflow missing", ts, "workflows_read.json", func(v any) { obj(v)["workflows"] = []any{} }, "asked Jira for 1 workflows", ""},
		{"project-scoped workflow", ts, "workflows_read.json", func(v any) {
			obj(at(v, "workflows", 0, "scope"))["type"] = "PROJECT"
		}, "", `has scope "PROJECT"`},
		{"status reference not described", ts, "workflows_read.json", func(v any) { m := obj(v); m["statuses"] = arr(m["statuses"])[1:] }, "which Jira did not describe", ""},
		{"transition without a target", ts, "workflows_read.json", func(v any) {
			obj(at(v, "workflows", 0, "transitions", 0))["toStatusReference"] = ""
		}, "", "read as looped; this reading of the API is unconfirmed"},
		{"holder type without a name", ts, "permissionscheme.json", func(v any) {
			obj(at(v, "permissions", 0, "holder"))["type"] = "somethingNew"
		}, "", `holder of type "somethingNew"`},
		{"role link of another shape", ts, "project_role.json", func(v any) {
			obj(v)["Administrators"] = "https://example.atlassian.net/rest/api/3/role"
		}, `role "Administrators" has an unexpected link`, ""},
		{"role member neither user nor group", ts, "project_role_10003.json", func(v any) {
			a := obj(at(v, "actors", 0))
			delete(a, "actorUser")
			a["type"] = "atlassian-app-role-actor"
		}, "", `member of type "atlassian-app-role-actor"`},
		{"no context for a mapped context ID", ts, "context_customfield_10004.json", func(v any) { obj(v)["values"] = []any{} }, "Jira returned 0", ""},
		{"transition screen rule of another kind", j2, "post_workflows_ab1c1abc.json", func(v any) {
			obj(at(v, "workflows", 0, "transitions", 3, "transitionScreen"))["ruleKey"] = "app:screen"
		}, "", `transition screen rule "app:screen" this tool does not understand`},
		{"resolution property naming an unknown resolution", j2, "post_workflows_ab1c1abc.json", func(v any) {
			obj(at(v, "workflows", 0, "transitions", 3, "properties"))["jira.field.resolution.include"] = "99999"
		}, "", "refers to resolution 99999, which is not on the site"},
		{"event not on the site", j2, "get_events.json", func(v any) { clear(v.([]any)) /* every event without an ID */ }, "", "which is not on the site; it is kept as id:"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir, key, routes := tc.set()
			srv := jiratest.New(t, editedCopy(t, dir, tc.file, tc.edit), routes)
			s, err := Space(context.Background(), srv.Client(), key, Options{ToolVersion: "test", Now: fixedNow})
			switch {
			case tc.wantErr != "":
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %v, want one containing %q", err, tc.wantErr)
				}
			case err != nil:
				t.Fatalf("export: %v", err)
			case !strings.Contains(strings.Join(s.Notes, "\n"), tc.note):
				t.Errorf("no note contains %q; notes:\n%s", tc.note, strings.Join(s.Notes, "\n"))
			}
		})
	}
}

func obj(v any) map[string]any { return v.(map[string]any) }
func arr(v any) []any          { return v.([]any) }

// at walks decoded JSON by object keys and array indexes.
func at(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			v = obj(v)[k]
		case int:
			v = arr(v)[k]
		}
	}
	return v
}

// twice repeats the first element of a top-level list.
func twice(key string) func(v any) {
	return func(v any) {
		m := obj(v)
		m[key] = append(arr(m[key]), arr(m[key])[0])
	}
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range entries {
		if en.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(from, en.Name()))
		if err != nil {
			t.Fatal(err)
		}
		//nolint:gosec // G703: names from the fixture directory, written into the test's own temp directory
		if err := os.WriteFile(filepath.Join(to, en.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestWorkTypeOverridesFieldSettings: a work type's own parameters in a field
// scheme override the field's, including back to the defaults. JSCTEST2's
// Severity is required; here Bug's parameters say it is not, with the plain
// renderer and the field's description.
func TestWorkTypeOverridesFieldSettings(t *testing.T) {
	routes, err := jiratest.LoadRoutes("testdata/jsctest2")
	if err != nil {
		t.Fatal(err)
	}
	tmp := editedCopy(t, "testdata/jsctest2", "get_config_fieldschemes_4_fields_04a46787.json", func(v any) {
		for _, it := range arr(at(v, "values")) {
			if obj(it)["fieldId"] == "customfield_10043" {
				obj(it)["parameters"].(map[string]any)["rendererType"] = "atlassian-wiki-renderer"
				obj(it)["workTypeParameters"] = []any{map[string]any{
					"workTypeId": "10013", "isRequired": false, "rendererType": "jira-text-renderer",
					"description": "Choose the severity after triage.",
				}}
			}
		}
	})
	srv := jiratest.New(t, tmp, routes)
	s, err := Space(context.Background(), srv.Client(), "JSCTEST2", Options{ToolVersion: "test", Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range s.Space.Fields.V {
		if f.Name != "JSCTEST2 Severity" {
			continue
		}
		if f.Required != spec.Some(true) || f.Renderer != spec.Some("wiki") {
			t.Errorf("field settings = %+v", f.FieldSettings)
		}
		want := []spec.IssueTypeFieldSettings{{IssueType: "Bug", FieldSettings: spec.FieldSettings{
			Required: spec.Some(false), Renderer: spec.Some("plain"),
		}}}
		if !reflect.DeepEqual(f.ByIssueType, want) {
			t.Errorf("by_issue_type = %+v, want %+v", f.ByIssueType, want)
		}
		out, err := spec.Write(s)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), "Bug: {required: false, renderer: plain}") {
			t.Errorf("written spec lacks the override:\n%s", out)
		}
		back, diags := spec.Parse(out)
		if diags = append(diags, spec.Validate(back)...); diags.HasErrors() {
			t.Errorf("written spec does not validate:\n%v", diags)
		}
		return
	}
	t.Fatal("no JSCTEST2 Severity field")
}

// TestRoleLookupForRulesFails: a workflow rule naming a role makes export
// read the role types; if that read fails, export stops. JSCTEST2's rules
// name no roles, so one is added.
func TestRoleLookupForRulesFails(t *testing.T) {
	routes, err := jiratest.LoadRoutes("testdata/jsctest2")
	if err != nil {
		t.Fatal(err)
	}
	tmp := editedCopy(t, "testdata/jsctest2", "post_workflows_ab1c1abc.json", func(v any) {
		obj(at(v, "workflows", 0, "transitions", 1, "conditions", "conditions", 0, "parameters"))["roleIds"] = "10002"
	})
	for i, r := range routes {
		if r.Path == "/rest/api/3/role" {
			routes[i] = jiratest.Route{Path: r.Path, Status: http.StatusInternalServerError}
		}
	}
	srv := jiratest.New(t, tmp, routes)
	_, err = Space(context.Background(), srv.Client(), "JSCTEST2", Options{ToolVersion: "test", Now: fixedNow})
	if err == nil || !strings.Contains(err.Error(), "reading role types to name them: GET /rest/api/3/role: 500") {
		t.Errorf("error = %v", err)
	}
}

// editedCopy copies a fixture directory and applies edit to one decoded file.
func editedCopy(t *testing.T, dir, file string, edit func(v any)) string {
	t.Helper()
	tmp := t.TempDir()
	copyDir(t, dir, tmp)
	path := filepath.Join(tmp, file)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	edit(v)
	if data, err = json.Marshal(v); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return tmp
}

// TestWorkflowsUseStoredStatusNames: when the exporting account sees a
// translated status name, workflows refer to the status by its stored name,
// the one statuses: declares.
func TestWorkflowsUseStoredStatusNames(t *testing.T) {
	tmp := editedCopy(t, "testdata/ts", "project_statuses.json", func(v any) {
		obj(at(v, 0, "statuses", 0))["untranslatedName"] = "Stored Backlog" // displayed as Backlog
	})
	srv := jiratest.New(t, tmp, tsRoutes)
	s, err := Space(context.Background(), srv.Client(), "TS", Options{ToolVersion: "test", Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, st := range s.Workflows[0].Statuses {
		names = append(names, st.Name)
	}
	if !slices.Contains(names, "Stored Backlog") || slices.Contains(names, "Backlog") {
		t.Errorf("workflow statuses = %v; want the stored name Stored Backlog", names)
	}
	for _, d := range spec.Validate(s) {
		if strings.Contains(d.Message, "Backlog") {
			t.Errorf("unexpected diagnostic: %s", d.Format("export"))
		}
	}
}

// TestCustomFieldsOnlyPermissionsName: a custom field the space does not
// have, but a permission grants to (a user field holder), is declared with
// its full definition, and gets no field_contexts entry.
func TestCustomFieldsOnlyPermissionsName(t *testing.T) {
	routes, err := jiratest.LoadRoutes("testdata/jsctest2")
	if err != nil {
		t.Fatal(err)
	}
	tmp := editedCopy(t, "testdata/jsctest2", "get_project_JSCTEST2_permissionscheme_397ba4fd.json", func(v any) {
		obj(at(v, "permissions", 0))["holder"] = map[string]any{"type": "userCustomField", "parameter": "customfield_10036"}
	})
	srv := jiratest.New(t, tmp, routes)
	s, err := Space(context.Background(), srv.Client(), "JSCTEST2", Options{ToolVersion: "test", Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, cf := range s.CustomFields {
		if cf.ID == "customfield_10036" {
			found = true
			if cf.Name != "Vulnerability" || cf.Description != spec.Some("Custom field that stores vulnerability information for JQL") {
				t.Errorf("definition = %+v", cf)
			}
		}
	}
	if !found {
		t.Error("customfield_10036 is not declared")
	}
	for _, set := range s.Space.FieldContexts {
		if set.Field == "Vulnerability" {
			t.Error("field_contexts has an entry for a field the space does not have")
		}
	}
	var holders []string
	for _, g := range s.Space.Permissions.V.Grants {
		holders = append(holders, g.Holders...)
	}
	if !slices.Contains(holders, "user_field:Vulnerability") {
		t.Errorf("no grant to user_field:Vulnerability in %v", holders)
	}
}
