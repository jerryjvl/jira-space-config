package steps

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// raw turns JSON text into the format's raw value, as a decoded spec has it.
func raw(t *testing.T, text string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatal(err)
	}
	var conv func(any) any
	conv = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			var out []spec.KVAny
			for k, val := range x {
				out = append(out, spec.KVAny{Key: k, Value: conv(val)})
			}
			return out
		case []any:
			for i := range x {
				x[i] = conv(x[i])
			}
			return x
		case string:
			return spec.ScalarValue{Tag: "!!str", Value: x}
		case float64:
			return spec.ScalarValue{Tag: "!!int", Value: strconv.FormatFloat(x, 'f', -1, 64)}
		case bool:
			if x {
				return spec.ScalarValue{Tag: "!!bool", Value: "true"}
			}
			return spec.ScalarValue{Tag: "!!bool", Value: "false"}
		}
		return nil
	}
	return conv(v)
}

const ruleBody = `{"trigger":{"component":"TRIGGER","type":"jira.issue.event.trigger:link","schemaVersion":-1,"value":{"linkTypes":["Duplicate"]},"conditions":[]},
 "components":[{"component":"BRANCH","type":"jira.issue.related","schemaVersion":1,"value":{"relatedType":"parent"},
   "children":[{"component":"ACTION","type":"jira.issue.comment","schemaVersion":1,"value":{"comment":"Linked"},"children":[],"conditions":[]}],"conditions":[]}],
 "notifyOnError":"FIRSTERROR"}`

func TestRuleComparison(t *testing.T) {
	want := spec.AutomationRule{Name: "VULN R", Rule: raw(t, ruleBody)}
	withComponentIDs := strings.Replace(ruleBody, `"component":"BRANCH",`, `"component":"BRANCH","id":"b-1","parentId":null,`, 1)
	got := spec.AutomationRule{Name: "VULN R", State: "enabled", Scope: "space", Rule: raw(t, withComponentIDs)}
	if d := compareRules(want, got); len(d) != 0 {
		t.Errorf("components' IDs make a difference: %v", d)
	}
	got.Rule = raw(t, strings.Replace(ruleBody, `"Linked"`, `"Duplicated"`, 1))
	d := compareRules(want, got)
	if len(d) != 1 || d[0].Attribute != "rule.components[0].children[0].value.comment" || d[0].Spec != `"Linked"` || d[0].Jira != `"Duplicated"` {
		t.Errorf("diffs %+v", d)
	}
	got.State = "disabled"
	if d := compareRules(want, got); len(d) != 2 || d[0].Attribute != "state" {
		t.Errorf("state: %+v", d)
	}
}

func TestRuleWrites(t *testing.T) {
	_, env := testSite(t)
	ari, err := env.R.SpaceARI(env.P)
	if err != nil {
		t.Fatal(err)
	}
	r := spec.AutomationRule{Name: "VULN R", Description: spec.Some("Comments"), Rule: raw(t, ruleBody)}
	base := "/automation/public/jira/cloud-1/rest/v1/rule"

	l := &log{answers: map[string]string{"GET /rest/api/3/myself": `{"accountId":"acc-1"}`, "POST " + base: `{"ruleUuid":"new-1"}`}}
	id, err := writeRule(env, ari, r, "")(context.Background(), l)
	if err != nil || id != "new-1" {
		t.Fatalf("create: %q, %v", id, err)
	}
	created := l.sent[len(l.sent)-1]
	for _, w := range []string{`"authorAccountId":"acc-1"`, `"description":"Comments [managed-by: jira-space-config]"`, `"state":"ENABLED"`,
		`"ruleScopeARIs":["ari:cloud:jira:cloud-1:project/10042"]`, `"notifyOnError":"FIRSTERROR"`} {
		if !strings.Contains(created, w) {
			t.Errorf("create lacks %s:\n%s", w, created)
		}
	}
	var sent struct {
		Rule struct {
			Components []struct {
				ID       string `json:"id"`
				Children []struct {
					ID, ParentID string
				} `json:"children"`
			} `json:"components"`
		} `json:"rule"`
	}
	_ = json.Unmarshal([]byte(strings.TrimPrefix(created, "POST "+base+" ")), &sent)
	c := sent.Rule.Components[0]
	if len(c.ID) != 36 || len(c.Children) != 1 || len(c.Children[0].ID) != 36 {
		t.Errorf("new IDs: %+v", sent.Rule.Components)
	}

	existing := `{"rule":{"authorAccountId":"acc-9","trigger":{"id":"t-1","type":"jira.issue.event.trigger:link"},
		"components":[{"id":"b-1","type":"jira.issue.related","children":[{"id":"c-1","type":"jira.issue.comment"}]}]}}`
	l = &log{answers: map[string]string{"GET " + base + "/r1": existing}}
	if _, err := writeRule(env, ari, r, "r1")(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	put := l.sent[1]
	for _, w := range []string{`"authorAccountId":"acc-9"`, `"id":"t-1"`, `"id":"b-1"`, `"id":"c-1"`, `"parentId":"b-1"`} {
		if !strings.Contains(put, w) {
			t.Errorf("update lacks %s:\n%s", w, put)
		}
	}
	if l.sent[2] != `PUT `+base+`/r1/state {"value":"ENABLED"}` {
		t.Errorf("state: %s", l.sent[2])
	}
}

const connectedRule = `{"trigger":{"component":"TRIGGER","type":"jira.manual.trigger.issue","connectionId":null},
 "components":[{"component":"ACTION","type":"rovo3p.use-claude.action","connectionId":"old-1","value":{"prompt":"Hi"}}],
 "connections":[{"id":"old-1","accountId":"acc","connectionTargetKey":"com.atlassian.rovo","authType":"ASAP","container":{"id":"r"},"createdAt":1,"updatedAt":2}]}`

// TestRuleConnections: a new rule's connections get fresh IDs that the
// components follow; a changed rule's own are kept and not listed again;
// two rules differing only in connection IDs compare equal.
func TestRuleConnections(t *testing.T) {
	build := func() map[string]any {
		return rulePayload("ari:x", spec.AutomationRule{Name: "R", Rule: raw(t, connectedRule)})
	}
	componentConnection := func(p map[string]any) string {
		c := p["rule"].(map[string]any)["components"].([]any)[0].(map[string]any)
		return c["connectionId"].(string)
	}

	fresh := build()
	renumberConnections(fresh, nil)
	list := fresh["connections"].([]any)
	id := list[0].(map[string]any)["id"].(string)
	if len(list) != 1 || id == "old-1" || len(id) != 36 || componentConnection(fresh) != id {
		t.Errorf("new rule: %v, component uses %q", list, componentConnection(fresh))
	}
	for _, k := range []string{"container", "createdAt", "updatedAt"} {
		if _, there := list[0].(map[string]any)[k]; there {
			t.Errorf("%s was sent", k)
		}
	}

	kept := build()
	renumberConnections(kept, map[string]bool{"old-1": true})
	if len(kept["connections"].([]any)) != 0 || componentConnection(kept) != "old-1" {
		t.Errorf("changed rule: %v, component uses %q", kept["connections"], componentConnection(kept))
	}

	a := spec.AutomationRule{Name: "R", Rule: raw(t, connectedRule)}
	b := spec.AutomationRule{Name: "R", State: "enabled", Scope: "space", Rule: raw(t, strings.ReplaceAll(connectedRule, "old-1", "other-9"))}
	if d := compareRules(a, b); len(d) != 0 {
		t.Errorf("connection IDs make a difference: %v", d)
	}
}
