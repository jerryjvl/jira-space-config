package spec

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// FieldTypes are the short custom field type names the format accepts. A
// full type key (containing ':') is also accepted, for app-provided fields.
var FieldTypes = []string{
	"cascading", "checkboxes", "date", "datetime", "group", "labels", "multigroup",
	"multiselect", "multiuser", "multiversion", "number", "paragraph", "project",
	"radio", "readonly", "select", "text", "url", "user", "version",
}

var (
	spaceKeyPattern   = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	permissionPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	datePattern       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	colourPattern     = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
)

// Holder kinds: kinds that need a value, and kinds that take none.
var (
	holderWithValue    = []string{"role", "group", "user", "user_field", "group_field", "email"}
	holderWithoutValue = []string{"reporter", "current_assignee", "current_user", "space_lead",
		"component_lead", "all_watchers", "anyone"}
	holderEitherValue = []string{"application_role"}
)

// idBearingProperties are transition properties whose values are lists of
// names the tool resolves to IDs.
var idBearingProperties = []string{"jira.field.resolution.include", "jira.field.resolution.exclude"}

// Validate checks a parsed spec offline: internal consistency, references
// between its own sections, naming rules and syntax of embedded values. It
// cannot know what exists on a site; names that the spec uses but does not
// declare are reported as info, because they must already exist there.
func Validate(s *Spec) Diagnostics {
	return validate(s).diags
}

// ManagedNames lists the names of the objects s manages, as validation finds
// them; export uses it to work out the space's managed_prefix.
func ManagedNames(s *Spec) []string {
	return validate(s).managed
}

func validate(s *Spec) *validator {
	v := &validator{s: s, external: map[string]Pos{}}
	v.globals()
	v.screens()
	v.workflows()
	v.space()
	v.filtersAndBoards()
	v.automationRules()
	v.raws()
	v.ids()
	v.externals()
	return v
}

type validator struct {
	s        *Spec
	diags    Diagnostics
	external map[string]Pos // "kind\x00name" → first use
	hasIDs   bool
	idPos    Pos
	managed  []string // the names managedName was given
}

func (v *validator) noteID(id string, p Pos) {
	if id != "" && !v.hasIDs {
		v.hasIDs, v.idPos = true, p
	}
}

// ref records a use of a name. If the spec declares it, nothing happens;
// otherwise it is reported once as an external reference.
func (v *validator) ref(kind, name string, declared []string, p Pos) {
	if name == "" || slices.Contains(declared, name) {
		return
	}
	key := kind + "\x00" + name
	if _, seen := v.external[key]; !seen {
		v.external[key] = p
	}
}

func (v *validator) externals() {
	for key, p := range v.external {
		kind, name, _ := strings.Cut(key, "\x00")
		v.diags.infof(p, "", "%s %q is not declared in this spec; it must already exist on the site", kind, name)
	}
}

func unique[T any](v *validator, items []T, section string, name func(T) string, at func(T) Pos) {
	seen := map[string]bool{}
	for i, it := range items {
		n := name(it)
		if n == "" {
			continue
		}
		if seen[n] {
			v.diags.errorf(at(it), fmt.Sprintf("%s[%d]", section, i), "%q appears more than once", n)
		}
		seen[n] = true
	}
}

func names[T any](items []T, name func(T) string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, name(it))
	}
	return out
}

func gname(g GlobalObject) string { return g.Name }
func gpos(g GlobalObject) Pos     { return g.Pos }

