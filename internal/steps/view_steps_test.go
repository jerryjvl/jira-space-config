package steps

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

func TestFilterBody(t *testing.T) {
	r, env := testSite(t)
	f := spec.Filter{Name: "VULN Open", JQL: "project = VULN", Viewers: []string{"space", "role:Administrators", "logged_in"},
		Editors: []string{"role:Administrators", "group:team-alpha", "user:acc-1", "anyone"}}
	body, err := filterBody(env, resolver{r, env.Spec}, f)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(body)
	for _, want := range []string{
		`"sharePermissions":[{"project":{"id":"10042"},"type":"project"},{"type":"loggedin"}]`,
		`{"project":{"id":"10042"},"role":{"id":10002},"type":"projectRole"}`,
		`{"group":{"groupId":"g-1"},"type":"group"}`, `{"type":"user","user":{"accountId":"acc-1"}}`, `{"type":"global"}`,
		`"description":"[managed-by: jira-space-config]"`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("body lacks %s:\n%s", want, got)
		}
	}
	// An editor need not be listed as a viewer: Jira keeps no view share an edit share implies.
	if d := compareFilters(f, spec.Filter{JQL: f.JQL, Viewers: []string{"space", "logged_in"}, Editors: f.Editors}); len(d) != 0 {
		t.Errorf("diffs %v", d)
	}
}

func TestBoardsNeedBrowse(t *testing.T) {
	_, env := testSite(t)
	env.Spec.Boards = []spec.Board{{Name: "VULN Board", Type: "kanban", Filter: "VULN Open"}}
	st, err := boards(env)
	if err != nil || st.Items[0].Kind != Error || !strings.Contains(st.Items[0].Fix, "Browse projects") {
		t.Errorf("%+v, %v", st, err)
	}
}

func TestBoardDiffersFix(t *testing.T) {
	_, env := testSite(t)
	k := kind[spec.Board]{noun: "board", plural: "boards", shareable: true, anyone: true, object: boardObject,
		compare: func(w, g spec.Board) []Diff { return []Diff{{Attribute: "type", Spec: w.Type, Jira: g.Type}} },
		fixed:   "Jira's API cannot change a board's type or filter."}
	want := spec.Board{Name: "VULN Board", Mode: spec.ModeManaged, Type: "scrum"}
	items, _ := globals(env, k, []spec.Board{want}, []spec.Board{{Meta: spec.Meta{ID: "6"}, Name: "VULN Board", Mode: spec.ModeManaged, Type: "kanban"}})
	if items[0].Kind != Differs || !strings.HasSuffix(items[0].Fix, "cannot change a board's type or filter.") {
		t.Errorf("%+v", items[0])
	}
}

func TestBoardWithoutItsFilter(t *testing.T) {
	r, env := testSite(t)
	_, err := createBoard(env, resolver{r, env.Spec}, spec.Board{Name: "VULN Board", Type: "kanban", Filter: "Nowhere"})(context.Background(), &log{})
	if err == nil || !strings.Contains(err.Error(), `0 filters this account can see are named "Nowhere"`) {
		t.Errorf("err = %v", err)
	}
}
