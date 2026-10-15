package reader

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/spec"
	"github.com/jerryjvl/jira-space-config/internal/util"
)

// optionFieldTypes are the custom field types whose contexts carry options.
var optionFieldTypes = []string{"select", "multiselect", "radio", "checkboxes", "cascading"}

// contextOption is one option of a context; child options of a cascading
// field carry their parent's ID in OptionID.
type contextOption struct {
	ID       string `json:"id"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled"`
	OptionID string `json:"optionId"`
}

// FieldContexts reads, for each of the given custom fields, the contexts that
// apply to the space, where they say something: options or a default value.
// Fields with nothing to
// say are left out.
func (r *Reader) FieldContexts(p *Project, its *IssueTypes, fields []spec.CustomField) ([]spec.FieldContextSet, error) {
	var out []spec.FieldContextSet
	for _, cf := range fields {
		set, err := r.contextsOf(p, its, cf)
		if err != nil {
			return nil, fmt.Errorf("reading the contexts of field %q: %w", cf.Name, err)
		}
		if len(set.Contexts) > 0 {
			out = append(out, set)
		}
	}
	return out, r.done()
}

func (r *Reader) contextsOf(p *Project, its *IssueTypes, cf spec.CustomField) (spec.FieldContextSet, error) {
	set := spec.FieldContextSet{Field: cf.Name}

	// Which context applies to each (space, issue type) pair: one request
	// per field, with every issue type of the space.
	type pair struct {
		ProjectID   string `json:"projectId"`
		IssueTypeID string `json:"issueTypeId"`
	}
	body := struct {
		Mappings []pair `json:"mappings"`
	}{}
	for i := range its.Len() {
		body.Mappings = append(body.Mappings, pair{p.ID, its.ID(i)})
	}
	// Paged (50 pairs a page by default), so spaces with many issue types
	// are read in full.
	type contextFor struct {
		ContextID   string `json:"contextId"`
		IssueTypeID string `json:"issueTypeId"`
	}
	values, err := jira.PostReadAll[contextFor](r.ctx, r.c, "/rest/api/3/field/"+url.PathEscape(cf.ID)+"/context/mapping", body)
	if err != nil {
		return set, err
	}
	mapped := struct{ Values []contextFor }{values}
	var contextIDs []string
	issueTypesOf := map[string][]string{}   // context → names of the space's issue types it covers
	issueTypeIDsOf := map[string][]string{} // the same, as IDs
	for _, m := range mapped.Values {
		if m.ContextID == "" {
			continue // the field has no context for this issue type in the space
		}
		contextIDs = util.AppendNew(contextIDs, m.ContextID)
		if name, ok := its.Name(m.IssueTypeID); ok {
			issueTypesOf[m.ContextID] = append(issueTypesOf[m.ContextID], name)
			issueTypeIDsOf[m.ContextID] = append(issueTypeIDsOf[m.ContextID], m.IssueTypeID)
		}
	}
	if len(contextIDs) == 0 {
		return set, nil
	}

	defaults, err := r.contextDefaults(cf, contextIDs)
	if err != nil {
		return set, err
	}
	var options []contextOption
	for _, ctxID := range contextIDs {
		options = nil
		if slices.Contains(optionFieldTypes, cf.Type) {
			options, err = jira.GetAll[contextOption](r.ctx, r.c,
				"/rest/api/3/field/"+url.PathEscape(cf.ID)+"/context/"+url.PathEscape(ctxID)+"/option", nil)
			if err != nil {
				return set, err
			}
		}
		def, hasDefault := r.defaultValue(cf, defaults[ctxID], issueTypeIDsOf[ctxID], options)
		fc := spec.FieldContext{Options: optionTree(options)}
		if hasDefault {
			fc.Default = def
		}
		if len(issueTypesOf[ctxID]) < its.Len() {
			fc.IssueTypes = issueTypesOf[ctxID]
		}
		if fc.Binding, err = r.contextBinding(p, cf, ctxID); err != nil {
			return set, err
		}
		// A context that says nothing (no options, no default) is left out, unless
		// it is this space's own: that one limits the field to some issue types,
		// and a run that did not know it was there would make it again.
		if len(options) == 0 && !hasDefault && fc.Binding.Scope != spec.ScopeSpace {
			continue
		}
		set.Contexts = append(set.Contexts, fc)
	}
	return set, nil
}

// optionTree nests cascading children under their parents, in Jira's order.
func optionTree(options []contextOption) []spec.Option {
	var out []spec.Option
	index := map[string]int{}
	for _, o := range options {
		if o.OptionID == "" {
			index[o.ID] = len(out)
			out = append(out, spec.Option{Value: o.Value, Disabled: o.Disabled})
		}
	}
	for _, o := range options {
		if i, ok := index[o.OptionID]; ok && o.OptionID != "" {
			out[i].Children = append(out[i].Children, spec.Option{Value: o.Value, Disabled: o.Disabled})
		}
	}
	return out
}