func (v *validator) globals() {
	s := v.s
	// The kinds that are a GlobalObject and nothing more that is checked here.
	for _, sec := range []struct {
		name  string
		items []GlobalObject
	}{
		{"roles", globals(s.Roles, func(x Role) GlobalObject { return x.GlobalObject })},
		{"groups", s.Groups},
		{"resolutions", globals(s.Resolutions, func(x Resolution) GlobalObject { return x.GlobalObject })},
		{"priorities", globals(s.Priorities, func(x Priority) GlobalObject { return x.GlobalObject })},
	} {
		unique(v, sec.items, sec.name, gname, gpos)
		for _, g := range sec.items {
			v.noteID(g.ID, g.Pos)
		}
	}
	for _, g := range s.Groups {
		if g.Description.Has() {
			v.diags.warnf(g.Pos, "groups", "group %q: Jira keeps no description for a group, so it is ignored", g.Name)
		}
	}

	for i, r := range s.Roles {
		for _, h := range r.DefaultMembers {
			v.holder(h, r.Pos, fmt.Sprintf("roles[%d].default_members", i), []string{"user", "group"})
		}
	}
	siteDefaults := 0
	for i, p := range s.Priorities {
		if p.Colour.Has() && !colourPattern.MatchString(p.Colour.V) {
			v.diags.errorf(p.Pos, fmt.Sprintf("priorities[%d].colour", i), "must be a colour as #rrggbb, found %q", p.Colour.V)
		}
		if p.Mode == ModeEnsure && (!p.Colour.Has()) {
			v.diags.errorf(p.Pos, fmt.Sprintf("priorities[%d]", i), "a priority with mode ensure needs a colour, so it can be created")
		}
		if p.SiteDefault {
			siteDefaults++
		}
	}
	if siteDefaults > 1 {
		v.diags.errorf(s.Pos, "priorities", "only one priority can be the site default, found %d", siteDefaults)
	}

	unique(v, s.Statuses, "statuses", func(x Status) string { return x.Name }, func(x Status) Pos { return x.Pos })
	for i, st := range s.Statuses {
		path := fmt.Sprintf("statuses[%d]", i)
		v.noteID(st.ID, st.Pos)
		if st.Category.Has() && !slices.Contains([]string{"todo", "in_progress", "done"}, st.Category.V) {
			v.diags.errorf(st.Pos, path+".category", "must be todo, in_progress or done, found %q", st.Category.V)
		}
		if st.Mode == ModeEnsure && (!st.Category.Has()) {
			v.diags.errorf(st.Pos, path, "a status with mode ensure needs a category, so it can be created")
		}
	}

	unique(v, s.IssueLinkTypes, "issue_link_types", func(x LinkType) string { return x.Name }, func(x LinkType) Pos { return x.Pos })
	for i, l := range s.IssueLinkTypes {
		v.noteID(l.ID, l.Pos)
		if l.Mode == ModeEnsure && (!l.Inward.Has() || !l.Outward.Has()) {
			v.diags.errorf(l.Pos, fmt.Sprintf("issue_link_types[%d]", i), "a link type with mode ensure needs inward and outward descriptions")
		}
	}

	unique(v, s.IssueTypes, "issue_types", func(x IssueType) string { return x.Name }, func(x IssueType) Pos { return x.Pos })
	for i, t := range s.IssueTypes {
		path := fmt.Sprintf("issue_types[%d]", i)
		v.noteID(t.ID, t.Pos)
		if !t.Level.Has() {
			continue
		}
		switch lvl := t.Level.V; {
		case lvl == "subtask" || lvl == "standard":
		case lvl == "epic" || isNumberAbove(lvl, 1):
			if t.Mode == ModeEnsure {
				v.diags.apiErrorf(t.Pos, path+".level", "issue types at level %q cannot be created through the API; use mode reuse", lvl)
			}
		default:
			v.diags.errorf(t.Pos, path+".level", "must be subtask, standard, epic or a number above 1, found %q", lvl)
		}
	}

	unique(v, s.CustomFields, "custom_fields", func(x CustomField) string { return x.Name }, func(x CustomField) Pos { return x.Pos })
	for i, f := range s.CustomFields {
		path := fmt.Sprintf("custom_fields[%d]", i)
		v.noteID(f.ID, f.Pos)
		switch {
		case f.Type == "" && f.Mode == ModeEnsure:
			v.diags.errorf(f.Pos, path, "a custom field with mode ensure needs a type, so it can be created")
		// For reused fields the type is only compared with what Jira reports, and
		// Jira has type keys in more shapes than vendor:name, so any is accepted.
		case f.Mode == ModeEnsure && f.Type != "" && !strings.Contains(f.Type, ":") && !slices.Contains(FieldTypes, f.Type):
			msg := fmt.Sprintf("unknown field type %q", f.Type)
			if sg := suggest(f.Type, FieldTypes); sg != "" {
				msg += fmt.Sprintf(" (did you mean %q?)", sg)
			}
			v.diags.errorf(f.Pos, path+".type", "%s; known types: %s, or a full type key", msg, strings.Join(FieldTypes, ", "))
		case strings.Contains(f.Type, ":") && f.Mode == ModeEnsure && !strings.HasPrefix(f.Type, "com.atlassian.jira.plugin.system.customfieldtypes:"):
			v.diags.apiErrorf(f.Pos, path+".type", "app-provided field types cannot be created through the API; use mode reuse")
		}
	}
}

