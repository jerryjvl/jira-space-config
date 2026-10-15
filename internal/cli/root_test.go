package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Execute([]string{"--version"}, &stdout, &stderr); err != nil {
		t.Fatalf("Execute: %v (stderr: %q)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), version) {
		t.Errorf("version output %q does not contain %q", stdout.String(), version)
	}
}

func TestUnknownCommandFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Execute([]string{"no-such-command"}, &stdout, &stderr); err == nil {
		t.Fatal("expected an error for an unknown command")
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Errorf("stderr %q does not explain the error", stderr.String())
	}
}
