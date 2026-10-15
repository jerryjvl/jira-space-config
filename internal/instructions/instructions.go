// Package instructions writes a spec as the steps to set its space up by
// hand in Jira, in Markdown: one section per kind of object, in the order
// apply works (so each section needs only the ones before it), with a line
// saying where in Jira it is done and a table of the values to enter.
//
// It reads only the spec. What the spec reuses is assumed to be on the site
// and is named, not described; what it ensures is to be created if missing;
// what it manages is to be created. Jira's own objects that the spec does not
// declare (standard statuses, system fields) are taken as they are.
package instructions

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// Options say where the spec came from.
type Options struct {
	Source      string // the spec's file name
	ToolVersion string
}

// Write returns the instructions for a valid spec.
func Write(s *spec.Spec, o Options) []byte {
	d := &doc{s: s, handOnly: map[spec.Pos]bool{}}
	for _, diag := range spec.Validate(s) {
		if diag.APIOnly {
			d.handOnly[diag.Pos] = true
		}
	}
	d.header(o)
	d.space()
	d.roles()
	d.groups()
	d.statuses()
	d.resolutions()
	d.priorities()
	d.linkTypes()
	d.issueTypes()
	d.customFields()
	d.contexts()
	d.screens()
	d.workflows()
	d.issueTypeScheme()
	d.screenSchemes()
	d.spaceFields()
	d.workflowScheme()
	d.permissions()
	d.notifications()
	d.security()
	d.spacePriorities()
	d.components()
	d.versions()
	d.roleMembers()
	d.switches()
	d.filters()
	d.boards()
	d.automation()
	d.layouts()
	d.notes()
	return []byte(strings.TrimRight(d.b.String(), "\n") + "\n")
}

type doc struct {
	s     *spec.Spec
	b     strings.Builder
	n     int        // sections written
	assoc [][]string // scheme switches gathered from steps 13 to 20
	// handOnly are the objects only a person can create (validate's API-only
	// errors), by their place in the file.
	handOnly map[spec.Pos]bool
}

func (d *doc) printf(format string, args ...any) { fmt.Fprintf(&d.b, format, args...) }

// section starts a numbered section with where it is done.
func (d *doc) section(title, where string) {
	d.n++
	d.printf("## %d. %s\n\n%s.\n\n", d.n, title, where)
}

// table writes a Markdown table, leaving out the columns that are empty in
// every row; a row shorter than the head is padded.
func (d *doc) table(head []string, rows [][]string) {
	var keep []int
	for i := range head {
		if slices.ContainsFunc(rows, func(r []string) bool { return i < len(r) && strings.TrimSpace(r[i]) != "" }) {
			keep = append(keep, i)
		}
	}
	line := func(r []string) string {
		cells := make([]string, len(keep))
		for j, i := range keep {
			if i < len(r) {
				cells[j] = cell(r[i])
			}
		}
		return "| " + strings.Join(cells, " | ") + " |\n"
	}
	d.printf("%s|%s\n", line(head), strings.Repeat(" --- |", len(keep)))
	for _, r := range rows {
		d.printf("%s", line(r))
	}
	d.printf("\n")
}

func (d *doc) para(format string, args ...any) { d.printf(format+"\n\n", args...) }

// cell makes text safe in a table cell.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.ReplaceAll(strings.TrimSpace(s), "\n", "<br>")
}

func text(o spec.Opt[string]) string {
	if o.Has() {
		return o.V
	}
	return ""
}

func code(s string) string {
	if s == "" {
		return ""
	}
	return "`" + strings.ReplaceAll(s, "`", "'") + "`"
}

// existing names, in one line, what a section relies on but does not create.
func (d *doc) existing(names []string) {
	if len(names) > 0 {
		d.para("Already on the site, used as they are: %s.", strings.Join(names, ", "))
	}
}

// noted is an object's name and the spec's notes on it.
type noted struct {
	name  string
	notes []string
}

// objectNotes lists the spec's notes on the objects of a section.
func (d *doc) objectNotes(objects []noted) {
	wrote := false
	for _, o := range objects {
		for _, line := range o.notes {
			d.printf("- *%s:* %s\n", o.name, strings.Join(strings.Fields(line), " "))
			wrote = true
		}
	}
	if wrote {
		d.printf("\n")
	}
}

// metaNotes lists the notes of a section's objects.
func metaNotes[T any](d *doc, items []T, of func(T) (string, []string)) {
	var objects []noted
	for _, it := range items {
		n, ns := of(it)
		objects = append(objects, noted{n, ns})
	}
	d.objectNotes(objects)
}

