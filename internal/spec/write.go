package spec

import (
	"bytes"
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// maxFlowWidth is the longest a one-line {…} or […] may be; longer ones are
// written as blocks.
const maxFlowWidth = 100

// Write renders s in canonical form: fixed key order,
// name-sorted lists except where Jira's order is meaningful, defaults
// omitted, and short all-scalar mappings and lists on one line.
func Write(s *Spec) ([]byte, error) {
	w := &writer{block: map[*yaml.Node]bool{}}
	root := w.spec(s)
	w.style(root)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return addSpacing(buf.Bytes()), nil
}

// addSpacing puts a blank line before each top-level section (except kind,
// which stays with apiVersion) and before each section inside space. Lines
// inside multi-line block scalars (| or >) are left alone, since a blank line
// there would change the text.
func addSpacing(in []byte) []byte {
	lines := strings.Split(string(in), "\n")
	out := make([]string, 0, len(lines)+32)
	inSpace := false
	prevIndent := 0
	scalarIndent := -1 // indentation of the key owning an open block scalar
	for i, line := range lines {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if scalarIndent >= 0 {
			if strings.TrimSpace(line) == "" || indent > scalarIndent {
				out = append(out, line)
				continue
			}
			scalarIndent = -1
		}
		isKey := strings.TrimSpace(line) != "" && !strings.HasPrefix(strings.TrimSpace(line), "- ") &&
			!strings.HasPrefix(strings.TrimSpace(line), "#") && strings.Contains(line, ":")
		switch {
		case isKey && indent == 0:
			inSpace = strings.HasPrefix(line, "space:")
			if i > 0 && !strings.HasPrefix(line, "kind:") {
				out = append(out, "")
			}
		case isKey && indent == 2 && inSpace && !isFirstChild(lines, i) &&
			(strings.HasSuffix(line, ":") || prevIndent > 2):
			// Within space, short values (key, id, prefix) stay together;
			// sections that open or follow a nested block are set apart.
			out = append(out, "")
		}
		out = append(out, line)
		if strings.TrimSpace(line) != "" {
			prevIndent = indent
		}
		if opensBlockScalar(line) {
			scalarIndent = indent
		}
	}
	return []byte(strings.Join(out, "\n"))
}

// isFirstChild reports whether line i is the first key under its parent.
func isFirstChild(lines []string, i int) bool {
	return i > 0 && strings.HasSuffix(strings.TrimSpace(lines[i-1]), ":") &&
		len(lines[i-1])-len(strings.TrimLeft(lines[i-1], " ")) < 2
}

// opensBlockScalar reports whether a line ends with a block scalar indicator
// such as |, |-, >+ or |2.
func opensBlockScalar(line string) bool {
	_, value, ok := strings.Cut(line, ": ")
	if !ok {
		value = strings.TrimPrefix(strings.TrimSpace(line), "- ")
	}
	value = strings.TrimSpace(value)
	return value != "" && (value[0] == '|' || value[0] == '>') && strings.Trim(value[1:], "+-0123456789") == ""
}

type writer struct {
	block map[*yaml.Node]bool // nodes that stay in block style however short
}

// node builders

func scalar(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }
func boolNode(v bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(v)}
}

// floatNode writes whole numbers as integers (120, not 120.0 or !!float 120).
func floatNode(v float64) *yaml.Node {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	tag := "!!float"
	if !strings.ContainsAny(s, ".eE") {
		tag = "!!int"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: s}
}
func nullNode() *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"} }

func seq(items ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: items}
}

func strSeq(vs []string) *yaml.Node {
	n := seq()
	for _, v := range vs {
		n.Content = append(n.Content, scalar(v))
	}
	return n
}

// obj is a mapping under construction; empty values are skipped by the
// helpers so that defaults are omitted.
type obj struct{ n *yaml.Node }

func newObj() obj { return obj{&yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}} }

