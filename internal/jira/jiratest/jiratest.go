// Package jiratest serves recorded Jira responses to tests, through the real
// read-only client, so tests exercise the same guard and decoding as live
// runs without touching the network.
package jiratest

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jerryjvl/jira-space-config/internal/credentials"
	"github.com/jerryjvl/jira-space-config/internal/jira"
)

// Route maps one request to a fixture file. Query lists the parameters the
// request must carry (others, such as startAt=0, are ignored). A non-zero
// Status answers with that status: with File's content (a recorded error
// response), or with a generic Jira-style error when File is empty.
type Route struct {
	Method string     `json:"method,omitempty"` // GET if empty; POST only for the documented read-only POSTs
	Path   string     `json:"path"`
	Query  url.Values `json:"query,omitempty"`
	Body   string     `json:"body,omitempty"` // POST: the exact JSON body expected
	File   string     `json:"file"`
	Status int        `json:"status,omitempty"`
}

// Server is a fake Jira serving fixtures from one directory.
type Server struct {
	t      *testing.T
	dir    string
	routes []Route
	srv    *httptest.Server

	mu   sync.Mutex
	seen []string
	used map[int]bool // indexes of routes that served a request
}

// New starts a fake Jira. Any request without a matching route fails the
// test, so a change that adds requests is noticed.
func New(t *testing.T, dir string, routes []Route) *Server {
	t.Helper()
	s := &Server{t: t, dir: dir, routes: routes, used: map[int]bool{}}
	s.srv = httptest.NewTLSServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.seen = append(s.seen, r.Method+" "+r.URL.RequestURI())
	s.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	var expected []string // bodies of POST routes for this path, when none matched
	for i, rt := range s.routes {
		method := rt.Method
		if method == "" {
			method = http.MethodGet
		}
		if method != r.Method || rt.Path != r.URL.Path || !matches(rt.Query, r.URL.Query()) {
			continue
		}
		if method == http.MethodPost && strings.TrimSpace(string(body)) != rt.Body {
			expected = append(expected, rt.Body)
			continue
		}
		s.mu.Lock()
		s.used[i] = true
		s.mu.Unlock()
		if rt.Status != 0 && rt.File == "" {
			w.WriteHeader(rt.Status)
			_, _ = w.Write([]byte(`{"errorMessages":["fixture status"]}`))
			return
		}
		data, err := os.ReadFile(filepath.Join(s.dir, rt.File))
		if err != nil {
			s.t.Errorf("fixture %s: %v", rt.File, err)
			http.Error(w, "missing fixture", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if rt.Status != 0 {
			w.WriteHeader(rt.Status) // a recorded error response
		}
		_, _ = w.Write(data)
		return
	}
	if len(expected) > 0 {
		s.t.Errorf("fake Jira: POST %s with body %s; the fixtures expect %s", r.URL.Path, body, strings.Join(expected, " or "))
	} else {
		s.t.Errorf("fake Jira has no fixture for %s %s", r.Method, r.URL.RequestURI())
	}
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"errorMessages":["no fixture"]}`))
}

func matches(want, got url.Values) bool {
	for k, vs := range want {
		if strings.Join(got[k], ",") != strings.Join(vs, ",") {
			return false
		}
	}
	return true
}

// Client returns a read-only client for the fake site.
func (s *Server) Client() *jira.Client {
	s.t.Helper()
	c, err := jira.New(context.Background(), s.Options())
	if err != nil {
		s.t.Fatal(err)
	}
	return c
}

// Options returns the options that reach the fake site, for clients a test
// does not create itself (the apply runner's writer).
func (s *Server) Options() jira.Options {
	s.t.Helper()
	site, err := jira.ParseSite(s.srv.URL)
	if err != nil {
		s.t.Fatal(err)
	}
	addr := s.srv.Listener.Addr().String()
	return jira.Options{
		Site:        site,
		Credentials: credentials.Credentials{Email: "admin@example.com", Token: credentials.NewSecret("test-token")},
		UserAgent:   "jira-space-config/test",
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test server only
		},
		Sleep: func(context.Context, time.Duration) error { return nil },
	}
}

// URL is the fake site's address.
func (s *Server) URL() string { return s.srv.URL }

// Requests returns the requests received, sorted, as "METHOD /path?query".
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]string(nil), s.seen...)
	sort.Strings(out)
	return out
}

// Unused returns the routes that no request reached, so tests can require
// that fixtures and the requests made match exactly.
func (s *Server) Unused() []Route {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Route
	for i, rt := range s.routes {
		if !s.used[i] {
			out = append(out, rt)
		}
	}
	return out
}
