package steps

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// log is a Sender that keeps every request, answering from a map by
// "METHOD path".
type log struct {
	sent    []string
	answers map[string]string
}

func (l *log) Send(_ context.Context, method, path string, _ url.Values, body, out any) error {
	line := method + " " + path
	if body != nil {
		data, _ := json.Marshal(body)
		line += " " + string(data)
	}
	l.sent = append(l.sent, line)
	text, ok := l.answers[method+" "+path]
	if !ok {
		text = "{}"
	}
	return decode(text, out)
}

func screen(tabs ...spec.ScreenTab) spec.Screen {
	return spec.Screen{Meta: spec.Meta{ID: "10100"}, Name: "VULN Screen", Tabs: tabs}
}

func tab(name string, fields ...string) spec.ScreenTab {
	return spec.ScreenTab{Name: name, Fields: fields}
}

func TestScreenUpdates(t *testing.T) {
	r, env := testSite(t)
	x := resolver{r, env.Spec}
	tabs := `[{"id":1,"name":"A"},{"id":2,"name":"B"}]`
	for name, tc := range map[string]struct {
		want, got spec.Screen
		class     string
		requests  []string
		sent      []string
	}{
		"a field added": {screen(tab("A", "Summary", "Description")), screen(tab("A", "Summary")), "additive",
			[]string{"POST /rest/api/3/screens/{screenId}/tabs/{tabId}/fields"},
			[]string{"GET /rest/api/3/screens/10100/tabs", `POST /rest/api/3/screens/10100/tabs/1/fields {"fieldId":"description"}`}},
		"a field moved to a new tab, a tab removed, both reordered": {
			screen(tab("C", "Description"), tab("A", "Summary")), screen(tab("A", "Description", "Summary"), tab("B")), "altering",
			[]string{"DELETE /rest/api/3/screens/{screenId}/tabs/{tabId}/fields/{id}", "POST /rest/api/3/screens/{screenId}/tabs",
				"DELETE /rest/api/3/screens/{screenId}/tabs/{tabId}", "POST /rest/api/3/screens/{screenId}/tabs/{tabId}/fields",
				"POST /rest/api/3/screens/{screenId}/tabs/{tabId}/move/{pos}"},
			[]string{"GET /rest/api/3/screens/10100/tabs",
				"DELETE /rest/api/3/screens/10100/tabs/1/fields/description",
				`POST /rest/api/3/screens/10100/tabs {"name":"C"}`,
				"DELETE /rest/api/3/screens/10100/tabs/2",
				`POST /rest/api/3/screens/10100/tabs/3/fields {"fieldId":"description"}`,
				"POST /rest/api/3/screens/10100/tabs/3/move/0"}},
		"fields reordered": {screen(tab("A", "Description", "Summary")), screen(tab("A", "Summary", "Description")), "altering",
			[]string{"POST /rest/api/3/screens/{screenId}/tabs/{tabId}/fields/{id}/move"},
			[]string{"GET /rest/api/3/screens/10100/tabs", `POST /rest/api/3/screens/10100/tabs/1/fields/description/move {"position":"First"}`}},
		"a description added": {spec.Screen{Meta: spec.Meta{ID: "10100"}, Description: spec.Some("Triage")}, screen(), "additive",
			[]string{"PUT /rest/api/3/screens/{screenId} (description)"},
			[]string{`PUT /rest/api/3/screens/10100 {"description":"Triage [managed-by: jira-space-config]"}`}},
	} {
		t.Run(name, func(t *testing.T) {
			ch := updateScreen(x, tc.want, tc.got)
			if ch.class != tc.class || strings.Join(ch.requests, "\n") != strings.Join(tc.requests, "\n") {
				t.Errorf("class %s, requests:\n%s", ch.class, strings.Join(ch.requests, "\n"))
			}
			l := &log{answers: map[string]string{"GET /rest/api/3/screens/10100/tabs": tabs,
				"POST /rest/api/3/screens/10100/tabs": `{"id":3,"name":"C"}`}}
			if _, err := ch.do(context.Background(), l); err != nil {
				t.Fatal(err)
			}
			if strings.Join(l.sent, "\n") != strings.Join(tc.sent, "\n") {
				t.Errorf("sent:\n%s", strings.Join(l.sent, "\n"))
			}
		})
	}
}

func TestScreenChangedSinceEvaluated(t *testing.T) {
	r, env := testSite(t)
	ch := updateScreen(resolver{r, env.Spec}, screen(tab("A", "Summary", "Description")), screen(tab("A", "Summary")))
	_, err := ch.do(context.Background(), &log{answers: map[string]string{"GET /rest/api/3/screens/10100/tabs": `[]`}})
	if err == nil || !strings.Contains(err.Error(), `the screen has no tab "A" now`) {
		t.Errorf("err = %v", err)
	}
}

