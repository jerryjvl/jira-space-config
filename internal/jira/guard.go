package jira

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strings"
)

// ErrWriteBlocked is returned for any request the read-only guard refuses.
var ErrWriteBlocked = errors.New("blocked by the read-only guard")

// readOnlyPosts are the documented POST endpoints that only read. It must
// match the "Read-only POST allow-list" table in docs/api/inventory.md; a test
// checks that. Path parameters are written as {name}.
var readOnlyPosts = []string{
	"/rest/api/3/workflows",
	"/rest/api/3/workflows/create/validation",
	"/rest/api/3/workflows/update/validation",
	"/rest/api/3/workflowscheme/read",
	"/rest/api/3/workflowscheme/update/mappings",
	"/rest/api/3/field/{fieldId}/context/mapping",
	"/rest/api/3/permissions/check",
	"/rest/api/3/search/approximate-count",
	"/rest/api/3/priorityscheme/mappings",
	"/automation/public/jira/{cloudId}/rest/v1/rule/summary",
}

// gatewayPrefix is how paths start when a request goes through the Atlassian
// API gateway (https://api.atlassian.com/ex/jira/{cloudId}/rest/...), or to the
// automation API on the site (/gateway/api/automation/...).
var gatewayPrefix = regexp.MustCompile(`^(/ex/jira/[^/]+|/gateway/api)`)

// guard is the outermost http.RoundTripper of every client in this package.
// It refuses, before anything reaches the network:
//   - any host other than the allowed ones (so credentials cannot leak to
//     another host through a crafted "next page" link or similar);
//   - anything but https;
//   - any method other than GET, and POST to the read-only allow-list.
type guard struct {
	next  http.RoundTripper
	hosts map[string]bool
	posts []*regexp.Regexp
	// writes is the write plan of a Writer; nil for read-only clients, which
	// then refuse every write.
	writes *writePlan
}

func newGuard(next http.RoundTripper, hosts ...string) *guard {
	g := &guard{next: next, hosts: map[string]bool{}}
	for _, h := range hosts {
		g.hosts[strings.ToLower(h)] = true
	}
	for _, p := range readOnlyPosts {
		g.posts = append(g.posts, templateRegexp(p))
	}
	return g
}

// templateRegexp turns "/a/{id}/b" into ^/a/[^/]+/b$.
func templateRegexp(template string) *regexp.Regexp {
	parts := strings.Split(template, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
			parts[i] = `[^/]+`
		} else {
			parts[i] = regexp.QuoteMeta(p)
		}
	}
	return regexp.MustCompile("^" + strings.Join(parts, "/") + "$")
}

func (g *guard) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := g.check(req); err != nil {
		return nil, err
	}
	return g.next.RoundTrip(req)
}

func (g *guard) check(req *http.Request) error {
	if req.URL.Scheme != "https" {
		return fmt.Errorf("%w: %s %s is not https", ErrWriteBlocked, req.Method, req.URL.Redacted())
	}
	if !g.hosts[strings.ToLower(req.URL.Host)] {
		return fmt.Errorf("%w: host %q is not the target site", ErrWriteBlocked, req.URL.Host)
	}
	// A path that the server would normalise differently ("..", ".", "//",
	// or percent-encoded separators) could make an allowed-looking path reach
	// another endpoint, so only clean, unencoded paths are sent.
	if p := req.URL.Path; p == "" || path.Clean(p) != p || req.URL.RawPath != "" || strings.Contains(p, "%") {
		return fmt.Errorf("%w: path %q is not in canonical form", ErrWriteBlocked, req.URL.EscapedPath())
	}
	switch req.Method {
	case http.MethodGet:
		return nil
	case http.MethodPost:
		apiPath := gatewayPrefix.ReplaceAllString(req.URL.Path, "")
		for _, re := range g.posts {
			if re.MatchString(apiPath) {
				return nil
			}
		}
		if g.writes != nil {
			return g.writes.check(req)
		}
		return fmt.Errorf("%w: POST %s is not a documented read-only endpoint", ErrWriteBlocked, apiPath)
	default:
		return g.checkWrite(req)
	}
}

func (g *guard) checkWrite(req *http.Request) error {
	if g.writes == nil {
		return fmt.Errorf("%w: %s %s", ErrWriteBlocked, req.Method, req.URL.Path)
	}
	return g.writes.check(req)
}

// authenticator adds basic auth and fixed headers. It sits inside the guard,
// so it only ever sees requests the guard has already allowed.
type authenticator struct {
	next      http.RoundTripper
	email     string
	token     string
	userAgent string
}

func (a *authenticator) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.SetBasicAuth(a.email, a.token)
	r.Header.Set("Accept", "application/json")
	r.Header.Set("User-Agent", a.userAgent)
	if r.Body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	return a.next.RoundTrip(r)
}

// IsWrite reports whether a request may change Jira: anything but GET and
// the read-only POSTs (a workflow validation, say).
func IsWrite(method, urlPath string) bool {
	if method == http.MethodGet {
		return false
	}
	apiPath := gatewayPrefix.ReplaceAllString(urlPath, "")
	for _, p := range readOnlyPosts {
		if method == http.MethodPost && templateRegexp(p).MatchString(apiPath) {
			return false
		}
	}
	return true
}
