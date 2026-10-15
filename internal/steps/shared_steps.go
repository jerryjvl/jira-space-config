package steps

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/reader"
	"github.com/jerryjvl/jira-space-config/internal/spec"
	"github.com/jerryjvl/jira-space-config/internal/util"
)

// The steps for shareable objects that are defined on their own: screens
// (step 11) and workflows (step 12). They can be used by several spaces, so
// a managed one is created for this space when missing, and changed while
// only this space uses it; a reused one must exist, and is never changed.

// sharedIntro introduces the steps for shareable objects; it is shown once,
// before the first of them.
const sharedIntro = "Screens and workflows can be used by several spaces. A managed one (mode managed, the default) is " +
	"created for this space when missing, and changed to match the spec only while this space alone uses it; " +
	"a change that removes something needs --allow-delete. A reused one must exist, and is never changed."

// candidates are what a lookup found for the spec's objects: the IDs worth
// reading in full (exact names and IDs), and stubs for the rest, which only
// show near-misses.
func candidates(wanted []spec.GlobalObject, ignoreIDs bool, found []reader.Found) (full []string, stubs []reader.Found) {
	for _, f := range found {
		exact := slices.ContainsFunc(wanted, func(w spec.GlobalObject) bool {
			return w.Name == f.Name || !ignoreIDs && w.ID != "" && w.ID == f.ID
		})
		switch {
		case exact && !slices.Contains(full, f.ID):
			full = append(full, f.ID)
		case !exact && !slices.ContainsFunc(stubs, func(s reader.Found) bool { return s.ID == f.ID }):
			stubs = append(stubs, f)
		}
	}
	return full, stubs
}

func screenObject(s spec.Screen) spec.GlobalObject {
	return spec.GlobalObject{Meta: s.Meta, Name: s.Name, Mode: s.Mode, Description: s.Description}
}

func screens(env *Env) (*Step, error) {
	return evaluateWith(env, 11, "Screens", "screen", "screens", sharedIntro, func() ([]Item, error) {
		var wanted []spec.GlobalObject
		var found []reader.Found
		for _, s := range env.Spec.Screens {
			wanted = append(wanted, screenObject(s))
			f, err := env.R.FindScreens(strings.Join(strings.Fields(s.Name), " "))
			if err != nil {
				return nil, err
			}
			found = append(found, f...)
		}
		byID, err := env.R.ScreensByID(ids(env, env.Spec.Screens, screenObject))
		if err != nil {
			return nil, err
		}
		full, stubs := candidates(wanted, env.IgnoreIDs, append(found, byID...))
		live, err := env.R.Screens(full)
		if err != nil {
			return nil, err
		}
		for _, s := range stubs {
			live = append(live, spec.Screen{Meta: spec.Meta{ID: s.ID}, Name: s.Name})
		}
		fields, err := env.R.FieldNames()
		if err != nil {
			return nil, err
		}
		check := func(s spec.Screen) (string, string) {
			var unknown []string
			for _, t := range s.Tabs {
				for _, f := range t.Fields {
					if len(fields[f]) == 0 && !env.Planned("custom field", f) {
						unknown = append(unknown, f)
					}
				}
			}
			if len(unknown) == 0 {
				return "", ""
			}
			return fmt.Sprintf("no field on the site is named %s", quoteList(unknown)),
				"Check the field names, or declare the fields in custom_fields with mode: ensure so they are created first."
		}
		return globals(env, kind[spec.Screen]{
			noun: "screen", plural: "screens", shareable: true, object: screenObject,
			do:      func(s spec.Screen) action { return createScreen(resolver{env.R, env.Spec}, s) },
			compare: compareScreens,
			create: func(s spec.Screen) string {
				n := 0
				for _, t := range s.Tabs {
					n += len(t.Fields)
				}
				return fmt.Sprintf("new screen · %s, %s", util.Plural(len(s.Tabs), "tab", "tabs"), util.Plural(n, "field", "fields"))
			},
			requests: func(s spec.Screen) []string {
				out := []string{"POST /rest/api/3/screens", "PUT /rest/api/3/screens/{screenId}/tabs/{tabId} (names the first tab)"}
				n := 0
				for i, t := range s.Tabs {
					if i > 0 {
						out = append(out, "POST /rest/api/3/screens/{screenId}/tabs")
					}
					n += len(t.Fields)
				}
				if n > 0 {
					out = append(out, fmt.Sprintf("POST /rest/api/3/screens/{screenId}/tabs/{tabId}/fields × %d", n))
				}
				return out
			},
			check: func(s spec.Screen) (string, string) {
				if problem, fix := check(s); problem != "" {
					return "cannot be created: " + problem, fix
				}
				return "", ""
			},
			update: func(w, g spec.Screen) change {
				if problem, fix := check(w); problem != "" {
					return change{problem: problem, fix: fix}
				}
				return updateScreen(resolver{env.R, env.Spec}, w, g)
			},
			usage: func(g spec.Screen) ([]string, error) { return env.R.ScreenUsers(g.ID) },
		}, env.Spec.Screens, live)
	})
}

