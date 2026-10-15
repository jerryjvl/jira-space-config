package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

// Writer is the only client that can change Jira. It reads like a Client,
// and sends a write only when the step being carried out planned it
// (Allow): the method and path must match a planned write, and the request
// must not name another space in its path or body. Only the apply runner creates one; a test checks that.
type Writer struct {
	*Client
	plan *writePlan
}

// NewWriter returns a Writer for one space. It starts with no write
// allowed.
func NewWriter(ctx context.Context, opts Options, spaceKey, spaceID string) (*Writer, error) {
	c, err := New(ctx, opts)
	if err != nil {
		return nil, err
	}
	plan := &writePlan{key: spaceKey, id: spaceID}
	guard, ok := c.http.Transport.(*guard)
	if !ok {
		return nil, fmt.Errorf("jira: client without its guard") // checkcover:unreachable New always installs the guard
	}
	guard.writes = plan
	return &Writer{Client: c, plan: plan}, nil
}

// Allow adds a planned write: a method and a path template, with path
// parameters as {name} ("POST /rest/api/3/screens/{screenId}/tabs").
func (w *Writer) Allow(method, pathTemplate string) {
	w.plan.mu.Lock()
	defer w.plan.mu.Unlock()
	w.plan.allowed = append(w.plan.allowed, plannedWrite{method, templateRegexp(pathTemplate)})
}

// Reset forgets every planned write, so writes planned for one step cannot
// be sent in the next.
func (w *Writer) Reset() {
	w.plan.mu.Lock()
	defer w.plan.mu.Unlock()
	w.plan.allowed = nil
}

// Send sends one write and decodes the response into out (which may be nil).
// A write is never retried after Jira answered it, except on rate limiting
// (429), when Jira did not process it: retrying anything else could make it
// twice. Cancelling ctx does not interrupt a write that has started.
func (w *Writer) Send(ctx context.Context, method, apiPath string, query url.Values, body, out any) error {
	var data []byte
	if body != nil {
		var err error
		if data, err = json.Marshal(body); err != nil {
			return fmt.Errorf("encoding request body for %s %s: %w", method, apiPath, err)
		}
	}
	return w.do(context.WithoutCancel(ctx), method, w.resolve(apiPath, query), data, out)
}

type plannedWrite struct {
	method string
	path   *regexp.Regexp
}

// writePlan holds the writes a Writer may send now.
type writePlan struct {
	key, id string

	mu      sync.Mutex
	allowed []plannedWrite
}

// spaceKeys are the request body keys that name a space.
var spaceKeys = map[string]bool{"projectId": true, "projectIds": true, "projectKey": true, "project": true, "projects": true}

var projectPath = regexp.MustCompile(`/project/([^/]+)`)

func (p *writePlan) check(req *http.Request) error {
	apiPath := gatewayPrefix.ReplaceAllString(req.URL.Path, "")
	p.mu.Lock()
	planned := false
	for _, a := range p.allowed {
		planned = planned || a.method == req.Method && a.path.MatchString(apiPath)
	}
	p.mu.Unlock()
	if !planned {
		return fmt.Errorf("%w: %s %s was not planned by the step being carried out", ErrWriteBlocked, req.Method, apiPath)
	}
	if m := projectPath.FindStringSubmatch(apiPath); m != nil && m[1] != p.key && m[1] != p.id {
		return fmt.Errorf("%w: %s %s names space %s, not %s", ErrWriteBlocked, req.Method, apiPath, m[1], p.key)
	}
	if req.Body == nil || req.GetBody == nil {
		return nil
	}
	body, err := req.GetBody()
	if err != nil {
		// checkcover:unreachable Send builds every request from bytes it holds.
		return fmt.Errorf("%w: cannot check the body of %s %s: %w", ErrWriteBlocked, req.Method, apiPath, err)
	}
	data, err := io.ReadAll(body)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return err
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		// checkcover:unreachable Send encodes every body as JSON.
		return fmt.Errorf("%w: the body of %s %s is not JSON", ErrWriteBlocked, req.Method, apiPath)
	}
	if other := p.otherSpace(v, false); other != "" {
		return fmt.Errorf("%w: %s %s names space %s, not %s", ErrWriteBlocked, req.Method, apiPath, other, p.key)
	}
	return nil
}

// otherSpace finds a value under a space key that is not this space.
func (p *writePlan) otherSpace(v any, underSpaceKey bool) string {
	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			if other := p.otherSpace(c, underSpaceKey || spaceKeys[k]); other != "" {
				return other
			}
		}
	case []any:
		for _, c := range x {
			if other := p.otherSpace(c, underSpaceKey); other != "" {
				return other
			}
		}
	case string:
		if underSpaceKey && x != p.key && x != p.id && !strings.EqualFold(x, p.key) {
			return x
		}
	case float64:
		if s := fmt.Sprint(int64(x)); underSpaceKey && s != p.id {
			return s
		}
	}
	return ""
}