func isNumberAbove(s string, min int) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n > min
}

// managedName checks a managed object's name carries the space's prefix.
func (v *validator) managedName(name string, mode Mode, p Pos, path string) {
	if mode != ModeManaged || name == "" {
		return
	}
	v.managed = append(v.managed, name)
	prefix := ""
	if v.s.Space != nil {
		prefix = v.s.Space.ManagedPrefix
	}
	if prefix == "" {
		v.diags.errorf(p, path, "%q is managed (the default), so space.managed_prefix must be set; or use mode reuse for an existing object", name)
		return
	}
	if !strings.HasPrefix(name, prefix+" ") {
		v.diags.errorf(p, path, "managed name %q must start with the prefix %q followed by a space", name, prefix)
	}
}

func (v *validator) screens() {
	s := v.s
	unique(v, s.Screens, "screens", func(x Screen) string { return x.Name }, func(x Screen) Pos { return x.Pos })
	for i, sc := range s.Screens {
		path := fmt.Sprintf("screens[%d]", i)
		v.noteID(sc.ID, sc.Pos)
		v.managedName(sc.Name, sc.Mode, sc.Pos, path)
		if sc.Mode == ModeManaged && len(sc.Tabs) == 0 {
			v.diags.errorf(sc.Pos, path, "a managed screen needs at least one tab")
		}
		unique(v, sc.Tabs, path+".tabs", func(x ScreenTab) string { return x.Name }, func(x ScreenTab) Pos { return x.Pos })
		seen := map[string]bool{}
		for j, t := range sc.Tabs {
			for _, f := range t.Fields {
				if seen[f] {
					v.diags.errorf(t.Pos, fmt.Sprintf("%s.tabs[%d]", path, j), "field %q is on this screen more than once", f)
				}
				seen[f] = true
				v.fieldRef(f, t.Pos)
			}
		}
	}
}

func (v *validator) fieldRef(name string, p Pos) {
	v.ref("field", name, append(names(v.s.CustomFields, func(x CustomField) string { return x.Name }), systemFields...), p)
}

// systemFields are Jira's built-in fields, which never need declaring.
var systemFields = []string{
	"Summary", "Description", "Issue Type", "Reporter", "Assignee", "Priority", "Labels", "Components",
	"Fix versions", "Affects versions", "Due date", "Environment", "Attachment", "Comment",
	"Linked Issues", "Linked work items", // Atlassian renamed it; both appear
	"Time tracking", "Original estimate", "Remaining Estimate", "Resolution", "Security Level",
	"Parent", "Sprint", "Log Work", "Start date", "Team",
}

func (v *validator) workflows() {
	s := v.s
	statusNames := names(s.Statuses, func(x Status) string { return x.Name })
	screenNames := names(s.Screens, func(x Screen) string { return x.Name })
	unique(v, s.Workflows, "workflows", func(x Workflow) string { return x.Name }, func(x Workflow) Pos { return x.Pos })
	for i, wf := range s.Workflows {
		path := fmt.Sprintf("workflows[%d]", i)
		v.noteID(wf.ID, wf.Pos)
		v.managedName(wf.Name, wf.Mode, wf.Pos, path)
		unique(v, wf.Statuses, path+".statuses", func(x WorkflowStatus) string { return x.Name }, func(x WorkflowStatus) Pos { return x.Pos })
		inWorkflow := names(wf.Statuses, func(x WorkflowStatus) string { return x.Name })
		for _, st := range wf.Statuses {
			v.ref("status", st.Name, statusNames, st.Pos)
		}
		if wf.Mode == ModeReuse && len(wf.Transitions) == 0 {
			continue
		}
		initial := 0
		transitionNames := map[string]bool{}
		for j, t := range wf.Transitions {
			tp := fmt.Sprintf("%s.transitions[%d]", path, j)
			v.transition(t, tp, inWorkflow, screenNames)
			if t.Type == TransitionInitial {
				initial++
			}
			if transitionNames[t.Name] {
				v.diags.warnf(t.Pos, tp, "another transition is also named %q; Jira allows it, but it is easy to confuse", t.Name)
			}
			transitionNames[t.Name] = true
		}
		if initial != 1 {
			v.diags.errorf(wf.Pos, path, "a workflow needs exactly one initial transition, found %d", initial)
		}
	}
}