// issueTypeDefault is one entry of GET .../context/defaultValues.
type issueTypeDefault struct {
	IsAnyIssueType bool            `json:"isAnyIssueType"`
	IssueTypeID    string          `json:"issueTypeId"`
	Value          json.RawMessage `json:"value"`
}

// contextDefaults reads default values. Only Jira's built-in field types
// support defaults (their type has a short name); for others Jira answers
// 400 "not supported", so they are not asked, and that answer is tolerated.
func (r *Reader) contextDefaults(cf spec.CustomField, contextIDs []string) (map[string][]issueTypeDefault, error) {
	if !slices.Contains(spec.FieldTypes, cf.Type) {
		return nil, nil
	}
	if cf.Type == "date" || cf.Type == "datetime" {
		// The newer endpoint gives "the current date" as a fixed date, the
		// moment it is read (docs/api/inventory.md); the older one does not.
		return r.contextDefaultsLegacy(cf, contextIDs)
	}
	type entry struct {
		ContextID     json.Number        `json:"contextId"`
		DefaultValues []issueTypeDefault `json:"defaultValues"`
	}
	entries, err := jira.GetAll[entry](r.ctx, r.c, "/rest/api/3/field/"+url.PathEscape(cf.ID)+"/context/defaultValues",
		url.Values{"contextId": contextIDs})
	var apiErr *jira.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusBadRequest && strings.Contains(apiErr.Error(), "not supported") {
		return nil, nil
	}
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden && strings.Contains(apiErr.Error(), "early access") {
		return r.contextDefaultsLegacy(cf, contextIDs)
	}
	if err != nil {
		return nil, err
	}
	out := map[string][]issueTypeDefault{}
	for _, en := range entries {
		out[en.ContextID.String()] = en.DefaultValues
	}
	return out, nil
}

// defaultValue converts a context's default. The format has one default per
// context; per-issue-type defaults within a context are noted, not exported.
func (r *Reader) defaultValue(cf spec.CustomField, entries []issueTypeDefault, issueTypeIDs []string,
	options []contextOption) (spec.Opt[spec.Value], bool) {
	var value json.RawMessage
	perType := map[string]string{} // issue type ID → value, for per-issue-type entries
	for _, en := range entries {
		switch {
		case len(en.Value) == 0 || string(en.Value) == "null":
		case en.IsAnyIssueType:
			value = en.Value
		default:
			perType[en.IssueTypeID] = string(en.Value)
		}
	}
	// A context limited to some issue types reports its default once per
	// issue type. When every issue type it covers has the same value, that is
	// simply the context's default.
	if value == nil && len(perType) > 0 && len(issueTypeIDs) > 0 {
		same, first := true, perType[issueTypeIDs[0]]
		for _, id := range issueTypeIDs {
			same = same && perType[id] == first
		}
		if same && first != "" && len(perType) == len(issueTypeIDs) {
			value = json.RawMessage(first)
		} else {
			r.note("Field %q has different default values per issue type; per-issue-type defaults are not in the format yet.", cf.Name)
		}
	}
	if value == nil {
		return spec.Opt[spec.Value]{}, false
	}
	var v struct {
		Type              string   `json:"type"`
		OptionID          string   `json:"optionId"`
		OptionIDs         []string `json:"optionIds"`
		CascadingOptionID string   `json:"cascadingOptionId"`
		Text              string   `json:"text"`
		Number            *float64 `json:"number"`
		Labels            []string `json:"labels"`
		URL               string   `json:"url"`
		Date              string   `json:"date"`
		DateTime          string   `json:"dateTime"`
		UseCurrent        bool     `json:"useCurrent"`
		AccountID         string   `json:"accountId"`
		AccountIDs        []string `json:"accountIds"`
		UserFilter        *struct {
			Enabled bool `json:"enabled"`
		} `json:"userFilter"`
	}
	if err := json.Unmarshal(value, &v); err != nil {
		r.note("Field %q has a default value this tool could not read; it is left out.", cf.Name)
		return spec.Opt[spec.Value]{}, false
	}
	optionName := func(id string) string {
		for _, o := range options {
			if o.ID == id {
				return o.Value
			}
		}
		r.note("Field %q's default refers to option %s, which is not among its options; it is kept as id:%s.", cf.Name, id, id)
		return "id:" + id
	}
	switch v.Type {
	case "option.single":
		return spec.Some(spec.Value{Str: optionName(v.OptionID)}), true
	case "option.multiple":
		if len(v.OptionIDs) == 0 {
			return spec.Opt[spec.Value]{}, false // Jira reports "no default" as an empty list
		}
		val := spec.Value{IsList: true}
		for _, id := range v.OptionIDs {
			val.List = append(val.List, optionName(id))
		}
		return spec.Some(val), true
	case "option.cascading":
		val := spec.Value{IsList: true, List: []string{optionName(v.OptionID)}}
		if v.CascadingOptionID != "" {
			val.List = append(val.List, optionName(v.CascadingOptionID))
		}
		return spec.Some(val), true
	case "textfield", "textarea":
		return spec.Some(spec.Value{Str: v.Text}), true
	case "url":
		return spec.Some(spec.Value{Str: v.URL}), true
	case "datepicker", "datetimepicker":
		// "current" means the date (and time) when the work item is created.
		switch {
		case v.UseCurrent:
			return spec.Some(spec.Value{Str: "current"}), true
		case v.Type == "datepicker" && len(v.Date) >= 10:
			return spec.Some(spec.Value{Str: v.Date[:10]}), true
		case v.DateTime != "":
			return spec.Some(spec.Value{Str: v.DateTime}), true
		}
	case "single.user.select", "multi.user.select":
		if v.UserFilter != nil && v.UserFilter.Enabled {
			r.note("Field %q limits which users can be picked (a user filter); that is not in the format yet.", cf.Name)
		}
		if v.Type == "single.user.select" {
			return spec.Some(spec.Value{Str: "user:" + v.AccountID}), v.AccountID != ""
		}
		val := spec.Value{IsList: true}
		for _, id := range v.AccountIDs {
			val.List = append(val.List, "user:"+id)
		}
		return spec.Some(val), len(val.List) > 0
	case "float":
		if v.Number != nil {
			return spec.Some(spec.Value{Str: strconv.FormatFloat(*v.Number, 'f', -1, 64)}), true
		}
	case "labels":
		return spec.Some(spec.Value{IsList: true, List: v.Labels}), true
	}
	r.note("Field %q has a default value of type %q, which the format has no form for; set it in Jira by hand.", cf.Name, v.Type)
	return spec.Opt[spec.Value]{}, false
}

