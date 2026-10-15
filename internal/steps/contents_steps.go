package steps

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// The steps for the space itself: its details (step 21), components (22),
// versions (23) and role members (24). They belong to the space alone, so
// what differs is updated and what is missing is created. Nothing is
// deleted: what Jira has and the spec does not is noted and left as it is.

const contentsIntro = "The space's own details, components, versions and role members belong to this space alone: " +
	"what differs is updated, what is missing is created. Nothing is deleted; what Jira has and the spec does not " +
	"is left as it is, and noted."

func compareBool(attr string, want, got spec.Opt[bool]) (Diff, bool) {
	if !want.Has() || want.V == got.V {
		return Diff{}, false
	}
	return Diff{Attribute: attr, Spec: strconv.FormatBool(want.V), Jira: strconv.FormatBool(got.V)}, true
}

func details(env *Env) (*Step, error) {
	return evaluateWith(env, 21, "Space details", "space detail", "space details", contentsIntro, func() ([]Item, error) {
		want, got := env.Spec.Space.Details, env.P.Details
		var l diffList
		l.add(compareText("name", want.Name, got.Name))
		l.add(compareText("description", want.Description, got.Description))
		l.add(compareText("lead", want.Lead, got.Lead))
		l.add(compareText("assignee_type", want.AssigneeType, got.AssigneeType))
		l.add(compareText("category", want.Category, got.Category))
		l.add(compareText("url", want.URL, got.URL))
		l.add(compareText("avatar", want.Avatar, got.Avatar))
		item := Item{Name: "Details"}
		if d, ok := compareText("type", want.Type, got.Type); ok {
			item.Kind = Error
			item.Summary = "the spec gives another type of space:"
			item.Diffs = []Diff{d}
			item.Fix = "Jira cannot change a space's type through the API. Change the type in the spec to match, or " +
				"use a space of the type you want."
			return []Item{item}, nil
		}
		if len(l) == 0 {
			item.Kind, item.Summary = Exists, "match"
			return []Item{item}, nil
		}
		item.Kind = Update
		item.Summary = "differ from the spec; they are updated:"
		item.Diffs = l
		item.Requests = []string{"PUT /rest/api/3/project/" + env.P.Key}
		item.Do = updateDetails(env, changed(want, l))
		return []Item{item}, nil
	})
}

// contents compares a list of the space's own objects by ID (when the spec
// gives one) or name, and notes what only Jira has.
func contents[T any](env *Env, noun string, want, live []T, id func(T) (string, string), compare func(w, g T) []Diff,
	create, update string, do func(w T, liveID string) action) ([]Item, []string) {
	var items []Item
	used := map[int]bool{}
	for _, w := range want {
		wid, name := id(w)
		if env.IgnoreIDs {
			wid = ""
		}
		i := slices.IndexFunc(live, func(g T) bool {
			gid, gname := id(g)
			return wid != "" && gid == wid || wid == "" && gname == name
		})
		item := Item{Name: name}
		switch {
		case i < 0 && wid != "":
			item.Kind = Error
			item.Summary = fmt.Sprintf("has id %s in the spec, but the space has no %s with that ID", wid, noun)
			item.Fix = "If it was deleted in Jira, remove the id from the spec so it is created again. If the spec was " +
				"written for another site, run with --ignore-ids."
		case i < 0:
			item.Kind, item.Summary, item.Requests, item.Do = Create, "new "+noun, []string{create}, do(w, "")
		default:
			used[i] = true
			gid, gname := id(live[i])
			if gname != name {
				item.Kind = Update
				item.Summary = fmt.Sprintf("id %s is named %q in Jira; it is renamed", gid, gname)
				item.Requests, item.Do = []string{update}, do(w, gid)
				break
			}
			if d := compare(w, live[i]); len(d) > 0 {
				item.Kind, item.Summary, item.Diffs, item.Requests = Update, "differs from the spec; it is updated:", d, []string{update}
				item.Do = do(w, gid)
			} else {
				item.Kind, item.Summary = Exists, fmt.Sprintf("matches (id %s)", gid)
			}
		}
		items = append(items, item)
	}
	var notes []string
	for i, g := range live {
		if !used[i] {
			_, name := id(g)
			notes = append(notes, fmt.Sprintf("%s %q is in the space but not in the spec; it is left as it is.", capitalFirst(noun), name))
		}
	}
	return items, notes
}

func capitalFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func components(env *Env) (*Step, error) {
	return evaluateWithNotes(env, 22, "Components", "component", "components", contentsIntro, func() ([]Item, []string, error) {
		live, err := env.R.Components(env.P)
		if err != nil {
			return nil, nil, err
		}
		items, notes := contents(env, "component", env.Spec.Space.Components, live,
			func(c spec.Component) (string, string) { return c.ID, c.Name },
			func(w, g spec.Component) []Diff {
				var l diffList
				l.add(compareText("description", w.Description, g.Description))
				l.add(compareText("lead", w.Lead, g.Lead))
				l.add(compareText("assignee_type", w.AssigneeType, g.AssigneeType))
				return l
			}, "POST /rest/api/3/component", "PUT /rest/api/3/component/{id}",
			func(c spec.Component, id string) action {
				if id == "" {
					return send(http.MethodPost, "/rest/api/3/component", componentBody(env, c))
				}
				return send(http.MethodPut, "/rest/api/3/component/"+id, componentBody(env, c))
			})
		return items, notes, nil
	})
}

func versions(env *Env) (*Step, error) {
	return evaluateWithNotes(env, 23, "Versions", "version", "versions", contentsIntro, func() ([]Item, []string, error) {
		live, err := env.R.Versions(env.P)
		if err != nil {
			return nil, nil, err
		}
		items, notes := contents(env, "version", env.Spec.Space.Versions, live,
			func(v spec.Version) (string, string) { return v.ID, v.Name },
			func(w, g spec.Version) []Diff {
				var l diffList
				l.add(compareText("description", w.Description, g.Description))
				l.add(compareText("start_date", w.StartDate, g.StartDate))
				l.add(compareText("release_date", w.ReleaseDate, g.ReleaseDate))
				l.add(compareBool("released", w.Released, g.Released))
				l.add(compareBool("archived", w.Archived, g.Archived))
				return l
			}, "POST /rest/api/3/version", "PUT /rest/api/3/version/{id}",
			func(v spec.Version, id string) action {
				if id == "" {
					return send(http.MethodPost, "/rest/api/3/version", versionBody(env, v))
				}
				return send(http.MethodPut, "/rest/api/3/version/"+id, versionBody(env, v))
			})
		return items, notes, nil
	})
}

func roleMembers(env *Env) (*Step, error) {
	return evaluateWithNotes(env, 24, "Role members", "role", "roles", contentsIntro, func() ([]Item, []string, error) {
		roles, err := env.R.Roles(env.P)
		if err != nil {
			return nil, nil, err
		}
		var items []Item
		var notes []string
		for _, want := range env.Spec.Space.RoleMembers {
			var have []string
			for _, m := range roles.Members {
				if m.Role == want.Role {
					have = m.Holders
				}
			}
			add, extra := addedAndRemoved(want.Holders, have)
			for _, h := range extra {
				notes = append(notes, fmt.Sprintf("Role %q has member %s in the space, who is not in the spec; they stay.", want.Role, h))
			}
			item := Item{Name: want.Role}
			if len(add) == 0 {
				item.Kind, item.Summary = Exists, "has every member the spec lists"
			} else {
				item.Kind = Update
				item.Summary = "adds " + strings.Join(add, ", ")
				if !slices.Contains(env.AllowUpdate, "role_members") {
					item.Kind = Error
					item.Summary += ", which widens access to the space"
					item.Fix = "Changes that widen access are made only when allowed explicitly: run with --allow-update=role_members, " +
						"or add the members by hand."
				}
				item.Requests = []string{"POST /rest/api/3/project/" + env.P.Key + "/role/{id}"}
				item.Do = addRoleMembers(env, resolver{env.R, env.Spec}, want.Role, add)
			}
			items = append(items, item)
		}
		return items, notes, nil
	})
}

// changed keeps the details that differ: an update sends only what changes.
func changed(want *spec.Details, diffs []Diff) *spec.Details {
	out := &spec.Details{}
	for _, d := range diffs {
		switch d.Attribute {
		case "name":
			out.Name = want.Name
		case "description":
			out.Description = want.Description
		case "lead":
			out.Lead = want.Lead
		case "assignee_type":
			out.AssigneeType = want.AssigneeType
		case "category":
			out.Category = want.Category
		case "url":
			out.URL = want.URL
		case "avatar":
			out.Avatar = want.Avatar
		}
	}
	return out
}