func (v *validator) transition(t Transition, path string, inWorkflow, screenNames []string) {
	v.noteID(t.ID, t.Pos)
	checkStatus := func(name, key string) {
		if !slices.Contains(inWorkflow, name) {
			v.diags.errorf(t.Pos, path+"."+key, "status %q is not one of this workflow's statuses", name)
		}
	}
	switch t.Type {
	case TransitionInitial:
		if len(t.From) > 0 || t.Looped || t.To == "" {
			v.diags.errorf(t.Pos, path, "an initial transition has a to status and no from or looped")
		}
	case TransitionDirected:
		if len(t.From) == 0 {
			v.diags.errorf(t.Pos, path, "a directed transition needs from: [statuses] (or set type: global)")
		}
	case TransitionGlobal:
		if len(t.From) > 0 {
			v.diags.errorf(t.Pos, path, "a global transition is available from every status, so it has no from")
		}
	default:
		v.diags.errorf(t.Pos, path+".type", "must be initial, directed or global, found %q", t.Type)
	}
	if t.Type != TransitionInitial {
		switch {
		case t.Looped && t.To != "":
			v.diags.errorf(t.Pos, path, "a looped transition returns to the status it started from, so it has no to")
		case !t.Looped && t.To == "":
			v.diags.errorf(t.Pos, path, "needs a to status, or looped: true")
		}
	}
	for _, f := range t.From {
		checkStatus(f, "from")
	}
	if t.To != "" {
		checkStatus(t.To, "to")
	}
	if t.Screen.Has() {
		v.ref("screen", t.Screen.V, screenNames, t.Pos)
	}
	for _, p := range t.Properties {
		if p.IsList && !slices.Contains(idBearingProperties, p.Key) {
			v.diags.errorf(p.Pos, path+".properties."+p.Key, "only %s take a list of names; give other properties as a single value",
				strings.Join(idBearingProperties, " and "))
		}
	}
}

func (v *validator) space() {
	sp := v.s.Space
	if sp == nil {
		v.diags.errorf(v.s.Pos, "", "missing required key %q: every spec describes one space", "space")
		return
	}
	if sp.Key != "" && !spaceKeyPattern.MatchString(sp.Key) {
		v.diags.errorf(sp.Pos, "space.key", "%q is not a valid space key: a capital letter, then capital letters, digits or underscores", sp.Key)
	}
	v.noteID(sp.ID, sp.Pos)
	if sp.ManagedPrefix != "" && strings.TrimSpace(sp.ManagedPrefix) != sp.ManagedPrefix {
		v.diags.errorf(sp.Pos, "space.managed_prefix", "must not start or end with spaces")
	}
	if d := sp.Details; d != nil {
		v.enum(d.AssigneeType, []string{"unassigned", "space_lead"}, d.Pos, "space.details.assignee_type")
		v.enum(d.Type, []string{"software", "business", "service_desk", "product_discovery"}, d.Pos, "space.details.type")
		if d.Lead.Has() {
			v.holder(d.Lead.V, d.Pos, "space.details.lead", []string{"user"})
		}
	}
	v.spaceIssueTypes(sp)
	v.spaceFields(sp)
	v.fieldContexts(sp)
	v.spaceSchemes(sp)

	roleNames := names(v.s.Roles, func(x Role) string { return x.Name })
	for _, r := range sp.RoleMembers {
		v.ref("role", r.Role, roleNames, r.Pos)
		for _, h := range r.Holders {
			v.holder(h, r.Pos, "space.role_members."+r.Role, []string{"user", "group"})
		}
	}
	unique(v, sp.Components, "space.components", func(x Component) string { return x.Name }, func(x Component) Pos { return x.Pos })
	for i, c := range sp.Components {
		path := fmt.Sprintf("space.components[%d]", i)
		v.noteID(c.ID, c.Pos)
		v.enum(c.AssigneeType, []string{"space_default", "component_lead", "space_lead", "unassigned"}, c.Pos, path+".assignee_type")
		if c.Lead.Has() {
			v.holder(c.Lead.V, c.Pos, path+".lead", []string{"user"})
		}
	}
	unique(v, sp.Versions, "space.versions", func(x Version) string { return x.Name }, func(x Version) Pos { return x.Pos })
	for i, ver := range sp.Versions {
		path := fmt.Sprintf("space.versions[%d]", i)
		v.noteID(ver.ID, ver.Pos)
		for key, d := range map[string]Opt[string]{"start_date": ver.StartDate, "release_date": ver.ReleaseDate} {
			if d.Has() && !datePattern.MatchString(d.V) {
				v.diags.errorf(ver.Pos, path+"."+key, "must be a date as YYYY-MM-DD, found %q", d.V)
			}
		}
	}
}

