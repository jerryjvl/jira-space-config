package reader

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/util"
)

// The sharing checks: which spaces use a
// shareable object, read from Jira's reverse lookups just before the object
// would be changed. Each returns the IDs of the spaces. A read that fails is
// an error, never "unused": a change is made only when the object is known
// to be used by the target space alone.

// WorkflowUsers returns the spaces that use a workflow, through their
// workflow schemes.
func (r *Reader) WorkflowUsers(id string) ([]string, error) {
	return r.tokenProjects("/rest/api/3/workflow/"+url.PathEscape(id)+"/projectUsages", "workflow", id)
}

// WorkflowSchemeUsers returns the spaces that use a workflow scheme.
func (r *Reader) WorkflowSchemeUsers(id string) ([]string, error) {
	return r.tokenProjects("/rest/api/3/workflowscheme/"+url.PathEscape(id)+"/projectUsages", "workflow scheme", id)
}

// tokenProjects reads the spaces of a usage lookup paged by token.
func (r *Reader) tokenProjects(path, noun, id string) ([]string, error) {
	var out []string
	q := url.Values{"maxResults": {"200"}}
	for {
		var page struct {
			Projects struct {
				NextPageToken string `json:"nextPageToken"`
				Values        []struct {
					ID string `json:"id"`
				} `json:"values"`
			} `json:"projects"`
		}
		if err := r.get(path, q, &page); err != nil {
			return nil, fmt.Errorf("checking which spaces use %s %s: %w", noun, id, err)
		}
		for _, p := range page.Projects.Values {
			out = util.AppendNew(out, p.ID)
		}
		if page.Projects.NextPageToken == "" {
			return out, nil
		}
		q.Set("nextPageToken", page.Projects.NextPageToken)
	}
}

// ScreenUsers returns the spaces that use a screen: through a screen
// scheme in their issue type screen scheme, or as a transition screen in an
// active workflow. Jira has no reverse lookup for screens, so this reads
// every screen scheme and every active workflow's transitions: one page per
// hundred screen schemes and per fifty workflows.
func (r *Reader) ScreenUsers(id string) ([]string, error) {
	sid, err := strconv.Atoi(id)
	if err != nil {
		// checkcover:unreachable screen IDs come from Jira's screen reads, which give numbers.
		return nil, fmt.Errorf("screen id %q is not a number", id)
	}
	schemes, err := jira.GetAll[screenScheme](r.ctx, r.c, "/rest/api/3/screenscheme", url.Values{"maxResults": {"100"}})
	if err != nil {
		return nil, fmt.Errorf("checking which screen schemes use screen %s: %w", id, err)
	}
	var schemeIDs []string
	for _, s := range schemes {
		if slices.Contains([]int{s.Screens.Default, s.Screens.Create, s.Screens.Edit, s.Screens.View}, sid) {
			schemeIDs = append(schemeIDs, strconv.Itoa(s.ID))
		}
	}
	var out []string
	if len(schemeIDs) > 0 {
		type mapping struct {
			ITSS           string `json:"issueTypeScreenSchemeId"`
			ScreenSchemeID string `json:"screenSchemeId"`
		}
		mappings, err := jira.GetAll[mapping](r.ctx, r.c, "/rest/api/3/issuetypescreenscheme/mapping", url.Values{"maxResults": {"100"}})
		if err != nil {
			return nil, fmt.Errorf("checking which issue type screen schemes use screen %s: %w", id, err)
		}
		var itss []string
		for _, m := range mappings {
			if slices.Contains(schemeIDs, m.ScreenSchemeID) {
				itss = util.AppendNew(itss, m.ITSS)
			}
		}
		for _, s := range itss {
			type project struct {
				ID string `json:"id"`
			}
			projects, err := jira.GetAll[project](r.ctx, r.c, "/rest/api/3/issuetypescreenscheme/"+url.PathEscape(s)+"/project", nil)
			if err != nil {
				return nil, fmt.Errorf("checking which spaces use screen %s: %w", id, err)
			}
			for _, p := range projects {
				out = util.AppendNew(out, p.ID)
			}
		}
	}
	type transition struct {
		Screen *struct {
			Parameters struct {
				ScreenID string `json:"screenId"`
			} `json:"parameters"`
		} `json:"transitionScreen"`
	}
	type workflow struct {
		ID          string       `json:"id"`
		Transitions []transition `json:"transitions"`
	}
	active, err := jira.GetAll[workflow](r.ctx, r.c, "/rest/api/3/workflows/search",
		url.Values{"isActive": {"true"}, "expand": {"values.transitions"}, "maxResults": {"50"}})
	if err != nil {
		return nil, fmt.Errorf("checking which workflows use screen %s: %w", id, err)
	}
	for _, w := range active {
		if !slices.ContainsFunc(w.Transitions, func(t transition) bool { return t.Screen != nil && t.Screen.Parameters.ScreenID == id }) {
			continue
		}
		users, err := r.WorkflowUsers(w.ID)
		if err != nil {
			return nil, err
		}
		for _, p := range users {
			out = util.AppendNew(out, p)
		}
	}
	return out, nil
}

