package steps

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/reader"
	"github.com/jerryjvl/jira-space-config/internal/spec"
	"github.com/jerryjvl/jira-space-config/internal/util"
)

// The steps that tie the space's definition to Jira through schemes: field
// contexts (step 10), the issue type scheme (13), screens (14), fields (15)
// and workflows (16) of the space's issue types.

const schemeIntro = "These steps tie the space's issue types to the objects above, through schemes. A scheme the " +
	"space uses that matches is kept; one this tool made that only this space uses is changed to match. Jira's own " +
	"and shared schemes are never changed: when the space's scheme does not match, the one the spec's binding names, " +
	"or a new one named after space.managed_prefix, is used instead, and step 25 switches the space to it."

const contextIntro = "A field's contexts give its options and default value, per space and issue type. A context " +
	"used only by this space is created when missing, and its options, default and issue types are changed to match; " +
	"taking an option, an issue type or a context away needs --allow-delete=contexts, and is blocked while work items " +
	"have a value it would affect. A shared one must exist, and is never changed."

// prefixed is a generated name: "VULN Workflow Scheme", or "" without a prefix.
func prefixed(env *Env, what string) string {
	if env.Spec.Space.ManagedPrefix == "" {
		return ""
	}
	return env.Spec.Space.ManagedPrefix + " " + what
}

func foundRef(r *spec.ContainerRef, fallback string) found {
	if r == nil {
		return found{Name: fallback}
	}
	return found{ID: r.ID, Name: r.Name, Managed: r.Mode == spec.ModeManaged}
}

func spaceBinding(env *Env) *spec.SpaceBinding {
	if b := env.Spec.Space.Binding; b != nil {
		return b
	}
	return &spec.SpaceBinding{}
}

// containerStep is a step with one container item, counted by its noun.
func containerStep(env *Env, number int, title, noun string, c func() (container, error)) (*Step, error) {
	return containerStepWith(env, number, title, noun, schemeIntro, c)
}

func containerStepWith(env *Env, number int, title, noun, intro string, c func() (container, error)) (*Step, error) {
	return evaluateWith(env, number, title, noun, noun+"s", intro, func() ([]Item, error) {
		ct, err := c()
		if err != nil {
			return nil, err
		}
		return evalContainerItems(env, ct)
	})
}

func issueTypeScheme(env *Env) (*Step, error) {
	return containerStep(env, 13, "Issue type scheme", "issue type scheme", func() (container, error) {
		lines := func(names []string, def string) []line {
			return []line{{"issue types", listText(names)}, {"default", cmp.Or(def, "none")}}
		}
		var names []string
		var def string
		for _, t := range env.Spec.Space.IssueTypes {
			names = append(names, t.Name)
			if t.Default {
				def = t.Name
			}
		}
		its, err := env.issueTypes()
		if err != nil {
			return container{}, err
		}
		var curNames []string
		var curDef string
		for _, t := range its.Space {
			curNames = append(curNames, t.Name)
			if t.Default {
				curDef = t.Name
			}
		}
		current := foundRef(its.Binding, "")
		current.Lines = lines(curNames, curDef)
		return container{
			noun: "issue type scheme", title: "Issue type scheme",
			want: lines(names, def), current: current, binding: spaceBinding(env).IssueTypeScheme,
			generated: prefixed(env, "Issue Type Scheme"),
			find: func(name string) ([]found, error) {
				all, err := env.R.FindIssueTypeSchemes(name)
				if err != nil {
					return nil, err
				}
				if _, err := env.issueTypeID(""); err != nil { // reads the site's issue types for issueTypeName
					return nil, err
				}
				var out []found
				for _, s := range all {
					var n []string
					for _, id := range s.IssueTypeIDs {
						n = append(n, env.issueTypeName(id))
					}
					out = append(out, found{ID: s.ID, Name: s.Name, Managed: s.Mode == spec.ModeManaged,
						Lines: lines(n, env.issueTypeName(s.DefaultID))})
				}
				return out, nil
			},
			do:       func(name string) action { return createIssueTypeScheme(env, name) },
			update:   func() change { return updateIssueTypeScheme(env, current.ID, curNames, curDef, names, def) },
			usage:    func() ([]string, error) { return env.R.IssueTypeSchemeUsers(current.ID) },
			create:   util.Plural(len(names), "issue type", "issue types"),
			requests: []string{"POST /rest/api/3/issuetypescheme"},
			safety: func() ([]safety, error) {
				var out []safety
				for _, t := range curNames {
					if !slices.Contains(names, t) {
						out = append(out, safety{name: "issue type " + quote(t),
							jql:     fmt.Sprintf("project = %s AND issuetype = %s", env.P.ID, jqlString(t)),
							summary: "have this issue type, which the space would no longer have",
							fix:     "Move them to another issue type first, or keep the issue type in the space."})
					}
				}
				return out, nil
			},
		}, nil
	})
}

