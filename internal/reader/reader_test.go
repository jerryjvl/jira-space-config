package reader

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/jira/jiratest"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// TestLookupFailureFailsTheReader: a name lookup that fails deep in a
// conversion fails the reader that made it, naming the request. Export
// reads resolutions before workflows, but apply may read workflows first.
func TestLookupFailureFailsTheReader(t *testing.T) {
	srv := jiratest.New(t, t.TempDir(), []jiratest.Route{
		{Path: "/rest/api/3/resolution/search", Status: http.StatusInternalServerError},
	})
	r := New(context.Background(), srv.Client())
	if got := r.resolutionName("10000"); got != "id:10000" {
		t.Errorf("resolutionName = %q", got)
	}
	if err := r.done(); err == nil || !strings.Contains(err.Error(), "GET /rest/api/3/resolution/search: 500") {
		t.Errorf("done() = %v, want the failed read", err)
	}
	if err := r.done(); err != nil {
		t.Errorf("a second done() = %v; the failure belongs to the reader that met it", err)
	}
}

// TestSchemeLookupsReportFailures: each scheme lookup names the request
// that failed, at either of its two levels (the list, and what an exact
// match holds).
func TestSchemeLookupsReportFailures(t *testing.T) {
	fail := func(path string) jiratest.Route {
		return jiratest.Route{Path: path, Status: http.StatusInternalServerError}
	}
	ok := func(path string) jiratest.Route { return jiratest.Route{Path: path, File: "one_scheme.json"} }
	its := &IssueTypes{}
	tests := []struct {
		name   string
		routes []jiratest.Route
		call   func(r *Reader) error
		want   string
	}{
		{"issue type schemes", []jiratest.Route{fail("/rest/api/3/issuetypescheme")},
			func(r *Reader) error { _, err := r.FindIssueTypeSchemes("S"); return err }, "looking up issue type schemes"},
		{"issue types of a scheme", []jiratest.Route{ok("/rest/api/3/issuetypescheme"), fail("/rest/api/3/issuetypescheme/mapping")},
			func(r *Reader) error { _, err := r.FindIssueTypeSchemes("S"); return err }, `reading issue types of scheme "S"`},
		{"workflow schemes", []jiratest.Route{fail("/rest/api/3/workflowscheme")},
			func(r *Reader) error { _, err := r.FindWorkflowSchemes("S"); return err }, "looking up workflow schemes"},
		{"issue type screen schemes", []jiratest.Route{fail("/rest/api/3/issuetypescreenscheme")},
			func(r *Reader) error { _, err := r.FindIssueTypeScreenSchemes("S"); return err }, "looking up issue type screen schemes"},
		{"issue type screen scheme mappings", []jiratest.Route{ok("/rest/api/3/issuetypescreenscheme"), fail("/rest/api/3/issuetypescreenscheme/mapping")},
			func(r *Reader) error { _, err := r.FindIssueTypeScreenSchemes("S"); return err }, `reading the mappings of "S"`},
		{"screen schemes", []jiratest.Route{ok("/rest/api/3/issuetypescreenscheme"), ok("/rest/api/3/issuetypescreenscheme/mapping"), fail("/rest/api/3/screenscheme")},
			func(r *Reader) error { _, err := r.FindIssueTypeScreenSchemes("S"); return err }, "reading screen schemes"},
		{"field schemes", []jiratest.Route{fail("/rest/api/3/config/fieldschemes")},
			func(r *Reader) error { _, err := r.FindFieldSchemes(its, "S"); return err }, "looking up field schemes"},
		{"permission scheme", []jiratest.Route{{Path: "/rest/api/3/permissionscheme", File: "permission_list.json"}, fail("/rest/api/3/permissionscheme/20")},
			func(r *Reader) error { _, err := r.FindPermissionSchemes("S"); return err }, `reading permission scheme "S"`},
		{"notification schemes", []jiratest.Route{fail("/rest/api/3/notificationscheme")},
			func(r *Reader) error { _, err := r.FindNotificationSchemes("S"); return err }, "looking up notification schemes"},
		{"issue security schemes", []jiratest.Route{fail("/rest/api/3/issuesecurityschemes")},
			func(r *Reader) error { _, err := r.FindIssueSecuritySchemes("S"); return err }, "looking up issue security schemes"},
		{"priority schemes", []jiratest.Route{fail("/rest/api/3/priorityscheme")},
			func(r *Reader) error { _, err := r.FindPrioritySchemes("S"); return err }, "looking up priority schemes"},
		{"field configuration schemes", []jiratest.Route{fail("/rest/api/3/fieldconfigurationscheme")},
			func(r *Reader) error { _, err := r.FindFieldConfigurationSchemes(its, "S"); return err }, "looking up field configuration schemes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := jiratest.New(t, "testdata", tc.routes)
			err := tc.call(New(context.Background(), srv.Client()))
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "500") {
				t.Errorf("err = %v, want %q and the 500", err, tc.want)
			}
		})
	}
}

// TestEveryHolderSpellingReadsBack: each spelling apply writes a holder as
// reads back as the same kind, with a value where the kind takes one.
func TestEveryHolderSpellingReadsBack(t *testing.T) {
	r := New(context.Background(), nil)
	resolved := []string{"role", "group", "user_field", "group_field"}
	for kind, ty := range spec.HolderTypes {
		for place, name := range []string{ty.Permission, ty.Notification, ty.Security} {
			if name == "" {
				continue
			}
			if slices.Contains(resolved, kind) {
				continue // their values are resolved by name from the site
			}
			got, ok := r.holderText(name, "x", "", "test")
			want := kind
			if ty.Value {
				want += ":x"
			}
			if !ok || got != want {
				t.Errorf("%s in place %d is written %q and read back as %q (%v)", kind, place, name, got, ok)
			}
		}
	}
}

func TestIssueTypesWith(t *testing.T) {
	base := &IssueTypes{Space: []spec.SpaceIssueType{{Name: "Task"}}, ids: []string{"1"}}
	more := base.With([]spec.SpaceIssueType{{Name: "Vulnerability"}}, []string{"2"})
	if base.Len() != 1 || more.Len() != 2 {
		t.Errorf("lengths %d and %d: the original must not change", base.Len(), more.Len())
	}
	if n, ok := more.Name("2"); !ok || n != "Vulnerability" {
		t.Errorf("Name(2) = %q, %v", n, ok)
	}
	if id, ok := more.IDOf("Vulnerability"); !ok || id != "2" {
		t.Errorf("IDOf = %q, %v", id, ok)
	}
	if _, ok := base.IDOf("Vulnerability"); ok {
		t.Error("the original has the added type")
	}
}
