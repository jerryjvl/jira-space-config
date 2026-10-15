package steps

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// TestGeneratedSchemesAreListed checks every scheme name the steps generate is
// in spec.GeneratedSchemes, which export reads the prefix back from.
func TestGeneratedSchemesAreListed(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	call := regexp.MustCompile(`prefixed\(env, "([^"]+)"\)`)
	found := 0
	for _, f := range files {
		src, err := os.ReadFile(f) //nolint:gosec // the package's own source files
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range call.FindAllSubmatch(src, -1) {
			found++
			if !slices.Contains(spec.GeneratedSchemes, string(m[1])) {
				t.Errorf("%s: %q is generated but not in spec.GeneratedSchemes", f, m[1])
			}
		}
	}
	if found < len(spec.GeneratedSchemes) {
		t.Errorf("found %d generated names, fewer than the %d listed", found, len(spec.GeneratedSchemes))
	}
}