func (d *doc) header(o Options) {
	name := d.s.Space.Key
	if d.s.Space.Details != nil && text(d.s.Space.Details.Name) != "" {
		name = text(d.s.Space.Details.Name) + " (" + d.s.Space.Key + ")"
	}
	d.printf("# Setting up %s by hand\n\n", name)
	if desc := text(d.s.Metadata.Description); desc != "" {
		d.para("%s", strings.Join(strings.Fields(desc), " "))
	}
	from := "the spec"
	if o.Source != "" {
		from = code(o.Source)
	}
	d.para("Written from %s by jira-space-config %s. Work through the sections in order: each needs only "+
		"the ones before it. Only what has to be added or changed is listed; Jira's own objects (standard "+
		"statuses, system fields, the default schemes) are used as they are. \"Create if missing\" means: if "+
		"one of that name is already there, check it has these values instead. Menu paths were checked "+
		"against Atlassian's documentation in %s and may have moved since.", from, o.ToolVersion, checked)
	d.para("You need the Administer Jira global permission, and Atlassian organisation admin for groups.")
}

func (d *doc) space() {
	sp := d.s.Space
	d.section("Space", pathSpace)
	rows := [][]string{{"Key", sp.Key}}
	det := sp.Details
	if det == nil {
		det = &spec.Details{}
	}
	// A new space needs a name and a type; the spec may leave them to Jira's
	// current values.
	for _, kv := range [][2]string{{"Name", text(det.Name)}, {"Type", text(det.Type)}} {
		if kv[1] == "" {
			rows = append(rows, []string{kv[0], "*not in the spec: choose one*"})
		}
	}
	{
		for _, kv := range [][2]string{{"Name", text(det.Name)}, {"Type", spaceTypes[text(det.Type)]},
			{"Description", text(det.Description)}, {"Lead", d.holder(text(det.Lead))},
			{"Default assignee", assignees[text(det.AssigneeType)]}, {"Category", text(det.Category)},
			{"URL", text(det.URL)}, {"Avatar", text(det.Avatar)}} {
			if kv[1] != "" {
				rows = append(rows, []string{kv[0], kv[1]})
			}
		}
	}
	d.table([]string{"Setting", "Value"}, rows)
	d.para("If the space already exists, set these in %s instead.", pathDetails)
}

var spaceTypes = map[string]string{"": "", "software": "Software", "business": "Business (work management)",
	"service_desk": "Service management", "product_discovery": "Product discovery"}

var assignees = map[string]string{"": "", "unassigned": "Unassigned", "space_lead": "Space lead"}

// globals writes a section of site-wide objects: those reused are named,
// the rest are tabled.
func globals[T any](d *doc, items []T, title, where string, obj func(T) spec.GlobalObject, head []string, row func(T) []string) {
	var reused []string
	var objects []noted
	var rows [][]string
	for _, it := range items {
		g := obj(it)
		if g.Mode == spec.ModeReuse {
			reused = append(reused, g.Name)
			continue
		}
		action := "Create"
		if g.Mode == spec.ModeEnsure {
			action = "Create if missing"
		}
		if d.handOnly[g.Pos] {
			action += " (by hand only: apply cannot)"
		}
		rows = append(rows, append([]string{action}, row(it)...))
		objects = append(objects, noted{g.Name, g.Notes})
	}
	if len(rows) == 0 && len(reused) == 0 {
		return
	}
	if len(rows) == 0 {
		d.existingOnly(title, reused)
		return
	}
	d.section(title, where)
	d.table(append([]string{""}, head...), rows)
	d.objectNotes(objects)
	d.existing(reused)
}

// existingOnly records a section with nothing to do, in one line.
func (d *doc) existingOnly(title string, names []string) {
	d.n++
	d.para("## %d. %s\n\nNothing to create. Already on the site, used as they are: %s.", d.n, title, strings.Join(names, ", "))
}

func (d *doc) roles() {
	globals(d, d.s.Roles, "Project roles", pathRoles, func(r spec.Role) spec.GlobalObject { return r.GlobalObject },
		[]string{"Role", "Description", "Default members"}, func(r spec.Role) []string {
			return []string{r.Name, text(r.Description), d.holders(r.DefaultMembers)}
		})
}

func (d *doc) groups() {
	globals(d, d.s.Groups, "Groups", pathGroups, func(g spec.GlobalObject) spec.GlobalObject { return g },
		[]string{"Group"}, func(g spec.GlobalObject) []string { return []string{g.Name} })
}

var categories = map[string]string{"todo": "To do", "in_progress": "In progress", "done": "Done"}

func (d *doc) statuses() {
	globals(d, d.s.Statuses, "Statuses", pathStatuses, func(s spec.Status) spec.GlobalObject { return s.GlobalObject },
		[]string{"Status", "Category", "Description"}, func(s spec.Status) []string {
			return []string{s.Name, categories[text(s.Category)], text(s.Description)}
		})
}

func (d *doc) resolutions() {
	globals(d, d.s.Resolutions, "Resolutions", pathResolutions, func(r spec.Resolution) spec.GlobalObject { return r.GlobalObject },
		[]string{"Resolution", "Description"}, func(r spec.Resolution) []string { return []string{r.Name, text(r.Description)} })
}

func (d *doc) priorities() {
	globals(d, d.s.Priorities, "Priorities", pathPriorities, func(p spec.Priority) spec.GlobalObject { return p.GlobalObject },
		[]string{"Priority", "Colour", "Icon", "Description"}, func(p spec.Priority) []string {
			return []string{p.Name, text(p.Colour), text(p.Icon), text(p.Description)}
		})
}

