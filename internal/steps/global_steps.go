package steps

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// The steps for global objects (steps 2 to 9).
// Each reads what the site has for the spec's objects and hands both to
// globals.

func ids[T any](env *Env, list []T, object func(T) spec.GlobalObject) []string {
	if env.IgnoreIDs {
		return nil
	}
	var out []string
	for _, x := range list {
		if id := object(x).ID; id != "" {
			out = append(out, id)
		}
	}
	return out
}

func roleTypes(env *Env) (*Step, error) {
	return evaluate(env, 2, "Role types", "role type", "role types", func() ([]Item, error) {
		live, err := env.R.SiteRoleTypes()
		if err != nil {
			return nil, err
		}
		return globals(env, kind[spec.Role]{
			noun: "role type", plural: "role types",
			object: func(x spec.Role) spec.GlobalObject { return x.GlobalObject },
			compare: func(want, got spec.Role) []Diff {
				var l diffList
				l.add(compareSet("default_members", want.DefaultMembers, got.DefaultMembers))
				return l
			},
			create:  func(spec.Role) string { return "new role type" },
			request: "POST /rest/api/3/role",
			do:      createRole,
		}, env.Spec.Roles, live)
	})
}

func groups(env *Env) (*Step, error) {
	return evaluate(env, 3, "Groups", "group", "groups", func() ([]Item, error) {
		object := func(x spec.GlobalObject) spec.GlobalObject { return x }
		var names []string
		for _, g := range env.Spec.Groups {
			names = append(names, g.Name)
		}
		live, err := env.R.FindGroups(names, ids(env, env.Spec.Groups, object))
		if err != nil {
			return nil, err
		}
		return globals(env, kind[spec.GlobalObject]{
			noun: "group", plural: "groups", object: object, noDescription: true,
			compare: func(_, _ spec.GlobalObject) []Diff { return nil },
			create:  func(spec.GlobalObject) string { return "new group" },
			request: "POST /rest/api/3/group",
			do:      createGroup,
		}, env.Spec.Groups, live)
	})
}

func statuses(env *Env) (*Step, error) {
	return evaluate(env, 4, "Statuses", "status", "statuses", func() ([]Item, error) {
		object := func(x spec.Status) spec.GlobalObject { return x.GlobalObject }
		// One search per name: Jira matches parts of names without regard
		// to case, so near-misses come back too.
		var live []spec.Status
		addLive := func(found []spec.Status) {
			for _, s := range found {
				if !slices.ContainsFunc(live, func(l spec.Status) bool { return l.ID == s.ID }) {
					live = append(live, s)
				}
			}
		}
		for _, s := range env.Spec.Statuses {
			found, err := env.R.FindStatuses(strings.Join(strings.Fields(s.Name), " "))
			if err != nil {
				return nil, err
			}
			addLive(found)
		}
		byID, err := env.R.StatusesByID(ids(env, env.Spec.Statuses, object))
		if err != nil {
			return nil, err
		}
		addLive(byID)
		return globals(env, kind[spec.Status]{
			noun: "status", plural: "statuses", object: object,
			compare: func(want, got spec.Status) []Diff {
				var l diffList
				l.add(compareText("category", want.Category, got.Category))
				return l
			},
			create:  func(s spec.Status) string { return "new status · category " + s.Category.V },
			request: "POST /rest/api/3/statuses",
			do:      createStatus,
		}, env.Spec.Statuses, live)
	})
}

func resolutions(env *Env) (*Step, error) {
	return evaluate(env, 5, "Resolutions", "resolution", "resolutions", func() ([]Item, error) {
		live, err := env.R.Resolutions()
		if err != nil {
			return nil, err
		}
		return globals(env, kind[spec.Resolution]{
			noun: "resolution", plural: "resolutions",
			object: func(x spec.Resolution) spec.GlobalObject { return x.GlobalObject },
			compare: func(want, got spec.Resolution) []Diff {
				var l diffList
				l.add(compareFlag("site_default", want.SiteDefault, got.SiteDefault))
				return l
			},
			create:  func(spec.Resolution) string { return "new resolution" },
			request: "POST /rest/api/3/resolution",
			do:      createResolution,
		}, env.Spec.Resolutions, live)
	})
}

