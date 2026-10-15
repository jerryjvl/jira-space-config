package steps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/reader"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// The writes that create shareable objects: screens, field contexts and
// workflows. Names in the spec become IDs here, through the reader of the
// evaluation the write belongs to, so objects earlier steps created in the
// same run are found.

// resolver turns the spec's names into Jira's IDs.
type resolver struct {
	r    *reader.Reader
	spec *spec.Spec
}

func (x resolver) field(name string) (string, error) {
	if after, ok := strings.CutPrefix(name, "id:"); ok {
		return after, nil
	}
	names, err := x.r.FieldNames()
	if err != nil {
		return "", err
	}
	found := names[name]
	if len(found) > 1 {
		for _, f := range x.spec.CustomFields {
			if f.Name == name && f.ID != "" && slices.Contains(found, f.ID) {
				return f.ID, nil
			}
		}
		return "", fmt.Errorf("%d fields on the site are named %q; give its id in custom_fields", len(found), name)
	}
	if len(found) == 0 {
		return "", fmt.Errorf("no field on the site is named %q", name)
	}
	return found[0], nil
}

func (x resolver) status(name string) (spec.Status, error) {
	if after, ok := strings.CutPrefix(name, "id:"); ok {
		all, err := x.r.StatusesByID([]string{after})
		if err != nil || len(all) == 0 {
			return spec.Status{}, errors.Join(err, fmt.Errorf("no status has id %s", after))
		}
		return all[0], nil
	}
	all, err := x.r.FindStatuses(name)
	if err != nil {
		return spec.Status{}, err
	}
	for _, s := range all {
		if s.Name == name {
			return s, nil
		}
	}
	return spec.Status{}, fmt.Errorf("no status on the site is named %q", name)
}

func (x resolver) role(name string) (string, error) {
	all, err := x.r.SiteRoleTypes()
	if err != nil {
		return "", err
	}
	var found []string
	for _, rt := range all {
		if rt.Name == name {
			found = append(found, rt.ID)
		}
	}
	switch {
	case len(found) == 1:
		return found[0], nil
	case len(found) == 0:
		return "", fmt.Errorf("no role type on the site is named %q", name)
	}
	// Several role types may share a name: the spec's ID says which.
	for _, r := range x.spec.Roles {
		if r.Name == name && r.ID != "" && slices.Contains(found, r.ID) {
			return r.ID, nil
		}
	}
	return "", fmt.Errorf("%d role types on the site are named %q; give its id in roles", len(found), name)
}

func (x resolver) group(name string) (string, error) {
	all, err := x.r.FindGroups([]string{name}, nil)
	if err != nil {
		return "", err
	}
	for _, g := range all {
		if g.Name == name {
			return g.ID, nil
		}
	}
	return "", fmt.Errorf("no group on the site is named %q", name)
}

func (x resolver) resolution(name string) (string, error) {
	all, err := x.r.Resolutions()
	if err != nil {
		return "", err
	}
	for _, r := range all {
		if r.Name == name {
			return r.ID, nil
		}
	}
	return "", fmt.Errorf("no resolution on the site is named %q", name)
}

// ref turns one referenced name into an ID, by what it refers to.
func (x resolver) ref(kind, name string) (string, error) {
	if after, ok := strings.CutPrefix(name, "id:"); ok {
		return after, nil
	}
	switch kind {
	case spec.RefField:
		return x.field(name)
	case spec.RefStatus:
		s, err := x.status(name)
		return s.ID, err
	case spec.RefRole:
		return x.role(name)
	case spec.RefGroup:
		return x.group(name)
	}
	return name, nil // users by account ID; plain values as they are
}

