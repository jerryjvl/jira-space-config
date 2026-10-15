package reader

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/jerryjvl/jira-space-config/internal/jira"
)

// Lookups for shareable objects (screens, workflows) by name or ID, before
// reading their definitions with Screens or Workflows.

// Found is an object a lookup found: enough to tell exact names from
// near-misses and to cross-check IDs.
type Found struct{ ID, Name string }

// FindScreens returns the screens whose name contains name (Jira matches
// without regard to case), so near-misses are found too.
func (r *Reader) FindScreens(name string) ([]Found, error) {
	return r.screenList(url.Values{"queryString": {name}}, fmt.Sprintf("looking up screens named %q", name))
}

// ScreensByID returns the screens with the given IDs; IDs Jira does not
// know are left out.
func (r *Reader) ScreensByID(ids []string) ([]Found, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return r.screenList(url.Values{"id": ids}, "looking up screens by ID")
}

func (r *Reader) screenList(q url.Values, what string) ([]Found, error) {
	type screen struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	all, err := jira.GetAll[screen](r.ctx, r.c, "/rest/api/3/screens", q)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	var out []Found
	for _, s := range all {
		out = append(out, Found{strconv.Itoa(s.ID), s.Name})
	}
	return out, nil
}

// FindWorkflows returns the company-managed (global) workflows whose name
// contains name, so near-misses are found too.
func (r *Reader) FindWorkflows(name string) ([]Found, error) {
	type wf struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Scope struct {
			Type string `json:"type"`
		} `json:"scope"`
	}
	all, err := jira.GetAll[wf](r.ctx, r.c, "/rest/api/3/workflows/search", url.Values{"queryString": {name}})
	if err != nil {
		return nil, fmt.Errorf("looking up workflows named %q: %w", name, err)
	}
	var out []Found
	for _, w := range all {
		if w.Scope.Type == "" || w.Scope.Type == "GLOBAL" {
			out = append(out, Found{w.ID, w.Name})
		}
	}
	return out, nil
}

// WorkflowsByID returns the workflows with the given IDs; IDs Jira does not
// know are left out.
func (r *Reader) WorkflowsByID(ids []string) ([]Found, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var read struct {
		Workflows []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"workflows"`
	}
	if err := r.c.PostRead(r.ctx, "/rest/api/3/workflows", nil, map[string][]string{"workflowIds": ids}, &read); err != nil {
		return nil, fmt.Errorf("looking up workflows by ID: %w", err)
	}
	var out []Found
	for _, w := range read.Workflows {
		out = append(out, Found{w.ID, w.Name})
	}
	return out, nil
}

// FieldNames returns the IDs of the site's fields by stored name, system
// fields and custom fields alike; a name may belong to several fields.
func (r *Reader) FieldNames() (map[string][]string, error) {
	if r.siteFields == nil {
		if err := r.loadSiteFields(); err != nil {
			return nil, err
		}
	}
	out := map[string][]string{}
	for _, id := range sortedKeys(r.siteFields) {
		name := r.customName(id)
		out[name] = append(out[name], id)
	}
	return out, nil
}