func screenSetText(s spec.ScreenSet) string {
	if s.Create == s.Edit && s.Edit == s.View {
		return s.Create
	}
	return fmt.Sprintf("create %s · edit %s · view %s", s.Create, s.Edit, s.View)
}

func screenSchemes(env *Env) (*Step, error) {
	return containerStep(env, 14, "Screens of the issue types", "issue type screen scheme", func() (container, error) {
		var want []line
		sets := map[string]bool{}
		for _, t := range env.Spec.Space.IssueTypes {
			if t.Screens != nil {
				want = append(want, line{t.Name, screenSetText(*t.Screens)})
				sets[screenSetText(*t.Screens)] = true
			}
		}
		its, err := env.issueTypes()
		if err != nil {
			return container{}, err
		}
		layout, err := env.R.ScreenScheme(env.P, its)
		if err != nil {
			return container{}, err
		}
		current := foundRef(layout.Binding, "")
		for i, t := range its.Space {
			current.Lines = append(current.Lines, line{t.Name, screenSetText(*layout.Screens[i])})
		}
		return container{
			noun: "issue type screen scheme", title: "Issue type screen scheme",
			want: want, current: current, binding: spaceBinding(env).IssueTypeScreenScheme,
			generated: prefixed(env, "Issue Type Screen Scheme"),
			find: func(name string) ([]found, error) {
				all, err := env.R.FindIssueTypeScreenSchemes(name)
				if err != nil {
					return nil, err
				}
				var out []found
				for _, s := range all {
					f := found{ID: s.ID, Name: s.Name, Managed: s.Mode == spec.ModeManaged}
					for _, w := range want {
						id, err := env.issueTypeID(w.key)
						if err != nil {
							return nil, err
						}
						set, ok := s.Screens[id]
						if !ok {
							set = s.Screens["default"]
						}
						f.Lines = append(f.Lines, line{w.key, screenSetText(set)})
					}
					out = append(out, f)
				}
				return out, nil
			},
			create: fmt.Sprintf("screens for %s, through %s (existing ones are reused where they match)",
				util.Plural(len(want), "issue type", "issue types"), util.Plural(len(sets), "screen scheme", "screen schemes")),
			requests: []string{fmt.Sprintf("POST /rest/api/3/screenscheme × %d", len(sets)), "POST /rest/api/3/issuetypescreenscheme"},
			do:       func(name string) action { return createScreenSchemes(env, resolver{env.R, env.Spec}, name) },
			update: func() change {
				return updateScreenSchemes(env, resolver{env.R, env.Spec}, current.ID, want, current.Lines)
			},
			usage: func() ([]string, error) { return env.R.ScreenSchemeUsers(current.ID) },
		}, nil
	})
}

