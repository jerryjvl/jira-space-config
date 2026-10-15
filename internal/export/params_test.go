package export

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/jira/jiratest"
)

// TestRequestParametersAreDocumented checks every recorded request's query
// parameters against those the OpenAPI spec declares for its operation.
func TestRequestParametersAreDocumented(t *testing.T) {
	routes := append(slices.Clone(tsRoutes), legacyRoutes...)
	manifests, _ := filepath.Glob("testdata/*/" + jiratest.ManifestFile)
	for _, m := range manifests {
		r, err := jiratest.LoadRoutes(filepath.Dir(m))
		if err != nil {
			t.Fatal(err)
		}
		routes = append(routes, r...)
	}
	var requests []string
	for _, r := range routes {
		method := r.Method
		if method == "" {
			method = "GET"
		}
		requests = append(requests, method+" "+r.Path+"?"+r.Query.Encode())
	}
	jiratest.CheckDocumented(t, "../../docs/api/openapi", requests)
}
