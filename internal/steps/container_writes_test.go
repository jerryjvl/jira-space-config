package steps

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

func TestIssueTypeSchemeUpdate(t *testing.T) {
	_, env := testSite(t)
	remove := updateIssueTypeScheme(env, "20", []string{"Story", "Task"}, "Story", []string{"Story"}, "")
	if remove.class != "removing" || strings.Join(remove.removes, ",") != `issue type "Task"` || remove.allow != "issue_types" {
		t.Errorf("class %s, removes %v", remove.class, remove.removes)
	}
	l := &log{}
	if _, err := remove.do(context.Background(), l); err != nil || strings.Join(l.sent, "") != "DELETE /rest/api/3/issuetypescheme/20/issuetype/10014" {
		t.Errorf("err %v, sent %v", err, l.sent)
	}
	ch := updateIssueTypeScheme(env, "20", []string{"Story"}, "Story", []string{"Task", "Story"}, "Task")
	if ch.class != "altering" {
		t.Errorf("class %s", ch.class)
	}
	l = &log{}
	if _, err := ch.do(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`PUT /rest/api/3/issuetypescheme/20/issuetype {"issueTypeIds":["10014"]}`,
		`PUT /rest/api/3/issuetypescheme/20 {"defaultIssueTypeId":"10014"}`,
		`PUT /rest/api/3/issuetypescheme/20/issuetype/move {"issueTypeIds":["10014","10005"],"position":"First"}`,
	}
	if strings.Join(l.sent, "\n") != strings.Join(want, "\n") {
		t.Errorf("sent:\n%s", strings.Join(l.sent, "\n"))
	}
	if ch := updateIssueTypeScheme(env, "20", []string{"Story"}, "Story", []string{"Story", "Story"}, ""); ch.class != "altering" {
		t.Errorf("reordered: class %s", ch.class)
	}
	_, err := updateIssueTypeScheme(env, "20", nil, "", []string{"Nothing"}, "").do(context.Background(), &log{})
	if err == nil || !strings.Contains(err.Error(), `no issue type on the site is named "Nothing"`) {
		t.Errorf("err = %v", err)
	}
}