func (d *doc) linkTypes() {
	globals(d, d.s.IssueLinkTypes, "Work item link types", pathLinkTypes, func(t spec.LinkType) spec.GlobalObject { return t.GlobalObject },
		[]string{"Name", "Outward description", "Inward description"}, func(t spec.LinkType) []string {
			return []string{t.Name, text(t.Outward), text(t.Inward)}
		})
}

var levels = map[string]string{"": "Standard", "standard": "Standard", "subtask": "Sub-task", "epic": "Epic"}

func (d *doc) issueTypes() {
	globals(d, d.s.IssueTypes, "Work item types", pathIssueTypes, func(t spec.IssueType) spec.GlobalObject { return t.GlobalObject },
		[]string{"Work item type", "Level", "Description"}, func(t spec.IssueType) []string {
			level, ok := levels[text(t.Level)]
			if !ok {
				level = "Hierarchy level " + text(t.Level)
			}
			return []string{t.Name, level, text(t.Description)}
		})
}

// fieldTypes are the field types as Jira's Create new field panel shows them.
var fieldTypes = map[string]string{
	"cascading": "Select List (cascading)", "checkboxes": "Checkboxes", "date": "Date Picker",
	"datetime": "Date Time Picker", "group": "Group Picker (single group)", "labels": "Labels",
	"multigroup": "Group Picker (multiple groups)", "multiselect": "Select List (multiple choices)",
	"multiuser": "User Picker (multiple users)", "multiversion": "Version Picker (multiple versions)",
	"number": "Number Field", "paragraph": "Paragraph (supports rich text)", "project": "Project Picker (single project)",
	"radio": "Radio Buttons", "readonly": "Text Field (read only)", "select": "Select List (single choice)",
	"text": "Short text (plain text only)", "url": "URL Field", "user": "User Picker (single user)",
	"version": "Version Picker (single version)",
}

func (d *doc) customFields() {
	globals(d, d.s.CustomFields, "Custom fields", pathFields, func(f spec.CustomField) spec.GlobalObject { return f.GlobalObject },
		[]string{"Field", "Type", "Description"}, func(f spec.CustomField) []string {
			t, ok := fieldTypes[f.Type]
			if !ok {
				t = "app field " + code(f.Type)
				if key, own := strings.CutPrefix(f.Type, "com.atlassian.jira.plugin.system.customfieldtypes:"); own {
					t = "Jira field type " + code(key)
				}
			}
			return []string{f.Name, t, text(f.Description)}
		})
	if slices.ContainsFunc(d.s.CustomFields, func(f spec.CustomField) bool { return f.Mode != spec.ModeReuse }) {
		d.para("Options and work types are set per context in the next section, not here. A new field is not added to any field scheme by itself.")
	}
}

// contextName is the name apply gives a new context.
func (d *doc) contextName(field string, c spec.FieldContext) string {
	if c.Binding != nil && c.Binding.Name != "" {
		return c.Binding.Name
	}
	if p := d.s.Space.ManagedPrefix; p != "" {
		return p + " " + field
	}
	return field + " (" + d.s.Space.Key + ")"
}

func (d *doc) contexts() {
	var rows [][]string
	var shared []string
	for _, set := range d.s.Space.FieldContexts {
		for _, c := range set.Contexts {
			if c.Binding != nil && (c.Binding.Scope == spec.ScopeShared || c.Binding.Mode == spec.ModeReuse) {
				shared = append(shared, fmt.Sprintf("%s (%s)", c.Binding.Name, set.Field))
				continue
			}
			types := "All"
			if c.IssueTypes != nil {
				types = strings.Join(c.IssueTypes, ", ")
			}
			rows = append(rows, []string{set.Field, d.contextName(set.Field, c), types, options(c.Options), value(c.Default)})
		}
	}
	if len(rows) == 0 && len(shared) == 0 {
		return
	}
	d.section("Field contexts", pathContexts+", then **Add new context**")
	if len(rows) > 0 {
		d.para("Each context applies to space %s only, for the work types listed.", d.s.Space.Key)
		d.table([]string{"Field", "Context", "Work types", "Options, in order", "Default"}, rows)
	}
	d.existing(shared)
}

func options(opts []spec.Option) string {
	var out []string
	for _, o := range opts {
		v := o.Value
		if o.Disabled {
			v += " (disabled)"
		}
		if len(o.Children) > 0 {
			v += " › " + options(o.Children)
		}
		out = append(out, v)
	}
	return strings.Join(out, ", ")
}

func value(v spec.Opt[spec.Value]) string {
	switch {
	case !v.Set:
		return ""
	case v.Null:
		return "None"
	case v.V.IsList:
		return strings.Join(v.V.List, ", ")
	}
	return v.V.Str
}