// createScreen creates a screen, names its first tab (a new screen has
// one), adds the other tabs and the fields.
func createScreen(x resolver, s spec.Screen) action {
	return func(ctx context.Context, snd Sender) (string, error) {
		var fieldIDs [][]string
		for _, t := range s.Tabs {
			var tab []string
			for _, f := range t.Fields {
				id, err := x.field(f)
				if err != nil {
					return "", err
				}
				tab = append(tab, id)
			}
			fieldIDs = append(fieldIDs, tab)
		}
		var screen struct {
			ID int `json:"id"`
		}
		if err := snd.Send(ctx, http.MethodPost, "/rest/api/3/screens", nil,
			map[string]string{"name": s.Name, "description": managedDescription(s.Description)}, &screen); err != nil {
			return "", err
		}
		base := "/rest/api/3/screens/" + strconv.Itoa(screen.ID) + "/tabs"
		var tabs []struct {
			ID int `json:"id"`
		}
		if err := snd.Send(ctx, http.MethodGet, base, nil, nil, &tabs); err != nil {
			return "", err
		}
		for i, t := range s.Tabs {
			var tab struct {
				ID int `json:"id"`
			}
			switch {
			case i == 0 && len(tabs) > 0:
				tab.ID = tabs[0].ID
				if err := snd.Send(ctx, http.MethodPut, base+"/"+strconv.Itoa(tab.ID), nil, map[string]string{"name": t.Name}, nil); err != nil {
					return "", err
				}
			default:
				if err := snd.Send(ctx, http.MethodPost, base, nil, map[string]string{"name": t.Name}, &tab); err != nil {
					return "", err
				}
			}
			for _, f := range fieldIDs[i] {
				if err := snd.Send(ctx, http.MethodPost, base+"/"+strconv.Itoa(tab.ID)+"/fields", nil, map[string]string{"fieldId": f}, nil); err != nil {
					return "", err
				}
			}
		}
		return strconv.Itoa(screen.ID), nil
	}
}

// contextName is the name of a new context: the binding's, or one made
// from the managed prefix (or the space key) and the field.
func contextName(env *Env, field string, c spec.FieldContext) string {
	if c.Binding != nil && c.Binding.Name != "" {
		return c.Binding.Name
	}
	if p := env.Spec.Space.ManagedPrefix; p != "" {
		return p + " " + field
	}
	return field + " (" + env.Spec.Space.Key + ")"
}

// createContexts creates a field's contexts for the space, with their
// options and defaults.
func createContexts(env *Env, x resolver, field string, contexts []spec.FieldContext) action {
	project := env.P.ID
	return func(ctx context.Context, snd Sender) (string, error) {
		fid, err := x.field(field)
		if err != nil {
			return "", err
		}
		var created []string
		for _, c := range contexts {
			body := map[string]any{"name": contextName(env, field, c), "description": managedDescription(spec.Opt[string]{}),
				"projectIds": []string{project}}
			if c.IssueTypes != nil {
				var types []string
				for _, name := range c.IssueTypes {
					id, err := env.knownIssueTypeID(name)
					if err != nil {
						return "", err
					}
					types = append(types, id)
				}
				body["issueTypeIds"] = types
			}
			var out struct {
				ID string `json:"id"`
			}
			if err := snd.Send(ctx, http.MethodPost, "/rest/api/3/field/"+fid+"/context", nil, body, &out); err != nil {
				return "", err
			}
			optionIDs, err := createOptions(ctx, snd, fid, out.ID, c.Options)
			if err != nil {
				return "", err
			}
			if c.Default.Has() {
				def, err := defaultValue(out.ID, c.Default.V, optionIDs, env.customFieldType(field))
				if err != nil {
					return "", err
				}
				if err := snd.Send(ctx, http.MethodPut, "/rest/api/3/field/"+fid+"/context/defaultValue", nil,
					map[string]any{"defaultValues": []any{def}}, nil); err != nil {
					return "", err
				}
			}
			created = append(created, out.ID)
		}
		return strings.Join(created, ", "), nil
	}
}

