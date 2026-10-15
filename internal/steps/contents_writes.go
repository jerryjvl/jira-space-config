package steps

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// The writes for the space's own details, components, versions and role
// members (steps 21 to 24). Nulls are never sent: Jira accepts some and then
// does something else than asked (docs/api/inventory.md).

func account(holder string) string { return strings.TrimPrefix(holder, "user:") }

var assigneeTypes = map[string]string{"unassigned": "UNASSIGNED", "space_lead": "PROJECT_LEAD"}

var componentAssignees = map[string]string{"space_default": "PROJECT_DEFAULT", "component_lead": "COMPONENT_LEAD",
	"space_lead": "PROJECT_LEAD", "unassigned": "UNASSIGNED"}

func updateDetails(env *Env, d *spec.Details) action {
	return func(ctx context.Context, snd Sender) (string, error) {
		body := map[string]any{}
		set := func(o spec.Opt[string], key string, conv func(string) any) {
			if o.Has() {
				body[key] = conv(o.V)
			}
		}
		same := func(s string) any { return s }
		set(d.Name, "name", same)
		set(d.Description, "description", same)
		set(d.Lead, "leadAccountId", func(s string) any { return account(s) })
		set(d.AssigneeType, "assigneeType", func(s string) any { return assigneeTypes[s] })
		set(d.URL, "url", same)
		set(d.Avatar, "avatarId", func(s string) any { n, _ := strconv.Atoi(s); return n })
		if d.URL.Set && d.URL.Null {
			body["url"] = ""
		}
		if d.Category.Set && d.Category.Null {
			body["categoryId"] = -1 // Jira's way to take the space out of its category
		}
		if d.Category.Has() {
			var cats []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if err := snd.Send(ctx, http.MethodGet, "/rest/api/3/projectCategory", nil, nil, &cats); err != nil {
				return "", err
			}
			id := ""
			for _, c := range cats {
				if c.Name == d.Category.V {
					id = c.ID
				}
			}
			if id == "" {
				return "", fmt.Errorf("no space category on the site is named %q", d.Category.V)
			}
			n, _ := strconv.Atoi(id)
			body["categoryId"] = n
		}
		if err := snd.Send(ctx, http.MethodPut, "/rest/api/3/project/"+env.P.Key, nil, body, nil); err != nil {
			return "", err
		}
		return env.P.ID, nil
	}
}

func componentBody(env *Env, c spec.Component) map[string]any {
	body := map[string]any{"name": c.Name, "project": env.P.Key}
	if c.Description.Has() {
		body["description"] = c.Description.V
	}
	if c.Lead.Has() {
		body["leadAccountId"] = account(c.Lead.V)
	}
	if c.AssigneeType.Has() {
		body["assigneeType"] = componentAssignees[c.AssigneeType.V]
	}
	return body
}

func versionBody(env *Env, v spec.Version) map[string]any {
	pid, _ := strconv.Atoi(env.P.ID)
	body := map[string]any{"name": v.Name, "projectId": pid}
	text := func(o spec.Opt[string], key string) {
		if o.Has() {
			body[key] = o.V
		}
	}
	text(v.Description, "description")
	text(v.StartDate, "startDate")
	text(v.ReleaseDate, "releaseDate")
	if v.Released.Has() {
		body["released"] = v.Released.V
	}
	if v.Archived.Has() {
		body["archived"] = v.Archived.V
	}
	return body
}

// send makes one write with a body built in advance and returns an ID.
func send(method, path string, body any) action {
	return func(ctx context.Context, snd Sender) (string, error) {
		var out struct {
			ID jira.ID `json:"id"`
		}
		if err := snd.Send(ctx, method, path, nil, body, &out); err != nil {
			return "", err
		}
		return string(out.ID), nil
	}
}

// addRoleMembers adds users and groups to a role in the space.
func addRoleMembers(env *Env, x resolver, role string, holders []string) action {
	return func(ctx context.Context, snd Sender) (string, error) {
		id, err := x.role(role)
		if err != nil {
			return "", err
		}
		body := map[string][]string{}
		for _, h := range holders {
			kind, value, _ := strings.Cut(h, ":")
			switch kind {
			case "user":
				body["user"] = append(body["user"], value)
			case "group":
				gid, err := x.group(value)
				if err != nil {
					return "", err
				}
				body["groupId"] = append(body["groupId"], gid)
			}
		}
		if err := snd.Send(ctx, http.MethodPost, "/rest/api/3/project/"+env.P.Key+"/role/"+id, nil, body, nil); err != nil {
			return "", err
		}
		return id, nil
	}
}
