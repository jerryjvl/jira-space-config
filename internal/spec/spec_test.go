package spec

import (
	"flag"
	"os"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func load(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustParse(t *testing.T, data []byte) *Spec {
	t.Helper()
	s, diags := Parse(data)
	diags = append(diags, Validate(s)...)
	if diags.HasErrors() {
		for _, d := range diags.Sorted() {
			t.Log(d.Format("input"))
		}
		t.Fatal("unexpected errors")
	}
	return s
}

func mustWrite(t *testing.T, s *Spec) []byte {
	t.Helper()
	out, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestCanonicalGolden: the full fixture parses and validates cleanly, and the
// canonical writer produces the golden file.
func TestCanonicalGolden(t *testing.T) {
	got := mustWrite(t, mustParse(t, load(t, "full.yaml")))
	if *update {
		if err := os.WriteFile("testdata/full.golden.yaml", got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if want := load(t, "full.golden.yaml"); string(got) != string(want) {
		t.Errorf("canonical output differs from testdata/full.golden.yaml (run go test ./internal/spec -update to accept):\n%s", got)
	}
}

// TestCanonicalIsFixpoint is guarantee R2/R4: canonical text parses and
// writes back to the same bytes.
func TestCanonicalIsFixpoint(t *testing.T) {
	golden := load(t, "full.golden.yaml")
	if again := mustWrite(t, mustParse(t, golden)); string(again) != string(golden) {
		t.Errorf("writing the golden file back changed it:\n%s", again)
	}
}

// TestSpacingLeavesMultilineTextAlone: blank lines are only added between
// sections, never inside multi-line text, even when that text contains lines
// that look like keys or has blank lines of its own.
func TestSpacingLeavesMultilineTextAlone(t *testing.T) {
	tricky := "First line.\n\nspace:\n  details: looks like a key\nlast: line"
	s := &Spec{
		APIVersion: APIVersion, Kind: Kind,
		Metadata: Metadata{Name: "t", Description: Some(tricky)},
		Screens:  []Screen{{Name: "Default Screen", Mode: ModeReuse, Description: Some(tricky)}},
		Space: &Space{Key: "T", Details: &Details{Description: Some(tricky)},
			IssueTypes: []SpaceIssueType{{Name: "Task"}}},
	}
	out := mustWrite(t, s)
	back, diags := Parse(out)
	if diags.HasErrors() {
		t.Fatalf("output does not parse:\n%v\n%s", diags, out)
	}
	for name, got := range map[string]string{
		"metadata.description":      back.Metadata.Description.V,
		"screens[0].description":    back.Screens[0].Description.V,
		"space.details.description": back.Space.Details.Description.V,
	} {
		if got != tricky {
			t.Errorf("%s changed:\n got %q\nwant %q", name, got, tricky)
		}
	}
	if again := mustWrite(t, back); string(again) != string(out) {
		t.Errorf("not a fixpoint:\n%s\n---\n%s", out, again)
	}
}

// TestNothingLost checks that values from the input survive into the
// canonical form. It guards against a key being decoded but never written.
func TestNothingLost(t *testing.T) {
	golden := string(load(t, "full.golden.yaml"))
	for _, want := range []string{
		"cloud_id: 00000000-0000-0000-0000-000000000000", "Being assessed", `id: "10101"`,
		"inward: is duplicated by", "level: epic", "searcher: default",
		"x: 120", "y: 40.5", "jira.issue.editable: \"false\"",
		"looped: true", "type: global", "screen: DEMO Triage", "event: Issue Assigned",
		"type: restrict_to", "most_recent_only: true", "mode: users",
		"error_message: Assess first", "forge:expression-validator", "unknownParam",
		"jira.field.resolution.include: [False Positive, Duplicate]", `opsbar-sequence: "10"`,
		"category: null", "url: null", "renderer: wiki", "What was found", "issue_types: [Finding]", "by_issue_type:", "Finding: {required: true}",
		"screens: Default Screen", "screens: {create: DEMO Create, edit: DEMO View, view: DEMO View}",
		"value: Legacy", "disabled: true", "children: [Linux, Windows]", "default: [Host, Linux]",
		"scope: shared", "issue_security: null", "SCHEDULE_ISSUES",
		"current_assignee", `name: "1.10"`, "start_date: \"2026-01-01\"", "archived: false",
		"since: 2026-01-01", "threshold: 3", "viewers: ['group:demo-team', space]", "filter: DEMO Open findings", "Written by hand for tests.",
		"default_members: ['group:team-alpha']", "colour: '#f15C75'", "icon: /images/icons/priorities/high.svg", "site_default: true",
		"avatar: \"10303\"", "icon: /images/icons/issuetypes/epic.svg", "locked: true", "deprecated: true",
		"type: software", "avatar: \"10417\"", "description: The site default",
	} {
		if !strings.Contains(golden, want) {
			t.Errorf("golden output lacks %q", want)
		}
	}
}

// TestAutomationExamples: every rule example in docs/automation.md is a
// valid automation_rules entry.
func TestAutomationExamples(t *testing.T) {
	data, err := os.ReadFile("../../docs/automation.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```yaml\n(- name: .*?)```").FindAllStringSubmatch(string(data), -1)
	if len(blocks) < 7 {
		t.Fatalf("found %d examples", len(blocks))
	}
	for _, b := range blocks {
		if strings.Contains(b[1], "{...}") {
			continue // an outline, not a whole rule
		}
		indented := "  " + strings.ReplaceAll(strings.TrimSuffix(b[1], "\n"), "\n", "\n  ")
		src := "apiVersion: jira-space-config/v1alpha1\nkind: SpaceConfig\nmetadata: {name: t}\nbinding: {site: https://x.atlassian.net, cloud_id: c}\nautomation_rules:\n" + indented +
			"\nspace: {key: VULN, managed_prefix: VULN}\n"
		s, diags := Parse([]byte(src))
		if !diags.HasErrors() {
			diags = append(diags, Validate(s)...)
		}
		for _, d := range diags {
			if d.Severity == SeverityError {
				t.Errorf("%s: %v", strings.SplitN(b[1], "\n", 2)[0], d)
			}
		}
	}
}

// TestSpecFormatExamples: every whole spec in the user documentation (a
// block that starts with apiVersion) parses and validates without errors, so
// the documents cannot describe a format the tool refuses.
func TestSpecFormatExamples(t *testing.T) {
	for file, least := range map[string]int{"../../docs/spec-format.md": 4, "../../README.md": 1} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		blocks := regexp.MustCompile("(?s)```yaml\n(apiVersion: .*?)```").FindAllStringSubmatch(string(data), -1)
		if len(blocks) < least {
			t.Fatalf("%s: found %d whole specs", file, len(blocks))
		}
		for _, b := range blocks {
			s, diags := Parse([]byte(b[1]))
			if !diags.HasErrors() {
				diags = append(diags, Validate(s)...)
			}
			for _, d := range diags {
				if d.Severity == SeverityError {
					t.Errorf("%s: %s: %v", file, strings.SplitN(b[1], "\n", 4)[2], d)
				}
			}
		}
	}
}