// createOptions adds a context's options, then each one's children, and
// returns the IDs by "value" and "parent/child".
func createOptions(ctx context.Context, snd Sender, fid, cid string, options []spec.Option) (map[string]string, error) {
	out := map[string]string{}
	if len(options) == 0 {
		return out, nil
	}
	path := "/rest/api/3/field/" + fid + "/context/" + cid + "/option"
	type option struct {
		Value    string `json:"value"`
		Disabled bool   `json:"disabled"`
		OptionID string `json:"optionId,omitempty"`
	}
	var answer struct {
		Options []struct {
			ID    string `json:"id"`
			Value string `json:"value"`
		} `json:"options"`
	}
	var top []option
	for _, o := range options {
		top = append(top, option{Value: o.Value, Disabled: o.Disabled})
	}
	if err := snd.Send(ctx, http.MethodPost, path, nil, map[string]any{"options": top}, &answer); err != nil {
		return nil, err
	}
	for _, a := range answer.Options {
		out[a.Value] = a.ID
	}
	for _, o := range options {
		if len(o.Children) == 0 {
			continue
		}
		var children []option
		for _, c := range o.Children {
			children = append(children, option{Value: c.Value, Disabled: c.Disabled, OptionID: out[o.Value]})
		}
		answer.Options = nil
		if err := snd.Send(ctx, http.MethodPost, path, nil, map[string]any{"options": children}, &answer); err != nil {
			return nil, err
		}
		for _, a := range answer.Options {
			out[o.Value+"/"+a.Value] = a.ID
		}
	}
	return out, nil
}

// defaultValue is a context default in Jira's form, for the field types
// whose defaults the format describes.
func defaultValue(cid string, v spec.Value, options map[string]string, fieldType string) (map[string]any, error) {
	d := map[string]any{"contextId": cid}
	option := func(value string) (string, error) {
		if id, ok := options[value]; ok {
			return id, nil
		}
		return "", fmt.Errorf("the default %q is not one of the options", value)
	}
	var err error
	switch fieldType {
	case "select", "radio":
		d["type"] = "option.single"
		d["optionId"], err = option(v.Str)
	case "multiselect", "checkboxes":
		d["type"] = "option.multiple"
		var list []string
		for _, s := range v.List {
			id, e := option(s)
			err = errors.Join(err, e)
			list = append(list, id)
		}
		d["optionIds"] = list
	case "cascading":
		d["type"] = "option.cascading"
		if len(v.List) > 0 {
			d["optionId"], err = option(v.List[0])
		}
		if len(v.List) > 1 {
			var e error
			d["cascadingOptionId"], e = option(v.List[0] + "/" + v.List[1])
			err = errors.Join(err, e)
		}
	case "text", "paragraph":
		d["type"] = map[string]string{"text": "textfield", "paragraph": "textarea"}[fieldType]
		d["text"] = v.Str
	case "number":
		d["type"] = "float"
		var n float64
		n, err = strconv.ParseFloat(v.Str, 64)
		d["number"] = n
	case "url":
		d["type"], d["url"] = "url", v.Str
	case "labels":
		d["type"], d["labels"] = "labels", v.List
	case "date", "datetime":
		d["type"] = map[string]string{"date": "datepicker", "datetime": "datetimepicker"}[fieldType]
		switch {
		case v.Str == "current":
			d["useCurrent"] = true
		case fieldType == "date":
			d["date"] = v.Str
		default:
			d["dateTime"] = v.Str
		}
	case "user":
		// Jira requires the user filter; it is off, as the format has none (export notes one).
		d["type"], d["accountId"] = "single.user.select", strings.TrimPrefix(v.Str, "user:")
		d["userFilter"] = map[string]bool{"enabled": false}
	case "multiuser":
		var ids []string
		for _, u := range v.List {
			ids = append(ids, strings.TrimPrefix(u, "user:"))
		}
		d["type"], d["accountIds"] = "multi.user.select", ids
	default:
		return nil, fmt.Errorf("the format cannot set a default for a field of type %q; set it on the field's context in Jira by hand", fieldType)
	}
	return d, err
}