// compareScreens compares the tabs, if the spec lists them: their names in
// order, and each tab's fields in order.
func compareScreens(want, got spec.Screen) []Diff {
	if want.Tabs == nil {
		return nil
	}
	tabNames := func(s spec.Screen) []string {
		var out []string
		for _, t := range s.Tabs {
			out = append(out, t.Name)
		}
		return out
	}
	var out []Diff
	if !slices.Equal(tabNames(want), tabNames(got)) {
		out = append(out, Diff{Attribute: "tabs", Spec: listText(tabNames(want)), Jira: listText(tabNames(got))})
	}
	// Each tab the spec lists, by name: a new one shows Jira as none.
	for _, t := range want.Tabs {
		var have []string
		for _, g := range got.Tabs {
			if g.Name == t.Name {
				have = g.Fields
			}
		}
		if !slices.Equal(t.Fields, have) {
			out = append(out, Diff{Attribute: fmt.Sprintf("tab %q fields", t.Name), Spec: listText(t.Fields), Jira: listText(have)})
		}
	}
	return out
}

func workflowObject(w spec.Workflow) spec.GlobalObject {
	return spec.GlobalObject{Meta: w.Meta, Name: w.Name, Mode: w.Mode, Description: w.Description}
}

func workflows(env *Env) (*Step, error) {
	return evaluateWith(env, 12, "Workflows", "workflow", "workflows", sharedIntro, func() ([]Item, error) {
		var wanted []spec.GlobalObject
		var found []reader.Found
		for _, w := range env.Spec.Workflows {
			wanted = append(wanted, workflowObject(w))
			f, err := env.R.FindWorkflows(strings.Join(strings.Fields(w.Name), " "))
			if err != nil {
				return nil, err
			}
			found = append(found, f...)
		}
		byID, err := env.R.WorkflowsByID(ids(env, env.Spec.Workflows, workflowObject))
		if err != nil {
			return nil, err
		}
		full, stubs := candidates(wanted, env.IgnoreIDs, append(found, byID...))
		var live []spec.Workflow
		if len(full) > 0 {
			var names []string
			for _, f := range append(found, byID...) {
				if slices.Contains(full, f.ID) && !slices.Contains(names, f.Name) {
					names = append(names, f.Name)
				}
			}
			defs, err := env.R.Workflows(names)
			if err != nil {
				return nil, err
			}
			live = defs.Workflows
		}
		for _, s := range stubs {
			live = append(live, spec.Workflow{Meta: spec.Meta{ID: s.ID}, Name: s.Name})
		}
		known, err := workflowReferences(env)
		if err != nil {
			return nil, err
		}
		// missingRefs says which statuses and screens w uses that are not on
		// the site and will not be created.
		missingRefs := func(w spec.Workflow) (string, string) {
			var unknown []string
			for _, s := range w.Statuses {
				if !known["status:"+s.Name] {
					unknown = append(unknown, "status "+quote(s.Name))
				}
			}
			for _, t := range w.Transitions {
				if t.Screen.Has() && !known["screen:"+t.Screen.V] {
					unknown = util.AppendNew(unknown, "screen "+quote(t.Screen.V))
				}
			}
			if len(unknown) == 0 {
				return "", ""
			}
			return "the site has no " + strings.Join(unknown, ", "),
				"Check the names, or declare them in the spec (statuses with mode: ensure, screens with mode: managed) so they are created first."
		}
		return globals(env, kind[spec.Workflow]{
			noun: "workflow", plural: "workflows", shareable: true, object: workflowObject,
			do:      func(w spec.Workflow) action { return createWorkflow(env, resolver{env.R, env.Spec}, w) },
			compare: compareWorkflows,
			create: func(w spec.Workflow) string {
				return fmt.Sprintf("new workflow · %s, %s", util.Plural(len(w.Statuses), "status", "statuses"),
					util.Plural(len(w.Transitions), "transition", "transitions"))
			},
			requests: func(spec.Workflow) []string {
				return []string{"POST /rest/api/3/workflows/create/validation (read-only check)", "POST /rest/api/3/workflows/create"}
			},
			check: func(w spec.Workflow) (string, string) {
				if slices.ContainsFunc(w.Statuses, func(s spec.WorkflowStatus) bool { return s.Deprecated }) {
					return "cannot be created with a deprecated status",
						"Jira's API cannot create a workflow with a deprecated status. Create it without, then deprecate the status in Jira's workflow editor."
				}
				if problem, fix := missingRefs(w); problem != "" {
					return "cannot be created: " + problem, fix
				}
				return "", ""
			},
			update: func(w, g spec.Workflow) change {
				if problem, fix := missingRefs(w); problem != "" {
					return change{problem: problem, fix: fix}
				}
				return updateWorkflow(env, resolver{env.R, env.Spec}, w, g)
			},
			usage: func(g spec.Workflow) ([]string, error) { return env.R.WorkflowUsers(g.ID) },
		}, env.Spec.Workflows, live)
	})
}