func workflowScheme(env *Env) (*Step, error) {
	return containerStep(env, 16, "Workflows of the issue types", "workflow scheme", func() (container, error) {
		var want []line
		for _, t := range env.Spec.Space.IssueTypes {
			if t.Workflow.Has() {
				want = append(want, line{t.Name, t.Workflow.V})
			}
		}
		its, err := env.issueTypes()
		if err != nil {
			return container{}, err
		}
		assignment, err := env.R.WorkflowScheme(env.P, its)
		if err != nil {
			return container{}, err
		}
		current := foundRef(assignment.Binding, "")
		for i, t := range its.Space {
			current.Lines = append(current.Lines, line{t.Name, assignment.Workflows[i]})
		}
		return container{
			noun: "workflow scheme", title: "Workflow scheme",
			want: want, current: current, binding: spaceBinding(env).WorkflowScheme,
			generated: prefixed(env, "Workflow Scheme"),
			find: func(name string) ([]found, error) {
				all, err := env.R.FindWorkflowSchemes(name)
				if err != nil {
					return nil, err
				}
				var out []found
				for _, s := range all {
					f := found{ID: s.ID, Name: s.Name, Managed: s.Mode == spec.ModeManaged}
					for _, w := range want {
						id, err := env.issueTypeID(w.key)
						if err != nil {
							return nil, err
						}
						wf, ok := s.Mappings[id]
						if !ok {
							wf = s.Default
						}
						f.Lines = append(f.Lines, line{w.key, wf})
					}
					out = append(out, f)
				}
				return out, nil
			},
			do: func(name string) action { return createWorkflowScheme(env, name) },
			update: func() change {
				return updateWorkflowScheme(env, resolver{env.R, env.Spec}, current.ID, want, current.Lines)
			},
			usage:    func() ([]string, error) { return env.R.WorkflowSchemeUsers(current.ID) },
			create:   "workflows for " + util.Plural(len(want), "issue type", "issue types"),
			requests: []string{"POST /rest/api/3/workflowscheme"},
			safety:   func() ([]safety, error) { return workflowSafety(env, want, current.Lines) },
		}, nil
	})
}

// fieldLines are a field list's lines; fields Jira will not take out (fixed)
// are left out unless the spec lists them, as they are Jira's, not a difference.
func fieldLines(fields []spec.SpaceField, fixed ...string) []line {
	var out []line
	for _, f := range fields {
		if slices.Contains(fixed, f.Name) {
			continue
		}
		out = append(out, line{f.Name, spec.Text(f)})
	}
	return out
}

func spaceFields(env *Env) (*Step, error) {
	return evaluateWith(env, 15, "Fields in the space", "field scheme", "field schemes", schemeIntro, func() ([]Item, error) {
		if problem, err := fieldBackend(env); err != nil || problem != nil {
			if problem != nil {
				return []Item{*problem}, nil
			}
			return nil, err
		}
		ct, err := fieldContainer(env)
		if err != nil {
			return nil, err
		}
		return evalContainerItems(env, ct)
	})
}

func fieldContainer(env *Env) (container, error) {
	its, err := env.issueTypes()
	if err != nil {
		return container{}, err
	}
	layout, err := env.R.Fields(env.P, its)
	if err != nil {
		return container{}, err
	}
	want := fieldLines(spaceOrder(its, inherited(env, env.Spec.Space.Fields.V)))
	b := spaceBinding(env)
	if layout.Backend == spec.FieldBackendSchemes {
		current := foundRef(layout.FieldScheme, "")
		current.Lines = fieldLines(layout.Fields, unlisted(env, layout.Fixed)...)
		return container{
			noun: "field scheme", title: "Field scheme", want: want, current: current, binding: b.FieldScheme,
			generated: prefixed(env, "Field Scheme"), complete: true,
			find: func(name string) ([]found, error) {
				return foundLayouts(env)(env.R.FindFieldSchemes(its, name))
			},
			create: util.Plural(len(want), "field", "fields"),
			requests: []string{"POST /rest/api/3/config/fieldschemes", "PUT /rest/api/3/config/fieldschemes/fields",
				"PUT /rest/api/3/config/fieldschemes/fields/parameters", "DELETE /rest/api/3/config/fieldschemes/fields (fields Jira added)"},
			do: func(name string) action { return createFieldScheme(env, resolver{env.R, env.Spec}, name) },
			update: func() change {
				return updateFieldScheme(env, resolver{env.R, env.Spec}, current.ID, spaceOrder(its, inherited(env, env.Spec.Space.Fields.V)),
					layout.Fields, unlisted(env, layout.Fixed))
			},
			usage:  func() ([]string, error) { return env.R.FieldSchemeUsers(current.ID) },
			safety: func() ([]safety, error) { return fieldSafety(env, layout.Fields) },
		}, nil
	}
	current := foundRef(layout.FieldConfigurationScheme, "Jira's system default field configuration scheme")
	current.Lines = fieldLines(layout.Fields)
	return container{
		noun: "field configuration scheme", title: "Field configuration scheme", want: want, current: current,
		binding: b.FieldConfigurationScheme, generated: prefixed(env, "Field Configuration Scheme"), complete: true,
		find: func(name string) ([]found, error) {
			return foundLayouts(env)(env.R.FindFieldConfigurationSchemes(its, name))
		},
		create: util.Plural(len(want), "field", "fields") + ", with a field configuration per kind of issue type",
		requests: []string{"POST /rest/api/3/fieldconfiguration", "PUT /rest/api/3/fieldconfiguration/{id}/fields",
			"POST /rest/api/3/fieldconfigurationscheme", "PUT /rest/api/3/fieldconfigurationscheme/{id}/mapping"},
		safety: func() ([]safety, error) { return fieldSafety(env, layout.Fields) },
	}, nil
}

