package steps

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/spec"
	"github.com/jerryjvl/jira-space-config/internal/util"
)

// The steps for the space's views: its filters (step 26) and boards (27).
// A managed filter is created when missing and changed while only this
// space's boards show it; a board is created when missing and compared
// otherwise, as Jira's API cannot change one.

const viewsIntro = "Filters and boards are what the space's users look at. A managed filter (mode managed, the default) is " +
	"created when missing, and changed to match while no other space's board shows it; dashboards and subscriptions " +
	"that use it cannot be looked up. A missing board is created in the space; an existing one is compared, as Jira's " +
	"API cannot change a board's type or filter. Columns, swimlanes and quick filters are set on the board by hand."

func filterObject(f spec.Filter) spec.GlobalObject {
	return spec.GlobalObject{Meta: f.Meta, Name: f.Name, Mode: f.Mode, Description: f.Description}
}

func filters(env *Env) (*Step, error) {
	return evaluateWith(env, 26, "Filters", "filter", "filters", viewsIntro, func() ([]Item, error) {
		var live []spec.Filter
		for _, f := range env.Spec.Filters {
			found, err := env.R.FindFilters(env.P, f.Name)
			if err != nil {
				return nil, err
			}
			live = append(live, found...)
		}
		x := resolver{env.R, env.Spec}
		return globals(env, kind[spec.Filter]{
			noun: "filter", plural: "filters", shareable: true, anyone: true, object: filterObject,
			compare: compareFilters,
			create:  func(f spec.Filter) string { return "new filter · " + util.Plural(len(f.Viewers), "viewer", "viewers") },
			request: "POST /rest/api/3/filter",
			do:      func(f spec.Filter) action { return createFilter(env, x, f) },
			update:  func(w, g spec.Filter) change { return updateFilter(env, x, w, g) },
			usage:   func(g spec.Filter) ([]string, error) { return env.R.FilterUsers(g.ID) },
		}, env.Spec.Filters, live)
	})
}

// compareFilters compares the JQL and who may view and edit the filter.
func compareFilters(want, got spec.Filter) []Diff {
	var l diffList
	if want.JQL != got.JQL {
		l = append(l, Diff{Attribute: "jql", Spec: want.JQL, Jira: got.JQL})
	}
	l.add(compareSet("viewers", nonNil(viewersOnly(want)), viewersOnly(got)))
	l.add(compareSet("editors", nonNil(want.Editors), got.Editors))
	return l
}

// nonNil makes an unlisted holder list "none": a filter's viewers and
// editors are its complete lists.
func nonNil(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

func boardObject(b spec.Board) spec.GlobalObject {
	return spec.GlobalObject{Meta: b.Meta, Name: b.Name, Mode: b.Mode}
}

func boards(env *Env) (*Step, error) {
	return evaluateWith(env, 27, "Boards", "board", "boards", viewsIntro, func() ([]Item, error) {
		if !env.Browse {
			var items []Item
			for _, b := range env.Spec.Boards {
				items = append(items, Item{Kind: Error, Name: b.Name, Summary: "this account cannot read the space's boards",
					Fix: "Jira lets only accounts with the Browse projects permission read boards. Grant it, or set boards up by hand."})
			}
			return items, nil
		}
		_, live, err := env.R.BoardsAndFilters(env.P)
		if err != nil {
			return nil, err
		}
		x := resolver{env.R, env.Spec}
		return globals(env, kind[spec.Board]{
			noun: "board", plural: "boards", shareable: true, anyone: true, object: boardObject,
			compare: func(w, g spec.Board) []Diff {
				var l diffList
				l.add(compareText("type", spec.Some(w.Type), spec.Some(g.Type)))
				l.add(compareText("filter", spec.Some(w.Filter), spec.Some(g.Filter)))
				return l
			},
			create:  func(b spec.Board) string { return fmt.Sprintf("new %s board · filter %q", b.Type, b.Filter) },
			request: "POST /rest/agile/1.0/board",
			do:      func(b spec.Board) action { return createBoard(env, x, b) },
			fixed: "Jira's API cannot change a board's type or filter: change it in the board's settings by hand, " +
				"or give the spec's board another name so a new one is made.",
		}, env.Spec.Boards, live)
	})
}

// shareBody is a filter's viewer or editor as Jira takes it.
func shareBody(env *Env, x resolver, h string) (map[string]any, error) {
	kind, value, _ := strings.Cut(h, ":")
	project := map[string]string{"id": env.P.ID}
	switch kind {
	case "space":
		return map[string]any{"type": "project", "project": project}, nil
	case "role":
		id, err := x.role(value)
		if err != nil {
			return nil, err
		}
		// Type project with a role is taken as the whole space: projectRole it
		// must be (and Jira lets only the role's members share with it).
		n, _ := strconv.Atoi(id)
		return map[string]any{"type": "projectRole", "project": project, "role": map[string]int{"id": n}}, nil
	case "group":
		id, err := x.group(value)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "group", "group": map[string]string{"groupId": id}}, nil
	case "user":
		return map[string]any{"type": "user", "user": map[string]string{"accountId": value}}, nil
	case "logged_in":
		return map[string]any{"type": "loggedin"}, nil
	case "anyone":
		return map[string]any{"type": "global"}, nil
	}
	// checkcover:unreachable validate allows only the kinds above.
	return nil, fmt.Errorf("%q cannot be a filter's viewer or editor", h)
}

