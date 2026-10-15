package cli

import (
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSpec(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLiveApplyNeedsATerminal(t *testing.T) {
	_, transport := fakeSite(t, whoamiResponses, http.StatusOK)
	_, stderr, err := runCLI(t, transport, goodEnv, "apply", "--site", "https://example.atlassian.net", "--space", "DEMO", writeSpec(t, "x"))
	if err == nil || !strings.Contains(stderr, "a live apply asks before every step, so it needs a terminal") {
		t.Errorf("err = %v, stderr:\n%s", err, stderr)
	}
}

func TestApplyDryRun(t *testing.T) {
	responses := maps.Clone(whoamiResponses)
	responses["/rest/api/3/search/approximate-count"] = `{"count":3}`
	srv, transport := fakeSite(t, responses, http.StatusOK)
	spec := writeSpec(t, "apiVersion: jira-space-config/v1alpha1\nkind: SpaceConfig\nmetadata: {name: t}\nspace: {key: DEMO}\n")
	stdout, stderr, err := runCLI(t, transport, goodEnv, "apply", "--dry-run", "--site", srv.URL, "--space", "DEMO", spec)
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, stderr)
	}
	for _, want := range []string{"DRY RUN · nothing will be changed", `DEMO "Demo space" · company-managed · 3 work items`, "Nothing was changed."} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Error("colour codes in output that is not a terminal")
	}
}

func TestApplyUnknownAllowUpdate(t *testing.T) {
	_, transport := fakeSite(t, whoamiResponses, http.StatusOK)
	_, stderr, err := runCLI(t, transport, goodEnv, "apply", "--dry-run", "--allow-update=everything", "--site", "https://example.atlassian.net",
		"--space", "DEMO", writeSpec(t, "x"))
	if err == nil || !strings.Contains(stderr, "--allow-update=everything: the kinds of change that can be allowed are: role_members, notifications, permissions, issue_security, filters") {
		t.Errorf("err = %v, stderr:\n%s", err, stderr)
	}
}

func TestApplyUnknownAllowDelete(t *testing.T) {
	_, transport := fakeSite(t, whoamiResponses, http.StatusOK)
	_, stderr, err := runCLI(t, transport, goodEnv, "apply", "--dry-run", "--allow-delete=everything", "--site", "https://example.atlassian.net",
		"--space", "DEMO", writeSpec(t, "x"))
	if err == nil || !strings.Contains(stderr, "--allow-delete=everything: the kinds of removal that can be allowed are: workflows, contexts, issue_types, fields, priorities, notifications, permissions, issue_security, filters") {
		t.Errorf("err = %v, stderr:\n%s", err, stderr)
	}
}

func TestApplyUnknownCheckUsage(t *testing.T) {
	_, transport := fakeSite(t, whoamiResponses, http.StatusOK)
	_, stderr, err := runCLI(t, transport, goodEnv, "apply", "--dry-run", "--check-usage=screens", "--site", "https://example.atlassian.net",
		"--space", "DEMO", writeSpec(t, "x"))
	if err == nil || !strings.Contains(stderr, "--check-usage=screens: the kinds whose usage can be checked space by space are: permission_scheme") {
		t.Errorf("err = %v, stderr:\n%s", err, stderr)
	}
}