// toCreate separates a section's objects to create from those reused, by
// name. With nothing to create, it writes the section's one line about what
// is already there, and returns none to create.
func toCreate[T any](d *doc, title string, items []T, mode func(T) spec.Mode, name func(T) string) (create []T, reused []string) {
	for _, it := range items {
		if mode(it) == spec.ModeReuse {
			reused = append(reused, name(it))
		} else {
			create = append(create, it)
		}
	}
	if len(create) == 0 && len(reused) > 0 {
		d.existingOnly(title, reused)
	}
	return create, reused
}

func (d *doc) screens() {
	create, reused := toCreate(d, "Screens", d.s.Screens, func(s spec.Screen) spec.Mode { return s.Mode }, func(s spec.Screen) string { return s.Name })
	if len(create) == 0 {
		return
	}
	d.section("Screens", pathScreens+"; add each tab and its fields in the order given")
	var rows [][]string
	for _, s := range create {
		for i, t := range s.Tabs {
			name, desc := "", ""
			if i == 0 {
				name, desc = s.Name, text(s.Description)
			}
			rows = append(rows, []string{name, desc, t.Name, strings.Join(t.Fields, ", ")})
		}
	}
	d.table([]string{"Screen", "Description", "Tab", "Fields, in order"}, rows)
	metaNotes(d, create, func(s spec.Screen) (string, []string) { return s.Name, s.Notes })
	d.existing(reused)
}

func (d *doc) workflows() {
	create, reused := toCreate(d, "Workflows", d.s.Workflows, func(w spec.Workflow) spec.Mode { return w.Mode }, func(w spec.Workflow) string { return w.Name })
	if len(create) == 0 {
		return
	}
	d.section("Workflows", pathWorkflows+"; add the statuses, then each transition with its rules")
	for _, w := range create {
		d.printf("### %s\n\n", w.Name)
		if desc := text(w.Description); desc != "" {
			d.para("Description: %s", desc)
		}
		var names []string
		for _, st := range w.Statuses {
			names = append(names, st.Name)
		}
		d.para("Statuses: %s.", strings.Join(names, ", "))
		var rows [][]string
		for _, t := range w.Transitions {
			from := strings.Join(t.From, ", ")
			to := t.To
			switch t.Type {
			case spec.TransitionInitial:
				from = "(create)"
			case spec.TransitionGlobal:
				from = "Any status"
			}
			if t.Looped {
				to = "(back to the same status)"
			}
			rows = append(rows, []string{t.Name, from, to, text(t.Screen), d.transitionRules(t)})
		}
		d.table([]string{"Transition", "From", "To", "Screen", "Rules"}, rows)
		d.objectNotes([]noted{{w.Name, w.Notes}})
	}
	d.existing(reused)
}

// transitionRules describes a transition's rules in words, one kind a line.
func (d *doc) transitionRules(t spec.Transition) string {
	var lines []string
	if desc := text(t.Description); desc != "" {
		lines = append(lines, "Description: "+desc)
	}
	if t.Conditions != nil {
		lines = append(lines, "**Only if:** "+condition(*t.Conditions))
	}
	for _, list := range []struct {
		label string
		rules []spec.Rule
	}{{"Validate", t.Validators}, {"Then", t.PostFunctions}, {"Triggers", t.Triggers}} {
		if len(list.rules) == 0 {
			continue
		}
		var parts []string
		for _, r := range list.rules {
			parts = append(parts, rule(r))
		}
		lines = append(lines, "**"+list.label+":** "+strings.Join(parts, "; "))
	}
	if ev := text(t.Event); ev != "" {
		lines = append(lines, "Fires event: "+ev)
	}
	var props []string
	for _, p := range t.Properties {
		v := p.Value
		if p.IsList {
			v = strings.Join(p.List, ", ")
		}
		props = append(props, code(p.Key)+" = "+v)
	}
	if len(props) > 0 {
		lines = append(lines, "Properties: "+strings.Join(props, "; "))
	}
	return strings.Join(lines, "\n")
}

func condition(c spec.Condition) string {
	if c.Rule != nil {
		return rule(*c.Rule)
	}
	var parts []string
	for _, item := range c.Items {
		p := condition(item)
		// A part that joins its own items reads apart from this group's.
		if len(c.Items) > 1 && (item.Rule == nil && len(item.Items) > 1 || item.Rule != nil && strings.Contains(p, " or ")) {
			p = "(" + p + ")"
		}
		parts = append(parts, p)
	}
	join := " and "
	if c.Op == "any" {
		join = " or "
	}
	return strings.Join(parts, join)
}

// params maps a rule's parameters by key, as text.
func params(r spec.Rule) map[string]string {
	m := map[string]string{}
	for _, p := range r.Params {
		switch p.Kind {
		case spec.KindList:
			m[p.Key] = strings.Join(p.List, ", ")
		case spec.KindMap:
			var kv []string
			for _, e := range p.Map {
				kv = append(kv, e.Key+"="+e.Value)
			}
			m[p.Key] = strings.Join(kv, ", ")
		default:
			m[p.Key] = p.Str
		}
	}
	return m
}

