package credentials

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const canary = "CANARY-TOKEN-6f1d2c"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad(t *testing.T) {
	creds, err := Load(env(map[string]string{EnvEmail: " me@example.com ", EnvToken: canary + "\n"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if creds.Email != "me@example.com" {
		t.Errorf("email = %q", creds.Email)
	}
	if got, _ := creds.Token.Reveal(); got != canary {
		t.Errorf("token not trimmed or not kept: got %d chars", len(got))
	}
}

func TestLoadReportsAllProblemsWithoutValues(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{"nothing set", map[string]string{}, []string{"JIRA_EMAIL is not set", "JIRA_API_TOKEN is not set"}},
		{"quoted token", map[string]string{EnvEmail: "me@example.com", EnvToken: `"` + canary + `"`}, []string{"JIRA_API_TOKEN is wrapped in quotes"}},
		{"bad email", map[string]string{EnvEmail: "me", EnvToken: canary}, []string{"does not look like an email"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(env(tc.env))
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not mention %q", err, w)
				}
			}
			if strings.Contains(err.Error(), canary) {
				t.Errorf("error leaks the token: %q", err)
			}
		})
	}
}

func TestSecretNeverPrints(t *testing.T) {
	s := NewSecret(canary)
	holder := struct {
		Email string
		Token Secret
	}{"me@example.com", s}

	outputs := map[string]string{}
	for _, verb := range []string{"%s", "%v", "%+v", "%#v", "%q", "%x", "%X", "%d", "%T"} {
		outputs["Sprintf "+verb] = fmt.Sprintf(verb, s)
		outputs["Sprintf struct "+verb] = fmt.Sprintf(verb, holder)
		outputs["Sprintf pointer "+verb] = fmt.Sprintf(verb, &holder)
	}
	outputs["Sprint"] = fmt.Sprint(s, holder)
	outputs["String"] = s.String()
	j, err := json.Marshal(holder)
	if err != nil {
		t.Fatal(err)
	}
	outputs["json"] = string(j)
	outputs["error wrap"] = fmt.Errorf("failed with %v", s).Error()

	for name, out := range outputs {
		if strings.Contains(out, canary) {
			t.Errorf("%s leaks the token: %q", name, out)
		}
	}
}

// fmt prints unexported fields by reflection, bypassing Secret's methods.
// This guards the closure representation that keeps those prints safe.
func TestSecretInUnexportedFieldNeverPrints(t *testing.T) {
	type wrapper struct{ c Credentials }
	w := wrapper{Credentials{Email: "me@example.com", Token: NewSecret(canary)}}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
		if out := fmt.Sprintf(verb, w); strings.Contains(out, canary) {
			t.Errorf("%s leaks the token: %q", verb, out)
		}
		if out := fmt.Sprintf(verb, &w); strings.Contains(out, canary) {
			t.Errorf("%s on pointer leaks the token: %q", verb, out)
		}
	}
}

func TestRevealEmpty(t *testing.T) {
	if _, err := (Secret{}).Reveal(); err == nil {
		t.Error("expected an error revealing an empty secret")
	}
}
