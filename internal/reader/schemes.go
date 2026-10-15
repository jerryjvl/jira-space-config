package reader

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/spec"
	"github.com/jerryjvl/jira-space-config/internal/util"
)

// Lookups for schemes by name, for apply: the scheme a spec names, or the
// one the tool would create, may already exist without being the space's.

// Scheme is a scheme found by name.
type Scheme struct {
	ID, Name string
	Mode     spec.Mode // managed if it carries the tool's marker
}

func scheme(id, name, description string) Scheme {
	_, mode := ownership(description)
	return Scheme{ID: id, Name: name, Mode: mode}
}

// IssueTypeSchemeContent is what an issue type scheme holds.
type IssueTypeSchemeContent struct {
	Scheme
	IssueTypeIDs []string // in the scheme's order
	DefaultID    string
}

// FindIssueTypeSchemes returns the issue type schemes named exactly name,
// with their issue types.
func (r *Reader) FindIssueTypeSchemes(name string) ([]IssueTypeSchemeContent, error) {
	type its struct {
		ID                 string `json:"id"`
		Name               string `json:"name"`
		Description        string `json:"description"`
		DefaultIssueTypeID string `json:"defaultIssueTypeId"`
	}
	all, err := jira.GetAll[its](r.ctx, r.c, "/rest/api/3/issuetypescheme", url.Values{"queryString": {name}})
	if err != nil {
		return nil, fmt.Errorf("looking up issue type schemes named %q: %w", name, err)
	}
	var out []IssueTypeSchemeContent
	for _, s := range all {
		if s.Name != name {
			continue
		}
		type mapping struct {
			IssueTypeID string `json:"issueTypeId"`
		}
		items, err := jira.GetAll[mapping](r.ctx, r.c, "/rest/api/3/issuetypescheme/mapping", url.Values{"issueTypeSchemeId": {s.ID}})
		if err != nil {
			return nil, fmt.Errorf("reading issue types of scheme %q: %w", s.Name, err)
		}
		c := IssueTypeSchemeContent{Scheme: scheme(s.ID, s.Name, s.Description), DefaultID: s.DefaultIssueTypeID}
		for _, m := range items {
			c.IssueTypeIDs = append(c.IssueTypeIDs, m.IssueTypeID)
		}
		out = append(out, c)
	}
	return out, nil
}

// WorkflowSchemeContent is what a workflow scheme holds.
type WorkflowSchemeContent struct {
	Scheme
	Default  string            // workflow name
	Mappings map[string]string // issue type ID → workflow name
}

// FindWorkflowSchemes returns the workflow schemes named exactly name. The
// list cannot be filtered by name, so it is read in full (sites have few).
func (r *Reader) FindWorkflowSchemes(name string) ([]WorkflowSchemeContent, error) {
	type ws struct {
		ID                int               `json:"id"`
		Name              string            `json:"name"`
		Description       string            `json:"description"`
		DefaultWorkflow   string            `json:"defaultWorkflow"`
		IssueTypeMappings map[string]string `json:"issueTypeMappings"`
	}
	all, err := jira.GetAll[ws](r.ctx, r.c, "/rest/api/3/workflowscheme", nil)
	if err != nil {
		return nil, fmt.Errorf("looking up workflow schemes: %w", err)
	}
	var out []WorkflowSchemeContent
	for _, s := range all {
		if s.Name == name {
			out = append(out, WorkflowSchemeContent{Scheme: scheme(strconv.Itoa(s.ID), s.Name, s.Description),
				Default: s.DefaultWorkflow, Mappings: s.IssueTypeMappings})
		}
	}
	return out, nil
}

// ScreenSchemeContent is what an issue type screen scheme holds: the
// screens of each issue type, by name.
type ScreenSchemeContent struct {
	Scheme
	Screens map[string]spec.ScreenSet // issue type ID, or "default" → screens
}

