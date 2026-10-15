package steps

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/spec"
	"github.com/jerryjvl/jira-space-config/internal/util"
)

// The steps for who may do what in the space, and its priorities:
// permissions (step 17), notifications (18), issue security (19) and the
// priority scheme (20). Each is a scheme, evaluated like the others, with
// the work item safety counts on a switch.

const accessIntro = "Who may do what in the space, who is told of changes, who may see which work items, and " +
	"which priorities it offers are schemes too, decided the same way: the space's scheme is kept when it matches; " +
	"otherwise the one the spec's binding names, or a new one named after space.managed_prefix, is used, and step 25 " +
	"switches the space to it. A switch that would leave work items without their priority or security level is blocked."

func grantLines(grants []spec.Grant) []line {
	var out []line
	for _, g := range grants {
		h := slices.Clone(g.Holders)
		slices.Sort(h)
		out = append(out, line{g.Permission, listText(h)})
	}
	return out
}

// switching reports whether an item makes the space use another scheme.
func switching(it Item) bool { return it.Kind == Create || it.Kind == Update }

func permissions(env *Env) (*Step, error) {
	return evaluateWith(env, 17, "Permissions", "permission scheme", "permission schemes", accessIntro, func() ([]Item, error) {
		want := env.Spec.Space.Permissions.V
		current, err := env.R.PermissionScheme(env.P)
		if err != nil {
			return nil, err
		}
		cur := foundRef(current.Binding, "")
		cur.Lines = grantLines(current.Grants)
		item, err := evalContainer(env, container{
			noun: "permission scheme", title: "Permission scheme", want: grantLines(want.Grants), current: cur,
			binding: want.Binding, generated: prefixed(env, "Permission Scheme"), complete: true,
			find: func(name string) ([]found, error) {
				all, err := env.R.FindPermissionSchemes(name)
				if err != nil {
					return nil, err
				}
				var out []found
				for _, p := range all {
					f := foundRef(p.Binding, "")
					f.Lines = grantLines(p.Grants)
					out = append(out, f)
				}
				return out, nil
			},
			do: func(name string) action { return createPermissionScheme(resolver{env.R, env.Spec}, want, name) },
			update: func() change {
				return updatePermissionScheme(env, resolver{env.R, env.Spec}, cur.ID, want.Grants, current.Grants)
			},
			usage: func() ([]string, error) {
				if !slices.Contains(env.CheckUsage, "permission_scheme") || !env.AdministerJira {
					return nil, nil // not checked: the update says why it is not made
				}
				return env.R.PermissionSchemeUsers(cur.ID)
			},
			create: util.Plural(len(want.Grants), "permission", "permissions"),
			requests: []string{"POST /rest/api/3/permissionscheme",
				"DELETE /rest/api/3/permissionscheme/{schemeId}/permission/{permissionId} (grants Jira adds)"},
		})
		if err != nil || !switching(item) {
			return []Item{item}, err
		}
		switch {
		case env.FreePlan:
			item = freePlan(item, "permission scheme", "permissions")
		case !slices.ContainsFunc(want.Grants, func(g spec.Grant) bool { return g.Permission == "ADMINISTER_PROJECTS" && len(g.Holders) > 0 }):
			item.Kind = Blocked
			item.Summary += "; but it grants Administer projects to nobody"
			item.Fix = "Grant ADMINISTER_PROJECTS to someone (role:Administrators, for example), or nobody could administer the space."
		default:
			if broad := broadGrants(want.Grants); len(broad) > 0 && !slices.Contains(env.AllowUpdate, "permissions") {
				item.Kind = Error
				item.Summary += fmt.Sprintf("; but it grants %s to every user", someOf(broad))
				item.Fix = "Granting to every user widens access the most, so it is made only when allowed explicitly: run with " +
					"--allow-update=permissions, or remove the anyone and application_role holders."
			}
		}
		if item.Kind.Blocking() {
			env.cancelSwitch("permission scheme") // the switch is not made
		}
		return []Item{item}, nil
	})
}

// broadGrants lists the permissions a scheme grants to every user.
func broadGrants(grants []spec.Grant) []string {
	var out []string
	for _, g := range grants {
		if slices.Contains(g.Holders, "anyone") || slices.Contains(g.Holders, "application_role") {
			out = append(out, g.Permission)
		}
	}
	return out
}

func freePlan(item Item, noun, what string) Item {
	item.Kind = Error
	item.Summary = fmt.Sprintf("the space would need another %s, but this site is on the Free plan", noun)
	item.Fix = fmt.Sprintf("On the Free plan Jira's %s are fixed. Keep the spec matching them, or upgrade the plan.", what)
	item.Requests = nil
	return item
}