// customFieldType is the short type of a custom field of the site.
func (e *Env) customFieldType(name string) string {
	for _, f := range e.Spec.CustomFields {
		if f.Name == name && f.Type != "" {
			return f.Type
		}
	}
	all, err := e.R.SiteCustomFields()
	if err != nil {
		return ""
	}
	for _, f := range all {
		if f.Name == name {
			return f.Type
		}
	}
	return ""
}

// jiraRule is a rule in Jira's form.
type jiraRule struct {
	RuleKey    string            `json:"ruleKey"`
	Parameters map[string]string `json:"parameters"`
}

// ruleToJira writes a rule back in Jira's form: typed rules through the
// registry, opaque ones as they were read.
func ruleToJira(x resolver, list string, r spec.Rule) (jiraRule, error) {
	if r.Type == spec.RuleOpaque {
		out := jiraRule{Parameters: map[string]string{}}
		for _, p := range r.Params {
			switch p.Key {
			case "key":
				out.RuleKey = p.Str
			case "parameters":
				for _, kv := range p.Map {
					out.Parameters[kv.Key] = kv.Value
				}
			}
		}
		return out, nil
	}
	rs, ok := spec.RuleSpecFor(list, r.Type)
	if !ok {
		return jiraRule{}, fmt.Errorf("no %s rule is of type %q", list, r.Type)
	}
	out := jiraRule{RuleKey: rs.RuleKey, Parameters: map[string]string{}}
	for k, v := range rs.Fixed {
		out.Parameters[k] = v
	}
	for _, p := range r.Params {
		if p.Key == "extra" {
			for _, kv := range p.Map {
				out.Parameters[kv.Key] = kv.Value
			}
			continue
		}
		i := slices.IndexFunc(rs.Params, func(ps spec.ParamSpec) bool { return ps.Key == p.Key })
		if i < 0 {
			return jiraRule{}, fmt.Errorf("rule %s has no parameter %q", r.Type, p.Key)
		}
		ps := rs.Params[i]
		var err error
		switch ps.Conv {
		case spec.ConvPlain:
			out.Parameters[ps.Jira], err = x.ref(ps.Ref, p.Str)
		case spec.ConvBool:
			out.Parameters[ps.Jira] = p.Str
		case spec.ConvEnum:
			for jira, typed := range ps.Values {
				if typed == p.Str {
					out.Parameters[ps.Jira] = jira
				}
			}
		case spec.ConvCSV:
			var parts []string
			for _, item := range p.List {
				id, e := x.ref(ps.Ref, item)
				err = errors.Join(err, e)
				parts = append(parts, id)
			}
			out.Parameters[ps.Jira] = strings.Join(parts, ",")
		case spec.ConvJSONList:
			data, _ := json.Marshal(p.List)
			out.Parameters[ps.Jira] = string(data)
		}
		if err != nil {
			return jiraRule{}, err
		}
	}
	return out, nil
}