func TestScreenSchemeUpdates(t *testing.T) {
	r, env := testSite(t)
	x := resolver{r, env.Spec}
	env.Spec.Space.ManagedPrefix = "VULN"
	screens := &spec.ScreenSet{Create: "Default Screen", Edit: "Default Screen", View: "Default Screen"}
	env.Spec.Space.IssueTypes = []spec.SpaceIssueType{{Name: "Story", Screens: screens}}
	current := []line{{"Story", "Other Screen"}}
	scheme := func(desc string, screen int) string {
		return `{"isLast":true,"values":[{"id":5,"name":"VULN Story Screen Scheme","description":"` + desc + `","screens":{"default":` +
			itoa(screen) + `,"create":` + itoa(screen) + `,"edit":` + itoa(screen) + `,"view":` + itoa(screen) + `}}]}`
	}
	const managed = "[managed-by: jira-space-config]"
	const mappingPath = "GET /rest/api/3/issuetypescreenscheme/mapping"
	for name, tc := range map[string]struct {
		answers map[string]string
		want    string // an error, or the last request
	}{
		"a new screen scheme, and the issue type mapped again": {map[string]string{
			mappingPath:                     `{"isLast":true,"values":[{"issueTypeId":"10005","screenSchemeId":"6"}]}`,
			"GET /rest/api/3/screenscheme":  `{"isLast":true,"values":[]}`,
			"POST /rest/api/3/screenscheme": `{"id":7}`,
		}, `PUT /rest/api/3/issuetypescreenscheme/30/mapping {"issueTypeMappings":[{"issueTypeId":"10005","screenSchemeId":"7"}]}`},
		"its screen scheme changed, and the mapping kept": {map[string]string{
			mappingPath:                    `{"isLast":true,"values":[{"issueTypeScreenSchemeId":"30","issueTypeId":"10005","screenSchemeId":"5"}]}`,
			"GET /rest/api/3/screenscheme": scheme(managed, 9),
		}, `PUT /rest/api/3/screenscheme/5 {"screens":{"create":"1","default":"1","edit":"1","view":"1"}}`},
		"a screen scheme the tool did not make": {map[string]string{"GET /rest/api/3/screenscheme": scheme("by hand", 9)},
			"this tool did not create it"},
		"a screen scheme another issue type screen scheme uses": {map[string]string{
			mappingPath:                    `{"isLast":true,"values":[{"issueTypeScreenSchemeId":"31","issueTypeId":"10005","screenSchemeId":"5"}]}`,
			"GET /rest/api/3/screenscheme": scheme(managed, 9),
		}, "issue type screen scheme 31 uses it too"},
		"a screen scheme that is the default": {map[string]string{
			mappingPath:                    `{"isLast":true,"values":[{"issueTypeScreenSchemeId":"30","issueTypeId":"default","screenSchemeId":"5"}]}`,
			"GET /rest/api/3/screenscheme": scheme(managed, 9),
		}, "is the default of the issue type screen scheme"},
		"a screen scheme another issue type keeps": {map[string]string{
			mappingPath:                    `{"isLast":true,"values":[{"issueTypeScreenSchemeId":"30","issueTypeId":"10014","screenSchemeId":"5"}]}`,
			"GET /rest/api/3/screenscheme": scheme(managed, 9),
		}, `is also used for issue type "Task", whose screens stay`},
	} {
		t.Run(name, func(t *testing.T) {
			ch := updateScreenSchemes(env, x, "30", nil, current)
			l := &log{answers: tc.answers}
			_, err := ch.do(context.Background(), l)
			switch {
			case strings.HasPrefix(tc.want, "PUT "):
				if err != nil || !slices.Contains(l.sent, tc.want) {
					t.Errorf("err %v, sent:\n%s", err, strings.Join(l.sent, "\n"))
				}
			case err == nil || !strings.Contains(err.Error(), tc.want):
				t.Errorf("err = %v", err)
			}
		})
	}
	env.Spec.Space.IssueTypes = []spec.SpaceIssueType{{Name: "Nothing", Screens: screens}}
	_, err := updateScreenSchemes(env, x, "30", nil, nil).do(context.Background(), &log{answers: map[string]string{
		"GET /rest/api/3/screenscheme": `{"isLast":true,"values":[]}`, "POST /rest/api/3/screenscheme": `{"id":8}`}})
	if err == nil || !strings.Contains(err.Error(), `no issue type on the site is named "Nothing"`) {
		t.Errorf("unknown issue type: %v", err)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// TestSharedContainerIsNotChanged: a managed scheme another space uses
// stays as it is.
func TestSharedContainerIsNotChanged(t *testing.T) {
	_, env := testSite(t)
	items, err := evalContainerItems(env, container{noun: "issue type scheme", title: "Issue type scheme",
		want: []line{{"issue types", "Story"}}, current: found{ID: "20", Name: "VULN Issue Type Scheme", Managed: true, Lines: []line{{"issue types", "Task"}}},
		update: func() change { t.Fatal("planned a change"); return change{} },
		usage:  func() ([]string, error) { return []string{"10042", "10077"}, nil }})
	if err != nil || items[0].Kind != Error || !strings.Contains(items[0].Summary, "another space uses it too (id 10077)") {
		t.Errorf("%+v, %v", items, err)
	}
}

// TestFieldSettingsPerIssueTypeTakenAway: an override the spec leaves out
// is removed, and an empty overall setting is not sent.
func TestFieldSettingsPerIssueTypeTakenAway(t *testing.T) {
	r, env := testSite(t)
	got := spec.SpaceField{Name: "Summary", ByIssueType: []spec.IssueTypeFieldSettings{
		{IssueType: "Story", FieldSettings: spec.FieldSettings{Required: spec.Some(true), Description: spec.Some("x")}}}}
	want := spec.SpaceField{Name: "Summary", ByIssueType: []spec.IssueTypeFieldSettings{
		{IssueType: "Task", FieldSettings: spec.FieldSettings{Required: spec.Some(true)}}}}
	ch := updateFieldScheme(env, resolver{r, env.Spec}, "7", []spec.SpaceField{want}, []spec.SpaceField{got}, nil)
	l := &log{}
	if _, err := ch.do(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	sent := strings.Join(l.sent, "\n")
	for _, w := range []string{
		`PUT /rest/api/3/config/fieldschemes/fields/parameters {"summary":[{"schemeIds":[7],"workTypeParameters":[{"isRequired":true,"workTypeId":10014}]}]}`,
		`DELETE /rest/api/3/config/fieldschemes/fields/parameters {"summary":[{"parameters":["isRequired","description"],"schemeId":7,"workTypeIds":[10005]}]}`,
	} {
		if !strings.Contains(sent, w) {
			t.Errorf("sent lacks %s:\n%s", w, sent)
		}
	}
}
