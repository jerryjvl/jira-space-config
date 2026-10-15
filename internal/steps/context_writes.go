package steps

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// Changes to a field's contexts that this space alone uses: options added, removed, enabled or disabled and put in
// order; the default set or taken away; issue types added and taken away;
// and contexts the spec leaves out deleted.

// contextChange plans changing a field's contexts to the spec's, and says
// whether it can: not when a context it would change is shared. A spec's
// context is matched to Jira's by its binding's ID, or else by the issue
// types it covers. Removals (options, issue types, contexts) need
// --allow-delete=contexts, and are blocked while work items they affect
// have a value.
func contextChange(env *Env, cf spec.CustomField, want, got spec.FieldContextSet) (change, bool) {
	ch := change{class: "additive", allow: "contexts"}
	var missing []spec.FieldContext
	type pair struct{ w, g spec.FieldContext }
	var pairs []pair
	used := map[int]bool{}
	isSpace := func(c spec.FieldContext) bool { return c.Binding != nil && c.Binding.Scope == spec.ScopeSpace }
	for _, w := range want.Contexts {
		i := -1
		if w.Binding != nil && w.Binding.ID != "" && !env.IgnoreIDs {
			i = slices.IndexFunc(got.Contexts, func(g spec.FieldContext) bool { return g.Binding != nil && g.Binding.ID == w.Binding.ID })
		}
		if i < 0 {
			i = slices.IndexFunc(got.Contexts, func(g spec.FieldContext) bool { return contextKey(g) == contextKey(w) })
		}
		switch {
		case i < 0:
			missing = append(missing, w)
		case !isSpace(got.Contexts[i]):
			return change{}, false
		default:
			used[i] = true
			pairs = append(pairs, pair{w, got.Contexts[i]})
		}
	}
	field := strings.TrimPrefix(cf.ID, "customfield_")
	valued := func(types []string, why, fix string) {
		if types == nil {
			ch.safety = append(ch.safety, safety{name: fmt.Sprintf("field %q", want.Field),
				jql: fmt.Sprintf("project = %s AND cf[%s] is not EMPTY", env.P.ID, field), summary: why, fix: fix})
			return
		}
		for _, t := range types {
			ch.safety = append(ch.safety, safety{name: fmt.Sprintf("field %q (issue type %q)", want.Field, t),
				jql:     fmt.Sprintf("project = %s AND issuetype = %s AND cf[%s] is not EMPTY", env.P.ID, jqlString(t), field),
				summary: why, fix: fix})
		}
	}
	var extra []spec.FieldContext // the space's contexts the spec leaves out
	for i, g := range got.Contexts {
		if used[i] {
			continue
		}
		if !isSpace(g) {
			return change{}, false
		}
		extra = append(extra, g)
		ch.removes = append(ch.removes, "the context for "+contextKey(g))
		valued(g.IssueTypes, "have a value in this field from a context the spec leaves out",
			"Clear the field on those work items first, or keep the context in the spec.")
	}
	type cover struct {
		context     string
		add, remove []string
	}
	var covers []cover
	for _, p := range pairs {
		if contextKey(p.w) == contextKey(p.g) {
			continue
		}
		c := cover{context: p.g.Binding.ID}
		switch {
		case p.g.IssueTypes == nil: // from all issue types to some: the others leave
			c.add = p.w.IssueTypes
			valued(nil, "have a value in this field, which could come from another context once the context covers fewer issue types",
				"Clear the field on those work items first, or keep the context for all issue types.")
		case p.w.IssueTypes == nil: // from some to all: the others join
			c.remove = p.g.IssueTypes
			ch.removes = append(ch.removes, "the context's restriction to "+contextKey(p.g))
			valued(nil, "have a value in this field, which could come from another context once the context covers every issue type",
				"Clear the field on those work items first, or keep the context's issue types.")
		default:
			for _, t := range p.w.IssueTypes {
				if !slices.Contains(p.g.IssueTypes, t) {
					c.add = append(c.add, t)
				}
			}
			for _, t := range p.g.IssueTypes {
				if !slices.Contains(p.w.IssueTypes, t) {
					c.remove = append(c.remove, t)
					ch.removes = append(ch.removes, fmt.Sprintf("issue type %q from a context", t))
				}
			}
			// An issue type that moves to another context keeps a value only if
			// that context has the option, so any value blocks the move.
			valued(append(slices.Clone(c.add), c.remove...), "have a value in this field, which would come from another context",
				"Clear the field on those work items first, or keep the context's issue types.")
		}
		if ch.class == "additive" {
			ch.class = "altering"
		}
		covers = append(covers, c)
	}
	var requests []string
	for range extra {
		requests = append(requests, "DELETE /rest/api/3/field/{fieldId}/context/{contextId}")
	}
	for _, c := range covers {
		if c.remove != nil {
			requests = append(requests, "POST /rest/api/3/field/{fieldId}/context/{contextId}/issuetype/remove")
		}
		if c.add != nil {
			requests = append(requests, "PUT /rest/api/3/field/{fieldId}/context/{contextId}/issuetype")
		}
	}
	if len(missing) > 0 {
		requests = append(requests, contextRequests...)
	}
	var plans []optionPlan
	for _, p := range pairs {
		plan := planOptions(p.w.Options, p.g.Options)
		plan.context = p.g.Binding.ID
		wantDefault, gotDefault := p.w.Default.Has(), p.g.Default.Has()
		if wantDefault && (!gotDefault || !sameValue(p.w.Default.V, p.g.Default.V)) {
			plan.setDefault = &p.w.Default.V
		}
		plan.clearDefault = !wantDefault && gotDefault
		for _, r := range plan.remove {
			ch.removes = append(ch.removes, "option "+quote(r))
			parent, child, isChild := strings.Cut(r, "/")
			jql := fmt.Sprintf("project = %s AND cf[%s] = %s", env.P.ID, field, jqlString(r))
			if isChild {
				jql = fmt.Sprintf("project = %s AND cf[%s] in cascadeOption(%s, %s)", env.P.ID, field, jqlString(parent), jqlString(child))
			}
			ch.safety = append(ch.safety, safety{name: fmt.Sprintf("option %q (field %q)", r, want.Field), jql: jql,
				summary: "have this value, which the context would lose", fix: "Change their value first, or keep the option."})
		}
		if len(plan.toggle) > 0 || plan.reorder || plan.setDefault != nil || plan.clearDefault {
			ch.class = "altering"
		}
		requests = append(requests, plan.requests()...)
		plans = append(plans, plan)
	}
	if len(ch.removes) > 0 {
		ch.class = "removing"
	}
	ch.requests = countRequests(requests)
	x := resolver{env.R, env.Spec}
	ch.do = func(ctx context.Context, snd Sender) (string, error) {
		// Contexts and issue types leave first: Jira lets one context cover an
		// issue type in a space.
		base := "/rest/api/3/field/" + cf.ID + "/context/"
		for _, g := range extra {
			if err := snd.Send(ctx, http.MethodDelete, base+g.Binding.ID, nil, nil, nil); err != nil {
				return "", err
			}
		}
		for _, c := range covers {
			if c.remove == nil {
				continue
			}
			ids, err := env.issueTypeIDs(c.remove)
			if err != nil {
				return "", err
			}
			if err := snd.Send(ctx, http.MethodPost, base+c.context+"/issuetype/remove", nil, map[string][]string{"issueTypeIds": ids}, nil); err != nil {
				return "", err
			}
		}
		for _, c := range covers {
			if c.add == nil {
				continue
			}
			ids, err := env.issueTypeIDs(c.add)
			if err != nil {
				return "", err
			}
			if err := snd.Send(ctx, http.MethodPut, base+c.context+"/issuetype", nil, map[string][]string{"issueTypeIds": ids}, nil); err != nil {
				return "", err
			}
		}
		if len(missing) > 0 {
			if _, err := createContexts(env, x, want.Field, missing)(ctx, snd); err != nil {
				return "", err
			}
		}
		for _, p := range plans {
			if err := p.carryOut(ctx, snd, cf.ID, env.customFieldType(want.Field)); err != nil {
				return "", err
			}
		}
		return cf.ID, nil
	}
	return ch, true
}

