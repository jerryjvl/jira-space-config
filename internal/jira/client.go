package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jerryjvl/jira-space-config/internal/credentials"
)

// API selects how requests reach the site.
type API string

const (
	// APISite sends requests to the site URL. Works with classic (unscoped)
	// API tokens.
	APISite API = "site"
	// APIGateway sends requests through https://api.atlassian.com/ex/jira/{cloudId}.
	// Required for scoped API tokens.
	APIGateway API = "gateway"
)

const gatewayHost = "api.atlassian.com"

// DefaultBudget is the default maximum number of HTTP requests per run.
const DefaultBudget = 2000

const (
	maxAttempts   = 4
	maxRetryAfter = 60 * time.Second
	maxBodyBytes  = 32 << 20
)

// ErrBudgetExceeded is returned once a client has made its maximum number of requests.
var ErrBudgetExceeded = errors.New("request budget exceeded")

// Options configure a Client.
type Options struct {
	Site        Site
	Credentials credentials.Credentials
	API         API // APISite if empty
	UserAgent   string
	Budget      int               // maximum requests; DefaultBudget if zero
	Transport   http.RoundTripper // base transport; http.DefaultTransport if nil (tests inject one)
	// Sleep waits before a retry; time-based by default (tests inject one).
	Sleep func(context.Context, time.Duration) error
}

// Client is a read-only Jira client. It is safe for concurrent use.
type Client struct {
	http    *http.Client
	site    Site
	base    *url.URL // site URL, or the gateway URL for this site
	api     API
	cloudID string
	sleep   func(context.Context, time.Duration) error

	mu       sync.Mutex
	budget   int
	requests int
}

// New returns a read-only client. With APIGateway it first looks up the
// site's cloud ID (one GET to the site), because the gateway URL contains it.
func New(ctx context.Context, opts Options) (*Client, error) {
	if opts.Site.IsZero() {
		return nil, errors.New("jira: no site given")
	}
	token, err := opts.Credentials.Token.Reveal()
	if err != nil {
		return nil, errors.New("jira: no API token given")
	}
	api := opts.API
	if api == "" {
		api = APISite
	}
	if api != APISite && api != APIGateway {
		return nil, fmt.Errorf("jira: unknown API mode %q (use %q or %q)", api, APISite, APIGateway)
	}
	base := opts.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	hosts := []string{opts.Site.Host()}
	if api == APIGateway {
		hosts = append(hosts, gatewayHost)
	}
	c := &Client{
		http: &http.Client{
			Transport: newGuard(&authenticator{
				next:      base,
				email:     opts.Credentials.Email,
				token:     token,
				userAgent: opts.UserAgent,
			}, hosts...),
			// Jira's API does not redirect; a redirect means something is
			// wrong (wrong site, proxy, sign-in page), so it is reported, not followed.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Timeout:       60 * time.Second,
		},
		site:   opts.Site,
		base:   opts.Site.ref(""),
		api:    api,
		sleep:  opts.Sleep,
		budget: opts.Budget,
	}
	if c.budget <= 0 {
		c.budget = DefaultBudget
	}
	if c.sleep == nil {
		c.sleep = sleepContext
	}
	if api == APIGateway {
		info, err := c.TenantInfo(ctx)
		if err != nil {
			return nil, fmt.Errorf("looking up the cloud ID for the gateway: %w", err)
		}
		c.cloudID = info.CloudID
		c.base = &url.URL{Scheme: "https", Host: gatewayHost, Path: "/ex/jira/" + info.CloudID}
	}
	return c, nil
}

// Site returns the site the client targets.
func (c *Client) Site() Site { return c.site }

// API returns how requests reach the site.
func (c *Client) API() API { return c.api }

// BaseURL returns where API requests are sent.
func (c *Client) BaseURL() string { return c.base.String() }

// Requests returns how many HTTP requests the client has made.
func (c *Client) Requests() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests
}

// TenantInfo is the response of the site's /_edge/tenant_info endpoint.
type TenantInfo struct {
	CloudID string `json:"cloudId"`
}

// TenantInfo returns the site's cloud ID. This endpoint is not in Atlassian's
// OpenAPI specs (see docs/api/inventory.md) but is widely used; it is always
// requested from the site itself, never the gateway.
func (c *Client) TenantInfo(ctx context.Context) (TenantInfo, error) {
	if c.cloudID != "" {
		return TenantInfo{CloudID: c.cloudID}, nil
	}
	var info TenantInfo
	if err := c.do(ctx, http.MethodGet, c.site.ref("/_edge/tenant_info"), nil, &info); err != nil {
		return info, err
	}
	if info.CloudID == "" {
		return info, errors.New("the site did not return a cloud ID; is this a Jira Cloud site?")
	}
	c.cloudID = info.CloudID
	return info, nil
}

