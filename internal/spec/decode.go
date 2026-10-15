package spec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Parse decodes a spec. It returns the spec (as complete as the input
// allows) and every structural problem found. It does not validate
// references; see Validate.
func Parse(data []byte) (*Spec, Diagnostics) {
	var diags Diagnostics
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			diags.errorf(Pos{}, "", "the file is empty")
		} else {
			diags.errorf(yamlErrorPos(err), "", "not valid YAML: %s", strings.TrimPrefix(err.Error(), "yaml: "))
		}
		return &Spec{}, diags
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		diags.errorf(pos(&extra), "", "the file contains more than one YAML document; use one spec per file")
	}
	root := &doc
	if root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		root = root.Content[0]
	}
	if rejectAliases(root, &diags) {
		return &Spec{}, diags
	}
	d := &decoder{diags: &diags}
	return d.spec(root), diags
}

// yamlErrorPos extracts "line N" from a YAML library error, if present.
func yamlErrorPos(err error) Pos {
	var line int
	if _, scanErr := fmt.Sscanf(strings.TrimPrefix(err.Error(), "yaml: "), "line %d:", &line); scanErr == nil {
		return Pos{Line: line, Col: 1}
	}
	return Pos{}
}

// rejectAliases refuses YAML anchors and aliases: they make files harder to
// review and allow exponential expansion.
func rejectAliases(n *yaml.Node, diags *Diagnostics) bool {
	found := false
	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		if n.Kind == yaml.AliasNode || n.Anchor != "" {
			diags.errorf(pos(n), "", "YAML anchors and aliases (& and *) are not supported; write the value out in full")
			found = true
			return
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(n)
	return found
}

func (d *decoder) spec(n *yaml.Node) *Spec {
	m := d.mapping(n, "")
	s := &Spec{Pos: pos(n)}
	s.APIVersion = m.str("apiVersion", true)
	if s.APIVersion != "" && s.APIVersion != APIVersion {
		d.diags.errorf(pos(m.vals["apiVersion"]), "apiVersion", "unsupported version %q; this tool reads %q", s.APIVersion, APIVersion)
	}
	s.Kind = m.str("kind", true)
	if s.Kind != "" && s.Kind != Kind {
		d.diags.errorf(pos(m.vals["kind"]), "kind", "must be %q, found %q", Kind, s.Kind)
	}
	s.Metadata = d.metadata(m.get("metadata"))
	if b := m.get("binding"); b != nil {
		s.Binding = d.siteBinding(b)
	}

	s.Roles = decodeList(d, m, "roles", d.role)
	s.Groups = decodeList(d, m, "groups", d.globalObject)
	s.Statuses = decodeList(d, m, "statuses", d.status)
	s.Resolutions = decodeList(d, m, "resolutions", func(n *yaml.Node, path string) Resolution {
		m := d.mapping(n, path)
		r := Resolution{GlobalObject: d.globalFields(m), SiteDefault: m.boolean("site_default")}
		m.done()
		return r
	})
	s.Priorities = decodeList(d, m, "priorities", d.priority)
	s.IssueLinkTypes = decodeList(d, m, "issue_link_types", d.linkType)
	s.IssueTypes = decodeList(d, m, "issue_types", d.issueType)
	s.CustomFields = decodeList(d, m, "custom_fields", d.customField)
	s.Screens = decodeList(d, m, "screens", d.screen)
	s.Workflows = decodeList(d, m, "workflows", d.workflow)

	if sp := m.get("space"); sp != nil {
		s.Space = d.space(sp)
	}

	s.Filters = decodeList(d, m, "filters", d.filter)
	s.Boards = decodeList(d, m, "boards", d.board)
	s.AutomationRules = decodeList(d, m, "automation_rules", d.automationRule)
	s.IssueLayouts = decodeList(d, m, "issue_layouts", d.raw)
	s.Notes = m.strList("notes")
	m.done()
	return s
}

// decodeList decodes each item of a list-valued key.
func decodeList[T any](d *decoder, m *mapping, key string, item func(*yaml.Node, string) T) []T {
	var out []T
	for i, n := range m.list(key) {
		out = append(out, item(n, fmt.Sprintf("%s[%d]", m.sub(key), i)))
	}
	return out
}

func (d *decoder) metadata(n *yaml.Node) Metadata {
	if n == nil {
		d.diags.errorf(Pos{}, "", "missing required key %q", "metadata")
		return Metadata{}
	}
	m := d.mapping(n, "metadata")
	md := Metadata{Pos: pos(n), Name: m.str("name", true), Description: m.optStr("description")}
	m.done()
	return md
}

func (d *decoder) siteBinding(n *yaml.Node) *SiteBinding {
	m := d.mapping(n, "binding")
	b := &SiteBinding{
		Pos:          pos(n),
		Site:         m.str("site", true),
		CloudID:      m.str("cloud_id", true),
		WrittenAt:    m.str("written_at", false),
		ToolVersion:  m.str("tool_version", false),
		FieldBackend: m.str("field_backend", false),
	}
	if b.FieldBackend != "" && b.FieldBackend != FieldBackendConfigurations && b.FieldBackend != FieldBackendSchemes {
		d.diags.errorf(pos(m.vals["field_backend"]), "binding.field_backend", "must be %s or %s, found %q",
			FieldBackendConfigurations, FieldBackendSchemes, b.FieldBackend)
	}
	m.done()
	return b
}

func (d *decoder) globalFields(m *mapping) GlobalObject {
	return GlobalObject{
		Meta:        m.meta(),
		Name:        m.str("name", true),
		Mode:        m.mode("", ModeReuse, ModeEnsure),
		Description: m.optStr("description"),
	}
}

func (d *decoder) globalObject(n *yaml.Node, path string) GlobalObject {
	m := d.mapping(n, path)
	g := d.globalFields(m)
	m.done()
	return g
}

func (d *decoder) role(n *yaml.Node, path string) Role {
	m := d.mapping(n, path)
	r := Role{GlobalObject: d.globalFields(m), DefaultMembers: m.strList("default_members")}
	m.done()
	return r
}

func (d *decoder) priority(n *yaml.Node, path string) Priority {
	m := d.mapping(n, path)
	p := Priority{GlobalObject: d.globalFields(m), Colour: m.optStr("colour"), Icon: m.optStr("icon"), SiteDefault: m.boolean("site_default")}
	m.done()
	return p
}

func (d *decoder) status(n *yaml.Node, path string) Status {
	m := d.mapping(n, path)
	s := Status{GlobalObject: d.globalFields(m), Category: m.optStr("category")}
	m.done()
	return s
}

func (d *decoder) linkType(n *yaml.Node, path string) LinkType {
	m := d.mapping(n, path)
	l := LinkType{GlobalObject: d.globalFields(m), Inward: m.optStr("inward"), Outward: m.optStr("outward")}
	m.done()
	return l
}

func (d *decoder) issueType(n *yaml.Node, path string) IssueType {
	m := d.mapping(n, path)
	t := IssueType{GlobalObject: d.globalFields(m), Level: m.optStr("level"), Avatar: m.optStr("avatar"), Icon: m.optStr("icon")}
	m.done()
	return t
}

func (d *decoder) customField(n *yaml.Node, path string) CustomField {
	m := d.mapping(n, path)
	f := CustomField{GlobalObject: d.globalFields(m), Type: m.str("type", false), Searcher: m.optStr("searcher"), Locked: m.boolean("locked")}
	m.done()
	return f
}

func (d *decoder) screen(n *yaml.Node, path string) Screen {
	m := d.mapping(n, path)
	s := Screen{
		Meta:        m.meta(),
		Name:        m.str("name", true),
		Mode:        m.mode(ModeManaged, ModeManaged, ModeReuse),
		Description: m.optStr("description"),
	}
	s.Tabs = decodeList(d, m, "tabs", func(n *yaml.Node, path string) ScreenTab {
		tm := d.mapping(n, path)
		t := ScreenTab{Pos: pos(n), Name: tm.str("name", true), Fields: tm.strList("fields")}
		tm.done()
		return t
	})
	m.done()
	return s
}

func (d *decoder) workflow(n *yaml.Node, path string) Workflow {
	m := d.mapping(n, path)
	w := Workflow{
		Meta:        m.meta(),
		Name:        m.str("name", true),
		Mode:        m.mode(ModeManaged, ModeManaged, ModeReuse),
		Description: m.optStr("description"),
	}
	w.Statuses = decodeList(d, m, "statuses", d.workflowStatus)
	w.Transitions = decodeList(d, m, "transitions", d.transition)
	m.done()
	return w
}

// workflowStatus accepts a bare status name or a mapping with layout and properties.
func (d *decoder) workflowStatus(n *yaml.Node, path string) WorkflowStatus {
	if n.Kind == yaml.ScalarNode {
		v, _ := d.scalar(n, path)
		return WorkflowStatus{Pos: pos(n), Name: v}
	}
	m := d.mapping(n, path)
	s := WorkflowStatus{Pos: pos(n), Name: m.str("name", true), Properties: m.strMap("properties"), Deprecated: m.boolean("deprecated")}
	if l := m.get("layout"); l != nil {
		lm := d.mapping(l, m.sub("layout"))
		s.Layout = &Layout{X: lm.number("x"), Y: lm.number("y")}
		lm.done()
	}
	m.done()
	return s
}

func (d *decoder) transition(n *yaml.Node, path string) Transition {
	m := d.mapping(n, path)
	t := Transition{
		Pos:         pos(n),
		ID:          m.str("id", false),
		Name:        m.str("name", true),
		Type:        m.str("type", false),
		From:        m.strList("from"),
		To:          m.str("to", false),
		Looped:      m.boolean("looped"),
		Description: m.optStr("description"),
		Screen:      m.optStr("screen"),
		Event:       m.optStr("event"),
	}
	if t.Type == "" {
		t.Type = TransitionDirected
	}
	if c := m.get("conditions"); c != nil {
		cond := d.conditionGroup(c, m.sub("conditions"))
		t.Conditions = &cond
	}
	t.Validators = decodeList(d, m, ListValidators, func(n *yaml.Node, p string) Rule { return d.rule(n, p, ListValidators) })
	t.PostFunctions = decodeList(d, m, ListPostFunctions, func(n *yaml.Node, p string) Rule { return d.rule(n, p, ListPostFunctions) })
	t.Triggers = decodeList(d, m, ListTriggers, func(n *yaml.Node, p string) Rule { return d.rule(n, p, ListTriggers) })
	keys, vals := m.pairs("properties")
	for i, k := range keys {
		p := Property{Pos: pos(k), Key: k.Value}
		kp := m.sub("properties") + "." + k.Value
		if vals[i].Kind == yaml.SequenceNode {
			p.List, p.IsList = d.strList(vals[i], kp), true
		} else if v, ok := d.scalar(vals[i], kp); ok {
			p.Value = v
		}
		t.Properties = append(t.Properties, p)
	}
	m.done()
	return t
}

// conditionGroup reads {all: [...]} or {any: [...]}.
func (d *decoder) conditionGroup(n *yaml.Node, path string) Condition {
	m := d.mapping(n, path)
	c := Condition{Pos: pos(n)}
	switch {
	case m.has("all") && m.has("any"):
		d.diags.errorf(pos(n), path, "a condition group has either all or any, not both")
	case m.has("all"):
		c.Op = "all"
	case m.has("any"):
		c.Op = "any"
	default:
		d.diags.errorf(pos(n), path, "a condition group needs all: [...] or any: [...]")
	}
	if c.Op != "" {
		for i, item := range m.list(c.Op) {
			c.Items = append(c.Items, d.conditionItem(item, fmt.Sprintf("%s.%s[%d]", path, c.Op, i)))
		}
	}
	m.done()
	return c
}

// conditionItem is either a nested group or a rule.
func (d *decoder) conditionItem(n *yaml.Node, path string) Condition {
	if n.Kind == yaml.MappingNode {
		probe := d.mapping(n, path)
		if (probe.has("all") || probe.has("any")) && !probe.has("type") {
			return d.conditionGroup(n, path)
		}
	}
	r := d.rule(n, path, ListConditions)
	return Condition{Pos: pos(n), Rule: &r}
}

func (d *decoder) rule(n *yaml.Node, path, list string) Rule {
	m := d.mapping(n, path)
	r := Rule{Pos: pos(n), Type: m.str("type", true)}
	if r.Type == "" {
		return r // parameters can't be checked without a type; the missing type is already reported
	}
	rs, ok := lookupRule(list, r.Type)
	if !ok {
		msg := fmt.Sprintf("unknown %s rule type %q", strings.ReplaceAll(list, "_", " "), r.Type)
		if s := suggest(r.Type, ruleTypes(list)); s != "" {
			msg += fmt.Sprintf(" (did you mean %q?)", s)
		}
		d.diags.errorf(pos(m.vals["type"]), m.sub("type"), "%s; known types: %s", msg, strings.Join(ruleTypes(list), ", "))
		for _, k := range m.keys {
			m.used[k] = true
		}
		return r
	}
	for _, ps := range rs.Params {
		vn := m.get(ps.Key)
		if vn == nil {
			if ps.Required {
				d.diags.errorf(pos(n), path, "rule %q needs %q", r.Type, ps.Key)
			}
			continue
		}
		p := Param{Key: ps.Key, Kind: ps.Kind, Value: pos(vn)}
		kp := m.sub(ps.Key)
		switch ps.Kind {
		case KindString:
			p.Str, _ = d.scalar(vn, kp)
			if len(ps.Enum) > 0 && p.Str != "" && !containsStr(ps.Enum, p.Str) {
				d.diags.errorf(pos(vn), kp, "must be one of %s, found %q", strings.Join(ps.Enum, ", "), p.Str)
			}
		case KindList:
			p.List = d.strList(vn, kp)
		case KindBool:
			b, _ := d.boolValue(vn, kp)
			p.Str = fmt.Sprint(b)
		case KindMap:
			p.Map = m.strMap(ps.Key)
		}
		r.Params = append(r.Params, p)
	}
	if len(rs.AnyOf) > 0 {
		found := false
		for _, k := range rs.AnyOf {
			found = found || m.has(k)
		}
		if !found {
			d.diags.errorf(pos(n), path, "rule %q needs at least one of %s", r.Type, strings.Join(rs.AnyOf, ", "))
		}
	}
	m.done()
	return r
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (d *decoder) raw(n *yaml.Node, path string) Raw {
	m := d.mapping(n, path)
	r := Raw{Pos: pos(n), Name: m.str("name", true), Value: d.anyValue(n, path)}
	for _, k := range m.keys {
		m.used[k] = true // raw objects keep every key as written
	}
	return r
}

func (d *decoder) filter(n *yaml.Node, path string) Filter {
	m := d.mapping(n, path)
	f := Filter{Meta: m.meta(), Name: m.str("name", true), Mode: m.mode(ModeManaged, ModeManaged, ModeReuse),
		Description: m.optStr("description"), JQL: m.str("jql", true), Viewers: m.strList("viewers"), Editors: m.strList("editors")}
	m.done()
	return f
}

func (d *decoder) board(n *yaml.Node, path string) Board {
	m := d.mapping(n, path)
	b := Board{Meta: m.meta(), Name: m.str("name", true), Mode: m.mode(ModeManaged, ModeManaged, ModeReuse),
		Type: m.str("type", true), Filter: m.str("filter", true)}
	m.done()
	return b
}

func (d *decoder) automationRule(n *yaml.Node, path string) AutomationRule {
	m := d.mapping(n, path)
	r := AutomationRule{Meta: m.meta(), Name: m.str("name", true), Mode: m.mode(ModeManaged, ModeManaged, ModeReuse),
		Description: m.optStr("description"), State: m.str("state", false), Scope: m.str("scope", false)}
	if rule := m.get("rule"); rule != nil {
		r.Rule = d.anyValue(rule, path+".rule")
	}
	m.done()
	return r
}