// optionPlan is how one context's options change, by value: "Host" for an
// option, "Host/Linux" for a cascading child.
type optionPlan struct {
	context      string
	add          []spec.Option // with children only where the parent is new
	addChild     map[string][]spec.Option
	remove       []string
	toggle       map[string]bool // value → disabled
	reorder      bool
	order        []string            // the top-level values in the spec's order
	children     map[string][]string // parent → its children in the spec's order
	setDefault   *spec.Value
	clearDefault bool // the spec gives no default where Jira has one
}

func values(options []spec.Option) []string {
	var out []string
	for _, o := range options {
		out = append(out, o.Value)
	}
	return out
}

func planOptions(want, got []spec.Option) optionPlan {
	p := optionPlan{addChild: map[string][]spec.Option{}, toggle: map[string]bool{}, children: map[string][]string{}}
	p.order = values(want)
	find := func(l []spec.Option, v string) *spec.Option {
		for i := range l {
			if l[i].Value == v {
				return &l[i]
			}
		}
		return nil
	}
	kept := []string{}
	for _, g := range got {
		w := find(want, g.Value)
		if w == nil {
			p.remove = append(p.remove, g.Value)
			continue
		}
		kept = append(kept, g.Value)
		if w.Disabled != g.Disabled {
			p.toggle[g.Value] = w.Disabled
		}
		keptChildren := []string{}
		for _, gc := range g.Children {
			wc := find(w.Children, gc.Value)
			if wc == nil {
				p.remove = append(p.remove, g.Value+"/"+gc.Value)
				continue
			}
			keptChildren = append(keptChildren, gc.Value)
			if wc.Disabled != gc.Disabled {
				p.toggle[g.Value+"/"+gc.Value] = wc.Disabled
			}
		}
		for _, wc := range w.Children {
			if find(g.Children, wc.Value) == nil {
				p.addChild[g.Value] = append(p.addChild[g.Value], wc)
				keptChildren = append(keptChildren, wc.Value)
			}
		}
		if !slices.Equal(keptChildren, values(w.Children)) {
			p.reorder = true
		}
		p.children[g.Value] = values(w.Children)
	}
	for _, w := range want {
		if find(got, w.Value) == nil {
			p.add = append(p.add, w)
			kept = append(kept, w.Value)
		}
	}
	if !slices.Equal(kept, p.order) {
		p.reorder = true
	}
	return p
}

