package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const canary = "CANARY-TOKEN-3a9f02"

// fakeSite serves canned responses keyed by request path.
func fakeSite(t *testing.T, responses map[string]string, status int) (*httptest.Server, http.RoundTripper) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := responses[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errorMessages":["not found"]}`))
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	return srv, &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test server only
	}
}

var whoamiResponses = map[string]string{
	"/_edge/tenant_info":           `{"cloudId":"cloud-123"}`,
	"/rest/api/3/myself":           `{"accountId":"acc-1","displayName":"Test User","emailAddress":"me@example.com","active":true}`,
	"/rest/api/3/serverInfo":       `{"deploymentType":"Cloud","version":"1001.0.0"}`,
	"/rest/api/3/instance/license": `{"applications":[{"id":"jira-software","plan":"FREE"}]}`,
	"/rest/api/3/mypermissions": `{"permissions":{
		"ADMINISTER":{"havePermission":true},
		"ADMINISTER_PROJECTS":{"havePermission":true},
		"BROWSE_PROJECTS":{"havePermission":false}}}`,
	"/rest/api/3/project/DEMO": `{"id":"10042","key":"DEMO","name":"Demo space","style":"classic"}`,
}

func runCLI(t *testing.T, transport http.RoundTripper, getenv func(string) string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err = execute(context.Background(), args, &out, &errOut, environment{getenv: getenv, transport: transport, terminal: func(any) bool { return false }})
	return out.String(), errOut.String(), err
}

func goodEnv(k string) string {
	return map[string]string{"JIRA_EMAIL": "me@example.com", "JIRA_API_TOKEN": canary}[k]
}

func TestWhoami(t *testing.T) {
	srv, transport := fakeSite(t, whoamiResponses, http.StatusOK)
	stdout, stderr, err := runCLI(t, transport, goodEnv, "whoami", "--site", srv.URL, "--space", "DEMO")
	if err != nil {
		t.Fatalf("whoami: %v\nstderr: %s", err, stderr)
	}
	for _, want := range []string{
		"cloud-123", "Test User (account acc-1)", "jira-software: FREE",
		`DEMO "Demo space" (company-managed, id 10042)`,
		"Administer Jira (global)", "Browse projects (space DEMO)",
		"Free plan", `Missing "Browse projects"`, "6 requests made, all read-only.",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout+stderr, canary) {
		t.Error("output contains the API token")
	}
}

// Administer Jira covers every read that would otherwise need Administer
// projects, so its absence is only reported when both are missing.
func TestWhoamiAdministerProjectsCoveredByAdministerJira(t *testing.T) {
	for _, tc := range []struct {
		adminJira bool
		wantNote  bool
	}{{adminJira: true, wantNote: false}, {adminJira: false, wantNote: true}} {
		responses := maps.Clone(whoamiResponses)
		responses["/rest/api/3/mypermissions"] = fmt.Sprintf(`{"permissions":{
			"ADMINISTER":{"havePermission":%t},
			"ADMINISTER_PROJECTS":{"havePermission":false},
			"BROWSE_PROJECTS":{"havePermission":true}}}`, tc.adminJira)
		srv, transport := fakeSite(t, responses, http.StatusOK)
		stdout, stderr, err := runCLI(t, transport, goodEnv, "whoami", "--site", srv.URL, "--space", "DEMO")
		if err != nil {
			t.Fatalf("whoami: %v\n%s", err, stderr)
		}
		if got := strings.Contains(stdout, `Missing "Administer projects"`); got != tc.wantNote {
			t.Errorf("Administer Jira=%t: note shown=%t, want %t\n%s", tc.adminJira, got, tc.wantNote, stdout)
		}
	}
}

func TestWhoamiUnauthorised(t *testing.T) {
	responses := map[string]string{"/_edge/tenant_info": `{"errorMessages":["Client must be authenticated"]}`}
	srv, transport := fakeSite(t, responses, http.StatusUnauthorized)
	stdout, stderr, err := runCLI(t, transport, goodEnv, "whoami", "--site", srv.URL)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(stderr, "401") || !strings.Contains(stderr, "Hint:") || !strings.Contains(stderr, "JIRA_API_TOKEN") {
		t.Errorf("stderr does not explain the failure:\n%s", stderr)
	}
	if strings.Contains(stdout+stderr, canary) {
		t.Error("output contains the API token")
	}
}

func TestWhoamiInputErrors(t *testing.T) {
	_, transport := fakeSite(t, whoamiResponses, http.StatusOK)
	tests := []struct {
		name   string
		getenv func(string) string
		args   []string
		want   string
	}{
		{"no site", goodEnv, []string{"whoami"}, `required flag(s) "site" not set`},
		{"http site", goodEnv, []string{"whoami", "--site", "http://example.atlassian.net"}, "must start with https://"},
		{"no credentials", func(string) string { return "" }, []string{"whoami", "--site", "https://example.atlassian.net"}, "JIRA_EMAIL is not set; JIRA_API_TOKEN is not set"},
		{"bad space", goodEnv, []string{"whoami", "--site", "https://example.atlassian.net", "--space", "demo/../x"}, "space key"},
		{"bad api", goodEnv, []string{"whoami", "--site", "https://example.atlassian.net", "--api", "other"}, "unknown API mode"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, err := runCLI(t, transport, tc.getenv, tc.args...)
			if err == nil || !strings.Contains(stderr, tc.want) {
				t.Errorf("err=%v stderr=%q; want it to mention %q", err, stderr, tc.want)
			}
			if strings.Contains(stderr, canary) {
				t.Error("output contains the API token")
			}
		})
	}
}

func TestWhoamiSpaceChecks(t *testing.T) {
	tests := []struct {
		name    string
		project string // response for /rest/api/3/project/DEMO; "" for 404
		args    []string
		wantErr string
		want    string
	}{
		{"space not found", "", []string{"--space", "DEMO"}, `space "DEMO" was not found, or this account cannot see it`, ""},
		{"team-managed space", `{"id":"10042","key":"DEMO","name":"Demo space","style":"next-gen"}`, []string{"--space", "DEMO"}, "",
			`Space DEMO is team-managed ("next-gen"); this tool only supports company-managed spaces.`},
		{"no space given", whoamiResponses["/rest/api/3/project/DEMO"], nil, "",
			"No --space given: space permissions show whether the account has them in at least one space."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			responses := maps.Clone(whoamiResponses)
			delete(responses, "/rest/api/3/project/DEMO")
			if tc.project != "" {
				responses["/rest/api/3/project/DEMO"] = tc.project
			}
			srv, transport := fakeSite(t, responses, http.StatusOK)
			stdout, stderr, err := runCLI(t, transport, goodEnv, append([]string{"whoami", "--site", srv.URL}, tc.args...)...)
			switch {
			case tc.wantErr != "":
				if err == nil || !strings.Contains(stderr, tc.wantErr) {
					t.Errorf("err = %v, stderr:\n%s", err, stderr)
				}
			case err != nil:
				t.Fatalf("whoami: %v\n%s", err, stderr)
			case !strings.Contains(stdout, tc.want):
				t.Errorf("output lacks %q:\n%s", tc.want, stdout)
			}
		})
	}
}
