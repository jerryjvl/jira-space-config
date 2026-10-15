package reader

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// jiraRule is a workflow rule as the bulk workflow API returns it.
type jiraRule struct {
	RuleKey    string                     `json:"ruleKey"`
	Parameters map[string]json.RawMessage `json:"parameters"`
}

// params returns the rule's parameters as strings. Jira sends strings; any
// other JSON value is kept as its JSON text.
func (r jiraRule) params() map[string]string {
	out := map[string]string{}
	for k, raw := range r.Parameters {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			out[k] = s
		} else {
			out[k] = string(raw)
		}
	}
	return out
}

// resolver turns IDs in rule parameters into names.
type resolver func(id string) (string, bool)

// rule converts a Jira rule to its typed form when the registry knows it and
// every parameter converts cleanly; otherwise to an opaque rule, so nothing
// is lost.
func (r *Reader) rule(list string, jr jiraRule, where string) spec.Rule {
	params := jr.params()
	for _, rs := range spec.RulesFor(list, jr.RuleKey) {
		if rule, ok := r.typedRule(rs, params, where); ok {
			return rule
		}
	}
	return opaque(jr.RuleKey, params)
}

func opaque(key string, params map[string]string) spec.Rule {
	return spec.Rule{Type: spec.RuleOpaque, Params: []spec.Param{
		{Key: "key", Kind: spec.KindString, Str: key},
		{Key: "parameters", Kind: spec.KindMap, Map: sortedKV(params)},
	}}
}

func (r *Reader) typedRule(rs spec.RuleSpec, params map[string]string, where string) (spec.Rule, bool) {
	rest := map[string]string{}
	for k, v := range params {
		rest[k] = v
	}
	for k, want := range rs.Fixed {
		if got, ok := rest[k]; !ok || got != want {
			return spec.Rule{}, false
		}
		delete(rest, k)
	}
	out := spec.Rule{Type: rs.Type}
	for _, ps := range rs.Params {
		v, present := rest[ps.Jira]
		delete(rest, ps.Jira)
		switch {
		case !present && ps.Required:
			return spec.Rule{}, false // not the shape this typed rule expects
		case !present, v == "" && !ps.Required:
			continue // optional and unset
		}
		p, ok := r.convert(ps, v, where)
		if !ok {
			return spec.Rule{}, false
		}
		if ps.Conv == spec.ConvBool && p.Str == "false" {
			continue // false is every flag's default, and Jira fills it in, so it is left out
		}
		out.Params = append(out.Params, p)
	}
	if len(rest) > 0 {
		out.Params = append(out.Params, spec.Param{Key: "extra", Kind: spec.KindMap, Map: sortedKV(rest)})
	}
	return out, true
}

// convert turns one Jira parameter value into a typed parameter.
func (r *Reader) convert(ps spec.ParamSpec, v, where string) (spec.Param, bool) {
	p := spec.Param{Key: ps.Key, Kind: ps.Kind}
	switch ps.Conv {
	case spec.ConvPlain:
		p.Str = r.resolve(ps.Ref, v, where)
	case spec.ConvBool:
		if v != "true" && v != "false" {
			return p, false
		}
		p.Str = v
	case spec.ConvEnum:
		typed, ok := ps.Values[v]
		if !ok {
			return p, false
		}
		p.Str = typed
	case spec.ConvCSV:
		for _, item := range strings.Split(v, ",") {
			if item = strings.TrimSpace(item); item != "" {
				p.List = append(p.List, r.resolve(ps.Ref, item, where))
			}
		}
	case spec.ConvJSONList:
		if err := json.Unmarshal([]byte(v), &p.List); err != nil {
			return p, false
		}
	default:
		return p, false
	}
	return p, true
}

// resolve turns an ID into a name. Unresolvable IDs are kept as id:<value>
// and noted, so the spec stays complete and the gap is visible.
func (r *Reader) resolve(kind, id, where string) string {
	var fn resolver
	switch kind {
	case spec.RefNone, spec.RefUser:
		return id // users are referenced by account ID throughout
	case spec.RefField:
		fn = func(id string) (string, bool) {
			if _, ok := r.siteField(id); ok {
				return r.fieldName(id), true
			}
			return "", false
		}
	case spec.RefStatus:
		fn = func(id string) (string, bool) { n, ok := r.statusNames[id]; return n, ok }
	case spec.RefRole:
		fn = r.roleName
	case spec.RefGroup:
		fn = r.groupName
	}
	if fn != nil {
		if name, ok := fn(id); ok {
			return name
		}
	}
	r.note("%s refers to %s %s, which could not be resolved to a name; it is kept as id:%s.", where, kind, id, id)
	return "id:" + id
}

// roleName names a role type by ID.
func (r *Reader) roleName(id string) (string, bool) {
	types, err := r.loadRoleTypes()
	if err != nil {
		r.lookupFailed(fmt.Errorf("reading role types to name them: %w", err))
	}
	for _, t := range types {
		if strconv.Itoa(t.ID) == id {
			return t.Name, true
		}
	}
	return "", false
}

// roleType is a role of the site, with its default members.
type roleType struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       *struct {
		Type string `json:"type"`
	} `json:"scope"` // set for team-managed spaces' own roles
	Actors []roleActor `json:"actors"` // the role's default members
}

// loadRoleTypes reads the site's role types on first use.
func (r *Reader) loadRoleTypes() ([]roleType, error) {
	if r.roleTypesRead {
		return r.roleTypes, nil
	}
	var all []roleType
	if err := r.get("/rest/api/3/role", nil, &all); err != nil {
		return nil, err
	}
	r.roleTypes, r.roleTypesRead = all, true
	return all, nil
}

// groupName looks one group up by ID on first use.
func (r *Reader) groupName(id string) (string, bool) {
	if r.groupNames == nil {
		r.groupNames = map[string]string{}
	}
	if n, ok := r.groupNames[id]; ok {
		return n, n != ""
	}
	var page struct {
		Values []struct {
			GroupID string `json:"groupId"`
			Name    string `json:"name"`
		} `json:"values"`
	}
	if err := r.get("/rest/api/3/group/bulk", map[string][]string{"groupId": {id}}, &page); err != nil {
		r.lookupFailed(fmt.Errorf("reading group %s to name it: %w", id, err))
	}
	r.groupNames[id] = ""
	for _, g := range page.Values {
		r.groupNames[g.GroupID] = g.Name
	}
	n := r.groupNames[id]
	return n, n != ""
}