func TestWorkflowUpdates(t *testing.T) {
	r, env := testSite(t)
	x := resolver{r, env.Spec}
	create := spec.Transition{Name: "Create", Type: spec.TransitionInitial, To: "Open"}
	got := spec.Workflow{Meta: spec.Meta{ID: "wf-1"}, Name: "VULN Workflow",
		Statuses:    []spec.WorkflowStatus{{Name: "Open"}, {Name: "Done"}},
		Transitions: []spec.Transition{{ID: "1", Name: "Create", Type: spec.TransitionInitial, To: "Open"}, {ID: "21", Name: "Finish", Type: spec.TransitionGlobal, To: "Done"}}}
	answers := map[string]string{"POST /rest/api/3/workflows": `{"workflows":[{"id":"wf-1","version":{"id":"v-1","versionNumber":3}}]}`}

	t.Run("a transition added keeps the others' IDs", func(t *testing.T) {
		want := got
		want.Transitions = []spec.Transition{create, {Name: "Finish", Type: spec.TransitionGlobal, To: "Done"},
			{Name: "Restart", Type: spec.TransitionGlobal, To: "Open"}}
		ch := updateWorkflow(env, x, want, got)
		if ch.class != "additive" || len(ch.removes) != 0 || len(ch.safety) != 0 {
			t.Errorf("class %s, removes %v, safety %v", ch.class, ch.removes, ch.safety)
		}
		l := &log{answers: answers}
		if _, err := ch.do(context.Background(), l); err != nil {
			t.Fatal(err)
		}
		update := l.sent[len(l.sent)-1]
		for _, want := range []string{`"id":"wf-1"`, `"version":{"id":"v-1","versionNumber":3}`, `"id":"1","name":"Create"`, `"id":"21",`, `"id":"11",`} {
			if !strings.Contains(update, want) {
				t.Errorf("update lacks %s:\n%s", want, update)
			}
		}
		if strings.Contains(update, `"name":"VULN Workflow"`) || !strings.HasPrefix(update, "POST /rest/api/3/workflows/update ") ||
			!strings.HasPrefix(l.sent[1], "POST /rest/api/3/workflows/update/validation ") {
			t.Errorf("sent:\n%s", strings.Join(l.sent, "\n"))
		}
	})

	t.Run("a status removed is counted, mapped, and still listed", func(t *testing.T) {
		env.WorkItems = 3
		env.Spec.Space.IssueTypes = []spec.SpaceIssueType{{Name: "Story", Workflow: spec.Some("VULN Workflow")}}
		defer func() { env.WorkItems, env.Spec.Space.IssueTypes = 0, nil }()
		want := got
		want.Statuses = []spec.WorkflowStatus{{Name: "Open"}}
		want.Transitions = []spec.Transition{create}
		ch := updateWorkflow(env, x, want, got)
		if ch.class != "removing" || strings.Join(ch.removes, "; ") != `status "Done"; transition "Finish"` || ch.allow != "workflows" {
			t.Errorf("class %s, removes %v", ch.class, ch.removes)
		}
		if len(ch.safety) != 1 || ch.safety[0].jql != `project = 10042 AND issuetype in ("Story") AND status = "Done"` {
			t.Errorf("safety %+v", ch.safety)
		}
		l := &log{answers: answers}
		if _, err := ch.do(context.Background(), l); err != nil {
			t.Fatal(err)
		}
		if update := l.sent[len(l.sent)-1]; !strings.Contains(update, `"statusMappings":[{"issueTypeId":"10005","projectId":"10042","statusMigrations":[{"newStatusReference":"1","oldStatusReference":"10001"}]}]`) ||
			!strings.Contains(update, `{"description":"","id":"10001","name":"Done","statusCategory":"DONE","statusReference":"10001"}],"workflows"`) {
			t.Errorf("update:\n%s", update)
		}
	})

	t.Run("a transition changed is altering", func(t *testing.T) {
		want := got
		want.Transitions = []spec.Transition{create, {Name: "Finish", Type: spec.TransitionGlobal, To: "Open"}}
		want.Statuses = got.Statuses
		if ch := updateWorkflow(env, x, want, got); ch.class != "altering" {
			t.Errorf("class %s", ch.class)
		}
	})

	t.Run("the workflow is gone when the change is made", func(t *testing.T) {
		ch := updateWorkflow(env, x, got, got)
		_, err := ch.do(context.Background(), &log{answers: map[string]string{"POST /rest/api/3/workflows": `{"workflows":[]}`}})
		if err == nil || !strings.Contains(err.Error(), "workflow wf-1 was not returned") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestWorkflowUpdateUnknownIssueType(t *testing.T) {
	r, env := testSite(t)
	env.Spec.Space.IssueTypes = []spec.SpaceIssueType{{Name: "Nothing", Workflow: spec.Some("W")}}
	got := spec.Workflow{Meta: spec.Meta{ID: "wf-1"}, Name: "W", Statuses: []spec.WorkflowStatus{{Name: "Open"}, {Name: "Done"}},
		Transitions: []spec.Transition{{ID: "1", Name: "Create", Type: spec.TransitionInitial, To: "Open"}}}
	want := got
	want.Statuses = got.Statuses[:1]
	ch := updateWorkflow(env, resolver{r, env.Spec}, want, got)
	_, err := ch.do(context.Background(), &log{answers: map[string]string{"POST /rest/api/3/workflows": `{"workflows":[{"id":"wf-1"}]}`}})
	if err == nil || !strings.Contains(err.Error(), `no issue type on the site is named "Nothing"`) {
		t.Errorf("err = %v", err)
	}
}