func (p optionPlan) requests() []string {
	path := "/rest/api/3/field/{fieldId}/context/{contextId}"
	var out []string
	if len(p.add) > 0 || len(p.addChild) > 0 {
		out = append(out, "POST "+path+"/option")
	}
	if len(p.toggle) > 0 {
		out = append(out, "PUT "+path+"/option")
	}
	for range p.remove {
		out = append(out, "DELETE "+path+"/option/{optionId}")
	}
	if p.reorder {
		out = append(out, "PUT "+path+"/option/move")
	}
	if p.setDefault != nil || p.clearDefault {
		out = append(out, "PUT /rest/api/3/field/{fieldId}/context/defaultValue")
	}
	return out
}

// carryOut makes the planned changes, with the option IDs read now.
func (p optionPlan) carryOut(ctx context.Context, snd Sender, fid, fieldType string) error {
	path := "/rest/api/3/field/" + fid + "/context/" + p.context + "/option"
	// Options are read only when the plan needs their IDs: a field without
	// options (a date, say) answers 400 "doesn't support options".
	ids := map[string]string{}
	_, optionDefault := map[string]bool{"select": true, "radio": true, "multiselect": true, "checkboxes": true, "cascading": true}[fieldType]
	if len(p.add)+len(p.addChild)+len(p.toggle)+len(p.remove) > 0 || p.reorder || p.setDefault != nil && optionDefault {
		var err error
		if ids, err = optionIDs(ctx, snd, path); err != nil {
			return err
		}
	}
	if len(p.add) > 0 {
		added, err := createOptions(ctx, snd, fid, p.context, p.add)
		if err != nil {
			return err
		}
		for k, v := range added {
			ids[k] = v
		}
	}
	for parent, children := range p.addChild {
		type option struct {
			Value    string `json:"value"`
			Disabled bool   `json:"disabled"`
			OptionID string `json:"optionId"`
		}
		var body []option
		for _, c := range children {
			body = append(body, option{c.Value, c.Disabled, ids[parent]})
		}
		var answer struct {
			Options []struct {
				ID    string `json:"id"`
				Value string `json:"value"`
			} `json:"options"`
		}
		if err := snd.Send(ctx, http.MethodPost, path, nil, map[string]any{"options": body}, &answer); err != nil {
			return err
		}
		for _, a := range answer.Options {
			ids[parent+"/"+a.Value] = a.ID
		}
	}
	if len(p.toggle) > 0 {
		var body []map[string]any
		for _, v := range sortedKeys(p.toggle) {
			value := v
			if _, child, ok := strings.Cut(v, "/"); ok {
				value = child
			}
			body = append(body, map[string]any{"id": ids[v], "value": value, "disabled": p.toggle[v]})
		}
		if err := snd.Send(ctx, http.MethodPut, path, nil, map[string]any{"options": body}, nil); err != nil {
			return err
		}
	}
	for _, v := range p.remove {
		id, ok := ids[v]
		if !ok {
			return fmt.Errorf("the context has no option %q now; it was changed since this step was evaluated", v)
		}
		if err := snd.Send(ctx, http.MethodDelete, path+"/"+id, nil, nil, nil); err != nil {
			return err
		}
	}
	if p.reorder {
		move := func(list []string) error {
			var l []string
			for _, v := range list {
				l = append(l, ids[v])
			}
			if len(l) == 0 {
				return nil
			}
			return snd.Send(ctx, http.MethodPut, path+"/move", nil, map[string]any{"customFieldOptionIds": l, "position": "First"}, nil)
		}
		if err := move(p.order); err != nil {
			return err
		}
		for _, parent := range p.order {
			var full []string
			for _, c := range p.children[parent] {
				full = append(full, parent+"/"+c)
			}
			if err := move(full); err != nil {
				return err
			}
		}
	}
	if p.clearDefault {
		def, err := noDefault(p.context, fieldType)
		if err != nil {
			return err
		}
		if err := snd.Send(ctx, http.MethodPut, "/rest/api/3/field/"+fid+"/context/defaultValue", nil,
			map[string]any{"defaultValues": []any{def}}, nil); err != nil {
			return err
		}
	}
	if p.setDefault != nil {
		def, err := defaultValue(p.context, *p.setDefault, ids, fieldType)
		if err != nil {
			return err
		}
		if err := snd.Send(ctx, http.MethodPut, "/rest/api/3/field/"+fid+"/context/defaultValue", nil,
			map[string]any{"defaultValues": []any{def}}, nil); err != nil {
			return err
		}
	}
	return nil
}

// optionIDs reads a context's options: their IDs by value, and by
// "parent/child" for cascading children.
func optionIDs(ctx context.Context, snd Sender, path string) (map[string]string, error) {
	type option struct {
		ID       string `json:"id"`
		Value    string `json:"value"`
		OptionID string `json:"optionId"`
	}
	all, err := pagedGet[option](ctx, snd, path, nil)
	if err != nil {
		return nil, err
	}
	byID := map[string]string{}
	for _, o := range all {
		if o.OptionID == "" {
			byID[o.ID] = o.Value
		}
	}
	out := map[string]string{}
	for _, o := range all {
		if o.OptionID == "" {
			out[o.Value] = o.ID
		} else {
			out[byID[o.OptionID]+"/"+o.Value] = o.ID
		}
	}
	return out, nil
}

func sortedKeys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func sameValue(a, b spec.Value) bool {
	return a.IsList == b.IsList && a.Str == b.Str && slices.Equal(a.List, b.List)
}
