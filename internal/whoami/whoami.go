// Package whoami gathers what the tool needs to know about the account and
// site before doing anything else: who is signed in, which site and plan,
// and whether the account has the permissions the tool relies on.
package whoami

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"

	"github.com/jerryjvl/jira-space-config/internal/jira"
)

// Permission keys checked, in display order, with why the tool needs them.
var checked = []struct {
	key, label, need string
	global           bool
}{
	{"ADMINISTER", "Administer Jira", "needed to read schemes, workflows, screens and fields", true},
	{"ADMINISTER_PROJECTS", "Administer projects", "needed to read the space's configuration, unless the account has Administer Jira", false},
	{"BROWSE_PROJECTS", "Browse projects", "needed to see the space and count its work items", false},
}

// Report is everything whoami found.
type Report struct {
	Site        string
	API         jira.API
	BaseURL     string
	CloudID     string
	User        User
	Server      Server
	Plans       []Plan
	SpaceKey    string
	Space       *Space // nil when no space was asked for
	Permissions []Permission
	// AdministerJira covers every read that would otherwise need Administer
	// projects (docs/api/inventory.md), so Notes treats it specially.
	AdministerJira bool
	Requests       int
}

// User is the signed-in account.
type User struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
	Email       string `json:"emailAddress"`
	Active      bool   `json:"active"`
}

// Server is the subset of /serverInfo that is shown.
type Server struct {
	DeploymentType string `json:"deploymentType"`
	Version        string `json:"version"`
	ServerTitle    string `json:"serverTitle"`
}

// Plan is one licensed product and its plan (FREE, PAID or UNLICENSED).
type Plan struct {
	ID   string `json:"id"`
	Plan string `json:"plan"`
}

// Space is the subset of /project/{key} that is shown.
type Space struct {
	ID    string `json:"id"`
	Key   string `json:"key"`
	Name  string `json:"name"`
	Style string `json:"style"` // classic = company-managed, next-gen = team-managed
}

// Permission is one checked permission and whether the account has it.
type Permission struct {
	Key, Label, Need string
	Global           bool
	Have             bool
}

// Run collects the report. spaceKey may be empty.
func Run(ctx context.Context, c *jira.Client, spaceKey string) (Report, error) {
	r := Report{Site: c.Site().String(), API: c.API(), BaseURL: c.BaseURL(), SpaceKey: spaceKey}

	info, err := c.TenantInfo(ctx)
	if err != nil {
		return r, err
	}
	r.CloudID = info.CloudID

	if err := c.Get(ctx, "/rest/api/3/myself", nil, &r.User); err != nil {
		return r, err
	}
	if err := c.Get(ctx, "/rest/api/3/serverInfo", nil, &r.Server); err != nil {
		return r, err
	}
	var license struct {
		Applications []Plan `json:"applications"`
	}
	if err := c.Get(ctx, "/rest/api/3/instance/license", nil, &license); err != nil {
		return r, err
	}
	r.Plans = license.Applications

	keys := make([]string, len(checked))
	for i, p := range checked {
		keys[i] = p.key
	}
	query := url.Values{"permissions": {strings.Join(keys, ",")}}
	if spaceKey != "" {
		query.Set("projectKey", spaceKey)
	}
	var perms struct {
		Permissions map[string]struct {
			Have bool `json:"havePermission"`
		} `json:"permissions"`
	}
	if err := c.Get(ctx, "/rest/api/3/mypermissions", query, &perms); err != nil {
		return r, err
	}
	for _, p := range checked {
		r.Permissions = append(r.Permissions, Permission{
			Key: p.key, Label: p.label, Need: p.need, Global: p.global,
			Have: perms.Permissions[p.key].Have,
		})
	}
	r.AdministerJira = perms.Permissions["ADMINISTER"].Have

	if spaceKey != "" {
		var s Space
		if err := c.Get(ctx, "/rest/api/3/project/"+url.PathEscape(spaceKey), nil, &s); err != nil {
			var apiErr *jira.APIError
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
				return r, fmt.Errorf("space %q was not found, or this account cannot see it: %w", spaceKey, err)
			}
			return r, err
		}
		r.Space = &s
	}
	r.Requests = c.Requests()
	return r, nil
}

// Notes are observations that affect what the tool can do.
func (r Report) Notes() []string {
	var notes []string
	for _, p := range r.Plans {
		if p.Plan == "FREE" {
			notes = append(notes, fmt.Sprintf("%s is on the Free plan: permission schemes, issue security and "+
				"space role assignment cannot be configured; export reports Jira's fixed defaults for them.", p.ID))
		}
	}
	adminJira := r.AdministerJira
	for _, p := range r.Permissions {
		// Every read that accepts Administer projects also accepts Administer
		// Jira (docs/api/inventory.md), so only report it when both are missing.
		covered := p.Key == "ADMINISTER_PROJECTS" && adminJira
		if !p.Have && !covered {
			notes = append(notes, fmt.Sprintf("Missing %q: %s. Ask a site admin to grant it.", p.Label, p.Need))
		}
	}
	if r.Space != nil && r.Space.Style != "classic" {
		notes = append(notes, fmt.Sprintf("Space %s is team-managed (%q); this tool only supports company-managed spaces.",
			r.Space.Key, r.Space.Style))
	}
	if r.SpaceKey == "" {
		notes = append(notes, "No --space given: space permissions show whether the account has them in at least one space.")
	}
	return notes
}

// Render writes the report as aligned text.
func (r Report) Render(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	line := func(label, value string) { _, _ = fmt.Fprintf(tw, "%s\t%s\n", label, value) }

	line("Site", r.Site)
	line("Cloud ID", r.CloudID)
	line("API", fmt.Sprintf("%s (%s)", r.API, r.BaseURL))
	line("User", fmt.Sprintf("%s (account %s)", r.User.DisplayName, r.User.AccountID))
	if r.User.Email != "" {
		line("Email", r.User.Email)
	}
	line("Jira", strings.TrimSpace(r.Server.DeploymentType+" "+r.Server.Version))
	for _, p := range r.Plans {
		line("Plan", fmt.Sprintf("%s: %s", p.ID, p.Plan))
	}
	if r.Space != nil {
		managed := "company-managed"
		if r.Space.Style != "classic" {
			managed = "team-managed"
		}
		line("Space", fmt.Sprintf("%s %q (%s, id %s)", r.Space.Key, r.Space.Name, managed, r.Space.ID))
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(w, "\nPermissions")
	for _, p := range r.Permissions {
		scope := "global"
		if !p.Global {
			scope = "space " + r.SpaceKey
			if r.SpaceKey == "" {
				scope = "any space"
			}
		}
		have := "no "
		if p.Have {
			have = "yes"
		}
		_, _ = fmt.Fprintf(tw, "  %s (%s)\t%s\t%s\n", p.Label, scope, have, p.Need)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if notes := r.Notes(); len(notes) > 0 {
		_, _ = fmt.Fprintln(w, "\nNotes")
		for _, n := range notes {
			_, _ = fmt.Fprintf(w, "  - %s\n", n)
		}
	}
	_, err := fmt.Fprintf(w, "\n%d requests made, all read-only.\n", r.Requests)
	return err
}
