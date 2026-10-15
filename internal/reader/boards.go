package reader

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/spec"
	"github.com/jerryjvl/jira-space-config/internal/util"
)

// The space's boards and filters: the boards located in the space, the
// filters they show, and the filters shared with the space. A filter whose
// description has the marker was made by this tool (mode managed), and so
// was a board whose filter was.

// sharePermission is one of a filter's viewers or editors as Jira gives it.
type sharePermission struct {
	Type    string `json:"type"`
	Project *struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	} `json:"project"`
	Role *struct {
		Name string `json:"name"`
	} `json:"role"`
	Group *struct {
		Name string `json:"name"`
	} `json:"group"`
	User *struct {
		AccountID string `json:"accountId"`
	} `json:"user"`
}

type filterJSON struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	JQL              string            `json:"jql"`
	SharePermissions []sharePermission `json:"sharePermissions"`
	EditPermissions  []sharePermission `json:"editPermissions"`
}

// filterExpand asks for what a filter search leaves out by default.
const filterExpand = "description,jql,sharePermissions,editPermissions"

// BoardsAndFilters reads the space's boards and filters.
func (r *Reader) BoardsAndFilters(p *Project) ([]spec.Filter, []spec.Board, error) {
	type board struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		Type     string `json:"type"`
		Location struct {
			ProjectID int `json:"projectId"`
		} `json:"location"`
	}
	all, err := jira.GetAll[board](r.ctx, r.c, "/rest/agile/1.0/board", url.Values{"projectKeyOrId": {p.Key}})
	var apiErr *jira.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusBadRequest && strings.Contains(apiErr.Error(), "browse project permission") {
		// Boards need Browse projects; an account that administers the
		// space without it still exports the rest, and is told.
		r.note("The space's boards, and the filters they show, could not be read: Jira lets only accounts with the Browse " +
			"projects permission read boards. Grant it to read them; the filters shared with the space are exported.")
		all, err = nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading the space's boards: %w", err)
	}
	byID := map[string]filterJSON{}
	var order []string
	add := func(f filterJSON) {
		if _, ok := byID[f.ID]; !ok {
			order = append(order, f.ID)
		}
		byID[f.ID] = f
	}
	var boards []spec.Board
	boardFilter := map[string]string{} // board name → filter ID
	for _, b := range all {
		if strconv.Itoa(b.Location.ProjectID) != p.ID {
			continue // a board elsewhere that shows this space's work items
		}
		var config struct {
			Filter struct {
				ID string `json:"id"`
			} `json:"filter"`
		}
		if err := r.get("/rest/agile/1.0/board/"+strconv.Itoa(b.ID)+"/configuration", nil, &config); err != nil {
			return nil, nil, fmt.Errorf("reading the configuration of board %q: %w", b.Name, err)
		}
		var f filterJSON
		if err := r.get("/rest/api/3/filter/"+url.PathEscape(config.Filter.ID), url.Values{"expand": {filterExpand}}, &f); err != nil {
			return nil, nil, fmt.Errorf("reading the filter of board %q: %w", b.Name, err)
		}
		add(f)
		boardFilter[b.Name] = f.ID
		boards = append(boards, spec.Board{Meta: spec.Meta{ID: strconv.Itoa(b.ID)}, Name: b.Name, Type: b.Type})
	}
	shared, err := jira.GetAll[filterJSON](r.ctx, r.c, "/rest/api/3/filter/search",
		url.Values{"projectId": {p.ID}, "expand": {filterExpand}})
	if err != nil {
		return nil, nil, fmt.Errorf("reading the filters shared with the space: %w", err)
	}
	for _, f := range shared {
		add(f)
	}
	var filters []spec.Filter
	names := map[string]spec.Filter{}
	for _, id := range order {
		f := byID[id]
		description, mode := ownership(f.Description)
		sf := spec.Filter{Meta: spec.Meta{ID: f.ID}, Name: f.Name, Mode: mode, Description: description, JQL: f.JQL,
			Viewers: r.shares(p, f.Name, f.SharePermissions), Editors: r.shares(p, f.Name, f.EditPermissions)}
		filters = append(filters, sf)
		names[f.ID] = sf
	}
	for i := range boards {
		f := names[boardFilter[boards[i].Name]]
		boards[i].Filter, boards[i].Mode = f.Name, f.Mode
	}
	return filters, boards, nil
}

// shares writes a filter's viewers or editors as holders; what a spec
// cannot say (a share with another space) is noted and left out.
func (r *Reader) shares(p *Project, filter string, perms []sharePermission) []string {
	var out []string
	for _, s := range perms {
		h := ""
		switch {
		case s.Type == "project" && s.Project != nil && s.Project.ID == p.ID && s.Role == nil:
			h = "space"
		case s.Type == "project" && s.Project != nil && s.Project.ID == p.ID:
			h = "role:" + s.Role.Name
		case s.Type == "group" && s.Group != nil:
			h = "group:" + s.Group.Name
		case s.Type == "user" && s.User != nil:
			h = "user:" + s.User.AccountID
		case s.Type == "loggedin":
			h = "logged_in"
		case s.Type == "global":
			h = "anyone"
		}
		if h == "" {
			where := s.Type
			switch {
			case s.Type == "project-unknown":
				where = "a space this account cannot see"
			case s.Project != nil:
				where = "space " + s.Project.Key
			}
			r.note("Filter %q is shared with %s, which a spec cannot say; it is left out of the filter's viewers or editors.", filter, where)
			continue
		}
		if !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	return out
}

// FindFilters returns the filters the account can see named exactly name.
func (r *Reader) FindFilters(p *Project, name string) ([]spec.Filter, error) {
	all, err := jira.GetAll[filterJSON](r.ctx, r.c, "/rest/api/3/filter/search",
		url.Values{"filterName": {name}, "expand": {filterExpand}})
	if err != nil {
		return nil, fmt.Errorf("looking up filters named %q: %w", name, err)
	}
	var out []spec.Filter
	for _, f := range all {
		if f.Name != name {
			continue
		}
		description, mode := ownership(f.Description)
		out = append(out, spec.Filter{Meta: spec.Meta{ID: f.ID}, Name: f.Name, Mode: mode, Description: description, JQL: f.JQL,
			Viewers: r.shares(p, f.Name, f.SharePermissions), Editors: r.shares(p, f.Name, f.EditPermissions)})
	}
	return out, nil
}

// FilterUsers returns the spaces whose boards show a filter. Other uses
// (dashboards, subscriptions, other filters) cannot be looked up; changing a
// filter can change them.
func (r *Reader) FilterUsers(id string) ([]string, error) {
	type board struct {
		Location struct {
			ProjectID int `json:"projectId"`
		} `json:"location"`
	}
	all, err := jira.GetAll[board](r.ctx, r.c, "/rest/agile/1.0/board", url.Values{"filterId": {id}})
	if err != nil {
		return nil, fmt.Errorf("checking which boards show filter %s: %w", id, err)
	}
	var out []string
	for _, b := range all {
		if b.Location.ProjectID == 0 {
			out = util.AppendNew(out, "a user's board") // a board of its own, not in a space: another use
			continue
		}
		out = util.AppendNew(out, strconv.Itoa(b.Location.ProjectID))
	}
	return out, nil
}