// IssueTypeSchemeUsers returns the spaces that use an issue type scheme.
// The list's projects are a page of their own; one that is not the last
// is refused, as the scheme is then certainly shared but not fully known.
func (r *Reader) IssueTypeSchemeUsers(id string) ([]string, error) {
	var page struct {
		Values []struct {
			Projects struct {
				IsLast bool `json:"isLast"`
				Values []struct {
					ID string `json:"id"`
				} `json:"values"`
			} `json:"projects"`
		} `json:"values"`
	}
	if err := r.get("/rest/api/3/issuetypescheme", url.Values{"id": {id}, "expand": {"projects"}}, &page); err != nil {
		return nil, fmt.Errorf("checking which spaces use issue type scheme %s: %w", id, err)
	}
	if len(page.Values) != 1 {
		return nil, fmt.Errorf("checking which spaces use issue type scheme %s: Jira returned %d schemes", id, len(page.Values))
	}
	var out []string
	for _, p := range page.Values[0].Projects.Values {
		out = util.AppendNew(out, p.ID)
	}
	if !page.Values[0].Projects.IsLast {
		out = append(out, "more")
	}
	return out, nil
}

// ScreenSchemeUsers returns the spaces that use an issue type screen scheme.
func (r *Reader) ScreenSchemeUsers(id string) ([]string, error) {
	return r.projectIDs("/rest/api/3/issuetypescreenscheme/"+url.PathEscape(id)+"/project", "issue type screen scheme", id)
}

// FieldSchemeUsers returns the spaces that use a field scheme.
func (r *Reader) FieldSchemeUsers(id string) ([]string, error) {
	return r.projectIDs("/rest/api/3/config/fieldschemes/"+url.PathEscape(id)+"/projects", "field scheme", id)
}

func (r *Reader) projectIDs(path, noun, id string) ([]string, error) {
	type project struct {
		ID string `json:"id"`
	}
	projects, err := jira.GetAll[project](r.ctx, r.c, path, nil)
	if err != nil {
		return nil, fmt.Errorf("checking which spaces use %s %s: %w", noun, id, err)
	}
	var out []string
	for _, p := range projects {
		out = util.AppendNew(out, p.ID)
	}
	return out, nil
}

// PrioritySchemeUsers returns the spaces that use a priority scheme.
func (r *Reader) PrioritySchemeUsers(id string) ([]string, error) {
	return r.projectIDs("/rest/api/3/priorityscheme/"+url.PathEscape(id)+"/projects", "priority scheme", id)
}

// NotificationSchemeUsers returns the spaces that use a notification scheme.
func (r *Reader) NotificationSchemeUsers(id string) ([]string, error) {
	return r.mappedProjects("/rest/api/3/notificationscheme/project", url.Values{"notificationSchemeId": {id}}, "notification scheme", id)
}

// SecuritySchemeUsers returns the spaces that use an issue security scheme.
func (r *Reader) SecuritySchemeUsers(id string) ([]string, error) {
	return r.mappedProjects("/rest/api/3/issuesecurityschemes/project", url.Values{"issueSecuritySchemeId": {id}}, "issue security scheme", id)
}

// mappedProjects reads the spaces of a lookup that lists scheme and space
// pairs.
func (r *Reader) mappedProjects(path string, q url.Values, noun, id string) ([]string, error) {
	type mapping struct {
		ProjectID string `json:"projectId"`
	}
	all, err := jira.GetAll[mapping](r.ctx, r.c, path, q)
	if err != nil {
		return nil, fmt.Errorf("checking which spaces use %s %s: %w", noun, id, err)
	}
	var out []string
	for _, m := range all {
		out = util.AppendNew(out, m.ProjectID)
	}
	return out, nil
}

// PermissionSchemeUsers returns the spaces that use a permission scheme.
// Jira has no reverse lookup, so this reads every space's scheme: one
// request per space, which apply makes only with --check-usage. The spaces
// are listed with action=edit, which with Administer Jira is every space.
func (r *Reader) PermissionSchemeUsers(id string) ([]string, error) {
	type project struct {
		ID string `json:"id"`
	}
	projects, err := jira.GetAll[project](r.ctx, r.c, "/rest/api/3/project/search", url.Values{"action": {"edit"}, "maxResults": {"50"}})
	if err != nil {
		return nil, fmt.Errorf("listing the spaces to check which use permission scheme %s: %w", id, err)
	}
	var out []string
	for _, p := range projects {
		var scheme struct {
			ID jira.ID `json:"id"`
		}
		if err := r.get("/rest/api/3/project/"+url.PathEscape(p.ID)+"/permissionscheme", nil, &scheme); err != nil {
			return nil, fmt.Errorf("checking which spaces use permission scheme %s: %w", id, err)
		}
		if string(scheme.ID) == id {
			out = util.AppendNew(out, p.ID)
		}
	}
	return out, nil
}
