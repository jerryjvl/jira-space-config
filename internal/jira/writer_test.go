package jira

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jerryjvl/jira-space-config/internal/credentials"
)

func newTestWriter(t *testing.T, f *fakeJira) *Writer {
	t.Helper()
	w, err := NewWriter(context.Background(), Options{
		Site:        f.site(t),
		Credentials: credentials.Credentials{Email: "me@example.com", Token: credentials.NewSecret(canary)},
		Transport:   f.transport(),
		Sleep:       func(context.Context, time.Duration) error { return nil },
	}, "VULN", "10042")
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// TestWriterSendsOnlyPlannedWrites: a write the step did not plan, or one
// naming another space, never leaves.
func TestWriterSendsOnlyPlannedWrites(t *testing.T) {
	f := newFakeJira(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"id":"1"}`)) })
	w := newTestWriter(t, f)
	ctx := context.Background()
	send := func(method, path string, body any) error { return w.Send(ctx, method, path, nil, body, nil) }

	if err := send(http.MethodPost, "/rest/api/3/statuses", map[string]any{}); !errors.Is(err, ErrWriteBlocked) {
		t.Errorf("unplanned write: %v", err)
	}
	w.Allow(http.MethodPost, "/rest/api/3/statuses")
	w.Allow(http.MethodPut, "/rest/api/3/project/{key}")
	w.Allow(http.MethodPost, "/rest/api/3/priorityscheme")
	if err := send(http.MethodPost, "/rest/api/3/statuses", map[string]any{"scope": map[string]any{"type": "GLOBAL"}}); err != nil {
		t.Errorf("planned write: %v", err)
	}
	if err := send(http.MethodPut, "/rest/api/3/project/VULN", map[string]any{"name": "x"}); err != nil {
		t.Errorf("planned write to the space: %v", err)
	}
	for name, tc := range map[string]struct {
		method, path string
		body         any
	}{
		"another space in the path": {http.MethodPut, "/rest/api/3/project/OTHER", nil},
		"another space in the body": {http.MethodPost, "/rest/api/3/priorityscheme", map[string]any{"projects": map[string]any{"add": []any{"99"}}}},
		"another method":            {http.MethodDelete, "/rest/api/3/statuses", nil},
	} {
		if err := send(tc.method, tc.path, tc.body); !errors.Is(err, ErrWriteBlocked) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := send(http.MethodPost, "/rest/api/3/priorityscheme", map[string]any{"projects": map[string]any{"add": []any{"10042"}}}); err != nil {
		t.Errorf("this space in the body: %v", err)
	}
	w.Reset()
	if err := send(http.MethodPost, "/rest/api/3/statuses", map[string]any{}); !errors.Is(err, ErrWriteBlocked) {
		t.Errorf("after Reset: %v", err)
	}
	for _, r := range f.seen() {
		if strings.Contains(r.URL.Path, "OTHER") || r.Method == http.MethodDelete {
			t.Errorf("a refused write reached the server: %s %s", r.Method, r.URL.Path)
		}
	}
}

// TestWritesRetryOnlyOnRateLimiting: a write Jira answered may have been
// made, so a 503 is not retried; a 429 is.
func TestWritesRetryOnlyOnRateLimiting(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests} {
		f := newFakeJira(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
		w := newTestWriter(t, f)
		w.Allow(http.MethodPost, "/rest/api/3/statuses")
		_ = w.Send(context.Background(), http.MethodPost, "/rest/api/3/statuses", nil, map[string]any{}, nil)
		want := 1
		if status == http.StatusTooManyRequests {
			want = maxAttempts
		}
		if n := len(f.seen()); n != want {
			t.Errorf("status %d: %d attempts, want %d", status, n, want)
		}
	}
}

// TestOnlyApplyCreatesWriters: the write client is reachable from the apply
// runner and the sandbox tool only.
func TestOnlyApplyCreatesWriters(t *testing.T) {
	allowed := []string{"internal/jira/", "internal/apply/", "cmd/jsc-sandbox/"}
	root := "../.."
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		data, err := os.ReadFile(p) //nolint:gosec // G122: the repository's own source files
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "jira.NewWriter(") {
			ok := false
			for _, a := range allowed {
				ok = ok || strings.HasPrefix(filepath.ToSlash(rel), a)
			}
			if !ok {
				t.Errorf("%s creates a Writer; only %v may", rel, allowed)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWriterRefusesBodiesItCannotEncode(t *testing.T) {
	f := newFakeJira(t, func(http.ResponseWriter, *http.Request) {})
	w := newTestWriter(t, f)
	w.Allow(http.MethodPost, "/rest/api/3/statuses")
	if err := w.Send(context.Background(), http.MethodPost, "/rest/api/3/statuses", nil, make(chan int), nil); err == nil ||
		!strings.Contains(err.Error(), "encoding request body for POST /rest/api/3/statuses") {
		t.Errorf("err = %v", err)
	}
	if len(f.seen()) != 0 {
		t.Error("a request was sent")
	}
}