func (o obj) set(key string, v *yaml.Node) obj {
	if v != nil {
		o.n.Content = append(o.n.Content, scalar(key), v)
	}
	return o
}

func (o obj) str(key, v string) obj {
	if v == "" {
		return o
	}
	return o.set(key, scalar(v))
}

func (o obj) opt(key string, v Opt[string]) obj {
	switch {
	case !v.Set:
		return o
	case v.Null:
		return o.set(key, nullNode())
	}
	return o.set(key, scalar(v.V))
}

func (o obj) optBool(key string, v Opt[bool]) obj {
	switch {
	case !v.Set:
		return o
	case v.Null:
		return o.set(key, nullNode())
	}
	return o.set(key, boolNode(v.V))
}

func (o obj) flag(key string, v bool) obj {
	if !v {
		return o
	}
	return o.set(key, boolNode(true))
}

func (o obj) list(key string, vs []string) obj {
	if len(vs) == 0 {
		return o
	}
	return o.set(key, strSeq(vs))
}

func (o obj) items(key string, n *yaml.Node) obj {
	if n == nil || len(n.Content) == 0 {
		return o
	}
	return o.set(key, n)
}

func (o obj) meta(m Meta) obj {
	return o.str("id", m.ID).flag("manual", m.Manual).list("notes", m.Notes)
}

func (o obj) mode(m, def Mode) obj {
	if m == def {
		return o
	}
	return o.str("mode", string(m))
}

func sortedBy[T any](items []T, key func(T) string) []T {
	out := slices.Clone(items)
	slices.SortStableFunc(out, func(a, b T) int { return cmp.Compare(key(a), key(b)) })
	return out
}

func sortedStrings(vs []string) []string {
	out := slices.Clone(vs)
	slices.Sort(out)
	return out
}

func listOf[T any](items []T, item func(T) *yaml.Node) *yaml.Node {
	n := seq()
	for _, it := range items {
		n.Content = append(n.Content, item(it))
	}
	return n
}

// sections