// foundLayouts turns the field schemes (or field configuration schemes) a
// lookup found into candidates for the container step, with their fields as
// lines to compare.
func foundLayouts(env *Env) func([]reader.FieldSchemeContent, error) ([]found, error) {
	return func(all []reader.FieldSchemeContent, err error) ([]found, error) {
		if err != nil {
			return nil, err
		}
		var out []found
		for _, s := range all {
			out = append(out, found{ID: s.ID, Name: s.Name, Managed: s.Mode == spec.ModeManaged,
				Lines: fieldLines(s.Layout.Fields, unlisted(env, s.Layout.Fixed)...)})
		}
		return out, nil
	}
}

// fieldBackend is part of step 15: a spec written for one field backend
// cannot be applied to a site with the other.
func fieldBackend(env *Env) (*Item, error) {
	want := ""
	if b := env.Spec.Binding; b != nil {
		want = b.FieldBackend
	}
	if want == "" {
		return nil, nil
	}
	its, err := env.issueTypes()
	if err != nil {
		return nil, err
	}
	layout, err := env.R.Fields(env.P, its)
	if err != nil {
		return nil, err
	}
	if layout.Backend == want {
		return nil, nil
	}
	return &Item{Kind: Error, Name: "Field backend",
		Summary: fmt.Sprintf("the spec was written for a site with %s; this site uses %s", backendName(want), backendName(layout.Backend)),
		Fix:     "Remove binding.field_backend and the field configuration or field scheme bindings, so the fields are applied with this site's backend."}, nil
}

func backendName(b string) string {
	if b == spec.FieldBackendSchemes {
		return "field schemes"
	}
	return "field configurations"
}

// contextKey names the issue types a context covers, as a set: Jira lists
// them in its own order.
func contextKey(c spec.FieldContext) string {
	if c.IssueTypes == nil {
		return "all issue types"
	}
	types := slices.Clone(c.IssueTypes)
	slices.Sort(types)
	return strings.Join(types, ", ")
}

// contextLines are a field's contexts as lines to compare, keyed by the
// issue types each covers. The issue types are sorted in the text too, not
// only in the key, so the same set in Jira's order is no difference.
func contextLines(set spec.FieldContextSet) []line {
	var out []line
	for _, c := range set.Contexts {
		if c.IssueTypes != nil {
			c.IssueTypes = slices.Clone(c.IssueTypes)
			slices.Sort(c.IssueTypes) // a set: Jira lists them in its own order
		}
		out = append(out, line{contextKey(c), spec.Text(c)})
	}
	return out
}