// contextBinding names a context and says whether it is the space's own
// (only this space) or shared (global, or with other spaces).
func (r *Reader) contextBinding(p *Project, cf spec.CustomField, ctxID string) (*spec.ContainerRef, error) {
	type ctx struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		Description     string `json:"description"`
		IsGlobalContext bool   `json:"isGlobalContext"`
	}
	ctxs, err := jira.GetAll[ctx](r.ctx, r.c, "/rest/api/3/field/"+url.PathEscape(cf.ID)+"/context", url.Values{"contextId": {ctxID}})
	if err != nil {
		return nil, err
	}
	if len(ctxs) != 1 {
		return nil, fmt.Errorf("expected context %s of field %q, Jira returned %d", ctxID, cf.Name, len(ctxs))
	}
	c := ctxs[0]
	ref := containerRef(c.ID, c.Name, c.Description)
	ref.Scope = spec.ScopeShared
	if !c.IsGlobalContext {
		type mapping struct {
			ProjectID string `json:"projectId"`
		}
		projects, err := jira.GetAll[mapping](r.ctx, r.c, "/rest/api/3/field/"+url.PathEscape(cf.ID)+"/context/projectmapping",
			url.Values{"contextId": {ctxID}})
		if err != nil {
			return nil, err
		}
		// Jira warns a project may appear more than once in this list.
		onlyThis := len(projects) > 0
		for _, m := range projects {
			onlyThis = onlyThis && m.ProjectID == p.ID
		}
		if onlyThis {
			ref.Scope = spec.ScopeSpace
		}
	}
	if ref.Scope == spec.ScopeShared {
		ref.Mode = spec.ModeReuse // a shared context is never managed by this tool
	}
	return ref, nil
}

// contextDefaultsLegacy reads defaults through the deprecated endpoint, for
// sites where the newer one is limited to Atlassian's early access programme
// (CHANGE-3082). It knows one default per context, for any issue type.
func (r *Reader) contextDefaultsLegacy(cf spec.CustomField, contextIDs []string) (map[string][]issueTypeDefault, error) {
	values, err := jira.GetAll[json.RawMessage](r.ctx, r.c, "/rest/api/3/field/"+url.PathEscape(cf.ID)+"/context/defaultValue",
		url.Values{"contextId": contextIDs})
	if err != nil {
		return nil, err
	}
	out := map[string][]issueTypeDefault{}
	for _, v := range values {
		var ctx struct {
			ContextID string `json:"contextId"`
		}
		if json.Unmarshal(v, &ctx) == nil && ctx.ContextID != "" {
			out[ctx.ContextID] = append(out[ctx.ContextID], issueTypeDefault{IsAnyIssueType: true, Value: v})
		}
	}
	return out, nil
}