func (w *writer) spec(s *Spec) *yaml.Node {
	o := newObj().str("apiVersion", s.APIVersion).str("kind", s.Kind)
	md := newObj().str("name", s.Metadata.Name).opt("description", s.Metadata.Description)
	w.block[md.n] = true
	o.set("metadata", md.n)
	if b := s.Binding; b != nil {
		bo := newObj().str("site", b.Site).str("cloud_id", b.CloudID).str("written_at", b.WrittenAt).
			str("tool_version", b.ToolVersion).str("field_backend", b.FieldBackend)
		w.block[bo.n] = true
		o.set("binding", bo.n)
	}
	o.items("roles", listOf(sortedBy(s.Roles, func(x Role) string { return x.Name }), func(x Role) *yaml.Node {
		return globalWith(x.GlobalObject, func(o obj) { o.list("default_members", sortedStrings(x.DefaultMembers)) })
	}))
	o.items("groups", listOf(byName(s.Groups), globalNode))
	o.items("statuses", listOf(sortedBy(s.Statuses, func(x Status) string { return x.Name }), func(x Status) *yaml.Node {
		return globalWith(x.GlobalObject, func(o obj) { o.opt("category", x.Category) })
	}))
	o.items("resolutions", listOf(sortedBy(s.Resolutions, func(x Resolution) string { return x.Name }), func(x Resolution) *yaml.Node {
		return globalWith(x.GlobalObject, func(o obj) { o.flag("site_default", x.SiteDefault) })
	}))
	// Priorities keep the site's order: it is meaningful (it orders them everywhere).
	o.items("priorities", listOf(s.Priorities, func(x Priority) *yaml.Node {
		return globalWith(x.GlobalObject, func(o obj) { o.opt("colour", x.Colour).opt("icon", x.Icon).flag("site_default", x.SiteDefault) })
	}))
	o.items("issue_link_types", listOf(sortedBy(s.IssueLinkTypes, func(x LinkType) string { return x.Name }), func(x LinkType) *yaml.Node {
		return globalWith(x.GlobalObject, func(o obj) { o.opt("inward", x.Inward).opt("outward", x.Outward) })
	}))
	o.items("issue_types", listOf(sortedBy(s.IssueTypes, func(x IssueType) string { return x.Name }), func(x IssueType) *yaml.Node {
		return globalWith(x.GlobalObject, func(o obj) { o.opt("level", x.Level).opt("avatar", x.Avatar).opt("icon", x.Icon) })
	}))
	o.items("custom_fields", listOf(sortedBy(s.CustomFields, func(x CustomField) string { return x.Name }), func(x CustomField) *yaml.Node {
		return globalWith(x.GlobalObject, func(o obj) { o.str("type", x.Type).opt("searcher", x.Searcher).flag("locked", x.Locked) })
	}))
	o.items("screens", listOf(sortedBy(s.Screens, func(x Screen) string { return x.Name }), w.screen))
	o.items("workflows", listOf(sortedBy(s.Workflows, func(x Workflow) string { return x.Name }), w.workflow))
	if s.Space != nil {
		o.set("space", w.space(s.Space))
	}
	o.items("filters", listOf(sortedBy(s.Filters, func(x Filter) string { return x.Name }), func(f Filter) *yaml.Node {
		return newObj().str("name", f.Name).mode(f.Mode, ModeManaged).opt("description", f.Description).str("jql", f.JQL).
			list("viewers", sortedStrings(f.Viewers)).list("editors", sortedStrings(f.Editors)).meta(f.Meta).n
	}))
	o.items("boards", listOf(sortedBy(s.Boards, func(x Board) string { return x.Name }), func(b Board) *yaml.Node {
		return newObj().str("name", b.Name).mode(b.Mode, ModeManaged).str("type", b.Type).str("filter", b.Filter).meta(b.Meta).n
	}))
	o.items("automation_rules", listOf(sortedBy(s.AutomationRules, func(x AutomationRule) string { return x.Name }), func(r AutomationRule) *yaml.Node {
		return newObj().str("name", r.Name).mode(r.Mode, ModeManaged).opt("description", r.Description).str("state", r.State).
			str("scope", r.Scope).meta(r.Meta).set("rule", anyNode(r.Rule)).n
	}))
	o.items("issue_layouts", listOf(s.IssueLayouts, rawNode))
	o.list("notes", s.Notes)
	w.block[o.n] = true
	return o.n
}

func byName(items []GlobalObject) []GlobalObject {
	return sortedBy(items, func(x GlobalObject) string { return x.Name })
}

func globalNode(g GlobalObject) *yaml.Node { return globalWith(g, nil) }

// globalWith writes name, mode, type-specific keys, description, then meta.
func globalWith(g GlobalObject, extra func(obj)) *yaml.Node {
	o := newObj().str("name", g.Name).str("mode", string(g.Mode))
	if extra != nil {
		extra(o)
	}
	return o.opt("description", g.Description).meta(g.Meta).n
}

func (w *writer) screen(s Screen) *yaml.Node {
	o := newObj().str("name", s.Name).mode(s.Mode, ModeManaged).opt("description", s.Description).meta(s.Meta)
	o.items("tabs", listOf(s.Tabs, func(t ScreenTab) *yaml.Node {
		return newObj().str("name", t.Name).list("fields", t.Fields).n
	}))
	return o.n
}

func (w *writer) workflow(wf Workflow) *yaml.Node {
	o := newObj().str("name", wf.Name).mode(wf.Mode, ModeManaged).opt("description", wf.Description).meta(wf.Meta)
	o.items("statuses", listOf(wf.Statuses, func(st WorkflowStatus) *yaml.Node {
		if st.Layout == nil && len(st.Properties) == 0 && !st.Deprecated {
			return scalar(st.Name)
		}
		so := newObj().str("name", st.Name)
		if st.Layout != nil {
			so.set("layout", newObj().set("x", floatNode(st.Layout.X)).set("y", floatNode(st.Layout.Y)).n)
		}
		return so.items("properties", kvNode(st.Properties)).flag("deprecated", st.Deprecated).n
	}))
	o.items("transitions", listOf(wf.Transitions, w.transition))
	return o.n
}

