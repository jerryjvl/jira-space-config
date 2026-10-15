package export

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/jira/jiratest"
)

// TestEveryFailedReadIsReported fails each recorded request in turn with a
// 500 and requires export to stop with an error naming that request. Export
// has no optional reads: a spec written from a partial view of the space
// would differ from the space without saying so.
func TestEveryFailedReadIsReported(t *testing.T) {
	jsc, err := jiratest.LoadRoutes("testdata/jsctest2")
	if err != nil {
		t.Fatal(err)
	}
	legacyDir, legacyOwn := legacyOwnSchemeFixture(t)
	sets := []struct {
		name, dir, key string
		routes         []jiratest.Route
	}{
		{"ts", "testdata/ts", "TS", tsRoutes},
		{"ts-legacy", "testdata/ts", "TS", legacyRoutes},
		{"jsctest2", "testdata/jsctest2", "JSCTEST2", jsc},
		{"legacy-own-scheme", legacyDir, "TS", legacyOwn},
	}
	for _, set := range sets {
		for i, r := range set.routes {
			if r.Status != 0 {
				continue // a recorded error answer, which export is built to expect
			}
			t.Run(set.name+" "+r.Path+" "+r.Query.Encode(), func(t *testing.T) {
				t.Parallel()
				routes := slices.Clone(set.routes)
				routes[i] = jiratest.Route{Method: r.Method, Path: r.Path, Query: r.Query, Body: r.Body, Status: http.StatusInternalServerError}
				srv := jiratest.New(t, set.dir, routes)
				_, err := Space(context.Background(), srv.Client(), set.key, Options{ToolVersion: "test", Now: fixedNow})
				switch {
				case err == nil:
					t.Errorf("a failed %s went unreported: export succeeded", r.Path)
				case !strings.Contains(err.Error(), r.Path) || !strings.Contains(err.Error(), "500"):
					t.Errorf("the error does not name the failed request %s: %v", r.Path, err)
				}
			})
		}
	}
}