// FindIssueTypeScreenSchemes returns the issue type screen schemes named
// exactly name, with the screens they give each issue type.
func (r *Reader) FindIssueTypeScreenSchemes(name string) ([]ScreenSchemeContent, error) {
	type itss struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	all, err := jira.GetAll[itss](r.ctx, r.c, "/rest/api/3/issuetypescreenscheme", url.Values{"queryString": {name}})
	if err != nil {
		return nil, fmt.Errorf("looking up issue type screen schemes named %q: %w", name, err)
	}
	var out []ScreenSchemeContent
	for _, s := range all {
		if s.Name != name {
			continue
		}
		type mapping struct {
			IssueTypeID    string `json:"issueTypeId"`
			ScreenSchemeID string `json:"screenSchemeId"`
		}
		mappings, err := jira.GetAll[mapping](r.ctx, r.c, "/rest/api/3/issuetypescreenscheme/mapping",
			url.Values{"issueTypeScreenSchemeId": {s.ID}})
		if err != nil {
			return nil, fmt.Errorf("reading the mappings of %q: %w", s.Name, err)
		}
		var ids []string
		for _, m := range mappings {
			ids = util.AppendNew(ids, m.ScreenSchemeID)
		}
		schemes, err := jira.GetAll[screenScheme](r.ctx, r.c, "/rest/api/3/screenscheme", url.Values{"id": ids})
		if err != nil {
			return nil, fmt.Errorf("reading screen schemes: %w", err)
		}
		var screenIDs []string
		for _, ss := range schemes {
			for _, id := range []int{ss.Screens.Default, ss.Screens.Create, ss.Screens.Edit, ss.Screens.View} {
				if id != 0 {
					screenIDs = util.AppendNew(screenIDs, strconv.Itoa(id))
				}
			}
		}
		found, err := r.ScreensByID(screenIDs)
		if err != nil {
			return nil, err
		}
		names := map[string]string{}
		for _, f := range found {
			names[f.ID] = f.Name
		}
		c := ScreenSchemeContent{Scheme: scheme(s.ID, s.Name, s.Description), Screens: map[string]spec.ScreenSet{}}
		for _, m := range mappings {
			for _, ss := range schemes {
				if strconv.Itoa(ss.ID) != m.ScreenSchemeID {
					continue
				}
				pick := func(op int) string { return names[strconv.Itoa(orDefault(op, ss.Screens.Default))] }
				c.Screens[m.IssueTypeID] = spec.ScreenSet{Create: pick(ss.Screens.Create), Edit: pick(ss.Screens.Edit), View: pick(ss.Screens.View)}
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// FieldSchemeContent is a field scheme or field configuration scheme found
// by name, with the fields it gives the space's issue types.
type FieldSchemeContent struct {
	Scheme
	Layout *FieldLayout
}

// FindFieldSchemes returns the field schemes named exactly name.
func (r *Reader) FindFieldSchemes(its *IssueTypes, name string) ([]FieldSchemeContent, error) {
	type fs struct {
		ID          jira.ID `json:"id"` // a number in the list, a string elsewhere
		Name        string  `json:"name"`
		Description string  `json:"description"`
	}
	all, err := jira.GetAll[fs](r.ctx, r.c, "/rest/api/3/config/fieldschemes", url.Values{"query": {name}})
	if err != nil {
		return nil, fmt.Errorf("looking up field schemes named %q: %w", name, err)
	}
	var out []FieldSchemeContent
	for _, s := range all {
		if s.Name != name {
			continue
		}
		layout, err := r.fieldScheme(its, string(s.ID))
		if err != nil {
			return nil, err
		}
		out = append(out, FieldSchemeContent{Scheme: scheme(string(s.ID), s.Name, s.Description), Layout: layout})
	}
	return out, r.done()
}

// FindFieldConfigurationSchemes returns the field configuration schemes
// named exactly name. The list cannot be filtered by name, so it is read in
// full (sites have few).
func (r *Reader) FindFieldConfigurationSchemes(its *IssueTypes, name string) ([]FieldSchemeContent, error) {
	all, err := jira.GetAll[fcsRef](r.ctx, r.c, "/rest/api/3/fieldconfigurationscheme", nil)
	if err != nil {
		return nil, fmt.Errorf("looking up field configuration schemes: %w", err)
	}
	var out []FieldSchemeContent
	for _, s := range all {
		if s.Name != name {
			continue
		}
		layout, err := r.fieldConfigurationScheme(its, &s)
		if err != nil {
			return nil, err
		}
		out = append(out, FieldSchemeContent{Scheme: scheme(s.ID, s.Name, s.Description), Layout: layout})
	}
	return out, r.done()
}

// FindPermissionSchemes returns the permission schemes named exactly name,
// with their grants. The list cannot be filtered by name (sites have few).
func (r *Reader) FindPermissionSchemes(name string) ([]*spec.Permissions, error) {
	var list struct {
		Schemes []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"permissionSchemes"`
	}
	if err := r.get("/rest/api/3/permissionscheme", nil, &list); err != nil {
		return nil, fmt.Errorf("looking up permission schemes: %w", err)
	}
	var out []*spec.Permissions
	for _, s := range list.Schemes {
		if s.Name != name {
			continue
		}
		var scheme permissionScheme
		if err := r.get("/rest/api/3/permissionscheme/"+strconv.Itoa(s.ID), url.Values{"expand": {"all"}}, &scheme); err != nil {
			return nil, fmt.Errorf("reading permission scheme %q: %w", s.Name, err)
		}
		perms, err := r.permissions(scheme)
		if err != nil {
			return nil, err
		}
		out = append(out, perms)
	}
	return out, nil
}

// FindNotificationSchemes returns the notification schemes named exactly
// name, with their events. The list cannot be filtered by name.
func (r *Reader) FindNotificationSchemes(name string) ([]*spec.Notifications, error) {
	all, err := jira.GetAll[notificationScheme](r.ctx, r.c, "/rest/api/3/notificationscheme", url.Values{"expand": {"all"}})
	if err != nil {
		return nil, fmt.Errorf("looking up notification schemes: %w", err)
	}
	var out []*spec.Notifications
	for _, s := range all {
		if s.Name != name {
			continue
		}
		n, err := r.notifications(s)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// FindIssueSecuritySchemes returns the issue security schemes named exactly
// name, with their levels. The list cannot be filtered by name.
func (r *Reader) FindIssueSecuritySchemes(name string) ([]*spec.IssueSecurity, error) {
	var list struct {
		Schemes []securityScheme `json:"issueSecuritySchemes"`
	}
	if err := r.get("/rest/api/3/issuesecurityschemes", nil, &list); err != nil {
		return nil, fmt.Errorf("looking up issue security schemes: %w", err)
	}
	var out []*spec.IssueSecurity
	for _, s := range list.Schemes {
		if s.Name != name {
			continue
		}
		is, err := r.issueSecurityScheme(s)
		if err != nil {
			return nil, err
		}
		out = append(out, is)
	}
	return out, nil
}

// FindPrioritySchemes returns the priority schemes named exactly name, with
// their priorities.
func (r *Reader) FindPrioritySchemes(name string) ([]*Priorities, error) {
	all, err := jira.GetAll[priorityScheme](r.ctx, r.c, "/rest/api/3/priorityscheme", url.Values{"schemeName": {name}})
	if err != nil {
		return nil, fmt.Errorf("looking up priority schemes named %q: %w", name, err)
	}
	var out []*Priorities
	for _, s := range all {
		if s.Name != name {
			continue
		}
		p, err := r.prioritiesOf(s)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