func (v *validator) enum(o Opt[string], allowed []string, p Pos, path string) {
	if o.Has() && !slices.Contains(allowed, o.V) {
		v.diags.errorf(p, path, "must be one of %s, found %q", strings.Join(allowed, ", "), o.V)
	}
}

func (v *validator) spaceIssueTypes(sp *Space) {
	issueTypeNames := names(v.s.IssueTypes, func(x IssueType) string { return x.Name })
	workflowNames := names(v.s.Workflows, func(x Workflow) string { return x.Name })
	screenNames := names(v.s.Screens, func(x Screen) string { return x.Name })
	unique(v, sp.IssueTypes, "space.issue_types", func(x SpaceIssueType) string { return x.Name }, func(x SpaceIssueType) Pos { return x.Pos })
	defaults := 0
	for i, t := range sp.IssueTypes {
		path := fmt.Sprintf("space.issue_types[%d]", i)
		v.ref("issue type", t.Name, issueTypeNames, t.Pos)
		if t.Default {
			defaults++
		}
		if t.Workflow.Has() {
			v.ref("workflow", t.Workflow.V, workflowNames, t.Pos)
		}
		if sc := t.Screens; sc != nil {
			for _, name := range []string{sc.Create, sc.Edit, sc.View} {
				v.ref("screen", name, screenNames, sc.Pos)
			}
		}
		if b := t.Binding; b != nil {
			v.containerRef(b.ScreenScheme, path+".binding.screen_scheme")
			v.containerRef(b.FieldConfiguration, path+".binding.field_configuration")
		}
	}
	if defaults > 1 {
		v.diags.errorf(sp.Pos, "space.issue_types", "only one issue type can be the default, found %d", defaults)
	}
}

func (v *validator) containerRef(r *ContainerRef, path string) {
	if r == nil {
		return
	}
	v.noteID(r.ID, r.Pos)
	v.managedName(r.Name, r.Mode, r.Pos, path)
	if r.Scope == ScopeShared && r.Mode != ModeReuse {
		v.diags.errorf(r.Pos, path, "a shared context is used by other spaces too, so it must be mode reuse")
	}
}

func (v *validator) fieldContexts(sp *Space) {
	spaceTypes := names(sp.IssueTypes, func(x SpaceIssueType) string { return x.Name })
	customFields := names(v.s.CustomFields, func(x CustomField) string { return x.Name })
	for _, set := range sp.FieldContexts {
		path := "space.field_contexts." + set.Field
		v.ref("custom field", set.Field, customFields, set.Pos)
		spaceOwn := 0
		covered := map[string]bool{}
		all := false
		for i, c := range set.Contexts {
			cp := fmt.Sprintf("%s[%d]", path, i)
			overlap := ""
			if c.IssueTypes == nil {
				if all || len(covered) > 0 {
					overlap = "all issue types"
				}
				all = true
			}
			for _, it := range c.IssueTypes {
				if len(spaceTypes) > 0 && !slices.Contains(spaceTypes, it) {
					v.diags.errorf(c.Pos, cp+".issue_types", "%q is not one of the space's issue types", it)
				}
				if all || covered[it] {
					overlap = fmt.Sprintf("issue type %q", it)
				}
				covered[it] = true
			}
			if overlap != "" {
				v.diags.errorf(c.Pos, cp, "Jira allows one context per issue type in a space; this one overlaps another for %s", overlap)
			}
			if c.Binding == nil || c.Binding.Scope != ScopeShared {
				spaceOwn++
				if spaceOwn == 2 {
					v.diags.errorf(c.Pos, cp, "Jira allows one context of a field to be specific to a space; "+
						"other contexts that cover the space must be shared (binding scope: shared), such as the field's global context")
				}
			}
			v.options(c, cp)
			v.containerRef(c.Binding, cp+".binding")
		}
	}
}