func fieldContexts(env *Env) (*Step, error) {
	return evaluateWith(env, 10, "Field contexts", "field context", "field contexts", contextIntro, func() ([]Item, error) {
		its, err := env.issueTypes()
		if err != nil {
			return nil, err
		}
		site, err := env.R.SiteCustomFields()
		if err != nil {
			return nil, err
		}
		var items []Item
		for _, set := range env.Spec.Space.FieldContexts {
			item := Item{Name: set.Field}
			var cf *spec.CustomField
			for i := range site {
				if site[i].Name == set.Field {
					cf = &site[i].CustomField
				}
			}
			switch {
			case cf == nil && env.Planned("custom field", set.Field):
				item.Kind = Create
				item.Summary = fmt.Sprintf("%s for the new field", util.Plural(len(set.Contexts), "new context", "new contexts"))
				item.Requests = contextRequests
				item.Do = createContexts(env, resolver{env.R, env.Spec}, set.Field, set.Contexts)
			case cf == nil:
				item.Kind = Error
				item.Summary = "no custom field on the site has this name"
				item.Fix = "Check the name, or declare the field in custom_fields with mode: ensure so it is created first."
			default:
				// Jira's contexts and the spec's are made comparable first. Jira
				// is asked about the issue types the spec gives the space too,
				// so a context an earlier, stopped run made for them is found;
				// a spec context for every issue type reads as one for all, as
				// Jira reports it; and a shared context that applies to none of
				// the space's issue types is not counted against the spec.
				all := withSpecIssueTypes(env, its)
				set = coveringAll(set, all)
				live, err := env.R.FieldContexts(env.P, all, []spec.CustomField{*cf})
				if err != nil {
					return nil, err
				}
				var got []line
				if len(live) > 0 {
					live[0] = withoutUnusedShared(env, set, live[0])
					got = contextLines(live[0])
				}
				item = contextItem(set, got)
				if item.Kind == Differs && len(live) > 0 {
					if ch, ok := contextChange(env, *cf, set, live[0]); ok {
						changed, err := changeItems(env, item, ch, "the field's set of contexts exists", "field contexts of "+quote(set.Field))
						if err != nil {
							return nil, err
						}
						if changed[0].Kind == Update {
							changed[0].Summary = fmt.Sprintf("the contexts, used by this space alone, are changed to match (%s):", ch.class)
						}
						items = append(items, changed...)
						continue
					}
				}
				if item.Kind == Create {
					var missing []spec.FieldContext
					for _, c := range set.Contexts {
						if !slices.ContainsFunc(got, func(l line) bool { return l.key == contextKey(c) }) {
							missing = append(missing, c)
						}
					}
					item.Do = createContexts(env, resolver{env.R, env.Spec}, set.Field, missing)
				}
			}
			items = append(items, item)
		}
		return items, nil
	})
}

// contextItem compares a field's contexts: missing ones used only by this
// space would be created; anything else that differs must be fixed first.
func contextItem(set spec.FieldContextSet, got []line) Item {
	item := Item{Name: set.Field}
	diffs := compareLines(contextLines(set), got, true)
	if len(diffs) == 0 {
		item.Kind = Exists
		item.Summary = "matches (" + util.Plural(len(set.Contexts), "context", "contexts") + ")"
		return item
	}
	isShared := func(c spec.FieldContext) bool { return c.Binding != nil && c.Binding.Scope == spec.ScopeShared }
	// Creatable: every difference is a context the spec has, Jira lacks, and
	// that is not shared (a shared one must exist).
	creatable, shared := true, false
	for _, d := range diffs {
		i := slices.IndexFunc(set.Contexts, func(c spec.FieldContext) bool { return contextKey(c) == d.Attribute })
		if i >= 0 && isShared(set.Contexts[i]) {
			shared = true
		}
		if i < 0 || d.Jira != "none" || isShared(set.Contexts[i]) {
			creatable = false
		}
	}
	if creatable {
		item.Kind = Create
		item.Summary = util.Plural(len(diffs), "new context", "new contexts") + " for this space"
		item.Diffs = diffs
		item.Requests = contextRequests
		return item
	}
	item.Kind = Differs
	item.Summary = "the contexts do not match the spec:"
	item.Diffs = diffs
	item.Fix = "Change the spec to match, or the contexts in Jira by hand."
	if shared {
		item.Fix = "A shared context is used by other spaces too, and is never changed. Change the spec to match, or " +
			"give the space a context of its own (scope space)."
	}
	return item
}

// issueTypeName names a site issue type by ID; the site's issue types
// must have been read (issueTypeID reads them).
func (e *Env) issueTypeName(id string) string {
	for name, i := range e.siteTypes {
		if i == id {
			return name
		}
	}
	if slices.Contains([]string{"", "default"}, id) {
		return "none"
	}
	return "id:" + id
}

