package reader

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// Project is the space as Jira describes it; most readers take it as input.
type Project struct {
	ID, Key string
	Details *spec.Details
}

// project is the subset of GET /rest/api/3/project/{key} that is exported.
type project struct {
	ID           string            `json:"id"`
	Key          string            `json:"key"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Style        string            `json:"style"`
	Type         string            `json:"projectTypeKey"`
	AvatarURLs   map[string]string `json:"avatarUrls"`
	AssigneeType string            `json:"assigneeType"`
	URL          string            `json:"url"`
	Lead         struct {
		AccountID string `json:"accountId"`
	} `json:"lead"`
	Category *struct {
		Name string `json:"name"`
	} `json:"projectCategory"`
}

// Space reads a company-managed space. A team-managed space is an error:
// this tool does not support them.
func (r *Reader) Space(key string) (*Project, error) {
	var p project
	if err := r.get("/rest/api/3/project/"+url.PathEscape(key), nil, &p); err != nil {
		return nil, fmt.Errorf("reading space %s: %w", key, err)
	}
	if p.Style != "classic" {
		return nil, fmt.Errorf("space %s is team-managed (style %q); this tool only exports company-managed spaces", p.Key, p.Style)
	}
	d := &spec.Details{
		Name:         spec.Some(p.Name),
		Type:         spec.Some(p.Type),
		Description:  optText(p.Description),
		AssigneeType: assigneeType(p.AssigneeType),
		Category:     nullable(""),
		URL:          nullable(p.URL),
	}
	if p.Lead.AccountID != "" {
		d.Lead = spec.Some("user:" + p.Lead.AccountID)
	}
	if p.Category != nil {
		d.Category = nullable(p.Category.Name)
	}
	if id := avatarID(p.AvatarURLs["48x48"]); id != "" {
		d.Avatar = spec.Some(id)
	}
	return &Project{ID: p.ID, Key: p.Key, Details: d}, nil
}

func assigneeType(v string) spec.Opt[string] {
	switch v {
	case "UNASSIGNED":
		return spec.Some("unassigned")
	case "PROJECT_LEAD":
		return spec.Some("space_lead")
	}
	return spec.Opt[string]{}
}

// IssueTypes is the space's issue type scheme: its issue types in scheme
// order, as global definitions and as the space uses them. Readers of things
// configured per issue type take it as input and return per-issue-type
// results in the same order.
type IssueTypes struct {
	Binding     *spec.ContainerRef
	Definitions []spec.IssueType
	Space       []spec.SpaceIssueType
	ids         []string
}

// Len is the number of issue types in the space.
func (t *IssueTypes) Len() int { return len(t.ids) }

// ID returns the ID of the i-th issue type.
func (t *IssueTypes) ID(i int) string { return t.ids[i] }

// Name returns the name of an issue type of the space by ID.
func (t *IssueTypes) Name(id string) (string, bool) {
	if i := slices.Index(t.ids, id); i >= 0 {
		return t.Space[i].Name, true
	}
	return "", false
}

// IssueTypeScheme reads the space's issue types in scheme order, the
// default, and their global definitions.
func (r *Reader) IssueTypeScheme(p *Project) (*IssueTypes, error) {
	type schemeRef struct {
		IssueTypeScheme struct {
			ID                 string `json:"id"`
			Name               string `json:"name"`
			Description        string `json:"description"`
			DefaultIssueTypeID string `json:"defaultIssueTypeId"`
		} `json:"issueTypeScheme"`
	}
	schemes, err := jira.GetAll[schemeRef](r.ctx, r.c, "/rest/api/3/issuetypescheme/project", url.Values{"projectId": {p.ID}})
	if err != nil {
		return nil, fmt.Errorf("reading the issue type scheme: %w", err)
	}
	if len(schemes) != 1 {
		return nil, fmt.Errorf("expected one issue type scheme for space %s, Jira returned %d", p.Key, len(schemes))
	}
	scheme := schemes[0].IssueTypeScheme

	type mapping struct {
		IssueTypeID string `json:"issueTypeId"`
	}
	order, err := jira.GetAll[mapping](r.ctx, r.c, "/rest/api/3/issuetypescheme/mapping", url.Values{"issueTypeSchemeId": {scheme.ID}})
	if err != nil {
		return nil, fmt.Errorf("reading issue types of scheme %q: %w", scheme.Name, err)
	}

	type issueType struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		StoredName     string `json:"untranslatedName"`
		Description    string `json:"description"`
		HierarchyLevel int    `json:"hierarchyLevel"`
		AvatarID       *int   `json:"avatarId"`
		IconURL        string `json:"iconUrl"`
	}
	var details []issueType
	if err := r.get("/rest/api/3/issuetype/project", url.Values{"projectId": {p.ID}}, &details); err != nil {
		return nil, fmt.Errorf("reading the space's issue types: %w", err)
	}
	byID := map[string]issueType{}
	for _, t := range details {
		byID[t.ID] = t
	}

	out := &IssueTypes{Binding: containerRef(scheme.ID, scheme.Name, scheme.Description)}
	for _, m := range order {
		t, ok := byID[m.IssueTypeID]
		if !ok {
			return nil, fmt.Errorf("issue type %s is in scheme %q but Jira did not return its details", m.IssueTypeID, scheme.Name)
		}
		description, mode := globalOwnership(t.Description)
		t.Name = r.storedName("Issue type", t.Name, t.StoredName)
		it := spec.IssueType{
			GlobalObject: spec.GlobalObject{Meta: spec.Meta{ID: t.ID}, Name: t.Name, Mode: mode, Description: description},
			Level:        spec.Some(level(t.HierarchyLevel)),
		}
		if t.AvatarID != nil {
			it.Avatar = spec.Some(strconv.Itoa(*t.AvatarID))
		} else if t.IconURL != "" {
			it.Icon = spec.Some(sitePath(t.IconURL))
		}
		out.Definitions = append(out.Definitions, it)
		out.ids = append(out.ids, t.ID)
		out.Space = append(out.Space, spec.SpaceIssueType{Name: t.Name, Default: t.ID == scheme.DefaultIssueTypeID})
	}
	return out, nil
}

func level(hierarchyLevel int) string {
	switch hierarchyLevel {
	case -1:
		return "subtask"
	case 0:
		return "standard"
	case 1:
		return "epic"
	}
	return fmt.Sprint(hierarchyLevel)
}

// issueTypeNames names the space's issue types among ids, noting the others.
func (r *Reader) issueTypeNames(its *IssueTypes, ids []string, context string) []string {
	var out []string
	for _, id := range ids {
		if name, ok := its.Name(id); ok {
			out = append(out, name)
		} else {
			r.note("%s mentions issue type %s, which is not in this space; it is left out.", context, id)
		}
	}
	return out
}

// SpaceStatuses reads every status used by the space's issue types. Their
// stored names are what workflows read later refer to them by.
func (r *Reader) SpaceStatuses(p *Project) ([]spec.Status, error) {
	type status struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		StoredName     string `json:"untranslatedName"`
		StatusCategory struct {
			Key string `json:"key"`
		} `json:"statusCategory"`
	}
	var perType []struct {
		Statuses []status `json:"statuses"`
	}
	if err := r.get("/rest/api/3/project/"+url.PathEscape(p.Key)+"/statuses", nil, &perType); err != nil {
		return nil, fmt.Errorf("reading the space's statuses: %w", err)
	}
	var out []spec.Status
	seen := map[string]bool{}
	for _, t := range perType {
		for _, st := range t.Statuses {
			if seen[st.ID] {
				continue
			}
			seen[st.ID] = true
			st.Name = r.storedName("Status", st.Name, st.StoredName)
			r.statusNames[st.ID] = st.Name
			s := spec.Status{GlobalObject: spec.GlobalObject{Meta: spec.Meta{ID: st.ID}, Name: st.Name}}
			if cat, ok := statusCategories[st.StatusCategory.Key]; ok {
				s.Category = spec.Some(cat)
			} else {
				r.note("Status %q has status category %q, which the format has no name for; its category is left out.",
					st.Name, st.StatusCategory.Key)
			}
			out = append(out, s)
		}
	}
	// Descriptions come from the statuses API, the one apply compares with:
	// for Jira's own statuses the two endpoints word them differently.
	var ids []string
	for _, s := range out {
		ids = append(ids, s.ID)
	}
	defs, err := r.StatusesByID(ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		j := slices.IndexFunc(defs, func(d spec.Status) bool { return d.ID == out[i].ID })
		if j < 0 {
			return nil, fmt.Errorf("status %q is used by the space, but the statuses API does not return it", out[i].Name)
		}
		out[i].Description, out[i].Mode = defs[j].Description, defs[j].Mode
	}
	return out, nil
}

// statusCategories maps Jira's status category keys to the format's names.
var statusCategories = map[string]string{
	"new":           "todo",
	"indeterminate": "in_progress",
	"done":          "done",
}

// Resolutions reads every resolution on the site: all of them are available
// to every space.
func (r *Reader) Resolutions() ([]spec.Resolution, error) {
	if r.resolutionsRead {
		return r.resolutions, nil
	}
	type resolution struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		IsDefault   bool   `json:"isDefault"`
	}
	all, err := jira.GetAll[resolution](r.ctx, r.c, "/rest/api/3/resolution/search", nil)
	if err != nil {
		return nil, fmt.Errorf("reading resolutions: %w", err)
	}
	var out []spec.Resolution
	for _, res := range all {
		description, mode := globalOwnership(res.Description)
		out = append(out, spec.Resolution{
			GlobalObject: spec.GlobalObject{Meta: spec.Meta{ID: res.ID}, Name: res.Name, Mode: mode, Description: description},
			SiteDefault:  res.IsDefault,
		})
	}
	r.resolutions, r.resolutionsRead = out, true
	return out, nil
}

// Priorities is the space's priority scheme: the priorities it offers, as
// global definitions, and the scheme as the space uses it.
type Priorities struct {
	Definitions []spec.Priority
	Space       spec.SpacePriorities
}

// PriorityScheme reads the space's priority scheme and its priorities.
func (r *Reader) PriorityScheme(p *Project) (*Priorities, error) {
	type scheme = priorityScheme
	// The list cannot be filtered by space, so each non-default scheme is asked
	// whether it has this space; if none does, the default scheme applies.
	// That is one request plus one per non-default scheme (few, even on large sites).
	schemes, err := jira.GetAll[scheme](r.ctx, r.c, "/rest/api/3/priorityscheme", nil)
	if err != nil {
		return nil, fmt.Errorf("reading priority schemes: %w", err)
	}
	var sch *scheme
	for i := range schemes {
		if schemes[i].IsDefault {
			continue
		}
		type member struct {
			ID string `json:"id"`
		}
		projects, err := jira.GetAll[member](r.ctx, r.c, "/rest/api/3/priorityscheme/"+url.PathEscape(schemes[i].ID)+"/projects",
			url.Values{"projectId": {p.ID}})
		if err != nil {
			return nil, fmt.Errorf("reading the spaces of priority scheme %q: %w", schemes[i].Name, err)
		}
		if slices.ContainsFunc(projects, func(m member) bool { return m.ID == p.ID }) {
			sch = &schemes[i]
			break
		}
	}
	if sch == nil {
		for i := range schemes {
			if schemes[i].IsDefault {
				sch = &schemes[i]
			}
		}
	}
	if sch == nil {
		return nil, fmt.Errorf("no priority scheme applies to space %s, and Jira reported no default scheme", p.Key)
	}
	return r.prioritiesOf(*sch)
}

// priorityScheme is a priority scheme as Jira lists it.
type priorityScheme struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	DefaultPriorityID string `json:"defaultPriorityId"`
	IsDefault         bool   `json:"isDefault"`
}

// prioritiesOf reads a priority scheme's priorities.
func (r *Reader) prioritiesOf(sch priorityScheme) (*Priorities, error) {

	type priority struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Colour      string `json:"statusColor"`
		IconURL     string `json:"iconUrl"`
	}
	// The scheme's list marks the scheme's default priority isDefault, not the
	// site's (Jira Cloud has no site default; the field is in Space.Default).
	prios, err := jira.GetAll[priority](r.ctx, r.c, "/rest/api/3/priorityscheme/"+url.PathEscape(sch.ID)+"/priorities", nil)
	if err != nil {
		return nil, fmt.Errorf("reading priorities of scheme %q: %w", sch.Name, err)
	}
	out := &Priorities{Space: spec.SpacePriorities{Binding: containerRef(sch.ID, sch.Name, sch.Description)}}
	out.Space.Binding.SiteDefault = sch.IsDefault
	for _, pr := range prios {
		description, mode := globalOwnership(pr.Description)
		def := spec.Priority{
			GlobalObject: spec.GlobalObject{Meta: spec.Meta{ID: pr.ID}, Name: pr.Name, Mode: mode, Description: description},
			Colour:       optText(pr.Colour),
		}
		if pr.IconURL != "" {
			def.Icon = spec.Some(sitePath(pr.IconURL))
		}
		out.Definitions = append(out.Definitions, def)
		out.Space.Priorities = append(out.Space.Priorities, pr.Name)
		if pr.ID == sch.DefaultPriorityID {
			out.Space.Default = spec.Some(pr.Name)
		}
	}
	return out, nil
}

// storedName prefers the name Jira stores over the one it displays: display
// names are translated into the exporting account's language, so only the
// stored name is the same for everyone. A difference is noted.
func (r *Reader) storedName(kind, displayed, stored string) string {
	if stored == "" || stored == displayed {
		return displayed
	}
	r.note("%s %q is shown as %q in this account's language; the spec uses the stored name.", kind, stored, displayed)
	return stored
}

// With returns the issue types with others added, given as ID and name:
// issue types a spec adds to the space that the space does not have yet.
// Contexts are looked up per issue type, and a context made for one of them
// can only be found by asking about it.
func (t *IssueTypes) With(others []spec.SpaceIssueType, ids []string) *IssueTypes {
	out := *t
	out.Space = append(slices.Clone(t.Space), others...)
	out.ids = append(slices.Clone(t.ids), ids...)
	return &out
}

// IDOf returns the ID of an issue type of the space by name.
func (t *IssueTypes) IDOf(name string) (string, bool) {
	for i, s := range t.Space {
		if s.Name == name {
			return t.ids[i], true
		}
	}
	return "", false
}
