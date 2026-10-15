// Package apiinventory checks docs/api/inventory.md against the vendored
// Atlassian OpenAPI specifications in docs/api/openapi. It has no runtime
// code; it exists so that the documented API dependency cannot silently
// drift from what Atlassian publishes.
package apiinventory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const docsDir = "../../docs/api"

var specFiles = []string{
	"openapi/jira-platform-v3.json",
	"openapi/jira-software-agile.json",
	"openapi/automation.json",
}

// notInSpec lists endpoints the inventory relies on that Atlassian does not
// document in any of the specs. Each needs a stated fallback in the inventory.
var notInSpec = map[string]bool{
	"GET /_edge/tenant_info": true,
}

// excludedSections hold endpoints that are mentioned but not used.
var excludedSections = map[string]bool{
	"Stability policy":  true,
	"Deprecation watch": true,
}

var (
	// One table cell may name several methods for one path:
	// `GET` / `POST /rest/api/3/screens`.
	endpointRef  = regexp.MustCompile("((?:`(?:GET|POST|PUT|DELETE)`\\s*/\\s*)*)`(GET|POST|PUT|DELETE) ([^`\\s]+)[^`]*`")
	methodOnly   = regexp.MustCompile("`(GET|POST|PUT|DELETE)`")
	pathParam    = regexp.MustCompile(`\{[^}]*\}`)
	backtickPath = regexp.MustCompile("`(/[^`\\s]+)`")
)

type operation struct {
	Deprecated bool `json:"deprecated"`
}

// endpoint is a method and a path with parameter names erased, so that
// `{key}` in the inventory matches `{projectIdOrKey}` in the spec.
type endpoint string

func newEndpoint(method, path string) endpoint {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	return endpoint(method + " " + pathParam.ReplaceAllString(path, "{}"))
}

func (e endpoint) path() string {
	_, p, _ := strings.Cut(string(e), " ")
	return p
}

// loadSpecs returns every operation in the vendored specs, keyed by endpoint,
// with whether the spec marks it deprecated.
func loadSpecs(t *testing.T) map[endpoint]bool {
	t.Helper()
	ops := map[endpoint]bool{}
	for _, name := range specFiles {
		data, err := os.ReadFile(filepath.Join(docsDir, name))
		if err != nil {
			t.Fatalf("reading spec: %v", err)
		}
		var spec struct {
			Paths map[string]map[string]json.RawMessage `json:"paths"`
		}
		if err := json.Unmarshal(data, &spec); err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for path, item := range spec.Paths {
			if name == "openapi/automation.json" {
				path = "/automation/public/jira/{cloudId}" + path // its paths are relative to its own base
			}
			for method, raw := range item {
				method = strings.ToUpper(method)
				switch method {
				case "GET", "POST", "PUT", "DELETE", "PATCH":
				default:
					continue // "parameters", "summary" and similar
				}
				var op operation
				if err := json.Unmarshal(raw, &op); err != nil {
					t.Fatalf("parsing %s %s %s: %v", name, method, path, err)
				}
				ops[newEndpoint(method, path)] = op.Deprecated
			}
		}
	}
	return ops
}

type inventory struct {
	used         map[endpoint][]string // endpoint → sections that list it
	deprecatedOK []string              // path prefixes listed as "Used" under Deprecation watch
	pathErrors   []string              // references that are not full, absolute paths
}

// parseInventory reads the endpoints named in the inventory's tables. Prose
// mentions are ignored: an endpoint counts as used only if a table row names
// it, outside the excluded sections.
func parseInventory(t *testing.T) inventory {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(docsDir, "inventory.md"))
	if err != nil {
		t.Fatalf("reading inventory: %v", err)
	}
	inv := inventory{used: map[endpoint][]string{}}
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		if heading, ok := strings.CutPrefix(line, "## "); ok {
			section = strings.TrimSpace(heading)
			continue
		}
		if !strings.HasPrefix(line, "|") {
			continue
		}
		if section == "Deprecation watch" {
			cells := strings.Split(strings.Trim(line, "| "), "|")
			if strings.HasPrefix(strings.TrimSpace(cells[len(cells)-1]), "Used") {
				for _, m := range backtickPath.FindAllStringSubmatch(cells[0], -1) {
					inv.deprecatedOK = append(inv.deprecatedOK, strings.TrimSuffix(m[1], "*"))
				}
			}
		}
		if excludedSections[section] {
			continue
		}
		for _, m := range endpointRef.FindAllStringSubmatch(line, -1) {
			path, _, _ := strings.Cut(m[3], "?") // placeholders in query strings are fine
			if !strings.HasPrefix(path, "/") || strings.Contains(path, "…") {
				inv.pathErrors = append(inv.pathErrors, section+": "+m[0])
				continue
			}
			methods := []string{m[2]}
			for _, mm := range methodOnly.FindAllStringSubmatch(m[1], -1) {
				methods = append(methods, mm[1])
			}
			for _, method := range methods {
				e := newEndpoint(method, path)
				inv.used[e] = append(inv.used[e], section)
			}
		}
	}
	return inv
}

func TestInventoryEndpointsExistInSpecs(t *testing.T) {
	ops := loadSpecs(t)
	inv := parseInventory(t)

	if len(inv.used) < 50 {
		t.Fatalf("only %d endpoints parsed from the inventory; the parser is probably broken", len(inv.used))
	}
	for _, ref := range inv.pathErrors {
		t.Errorf("inventory reference is not a full absolute path (no “…”, no relative paths): %s", ref)
	}

	var missing, deprecated []string
	for e, sections := range inv.used {
		if notInSpec[string(e)] {
			continue
		}
		isDeprecated, ok := ops[e]
		switch {
		case !ok:
			missing = append(missing, string(e)+"  (section "+sections[0]+")")
		case isDeprecated && !allowedDeprecated(e, inv.deprecatedOK):
			deprecated = append(deprecated, string(e)+"  (section "+sections[0]+")")
		}
	}
	sort.Strings(missing)
	sort.Strings(deprecated)
	for _, m := range missing {
		t.Errorf("not in any vendored spec: %s", m)
	}
	for _, d := range deprecated {
		t.Errorf("deprecated in the spec but not listed as used under “Deprecation watch”: %s", d)
	}
}

func TestNotInSpecIsStillTrue(t *testing.T) {
	ops := loadSpecs(t)
	for e := range notInSpec {
		method, path, _ := strings.Cut(e, " ")
		if _, ok := ops[newEndpoint(method, path)]; ok {
			t.Errorf("%s is now documented; remove it from notInSpec", e)
		}
	}
}

func allowedDeprecated(e endpoint, prefixes []string) bool {
	p := e.path()
	for _, prefix := range prefixes {
		if strings.HasPrefix(p, pathParam.ReplaceAllString(prefix, "{}")) {
			return true
		}
	}
	return false
}