// filterBody is a filter as Jira takes it, with the marker.
func filterBody(env *Env, x resolver, f spec.Filter) (map[string]any, error) {
	shares := func(holders []string) ([]map[string]any, error) {
		out := []map[string]any{}
		for _, h := range holders {
			s, err := shareBody(env, x, h)
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		}
		return out, nil
	}
	viewers, err := shares(viewersOnly(f))
	if err != nil {
		return nil, err
	}
	editors, err := shares(f.Editors)
	if err != nil {
		return nil, err
	}
	return map[string]any{"name": f.Name, "description": managedDescription(f.Description), "jql": f.JQL,
		"sharePermissions": viewers, "editPermissions": editors}, nil
}

func createFilter(env *Env, x resolver, f spec.Filter) action {
	return func(ctx context.Context, snd Sender) (string, error) {
		body, err := filterBody(env, x, f)
		if err != nil {
			return "", err
		}
		return send(http.MethodPost, "/rest/api/3/filter", body)(ctx, snd)
	}
}

// updateFilter gives a managed filter the spec's JQL, description, viewers
// and editors. A viewer or editor added widens access
// (--allow-update=filters); one taken away needs --allow-delete=filters.
func updateFilter(env *Env, x resolver, want, got spec.Filter) change {
	ch := change{class: "additive", allow: "filters", widenAllow: "filters",
		requests: []string{"PUT /rest/api/3/filter/{id}"}}
	for _, list := range []struct {
		noun      string
		want, got []string
	}{{"viewer", viewersOnly(want), viewersOnly(got)}, {"editor", want.Editors, got.Editors}} {
		add, remove := addedAndRemoved(list.want, list.got)
		for _, h := range add {
			ch.widens = append(ch.widens, list.noun+" "+h)
		}
		for _, h := range remove {
			ch.removes = append(ch.removes, list.noun+" "+h)
		}
	}
	switch {
	case len(ch.removes) > 0:
		ch.class = "removing"
	case want.JQL != got.JQL || show(want.Description) != show(got.Description):
		ch.class = "altering"
	}
	ch.do = func(ctx context.Context, snd Sender) (string, error) {
		body, err := filterBody(env, x, want)
		if err != nil {
			return "", err
		}
		if err := snd.Send(ctx, http.MethodPut, "/rest/api/3/filter/"+got.ID, nil, body, nil); err != nil {
			return "", err
		}
		return got.ID, nil
	}
	return ch
}

// createBoard creates a board in the space, showing the spec's filter
// (step 26 has made it, or it exists).
func createBoard(env *Env, x resolver, b spec.Board) action {
	return func(ctx context.Context, snd Sender) (string, error) {
		found, err := x.r.FindFilters(env.P, b.Filter)
		if err != nil {
			return "", err
		}
		if len(found) != 1 {
			return "", fmt.Errorf("%s this account can see are named %q; one is needed", util.Plural(len(found), "filter", "filters"), b.Filter)
		}
		id, _ := strconv.Atoi(found[0].ID)
		body := map[string]any{"name": b.Name, "type": b.Type, "filterId": id,
			"location": map[string]string{"type": "project", "projectKeyOrId": env.P.Key}}
		return send(http.MethodPost, "/rest/agile/1.0/board", body)(ctx, snd)
	}
}

// viewersOnly are a filter's viewers that are not also editors: editors can
// view, and Jira keeps no view share an edit share implies.
func viewersOnly(f spec.Filter) []string {
	var out []string
	for _, v := range f.Viewers {
		if !slices.Contains(f.Editors, v) {
			out = append(out, v)
		}
	}
	return out
}
