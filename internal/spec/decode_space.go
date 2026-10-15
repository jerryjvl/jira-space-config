package spec

import (
	"fmt"

	"go.yaml.in/yaml/v3"
)

func (d *decoder) space(n *yaml.Node) *Space {
	m := d.mapping(n, "space")
	s := &Space{
		Pos:           pos(n),
		Key:           m.str("key", true),
		ID:            m.str("id", false),
		ManagedPrefix: m.str("managed_prefix", false),
	}
	if dn := m.get("details"); dn != nil {
		s.Details = d.details(dn)
	}
	s.IssueTypes = decodeList(d, m, "issue_types", d.spaceIssueType)
	s.Fields = d.spaceFields(m)

	keys, vals := m.pairs("field_contexts")
	for i, k := range keys {
		set := FieldContextSet{Pos: pos(k), Field: k.Value}
		path := "space.field_contexts." + k.Value
		if vals[i].Kind != yaml.SequenceNode {
			d.diags.errorf(pos(vals[i]), path, "expected a list of contexts, found %s", kindName(vals[i]))
			continue
		}
		for j, c := range vals[i].Content {
			set.Contexts = append(set.Contexts, d.fieldContext(c, fmt.Sprintf("%s[%d]", path, j)))
		}
		s.FieldContexts = append(s.FieldContexts, set)
	}

	s.Permissions = section(d, m, "permissions", func(sm *mapping) Permissions {
		return Permissions{Grants: d.grants(sm, "grants"), Binding: d.containerRef(sm.get("binding"), sm.sub("binding"), false)}
	})
	s.Notifications = section(d, m, "notifications", func(sm *mapping) Notifications {
		return Notifications{Events: d.grants(sm, "events"), Binding: d.containerRef(sm.get("binding"), sm.sub("binding"), false)}
	})
	s.IssueSecurity = section(d, m, "issue_security", func(sm *mapping) IssueSecurity {
		is := IssueSecurity{Binding: d.containerRef(sm.get("binding"), sm.sub("binding"), false)}
		is.Levels = decodeList(d, sm, "levels", func(n *yaml.Node, path string) SecurityLevel {
			lm := d.mapping(n, path)
			l := SecurityLevel{Pos: pos(n), Name: lm.str("name", true), Description: lm.optStr("description"),
				Default: lm.boolean("default"), Holders: lm.strList("holders")}
			lm.done()
			return l
		})
		return is
	})
	s.Priorities = section(d, m, "priorities", func(sm *mapping) SpacePriorities {
		return SpacePriorities{Priorities: sm.strList("priorities"), Default: sm.optStr("default"),
			Binding: d.containerRef(sm.get("binding"), sm.sub("binding"), false)}
	})

	if b := m.get("binding"); b != nil {
		bm := d.mapping(b, "space.binding")
		s.Binding = &SpaceBinding{
			Pos:                      pos(b),
			IssueTypeScheme:          d.containerRef(bm.get("issue_type_scheme"), bm.sub("issue_type_scheme"), false),
			IssueTypeScreenScheme:    d.containerRef(bm.get("issue_type_screen_scheme"), bm.sub("issue_type_screen_scheme"), false),
			FieldConfigurationScheme: d.containerRef(bm.get("field_configuration_scheme"), bm.sub("field_configuration_scheme"), false),
			FieldScheme:              d.containerRef(bm.get("field_scheme"), bm.sub("field_scheme"), false),
			WorkflowScheme:           d.containerRef(bm.get("workflow_scheme"), bm.sub("workflow_scheme"), false),
		}
		bm.done()
	}

	rkeys, rvals := m.pairs("role_members")
	for i, k := range rkeys {
		s.RoleMembers = append(s.RoleMembers, RoleMembers{
			Pos: pos(k), Role: k.Value, Holders: d.strList(rvals[i], "space.role_members."+k.Value),
		})
	}
	s.Components = decodeList(d, m, "components", func(n *yaml.Node, path string) Component {
		cm := d.mapping(n, path)
		c := Component{Meta: cm.meta(), Name: cm.str("name", true), Description: cm.optStr("description"),
			Lead: cm.optStr("lead"), AssigneeType: cm.optStr("assignee_type")}
		cm.done()
		return c
	})
	s.Versions = decodeList(d, m, "versions", func(n *yaml.Node, path string) Version {
		vm := d.mapping(n, path)
		v := Version{Meta: vm.meta(), Name: vm.str("name", true), Description: vm.optStr("description"),
			StartDate: vm.optStr("start_date"), ReleaseDate: vm.optStr("release_date"),
			Released: vm.optBool("released"), Archived: vm.optBool("archived")}
		vm.done()
		return v
	})
	pkeys, pvals := m.pairs("properties")
	for i, k := range pkeys {
		s.Properties = append(s.Properties, RawProperty{Pos: pos(k), Key: k.Value,
			Value: d.anyValue(pvals[i], "space.properties."+k.Value)})
	}
	m.done()
	return s
}

