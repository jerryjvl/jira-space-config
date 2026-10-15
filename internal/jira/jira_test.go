package jira

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jerryjvl/jira-space-config/internal/credentials"
)

const canary = "CANARY-TOKEN-91b7e4"

// fakeJira is a TLS test server that records every request it receives.
type fakeJira struct {
	*httptest.Server
	mu       sync.Mutex
	requests []*http.Request
}

func newFakeJira(t *testing.T, handler http.HandlerFunc) *fakeJira {
	t.Helper()
	f := &fakeJira{}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Clone(context.Background()))
		f.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeJira) seen() []*http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

// transport sends every request, whatever its host, to the fake server. That
// lets tests exercise the gateway host without touching the network.
func (f *fakeJira) transport() http.RoundTripper {
	addr := f.Listener.Addr().String()
	return &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test server only
	}
}

func (f *fakeJira) site(t *testing.T) Site {
	t.Helper()
	s, err := ParseSite(f.URL)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newTestClient(t *testing.T, f *fakeJira, mutate func(*Options)) *Client {
	t.Helper()
	opts := Options{
		Site:        f.site(t),
		Credentials: credentials.Credentials{Email: "me@example.com", Token: credentials.NewSecret(canary)},
		UserAgent:   "jira-space-config/test",
		Transport:   f.transport(),
		Sleep:       func(context.Context, time.Duration) error { return nil },
	}
	if mutate != nil {
		mutate(&opts)
	}
	c, err := New(context.Background(), opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestParseSite(t *testing.T) {
	good := map[string]string{
		"https://example.atlassian.net":   "https://example.atlassian.net",
		"https://Example.Atlassian.net/":  "https://example.atlassian.net",
		" https://example.atlassian.net ": "https://example.atlassian.net",
	}
	for in, want := range good {
		s, err := ParseSite(in)
		if err != nil || s.String() != want {
			t.Errorf("ParseSite(%q) = %q, %v; want %q", in, s, err, want)
		}
	}
	bad := map[string]string{
		"":                                    "site is empty",
		"https://exa mple.atlassian.net":      "is not a valid URL",
		"example.atlassian.net":               "must start with https://",
		"http://example.atlassian.net":        "must start with https://",
		"https://":                            "has no host name",
		"https://me:pw@example.atlassian.net": "must not contain credentials",
		"https://example.atlassian.net/jira":  "without a path",
		"https://example.atlassian.net?x=1":   "without a path",
		"https://example.atlassian.net#top":   "without a path",
	}
	for in, want := range bad {
		if _, err := ParseSite(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseSite(%q) error = %v; want one containing %q", in, err, want)
		}
	}
}

func TestGuard(t *testing.T) {
	g := newGuard(http.DefaultTransport, "example.atlassian.net", gatewayHost)
	tests := []struct {
		method, url string
		allowed     bool
	}{
		{"GET", "https://example.atlassian.net/rest/api/3/myself", true},
		{"GET", "https://api.atlassian.com/ex/jira/abc/rest/api/3/myself", true},
		{"POST", "https://example.atlassian.net/rest/api/3/workflows", true},
		{"POST", "https://example.atlassian.net/rest/api/3/field/customfield_1/context/mapping", true},
		{"POST", "https://api.atlassian.com/ex/jira/abc/rest/api/3/search/approximate-count", true},

		{"POST", "https://example.atlassian.net/rest/api/3/workflows/create", false},
		{"POST", "https://example.atlassian.net/rest/api/3/issue", false},
		{"POST", "https://example.atlassian.net/rest/api/3/field/a/b/context/mapping", false},
		{"PUT", "https://example.atlassian.net/rest/api/3/project/X", false},
		{"DELETE", "https://example.atlassian.net/rest/api/3/screens/1", false},
		{"PATCH", "https://example.atlassian.net/rest/api/3/myself", false},
		{"HEAD", "https://example.atlassian.net/rest/api/3/myself", false},
		{"GET", "https://other.atlassian.net/rest/api/3/myself", false},
		{"GET", "https://evil.example.com/rest/api/3/myself", false},
		{"GET", "http://example.atlassian.net/rest/api/3/myself", false},
		{"POST", "https://example.atlassian.net/rest/api/3/workflows/../issue", false},
		{"POST", "https://example.atlassian.net/rest/api/3/workflows/./", false},
		{"POST", "https://example.atlassian.net//rest/api/3/workflows", false},
		{"POST", "https://example.atlassian.net/rest/api/3/workflows%2F..%2Fissue", false},
		{"GET", "https://example.atlassian.net/rest/api/3/a/../b", false},
	}
	for _, tc := range tests {
		req, err := http.NewRequestWithContext(context.Background(), tc.method, tc.url, nil)
		if err != nil {
			t.Fatal(err)
		}
		err = g.check(req)
		if tc.allowed && err != nil {
			t.Errorf("%s %s blocked: %v", tc.method, tc.url, err)
		}
		if !tc.allowed && !errors.Is(err, ErrWriteBlocked) {
			t.Errorf("%s %s allowed; want ErrWriteBlocked", tc.method, tc.url)
		}
	}
}

// The code's allow-list must be exactly the documented one.
func TestReadOnlyPostsMatchInventory(t *testing.T) {
	data, err := os.ReadFile("../../docs/api/inventory.md")
	if err != nil {
		t.Fatal(err)
	}
	section := regexp.MustCompile(`(?s)## Read-only POST allow-list\n(.*?)\n## `).FindSubmatch(data)
	if section == nil {
		t.Fatal("inventory has no “Read-only POST allow-list” section")
	}
	var documented []string
	for _, m := range regexp.MustCompile("`POST (/[^`]+)`").FindAllSubmatch(section[1], -1) {
		documented = append(documented, string(m[1]))
	}
	got, want := slices.Sorted(slices.Values(readOnlyPosts)), slices.Sorted(slices.Values(documented))
	if !slices.Equal(got, want) {
		t.Errorf("readOnlyPosts = %v\ninventory      = %v", got, want)
	}
}

func TestBlockedRequestNeverReachesTheServer(t *testing.T) {
	f := newFakeJira(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	c := newTestClient(t, f, nil)
	err := c.PostRead(context.Background(), "/rest/api/3/workflows/create", nil, map[string]any{}, nil)
	if !errors.Is(err, ErrWriteBlocked) {
		t.Fatalf("got %v; want ErrWriteBlocked", err)
	}
	if n := len(f.seen()); n != 0 {
		t.Errorf("server saw %d requests; want 0", n)
	}
}

func TestGetSendsAuthAndDecodes(t *testing.T) {
	f := newFakeJira(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/myself" || r.URL.Query().Get("expand") != "groups" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"accountId":"abc","displayName":"Test User","extra":1}`))
	})
	c := newTestClient(t, f, nil)
	var out struct {
		AccountID   string `json:"accountId"`
		DisplayName string `json:"displayName"`
	}
	if err := c.Get(context.Background(), "/rest/api/3/myself", url.Values{"expand": {"groups"}}, &out); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if out.AccountID != "abc" || out.DisplayName != "Test User" {
		t.Errorf("decoded %+v", out)
	}
	r := f.seen()[0]
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("me@example.com:"+canary))
	if r.Header.Get("Authorization") != want {
		t.Error("Authorization header is not basic auth with email and token")
	}
	if r.Header.Get("User-Agent") != "jira-space-config/test" || r.Header.Get("Accept") != "application/json" {
		t.Errorf("headers: %v", r.Header)
	}
	if c.Requests() != 1 {
		t.Errorf("Requests() = %d", c.Requests())
	}
}

func TestErrorsExplainAndNeverLeakToken(t *testing.T) {
	tests := []struct {
		status   int
		body     string
		wantMsg  string
		wantHint string
	}{
		{401, `{"errorMessages":["Client must be authenticated"]}`, "Client must be authenticated", "JIRA_API_TOKEN"},
		{403, `{"errorMessages":[],"errors":{"projectKey":"no permission"}}`, "projectKey: no permission", "permission"},
		{404, `{"errorMessages":["No project could be found with key 'X'."]}`, "No project", "--space"},
		{500, `oops`, "oops", "status.atlassian.com"},
		{302, ``, "redirected to", "--site"},
	}
	for _, tc := range tests {
		f := newFakeJira(t, func(w http.ResponseWriter, _ *http.Request) {
			if tc.status == 302 {
				w.Header().Set("Location", "https://id.atlassian.com/login")
			}
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		})
		c := newTestClient(t, f, nil)
		err := c.Get(context.Background(), "/rest/api/3/project/X", nil, nil)
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("%d: got %v; want *APIError", tc.status, err)
		}
		if apiErr.Status != tc.status || !strings.Contains(err.Error(), tc.wantMsg) {
			t.Errorf("%d: error %q does not contain %q", tc.status, err, tc.wantMsg)
		}
		if !strings.Contains(apiErr.Hint(), tc.wantHint) {
			t.Errorf("%d: hint %q does not mention %q", tc.status, apiErr.Hint(), tc.wantHint)
		}
		if strings.Contains(err.Error()+apiErr.Hint(), canary) {
			t.Errorf("%d: error leaks the token", tc.status)
		}
		if len(f.seen()) != 1 {
			t.Errorf("%d: %d requests; redirects must not be followed and errors not retried", tc.status, len(f.seen()))
		}
	}
}

func TestRateLimitRetries(t *testing.T) {
	calls := 0
	var waits []time.Duration
	f := newFakeJira(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	c := newTestClient(t, f, func(o *Options) {
		o.Sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	})
	if err := c.Get(context.Background(), "/rest/api/3/myself", nil, &struct{}{}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if calls != 3 || !slices.Equal(waits, []time.Duration{7 * time.Second, 7 * time.Second}) {
		t.Errorf("calls=%d waits=%v", calls, waits)
	}
}

func TestRateLimitGivesUp(t *testing.T) {
	f := newFakeJira(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) })
	c := newTestClient(t, f, nil)
	err := c.Get(context.Background(), "/rest/api/3/myself", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 429 || len(f.seen()) != maxAttempts {
		t.Errorf("err=%v requests=%d", err, len(f.seen()))
	}
}

func TestBudget(t *testing.T) {
	f := newFakeJira(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	c := newTestClient(t, f, func(o *Options) { o.Budget = 2 })
	for i := 0; i < 2; i++ {
		if err := c.Get(context.Background(), "/rest/api/3/myself", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	err := c.Get(context.Background(), "/rest/api/3/myself", nil, nil)
	if !errors.Is(err, ErrBudgetExceeded) || !strings.Contains(err.Error(), "--max-requests") {
		t.Errorf("got %v", err)
	}
	if len(f.seen()) != 2 {
		t.Errorf("server saw %d requests; want 2", len(f.seen()))
	}
}

func TestGatewayLooksUpCloudIDAndRoutesThroughGateway(t *testing.T) {
	f := newFakeJira(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_edge/tenant_info":
			_, _ = w.Write([]byte(`{"cloudId":"cloud-123"}`))
		case r.Host == gatewayHost && r.URL.Path == "/ex/jira/cloud-123/rest/api/3/myself":
			_, _ = w.Write([]byte(`{"accountId":"abc"}`))
		default:
			http.NotFound(w, r)
		}
	})
	c := newTestClient(t, f, func(o *Options) { o.API = APIGateway })
	if c.BaseURL() != "https://api.atlassian.com/ex/jira/cloud-123" {
		t.Errorf("BaseURL = %s", c.BaseURL())
	}
	if err := c.Get(context.Background(), "/rest/api/3/myself", nil, &struct{}{}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	reqs := f.seen()
	if reqs[0].Host != f.site(t).Host() || reqs[1].Host != gatewayHost {
		t.Errorf("hosts: %s, %s", reqs[0].Host, reqs[1].Host)
	}
}

func TestPostReadAllPages(t *testing.T) {
	var starts []string
	f := newFakeJira(t, func(w http.ResponseWriter, r *http.Request) {
		starts = append(starts, r.URL.Query().Get("startAt"))
		if r.URL.Query().Get("startAt") == "0" {
			_, _ = w.Write([]byte(`{"isLast":false,"values":[{"contextId":"1"},{"contextId":"2"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"isLast":true,"values":[{"contextId":"3"}]}`))
	})
	c := newTestClient(t, f, nil)
	type v struct {
		ContextID string `json:"contextId"`
	}
	got, err := PostReadAll[v](context.Background(), c, "/rest/api/3/field/customfield_1/context/mapping", map[string]any{"mappings": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[2].ContextID != "3" || strings.Join(starts, ",") != "0,2" {
		t.Errorf("got %v after pages starting at %v", got, starts)
	}
}

func TestNewRefusesMissingInputs(t *testing.T) {
	site, err := ParseSite("https://example.atlassian.net")
	if err != nil {
		t.Fatal(err)
	}
	creds := credentials.Credentials{Email: "me@example.com", Token: credentials.NewSecret(canary)}
	tests := map[string]struct {
		opts Options
		want string
	}{
		"no site":  {Options{Credentials: creds}, "no site given"},
		"no token": {Options{Site: site, Credentials: credentials.Credentials{Email: "me@example.com"}}, "no API token given"},
		"bad API":  {Options{Site: site, Credentials: creds, API: "direct"}, `unknown API mode "direct"`},
	}
	for name, tc := range tests {
		if _, err := New(context.Background(), tc.opts); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want one containing %q", name, err, tc.want)
		}
	}
}

func TestGatewayNeedsACloudID(t *testing.T) {
	tests := map[string]struct {
		handler http.HandlerFunc
		want    string
	}{
		"tenant info fails": {func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
			"looking up the cloud ID for the gateway: GET /_edge/tenant_info: 404"},
		"no cloud ID": {func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) },
			"did not return a cloud ID; is this a Jira Cloud site?"},
	}
	for name, tc := range tests {
		f := newFakeJira(t, tc.handler)
		_, err := New(context.Background(), Options{
			Site:        f.site(t),
			Credentials: credentials.Credentials{Email: "me@example.com", Token: credentials.NewSecret(canary)},
			API:         APIGateway,
			Transport:   f.transport(),
		})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want one containing %q", name, err, tc.want)
		}
	}
}

// TestBrokenResponses: answers that are not what an API client expects are
// reported as such, never decoded into empty data.
func TestBrokenResponses(t *testing.T) {
	tests := map[string]struct {
		handler http.HandlerFunc
		want    string
	}{
		"sign-in page instead of JSON": {func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>Log in</html>"))
		}, "unexpected response (not the JSON this tool expects)"},
		"connection drops mid-body": {func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "1000")
			_, _ = w.Write([]byte(`{"accountId":`))
			w.(http.Flusher).Flush() // the headers and part of the body arrive; the rest never does
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
		}, "could not reach Jira"},
	}
	for name, tc := range tests {
		f := newFakeJira(t, tc.handler)
		c := newTestClient(t, f, nil)
		var out map[string]any
		if err := c.Get(context.Background(), "/rest/api/3/myself", nil, &out); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want one containing %q", name, err, tc.want)
		}
	}
}

