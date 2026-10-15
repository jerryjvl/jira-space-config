package reader

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/jira/jiratest"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// testReader has every lookup a rule may need pre-filled, so rule
// conversion runs without any requests (it has no client).
func testReader() *Reader {
	r := New(context.Background(), nil)
	r.siteFields = map[string]siteField{
		"summary":           {ID: "summary", Name: "Summary"},
		"customfield_10100": {ID: "customfield_10100", Name: "Severity", Custom: true},
	}
	r.statusNames = map[string]string{"3": "In Progress", "10010": "Done"}
	r.roleTypes, r.roleTypesRead = []roleType{{ID: 10002, Name: "Administrators"}}, true
	r.groupNames = map[string]string{"g-1": "team-alpha"}
	r.resolutionsRead = true
	return r
}

func rawParams(params map[string]string) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for k, v := range params {
		b, _ := json.Marshal(v)
		out[k] = b
	}
	return out
}

// sampleValue is a plausible Jira value for a parameter, and the typed value
// it should become.
func sampleValue(ps spec.ParamSpec) (jira string, typed string) {
	switch ps.Conv {
	case spec.ConvBool:
		return "true", "true"
	case spec.ConvEnum:
		keys := make([]string, 0, len(ps.Values))
		for k := range ps.Values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys[0], ps.Values[keys[0]]
	case spec.ConvJSONList:
		return `["a","b"]`, "a,b"
	}
	ids := map[string][2]string{
		spec.RefField:  {"customfield_10100", "Severity"},
		spec.RefStatus: {"10010", "Done"},
		spec.RefRole:   {"10002", "Administrators"},
		spec.RefGroup:  {"g-1", "team-alpha"},
		spec.RefUser:   {"5b10ac8d82e05b22cc7d4ef5", "5b10ac8d82e05b22cc7d4ef5"},
	}
	if v, ok := ids[ps.Ref]; ok {
		return v[0], v[1]
	}
	return "value-" + ps.Key, "value-" + ps.Key
}

func paramText(p spec.Param) string {
	if p.Kind == spec.KindList {
		return strings.Join(p.List, ",")
	}
	return p.Str
}

// TestEveryTypedRuleFromJira builds a Jira rule for every registry entry,
// with every parameter set, and checks it converts to that typed rule with
// every value translated.
func TestEveryTypedRuleFromJira(t *testing.T) {
	lists := []string{spec.ListConditions, spec.ListValidators, spec.ListPostFunctions}
	for _, list := range lists {
		for _, key := range spec.RuleKeys() {
			for _, rs := range spec.RulesFor(list, key) {
				t.Run(list+"/"+rs.Type, func(t *testing.T) {
					params := map[string]string{}
					want := map[string]string{}
					for k, v := range rs.Fixed {
						params[k] = v
					}
					for _, ps := range rs.Params {
						jv, tv := sampleValue(ps)
						params[ps.Jira] = jv
						want[ps.Key] = tv
					}
					e := testReader()
					got := e.rule(list, jiraRule{RuleKey: key, Parameters: rawParams(params)}, "test")
					if got.Type != rs.Type {
						t.Fatalf("converted to %q, want %q (params %v)", got.Type, rs.Type, params)
					}
					for _, p := range got.Params {
						if p.Key == "extra" {
							t.Errorf("unexpected extra parameters %v", p.Map)
							continue
						}
						if paramText(p) != want[p.Key] {
							t.Errorf("%s = %q, want %q", p.Key, paramText(p), want[p.Key])
						}
					}
					if len(got.Params) != len(rs.Params) {
						t.Errorf("%d parameters, want %d", len(got.Params), len(rs.Params))
					}
					if len(e.notes) > 0 {
						t.Errorf("unexpected notes: %v", e.notes)
					}
				})
			}
		}
	}
}