// section reads an optional, nullable mapping-valued block.
func section[T any](d *decoder, m *mapping, key string, read func(*mapping) T) Section[T] {
	n := m.get(key)
	if n == nil {
		return Section[T]{}
	}
	if isNull(n) {
		return Section[T]{Set: true, Null: true, Pos: pos(n)}
	}
	sm := d.mapping(n, m.sub(key))
	v := read(sm)
	sm.done()
	return Section[T]{Set: true, Pos: pos(n), V: v}
}

func (d *decoder) details(n *yaml.Node) *Details {
	m := d.mapping(n, "space.details")
	dt := &Details{
		Pos:          pos(n),
		Name:         m.optStr("name"),
		Type:         m.optStr("type"),
		Avatar:       m.optStr("avatar"),
		Description:  m.optStr("description"),
		Lead:         m.optStr("lead"),
		AssigneeType: m.optStr("assignee_type"),
		Category:     m.optStr("category"),
		URL:          m.optStr("url"),
	}
	m.done()
	return dt
}

func (d *decoder) spaceIssueType(n *yaml.Node, path string) SpaceIssueType {
	m := d.mapping(n, path)
	t := SpaceIssueType{
		Pos:      pos(n),
		Name:     m.str("name", true),
		Default:  m.boolean("default"),
		Workflow: m.optStr("workflow"),
	}
	if sn := m.get("screens"); sn != nil {
		t.Screens = d.screenSet(sn, m.sub("screens"))
	}
	if m.has("fields") {
		d.diags.errorf(pos(m.vals["fields"]), m.sub("fields"),
			"fields are listed once for the whole space, under space.fields, with issue_types or by_issue_type for differences")
		m.get("fields")
	}
	if b := m.get("binding"); b != nil {
		bm := d.mapping(b, m.sub("binding"))
		t.Binding = &IssueTypeBinding{
			Pos:                pos(b),
			ScreenScheme:       d.containerRef(bm.get("screen_scheme"), bm.sub("screen_scheme"), false),
			FieldConfiguration: d.containerRef(bm.get("field_configuration"), bm.sub("field_configuration"), false),
		}
		bm.done()
	}
	m.done()
	return t
}

// spaceFields reads space.fields: null or absent means fields are not
// managed; a list (even empty) is the complete set.
func (d *decoder) spaceFields(m *mapping) Section[[]SpaceField] {
	n := m.get("fields")
	switch {
	case n == nil:
		return Section[[]SpaceField]{}
	case isNull(n):
		d.diags.errorf(pos(n), "space.fields", "omit fields to leave them unmanaged; null has no meaning here")
		return Section[[]SpaceField]{}
	}
	out := Section[[]SpaceField]{Set: true, Pos: pos(n), V: []SpaceField{}}
	out.V = append(out.V, decodeList(d, m, "fields", func(n *yaml.Node, path string) SpaceField {
		fm := d.mapping(n, path)
		f := SpaceField{Pos: pos(n), Name: fm.str("name", true), FieldSettings: fieldSettings(fm)}
		if it := fm.get("issue_types"); it != nil {
			f.IssueTypes = d.strList(it, fm.sub("issue_types"))
			if len(f.IssueTypes) == 0 {
				d.diags.errorf(pos(it), fm.sub("issue_types"), "list at least one issue type, or omit issue_types for all")
			}
		}
		keys, vals := fm.pairs("by_issue_type")
		for i, k := range keys {
			bm := d.mapping(vals[i], fm.sub("by_issue_type")+"."+k.Value)
			f.ByIssueType = append(f.ByIssueType, IssueTypeFieldSettings{Pos: pos(k), IssueType: k.Value, FieldSettings: fieldSettings(bm)})
			bm.done()
		}
		fm.done()
		return f
	})...)
	return out
}