// workflowReferences finds which of the statuses and screens the spec's
// workflows use exist on the site or would be created by earlier steps.
func workflowReferences(env *Env) (map[string]bool, error) {
	known := map[string]bool{}
	for _, w := range env.Spec.Workflows {
		for _, s := range w.Statuses {
			key := "status:" + s.Name
			if _, done := known[key]; done {
				continue
			}
			known[key] = env.Planned("status", s.Name)
			if !known[key] {
				found, err := env.R.FindStatuses(s.Name)
				if err != nil {
					return nil, err
				}
				known[key] = slices.ContainsFunc(found, func(x spec.Status) bool { return x.Name == s.Name })
			}
		}
		for _, t := range w.Transitions {
			if !t.Screen.Has() {
				continue
			}
			key := "screen:" + t.Screen.V
			if _, done := known[key]; done {
				continue
			}
			known[key] = env.Planned("screen", t.Screen.V)
			if !known[key] {
				found, err := env.R.FindScreens(t.Screen.V)
				if err != nil {
					return nil, err
				}
				known[key] = slices.ContainsFunc(found, func(x reader.Found) bool { return x.Name == t.Screen.V })
			}
		}
	}
	return known, nil
}

// compareWorkflows compares the statuses (as a set, with the details the
// spec states for each) and, if the spec lists transitions, every
// transition by name.
func compareWorkflows(want, got spec.Workflow) []Diff {
	var out []Diff
	names := func(w spec.Workflow) []string {
		var l []string
		for _, s := range w.Statuses {
			l = append(l, s.Name)
		}
		return l
	}
	if d, ok := compareSet("statuses", names(want), names(got)); ok {
		out = append(out, d)
	}
	for _, s := range want.Statuses {
		if s.Layout == nil && len(s.Properties) == 0 && !s.Deprecated {
			continue // only the name is stated
		}
		for _, g := range got.Statuses {
			if g.Name == s.Name && spec.Text(s) != spec.Text(g) {
				out = append(out, Diff{Attribute: fmt.Sprintf("status %q", s.Name), Spec: spec.Text(s), Jira: spec.Text(g)})
			}
		}
	}
	if want.Transitions == nil {
		return out
	}
	// Transitions are matched by name; names may repeat, so the n-th of a
	// name is compared with Jira's n-th of that name.
	byName := func(w spec.Workflow) map[string][]spec.Transition {
		m := map[string][]spec.Transition{}
		for _, t := range expandLoops(w.Transitions) {
			m[t.Name] = append(m[t.Name], t)
		}
		return m
	}
	wantBy, gotBy := byName(want), byName(got)
	var order []string
	for _, t := range append(expandLoops(want.Transitions), expandLoops(got.Transitions)...) {
		order = util.AppendNew(order, t.Name)
	}
	for _, name := range order {
		w, g := wantBy[name], gotBy[name]
		for i := range max(len(w), len(g)) {
			attr := fmt.Sprintf("transition %q", name)
			switch {
			case i >= len(g):
				out = append(out, Diff{Attribute: attr, Spec: spec.Text(w[i]), Jira: "none"})
			case i >= len(w):
				out = append(out, Diff{Attribute: attr, Spec: "none", Jira: spec.Text(g[i])})
			case spec.Text(w[i]) != spec.Text(g[i]):
				out = append(out, Diff{Attribute: attr, Spec: spec.Text(w[i]), Jira: spec.Text(g[i])})
			}
		}
	}
	return out
}

func listText(l []string) string {
	if len(l) == 0 {
		return "none"
	}
	return strings.Join(l, ", ")
}

func quote(s string) string { return fmt.Sprintf("%q", s) }

func quoteList(l []string) string {
	q := make([]string, len(l))
	for i, s := range l {
		q[i] = quote(s)
	}
	return strings.Join(q, ", ")
}
