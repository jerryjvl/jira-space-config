package jiratest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// ManifestFile lists the recorded requests in a fixture directory.
const ManifestFile = "routes.json"

// Recorder is an http.RoundTripper that passes requests on and saves each
// response body to a directory, with a manifest of the request that produced
// it. It sits inside the read-only client's guard, so it only ever sees
// requests the guard allowed. Request headers (credentials) are never saved.
type Recorder struct {
	Next http.RoundTripper
	Dir  string

	mu     sync.Mutex
	routes []Route
	names  map[string]bool
}

func (r *Recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		var err error
		if body, err = io.ReadAll(req.Body); err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	resp, err := r.Next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(data))

	r.mu.Lock()
	defer r.mu.Unlock()
	route := Route{Method: req.Method, Path: req.URL.Path, Query: req.URL.Query(), Body: strings.TrimSpace(string(body))}
	if resp.StatusCode != http.StatusOK {
		route.Status = resp.StatusCode
	}
	for _, seen := range r.routes {
		if seen.Method == route.Method && seen.Path == route.Path && seen.Query.Encode() == route.Query.Encode() && seen.Body == route.Body {
			return resp, nil // recorded already; one response per request is replayed
		}
	}
	route.File = r.fileName(route)
	if err := os.WriteFile(filepath.Join(r.Dir, route.File), data, 0o600); err != nil {
		return nil, err
	}
	r.routes = append(r.routes, route)
	return resp, nil
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// fileName derives a readable, unique file name from a request.
func (r *Recorder) fileName(rt Route) string {
	if r.names == nil {
		r.names = map[string]bool{}
	}
	p := strings.TrimPrefix(rt.Path, "/rest/api/3/")
	p = strings.TrimPrefix(p, "/")
	name := strings.ToLower(rt.Method) + "_" + unsafeChars.ReplaceAllString(p, "_")
	if q := rt.Query.Encode(); q != "" || rt.Body != "" {
		sum := sha256.Sum256([]byte(q + "\n" + rt.Body))
		name += "_" + hex.EncodeToString(sum[:4])
	}
	if len(name) > 120 {
		name = name[:120]
	}
	base := name
	for i := 2; r.names[name]; i++ {
		name = fmt.Sprintf("%s_%d", base, i)
	}
	r.names[name] = true
	return name + ".json"
}

// WriteManifest saves the manifest, sorted for stable diffs.
func (r *Recorder) WriteManifest() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	routes := append([]Route(nil), r.routes...)
	sort.SliceStable(routes, func(i, j int) bool { return routes[i].File < routes[j].File })
	data, err := json.MarshalIndent(routes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.Dir, ManifestFile), append(data, '\n'), 0o600)
}

// LoadRoutes reads a fixture directory's manifest. Every recorded query
// parameter is required to match, including paging's startAt, so a
// multi-page response replays page by page.
func LoadRoutes(dir string) ([]Route, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFile)) //nolint:gosec // G304: test fixture directories named by tests
	if err != nil {
		return nil, err
	}
	var routes []Route
	if err := json.Unmarshal(data, &routes); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	return routes, nil
}
