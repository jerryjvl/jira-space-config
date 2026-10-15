package export

import (
	"context"
	"encoding/json"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/jira/jiratest"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// TestLegacyOwnFieldConfigurationScheme: on a site without field schemes, a
// space with its own field configuration scheme, where Bug has a field
// configuration of its own (environment hidden, due date required).
func TestLegacyOwnFieldConfigurationScheme(t *testing.T) {
	dir, routes := legacyOwnSchemeFixture(t)

	t.Run("export", func(t *testing.T) {
		srv := jiratest.New(t, dir, routes)
		s, err := Space(context.Background(), srv.Client(), "TS", Options{ToolVersion: "test", Now: fixedNow})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range srv.Unused() {
			t.Errorf("fixture never requested: %s %s", r.Method, r.Path)
		}
		if b := s.Space.Binding.FieldConfigurationScheme; b == nil || b.Name != "TS Field Configuration Scheme" || b.ID != "10100" {
			t.Errorf("field configuration scheme binding = %+v", b)
		}
		for _, it := range s.Space.IssueTypes {
			want := "Default Field Configuration"
			if it.Name == "Bug" {
				want = "TS Bug Fields"
			}
			if got := it.Binding.FieldConfiguration.Name; got != want {
				t.Errorf("%s uses field configuration %q, want %q", it.Name, got, want)
			}
		}
		field := func(name string) spec.SpaceField {
			for _, f := range s.Space.Fields.V {
				if f.Name == name {
					return f
				}
			}
			t.Fatalf("no field %q", name)
			return spec.SpaceField{}
		}
		if got := field("Environment").IssueTypes; slices.Contains(got, "Bug") || len(got) != 4 {
			t.Errorf("Environment issue types = %v; want every issue type but Bug", got)
		}
		due := field("Due date")
		if len(due.IssueTypes) != 0 || len(due.ByIssueType) != 1 || due.ByIssueType[0].IssueType != "Bug" ||
			due.ByIssueType[0].Required != spec.Some(true) {
			t.Errorf("Due date = %+v; want required for Bug only", due)
		}
		if diags := spec.Validate(s); diags.HasErrors() {
			t.Errorf("export does not validate:\n%v", diags)
		}
	})

	t.Run("a mapped configuration Jira does not return", func(t *testing.T) {
		tmp := editedCopy(t, dir, "fieldconfiguration_both.json", func(v any) {
			m := obj(v)
			m["values"] = arr(m["values"])[:1]
		})
		srv := jiratest.New(t, tmp, routes)
		_, err := Space(context.Background(), srv.Client(), "TS", Options{ToolVersion: "test", Now: fixedNow})
		if err == nil || !strings.Contains(err.Error(), `issue type "Bug" uses field configuration 10001, which Jira did not return`) {
			t.Errorf("error = %v", err)
		}
	})
}

// legacyOwnSchemeFixture is TS as it would be on a site without field
// schemes and with a field configuration scheme of its own: Bug has a
// configuration where environment is hidden and due date is required. The
// recorded TS space uses Jira's system default scheme, so this is synthetic,
// built from TS's recorded default configuration.
func legacyOwnSchemeFixture(t *testing.T) (string, []jiratest.Route) {
	t.Helper()
	dir := t.TempDir()
	copyDir(t, "testdata/ts", dir)
	var defaultItems struct {
		Values []map[string]any `json:"values"`
	}
	data, err := os.ReadFile("testdata/ts/fieldconfiguration_10000_fields.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &defaultItems); err != nil {
		t.Fatal(err)
	}
	var bugItems []map[string]any
	for _, it := range defaultItems.Values {
		it = maps.Clone(it)
		switch it["id"] {
		case "environment":
			it["isHidden"] = true
		case "duedate":
			it["isRequired"] = true
		}
		bugItems = append(bugItems, it)
	}
	page := func(values any) map[string]any {
		return map[string]any{"isLast": true, "startAt": 0, "maxResults": 50, "values": values}
	}
	writeJSON(t, dir, "fcs_project.json", page([]any{map[string]any{
		"projectIds":               []string{"10002"},
		"fieldConfigurationScheme": map[string]any{"id": "10100", "name": "TS Field Configuration Scheme", "description": ""},
	}}))
	writeJSON(t, dir, "fcs_mapping.json", page([]any{
		map[string]any{"issueTypeId": "default", "fieldConfigurationId": "10000", "fieldConfigurationSchemeId": "10100"},
		map[string]any{"issueTypeId": "10013", "fieldConfigurationId": "10001", "fieldConfigurationSchemeId": "10100"},
	}))
	writeJSON(t, dir, "fieldconfiguration_both.json", page([]any{
		map[string]any{"id": 10000, "name": "Default Field Configuration", "description": "The default field configuration", "isDefault": true},
		map[string]any{"id": 10001, "name": "TS Bug Fields", "description": ""},
	}))
	writeJSON(t, dir, "fieldconfiguration_10001_fields.json", page(bugItems))

	routes := slices.DeleteFunc(slices.Clone(legacyRoutes), func(r jiratest.Route) bool {
		return r.Path == "/rest/api/3/fieldconfiguration"
	})
	return dir, append(routes,
		jiratest.Route{Path: "/rest/api/3/fieldconfigurationscheme/mapping", Query: url.Values{"fieldConfigurationSchemeId": {"10100"}}, File: "fcs_mapping.json"},
		jiratest.Route{Path: "/rest/api/3/fieldconfiguration", Query: url.Values{"id": {"10000", "10001"}}, File: "fieldconfiguration_both.json"},
		jiratest.Route{Path: "/rest/api/3/fieldconfiguration/10001/fields", File: "fieldconfiguration_10001_fields.json"},
	)
}

func writeJSON(t *testing.T, dir, name string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