func TestRuleFallbacks(t *testing.T) {
	tests := []struct {
		name     string
		list     string
		rule     jiraRule
		wantType string
		check    func(t *testing.T, r spec.Rule, notes []string)
	}{
		{"unknown key stays opaque", spec.ListPostFunctions,
			jiraRule{RuleKey: "forge:workflow-post-function", Parameters: rawParams(map[string]string{"config": "{}"})},
			spec.RuleOpaque, func(t *testing.T, r spec.Rule, _ []string) {
				if r.Params[0].Str != "forge:workflow-post-function" || r.Params[1].Map[0] != (spec.KV{Key: "config", Value: "{}"}) {
					t.Errorf("opaque rule lost data: %+v", r.Params)
				}
			}},
		{"unexpected enum value stays opaque", spec.ListPostFunctions,
			jiraRule{RuleKey: "system:update-field", Parameters: rawParams(map[string]string{"field": "summary", "value": "x", "mode": "prepend"})},
			spec.RuleOpaque, nil},
		{"empty value and mode is clear_field", spec.ListPostFunctions,
			jiraRule{RuleKey: "system:update-field", Parameters: rawParams(map[string]string{"field": "summary", "value": "", "mode": ""})},
			"clear_field", nil},
		{"value without mode is set_field", spec.ListPostFunctions,
			jiraRule{RuleKey: "system:update-field", Parameters: rawParams(map[string]string{"field": "summary", "value": "x", "mode": ""})},
			"set_field", func(t *testing.T, r spec.Rule, _ []string) {
				if len(r.Params) != 2 {
					t.Errorf("empty optional mode should be omitted: %+v", r.Params)
				}
			}},
		{"unnamed parameter goes to extra", spec.ListValidators,
			jiraRule{RuleKey: "system:check-permission-validator", Parameters: rawParams(map[string]string{"permissionKey": "CREATE_ISSUES", "future": "1"})},
			"permission", func(t *testing.T, r spec.Rule, _ []string) {
				last := r.Params[len(r.Params)-1]
				if last.Key != "extra" || last.Map[0] != (spec.KV{Key: "future", Value: "1"}) {
					t.Errorf("extra parameter not kept: %+v", r.Params)
				}
			}},
		{"unresolvable ID is kept and noted", spec.ListValidators,
			jiraRule{RuleKey: "system:validate-field-value", Parameters: rawParams(map[string]string{"ruleType": "fieldRequired", "fieldsRequired": "summary,customfield_99999"})},
			"fields_required", func(t *testing.T, r spec.Rule, notes []string) {
				if got := strings.Join(r.Params[0].List, ","); got != "Summary,id:customfield_99999" {
					t.Errorf("fields = %s", got)
				}
				if len(notes) != 1 || !strings.Contains(notes[0], "customfield_99999") {
					t.Errorf("notes = %v", notes)
				}
			}},
		{"wrong validate-field-value type is opaque", spec.ListValidators,
			jiraRule{RuleKey: "system:validate-field-value", Parameters: rawParams(map[string]string{"ruleType": "somethingNew", "fieldKey": "summary"})},
			spec.RuleOpaque, nil},
		{"same key in another list does not match", spec.ListConditions,
			jiraRule{RuleKey: "system:check-permission-validator", Parameters: rawParams(map[string]string{"permissionKey": "X"})},
			spec.RuleOpaque, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := testReader()
			r := e.rule(tc.list, tc.rule, "test")
			if r.Type != tc.wantType {
				t.Fatalf("type %q, want %q", r.Type, tc.wantType)
			}
			if tc.check != nil {
				tc.check(t, r, e.notes)
			}
		})
	}
}

// TestRegistryMatchesSiteCapabilities: every rule key the registry maps is
// one the recorded site offers, so the registry cannot drift from Jira.
func TestRegistryMatchesSiteCapabilities(t *testing.T) {
	data, err := os.ReadFile("../export/testdata/ts/workflow_capabilities.json")
	if err != nil {
		t.Fatal(err)
	}
	var caps struct {
		SystemRules  []struct{ RuleKey string } `json:"systemRules"`
		TriggerRules []struct{ RuleKey string } `json:"triggerRules"`
	}
	if err := json.Unmarshal(data, &caps); err != nil {
		t.Fatal(err)
	}
	var offered []string
	for _, r := range append(caps.SystemRules, caps.TriggerRules...) {
		offered = append(offered, r.RuleKey)
	}
	for _, key := range spec.RuleKeys() {
		if !slices.Contains(offered, key) {
			t.Errorf("registry maps %s, which the site does not offer", key)
		}
	}
	var untyped []string
	for _, key := range offered {
		if !slices.Contains(spec.RuleKeys(), key) && key != "system:transition-screen" {
			untyped = append(untyped, key)
		}
	}
	t.Logf("site rules exported as opaque rules for now: %v", untyped)
}