// rule describes one workflow rule as Jira's rule editor names it.
func rule(r spec.Rule) string {
	p := params(r)
	var s string
	switch r.Type {
	case "restrict_to":
		var who []string
		for _, k := range []struct{ key, label string }{{"roles", "role"}, {"groups", "group"}, {"permissions", "permission"},
			{"group_fields", "a group in"}, {"user_fields", "the user in"}, {"deny_user_fields", "not the user in"}, {"accounts", "user"}} {
			if p[k.key] != "" {
				who = append(who, k.label+" "+p[k.key])
			}
		}
		s = "Restrict to " + strings.Join(who, " or ")
	case "field_value":
		s = fmt.Sprintf("%s %s %s (compared as %s)", p["field"], p["comparator"], p["values"], p["comparison"])
	case "previous_status":
		s = "Previously in " + p["status"]
		if p["not"] == "true" {
			s = "Never previously in " + p["status"]
		}
	case "parent_child_blocking":
		s = "Blocked unless related work items are in " + p["statuses"]
	case "separation_of_duties":
		s = "Not the person who made the transition from " + cmp.Or(p["from_status"], "any status") + " to " + cmp.Or(p["to_status"], "any status")
	case "block_all":
		s = "Nobody (" + p["mode"] + ")"
	case "block_in_progress_approval":
		s = "Blocked while an approval is in progress"
	case "permission":
		s = "Has permission " + p["permission"]
	case "fields_required":
		s = "Required: " + p["fields"]
	case "field_changed":
		s = p["field"] + " has changed"
		if p["exempt_groups"] != "" {
			s += " (except for " + p["exempt_groups"] + ")"
		}
	case "field_single_value":
		s = p["field"] + " has a single value"
	case "field_matches":
		s = p["field"] + " matches " + code(p["regexp"])
	case "date_compare":
		s = p["field"] + " " + p["comparator"] + " " + p["other_field"]
	case "date_window":
		s = p["field"] + " is within " + p["days"] + " days of " + p["other_field"]
	case "forms_attached":
		s = "A form is attached"
	case "forms_submitted":
		s = "Every form is submitted"
	case "assign":
		s = "Assign to " + strings.ReplaceAll(p["to"], "_", " ")
		if p["user"] != "" {
			s += " " + p["user"]
		}
	case "copy_field":
		s = "Copy " + p["from"] + " to " + p["to"]
		if p["source"] == "parent" {
			s += " from the parent"
		}
	case "clear_field":
		s = "Clear " + p["field"]
	case "set_field":
		s = "Set " + p["field"] + " to " + p["value"]
		if p["mode"] == "append" {
			s = "Add " + p["value"] + " to " + p["field"]
		}
	case "trigger_webhook":
		s = "Trigger webhook " + p["webhook"]
	case "trigger_agent":
		s = "Ask agent " + p["agent"]
		if p["prompt"] != "" {
			s += ": " + p["prompt"]
		}
	case spec.RuleOpaque:
		s = "App rule " + code(p["key"])
		if p["parameters"] != "" {
			s += " (" + p["parameters"] + ")"
		}
		return s
	default: // a typed rule added to the registry but not described here yet
		s = r.Type
	}
	if r.Type == "fields_required" || r.Type == "field_changed" || r.Type == "field_matches" {
		if msg := p["error_message"]; msg != "" {
			s += ", message \"" + msg + "\""
		}
	}
	if p["extra"] != "" {
		s += " (also " + p["extra"] + ")"
	}
	return s
}

// scheme is the scheme a space-level section creates or uses: the binding's,
// or one named after the managed prefix.
type scheme struct {
	name   string
	reused bool // must exist; only associated
	own    bool // no name: the space's current scheme is changed
}

func (d *doc) scheme(ref *spec.ContainerRef, kind string) scheme {
	switch {
	case ref != nil && ref.Name != "":
		return scheme{name: ref.Name, reused: ref.Mode == spec.ModeReuse}
	case d.s.Space.ManagedPrefix != "":
		return scheme{name: d.s.Space.ManagedPrefix + " " + kind}
	}
	return scheme{own: true}
}

// schemeIntro says what to do with a scheme, and records its switch; it
// returns false when the scheme is only to be associated.
func (d *doc) schemeIntro(sc scheme, kind, page string) bool {
	switch {
	case sc.reused:
		d.para("Use the existing %s %s; it is not changed.", strings.ToLower(kind), bold(sc.name))
	case sc.own:
		d.para("Change the %s the space uses now to match (the spec names none, and has no managed_prefix).", strings.ToLower(kind))
		return true
	default:
		d.para("Create the %s %s:", strings.ToLower(kind), bold(sc.name))
	}
	d.assoc = append(d.assoc, []string{kind, sc.name, page})
	return !sc.reused
}

// bold is Markdown bold text.
func bold(s string) string { return "**" + s + "**" }

func (d *doc) spaceBinding() *spec.SpaceBinding {
	if b := d.s.Space.Binding; b != nil {
		return b
	}
	return &spec.SpaceBinding{}
}