func kvNode(kvs []KV) *yaml.Node {
	if len(kvs) == 0 {
		return nil
	}
	o := newObj()
	for _, kv := range sortedBy(kvs, func(x KV) string { return x.Key }) {
		o.set(kv.Key, scalar(kv.Value))
	}
	return o.n
}

func (w *writer) transition(t Transition) *yaml.Node {
	o := newObj().str("name", t.Name)
	if t.Type != TransitionDirected {
		o.str("type", t.Type)
	}
	o.list("from", t.From).str("to", t.To).flag("looped", t.Looped).opt("description", t.Description).opt("screen", t.Screen).opt("event", t.Event)
	if t.Conditions != nil {
		o.set("conditions", w.condition(*t.Conditions))
	}
	o.items(ListValidators, listOf(t.Validators, ruleNode))
	o.items(ListPostFunctions, listOf(t.PostFunctions, ruleNode))
	o.items(ListTriggers, listOf(t.Triggers, ruleNode))
	if len(t.Properties) > 0 {
		po := newObj()
		for _, p := range sortedBy(t.Properties, func(x Property) string { return x.Key }) {
			if p.IsList {
				po.set(p.Key, strSeq(p.List))
			} else {
				po.set(p.Key, scalar(p.Value))
			}
		}
		o.set("properties", po.n)
	}
	return o.str("id", t.ID).n
}

func (w *writer) condition(c Condition) *yaml.Node {
	if c.Rule != nil {
		return ruleNode(*c.Rule)
	}
	return newObj().set(c.Op, listOf(c.Items, w.condition)).n
}

func ruleNode(r Rule) *yaml.Node {
	o := newObj().str("type", r.Type)
	for _, p := range r.Params {
		switch p.Kind {
		case KindString:
			o.set(p.Key, scalar(p.Str))
		case KindBool:
			o.set(p.Key, boolNode(p.Str == "true"))
		case KindList:
			o.set(p.Key, strSeq(p.List))
		case KindMap:
			o.items(p.Key, kvNode(p.Map))
		}
	}
	return o.n
}

func rawNode(r Raw) *yaml.Node { return anyNode(r.Value) }

func anyNode(v any) *yaml.Node {
	switch x := v.(type) {
	case nil:
		return nullNode()
	case ScalarValue:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: x.Tag, Value: x.Value}
	case []any:
		return listOf(x, anyNode)
	case []KVAny:
		o := newObj()
		for _, kv := range x {
			o.set(kv.Key, anyNode(kv.Value))
		}
		return o.n
	}
	return scalar(fmt.Sprint(v))
}