// workflowStatuses returns the statuses of a workflow: the spec's when it
// declares the workflow, Jira's otherwise.
func workflowStatuses(env *Env, name string) ([]string, error) {
	var out []string
	for _, w := range env.Spec.Workflows {
		if w.Name == name {
			for _, s := range w.Statuses {
				out = append(out, s.Name)
			}
			return out, nil
		}
	}
	defs, err := env.R.Workflows([]string{name})
	if err != nil {
		return nil, err
	}
	for _, s := range defs.Workflows[0].Statuses {
		out = append(out, s.Name)
	}
	return out, nil
}

// workflowSafety: when an issue type's workflow changes, its work items in
// a status the new workflow lacks would be stranded.
func workflowSafety(env *Env, want, current []line) ([]safety, error) {
	var out []safety
	for _, w := range want {
		i := slices.IndexFunc(current, func(c line) bool { return c.key == w.key })
		if i < 0 || current[i].value == w.value {
			continue
		}
		before, err := workflowStatuses(env, current[i].value)
		if err != nil {
			return nil, err
		}
		after, err := workflowStatuses(env, w.value)
		if err != nil {
			return nil, err
		}
		for _, s := range before {
			if !slices.Contains(after, s) {
				out = append(out, safety{name: fmt.Sprintf("status %q (issue type %q)", s, w.key),
					jql:     fmt.Sprintf("project = %s AND issuetype = %s AND status = %s", env.P.ID, jqlString(w.key), jqlString(s)),
					summary: fmt.Sprintf("are in this status, which workflow %q lacks", w.value),
					fix:     "Move them to another status first, or keep the status in the new workflow."})
			}
		}
	}
	return out, nil
}

// fieldSafety: a field leaving the space would hide its values; a field
// becoming required would block unresolved work items that have it empty.
func fieldSafety(env *Env, current []spec.SpaceField) ([]safety, error) {
	ids, err := env.R.FieldNames()
	if err != nil {
		return nil, err
	}
	jqlField := func(name string) string {
		if id := ids[name]; len(id) > 0 {
			if n, ok := strings.CutPrefix(id[0], "customfield_"); ok {
				return "cf[" + n + "]"
			}
			return id[0]
		}
		return jqlString(name)
	}
	want := env.Spec.Space.Fields.V
	var out []safety
	for _, c := range current {
		if !slices.ContainsFunc(want, func(w spec.SpaceField) bool { return w.Name == c.Name }) {
			out = append(out, safety{name: "field " + quote(c.Name),
				jql:     fmt.Sprintf("project = %s AND %s is not EMPTY", env.P.ID, jqlField(c.Name)),
				summary: "have a value in this field, which would no longer be in the space (the values would be hidden)",
				fix:     "Clear the field on those work items first, or keep the field in the space."})
		}
	}
	for _, w := range want {
		i := slices.IndexFunc(current, func(c spec.SpaceField) bool { return c.Name == w.Name })
		if w.Required.V && (i < 0 || !current[i].Required.V) {
			out = append(out, safety{name: "field " + quote(w.Name),
				jql:     fmt.Sprintf("project = %s AND resolution = Unresolved AND %s is EMPTY", env.P.ID, jqlField(w.Name)),
				summary: "are unresolved with this field empty; once it is required they could not be edited or moved on without filling it",
				fix:     "Fill the field on those work items first, or leave it optional."})
		}
	}
	return out, nil
}

// contextRequests are the writes that create a field's contexts.
var contextRequests = []string{"POST /rest/api/3/field/{fieldId}/context",
	"POST /rest/api/3/field/{fieldId}/context/{contextId}/option", "PUT /rest/api/3/field/{fieldId}/context/defaultValue"}

// inherited leaves out of the spec's fields the descriptions that are the
// field's own: a field scheme shows those but does not hold them.
func inherited(env *Env, fields []spec.SpaceField) []spec.SpaceField {
	site, err := env.R.SiteCustomFields()
	if err != nil {
		return fields
	}
	own := map[string]string{}
	for _, f := range site {
		if f.Description.Set {
			own[f.Name] = f.Description.V
		}
	}
	out := slices.Clone(fields)
	for i := range out {
		d, ok := own[out[i].Name]
		if !ok || d == "" {
			continue
		}
		if out[i].Description.V == d {
			out[i].Description = spec.Opt[string]{}
		}
		var per []spec.IssueTypeFieldSettings
		for _, b := range out[i].ByIssueType {
			if b.Description.V == d {
				b.Description = spec.Opt[string]{}
			}
			if b.FieldSettings != (spec.FieldSettings{}) {
				per = append(per, b)
			}
		}
		out[i].ByIssueType = per
	}
	return out
}