// TestClassicWorkflowFromJSCTEST2 converts the classic workflow recorded
// from the second test space: an initial transition whose only link is an
// arrow anchor, a self-loop stored as a directed transition, transitions
// with several sources, transition screens and several rule types.
func TestClassicWorkflowFromJSCTEST2(t *testing.T) {
	routes, err := jiratest.LoadRoutes("../export/testdata/jsctest2")
	if err != nil {
		t.Fatal(err)
	}
	var file string
	for _, r := range routes {
		if r.Method == http.MethodPost && r.Path == "/rest/api/3/workflows" {
			file = r.File
		}
	}
	data, err := os.ReadFile("../export/testdata/jsctest2/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var read struct {
		Workflows []workflow `json:"workflows"`
		Statuses  []struct {
			ID, Name string
			Ref      string `json:"statusReference"`
		} `json:"statuses"`
	}
	if err := json.Unmarshal(data, &read); err != nil {
		t.Fatal(err)
	}
	e := testReader()
	e.siteFields["assignee"] = siteField{ID: "assignee", Name: "Assignee"}
	e.siteFields["resolution"] = siteField{ID: "resolution", Name: "Resolution"}
	e.siteFields["customfield_10009"] = siteField{ID: "customfield_10009", Name: "Actual end", Custom: true}
	e.groupNames["58a02559-d97d-4dd0-835a-d1b4d19632c9"] = "jira-software-users"
	e.eventNames = map[string]string{"4": "Issue Resolved", "5": "Issue Closed", "7": "Issue Reopened", "11": "Work Started On Issue", "12": "Work Stopped On Issue"}
	for id, name := range map[string]string{"1": "Default Screen", "2": "Workflow Screen", "3": "Resolve Issue Screen"} {
		e.screens[id] = spec.Screen{Meta: spec.Meta{ID: id}, Name: name}
	}
	refNames := map[string]string{}
	for _, s := range read.Statuses {
		refNames[s.Ref] = s.Name
	}
	var classic *workflow
	for i := range read.Workflows {
		if read.Workflows[i].Name == "JSCTEST2 Classic Jira" {
			classic = &read.Workflows[i]
		}
	}
	if classic == nil {
		t.Fatal("fixture has no classic workflow")
	}
	wf, err := e.workflow(*classic, refNames, &WorkflowDefinitions{})
	if err != nil {
		t.Fatalf("workflow: %v", err)
	}
	byID := map[string]spec.Transition{}
	for _, tr := range wf.Transitions {
		byID[tr.ID] = tr
	}
	if tr := byID["1"]; tr.Type != spec.TransitionInitial || len(tr.From) != 0 || tr.To != "Open" {
		t.Errorf("initial transition: %+v", tr)
	}
	if tr := byID["6"]; tr.Looped || strings.Join(tr.From, ",") != "Resolved" || tr.To != "Resolved" {
		t.Errorf("self-loop should be directed Resolved → Resolved: %+v", tr)
	}
	if tr := byID["2"]; strings.Join(tr.From, ",") != "Open,In Progress,Reopened" || tr.Screen.V != "Resolve Issue Screen" || tr.Event.V != "Issue Closed" {
		t.Errorf("multi-source transition: from %v, screen %q, event %q", tr.From, tr.Screen.V, tr.Event.V)
	}
	if tr := byID["301"]; tr.Event.V != "Work Stopped On Issue" {
		t.Errorf("Stop Progress event = %q", tr.Event.V)
	}
	if tr := byID["4"]; tr.Conditions == nil || tr.Conditions.Items[0].Rule.Type != "restrict_to" || len(tr.PostFunctions) != 2 ||
		tr.PostFunctions[1].Type != "assign" {
		t.Errorf("Start Progress rules: %+v", tr)
	}
	for _, n := range e.notes {
		t.Errorf("unexpected note: %s", n)
	}
	// The whole workflow must survive the tool's own validation.
	s := &spec.Spec{APIVersion: spec.APIVersion, Kind: spec.Kind, Metadata: spec.Metadata{Name: "t"},
		Binding: &spec.SiteBinding{Site: "https://example.atlassian.net", CloudID: "00000000-0000-0000-0000-000000000000"},
		Space:   &spec.Space{Key: "T"}, Workflows: []spec.Workflow{wf}}
	if diags := spec.Validate(s); diags.HasErrors() {
		t.Errorf("exported workflow does not validate:\n%v", diags)
	}
}
