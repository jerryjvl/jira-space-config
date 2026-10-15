package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportRefusesToOverwrite(t *testing.T) {
	_, transport := fakeSite(t, map[string]string{}, http.StatusOK)
	existing := filepath.Join(t.TempDir(), "space.yaml")
	if err := os.WriteFile(existing, []byte("keep me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runCLI(t, transport, goodEnv, "export", "--site", "https://example.atlassian.net", "--space", "TS", "-o", existing)
	if err == nil || !strings.Contains(stderr, "already exists; use --force to overwrite it") {
		t.Errorf("err = %v, stderr:\n%s", err, stderr)
	}
	if data, _ := os.ReadFile(existing); string(data) != "keep me\n" {
		t.Errorf("the existing file was changed: %q", data)
	}

	// With --force the check is passed and export goes on to contact Jira
	// (here a fake site without the space).
	_, stderr, err = runCLI(t, transport, goodEnv, "export", "--site", "https://example.atlassian.net", "--space", "TS", "-o", existing, "--force")
	if err == nil || strings.Contains(stderr, "already exists") {
		t.Errorf("--force: err = %v, stderr:\n%s", err, stderr)
	}
}

func TestValidateMissingFile(t *testing.T) {
	_, stderr, err := runCLI(t, nil, goodEnv, "validate", filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil || !strings.Contains(stderr, "no such file") {
		t.Errorf("err = %v, stderr:\n%s", err, stderr)
	}
}
