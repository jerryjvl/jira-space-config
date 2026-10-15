package steps

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/jira/jiratest"
	"github.com/jerryjvl/jira-space-config/internal/reader"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

func TestNotificationSchemeUpdates(t *testing.T) {
	r, env := testSite(t)
	x := resolver{r, env.Spec}
	created := func(h ...string) []spec.Grant { return []spec.Grant{{Permission: "Issue Created", Holders: h}} }
	scheme := `{"notificationSchemeEvents":[{"event":{"id":1},"notifications":[{"id":5,"notificationType":"Reporter"}]}]}`
	answers := map[string]string{"GET /rest/api/3/notificationscheme/40": scheme}

	ch := updateNotificationScheme(env, x, "40", created("all_watchers"), created("reporter"))
	if ch.class != "removing" || strings.Join(ch.widens, ";") != "all_watchers for Issue Created" ||
		strings.Join(ch.removes, ";") != "recipient reporter from Issue Created" || ch.widenAllow != "notifications" {
		t.Errorf("class %s, widens %v, removes %v", ch.class, ch.widens, ch.removes)
	}
	l := &log{answers: answers}
	if _, err := ch.do(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /rest/api/3/notificationscheme/40",
		`PUT /rest/api/3/notificationscheme/40/notification {"notificationSchemeEvents":[{"event":{"id":"1"},"notifications":[{"notificationType":"AllWatchers"}]}]}`,
		"DELETE /rest/api/3/notificationscheme/40/notification/5"}
	if strings.Join(l.sent, "\n") != strings.Join(want, "\n") {
		t.Errorf("sent:\n%s", strings.Join(l.sent, "\n"))
	}

	for name, tc := range map[string]struct {
		want, got []spec.Grant
		answers   map[string]string
		err       string
	}{
		"an unknown event":       {[]spec.Grant{{Permission: "Gone", Holders: []string{"reporter"}}}, nil, answers, `no event on the site is named "Gone"`},
		"a recipient gone since": {nil, created("current_assignee"), answers, "the scheme has no Issue Created recipient current_assignee now"},
	} {
		_, err := updateNotificationScheme(env, x, "40", tc.want, tc.got).do(context.Background(), &log{answers: tc.answers})
		if err == nil || !strings.Contains(err.Error(), tc.err) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// replies is a Sender that answers each "METHOD path" from a function, so
// it can fail a request as Jira does.
type replies func(method, path string, out any) error

func (f replies) Send(_ context.Context, method, path string, _ url.Values, _, out any) error {
	return f(method, path, out)
}

func TestWorkflowSchemeUpdates(t *testing.T) {
	r, env := testSite(t)
	x := resolver{r, env.Spec}
	schemeWait = 0
	read := `[{"id":"50","name":"VULN Workflow Scheme","description":"d","version":{"id":"v","versionNumber":1},` +
		`"defaultWorkflow":{"id":"wf-0"},"workflowsForIssueTypes":[]}]`
	ok := func(task string) replies {
		reads := 0
		return func(method, path string, out any) error {
			switch {
			case path == "/rest/api/3/workflowscheme/read" && reads == 0:
				reads++
				return decode(read, out)
			case path == "/rest/api/3/workflowscheme/read":
				return decode(`[{"taskId":"`+task+`"}]`, out)
			case path == "/rest/api/3/workflowscheme/update":
				return &jira.APIError{Status: http.StatusSeeOther}
			}
			return nil
		}
	}
	want := []line{{"Story", "VULN Workflow"}}
	if _, err := updateWorkflowScheme(env, x, "50", want, nil).do(context.Background(), ok("")); err != nil {
		t.Errorf("a task Jira started and finished: %v", err)
	}
	if _, err := updateWorkflowScheme(env, x, "50", want, nil).do(context.Background(), ok("t-1")); err == nil ||
		!strings.Contains(err.Error(), "did not finish in time") {
		t.Errorf("a task that does not finish: %v", err)
	}
	for name, tc := range map[string]struct {
		want, current []line
		send          Sender
		err           string
	}{
		"the scheme is gone":              {want, nil, &log{answers: map[string]string{"POST /rest/api/3/workflowscheme/read": `[]`}}, "workflow scheme 50 was not returned"},
		"an unknown issue type":           {[]line{{"Nothing", "VULN Workflow"}}, nil, ok(""), `no issue type on the site is named "Nothing"`},
		"an unknown workflow":             {[]line{{"Story", "Nowhere"}}, nil, ok(""), `no workflow on the site is named "Nowhere"`},
		"a workflow that is not returned": {want, []line{{"Story", "Gone"}}, ok(""), "asked Jira for 2 workflows (Gone, VULN Workflow), got 1"},
	} {
		_, err := updateWorkflowScheme(env, x, "50", tc.want, tc.current).do(context.Background(), tc.send)
		if err == nil || !strings.Contains(err.Error(), tc.err) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestChangesThatNeedAnOverride: a change that widens access or removes
// is made only with the override for its kind.
func TestChangesThatNeedAnOverride(t *testing.T) {
	_, env := testSite(t)
	items, _ := changeItems(env, Item{Name: "Notification scheme"}, change{widens: []string{"reporter for Issue Created"}, widenAllow: "notifications"},
		"the space's, \"N\", exists", "notification scheme \"N\"")
	if items[0].Kind != Error || !strings.Contains(items[0].Fix, "--allow-update=notifications") {
		t.Errorf("widening: %+v", items[0])
	}
	env.AllowUpdate = []string{"notifications"}
	if items, _ := changeItems(env, Item{}, change{widens: []string{"x"}, widenAllow: "notifications"}, "it", "it"); items[0].Kind != Update {
		t.Errorf("allowed: %+v", items[0])
	}
}

// TestManagedContainerWithoutAnUpdate: without an update, a managed
// container that differs is reported as different.
func TestManagedContainerWithoutAnUpdate(t *testing.T) {
	_, env := testSite(t)
	items, err := evalContainerItems(env, container{noun: "field configuration scheme", title: "Field configuration scheme",
		want: []line{{"Summary", "required"}}, current: found{ID: "1", Name: "VULN FCS", Managed: true, Lines: []line{{"Summary", "optional"}}}})
	if err != nil || items[0].Kind != Differs || !strings.Contains(items[0].Fix, "this tool does not change an existing field configuration scheme") {
		t.Errorf("%+v, %v", items, err)
	}
}

// TestRoleTypesThatShareAName: the spec's ID says which role type a name
// means; without one, the name is refused.
func TestRoleTypesThatShareAName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "role.json"), []byte(`[{"id":10003,"name":"addons"},{"id":10013,"name":"addons"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := jiratest.New(t, dir, []jiratest.Route{{Path: "/rest/api/3/role", File: "role.json"}})
	r := reader.New(context.Background(), srv.Client())
	withID := &spec.Spec{Roles: []spec.Role{{GlobalObject: spec.GlobalObject{Name: "addons", Meta: spec.Meta{ID: "10003"}}}}}
	if id, err := (resolver{r, withID}).role("addons"); err != nil || id != "10003" {
		t.Errorf("with the spec's id: %q, %v", id, err)
	}
	if _, err := (resolver{r, &spec.Spec{}}).role("addons"); err == nil || !strings.Contains(err.Error(), `2 role types on the site are named "addons"; give its id in roles`) {
		t.Errorf("without: %v", err)
	}
}

func TestPermissionSchemeUpdates(t *testing.T) {
	r, env := testSite(t)
	x := resolver{r, env.Spec}
	grants := func(h ...string) []spec.Grant { return []spec.Grant{{Permission: "BROWSE_PROJECTS", Holders: h}} }
	if ch := updatePermissionScheme(env, x, "60", grants("reporter"), nil); !strings.Contains(ch.problem, "which other spaces use it is not known") {
		t.Errorf("unchecked: %q", ch.problem)
	}
	env.CheckUsage = []string{"permission_scheme"}
	if ch := updatePermissionScheme(env, x, "60", grants("reporter"), nil); !strings.Contains(ch.problem, "cannot see every space") {
		t.Errorf("without Administer Jira: %q", ch.problem)
	}
	env.AdministerJira = true
	ch := updatePermissionScheme(env, x, "60", grants("role:Administrators"), grants("reporter"))
	current := `{"permissions":[{"id":7,"permission":"BROWSE_PROJECTS","holder":{"type":"reporter"}}]}`
	l := &log{answers: map[string]string{"GET /rest/api/3/permissionscheme/60/permission": current}}
	if _, err := ch.do(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /rest/api/3/permissionscheme/60/permission",
		`POST /rest/api/3/permissionscheme/60/permission {"holder":{"type":"projectRole","value":"10002"},"permission":"BROWSE_PROJECTS"}`,
		"DELETE /rest/api/3/permissionscheme/60/permission/7"}
	if strings.Join(l.sent, "\n") != strings.Join(want, "\n") {
		t.Errorf("sent:\n%s", strings.Join(l.sent, "\n"))
	}
	_, err := updatePermissionScheme(env, x, "60", nil, grants("reporter")).do(context.Background(),
		&log{answers: map[string]string{"GET /rest/api/3/permissionscheme/60/permission": `{"permissions":[]}`}})
	if err == nil || !strings.Contains(err.Error(), "the scheme no longer grants BROWSE_PROJECTS to reporter") {
		t.Errorf("a grant gone since: %v", err)
	}
}

func TestSecuritySchemeUpdates(t *testing.T) {
	r, env := testSite(t)
	x := resolver{r, env.Spec}
	schemeWait = 0
	level := func(name string, def bool, h ...string) spec.SecurityLevel {
		return spec.SecurityLevel{Name: name, Default: def, Holders: h}
	}
	got := []spec.SecurityLevel{level("Internal", true, "reporter"), level("Old", false, "reporter")}
	want := []spec.SecurityLevel{level("Internal", false, "current_assignee"), level("Partners", true, "reporter")}
	want[0].Description = spec.Some("Staff only")
	levels := `{"isLast":true,"values":[{"id":"1","name":"Internal"},{"id":"2","name":"Old"},{"id":"3","name":"Partners"}]}`
	members := `{"isLast":true,"values":[{"id":"9","holder":{"type":"reporter"}}]}`
	deleted := 0
	send := replies(func(method, path string, out any) error {
		switch method + " " + path {
		case "GET /rest/api/3/issuesecurityschemes/level":
			if deleted > 0 {
				return decode(`{"isLast":true,"values":[{"id":"1","name":"Internal"},{"id":"3","name":"Partners"}]}`, out)
			}
			return decode(levels, out)
		case "GET /rest/api/3/issuesecurityschemes/level/member":
			return decode(members, out)
		case "DELETE /rest/api/3/issuesecurityschemes/70/level/2":
			deleted++
			return &jira.APIError{Status: http.StatusSeeOther}
		}
		return nil
	})
	ch := updateSecurityScheme(env, x, "70", want, got)
	if ch.class != "removing" || len(ch.widens) != 1 || len(ch.removes) != 2 {
		t.Errorf("class %s, widens %v, removes %v", ch.class, ch.widens, ch.removes)
	}
	if _, err := ch.do(context.Background(), send); err != nil {
		t.Errorf("err = %v", err)
	}
	if ch := updateSecurityScheme(env, x, "70", []spec.SecurityLevel{level("L", false, "space_lead")}, nil); !strings.Contains(ch.problem, "Jira does not accept in a security level") {
		t.Errorf("space lead: %q", ch.problem)
	}
	for name, tc := range map[string]struct {
		want, got []spec.SecurityLevel
		answers   map[string]string
		err       string
	}{
		"a level gone since": {[]spec.SecurityLevel{level("Internal", false, "current_assignee")}, []spec.SecurityLevel{level("Internal", false)},
			map[string]string{"GET /rest/api/3/issuesecurityschemes/level": `{"isLast":true,"values":[]}`}, `the scheme has no level "Internal" now`},
		"a member gone since": {[]spec.SecurityLevel{level("Internal", false)}, []spec.SecurityLevel{level("Internal", false, "reporter")},
			map[string]string{"GET /rest/api/3/issuesecurityschemes/level": levels, "GET /rest/api/3/issuesecurityschemes/level/member": `{"isLast":true,"values":[]}`},
			`level "Internal" no longer has member reporter`},
		"a level that is not deleted": {nil, []spec.SecurityLevel{level("Old", false)},
			map[string]string{"GET /rest/api/3/issuesecurityschemes/level": levels}, `deleting level "Old" did not finish in time`},
	} {
		_, err := updateSecurityScheme(env, x, "70", tc.want, tc.got).do(context.Background(), &log{answers: tc.answers})
		if err == nil || !strings.Contains(err.Error(), tc.err) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestSecurityMemberTypeIsMatchedIgnoringCase: apply writes a role member as
// "projectrole" and Jira reads it back as "projectRole"; removing it must
// still find it.
func TestSecurityMemberTypeIsMatchedIgnoringCase(t *testing.T) {
	r, env := testSite(t)
	level := func(h ...string) []spec.SecurityLevel { return []spec.SecurityLevel{{Name: "Internal", Holders: h}} }
	l := &log{answers: map[string]string{
		"GET /rest/api/3/issuesecurityschemes/level":        `{"isLast":true,"values":[{"id":"1","name":"Internal"}]}`,
		"GET /rest/api/3/issuesecurityschemes/level/member": `{"isLast":true,"values":[{"id":"9","holder":{"type":"projectRole","parameter":"10002","value":"10002"}}]}`,
	}}
	ch := updateSecurityScheme(env, resolver{r, env.Spec}, "70", level(), level("role:Administrators"))
	if _, err := ch.do(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	if last := l.sent[len(l.sent)-1]; last != "DELETE /rest/api/3/issuesecurityschemes/70/level/1/member/9" {
		t.Errorf("sent:\n%s", strings.Join(l.sent, "\n"))
	}
}

// lateGrants answers the second read of a scheme with a grant the first did
// not show, as Jira does for the guest role's.
type lateGrants struct {
	log
	reads int
}

func (l *lateGrants) Send(ctx context.Context, method, path string, q url.Values, body, out any) error {
	if method == http.MethodGet {
		l.reads++
		l.answers["GET "+path] = `{"permissions":[{"id":1,"permission":"BROWSE_PROJECTS","holder":{"type":"group","value":"g"}}]}`
		if l.reads > 1 {
			l.answers["GET "+path] = `{"permissions":[{"id":1,"permission":"BROWSE_PROJECTS","holder":{"type":"group","value":"g"}},` +
				`{"id":2,"permission":"BROWSE_PROJECTS","holder":{"type":"projectRole","value":"9"}}]}`
		}
	}
	return l.log.Send(ctx, method, path, q, body, out)
}

func TestRemoveOtherGrantsFindsLateOnes(t *testing.T) {
	settle := grantSettle
	grantSettle = 0
	t.Cleanup(func() { grantSettle = settle })
	l := &lateGrants{log: log{answers: map[string]string{}}}
	asked := []map[string]any{{"permission": "BROWSE_PROJECTS", "holder": map[string]string{"type": "group", "value": "g"}}}
	if err := removeOtherGrants(context.Background(), l, "5", asked); err != nil {
		t.Fatal(err)
	}
	var deleted []string
	for _, s := range l.sent {
		if strings.HasPrefix(s, "DELETE") {
			deleted = append(deleted, s)
		}
	}
	if len(deleted) != 1 || !strings.HasSuffix(deleted[0], "/permission/2") {
		t.Errorf("deleted %v", deleted)
	}
}

func TestExpandLoops(t *testing.T) {
	ts := []spec.Transition{{ID: "5", Name: "Go", Type: "directed", From: []string{"A"}, To: "B"},
		{ID: "6", Name: "Again", Type: "directed", From: []string{"A", "B"}, Looped: true}}
	got := expandLoops(ts)
	if len(got) != 3 || got[1].To != "A" || got[2].To != "B" || got[1].Looped || got[1].ID != "6" || got[2].ID != "" {
		t.Errorf("expanded: %+v", got)
	}
	if !ts[1].Looped || len(ts[1].From) != 2 {
		t.Error("the spec's own transitions were changed")
	}
}
