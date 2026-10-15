package reader

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/spec"
	"github.com/jerryjvl/jira-space-config/internal/util"
)

// The space's automation rules, from Jira's automation API: the rules whose
// scope includes the space. A rule's name, description, state and scope are
// the format's; the rest of Jira's rule object is passed through
// (docs/automation.md).

// ruleFields are the parts of Jira's rule object the format models, or that
// say who made it and when: they are left out of the opaque body.
var ruleFields = []string{"name", "description", "state", "ruleScopeARIs", "uuid", "created", "updated", "authorAccountId"}

// SpaceARI is the space's resource name, which automation rules use for
// their scope: ari:cloud:jira:{cloudId}:project/{id}.
func (r *Reader) SpaceARI(p *Project) (string, error) {
	info, err := r.c.TenantInfo(r.ctx)
	if err != nil {
		return "", err
	}
	return "ari:cloud:jira:" + info.CloudID + ":project/" + p.ID, nil
}

// AutomationRules reads the rules whose scope includes the space.
func (r *Reader) AutomationRules(p *Project) ([]spec.AutomationRule, error) {
	ari, err := r.SpaceARI(p)
	if err != nil {
		return nil, err
	}
	search, err := r.c.Automation(r.ctx, "/rule/summary")
	if err != nil {
		return nil, err
	}
	var uuids []string
	for cursor := ""; ; {
		body := map[string]any{"scope": ari, "limit": 100}
		if cursor != "" {
			body["cursor"] = cursor
		}
		var page struct {
			Data []struct {
				UUID string `json:"uuid"`
			} `json:"data"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		}
		if err := r.c.PostRead(r.ctx, search, nil, body, &page); err != nil {
			return nil, fmt.Errorf("reading the space's automation rules: %w", err)
		}
		for _, d := range page.Data {
			uuids = util.AppendNew(uuids, d.UUID)
		}
		// The next page is given as a query string: ?cursor=...&limit=...
		next, _ := url.ParseQuery(strings.TrimPrefix(page.Links.Next, "?"))
		if cursor = next.Get("cursor"); cursor == "" {
			break
		}
	}
	var out []spec.AutomationRule
	for _, id := range uuids {
		rule, err := r.automationRule(id, ari)
		if err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	return out, nil
}

func (r *Reader) automationRule(id, ari string) (spec.AutomationRule, error) {
	path, err := r.c.Automation(r.ctx, "/rule/"+url.PathEscape(id))
	if err != nil {
		return spec.AutomationRule{}, err
	}
	var answer struct {
		Rule        map[string]any `json:"rule"`
		Connections []any          `json:"connections"`
	}
	if err := r.get(path, nil, &answer); err != nil {
		return spec.AutomationRule{}, fmt.Errorf("reading automation rule %s: %w", id, err)
	}
	rule := answer.Rule
	text := func(key string) string {
		s, _ := rule[key].(string)
		return s
	}
	description, mode := ownership(text("description"))
	out := spec.AutomationRule{Meta: spec.Meta{ID: id}, Name: text("name"), Mode: mode, Description: description,
		State: strings.ToLower(text("state")), Scope: "space"}
	scopes, _ := rule["ruleScopeARIs"].([]any)
	if len(scopes) != 1 || scopes[0] != ari {
		// Other spaces too, or the whole site: used as it is, never changed.
		out.Scope, out.Mode = "shared", spec.ModeReuse
	}
	body := map[string]any{}
	for k, v := range rule {
		if !slices.Contains(ruleFields, k) {
			body[k] = v
		}
	}
	stripComponentIDs(body)
	if len(answer.Connections) > 0 {
		for _, c := range answer.Connections {
			if m, ok := c.(map[string]any); ok { // Jira's own bookkeeping, not the connection
				delete(m, "container")
				delete(m, "createdAt")
				delete(m, "updatedAt")
			}
		}
		body["connections"] = answer.Connections
	}
	out.Rule = rawValue(body)
	return out, nil
}

// AutomationPath is the path of an automation API endpoint, for writes.
func (r *Reader) AutomationPath(endpoint string) (string, error) {
	return r.c.Automation(r.ctx, endpoint)
}

// stripComponentIDs takes the components' IDs (their own, and their
// parents') out of a rule body: Jira gives them, and a new rule gets new
// ones (docs/automation.md).
func stripComponentIDs(v any) {
	switch x := v.(type) {
	case map[string]any:
		if _, ok := x["component"]; ok {
			delete(x, "id")
			delete(x, "parentId")
			delete(x, "conditionParentId")
		}
		for _, val := range x {
			stripComponentIDs(val)
		}
	case []any:
		for _, item := range x {
			stripComponentIDs(item)
		}
	}
}