func priorities(env *Env) (*Step, error) {
	return evaluate(env, 6, "Priorities", "priority", "priorities", func() ([]Item, error) {
		live, err := env.R.SitePriorities()
		if err != nil {
			return nil, err
		}
		lower := func(o spec.Opt[string]) spec.Opt[string] { o.V = strings.ToLower(o.V); return o }
		return globals(env, kind[spec.Priority]{
			noun: "priority", plural: "priorities",
			object: func(x spec.Priority) spec.GlobalObject { return x.GlobalObject },
			compare: func(want, got spec.Priority) []Diff {
				var l diffList
				l.add(compareText("colour", lower(want.Colour), lower(got.Colour)))
				l.add(compareText("icon", want.Icon, got.Icon))
				l.add(compareFlag("site_default", want.SiteDefault, got.SiteDefault))
				return l
			},
			create:  func(p spec.Priority) string { return "new priority · colour " + p.Colour.V },
			request: "POST /rest/api/3/priority",
			do:      func(p spec.Priority) action { return createPriority(env.Site, p) },
		}, env.Spec.Priorities, live)
	})
}

func linkTypes(env *Env) (*Step, error) {
	return evaluate(env, 7, "Issue link types", "issue link type", "issue link types", func() ([]Item, error) {
		live, err := env.R.SiteLinkTypes()
		if err != nil {
			return nil, err
		}
		return globals(env, kind[spec.LinkType]{
			noun: "issue link type", plural: "issue link types",
			object: func(x spec.LinkType) spec.GlobalObject { return x.GlobalObject },
			compare: func(want, got spec.LinkType) []Diff {
				var l diffList
				l.add(compareText("inward", want.Inward, got.Inward))
				l.add(compareText("outward", want.Outward, got.Outward))
				return l
			},
			create: func(t spec.LinkType) string {
				return fmt.Sprintf("new link type · inward %q · outward %q", t.Inward.V, t.Outward.V)
			},
			request: "POST /rest/api/3/issueLinkType",
			do:      createLinkType,
		}, env.Spec.IssueLinkTypes, live)
	})
}

func issueTypes(env *Env) (*Step, error) {
	return evaluate(env, 8, "Issue types", "issue type", "issue types", func() ([]Item, error) {
		live, err := env.R.SiteIssueTypes()
		if err != nil {
			return nil, err
		}
		return globals(env, kind[spec.IssueType]{
			noun: "issue type", plural: "issue types",
			object: func(x spec.IssueType) spec.GlobalObject { return x.GlobalObject },
			compare: func(want, got spec.IssueType) []Diff {
				var l diffList
				l.add(compareText("level", want.Level, got.Level))
				l.add(compareText("avatar", want.Avatar, got.Avatar))
				l.add(compareText("icon", want.Icon, got.Icon))
				return l
			},
			create: func(t spec.IssueType) string {
				return "new issue type · level " + cmp.Or(t.Level.V, "standard")
			},
			request: "POST /rest/api/3/issuetype",
			do:      createIssueType,
		}, env.Spec.IssueTypes, live)
	})
}

func customFields(env *Env) (*Step, error) {
	return evaluate(env, 9, "Custom fields", "custom field", "custom fields", func() ([]Item, error) {
		site, err := env.R.SiteCustomFields()
		if err != nil {
			return nil, err
		}
		live := make([]spec.CustomField, len(site))
		typeKey := map[string]string{}
		for i, f := range site {
			live[i] = f.CustomField
			typeKey[f.ID] = f.TypeKey
		}
		return globals(env, kind[spec.CustomField]{
			noun: "custom field", plural: "custom fields",
			object: func(x spec.CustomField) spec.GlobalObject { return x.GlobalObject },
			compare: func(want, got spec.CustomField) []Diff {
				var l diffList
				// A type is written as its short name, or in full for app fields.
				if want.Type != "" && want.Type != got.Type && want.Type != typeKey[got.ID] {
					l = append(l, Diff{Attribute: "type", Spec: want.Type, Jira: got.Type})
				}
				l.add(compareText("searcher", want.Searcher, got.Searcher))
				l.add(compareFlag("locked", want.Locked, got.Locked))
				return l
			},
			create:  func(f spec.CustomField) string { return "new custom field · type " + f.Type },
			request: "POST /rest/api/3/field",
			do:      createCustomField,
		}, env.Spec.CustomFields, live)
	})
}
