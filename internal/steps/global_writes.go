package steps

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/reader"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// The writes that create global objects (mode ensure). Bodies follow
// Atlassian's OpenAPI spec; every object created carries the marker in its
// description.

// managedDescription is a description with the tool's marker.
func managedDescription(d spec.Opt[string]) string {
	if d.Has() && d.V != "" {
		return d.V + " " + reader.Marker
	}
	return reader.Marker
}

type action = func(ctx context.Context, s Sender) (string, error)

// post sends one POST and returns the ID Jira gives back.
func post(path string, body any) action {
	return func(ctx context.Context, s Sender) (string, error) {
		var out struct {
			ID      jira.ID `json:"id"`
			GroupID string  `json:"groupId"`
		}
		if err := s.Send(ctx, http.MethodPost, path, nil, body, &out); err != nil {
			return "", err
		}
		if out.GroupID != "" {
			return out.GroupID, nil
		}
		return string(out.ID), nil
	}
}

var statusCategoryNames = map[string]string{"todo": "TODO", "in_progress": "IN_PROGRESS", "done": "DONE"}

func createStatus(s spec.Status) action {
	return func(ctx context.Context, snd Sender) (string, error) {
		var out []struct {
			ID string `json:"id"`
		}
		body := map[string]any{
			"scope": map[string]string{"type": "GLOBAL"},
			"statuses": []map[string]string{{"name": s.Name, "statusCategory": statusCategoryNames[s.Category.V],
				"description": managedDescription(s.Description)}},
		}
		if err := snd.Send(ctx, http.MethodPost, "/rest/api/3/statuses", nil, body, &out); err != nil {
			return "", err
		}
		if len(out) != 1 {
			return "", errors.New("the answer does not say which status was created")
		}
		return out[0].ID, nil
	}
}

func createResolution(r spec.Resolution) action {
	return post("/rest/api/3/resolution", map[string]string{"name": r.Name, "description": managedDescription(r.Description)})
}

// defaultPriorityIcon is the icon a new priority gets when the spec names
// none: Jira refuses a priority without an icon or avatar.
const defaultPriorityIcon = "/images/icons/priorities/medium.svg"

func createPriority(site string, p spec.Priority) action {
	icon := defaultPriorityIcon
	if p.Icon.Has() && p.Icon.V != "" {
		icon = p.Icon.V
	}
	body := map[string]string{"name": p.Name, "statusColor": p.Colour.V, "description": managedDescription(p.Description),
		"iconUrl": strings.TrimSuffix(site, "/") + icon}
	return post("/rest/api/3/priority", body)
}

func createLinkType(t spec.LinkType) action {
	return post("/rest/api/3/issueLinkType", map[string]string{"name": t.Name, "inward": t.Inward.V, "outward": t.Outward.V})
}

func createIssueType(t spec.IssueType) action {
	level := 0
	if t.Level.V == "subtask" {
		level = -1
	}
	return post("/rest/api/3/issuetype", map[string]any{"name": t.Name, "description": managedDescription(t.Description),
		"hierarchyLevel": level})
}

func createCustomField(f spec.CustomField) action {
	key := reader.FieldTypeKey(f.Type)
	body := map[string]string{"name": f.Name, "type": key, "description": managedDescription(f.Description)}
	switch {
	case f.Searcher.Has():
		body["searcherKey"] = f.Searcher.V
	case reader.DefaultSearcher(key) != "":
		body["searcherKey"] = reader.DefaultSearcher(key)
	}
	return post("/rest/api/3/field", body)
}

func createRole(r spec.Role) action {
	return post("/rest/api/3/role", map[string]string{"name": r.Name, "description": managedDescription(r.Description)})
}

func createGroup(g spec.GlobalObject) action {
	return post("/rest/api/3/group", map[string]string{"name": g.Name})
}