// Get sends a GET for an API path such as /rest/api/3/myself and decodes the
// JSON response into out.
func (c *Client) Get(ctx context.Context, apiPath string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, c.resolve(apiPath, query), nil, out)
}

// PostRead sends a POST to one of the documented read-only endpoints. The
// guard refuses any other path. query may be nil; paged endpoints take
// startAt there.
func (c *Client) PostRead(ctx context.Context, apiPath string, query url.Values, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		// checkcover:unreachable callers pass the tool's own request structs,
		// which always encode.
		return fmt.Errorf("encoding request body for %s: %w", apiPath, err)
	}
	return c.do(ctx, http.MethodPost, c.resolve(apiPath, query), data, out)
}

func (c *Client) resolve(apiPath string, query url.Values) *url.URL {
	u := *c.base
	u.Path = c.base.Path + apiPath
	if strings.HasPrefix(apiPath, AutomationPrefix) {
		// The automation API is not under Jira's REST path: on the site it is
		// under /gateway/api; on api.atlassian.com, at the root.
		if c.api == APIGateway {
			u.Path = apiPath
		} else {
			u.Path = "/gateway/api" + apiPath
		}
	}
	u.RawQuery = query.Encode()
	return &u
}

func (c *Client) do(ctx context.Context, method string, u *url.URL, body []byte, out any) error {
	for attempt := 1; ; attempt++ {
		if err := c.spend(); err != nil {
			return err
		}
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
		if err != nil {
			// checkcover:unreachable the method is a constant and the URL was built
			// from a parsed site; only a nil context could fail here.
			return fmt.Errorf("building request %s %s: %w", method, u.Path, err)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if errors.Is(err, ErrWriteBlocked) {
				return err
			}
			return &TransportError{Method: method, Path: u.Path, Err: err}
		}
		retry, wait, err := c.handle(resp, method, u.Path, out)
		_ = resp.Body.Close()
		// A write that Jira answered may have been carried out, so only rate
		// limiting, which Jira refuses before doing anything, is retried.
		if retry && IsWrite(method, u.Path) && resp.StatusCode != http.StatusTooManyRequests {
			retry = false
		}
		if !retry || attempt == maxAttempts {
			return err
		}
		if err := c.sleep(ctx, wait); err != nil {
			return err
		}
	}
}

// handle reads one response; the caller closes the body. It reports whether
// the request should be retried (rate limiting or a temporary outage) and how
// long to wait first.
func (c *Client) handle(resp *http.Response, method, apiPath string, out any) (retry bool, wait time.Duration, err error) {
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if readErr != nil {
			return false, 0, &TransportError{Method: method, Path: apiPath, Err: readErr}
		}
		if out == nil || len(data) == 0 {
			return false, 0, nil
		}
		if err := json.Unmarshal(data, out); err != nil {
			return false, 0, fmt.Errorf("%s %s: unexpected response (not the JSON this tool expects): %w", method, apiPath, err)
		}
		return false, 0, nil
	}

	apiErr := newAPIError(method, apiPath, resp, data)
	switch resp.StatusCode {
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return true, retryAfter(resp.Header.Get("Retry-After")), apiErr
	}
	return false, 0, apiErr
}

func (c *Client) spend() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.requests >= c.budget {
		return fmt.Errorf("%w: this run has made %d requests, the limit; "+
			"the site may be larger than expected, raise it with --max-requests if that is intended",
			ErrBudgetExceeded, c.requests)
	}
	c.requests++
	return nil
}

func retryAfter(header string) time.Duration {
	wait := 5 * time.Second
	if s, err := strconv.Atoi(header); err == nil && s >= 0 {
		wait = time.Duration(s) * time.Second
	}
	return min(wait, maxRetryAfter)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// AutomationPrefix starts the paths of Jira's automation API:
// /automation/public/jira/{cloudId}/rest/v1/... (Automation gives them).
const AutomationPrefix = "/automation/public/jira/"

// Automation returns the path of an automation API endpoint for this site:
// Automation(ctx, "/rule/summary").
func (c *Client) Automation(ctx context.Context, endpoint string) (string, error) {
	info, err := c.TenantInfo(ctx)
	if err != nil {
		return "", err
	}
	return AutomationPrefix + info.CloudID + "/rest/v1" + endpoint, nil
}