// unlisted are the fixed fields the spec does not list.
func unlisted(env *Env, fixed []string) []string {
	var out []string
	for _, n := range fixed {
		if !slices.ContainsFunc(env.Spec.Space.Fields.V, func(f spec.SpaceField) bool { return f.Name == n }) {
			out = append(out, n)
		}
	}
	return out
}

// spaceOrder puts each field's issue types in the space's order, as the
// reader does: the order of a restriction means nothing.
func spaceOrder(its *reader.IssueTypes, fields []spec.SpaceField) []spec.SpaceField {
	out := slices.Clone(fields)
	for i := range out {
		if out[i].IssueTypes == nil {
			continue
		}
		var ordered []string
		for _, t := range its.Space {
			if slices.Contains(out[i].IssueTypes, t.Name) {
				ordered = append(ordered, t.Name)
			}
		}
		for _, t := range out[i].IssueTypes { // issue types the space does not have yet keep their place at the end
			if !slices.Contains(ordered, t) {
				ordered = append(ordered, t)
			}
		}
		out[i].IssueTypes = ordered
	}
	return out
}

// withoutUnusedShared leaves out of a field's contexts in Jira the shared
// ones the spec does not mention, when the spec's own contexts cover every
// issue type the space will have: the shared context (a field's global
// default, say) then applies to nothing in the space. Without this, a field
// that already exists on the site could never be given a context of its own.
func withoutUnusedShared(env *Env, set spec.FieldContextSet, live spec.FieldContextSet) spec.FieldContextSet {
	types := env.Spec.Space.IssueTypes
	covered := len(types) > 0
	for _, t := range types {
		covered = covered && slices.ContainsFunc(set.Contexts, func(c spec.FieldContext) bool {
			return c.IssueTypes == nil || slices.Contains(c.IssueTypes, t.Name)
		})
	}
	if !covered {
		return live
	}
	var kept []spec.FieldContext
	for _, c := range live.Contexts {
		shared := c.Binding != nil && c.Binding.Scope == spec.ScopeShared
		mentioned := slices.ContainsFunc(set.Contexts, func(w spec.FieldContext) bool { return contextKey(w) == contextKey(c) })
		if !shared || mentioned {
			kept = append(kept, c)
		}
	}
	live.Contexts = kept
	return live
}

// coveringAll reads a context limited to every issue type the space has as a
// context for all of them, which is how Jira reports it.
func coveringAll(set spec.FieldContextSet, its *reader.IssueTypes) spec.FieldContextSet {
	contexts := slices.Clone(set.Contexts)
	for i, c := range contexts {
		covered := 0
		for j := range its.Len() {
			if name, ok := its.Name(its.ID(j)); ok && slices.Contains(c.IssueTypes, name) {
				covered++
			}
		}
		if c.IssueTypes != nil && covered == its.Len() {
			contexts[i].IssueTypes = nil
		}
	}
	set.Contexts = contexts
	return set
}

// withSpecIssueTypes adds to the space's issue types the spec's that exist on
// the site but that the space does not have yet (its issue type scheme is
// switched at step 25), so a context made for them by an earlier, stopped run
// is found rather than made again.
func withSpecIssueTypes(env *Env, its *reader.IssueTypes) *reader.IssueTypes {
	var others []spec.SpaceIssueType
	var ids []string
	for _, t := range env.Spec.Space.IssueTypes {
		if _, there := its.IDOf(t.Name); there {
			continue
		}
		if id, err := env.issueTypeID(t.Name); err == nil && id != "" {
			others = append(others, spec.SpaceIssueType{Name: t.Name})
			ids = append(ids, id)
		}
	}
	if len(others) == 0 {
		return its
	}
	return its.With(others, ids)
}
