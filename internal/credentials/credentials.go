// Package credentials reads the Jira account email and API token from the
// environment. The token is wrapped in Secret so that it cannot be printed,
// formatted, logged or serialised by accident.
package credentials

import (
	"errors"
	"fmt"
	"strings"
)

// Environment variable names. These are the only place credentials come from.
const (
	EnvEmail = "JIRA_EMAIL"
	EnvToken = "JIRA_API_TOKEN" //nolint:gosec // G101: the variable name, not a credential
)

// Credentials are the account email and API token used for HTTP basic auth.
type Credentials struct {
	Email string
	Token Secret
}

// Load reads credentials using getenv (normally os.Getenv). It reports every
// problem it finds at once, naming the variable but never echoing a value.
func Load(getenv func(string) string) (Credentials, error) {
	email := strings.TrimSpace(getenv(EnvEmail))
	token := strings.TrimSpace(getenv(EnvToken))

	var problems []string
	if email == "" {
		problems = append(problems, EnvEmail+" is not set")
	} else if quoted(email) {
		problems = append(problems, EnvEmail+" is wrapped in quotes; remove them")
	} else if !strings.Contains(email, "@") {
		problems = append(problems, EnvEmail+" does not look like an email address")
	}
	if token == "" {
		problems = append(problems, EnvToken+" is not set")
	} else if quoted(token) {
		problems = append(problems, EnvToken+" is wrapped in quotes; remove them")
	}
	if len(problems) > 0 {
		return Credentials{}, fmt.Errorf("credentials: %s (set %s and %s in the environment; "+
			"create a token at https://id.atlassian.com/manage-profile/security/api-tokens)",
			strings.Join(problems, "; "), EnvEmail, EnvToken)
	}
	return Credentials{Email: email, Token: NewSecret(token)}, nil
}

func quoted(s string) bool {
	return len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0]
}

// Secret holds a value that must never appear in output. Every way Go can
// turn it into text (String, GoString, fmt verbs, JSON, text marshalling)
// yields a fixed placeholder. The value is only reachable through Reveal.
//
// The value is kept inside a closure rather than a string field. When fmt
// prints a struct that holds a Secret in an unexported field, it uses
// reflection and skips the methods below; a closure then prints as a memory
// address, whereas a string field would print the token itself.
type Secret struct {
	value func() string
}

const redacted = "[redacted]"

// ErrNoSecret is returned by Reveal on an empty Secret.
var ErrNoSecret = errors.New("secret is empty")

// NewSecret wraps a value.
func NewSecret(value string) Secret { return Secret{value: func() string { return value }} }

// Reveal returns the value. It is called in exactly one place: where the
// HTTP Authorization header is built.
func (s Secret) Reveal() (string, error) {
	if s.value == nil || s.value() == "" {
		return "", ErrNoSecret
	}
	return s.value(), nil
}

// String implements fmt.Stringer.
func (Secret) String() string { return redacted }

// GoString implements fmt.GoStringer, covering %#v.
func (Secret) GoString() string { return redacted }

// Format implements fmt.Formatter, covering every verb (%s, %v, %q, %x, …).
func (Secret) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redacted)) }

// MarshalText implements encoding.TextMarshaler (used by JSON and YAML encoders).
func (Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// MarshalJSON implements json.Marshaler.
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }
