package steps

import (
	"context"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/reader"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

func options(vs ...string) []spec.Option {
	var out []spec.Option
	for _, v := range vs {
		out = append(out, spec.Option{Value: v})
	}
	return out
}

func contextSet(scope string, def spec.Opt[spec.Value], opts ...spec.Option) spec.FieldContextSet {
	return spec.FieldContextSet{Field: "Severity", Contexts: []spec.FieldContext{{Options: opts, Default: def,
		Binding: &spec.ContainerRef{ID: "c1", Scope: scope}}}}
}

func TestContextChanges(t *testing.T) {
	_, env := testSite(t)
	cf := spec.CustomField{GlobalObject: spec.GlobalObject{Name: "Severity", Meta: spec.Meta{ID: "customfield_10051"}}, Type: "select"}
	env.Spec.CustomFields = append(env.Spec.CustomFields, cf)
	none := spec.Opt[spec.Value]{}
	page := `{"isLast":true,"values":[{"id":"1","value":"Low"},{"id":"2","value":"High"},{"id":"3","value":"Old"}]}`
	optPath := "/rest/api/3/field/customfield_10051/context/c1/option"

	t.Run("added, removed, disabled, reordered and a default", func(t *testing.T) {
		got := contextSet(spec.ScopeSpace, none, options("Low", "High", "Old")...)
		want := contextSet(spec.ScopeSpace, spec.Some(spec.Value{Str: "High"}), append([]spec.Option{{Value: "High", Disabled: true}},
			options("Low", "Critical")...)...)
		ch, ok := contextChange(env, cf, want, got)
		if !ok || ch.class != "removing" || strings.Join(ch.removes, ",") != `option "Old"` || ch.allow != "contexts" {
			t.Fatalf("ok %v, class %s, removes %v", ok, ch.class, ch.removes)
		}
		if len(ch.safety) != 1 || ch.safety[0].jql != `project = 10042 AND cf[10051] = "Old"` {
			t.Errorf("safety %+v", ch.safety)
		}
		l := &log{answers: map[string]string{"GET " + optPath: page, "POST " + optPath: `{"options":[{"id":"4","value":"Critical"}]}`}}
		if _, err := ch.do(context.Background(), l); err != nil {
			t.Fatal(err)
		}
		want2 := []string{
			"GET " + optPath,
			`POST ` + optPath + ` {"options":[{"value":"Critical","disabled":false}]}`,
			`PUT ` + optPath + ` {"options":[{"disabled":true,"id":"2","value":"High"}]}`,
			"DELETE " + optPath + "/3",
			`PUT ` + optPath + `/move {"customFieldOptionIds":["2","1","4"],"position":"First"}`,
			`PUT /rest/api/3/field/customfield_10051/context/defaultValue {"defaultValues":[{"contextId":"c1","optionId":"2","type":"option.single"}]}`,
		}
		if strings.Join(l.sent, "\n") != strings.Join(want2, "\n") {
			t.Errorf("sent:\n%s", strings.Join(l.sent, "\n"))
		}
	})
	t.Run("only added", func(t *testing.T) {
		ch, ok := contextChange(env, cf, contextSet(spec.ScopeSpace, none, options("Low", "High", "Old", "New")...),
			contextSet(spec.ScopeSpace, none, options("Low", "High", "Old")...))
		if !ok || ch.class != "additive" || strings.Join(ch.requests, ";") != "POST /rest/api/3/field/{fieldId}/context/{contextId}/option" {
			t.Errorf("ok %v, class %s, requests %v", ok, ch.class, ch.requests)
		}
	})
	t.Run("a cascading child removed is counted by cascadeOption", func(t *testing.T) {
		parent := func(children ...string) spec.Option {
			return spec.Option{Value: "Host", Children: options(children...)}
		}
		ch, _ := contextChange(env, cf, contextSet(spec.ScopeSpace, none, parent("Linux")), contextSet(spec.ScopeSpace, none, parent("Linux", "Mac")))
		if len(ch.safety) != 1 || ch.safety[0].jql != `project = 10042 AND cf[10051] in cascadeOption("Host", "Mac")` {
			t.Errorf("safety %+v", ch.safety)
		}
	})
	t.Run("a shared context is never changed", func(t *testing.T) {
		if _, ok := contextChange(env, cf, contextSet(spec.ScopeShared, none, options("Low")...), contextSet(spec.ScopeShared, none, options("High")...)); ok {
			t.Error("a shared context would be changed")
		}
	})
	t.Run("a default the spec does not give is taken away", func(t *testing.T) {
		ch, _ := contextChange(env, cf, contextSet(spec.ScopeSpace, none, options("Low")...),
			contextSet(spec.ScopeSpace, spec.Some(spec.Value{Str: "Low"}), options("Low")...))
		l := &log{answers: map[string]string{"GET " + optPath: `{"isLast":true,"values":[{"id":"1","value":"Low"}]}`}}
		if _, err := ch.do(context.Background(), l); ch.class != "altering" || err != nil ||
			l.sent[len(l.sent)-1] != `PUT /rest/api/3/field/customfield_10051/context/defaultValue {"defaultValues":[{"contextId":"c1","type":"option.single"}]}` {
			t.Errorf("class %s, err %v, sent:\n%s", ch.class, err, strings.Join(l.sent, "\n"))
		}
	})
	t.Run("an option gone when the change is made", func(t *testing.T) {
		ch, _ := contextChange(env, cf, contextSet(spec.ScopeSpace, none, options("Low")...), contextSet(spec.ScopeSpace, none, options("Low", "Old")...))
		_, err := ch.do(context.Background(), &log{answers: map[string]string{"GET " + optPath: `{"isLast":true,"values":[{"id":"1","value":"Low"}]}`}})
		if err == nil || !strings.Contains(err.Error(), `the context has no option "Old" now`) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestContextIssueTypes(t *testing.T) {
	_, env := testSite(t)
	cf := spec.CustomField{GlobalObject: spec.GlobalObject{Name: "Severity", Meta: spec.Meta{ID: "customfield_10051"}}, Type: "date"}
	env.Spec.CustomFields = append(env.Spec.CustomFields, cf)
	ctx := func(id string, types ...string) spec.FieldContext {
		return spec.FieldContext{IssueTypes: types, Binding: &spec.ContainerRef{ID: id, Scope: spec.ScopeSpace}}
	}
	set := func(cs ...spec.FieldContext) spec.FieldContextSet {
		return spec.FieldContextSet{Field: "Severity", Contexts: cs}
	}
	env.WorkItems = 3
	defer func() { env.WorkItems = 0 }()

	t.Run("issue types added and taken away, matched by ID", func(t *testing.T) {
		ch, ok := contextChange(env, cf, set(ctx("c1", "Task", "Story")), set(ctx("c1", "Finding", "Story")))
		if !ok || ch.class != "removing" || strings.Join(ch.removes, ";") != `issue type "Finding" from a context` {
			t.Fatalf("ok %v, class %s, removes %v", ok, ch.class, ch.removes)
		}
		var jql []string
		for _, s := range ch.safety {
			jql = append(jql, s.jql)
		}
		if strings.Join(jql, "\n") != "project = 10042 AND issuetype = \"Task\" AND cf[10051] is not EMPTY\nproject = 10042 AND issuetype = \"Finding\" AND cf[10051] is not EMPTY" {
			t.Errorf("safety:\n%s", strings.Join(jql, "\n"))
		}
		ch, _ = contextChange(env, cf, set(ctx("c1", "Task", "Story")), set(ctx("c1", "Story")))
		l := &log{}
		if _, err := ch.do(context.Background(), l); err != nil || strings.Join(l.sent, "") !=
			`PUT /rest/api/3/field/customfield_10051/context/c1/issuetype {"issueTypeIds":["10014"]}` {
			t.Errorf("err %v, sent %v", err, l.sent)
		}
	})
	t.Run("the same issue types in another order", func(t *testing.T) {
		if ch, _ := contextChange(env, cf, set(ctx("c1", "Task", "Story")), set(ctx("c1", "Story", "Task"))); len(ch.requests) != 0 {
			t.Errorf("requests %v", ch.requests)
		}
	})
	t.Run("a context the spec leaves out is deleted, first", func(t *testing.T) {
		ch, _ := contextChange(env, cf, set(ctx("", "Task")), set(ctx("c2", "Story")))
		if strings.Join(ch.removes, ";") != "the context for Story" || len(ch.safety) != 1 {
			t.Errorf("removes %v, safety %v", ch.removes, ch.safety)
		}
		l := &log{}
		_, _ = ch.do(context.Background(), l)
		if len(l.sent) == 0 || l.sent[0] != "DELETE /rest/api/3/field/customfield_10051/context/c2" {
			t.Errorf("sent %v", l.sent)
		}
	})
	t.Run("from all issue types to some, and back", func(t *testing.T) {
		ch, ok := contextChange(env, cf, set(ctx("c1", "Task")), set(ctx("c1")))
		l := &log{}
		if _, err := ch.do(context.Background(), l); !ok || err != nil || ch.class != "altering" || len(ch.removes) != 0 ||
			strings.Join(l.sent, "") != `PUT /rest/api/3/field/customfield_10051/context/c1/issuetype {"issueTypeIds":["10014"]}` {
			t.Errorf("to some: ok %v, err %v, class %s, sent %v", ok, err, ch.class, l.sent)
		}
		ch, ok = contextChange(env, cf, set(ctx("c1")), set(ctx("c1", "Task", "Story")))
		l = &log{}
		if _, err := ch.do(context.Background(), l); !ok || err != nil || ch.class != "removing" || len(ch.removes) != 1 ||
			strings.Join(l.sent, "") != `POST /rest/api/3/field/customfield_10051/context/c1/issuetype/remove {"issueTypeIds":["10014","10005"]}` {
			t.Errorf("to all: ok %v, err %v, class %s, sent %v", ok, err, ch.class, l.sent)
		}
	})
	t.Run("a shared context left out", func(t *testing.T) {
		shared := ctx("c3", "Story")
		shared.Binding.Scope = spec.ScopeShared
		if _, ok := contextChange(env, cf, set(), set(shared)); ok {
			t.Error("a shared context would be deleted")
		}
	})
}

// TestSharedContextsTheSpaceDoesNotUse: a shared context the spec leaves out
// is not a difference when the spec's contexts cover all the space's issue
// types, but is when some issue type would still use it.
func TestSharedContextsTheSpaceDoesNotUse(t *testing.T) {
	_, env := testSite(t)
	env.Spec.Space.IssueTypes = []spec.SpaceIssueType{{Name: "Vulnerability"}}
	shared := spec.FieldContext{Binding: &spec.ContainerRef{ID: "g", Scope: spec.ScopeShared}}
	own := spec.FieldContext{IssueTypes: []string{"Vulnerability"}, Binding: &spec.ContainerRef{ID: "c", Scope: spec.ScopeSpace}}
	live := spec.FieldContextSet{Field: "Impact", Contexts: []spec.FieldContext{shared}}
	want := spec.FieldContextSet{Field: "Impact", Contexts: []spec.FieldContext{{IssueTypes: []string{"Vulnerability"}}}}

	if got := withoutUnusedShared(env, want, live); len(got.Contexts) != 0 {
		t.Errorf("covered: kept %v", got.Contexts)
	}
	env.Spec.Space.IssueTypes = append(env.Spec.Space.IssueTypes, spec.SpaceIssueType{Name: "Task"})
	if got := withoutUnusedShared(env, want, live); len(got.Contexts) != 1 {
		t.Errorf("Task is not covered: kept %v", got.Contexts)
	}
	env.Spec.Space.IssueTypes = nil
	if got := withoutUnusedShared(env, want, live); len(got.Contexts) != 1 {
		t.Errorf("no issue types listed: kept %v", got.Contexts)
	}
	env.Spec.Space.IssueTypes = []spec.SpaceIssueType{{Name: "Vulnerability"}}
	both := spec.FieldContextSet{Field: "Impact", Contexts: []spec.FieldContext{shared, own}}
	mentioned := spec.FieldContextSet{Field: "Impact", Contexts: []spec.FieldContext{{IssueTypes: nil, Binding: shared.Binding}, {IssueTypes: []string{"Vulnerability"}}}}
	if got := withoutUnusedShared(env, mentioned, both); len(got.Contexts) != 2 {
		t.Errorf("a shared context the spec names is kept: %v", got.Contexts)
	}
}

func TestCoveringAllIssueTypes(t *testing.T) {
	its := (&reader.IssueTypes{}).With([]spec.SpaceIssueType{{Name: "A"}, {Name: "B"}}, []string{"1", "2"})
	set := spec.FieldContextSet{Field: "F", Contexts: []spec.FieldContext{{IssueTypes: []string{"A", "B"}}, {IssueTypes: []string{"A"}}}}
	got := coveringAll(set, its)
	if got.Contexts[0].IssueTypes != nil || len(got.Contexts[1].IssueTypes) != 1 {
		t.Errorf("a context for every issue type is for all; one for some stays: %+v", got.Contexts)
	}
	if set.Contexts[0].IssueTypes == nil {
		t.Error("the spec's own contexts were changed")
	}
}
