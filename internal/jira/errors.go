package jira

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// APIError is a non-success response from Jira. It holds the request method
// and path and Jira's own messages; never headers or credentials.
type APIError struct {
	Method   string
	Path     string
	Status   int
	Messages []string
}

func newAPIError(method, apiPath string, resp *http.Response, body []byte) *APIError {
	e := &APIError{Method: method, Path: apiPath, Status: resp.StatusCode}
	var parsed struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
		Message       string            `json:"message"`
	}
	if json.Unmarshal(body, &parsed) == nil {
		e.Messages = append(e.Messages, parsed.ErrorMessages...)
		keys := make([]string, 0, len(parsed.Errors))
		for k := range parsed.Errors {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			e.Messages = append(e.Messages, k+": "+parsed.Errors[k])
		}
		if parsed.Message != "" {
			e.Messages = append(e.Messages, parsed.Message)
		}
	}
	if len(e.Messages) == 0 {
		if text := strings.Join(strings.Fields(string(body)), " "); text != "" && !strings.HasPrefix(text, "<") {
			e.Messages = append(e.Messages, truncate(text, 200))
		}
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		e.Messages = append(e.Messages, "redirected to "+resp.Header.Get("Location"))
	}
	return e
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("%s %s: %d %s", e.Method, e.Path, e.Status, http.StatusText(e.Status))
	if len(e.Messages) > 0 {
		msg += ": " + strings.Join(e.Messages, "; ")
	}
	return msg
}

// Hint explains the likely cause and the fix, for the operator.
func (e *APIError) Hint() string {
	switch {
	case e.Status == http.StatusUnauthorized:
		return "Jira did not accept the credentials. Check JIRA_EMAIL and JIRA_API_TOKEN, and that the token " +
			"has not expired (tokens last at most a year). A scoped token only works with --api=gateway."
	case e.Status == http.StatusForbidden:
		return "The account is signed in but lacks a permission this request needs. " +
			"Run 'whoami' to see which permissions the account has."
	case e.Status == http.StatusNotFound:
		return "Jira reports that this does not exist, or that the account may not see it. " +
			"Check the --site and --space values, and the account's permissions."
	case e.Status == http.StatusTooManyRequests:
		return "Jira is rate-limiting this account. Wait a few minutes and run the command again."
	case e.Status >= 300 && e.Status < 400:
		return "Jira redirected the request, which the API does not normally do. " +
			"Check that --site is the site's own address (https://<name>.atlassian.net)."
	case e.Status >= 500:
		return "Jira had an internal problem. Wait and try again; if it persists, check https://status.atlassian.com."
	}
	return ""
}

// TransportError is a failure to get any response at all (network, TLS, timeout).
type TransportError struct {
	Method string
	Path   string
	Err    error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("%s %s: could not reach Jira: %v", e.Method, e.Path, e.Err)
}

func (e *TransportError) Unwrap() error { return e.Err }

// Hint explains the likely cause and the fix.
func (e *TransportError) Hint() string {
	return "Check the network connection and the --site address."
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