func (w *writer) space(s *Space) *yaml.Node {
	o := newObj().str("key", s.Key).str("id", s.ID).str("managed_prefix", s.ManagedPrefix)
	if d := s.Details; d != nil {
		do := newObj().opt("name", d.Name).opt("type", d.Type).opt("avatar", d.Avatar).opt("description", d.Description).opt("lead", d.Lead).
			opt("assignee_type", d.AssigneeType).opt("category", d.Category).opt("url", d.URL)
		w.block[do.n] = true
		o.set("details", do.n)
	}
	o.items("issue_types", listOf(s.IssueTypes, w.spaceIssueType))
	if s.Fields.Set {
		// Written even when empty: an empty list means "no fields", not "unmanaged".
		o.set("fields", listOf(sortedBy(s.Fields.V, func(x SpaceField) string { return x.Name }), spaceFieldNode))
	}
	if len(s.FieldContexts) > 0 {
		fo := newObj()
		for _, set := range sortedBy(s.FieldContexts, func(x FieldContextSet) string { return x.Field }) {
			fo.set(set.Field, listOf(set.Contexts, w.fieldContext))
		}
		o.set("field_contexts", fo.n)
	}
	writeSection(o, "permissions", s.Permissions, func(p Permissions) *yaml.Node {
		return newObj().items("grants", grantsNode(p.Grants)).set("binding", refNode(p.Binding)).n
	})
	writeSection(o, "notifications", s.Notifications, func(p Notifications) *yaml.Node {
		return newObj().items("events", grantsNode(p.Events)).set("binding", refNode(p.Binding)).n
	})
	writeSection(o, "issue_security", s.IssueSecurity, func(p IssueSecurity) *yaml.Node {
		levels := listOf(sortedBy(p.Levels, func(x SecurityLevel) string { return x.Name }), func(l SecurityLevel) *yaml.Node {
			return newObj().str("name", l.Name).opt("description", l.Description).flag("default", l.Default).
				list("holders", sortedStrings(l.Holders)).n
		})
		return newObj().items("levels", levels).set("binding", refNode(p.Binding)).n
	})
	writeSection(o, "priorities", s.Priorities, func(p SpacePriorities) *yaml.Node {
		return newObj().list("priorities", p.Priorities).opt("default", p.Default).set("binding", refNode(p.Binding)).n
	})
	if b := s.Binding; b != nil {
		bo := newObj().
			set("issue_type_scheme", refNode(b.IssueTypeScheme)).
			set("issue_type_screen_scheme", refNode(b.IssueTypeScreenScheme)).
			set("field_configuration_scheme", refNode(b.FieldConfigurationScheme)).
			set("field_scheme", refNode(b.FieldScheme)).
			set("workflow_scheme", refNode(b.WorkflowScheme))
		w.block[bo.n] = true
		o.set("binding", bo.n)
	}
	if len(s.RoleMembers) > 0 {
		ro := newObj()
		for _, r := range sortedBy(s.RoleMembers, func(x RoleMembers) string { return x.Role }) {
			ro.set(r.Role, strSeq(sortedStrings(r.Holders)))
		}
		o.set("role_members", ro.n)
	}
	o.items("components", listOf(sortedBy(s.Components, func(x Component) string { return x.Name }), func(c Component) *yaml.Node {
		return newObj().str("name", c.Name).opt("description", c.Description).opt("lead", c.Lead).
			opt("assignee_type", c.AssigneeType).meta(c.Meta).n
	}))
	o.items("versions", listOf(s.Versions, func(v Version) *yaml.Node {
		return newObj().str("name", v.Name).opt("description", v.Description).opt("start_date", v.StartDate).
			opt("release_date", v.ReleaseDate).optBool("released", v.Released).optBool("archived", v.Archived).meta(v.Meta).n
	}))
	if len(s.Properties) > 0 {
		po := newObj()
		for _, p := range sortedBy(s.Properties, func(x RawProperty) string { return x.Key }) {
			po.set(p.Key, anyNode(p.Value))
		}
		o.set("properties", po.n)
	}
	w.block[o.n] = true
	return o.n
}

func writeSection[T any](o obj, key string, s Section[T], body func(T) *yaml.Node) {
	switch {
	case !s.Set:
	case s.Null:
		o.set(key, nullNode())
	default:
		o.set(key, body(s.V))
	}
}

func grantsNode(gs []Grant) *yaml.Node {
	if len(gs) == 0 {
		return nil
	}
	o := newObj()
	for _, g := range sortedBy(gs, func(x Grant) string { return x.Permission }) {
		o.set(g.Permission, strSeq(sortedStrings(g.Holders)))
	}
	return o.n
}

