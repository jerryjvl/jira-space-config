package steps

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// recorder is a Sender that keeps what it is asked to send.
type recorder struct {
	method, path string
	body         map[string]any
	answer       string
}

func (r *recorder) Send(_ context.Context, method, path string, _ url.Values, body, out any) error {
	r.method, r.path = method, path
	data, _ := json.Marshal(body)
	_ = json.Unmarshal(data, &r.body)
	return decode(r.answer, out)
}

func TestCreateBodies(t *testing.T) {
	tests := []struct {
		name   string
		do     action
		answer string
		path   string
		want   map[string]any
		id     string
	}{
		{"priority without an icon", createPriority("https://example.atlassian.net", spec.Priority{
			GlobalObject: spec.GlobalObject{Name: "Urgent"}, Colour: spec.Some("#ff0000")}), `{"id":"10000"}`, "/rest/api/3/priority",
			map[string]any{"name": "Urgent", "statusColor": "#ff0000", "description": "[managed-by: jira-space-config]",
				"iconUrl": "https://example.atlassian.net/images/icons/priorities/medium.svg"}, "10000"},
		{"priority with an icon", createPriority("https://example.atlassian.net/", spec.Priority{
			GlobalObject: spec.GlobalObject{Name: "Low", Description: spec.Some("Later")}, Colour: spec.Some("#00ff00"),
			Icon: spec.Some("/images/icons/priorities/low.svg")}), `{"id":"10001"}`, "/rest/api/3/priority",
			map[string]any{"name": "Low", "statusColor": "#00ff00", "description": "Later [managed-by: jira-space-config]",
				"iconUrl": "https://example.atlassian.net/images/icons/priorities/low.svg"}, "10001"},
		{"select field gets its searcher", createCustomField(spec.CustomField{GlobalObject: spec.GlobalObject{Name: "Severity"}, Type: "select"}),
			`{"id":"customfield_10053"}`, "/rest/api/3/field",
			map[string]any{"name": "Severity", "description": "[managed-by: jira-space-config]",
				"type":        "com.atlassian.jira.plugin.system.customfieldtypes:select",
				"searcherKey": "com.atlassian.jira.plugin.system.customfieldtypes:multiselectsearcher"}, "customfield_10053"},
		{"role type, numeric ID", createRole(spec.Role{GlobalObject: spec.GlobalObject{Name: "Triage"}}), `{"id":10020}`, "/rest/api/3/role",
			map[string]any{"name": "Triage", "description": "[managed-by: jira-space-config]"}, "10020"},
		{"group, by group ID", createGroup(spec.GlobalObject{Name: "triagers"}), `{"groupId":"e73f"}`, "/rest/api/3/group",
			map[string]any{"name": "triagers"}, "e73f"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{answer: tc.answer}
			id, err := tc.do(context.Background(), r)
			if err != nil || id != tc.id {
				t.Errorf("id = %q, err = %v; want %q", id, err, tc.id)
			}
			if r.path != tc.path || r.method != "POST" {
				t.Errorf("sent %s %s", r.method, r.path)
			}
			got, _ := json.Marshal(r.body)
			want, _ := json.Marshal(tc.want)
			if string(got) != string(want) {
				t.Errorf("body %s\nwant %s", got, want)
			}
		})
	}
}

func TestTransitionIDs(t *testing.T) {
	got := transitionIDs([]spec.Transition{{}, {ID: "21"}, {}, {}, {}})
	if strings.Join(got, ",") != "1,21,11,31,41" {
		t.Errorf("ids %v", got)
	}
}