func (d *doc) issueTypeScheme() {
	its := d.s.Space.IssueTypes
	if len(its) == 0 {
		return
	}
	d.section("Work item type scheme", pathITScheme)
	if d.schemeIntro(d.scheme(d.spaceBinding().IssueTypeScheme, "Issue Type Scheme"), "Work item type scheme", "Work items › Types") {
		var rows [][]string
		def := ""
		for _, t := range its {
			rows = append(rows, []string{t.Name})
			if t.Default {
				def = t.Name
			}
		}
		d.table([]string{"Work types, in order"}, rows)
		if def != "" {
			d.para("Default work type: %s.", def)
		}
	}
}

func screenSet(s spec.ScreenSet) string { return s.Create + "\x00" + s.Edit + "\x00" + s.View }

func (d *doc) screenSchemes() {
	type set struct {
		screens spec.ScreenSet
		name    string
		types   []string
	}
	var sets []*set
	for _, t := range d.s.Space.IssueTypes {
		if t.Screens == nil {
			continue
		}
		i := slices.IndexFunc(sets, func(s *set) bool { return screenSet(s.screens) == screenSet(*t.Screens) })
		if i < 0 {
			n := d.s.Space.ManagedPrefix + " " + t.Name + " Screen Scheme"
			if t.Binding != nil && t.Binding.ScreenScheme != nil && t.Binding.ScreenScheme.Name != "" {
				n = t.Binding.ScreenScheme.Name
			}
			sets = append(sets, &set{screens: *t.Screens, name: n})
			i = len(sets) - 1
		}
		sets[i].types = append(sets[i].types, t.Name)
	}
	if len(sets) == 0 {
		return
	}
	d.section("Screens of the work types", pathScreenScheme)
	var rows [][]string
	for _, s := range sets {
		rows = append(rows, []string{s.name, s.screens.Create, s.screens.Edit, s.screens.View})
	}
	d.para("Create the screen schemes:")
	d.table([]string{"Screen scheme", "Create", "Edit", "View"}, rows)
	// The most used screen scheme is the default, as apply makes it.
	def := sets[0]
	for _, s := range sets {
		if len(s.types) > len(def.types) {
			def = s
		}
	}
	if d.schemeIntro(d.scheme(d.spaceBinding().IssueTypeScreenScheme, "Issue Type Screen Scheme"), "Work item type screen scheme", "Work items › Screens") {
		rows = [][]string{{"Default", def.name}}
		for _, s := range sets {
			if s != def {
				for _, t := range s.types {
					rows = append(rows, []string{t, s.name})
				}
			}
		}
		d.table([]string{"Work type", "Screen scheme"}, rows)
	}
}

func boolText(o spec.Opt[bool]) string {
	if !o.Set {
		return ""
	}
	if o.V {
		return "Yes"
	}
	return "No"
}

func (d *doc) spaceFields() {
	fs := d.s.Space.Fields
	if !fs.Set {
		return
	}
	configurations := d.s.Binding != nil && d.s.Binding.FieldBackend == spec.FieldBackendConfigurations
	b := d.spaceBinding()
	if configurations {
		d.section("Fields in the space", pathFieldConfig)
		if !d.schemeIntro(d.scheme(b.FieldConfigurationScheme, "Field Configuration Scheme"), "Field configuration scheme", "Work items › Fields") {
			return
		}
		d.para("Its field configuration shows these fields and hides every other.")
	} else {
		d.section("Fields in the space", pathFieldScheme)
		if !d.schemeIntro(d.scheme(b.FieldScheme, "Field Scheme"), "Field scheme", "Work items › Fields") {
			return
		}
		d.para("Add these fields, and remove any other that Jira added and allows to be removed.")
	}
	own := map[string]string{}
	for _, f := range d.s.CustomFields {
		own[f.Name] = text(f.Description)
	}
	help := func(f spec.FieldSettings, field string) string {
		if t := text(f.Description); t != own[field] {
			return t
		}
		return ""
	}
	var rows [][]string
	for _, f := range fs.V {
		rows = append(rows, []string{f.Name, strings.Join(f.IssueTypes, ", "), boolText(f.Required), f.Renderer.V, help(f.FieldSettings, f.Name)})
		for _, o := range f.ByIssueType {
			rows = append(rows, []string{"↳ for " + o.IssueType, "", boolText(o.Required), o.Renderer.V, help(o.FieldSettings, f.Name)})
		}
	}
	d.table([]string{"Field", "Only for work types", "Required", "Renderer", "Help text in this space"}, rows)
}

func (d *doc) workflowScheme() {
	var rows [][]string
	for _, t := range d.s.Space.IssueTypes {
		if t.Workflow.Set {
			rows = append(rows, []string{t.Name, t.Workflow.V})
		}
	}
	if len(rows) == 0 {
		return
	}
	d.section("Workflows of the work types", pathWFScheme)
	if d.schemeIntro(d.scheme(d.spaceBinding().WorkflowScheme, "Workflow Scheme"), "Workflow scheme", "Work items › Workflows") {
		d.table([]string{"Work type", "Workflow"}, rows)
	}
}