func refNode(r *ContainerRef) *yaml.Node {
	if r == nil {
		return nil
	}
	return newObj().str("name", r.Name).str("id", r.ID).str("scope", r.Scope).mode(r.Mode, ModeManaged).
		flag("site_default", r.SiteDefault).opt("description", r.Description).n
}

func (w *writer) spaceIssueType(t SpaceIssueType) *yaml.Node {
	o := newObj().str("name", t.Name).flag("default", t.Default).opt("workflow", t.Workflow)
	if sc := t.Screens; sc != nil {
		if sc.Create == sc.Edit && sc.Edit == sc.View {
			o.str("screens", sc.Create) // one screen for every operation
		} else {
			o.set("screens", newObj().str("create", sc.Create).str("edit", sc.Edit).str("view", sc.View).n)
		}
	}
	if b := t.Binding; b != nil {
		o.set("binding", newObj().set("screen_scheme", refNode(b.ScreenScheme)).
			set("field_configuration", refNode(b.FieldConfiguration)).n)
	}
	return o.n
}

func (w *writer) fieldContext(c FieldContext) *yaml.Node {
	o := newObj()
	if c.IssueTypes == nil {
		o.set("issue_types", scalar("all"))
	} else {
		o.list("issue_types", c.IssueTypes)
	}
	o.items("options", listOf(c.Options, optionNode))
	switch {
	case !c.Default.Set:
	case c.Default.Null:
		o.set("default", nullNode())
	case c.Default.V.IsList:
		o.set("default", strSeq(c.Default.V.List))
	default:
		o.set("default", scalar(c.Default.V.Str))
	}
	return o.set("binding", refNode(c.Binding)).n
}

func optionNode(op Option) *yaml.Node {
	if !op.Disabled && len(op.Children) == 0 {
		return scalar(op.Value)
	}
	return newObj().str("value", op.Value).flag("disabled", op.Disabled).items("children", listOf(op.Children, optionNode)).n
}

// style puts short values on one line: a list of scalars, or a mapping whose
// values are scalars or such lists. Objects that are list items always stay
// in block form, so each item reads as its own block.
func (w *writer) style(n *yaml.Node) {
	for _, c := range n.Content {
		if n.Kind == yaml.SequenceNode && c.Kind == yaml.MappingNode {
			w.block[c] = true
		}
		w.style(c)
	}
	if w.block[n] || len(n.Content) == 0 {
		return
	}
	switch n.Kind {
	case yaml.SequenceNode:
		for _, c := range n.Content {
			if c.Kind != yaml.ScalarNode {
				return
			}
		}
	case yaml.MappingNode:
		for _, c := range n.Content {
			if c.Kind == yaml.MappingNode || (c.Kind == yaml.SequenceNode && c.Style != yaml.FlowStyle) {
				return
			}
		}
	default:
		return
	}
	n.Style = yaml.FlowStyle
	if width(n) > maxFlowWidth {
		n.Style = 0
	}
}

// width is the length of a node rendered on its own in flow style.
func width(n *yaml.Node) int {
	out, err := yaml.Marshal(n)
	if err != nil {
		return maxFlowWidth + 1
	}
	s := strings.TrimSuffix(string(out), "\n")
	if strings.Contains(s, "\n") {
		return maxFlowWidth + 1
	}
	return len(s)
}

func spaceFieldNode(f SpaceField) *yaml.Node {
	o := newObj().str("name", f.Name).list("issue_types", f.IssueTypes)
	settingsInto(o, f.FieldSettings)
	if len(f.ByIssueType) > 0 {
		bo := newObj()
		for _, b := range sortedBy(f.ByIssueType, func(x IssueTypeFieldSettings) string { return x.IssueType }) {
			bo.set(b.IssueType, settingsInto(newObj(), b.FieldSettings).n)
		}
		o.set("by_issue_type", bo.n)
	}
	return o.n
}

func settingsInto(o obj, s FieldSettings) obj {
	return o.optBool("required", s.Required).opt("renderer", s.Renderer).opt("description", s.Description)
}
