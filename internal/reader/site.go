package reader

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// Site-wide lookups, for objects a spec names whether or not the space uses
// them yet. Apply compares the spec's global objects with these.

// SiteRoleTypes reads every role type on the site, with its default members.
// Role types of team-managed spaces are left out.
func (r *Reader) SiteRoleTypes() ([]spec.Role, error) {
	all, err := r.loadRoleTypes()
	if err != nil {
		return nil, fmt.Errorf("reading role types: %w", err)
	}
	var out []spec.Role
	for _, rt := range all {
		if rt.Scope != nil {
			continue
		}
		description, mode := globalOwnership(rt.Description)
		out = append(out, spec.Role{
			GlobalObject:   spec.GlobalObject{Meta: spec.Meta{ID: strconv.Itoa(rt.ID)}, Name: rt.Name, Mode: mode, Description: description},
			DefaultMembers: r.actorHolders(rt.Actors, fmt.Sprintf("Default members of role %q", rt.Name)),
		})
	}
	return out, nil
}

// FindGroups reads the groups with any of the given names or IDs. Jira
// matches group names exactly, ignoring case.
func (r *Reader) FindGroups(names, ids []string) ([]spec.GlobalObject, error) {
	if len(names) == 0 && len(ids) == 0 {
		return nil, nil
	}
	type group struct {
		GroupID string `json:"groupId"`
		Name    string `json:"name"`
	}
	q := url.Values{}
	for _, n := range names {
		q.Add("groupName", n)
	}
	for _, id := range ids {
		q.Add("groupId", id)
	}
	all, err := jira.GetAll[group](r.ctx, r.c, "/rest/api/3/group/bulk", q)
	if err != nil {
		return nil, fmt.Errorf("looking up groups: %w", err)
	}
	var out []spec.GlobalObject
	for _, g := range all {
		out = append(out, spec.GlobalObject{Meta: spec.Meta{ID: g.GroupID}, Name: g.Name, Mode: spec.ModeReuse})
	}
	return out, nil
}

// jiraStatus is a status as the statuses API returns it.
type jiraStatus struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	StatusCategory string `json:"statusCategory"` // TODO, IN_PROGRESS, DONE
	Scope          struct {
		Type string `json:"type"` // GLOBAL, or PROJECT for team-managed spaces' own
	} `json:"scope"`
}

var statusCategoryKeys = map[string]string{"TODO": "todo", "IN_PROGRESS": "in_progress", "DONE": "done"}

func (r *Reader) globalStatuses(all []jiraStatus) []spec.Status {
	var out []spec.Status
	for _, st := range all {
		if st.Scope.Type != "" && st.Scope.Type != "GLOBAL" {
			continue
		}
		description, mode := globalOwnership(st.Description)
		s := spec.Status{GlobalObject: spec.GlobalObject{Meta: spec.Meta{ID: st.ID}, Name: st.Name, Mode: mode, Description: description}}
		if cat, ok := statusCategoryKeys[st.StatusCategory]; ok {
			s.Category = spec.Some(cat)
		}
		out = append(out, s)
	}
	return out
}

// FindStatuses reads the site's statuses whose name contains name (Jira
// matches without regard to case), so near-misses are found too. Statuses
// of team-managed spaces are left out.
func (r *Reader) FindStatuses(name string) ([]spec.Status, error) {
	all, err := jira.GetAll[jiraStatus](r.ctx, r.c, "/rest/api/3/statuses/search", url.Values{"searchString": {name}})
	if err != nil {
		return nil, fmt.Errorf("looking up statuses named %q: %w", name, err)
	}
	return r.globalStatuses(all), nil
}

// StatusesByID reads statuses by ID; IDs Jira does not know are left out.
func (r *Reader) StatusesByID(ids []string) ([]spec.Status, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var all []jiraStatus
	if err := r.get("/rest/api/3/statuses", url.Values{"id": ids}, &all); err != nil {
		return nil, fmt.Errorf("looking up statuses by ID: %w", err)
	}
	return r.globalStatuses(all), nil
}

// SitePriorities reads every priority on the site, in the site's order.
func (r *Reader) SitePriorities() ([]spec.Priority, error) {
	type priority struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Colour      string `json:"statusColor"`
		IconURL     string `json:"iconUrl"`
		IsDefault   bool   `json:"isDefault"`
	}
	all, err := jira.GetAll[priority](r.ctx, r.c, "/rest/api/3/priority/search", nil)
	if err != nil {
		return nil, fmt.Errorf("reading priorities: %w", err)
	}
	var out []spec.Priority
	for _, p := range all {
		description, mode := globalOwnership(p.Description)
		def := spec.Priority{
			GlobalObject: spec.GlobalObject{Meta: spec.Meta{ID: p.ID}, Name: p.Name, Mode: mode, Description: description},
			Colour:       optText(p.Colour),
			SiteDefault:  p.IsDefault,
		}
		if p.IconURL != "" {
			def.Icon = spec.Some(sitePath(p.IconURL))
		}
		out = append(out, def)
	}
	return out, nil
}