func notifications(env *Env) (*Step, error) {
	return evaluateWith(env, 18, "Notifications", "notification scheme", "notification schemes", accessIntro, func() ([]Item, error) {
		section := env.Spec.Space.Notifications
		want := section.V
		current, err := env.R.NotificationScheme(env.P)
		if err != nil {
			return nil, err
		}
		cur := found{Name: "none"}
		if current != nil {
			cur = foundRef(current.Binding, "")
			cur.Lines = grantLines(current.Events)
		}
		if section.Null {
			item := Item{Name: "Notification scheme", Kind: Exists, Summary: "the space has none, as the spec says"}
			if current != nil {
				item.Kind = Update
				item.Summary = fmt.Sprintf("the space stops using %s; step 25 removes it", describe(cur))
				item.Later = "step 25"
				env.switches = append(env.switches, switchPlan{step: env.step, noun: "notification scheme", from: cur.Name})
			}
			return []Item{item}, nil
		}
		item, err := evalContainer(env, container{
			noun: "notification scheme", title: "Notification scheme", want: grantLines(want.Events), current: cur,
			binding: want.Binding, generated: prefixed(env, "Notification Scheme"), complete: true,
			find: func(name string) ([]found, error) {
				all, err := env.R.FindNotificationSchemes(name)
				if err != nil {
					return nil, err
				}
				var out []found
				for _, n := range all {
					f := foundRef(n.Binding, "")
					f.Lines = grantLines(n.Events)
					out = append(out, f)
				}
				return out, nil
			},
			do: func(name string) action { return createNotificationScheme(env, resolver{env.R, env.Spec}, want, name) },
			update: func() change {
				return updateNotificationScheme(env, resolver{env.R, env.Spec}, cur.ID, want.Events, current.Events)
			},
			usage:    func() ([]string, error) { return env.R.NotificationSchemeUsers(cur.ID) },
			create:   util.Plural(len(want.Events), "event", "events"),
			requests: []string{"POST /rest/api/3/notificationscheme"},
		})
		return []Item{item}, err
	})
}

func levelLines(levels []spec.SecurityLevel) []line {
	var out []line
	for _, l := range levels {
		h := slices.Clone(l.Holders)
		slices.Sort(h)
		v := listText(h)
		if l.Default {
			v += " · the default"
		}
		if l.Description.Set && l.Description.V != "" {
			v += " · " + l.Description.V
		}
		out = append(out, line{"level " + quote(l.Name), v})
	}
	return out
}

func issueSecurity(env *Env) (*Step, error) {
	return evaluateWith(env, 19, "Issue security", "issue security scheme", "issue security schemes", accessIntro, func() ([]Item, error) {
		want := env.Spec.Space.IssueSecurity
		current, err := env.R.IssueSecurity(env.P)
		if err != nil {
			return nil, err
		}
		var curLevels []spec.SecurityLevel
		cur := found{Name: "none"}
		if current != nil {
			cur = foundRef(current.Binding, "")
			curLevels = current.Levels
		}
		cur.Lines = levelLines(curLevels)
		var item Item
		if want.Null {
			item = Item{Name: "Issue security scheme"}
			if current == nil {
				item.Kind, item.Summary = Exists, "the space has none, as the spec says"
			} else {
				item.Kind = Update
				item.Summary = fmt.Sprintf("the space stops using %s; step 25 removes it", describe(cur))
				item.Later = "step 25"
				env.switches = append(env.switches, switchPlan{step: env.step, noun: "issue security scheme", from: cur.Name})
			}
		} else {
			item, err = evalContainer(env, container{
				noun: "issue security scheme", title: "Issue security scheme", want: levelLines(want.V.Levels), current: cur,
				binding: want.V.Binding, generated: prefixed(env, "Issue Security Scheme"), complete: true,
				find: func(name string) ([]found, error) {
					all, err := env.R.FindIssueSecuritySchemes(name)
					if err != nil {
						return nil, err
					}
					var out []found
					for _, is := range all {
						f := foundRef(is.Binding, "")
						f.Lines = levelLines(is.Levels)
						out = append(out, f)
					}
					return out, nil
				},
				do: func(name string) action { return createSecurityScheme(resolver{env.R, env.Spec}, want.V, name) },
				update: func() change {
					return updateSecurityScheme(env, resolver{env.R, env.Spec}, cur.ID, want.V.Levels, curLevels)
				},
				usage:    func() ([]string, error) { return env.R.SecuritySchemeUsers(cur.ID) },
				create:   util.Plural(len(want.V.Levels), "level", "levels"),
				requests: []string{"POST /rest/api/3/issuesecurityschemes"},
			})
			if err != nil {
				return nil, err
			}
		}
		if !switching(item) {
			return []Item{item}, nil
		}
		if env.FreePlan && !want.Null {
			env.cancelSwitch("issue security scheme")
			return []Item{freePlan(item, "issue security scheme", "issue security settings")}, nil
		}
		// Work items keep their level only if the new scheme has it.
		var lost []string
		for _, l := range curLevels {
			if want.Null || !slices.ContainsFunc(want.V.Levels, func(w spec.SecurityLevel) bool { return w.Name == l.Name }) {
				lost = append(lost, l.Name)
			}
		}
		blocked, err := countBlocked(env, "level", lost, "this security level", "the new scheme lacks",
			"Change their security level first, or keep the level in the scheme.")
		return append([]Item{item}, blocked...), err
	})
}