func TestUnreachableSite(t *testing.T) {
	f := newFakeJira(t, func(http.ResponseWriter, *http.Request) {})
	c := newTestClient(t, f, nil)
	f.Close()
	err := c.Get(context.Background(), "/rest/api/3/myself", nil, nil)
	var te *TransportError
	if !errors.As(err, &te) || !strings.Contains(err.Error(), "GET /rest/api/3/myself: could not reach Jira") {
		t.Errorf("error = %v, want a TransportError naming the request", err)
	}
}

func TestCancelledWhileWaitingToRetry(t *testing.T) {
	f := newFakeJira(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) })
	c := newTestClient(t, f, func(o *Options) {
		o.Sleep = func(context.Context, time.Duration) error { return context.Canceled }
	})
	if err := c.Get(context.Background(), "/rest/api/3/myself", nil, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if n := len(f.seen()); n != 1 {
		t.Errorf("%d requests, want 1 (no retry after cancelling)", n)
	}
}

func TestIsWrite(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		want         bool
	}{
		{"GET", "/rest/api/3/workflows/search", false},
		{"POST", "/rest/api/3/workflows/create/validation", false},
		{"POST", "/rest/api/3/workflows/create", true},
		{"PUT", "/rest/api/3/workflows/update/validation", true},
		{"DELETE", "/rest/api/3/screens/1", true},
	} {
		if got := IsWrite(tc.method, tc.path); got != tc.want {
			t.Errorf("%s %s: %v", tc.method, tc.path, got)
		}
	}
}
