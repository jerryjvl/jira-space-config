// Package jira is a read-only client for the Jira Cloud REST API.
//
// Every request passes through a guard (guard.go) that allows only GET and a
// fixed list of documented read-only POST endpoints, only to the target
// site's host (and the Atlassian API gateway when it is in use). Nothing in
// this package can send a write. The write-capable client will live
// elsewhere, importable only by the apply code.
package jira

import (
	"fmt"
	"net/url"
	"strings"
)

// Site is the base URL of a Jira Cloud site, such as https://example.atlassian.net.
type Site struct {
	url *url.URL
}

// ParseSite accepts only a bare https URL: no path, query, fragment or
// credentials. Being strict here means a typo cannot point the tool somewhere
// unexpected.
func ParseSite(raw string) (Site, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Site{}, fmt.Errorf("site is empty; pass --site https://<name>.atlassian.net")
	}
	u, err := url.Parse(strings.TrimSuffix(raw, "/"))
	if err != nil {
		return Site{}, fmt.Errorf("site %q is not a valid URL: %w", raw, err)
	}
	switch {
	case u.Scheme != "https":
		return Site{}, fmt.Errorf("site %q must start with https://", raw)
	case u.Host == "":
		return Site{}, fmt.Errorf("site %q has no host name", raw)
	case u.User != nil:
		return Site{}, fmt.Errorf("site %q must not contain credentials; use JIRA_EMAIL and JIRA_API_TOKEN", raw)
	case u.Path != "" || u.RawQuery != "" || u.Fragment != "":
		return Site{}, fmt.Errorf("site %q must be just the site address, without a path (for example https://example.atlassian.net)", raw)
	}
	u.Host = strings.ToLower(u.Host)
	return Site{url: u}, nil
}

// String returns the site URL.
func (s Site) String() string {
	if s.url == nil {
		return ""
	}
	return s.url.String()
}

// Host returns the site's host name.
func (s Site) Host() string { return s.url.Host }

// IsZero reports whether the site is unset.
func (s Site) IsZero() bool { return s.url == nil }

// ref resolves an absolute path against the site.
func (s Site) ref(path string) *url.URL {
	u := *s.url
	u.Path = path
	return &u
}
