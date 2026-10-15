package export

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/jerryjvl/jira-space-config/internal/jira/jiratest"
)

// coverageRule is one row of docs/api/export-coverage.md: a response kind,
// field path patterns, and a disposition.
type coverageRule struct {
	line       int
	kind       string
	patterns   []string
	used       bool
	unrecorded bool // marked "(not recorded yet)": no recording contains it
}

var (
	coverageRow  = regexp.MustCompile("^\\| ([^|]+) \\| (`[^|]+`|\\*) \\| ((captured|dropped|gap):[^|]+) \\|$")
	backticked   = regexp.MustCompile("`([^`]+)`")
	listIndex    = strings.NewReplacer("[]", "#") // path.Match treats [ as a class
	coverageDocs = "../../docs/api/export-coverage.md"
)

// endpointOf names a request the way the coverage table does: method and
// path, without /rest/api/3, with IDs replaced by {id}, {key}, {fieldId}
// or {propertyKey}.
func endpointOf(method, p string) string {
	if method == "" {
		method = "GET"
	}
	segs := strings.Split(strings.TrimPrefix(p, "/rest/api/3"), "/")
	for i, s := range segs {
		prev := ""
		if i > 0 {
			prev = segs[i-1]
		}
		switch {
		case prev == "properties":
			segs[i] = "{propertyKey}"
		case strings.HasPrefix(s, "customfield_"):
			segs[i] = "{fieldId}"
		case idSegment.MatchString(s):
			segs[i] = "{id}"
		case prev == "project" && s != "search" && s != "type" && s != "recent":
			segs[i] = "{key}"
		}
	}
	return method + " " + strings.Join(segs, "/")
}

var idSegment = regexp.MustCompile(`^(\d+|[0-9a-f]{8}-[0-9a-f-]{27})$`)

// fixtureEndpoints maps every fixture file to the endpoint that serves it:
// TS from its route lists, recordings from their manifests.
func fixtureEndpoints(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{
		"testdata/defaults_not_supported.json":   "GET /field/{fieldId}/context/defaultValues",
		"testdata/ts/workflow_capabilities.json": "GET /workflows/capabilities",
		"testdata/defaults_early_access.json":    "GET /field/{fieldId}/context/defaultValues",
		"testdata/defaults_legacy_10043.json":    "GET /field/{fieldId}/context/defaultValue",
	}
	for _, r := range append(slices.Clone(tsRoutes), legacyRoutes...) {
		if r.File != "" {
			out["testdata/ts/"+r.File] = endpointOf(r.Method, r.Path)
		}
	}
	manifests, _ := filepath.Glob("testdata/*/" + jiratest.ManifestFile)
	for _, m := range manifests {
		dir := filepath.Dir(m)
		routes, err := jiratest.LoadRoutes(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range routes {
			out[filepath.Join(dir, r.File)] = endpointOf(r.Method, r.Path)
		}
	}
	return out
}

func loadCoverage(t *testing.T) []*coverageRule {
	t.Helper()
	data, err := os.ReadFile(coverageDocs)
	if err != nil {
		t.Fatal(err)
	}
	var rules []*coverageRule
	for i, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| Endpoint") || strings.HasPrefix(line, "| ---") {
			continue
		}
		m := coverageRow.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("%s:%d: row does not have the form | response | `path`, … | captured/dropped/gap: … |", coverageDocs, i+1)
			continue
		}
		r := &coverageRule{line: i + 1, kind: strings.TrimSpace(m[1]), unrecorded: strings.Contains(m[3], "(not recorded yet)")}
		if m[2] == "*" {
			r.patterns = []string{"*"}
		}
		prefix := ""
		for _, p := range backticked.FindAllStringSubmatch(m[2], -1) {
			pat := p[1]
			// `.name` after `values.[].scheme.id` means values.[].scheme.name.
			if strings.HasPrefix(pat, ".") {
				pat = prefix + pat
			} else if i := strings.LastIndex(pat, "."); i > 0 {
				prefix = pat[:i]
			}
			r.patterns = append(r.patterns, pat)
		}
		rules = append(rules, r)
	}
	if len(rules) < 100 {
		t.Fatalf("only %d coverage rows parsed; the parser is probably broken", len(rules))
	}
	return rules
}

// leafPaths lists every leaf of a JSON document, with list indexes as [].
func leafPaths(v any, prefix string, out map[string]bool) {
	join := func(k string) string {
		if prefix == "" {
			return k
		}
		return prefix + "." + k
	}
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			leafPaths(val, join(k), out)
		}
	case []any:
		for _, val := range x {
			leafPaths(val, join("[]"), out)
		}
	default:
		out[prefix] = true
	}
}

// TestExportCoverage: every field in every recorded response has a
// disposition in docs/api/export-coverage.md, and every row there still
// matches something.
func TestExportCoverage(t *testing.T) {
	rules := loadCoverage(t)
	endpoints := fixtureEndpoints(t)
	files, _ := filepath.Glob("testdata/*/*.json")
	more, _ := filepath.Glob("testdata/*.json")
	files = append(files, more...)
	if len(files) < 20 {
		t.Fatalf("only %d fixtures found", len(files))
	}
	var missing []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var doc any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		paths := map[string]bool{}
		leafPaths(doc, "", paths)
		if filepath.Base(f) == jiratest.ManifestFile {
			continue
		}
		kind, ok := endpoints[f]
		if !ok {
			t.Errorf("%s is not served by any route or manifest; remove it or add a route", f)
			continue
		}
		for p := range paths {
			if !covered(rules, kind, p) {
				missing = append(missing, kind+"\t"+p)
			}
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		kind, p, _ := strings.Cut(m, "\t")
		t.Errorf("no disposition in %s for %s field %q", filepath.Base(coverageDocs), kind, p)
	}
	for _, r := range rules {
		if r.used && r.unrecorded {
			t.Errorf("%s:%d: row for %s %v is marked \"(not recorded yet)\" but now matches recorded data; remove the marker", filepath.Base(coverageDocs), r.line, r.kind, r.patterns)
		}
		if !r.used && r.kind != "*" && !r.unrecorded {
			t.Errorf("%s:%d: row for %s %v matches no recorded field; remove or correct it", filepath.Base(coverageDocs), r.line, r.kind, r.patterns)
		}
	}
}

func covered(rules []*coverageRule, kind, p string) bool {
	found := false
	for _, r := range rules {
		if r.kind != kind && r.kind != "*" {
			continue
		}
		for _, pat := range r.patterns {
			if ok, _ := path.Match(listIndex.Replace(pat), listIndex.Replace(p)); ok {
				r.used = true
				found = true
			}
		}
	}
	return found
}
