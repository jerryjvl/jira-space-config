package reader

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/spec"
	"github.com/jerryjvl/jira-space-config/internal/util"
)

type screenScheme struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Screens     struct {
		Default, Create, Edit, View int
	} `json:"screens"`
}

// ScreenLayout is the space's issue type screen scheme flattened to
// create/edit/view screens per issue type; the screen scheme is kept in the
// binding, so its name survives.
type ScreenLayout struct {
	Binding *spec.ContainerRef
	// Screens and ScreenSchemes are per issue type, in the order of the
	// IssueTypes given.
	Screens       []*spec.ScreenSet
	ScreenSchemes []*spec.ContainerRef
	// Definitions are the screens used, with their tabs and fields.
	Definitions []spec.Screen
}

// ScreenScheme reads the space's issue type screen scheme and the screens it uses.
func (r *Reader) ScreenScheme(p *Project, its *IssueTypes) (*ScreenLayout, error) {
	type itssRef struct {
		IssueTypeScreenScheme struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"issueTypeScreenScheme"`
	}
	refs, err := jira.GetAll[itssRef](r.ctx, r.c, "/rest/api/3/issuetypescreenscheme/project", url.Values{"projectId": {p.ID}})
	if err != nil {
		return nil, fmt.Errorf("reading the issue type screen scheme: %w", err)
	}
	if len(refs) != 1 {
		return nil, fmt.Errorf("expected one issue type screen scheme for space %s, Jira returned %d", p.Key, len(refs))
	}
	itss := refs[0].IssueTypeScreenScheme

	type mapping struct {
		IssueTypeID    string `json:"issueTypeId"` // an issue type ID, or "default"
		ScreenSchemeID string `json:"screenSchemeId"`
	}
	mappings, err := jira.GetAll[mapping](r.ctx, r.c, "/rest/api/3/issuetypescreenscheme/mapping",
		url.Values{"issueTypeScreenSchemeId": {itss.ID}})
	if err != nil {
		return nil, fmt.Errorf("reading the mappings of %q: %w", itss.Name, err)
	}
	schemeFor := map[string]string{}
	var schemeIDs []string
	for _, m := range mappings {
		schemeFor[m.IssueTypeID] = m.ScreenSchemeID
		schemeIDs = util.AppendNew(schemeIDs, m.ScreenSchemeID)
	}

	schemes, err := jira.GetAll[screenScheme](r.ctx, r.c, "/rest/api/3/screenscheme", url.Values{"id": schemeIDs})
	if err != nil {
		return nil, fmt.Errorf("reading screen schemes: %w", err)
	}
	schemeByID := map[string]screenScheme{}
	var screenIDs []string
	for _, s := range schemes {
		schemeByID[strconv.Itoa(s.ID)] = s
		for _, id := range []int{s.Screens.Default, s.Screens.Create, s.Screens.Edit, s.Screens.View} {
			if id != 0 {
				screenIDs = util.AppendNew(screenIDs, strconv.Itoa(id))
			}
		}
	}

	out := &ScreenLayout{Binding: containerRef(itss.ID, itss.Name, itss.Description)}
	if out.Definitions, err = r.Screens(screenIDs); err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, s := range out.Definitions {
		names[s.ID] = s.Name
	}

	for i := range its.Len() {
		ssID, ok := schemeFor[its.ID(i)]
		if !ok {
			ssID = schemeFor["default"]
		}
		ss, ok := schemeByID[ssID]
		if !ok {
			return nil, fmt.Errorf("issue type %q uses screen scheme %s, which Jira did not return", its.Space[i].Name, ssID)
		}
		pick := func(op int) string { return names[strconv.Itoa(orDefault(op, ss.Screens.Default))] }
		out.Screens = append(out.Screens, &spec.ScreenSet{Create: pick(ss.Screens.Create), Edit: pick(ss.Screens.Edit), View: pick(ss.Screens.View)})
		out.ScreenSchemes = append(out.ScreenSchemes, containerRef(strconv.Itoa(ss.ID), ss.Name, ss.Description))
	}
	for issueTypeID := range schemeFor {
		if _, inSpace := its.Name(issueTypeID); !inSpace && issueTypeID != "default" {
			r.note("Issue type screen scheme %q maps issue type %s, which is not in this space; that mapping is left out.",
				itss.Name, issueTypeID)
		}
	}
	return out, r.done()
}

func orDefault(op, def int) int {
	if op == 0 {
		return def
	}
	return op
}

// Screens reads the screens with the given IDs, with their tabs and fields
// in Jira's order, and returns them in the order Jira lists them. Screens
// already read in this run are not read again.
func (r *Reader) Screens(ids []string) ([]spec.Screen, error) {
	var missing []string
	for _, id := range ids {
		if _, ok := r.screens[id]; !ok {
			missing = append(missing, id)
		}
	}
	var out []spec.Screen
	if len(missing) > 0 {
		read, err := r.readScreens(missing)
		if err != nil {
			return nil, err
		}
		out = read
	}
	for _, id := range ids {
		if !containsScreen(out, id) {
			out = append(out, r.screens[id])
		}
	}
	return out, r.done()
}

func containsScreen(list []spec.Screen, id string) bool {
	for _, s := range list {
		if s.ID == id {
			return true
		}
	}
	return false
}

func (r *Reader) readScreens(ids []string) ([]spec.Screen, error) {
	type screen struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	screens, err := jira.GetAll[screen](r.ctx, r.c, "/rest/api/3/screens", url.Values{"id": ids})
	if err != nil {
		return nil, fmt.Errorf("reading screens: %w", err)
	}
	var out []spec.Screen
	for _, s := range screens {
		id := strconv.Itoa(s.ID)
		description, mode := ownership(s.Description)
		def := spec.Screen{Meta: spec.Meta{ID: id}, Name: s.Name, Mode: mode, Description: description}

		var tabs []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
		if err := r.get("/rest/api/3/screens/"+id+"/tabs", nil, &tabs); err != nil {
			return nil, fmt.Errorf("reading tabs of screen %q: %w", s.Name, err)
		}
		for _, tab := range tabs {
			var fields []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if err := r.get(fmt.Sprintf("/rest/api/3/screens/%s/tabs/%d/fields", id, tab.ID), nil, &fields); err != nil {
				return nil, fmt.Errorf("reading fields of tab %q on screen %q: %w", tab.Name, s.Name, err)
			}
			st := spec.ScreenTab{Name: tab.Name}
			for _, f := range fields {
				st.Fields = append(st.Fields, r.fieldName(f.ID))
			}
			def.Tabs = append(def.Tabs, st)
		}
		r.screens[id] = def
		out = append(out, def)
	}
	for _, id := range ids {
		if _, ok := r.screens[id]; !ok {
			return nil, fmt.Errorf("screen %s is used by the space but Jira did not return it", id)
		}
	}
	return out, nil
}
