package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jerryjvl/jira-space-config/internal/credentials"
	"github.com/jerryjvl/jira-space-config/internal/jira"
)

// connection holds the flags every command that talks to Jira shares.
type connection struct {
	site        string
	api         string
	maxRequests int
}

func (c *connection) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&c.site, "site", "", "Jira Cloud site, e.g. https://example.atlassian.net (required)")
	cmd.Flags().StringVar(&c.api, "api", string(jira.APISite),
		`how to reach the site: "site" (classic API tokens) or "gateway" (scoped API tokens)`)
	cmd.Flags().IntVar(&c.maxRequests, "max-requests", jira.DefaultBudget, "stop after this many HTTP requests")
	_ = cmd.MarkFlagRequired("site")
}

// client builds a read-only Jira client from the flags and the environment.
func (c *connection) client(ctx context.Context, env environment) (*jira.Client, error) {
	opts, err := c.options(env)
	if err != nil {
		return nil, err
	}
	return jira.New(ctx, opts)
}

// options are how clients reach the site, from the flags and the environment.
func (c *connection) options(env environment) (jira.Options, error) {
	site, err := jira.ParseSite(c.site)
	if err != nil {
		return jira.Options{}, err
	}
	creds, err := credentials.Load(env.getenv)
	if err != nil {
		return jira.Options{}, err
	}
	return jira.Options{
		Site:        site,
		Credentials: creds,
		API:         jira.API(strings.ToLower(c.api)),
		UserAgent:   "jira-space-config/" + version,
		Budget:      c.maxRequests,
		Transport:   env.transport,
	}, nil
}

// validSpaceKey checks the shape of a space key before it is put in a URL.
func validSpaceKey(key string) error {
	if key == "" {
		return nil
	}
	for i, r := range key {
		upper := r >= 'A' && r <= 'Z'
		later := i > 0 && (r >= '0' && r <= '9' || r == '_')
		if !upper && !later {
			return fmt.Errorf("space key %q is not valid: it starts with a capital letter, "+
				"followed by capital letters, digits or underscores (for example VULN)", key)
		}
	}
	return nil
}
