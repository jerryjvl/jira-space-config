package jiratest

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// operation is an operation in the vendored OpenAPI specs with the query
// parameters it declares.
type operation struct {
	method string
	path   *regexp.Regexp
	query  []string
}

func loadOperations(t *testing.T, specDir string) []operation {
	t.Helper()
	var ops []operation
	// The automation API's paths are relative to its own base (on the site,
	// /gateway/api/automation/public/jira/{cloudId}).
	prefixes := map[string]string{"jira-platform-v3.json": "", "jira-software-agile.json": "", "automation.json": "/gateway/api/automation/public/jira/{cloudId}"}
	for _, f := range []string{"jira-platform-v3.json", "jira-software-agile.json", "automation.json"} {
		data, err := os.ReadFile(filepath.Join(specDir, f)) //nolint:gosec // G304: the repository's vendored specs
		if err != nil {
			t.Fatal(err)
		}
		var spec struct {
			Paths map[string]map[string]json.RawMessage `json:"paths"`
		}
		if err := json.Unmarshal(data, &spec); err != nil {
			t.Fatal(err)
		}
		for p, item := range spec.Paths {
			p = prefixes[f] + p
			re := regexp.MustCompile("^" + regexp.MustCompile(`\\\{[^}]*\\\}`).ReplaceAllString(regexp.QuoteMeta(p), `[^/]+`) + "$")
			var shared struct {
				Parameters []struct{ Name, In string } `json:"parameters"`
			}
			if raw, ok := item["parameters"]; ok {
				_ = json.Unmarshal([]byte(`{"parameters":`+string(raw)+`}`), &shared)
			}
			for method, raw := range item {
				var op struct {
					Parameters []struct{ Name, In string } `json:"parameters"`
				}
				if json.Unmarshal(raw, &op) != nil {
					continue
				}
				so := operation{method: strings.ToUpper(method), path: re}
				for _, prm := range append(op.Parameters, shared.Parameters...) {
					if prm.In == "query" {
						so.query = append(so.query, prm.Name)
					}
				}
				ops = append(ops, so)
			}
		}
	}
	return ops
}

// CheckDocumented fails the test for every request whose operation, or one
// of whose query parameters, the vendored OpenAPI specs in specDir do not
// declare. Requests are "METHOD /path?query", as Server.Requests gives them.
// An undocumented parameter is usually ignored by Jira without error, which
// is how export once filtered priority schemes by a projectId that does not
// exist.
func CheckDocumented(t *testing.T, specDir string, requests []string) {
	t.Helper()
	ops := loadOperations(t, specDir)
	var problems []string
	for _, req := range requests {
		method, uri, _ := strings.Cut(req, " ")
		path, rawQuery, _ := strings.Cut(uri, "?")
		if strings.HasPrefix(path, "/_edge/") {
			continue // not in the specs; see docs/api/inventory.md
		}
		var op *operation
		for i := range ops {
			// Prefer the most specific match: fewer templated segments, so
			// /issuesecurityschemes/level wins over /issuesecurityschemes/{id}.
			if ops[i].method == method && ops[i].path.MatchString(path) &&
				(op == nil || templated(ops[i]) < templated(*op)) {
				op = &ops[i]
			}
		}
		if op == nil {
			problems = append(problems, method+" "+path+": no such operation in the specs")
			continue
		}
		query, err := url.ParseQuery(rawQuery)
		if err != nil {
			problems = append(problems, req+": "+err.Error())
		}
		for name := range query {
			if name != "startAt" && !slices.Contains(op.query, name) {
				problems = append(problems, method+" "+path+": query parameter "+name+" is not documented")
			}
		}
	}
	sort.Strings(problems)
	for _, p := range slices.Compact(problems) {
		t.Error(p)
	}
}

func templated(op operation) int { return strings.Count(op.path.String(), `[^/]+`) }