// SiteLinkTypes reads every issue link type on the site.
func (r *Reader) SiteLinkTypes() ([]spec.LinkType, error) {
	var all struct {
		Types []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Inward  string `json:"inward"`
			Outward string `json:"outward"`
		} `json:"issueLinkTypes"`
	}
	if err := r.get("/rest/api/3/issueLinkType", nil, &all); err != nil {
		return nil, fmt.Errorf("reading issue link types: %w", err)
	}
	var out []spec.LinkType
	for _, t := range all.Types {
		out = append(out, spec.LinkType{
			GlobalObject: spec.GlobalObject{Meta: spec.Meta{ID: t.ID}, Name: t.Name, Mode: spec.ModeReuse},
			Inward:       optText(t.Inward), Outward: optText(t.Outward),
		})
	}
	return out, nil
}

// SiteIssueTypes reads every issue type on the site, by stored name. Issue
// types of team-managed spaces are left out.
func (r *Reader) SiteIssueTypes() ([]spec.IssueType, error) {
	var all []struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		StoredName     string `json:"untranslatedName"`
		Description    string `json:"description"`
		HierarchyLevel int    `json:"hierarchyLevel"`
		AvatarID       *int   `json:"avatarId"`
		IconURL        string `json:"iconUrl"`
		Scope          *struct {
			Type string `json:"type"`
		} `json:"scope"`
	}
	if err := r.get("/rest/api/3/issuetype", nil, &all); err != nil {
		return nil, fmt.Errorf("reading issue types: %w", err)
	}
	var out []spec.IssueType
	for _, t := range all {
		if t.Scope != nil {
			continue
		}
		description, mode := globalOwnership(t.Description)
		name := t.Name
		if t.StoredName != "" {
			name = t.StoredName
		}
		it := spec.IssueType{
			GlobalObject: spec.GlobalObject{Meta: spec.Meta{ID: t.ID}, Name: name, Mode: mode, Description: description},
			Level:        spec.Some(level(t.HierarchyLevel)),
		}
		if t.AvatarID != nil {
			it.Avatar = spec.Some(strconv.Itoa(*t.AvatarID))
		} else if t.IconURL != "" {
			it.Icon = spec.Some(sitePath(t.IconURL))
		}
		out = append(out, it)
	}
	return out, nil
}

// SiteCustomField is a custom field of the site with its full type key, for
// specs that name an app field's type in full.
type SiteCustomField struct {
	spec.CustomField
	TypeKey string
}

// SiteCustomFields reads every custom field on the site, by stored name.
func (r *Reader) SiteCustomFields() ([]SiteCustomField, error) {
	if r.siteFields == nil {
		if err := r.loadSiteFields(); err != nil {
			return nil, err
		}
	}
	var out []SiteCustomField
	for _, id := range sortedKeys(r.siteFields) {
		f := r.siteFields[id]
		if !f.Custom {
			continue
		}
		cf := spec.CustomField{
			GlobalObject: spec.GlobalObject{Meta: spec.Meta{ID: id}, Name: r.customName(id)},
			Type:         shortFieldType(f.Schema.Custom),
			Searcher:     optText(f.SearcherKey),
			Locked:       f.IsLocked,
		}
		cf.Description, cf.Mode = globalOwnership(f.Description)
		out = append(out, SiteCustomField{CustomField: cf, TypeKey: f.Schema.Custom})
	}
	return out, nil
}

// WorkItemCount is the number of work items in the space, from Jira's search
// index (it can lag briefly behind recent changes).
func (r *Reader) WorkItemCount(p *Project) (int, error) {
	n, err := r.CountWorkItems("project = " + p.ID)
	if err != nil {
		return 0, fmt.Errorf("counting the space's work items: %w", err)
	}
	return n, nil
}

// CountWorkItems counts the work items a JQL query finds, from Jira's search
// index (it can lag briefly behind recent changes).
func (r *Reader) CountWorkItems(jql string) (int, error) {
	var out struct {
		Count int `json:"count"`
	}
	if err := r.c.PostRead(r.ctx, "/rest/api/3/search/approximate-count", nil, map[string]string{"jql": jql}, &out); err != nil {
		return 0, fmt.Errorf("counting work items (%s): %w", jql, err)
	}
	return out.Count, nil
}