func fieldSettings(m *mapping) FieldSettings {
	return FieldSettings{Required: m.optBool("required"), Renderer: m.optStr("renderer"), Description: m.optStr("description")}
}

// screenSet accepts one screen name for all operations, or {create, edit, view}.
func (d *decoder) screenSet(n *yaml.Node, path string) *ScreenSet {
	if n.Kind == yaml.ScalarNode {
		v, _ := d.scalar(n, path)
		return &ScreenSet{Pos: pos(n), Create: v, Edit: v, View: v, Short: true}
	}
	m := d.mapping(n, path)
	s := &ScreenSet{Pos: pos(n), Create: m.str("create", true), Edit: m.str("edit", true), View: m.str("view", true)}
	m.done()
	return s
}

func (d *decoder) fieldContext(n *yaml.Node, path string) FieldContext {
	m := d.mapping(n, path)
	c := FieldContext{Pos: pos(n)}
	if it := m.get("issue_types"); it != nil {
		if it.Kind == yaml.ScalarNode && it.Value == "all" {
			c.IssueTypes = nil
		} else {
			c.IssueTypes = d.strList(it, m.sub("issue_types"))
			if it.Kind == yaml.SequenceNode && len(it.Content) == 0 {
				d.diags.errorf(pos(it), m.sub("issue_types"), "use all, or list at least one issue type")
			}
		}
	} else {
		d.diags.errorf(pos(n), path, "missing required key %q (all, or a list of the space's issue types)", "issue_types")
	}
	for i, o := range m.list("options") {
		c.Options = append(c.Options, d.option(o, fmt.Sprintf("%s.options[%d]", path, i), true))
	}
	if dv := m.get("default"); dv != nil {
		switch {
		case isNull(dv):
			c.Default = Opt[Value]{Set: true, Null: true}
		case dv.Kind == yaml.SequenceNode:
			c.Default = Some(Value{List: d.strList(dv, m.sub("default")), IsList: true})
		default:
			v, _ := d.scalar(dv, m.sub("default"))
			c.Default = Some(Value{Str: v})
		}
	}
	c.Binding = d.containerRef(m.get("binding"), m.sub("binding"), true)
	m.done()
	return c
}

// option accepts a bare value or {value, disabled, children}.
func (d *decoder) option(n *yaml.Node, path string, allowChildren bool) Option {
	if n.Kind == yaml.ScalarNode {
		v, _ := d.scalar(n, path)
		return Option{Pos: pos(n), Value: v}
	}
	m := d.mapping(n, path)
	o := Option{Pos: pos(n), Value: m.str("value", true), Disabled: m.boolean("disabled")}
	if allowChildren {
		for i, c := range m.list("children") {
			o.Children = append(o.Children, d.option(c, fmt.Sprintf("%s.children[%d]", path, i), false))
		}
	}
	m.done()
	return o
}

// grants reads a mapping of key → list of holders.
func (d *decoder) grants(m *mapping, key string) []Grant {
	keys, vals := m.pairs(key)
	out := make([]Grant, 0, len(keys))
	for i, k := range keys {
		out = append(out, Grant{Pos: pos(k), Permission: k.Value, Holders: d.strList(vals[i], m.sub(key)+"."+k.Value)})
	}
	return out
}

func (d *decoder) containerRef(n *yaml.Node, path string, allowScope bool) *ContainerRef {
	if n == nil {
		return nil
	}
	m := d.mapping(n, path)
	r := &ContainerRef{Pos: pos(n), Name: m.str("name", true), ID: m.str("id", false), Mode: m.mode(ModeManaged, ModeManaged, ModeReuse),
		Description: m.optStr("description"), SiteDefault: m.boolean("site_default")}
	if allowScope {
		r.Scope = m.str("scope", false)
		if r.Scope != "" && r.Scope != ScopeSpace && r.Scope != ScopeShared {
			d.diags.errorf(pos(m.vals["scope"]), m.sub("scope"), "must be %s or %s, found %q", ScopeSpace, ScopeShared, r.Scope)
		}
	}
	m.done()
	return r
}