func (d *doc) permissions() {
	p := d.s.Space.Permissions
	if !p.Set {
		return
	}
	d.section("Permissions", pathPermissions)
	if d.schemeIntro(d.scheme(p.V.Binding, "Permission Scheme"), "Permission scheme", "Permissions") {
		d.para("Grant exactly these, and revoke any grant Jira adds by itself (it adds the guest role to many):")
		var rows [][]string
		for _, g := range p.V.Grants {
			rows = append(rows, []string{permissionName(g.Permission), d.holders(g.Holders)})
		}
		d.table([]string{"Permission", "Granted to"}, rows)
	}
}

// permissionName is a permission key as words: BROWSE_PROJECTS is "Browse
// projects", as Jira lists it.
func permissionName(key string) string {
	words := strings.ToLower(strings.ReplaceAll(key, "_", " "))
	if words == "" {
		return code(key)
	}
	return strings.ToUpper(words[:1]) + words[1:] + " " + code(key)
}

func (d *doc) notifications() {
	n := d.s.Space.Notifications
	if !n.Set {
		return
	}
	d.section("Notifications", pathNotify)
	if d.schemeIntro(d.scheme(n.V.Binding, "Notification Scheme"), "Notification scheme", "Notifications") {
		var rows [][]string
		for _, e := range n.V.Events {
			rows = append(rows, []string{e.Permission, d.holders(e.Holders)})
		}
		d.table([]string{"Event", "Notify"}, rows)
	}
}

func (d *doc) security() {
	is := d.s.Space.IssueSecurity
	if !is.Set {
		return
	}
	if is.Null {
		d.section("Work item security", "**Space settings › Work item security**")
		d.para("The space uses no work item security scheme: remove the one set there, if any.")
		return
	}
	d.section("Work item security", pathSecurity)
	if d.schemeIntro(d.scheme(is.V.Binding, "Issue Security Scheme"), "Work item security scheme", "Work item security") {
		var rows [][]string
		def := ""
		for _, l := range is.V.Levels {
			rows = append(rows, []string{l.Name, text(l.Description), d.holders(l.Holders)})
			if l.Default {
				def = l.Name
			}
		}
		d.table([]string{"Security level", "Description", "Members"}, rows)
		if def != "" {
			d.para("Default level: %s.", def)
		}
	}
}

func (d *doc) spacePriorities() {
	p := d.s.Space.Priorities
	if !p.Has() {
		return
	}
	d.section("Priorities in the space", pathPrioScheme)
	if d.schemeIntro(d.scheme(p.V.Binding, "Priority Scheme"), "Priority scheme", "Priorities") {
		d.para("Priorities: %s.", strings.Join(p.V.Priorities, ", "))
		if def := text(p.V.Default); def != "" {
			d.para("Default priority: %s.", def)
		}
		d.para("Jira keeps them in the site's order (**Settings › Work items › Priorities**), not the order given here.")
	}
}

var componentAssignees = map[string]string{"": "", "space_default": "Space default", "component_lead": "Component lead",
	"space_lead": "Space lead", "unassigned": "Unassigned"}

func (d *doc) components() {
	if len(d.s.Space.Components) == 0 {
		return
	}
	d.section("Components", pathComponents)
	var rows [][]string
	for _, c := range d.s.Space.Components {
		rows = append(rows, []string{c.Name, text(c.Description), d.holder(text(c.Lead)), componentAssignees[text(c.AssigneeType)]})
	}
	d.table([]string{"Component", "Description", "Lead", "Default assignee"}, rows)
}

func (d *doc) versions() {
	if len(d.s.Space.Versions) == 0 {
		return
	}
	d.section("Versions", pathVersions)
	var rows [][]string
	for _, v := range d.s.Space.Versions {
		rows = append(rows, []string{v.Name, text(v.StartDate), text(v.ReleaseDate), boolText(v.Released), boolText(v.Archived), text(v.Description)})
	}
	d.table([]string{"Version", "Start date", "Release date", "Released", "Archived", "Description"}, rows)
}

func (d *doc) roleMembers() {
	if len(d.s.Space.RoleMembers) == 0 {
		return
	}
	d.section("Role members", pathRoleMembers)
	var rows [][]string
	for _, r := range d.s.Space.RoleMembers {
		rows = append(rows, []string{r.Role, d.holders(r.Holders)})
	}
	d.table([]string{"Role", "Add"}, rows)
}

func (d *doc) switches() {
	if len(d.assoc) == 0 {
		return
	}
	d.section("Switch the space to its schemes", pathSwitches)
	d.para("Jira may ask how to map existing work items' statuses, priorities or security levels; with a new space there are none.")
	d.table([]string{"Scheme", "Use", "Space settings page"}, d.assoc)
}