func (v *validator) options(c FieldContext, path string) {
	seen := map[string]bool{}
	for _, o := range c.Options {
		if seen[o.Value] {
			v.diags.errorf(o.Pos, path+".options", "option %q appears more than once", o.Value)
		}
		seen[o.Value] = true
		child := map[string]bool{}
		for _, ch := range o.Children {
			if child[ch.Value] {
				v.diags.errorf(ch.Pos, path+".options", "option %q appears more than once under %q", ch.Value, o.Value)
			}
			child[ch.Value] = true
		}
	}
	if !c.Default.Has() || len(c.Options) == 0 {
		return
	}
	defaults := []string{c.Default.V.Str}
	if c.Default.V.IsList {
		defaults = c.Default.V.List
	}
	for _, d := range defaults {
		if !seen[d] && !isChildOption(c.Options, defaults, d) {
			v.diags.errorf(c.Pos, path+".default", "default %q is not one of the options", d)
		}
	}
}

// isChildOption accepts the second value of a cascading default [parent, child].
func isChildOption(opts []Option, def []string, value string) bool {
	if len(def) != 2 || def[1] != value {
		return false
	}
	for _, o := range opts {
		if o.Value == def[0] {
			for _, ch := range o.Children {
				if ch.Value == value {
					return true
				}
			}
		}
	}
	return false
}

func (v *validator) spaceSchemes(sp *Space) {
	if p := sp.Permissions; p.Set {
		if p.Null {
			v.diags.errorf(p.Pos, "space.permissions", "every space has a permission scheme; omit permissions instead of setting null")
		} else {
			for _, g := range p.V.Grants {
				gp := "space.permissions.grants." + g.Permission
				if !permissionPattern.MatchString(g.Permission) {
					v.diags.errorf(g.Pos, gp, "permission keys are written as Jira's keys, such as BROWSE_PROJECTS")
				}
				for _, h := range g.Holders {
					v.holder(h, g.Pos, gp, nil)
					if h == "anyone" || h == "application_role" {
						v.diags.warnf(g.Pos, gp, "%q grants access to every user; apply refuses it unless explicitly overridden", h)
					}
				}
			}
			v.containerRef(p.V.Binding, "space.permissions.binding")
		}
	}
	if n := sp.Notifications; n.Has() { // null: the space has no notification scheme
		for _, e := range n.V.Events {
			for _, h := range e.Holders {
				v.holder(h, e.Pos, "space.notifications.events."+e.Permission, nil)
			}
		}
		v.containerRef(n.V.Binding, "space.notifications.binding")
	}
	if is := sp.IssueSecurity; is.Has() {
		defaults := 0
		for _, l := range is.V.Levels {
			if l.Default {
				defaults++
			}
			for _, h := range l.Holders {
				v.holder(h, l.Pos, "space.issue_security.levels."+l.Name, nil)
				kind, _, _ := strings.Cut(h, ":")
				if t, ok := HolderTypes[kind]; ok && t.Security == "" {
					v.diags.errorf(l.Pos, "space.issue_security.levels."+l.Name, "Jira does not accept %q as a member of a security level (it takes role, group, user, reporter and application_role)", kind)
				}
			}
		}
		if defaults > 1 {
			v.diags.errorf(is.Pos, "space.issue_security", "only one security level can be the default, found %d", defaults)
		}
		v.containerRef(is.V.Binding, "space.issue_security.binding")
	}
	if pr := sp.Priorities; pr.Set {
		if pr.Null {
			v.diags.errorf(pr.Pos, "space.priorities", "every space has a priority scheme; omit priorities instead of setting null")
		} else {
			priorityNames := names(v.s.Priorities, func(x Priority) string { return x.Name })
			for _, name := range pr.V.Priorities {
				v.ref("priority", name, priorityNames, pr.Pos)
			}
			if d := pr.V.Default; d.Has() && !slices.Contains(pr.V.Priorities, d.V) {
				v.diags.errorf(pr.Pos, "space.priorities.default", "default %q is not one of the listed priorities", d.V)
			}
			v.containerRef(pr.V.Binding, "space.priorities.binding")
		}
	}
	if b := sp.Binding; b != nil {
		for path, r := range map[string]*ContainerRef{
			"issue_type_scheme": b.IssueTypeScheme, "issue_type_screen_scheme": b.IssueTypeScreenScheme,
			"field_configuration_scheme": b.FieldConfigurationScheme, "field_scheme": b.FieldScheme,
			"workflow_scheme": b.WorkflowScheme,
		} {
			v.containerRef(r, "space.binding."+path)
		}
		if b.FieldConfigurationScheme != nil && b.FieldScheme != nil {
			v.diags.errorf(b.Pos, "space.binding", "a space uses either a field configuration scheme or a field scheme, not both")
		}
	}
}