// conditionGroup writes a condition tree in Jira's form.
func conditionGroup(x resolver, c spec.Condition) (map[string]any, error) {
	group := map[string]any{"operation": strings.ToUpper(c.Op), "conditions": []jiraRule{}, "conditionGroups": []any{}}
	var rules []jiraRule
	var groups []any
	for _, item := range c.Items {
		if item.Rule != nil {
			r, err := ruleToJira(x, spec.ListConditions, *item.Rule)
			if err != nil {
				return nil, err
			}
			rules = append(rules, r)
			continue
		}
		g, err := conditionGroup(x, item)
		if err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	if rules != nil {
		group["conditions"] = rules
	}
	if groups != nil {
		group["conditionGroups"] = groups
	}
	return group, nil
}

// workflowRequest is a workflow in the form of Jira's bulk create API, with
// statuses referenced by their IDs.
func workflowRequest(env *Env, x resolver, w spec.Workflow) (map[string]any, error) {
	var statuses []map[string]string
	var layouts []map[string]any
	ref := map[string]string{} // status name → reference
	for _, s := range w.Statuses {
		st, err := x.status(s.Name)
		if err != nil {
			return nil, err
		}
		ref[s.Name] = st.ID
		// Jira updates the statuses a workflow create lists, so each is sent as
		// it is, its description included, or Jira would clear it.
		statuses = append(statuses, statusEntry(st))
		props := map[string]string{}
		for _, kv := range s.Properties {
			props[kv.Key] = kv.Value
		}
		l := map[string]any{"statusReference": st.ID, "properties": props}
		if s.Layout != nil {
			l["layout"] = map[string]float64{"x": s.Layout.X, "y": s.Layout.Y}
		}
		layouts = append(layouts, l)
	}
	var transitions []map[string]any
	expanded := expandLoops(w.Transitions)
	transitionIDs := transitionIDs(expanded)
	for i, t := range expanded {
		tr := map[string]any{"id": transitionIDs[i], "name": t.Name, "type": strings.ToUpper(t.Type), "properties": map[string]string{}}
		if t.Description.Has() {
			tr["description"] = t.Description.V
		}
		tr["toStatusReference"] = ref[t.To]
		var links []map[string]string
		for _, f := range t.From {
			links = append(links, map[string]string{"fromStatusReference": ref[f]})
		}
		if links != nil {
			tr["links"] = links
		}
		if t.Screen.Has() {
			found, err := env.R.FindScreens(t.Screen.V)
			if err != nil {
				return nil, err
			}
			i := slices.IndexFunc(found, func(f reader.Found) bool { return f.Name == t.Screen.V })
			if i < 0 {
				return nil, fmt.Errorf("no screen on the site is named %q", t.Screen.V)
			}
			tr["transitionScreen"] = jiraRule{RuleKey: "system:transition-screen", Parameters: map[string]string{"screenId": found[i].ID}}
		}
		if t.Event.Has() {
			id, err := x.event(t.Event.V)
			if err != nil {
				return nil, err
			}
			tr["customIssueEventId"] = id
		}
		if t.Conditions != nil {
			g, err := conditionGroup(x, *t.Conditions)
			if err != nil {
				return nil, err
			}
			tr["conditions"] = g
		}
		for key, rules := range map[string][]spec.Rule{"validators": t.Validators, "actions": t.PostFunctions, "triggers": t.Triggers} {
			list := map[string]string{"validators": spec.ListValidators, "actions": spec.ListPostFunctions, "triggers": spec.ListTriggers}[key]
			var out []jiraRule
			for _, r := range rules {
				jr, err := ruleToJira(x, list, r)
				if err != nil {
					return nil, err
				}
				out = append(out, jr)
			}
			if out != nil {
				tr[key] = out
			}
		}
		props := tr["properties"].(map[string]string)
		for _, p := range t.Properties {
			if !p.IsList {
				props[p.Key] = p.Value
				continue
			}
			var parts []string
			for _, name := range p.List {
				id, err := x.resolution(name)
				if err != nil {
					return nil, err
				}
				parts = append(parts, id)
			}
			props[p.Key] = strings.Join(parts, ",")
		}
		transitions = append(transitions, tr)
	}
	description := managedDescription(w.Description)
	return map[string]any{
		"scope":    map[string]string{"type": "GLOBAL"},
		"statuses": statuses,
		"workflows": []map[string]any{{"name": w.Name, "description": description, "statuses": layouts,
			"transitions": transitions}},
	}, nil
}

// createWorkflow has Jira validate the workflow, then creates it.
func createWorkflow(env *Env, x resolver, w spec.Workflow) action {
	return func(ctx context.Context, snd Sender) (string, error) {
		payload, err := workflowRequest(env, x, w)
		if err != nil {
			return "", err
		}
		if err := validateWorkflow(ctx, snd, "/rest/api/3/workflows/create/validation", payload); err != nil {
			return "", err
		}
		var out struct {
			Workflows []struct {
				ID string `json:"id"`
			} `json:"workflows"`
		}
		if err := snd.Send(ctx, http.MethodPost, "/rest/api/3/workflows/create", nil, payload, &out); err != nil {
			return "", err
		}
		if len(out.Workflows) != 1 {
			return "", errors.New("the answer does not say which workflow was created")
		}
		return out.Workflows[0].ID, nil
	}
}

// transitionIDs are the IDs of a new workflow's transitions: the spec's
// where it gives one, otherwise the next free in Jira's classic numbering
// (1, 11, 21, ...). Jira requires them.
// expandLoops turns each looped transition into ordinary ones that return to
// their own status, one per from status: Jira answers a transition with no
// target with a 500, and one target cannot serve several statuses.
func expandLoops(ts []spec.Transition) []spec.Transition {
	var out []spec.Transition
	for _, t := range ts {
		if !t.Looped {
			out = append(out, t)
			continue
		}
		for i, from := range t.From {
			c := t
			c.Looped, c.From, c.To = false, []string{from}, from
			if i > 0 {
				c.ID = ""
			}
			out = append(out, c)
		}
	}
	return out
}

func transitionIDs(ts []spec.Transition) []string {
	used := map[string]bool{}
	for _, t := range ts {
		used[t.ID] = t.ID != ""
	}
	out := make([]string, len(ts))
	next := 1
	for i, t := range ts {
		if t.ID != "" {
			out[i] = t.ID
			continue
		}
		for out[i] == "" || used[out[i]] {
			out[i] = strconv.Itoa(next)
			next = next/10*10 + 11
		}
		used[out[i]] = true
	}
	return out
}

// jiraDescription is an object's description as Jira holds it: with the
// tool's marker when the tool created it (the reader takes it off).
func jiraDescription(g spec.GlobalObject) string {
	if g.Mode == spec.ModeEnsure || g.Mode == spec.ModeManaged {
		return managedDescription(g.Description)
	}
	if g.Description.Has() {
		return g.Description.V
	}
	return ""
}

// validateWorkflow has Jira check a workflow create or update before it is
// sent; the check changes nothing.
func validateWorkflow(ctx context.Context, snd Sender, path string, payload map[string]any) error {
	var check struct {
		Errors []struct {
			Level   string `json:"level"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := snd.Send(ctx, http.MethodPost, path, nil,
		map[string]any{"payload": payload, "validationOptions": map[string]any{"levels": []string{"ERROR"}}}, &check); err != nil {
		return err
	}
	if len(check.Errors) > 0 {
		var msgs []string
		for _, e := range check.Errors {
			msgs = append(msgs, e.Message)
		}
		return fmt.Errorf("validating the workflow, Jira found: %s", strings.Join(msgs, "; "))
	}
	return nil
}

// statusEntry is a status as a workflow create or update lists it: as Jira
// holds it, description and marker included, as Jira updates the statuses
// it is sent.
func statusEntry(st spec.Status) map[string]string {
	return map[string]string{"id": st.ID, "name": st.Name, "statusReference": st.ID,
		"statusCategory": statusCategoryNames[st.Category.V], "description": jiraDescription(st.GlobalObject)}
}

// defaultTypes are the default value types Jira uses, by field type.
var defaultTypes = map[string]string{"select": "option.single", "radio": "option.single", "multiselect": "option.multiple",
	"checkboxes": "option.multiple", "cascading": "option.cascading", "text": "textfield", "paragraph": "textarea",
	"number": "float", "url": "url", "labels": "labels", "date": "datepicker", "datetime": "datetimepicker",
	"user": "single.user.select", "multiuser": "multi.user.select"}

// noDefault is a context with its default taken away, in Jira's form: the
// type with no value, as Jira gives a context without a default.
func noDefault(cid, fieldType string) (map[string]any, error) {
	t, ok := defaultTypes[fieldType]
	if !ok {
		return nil, fmt.Errorf("taking the default of a field of type %q away is not supported", fieldType)
	}
	d := map[string]any{"contextId": cid, "type": t}
	if t == "single.user.select" {
		d["userFilter"] = map[string]bool{"enabled": false}
	}
	return d, nil
}
