package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValidateGoodSpec(t *testing.T) {
	stdout, stderr, err := runCLI(t, nil, goodEnv, "validate", "../spec/testdata/full.yaml")
	if err != nil {
		t.Fatalf("validate: %v\n%s%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "0 errors, 0 warnings; 3 names not declared") {
		t.Errorf("summary missing:\n%s", stdout)
	}
}

func TestValidateCanonical(t *testing.T) {
	stdout, stderr, err := runCLI(t, nil, goodEnv, "validate", "--canonical", "../spec/testdata/full.yaml")
	if err != nil {
		t.Fatalf("validate: %v\n%s", err, stderr)
	}
	golden, err := os.ReadFile("../spec/testdata/full.golden.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != string(golden) {
		t.Error("--canonical output differs from the golden file")
	}
	if !strings.Contains(stderr, "0 errors") {
		t.Errorf("messages should go to stderr with --canonical:\n%s", stderr)
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	path := writeTemp(t, "apiVersion: jira-space-config/v1alpha1\nkind: SpaceConfig\nmetadata: {name: t}\n"+
		"statuses:\n  - {name: A, mode: ensure, catgory: todo}\nspace:\n  key: demo\n")
	stdout, stderr, err := runCLI(t, nil, goodEnv, "validate", path)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{
		path + `:5:29: error: statuses[0]: unknown key "catgory" (did you mean "category"?)`,
		path + `:7:3: error: space.key:`,
		"3 errors, 0 warnings",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "Error: 3 errors in") {
		t.Errorf("stderr: %s", stderr)
	}
}

func TestValidateUnreadableYAML(t *testing.T) {
	path := writeTemp(t, "a: [\n")
	stdout, _, err := runCLI(t, nil, goodEnv, "validate", path)
	if err == nil || !strings.Contains(stdout, "not valid YAML") {
		t.Errorf("err=%v stdout=%s", err, stdout)
	}
}