func prioritySchemeStep(env *Env) (*Step, error) {
	return evaluateWith(env, 20, "Priorities in the space", "priority scheme", "priority schemes", accessIntro, func() ([]Item, error) {
		want := env.Spec.Space.Priorities.V
		lines := func(p spec.SpacePriorities) []line {
			return []line{{"priorities", listText(p.Priorities)}, {"default", cmp.Or(p.Default.V, "none")}}
		}
		current, err := env.R.PriorityScheme(env.P)
		if err != nil {
			return nil, err
		}
		cur := foundRef(current.Space.Binding, "")
		cur.Lines = lines(current.Space)
		item, err := evalContainer(env, container{
			noun: "priority scheme", title: "Priority scheme", want: lines(want), current: cur,
			binding: want.Binding, generated: prefixed(env, "Priority Scheme"),
			find: func(name string) ([]found, error) {
				all, err := env.R.FindPrioritySchemes(name)
				if err != nil {
					return nil, err
				}
				var out []found
				for _, p := range all {
					f := foundRef(p.Space.Binding, "")
					f.Lines = lines(p.Space)
					out = append(out, f)
				}
				return out, nil
			},
			do: func(name string) action { return createPriorityScheme(resolver{env.R, env.Spec}, want, name) },
			update: func() change {
				return updatePriorityScheme(env, resolver{env.R, env.Spec}, cur.ID, want, current.Space)
			},
			usage:    func() ([]string, error) { return env.R.PrioritySchemeUsers(cur.ID) },
			create:   util.Plural(len(want.Priorities), "priority", "priorities"),
			requests: []string{"POST /rest/api/3/priorityscheme"},
		})
		if err != nil || !switching(item) {
			return []Item{item}, err
		}
		var lost []string
		for _, p := range current.Space.Priorities {
			if !slices.Contains(want.Priorities, p) {
				lost = append(lost, p)
			}
		}
		blocked, err := countBlocked(env, "priority", lost, "this priority", "the new scheme lacks",
			"Change their priority first, or keep it in the scheme.")
		return append([]Item{item}, blocked...), err
	})
}

// countBlocked counts, for each value a change takes away, the work items
// of the space that have it; each value some work item has is BLOCKED.
func countBlocked(env *Env, field string, values []string, what, why, remedy string) ([]Item, error) {
	var checks []safety
	for _, v := range values {
		checks = append(checks, safety{name: fmt.Sprintf("%s %q", field, v),
			jql:     fmt.Sprintf("project = %s AND %s = %s", env.P.ID, field, jqlString(v)),
			summary: fmt.Sprintf("have %s, which %s", what, why), fix: remedy})
	}
	return runSafety(env, checks)
}

// runSafety runs safety checks: each that finds work items is BLOCKED. A
// space without work items needs no counts.
func runSafety(env *Env, checks []safety) ([]Item, error) {
	if env.WorkItems == 0 {
		return nil, nil
	}
	var out []Item
	for _, c := range checks {
		n, err := env.R.CountWorkItems(c.jql)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			out = append(out, Item{Kind: Blocked, Name: c.name,
				Summary: util.Plural(n, "work item", "work items") + " " + c.summary, Fix: c.fix})
		}
	}
	return out, nil
}

// jqlString quotes a value for JQL.
func jqlString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// someOf names up to three items of a list, and how many more there are.
func someOf(l []string) string {
	if len(l) <= 3 {
		return strings.Join(l, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(l[:3], ", "), len(l)-3)
}