// holder checks one kind:value holder. allowed limits the kinds; nil allows all.
func (v *validator) holder(h string, p Pos, path string, allowed []string) {
	kind, value, hasValue := strings.Cut(h, ":")
	switch {
	case allowed != nil && !slices.Contains(allowed, kind):
		v.diags.errorf(p, path, "%q: only %s holders are allowed here", h, strings.Join(allowed, " or "))
		return
	case slices.Contains(holderWithValue, kind):
		if !hasValue || value == "" {
			v.diags.errorf(p, path, "%q needs a value, as %s:<value>", h, kind)
		}
	case slices.Contains(holderWithoutValue, kind):
		if hasValue {
			v.diags.errorf(p, path, "%q takes no value; write just %s", h, kind)
		}
	case slices.Contains(holderEitherValue, kind):
	default:
		all := slices.Concat(holderWithValue, holderWithoutValue, holderEitherValue)
		msg := fmt.Sprintf("unknown holder kind %q", kind)
		if sg := suggest(kind, all); sg != "" {
			msg += fmt.Sprintf(" (did you mean %q?)", sg)
		}
		v.diags.errorf(p, path, "%s in %q; holders are kind:value, kinds: %s", msg, h, strings.Join(all, ", "))
		return
	}
	if kind == "user" && strings.Contains(value, "@") {
		v.diags.errorf(p, path, "users are referenced by Atlassian account ID, not email address (%q)", h)
	}
	if kind == "role" {
		v.ref("role", value, names(v.s.Roles, func(x Role) string { return x.Name }), p)
	}
	if kind == "group" {
		v.ref("group", value, names(v.s.Groups, gname), p)
	}
}

func (v *validator) raws() {
	s := v.s
	for _, sec := range []struct {
		name  string
		items []Raw
	}{
		{"issue_layouts", s.IssueLayouts}} {
		unique(v, sec.items, sec.name, func(x Raw) string { return x.Name }, func(x Raw) Pos { return x.Pos })
	}
}

// ids checks that a spec with IDs says which site they belong to.
func (v *validator) ids() {
	if v.hasIDs && (v.s.Binding == nil || v.s.Binding.CloudID == "") {
		v.diags.errorf(v.idPos, "", "the spec contains IDs, which only mean something on one site; "+
			"add binding.site and binding.cloud_id, or remove the IDs")
	}
}

// spaceFields checks space.fields.
func (v *validator) spaceFields(sp *Space) {
	if !sp.Fields.Set {
		return
	}
	spaceTypes := names(sp.IssueTypes, func(x SpaceIssueType) string { return x.Name })
	inSpace := func(it string, p Pos, path string) {
		if len(spaceTypes) > 0 && !slices.Contains(spaceTypes, it) {
			v.diags.errorf(p, path, "%q is not one of the space's issue types", it)
		}
	}
	unique(v, sp.Fields.V, "space.fields", func(x SpaceField) string { return x.Name }, func(x SpaceField) Pos { return x.Pos })
	for i, f := range sp.Fields.V {
		path := fmt.Sprintf("space.fields[%d]", i)
		v.fieldRef(f.Name, f.Pos)
		v.fieldSettings(f.FieldSettings, f.Pos, path)
		for _, it := range f.IssueTypes {
			inSpace(it, f.Pos, path+".issue_types")
		}
		for _, b := range f.ByIssueType {
			bp := path + ".by_issue_type." + b.IssueType
			inSpace(b.IssueType, b.Pos, bp)
			if f.IssueTypes != nil && !slices.Contains(f.IssueTypes, b.IssueType) {
				v.diags.errorf(b.Pos, bp, "the field is limited to %s, so it has no settings for %q",
					strings.Join(f.IssueTypes, ", "), b.IssueType)
			}
			v.fieldSettings(b.FieldSettings, b.Pos, bp)
		}
	}
}

