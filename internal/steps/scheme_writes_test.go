package steps

import (
	"context"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/reader"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// TestSchemeWriteProblems: a scheme write stops, before anything is
// written, when a name it needs is not on the site or Jira's answer is not
// what was expected.
func TestSchemeWriteProblems(t *testing.T) {
	r, env := testSite(t)
	x := resolver{r, env.Spec}
	ctx := context.Background()
	for name, tc := range map[string]struct {
		err  func() error
		want string
	}{
		"an answer without an ID": {func() error {
			_, err := postScheme("/rest/api/3/workflowscheme", map[string]any{})(ctx, &recorder{answer: `{}`})
			return err
		}, "the answer to POST /rest/api/3/workflowscheme does not say what was created"},
		"an unknown issue type": {func() error { _, err := env.issueTypeIDs([]string{"Nothing"}); return err },
			`no issue type on the site is named "Nothing"`},
		"an unknown screen": {func() error { _, err := x.screen("Missing Screen"); return err },
			`no screen on the site is named "Missing Screen"`},
		"a holder permissions cannot have": {func() error { _, _, err := x.holder("current_user", spec.PlacePermission); return err },
			`"current_user" cannot be written here`},
		"an unknown event": {func() error {
			n := spec.Notifications{Events: []spec.Grant{{Permission: "Gone", Holders: []string{"current_user"}}}}
			_, err := createNotificationScheme(env, x, n, "VULN Notification Scheme")(ctx, answers{})
			return err
		}, `no event on the site is named "Gone"`},
		"an unknown priority": {func() error { _, err := x.priority("Nothing"); return err },
			`no priority on the site is named "Nothing"`},
		"no scheme by the name": {func() error { _, err := x.schemeByName(env, "permission scheme", "Nothing"); return err },
			`0 permission schemes on the site are named "Nothing"; one is needed`},
		"no priority scheme to map onto": {func() error { _, err := priorityMappings(env, x, "Nothing"); return err },
			`0 priority schemes on the site are named "Nothing"; one is needed`},
	} {
		if err := tc.err(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v; want %q", name, err, tc.want)
		}
	}
}

// TestFieldConfigurationSchemeSwitch: the space is switched to the scheme
// by its ID and the space's own.
func TestFieldConfigurationSchemeSwitch(t *testing.T) {
	r, env := testSite(t)
	env.its = &reader.IssueTypes{}
	rec := &recorder{answer: `{}`}
	sw := switchPlan{noun: "field configuration scheme", from: "Old", to: "Legacy Field Configuration Scheme"}
	id, err := switchTo(env, resolver{r, env.Spec}, sw)(context.Background(), rec)
	if err != nil || id != "10300" {
		t.Fatalf("id %q, err %v", id, err)
	}
	if rec.method != "PUT" || rec.path != "/rest/api/3/fieldconfigurationscheme/project" ||
		rec.body["fieldConfigurationSchemeId"] != "10300" || rec.body["projectId"] != "10042" {
		t.Errorf("sent %s %s %v", rec.method, rec.path, rec.body)
	}
}

// TestDetailsCategory: a category is sent by its ID, none as -1, and a write
// stops when the site has no category by the name.
func TestDetailsCategory(t *testing.T) {
	_, env := testSite(t)
	ctx := context.Background()
	if _, err := updateDetails(env, &spec.Details{Category: spec.Some("Security")})(ctx, &recorder{answer: `[]`}); err == nil ||
		!strings.Contains(err.Error(), `no space category on the site is named "Security"`) {
		t.Errorf("unknown category: %v", err)
	}
	none := &recorder{}
	if _, err := updateDetails(env, &spec.Details{Category: spec.Opt[string]{Set: true, Null: true}})(ctx, none); err != nil ||
		none.body["categoryId"] != float64(-1) {
		t.Errorf("no category: %v, sent %v", err, none.body)
	}
	rec := &recorder{answer: `[{"id":"10001","name":"Security"}]`}
	if _, err := updateDetails(env, &spec.Details{Category: spec.Some("Security")})(ctx, rec); err != nil ||
		rec.path != "/rest/api/3/project/VULN" || rec.body["categoryId"] != float64(10001) {
		t.Errorf("category: %v, sent %s %v", err, rec.path, rec.body)
	}
}
