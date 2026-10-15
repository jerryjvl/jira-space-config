package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstructions(t *testing.T) {
	stdout, stderr, err := runCLI(t, nil, goodEnv, "instructions", "../spec/testdata/full.yaml")
	if err != nil {
		t.Fatalf("instructions: %v\n%s", err, stderr)
	}
	if !strings.HasPrefix(stdout, "# Setting up Demo space (DEMO) by hand") {
		t.Errorf("unexpected output:\n%s", stdout)
	}

	out := filepath.Join(t.TempDir(), "setup.md")
	if _, stderr, err := runCLI(t, nil, goodEnv, "instructions", "-o", out, "../spec/testdata/full.yaml"); err != nil {
		t.Fatalf("with -o: %v\n%s", err, stderr)
	} else if !strings.Contains(stderr, "Wrote the instructions for space DEMO") {
		t.Errorf("no summary: %s", stderr)
	}
	if data, err := os.ReadFile(out); err != nil || string(data) != stdout {
		t.Errorf("file differs from standard output: %v", err)
	}
	if _, _, err := runCLI(t, nil, goodEnv, "instructions", "-o", out, "../spec/testdata/full.yaml"); err == nil ||
		!strings.Contains(err.Error(), "already exists; use --force") {
		t.Errorf("overwrote without --force: %v", err)
	}
	if _, _, err := runCLI(t, nil, goodEnv, "instructions", "-o", out, "--force", "../spec/testdata/full.yaml"); err != nil {
		t.Errorf("--force: %v", err)
	}
}

func TestInstructionsRefusesAnInvalidSpec(t *testing.T) {
	path := writeTemp(t, "apiVersion: jira-space-config/v1alpha1\nkind: SpaceConfig\nmetadata: {name: x}\nspace: {key: lower}\n")
	_, stderr, err := runCLI(t, nil, goodEnv, "instructions", path)
	if err == nil || !strings.Contains(err.Error(), "1 error in") {
		t.Errorf("err = %v", err)
	}
	if !strings.Contains(stderr, "space.key") {
		t.Errorf("diagnostics not shown:\n%s", stderr)
	}
	if _, _, err := runCLI(t, nil, goodEnv, "instructions", filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Error("a missing file was not an error")
	}
	dir := t.TempDir()
	if _, _, err := runCLI(t, nil, goodEnv, "instructions", "-o", filepath.Join(dir, "x\x00"), "../spec/testdata/full.yaml"); err == nil {
		t.Error("a bad output path was not an error")
	}
}

// TestInstructionsAcceptsWhatOnlyTheAPICannotDo: an issue type above Epic is
// refused by validate (apply cannot create it) but set up by hand.
func TestInstructionsAcceptsWhatOnlyTheAPICannotDo(t *testing.T) {
	path := writeTemp(t, "apiVersion: jira-space-config/v1alpha1\nkind: SpaceConfig\nmetadata: {name: x}\n"+
		"issue_types:\n  - {name: Initiative, mode: ensure, level: \"2\"}\nspace: {key: T}\n")
	if _, _, err := runCLI(t, nil, goodEnv, "validate", path); err == nil {
		t.Fatal("validate accepted an issue type apply cannot create")
	}
	stdout, stderr, err := runCLI(t, nil, goodEnv, "instructions", path)
	if err != nil {
		t.Fatalf("instructions: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "| Create if missing (by hand only: apply cannot) | Initiative | Hierarchy level 2 |") {
		t.Errorf("not listed to be created by hand:\n%s", stdout)
	}
}