func (v *validator) fieldSettings(s FieldSettings, p Pos, path string) {
	v.enum(s.Renderer, []string{"wiki", "plain"}, p, path+".renderer")
}

func globals[T any](items []T, of func(T) GlobalObject) []GlobalObject {
	out := make([]GlobalObject, len(items))
	for i, it := range items {
		out[i] = of(it)
	}
	return out
}

// filtersAndBoards checks the filters and boards: unique names, managed
// names with the prefix, who may see and change a filter, and each
// board's type and filter.
func (v *validator) filtersAndBoards() {
	s := v.s
	unique(v, s.Filters, "filters", func(x Filter) string { return x.Name }, func(x Filter) Pos { return x.Pos })
	for i, f := range s.Filters {
		path := fmt.Sprintf("filters[%d]", i)
		v.noteID(f.ID, f.Pos)
		v.managedName(f.Name, f.Mode, f.Pos, path)
		for _, list := range []struct {
			key     string
			holders []string
		}{{"viewers", f.Viewers}, {"editors", f.Editors}} {
			for _, h := range list.holders {
				kind, _, hasValue := strings.Cut(h, ":")
				switch kind {
				case "space", "logged_in", "anyone":
					if hasValue {
						v.diags.errorf(f.Pos, path+"."+list.key, "%q takes no value; write just %s", h, kind)
					}
				default:
					v.holder(h, f.Pos, path+"."+list.key, []string{"role", "group", "user"})
				}
			}
		}
	}
	unique(v, s.Boards, "boards", func(x Board) string { return x.Name }, func(x Board) Pos { return x.Pos })
	filters := names(s.Filters, func(x Filter) string { return x.Name })
	for i, b := range s.Boards {
		path := fmt.Sprintf("boards[%d]", i)
		v.noteID(b.ID, b.Pos)
		v.managedName(b.Name, b.Mode, b.Pos, path)
		if !slices.Contains(BoardTypes, b.Type) {
			v.diags.errorf(b.Pos, path+".type", "%q is not a board type; it is one of %s", b.Type, strings.Join(BoardTypes, ", "))
		}
		if !slices.Contains(filters, b.Filter) {
			v.diags.errorf(b.Pos, path+".filter", "%q is not one of the spec's filters; a board's filter must be listed in filters", b.Filter)
		}
	}
}

// automationRules checks the automation rules: unique names, managed names
// with the prefix, the state and scope, and that the rule body is a mapping
// with a trigger (its content is Jira's, docs/automation.md).
func (v *validator) automationRules() {
	s := v.s
	unique(v, s.AutomationRules, "automation_rules", func(x AutomationRule) string { return x.Name }, func(x AutomationRule) Pos { return x.Pos })
	for i, r := range s.AutomationRules {
		path := fmt.Sprintf("automation_rules[%d]", i)
		v.noteID(r.ID, r.Pos)
		v.managedName(r.Name, r.Mode, r.Pos, path)
		if r.State != "" && !slices.Contains(RuleStates, r.State) {
			v.diags.errorf(r.Pos, path+".state", "%q is not a rule state; it is one of %s", r.State, strings.Join(RuleStates, ", "))
		}
		if r.Scope != "" && !slices.Contains(RuleScopes, r.Scope) {
			v.diags.errorf(r.Pos, path+".scope", "%q is not a rule scope; it is one of %s", r.Scope, strings.Join(RuleScopes, ", "))
		}
		if r.Scope == "shared" && r.Mode != ModeReuse {
			v.diags.errorf(r.Pos, path, "a shared rule is used by other spaces too, so it must be mode reuse")
		}
		body, isMap := r.Rule.([]KVAny)
		if !isMap {
			v.diags.errorf(r.Pos, path+".rule", "a rule needs its body: Jira's rule as a mapping, with a trigger (docs/automation.md)")
			continue
		}
		if !slices.ContainsFunc(body, func(kv KVAny) bool { return kv.Key == "trigger" }) {
			v.diags.errorf(r.Pos, path+".rule", "the rule has no trigger")
		}
	}
}