// shareText is a filter's viewers or editors in words.
func (d *doc) shareText(hs []string) string {
	if hs == nil {
		return ""
	}
	if len(hs) == 0 {
		return "Nobody else"
	}
	var out []string
	for _, h := range hs {
		kind, v, _ := strings.Cut(h, ":")
		switch kind {
		case "space":
			out = append(out, "Space "+d.s.Space.Key)
		case "logged_in":
			out = append(out, "Any logged-in user")
		case "anyone":
			out = append(out, "Public")
		case "role":
			out = append(out, "Space "+d.s.Space.Key+", role "+v)
		default:
			out = append(out, d.holder(h))
		}
	}
	return strings.Join(out, ", ")
}

func (d *doc) filters() {
	create, reused := toCreate(d, "Filters", d.s.Filters, func(f spec.Filter) spec.Mode { return f.Mode }, func(f spec.Filter) string { return f.Name })
	if len(create) == 0 {
		return
	}
	d.section("Filters", pathFilters)
	var rows [][]string
	for _, f := range create {
		rows = append(rows, []string{f.Name, code(f.JQL), d.shareText(f.Viewers), d.shareText(f.Editors), text(f.Description)})
	}
	d.table([]string{"Filter", "JQL", "Viewers", "Editors", "Description"}, rows)
	metaNotes(d, create, func(f spec.Filter) (string, []string) { return f.Name, f.Notes })
	d.existing(reused)
}

func (d *doc) boards() {
	create, reused := toCreate(d, "Boards", d.s.Boards, func(b spec.Board) spec.Mode { return b.Mode }, func(b spec.Board) string { return b.Name })
	if len(create) == 0 {
		return
	}
	d.section("Boards", pathBoards)
	var rows [][]string
	for _, b := range create {
		rows = append(rows, []string{b.Name, strings.ToUpper(b.Type[:1]) + b.Type[1:], b.Filter})
	}
	d.table([]string{"Board", "Type", "Filter"}, rows)
	metaNotes(d, create, func(b spec.Board) (string, []string) { return b.Name, b.Notes })
	d.existing(reused)
}

func (d *doc) automation() {
	create, reused := toCreate(d, "Automation rules", d.s.AutomationRules, func(r spec.AutomationRule) spec.Mode { return r.Mode },
		func(r spec.AutomationRule) string { return r.Name })
	if len(create) == 0 {
		return
	}
	d.section("Automation rules", pathAutomation)
	d.para("Build each rule in the rule editor from its outline; the types named are the components' types as Jira's "+
		"Automation REST API names them. The JSON is the whole rule in that API's form: use it to check a rule built by hand, "+
		"or to create the rule through the API. It is not a file for %s: Atlassian does not document that file's format, so "+
		"export a rule there first to see it.", pathImport)
	for _, r := range create {
		d.printf("### %s\n\n", r.Name)
		state := "Enabled"
		if r.State == "disabled" {
			state = "Disabled"
		}
		scope := "this space only"
		if r.Scope == spec.ScopeShared {
			scope = "shared with other spaces"
		}
		d.para("%s, %s.%s", state, scope, prefixed(" ", text(r.Description)))
		for _, n := range r.Notes {
			d.para("*Note:* %s", strings.Join(strings.Fields(n), " "))
		}
		body, _ := spec.JSONValue(r.Rule).(map[string]any)
		if outline := ruleOutline(body); outline != "" {
			d.printf("%s\n", outline)
		}
		d.printf("<details><summary>Rule JSON</summary>\n\n```json\n%s\n```\n\n</details>\n\n", jsonText(body))
	}
	d.existing(reused)
}

func prefixed(p, s string) string {
	if s == "" {
		return ""
	}
	return p + s
}

func (d *doc) layouts() {
	if len(d.s.IssueLayouts) == 0 {
		return
	}
	d.section("Work item layouts", pathLayouts)
	for _, l := range d.s.IssueLayouts {
		d.printf("### %s\n\n", l.Name)
		// Notes are the instructions; anything else the spec records is shown
		// as written.
		var rest []spec.KVAny
		entries, _ := l.Value.([]spec.KVAny)
		for _, kv := range entries {
			switch kv.Key {
			case "name", "manual", "id":
			case "notes":
				for _, n := range rawStrings(kv.Value) {
					d.printf("- %s\n", strings.Join(strings.Fields(n), " "))
				}
				d.printf("\n")
			default:
				rest = append(rest, kv)
			}
		}
		if len(rest) > 0 {
			d.printf("```yaml\n%s```\n\n", yamlText(rest))
		}
	}
}

func (d *doc) notes() {
	var notes []string
	for _, n := range d.s.Notes {
		notes = append(notes, strings.Join(strings.Fields(n), " "))
	}
	if props := d.s.Space.Properties; len(props) > 0 {
		var keys []string
		for _, p := range props {
			keys = append(keys, code(p.Key))
		}
		notes = append(notes, "Space properties ("+strings.Join(keys, ", ")+") can be set only through Jira's REST API.")
	}
	if len(notes) == 0 {
		return
	}
	d.printf("## Notes\n\n")
	for _, n := range notes {
		d.printf("- %s\n", n)
	}
	d.printf("\n")
}
