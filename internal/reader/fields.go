package reader

import (
	"cmp"
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

// siteField is one field of the site.
type siteField struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	StoredName  string `json:"untranslatedName"`
	Description string `json:"-"` // custom fields only, from the field search
	SearcherKey string `json:"-"`
	IsLocked    bool   `json:"-"`
	Custom      bool   `json:"custom"`
	Schema      struct {
		Custom string `json:"custom"`
	} `json:"schema"`
}

// customFieldTypes maps Jira's built-in custom field type keys to the
// format's short names (spec.FieldTypes). Other keys are exported in full.
var customFieldTypes = map[string]string{
	"cascadingselect": "cascading", "multicheckboxes": "checkboxes", "datepicker": "date",
	"datetime": "datetime", "grouppicker": "group", "labels": "labels", "multigrouppicker": "multigroup",
	"multiselect": "multiselect", "multiuserpicker": "multiuser", "multiversion": "multiversion",
	"float": "number", "textarea": "paragraph", "project": "project", "radiobuttons": "radio",
	"readonlyfield": "readonly", "select": "select", "textfield": "text", "url": "url",
	"userpicker": "user", "version": "version",
}

const builtinTypePrefix = "com.atlassian.jira.plugin.system.customfieldtypes:"

func shortFieldType(key string) string {
	if short, ok := customFieldTypes[strings.TrimPrefix(key, builtinTypePrefix)]; ok && strings.HasPrefix(key, builtinTypePrefix) {
		return short
	}
	return key
}

// renderers maps both backends' renderer names to the format's.
var renderers = map[string]string{
	"atlassian-wiki-renderer": "wiki", "wiki-renderer": "wiki",
	"jira-text-renderer": "plain", "text-renderer": "plain",
}

// siteField returns a field of the site by ID, reading the site's fields on
// first use. GET /field omits some custom fields (recently created ones, and
// ones the account cannot see), so custom fields come from the paged field
// search, which also has their description, searcher and locked flag; GET
// /field supplies the system fields.
func (r *Reader) siteField(id string) (siteField, bool) {
	if r.siteFields == nil {
		if err := r.loadSiteFields(); err != nil {
			r.lookupFailed(err)
			return siteField{}, false
		}
	}
	f, ok := r.siteFields[id]
	return f, ok
}

func (r *Reader) loadSiteFields() error {
	var all []siteField
	if err := r.get("/rest/api/3/field", nil, &all); err != nil {
		return fmt.Errorf("reading the site's fields: %w", err)
	}
	fields := map[string]siteField{}
	for _, f := range all {
		if !f.Custom {
			fields[f.ID] = f
		}
	}
	type searched struct {
		ID             string `json:"id"`
		Name           string `json:"name"`           // the stored name
		TranslatedName string `json:"translatedName"` // the name shown to this account
		Description    string `json:"description"`
		SearcherKey    string `json:"searcherKey"`
		IsLocked       bool   `json:"isLocked"`
		Schema         struct {
			Custom string `json:"custom"`
		} `json:"schema"`
	}
	custom, err := jira.GetAll[searched](r.ctx, r.c, "/rest/api/3/field/search", url.Values{
		"type": {"custom"}, "expand": {"searcherKey,isLocked"}, "maxResults": {"100"},
	})
	if err != nil {
		return fmt.Errorf("reading the site's custom fields: %w", err)
	}
	for _, c := range custom {
		f := siteField{ID: c.ID, Name: c.TranslatedName, StoredName: c.Name, Custom: true,
			Description: c.Description, SearcherKey: c.SearcherKey, IsLocked: c.IsLocked}
		if f.Name == "" {
			f.Name = c.Name
		}
		f.Schema.Custom = c.Schema.Custom
		fields[c.ID] = f
	}
	r.siteFields = fields
	return nil
}

// fieldName returns the name for a field ID. Custom fields it names are
// logged, in order, for CustomFields.
func (r *Reader) fieldName(id string) string {
	f, ok := r.siteField(id)
	if !ok {
		if r.siteFields != nil {
			r.note("Field %s is configured for the space but is not in the site's field list; the spec names it as id:%s.", id, id)
		}
		return "id:" + id
	}
	name := f.Name
	if f.StoredName != "" && f.StoredName != f.Name {
		if r.namedFields == nil {
			r.namedFields = map[string]bool{}
		}
		if !r.namedFields[id] {
			r.namedFields[id] = true
			r.note("Field %q is shown as %q in this account's language; the spec uses the stored name.", f.StoredName, f.Name)
		}
		name = f.StoredName
	}
	if f.Custom && !slices.Contains(r.customOrder, id) {
		for _, other := range r.customOrder {
			if r.customName(other) == name {
				r.note("Custom fields %s and %s are both named %q; references to that name are ambiguous.", other, id, name)
			}
		}
		r.customOrder = append(r.customOrder, id)
	}
	return name
}

func (r *Reader) customName(id string) string {
	f := r.siteFields[id]
	return cmp.Or(f.StoredName, f.Name)
}

// CustomFields returns the definitions of the custom fields readers have
// named so far, in the order they were first named.
func (r *Reader) CustomFields() []spec.CustomField {
	var out []spec.CustomField
	for _, id := range r.customOrder {
		f := r.siteFields[id]
		cf := spec.CustomField{
			GlobalObject: spec.GlobalObject{Meta: spec.Meta{ID: id}, Name: r.customName(id)},
			Type:         shortFieldType(f.Schema.Custom),
			Searcher:     optText(f.SearcherKey),
			Locked:       f.IsLocked,
		}
		cf.Description, cf.Mode = globalOwnership(f.Description)
		out = append(out, cf)
	}
	return out
}

// FieldLayout is which fields the space has, from whichever backend it uses:
// a field scheme, or legacy field configurations.
type FieldLayout struct {
	Backend string // spec.FieldBackendSchemes or spec.FieldBackendConfigurations
	Fields  []spec.SpaceField
	// FieldScheme is the space's field scheme (field schemes only).
	FieldScheme *spec.ContainerRef
	// FieldConfigurationScheme is the space's own scheme (legacy only; nil
	// when Jira's system default scheme applies).
	FieldConfigurationScheme *spec.ContainerRef
	// Fixed are the fields Jira will not take out of the field scheme (field
	// schemes only): Jira puts some in every new scheme.
	Fixed []string
	// FieldConfigurations is the field configuration of each issue type, in
	// the order of the IssueTypes given (legacy only).
	FieldConfigurations []*spec.ContainerRef
}

// Fields reads the fields in the space.
func (r *Reader) Fields(p *Project, its *IssueTypes) (*FieldLayout, error) {
	schemeID, err := r.fieldSchemeID(p)
	if err != nil {
		return nil, err
	}
	var out *FieldLayout
	if schemeID != "" {
		out, err = r.fieldScheme(its, schemeID)
	} else {
		out, err = r.fieldConfigurations(p, its)
	}
	if err != nil {
		return nil, err
	}
	return out, r.done()
}

// fieldSchemeID returns the space's field scheme, or "" when the site does
// not offer field schemes (the API is absent outside the beta) or the space
// has none, in which case legacy field configurations apply.
func (r *Reader) fieldSchemeID(p *Project) (string, error) {
	type assoc struct {
		ProjectID int `json:"projectId"`
		SchemeID  int `json:"schemeId"`
	}
	list, err := jira.GetAll[assoc](r.ctx, r.c, "/rest/api/3/config/fieldschemes/projects", url.Values{"projectId": {p.ID}})
	var apiErr *jira.APIError
	if errors.As(err, &apiErr) && (apiErr.Status == http.StatusNotFound || apiErr.Status == http.StatusForbidden) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("checking whether the space uses a field scheme: %w", err)
	}
	for _, a := range list {
		if strconv.Itoa(a.ProjectID) == p.ID {
			return strconv.Itoa(a.SchemeID), nil
		}
	}
	return "", nil
}

// settings exports only what differs from the defaults of a space.fields entry
// (not required, plain renderer, no description).
// Field schemes report a plain renderer even for fields that have no text to
// render, and legacy configurations report none; both export as the default.
func settings(required *bool, renderer, description string) spec.FieldSettings {
	s := spec.FieldSettings{Description: optText(description)}
	if required != nil && *required {
		s.Required = spec.Some(true)
	}
	if renderers[renderer] == "wiki" {
		s.Renderer = spec.Some("wiki")
	}
	return s
}

// fieldScheme reads a field scheme: one entry per associated field.
func (r *Reader) fieldScheme(its *IssueTypes, id string) (*FieldLayout, error) {
	var scheme struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		IsDefault   bool   `json:"isDefault"`
	}
	if err := r.get("/rest/api/3/config/fieldschemes/"+url.PathEscape(id), nil, &scheme); err != nil {
		return nil, fmt.Errorf("reading field scheme %s: %w", id, err)
	}
	type params struct {
		WorkTypeID  string `json:"workTypeId"`
		IsRequired  *bool  `json:"isRequired"`
		Renderer    string `json:"rendererType"`
		Description string `json:"description"`
	}
	type item struct {
		FieldID          string   `json:"fieldId"`
		Allowed          []string `json:"allowedOperations"`
		Parameters       *params  `json:"parameters"`
		Restricted       []string `json:"restrictedToWorkTypes"`
		WorkTypeSettings []params `json:"workTypeParameters"`
	}
	items, err := jira.GetAll[item](r.ctx, r.c, "/rest/api/3/config/fieldschemes/"+url.PathEscape(id)+"/fields",
		url.Values{"maxResults": {"100"}})
	if err != nil {
		return nil, fmt.Errorf("reading the fields of field scheme %q: %w", scheme.Name, err)
	}

	out := &FieldLayout{Backend: spec.FieldBackendSchemes, Fields: []spec.SpaceField{}}
	for _, it := range items {
		f := spec.SpaceField{Name: r.fieldName(it.FieldID)}
		if !slices.Contains(it.Allowed, "REMOVE") {
			out.Fixed = append(out.Fixed, f.Name)
		}
		if it.Restricted != nil {
			f.IssueTypes = inSpaceOrder(its, r.issueTypeNames(its, it.Restricted, fmt.Sprintf("Field %q", f.Name)))
			if len(f.IssueTypes) == 0 {
				continue // only on issue types outside this space
			}
		}
		if p := it.Parameters; p != nil {
			f.FieldSettings = settings(p.IsRequired, p.Renderer, r.ownDescription(it.FieldID, p.Description))
		}
		var per []spec.IssueTypeFieldSettings
		for _, w := range it.WorkTypeSettings {
			if name, ok := its.Name(w.WorkTypeID); ok {
				// What a work type does not set, it takes from the field.
				eff := f.FieldSettings
				if w.IsRequired != nil {
					eff.Required = settings(w.IsRequired, "", "").Required
				}
				if w.Renderer != "" {
					eff.Renderer = settings(nil, w.Renderer, "").Renderer
				}
				if d := r.ownDescription(it.FieldID, w.Description); d != "" {
					eff.Description = optText(d)
				}
				per = append(per, spec.IssueTypeFieldSettings{IssueType: name, FieldSettings: eff})
			}
		}
		foldSettings(&f, per, its.Len())
		out.Fields = append(out.Fields, f)
	}
	out.FieldScheme = containerRef(scheme.ID, scheme.Name, scheme.Description)
	out.FieldScheme.SiteDefault = scheme.IsDefault
	return out, nil
}

// foldSettings takes each issue type's effective settings. It puts them on
// the entry itself when the entry has none and they are the same for every
// issue type that has the field; otherwise it writes, as by_issue_type, what
// each issue type changes from the entry (unwritten settings inherit the
// entry's), leaving out issue types that change
// nothing. spaceTypes is the number of issue types in the space.
func foldSettings(f *spec.SpaceField, per []spec.IssueTypeFieldSettings, spaceTypes int) {
	if len(per) == 0 {
		return
	}
	covers := f.IssueTypes == nil && len(per) == spaceTypes || f.IssueTypes != nil && len(per) == len(f.IssueTypes)
	same := true
	for _, p := range per[1:] {
		same = same && p.FieldSettings == per[0].FieldSettings
	}
	if same && covers && f.FieldSettings == (spec.FieldSettings{}) {
		f.FieldSettings = per[0].FieldSettings
		return
	}
	for _, p := range per {
		if d := changes(f.FieldSettings, p.FieldSettings); d != (spec.FieldSettings{}) {
			f.ByIssueType = append(f.ByIssueType, spec.IssueTypeFieldSettings{IssueType: p.IssueType, FieldSettings: d})
		}
	}
}

// changes returns the settings of eff that differ from base, written out in
// full where the difference is a default (not required, plain renderer).
func changes(base, eff spec.FieldSettings) spec.FieldSettings {
	var d spec.FieldSettings
	if eff.Required.V != base.Required.V {
		d.Required = spec.Some(eff.Required.V)
	}
	if eff.Renderer.V != base.Renderer.V {
		d.Renderer = spec.Some(cmp.Or(eff.Renderer.V, "plain"))
	}
	if eff.Description.V != base.Description.V {
		d.Description = eff.Description
	}
	return d
}

// fieldConfigurations reads legacy field configurations: per issue type,
// the fields that are not hidden in its field configuration.
func (r *Reader) fieldConfigurations(p *Project, its *IssueTypes) (*FieldLayout, error) {
	type schemeRef struct {
		FieldConfigurationScheme *fcsRef `json:"fieldConfigurationScheme"`
	}
	refs, err := jira.GetAll[schemeRef](r.ctx, r.c, "/rest/api/3/fieldconfigurationscheme/project", url.Values{"projectId": {p.ID}})
	if err != nil {
		return nil, fmt.Errorf("reading the field configuration scheme: %w", err)
	}
	var fcs *fcsRef
	if len(refs) > 0 {
		fcs = refs[0].FieldConfigurationScheme
	}
	return r.fieldConfigurationScheme(its, fcs)
}

// fcsRef names a field configuration scheme.
type fcsRef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// fieldConfigurationScheme reads the fields a field configuration scheme
// gives the issue types; nil is Jira's system default scheme.
func (r *Reader) fieldConfigurationScheme(its *IssueTypes, fcs *fcsRef) (*FieldLayout, error) {

	type fieldConfiguration struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		IsDefault   bool   `json:"isDefault"`
	}
	out := &FieldLayout{Backend: spec.FieldBackendConfigurations, Fields: []spec.SpaceField{}}
	configFor := map[string]string{} // issue type ID → field configuration ID
	defaultConfig := ""
	var configs []fieldConfiguration
	if fcs == nil {
		// No scheme associated: Jira's system default scheme applies, which
		// uses the site's default field configuration for every issue type.
		all, err := jira.GetAll[fieldConfiguration](r.ctx, r.c, "/rest/api/3/fieldconfiguration", url.Values{"isDefault": {"true"}})
		if err != nil {
			return nil, fmt.Errorf("reading the default field configuration: %w", err)
		}
		for _, c := range all {
			if c.IsDefault {
				configs, defaultConfig = []fieldConfiguration{c}, strconv.Itoa(c.ID)
			}
		}
		if defaultConfig == "" {
			return nil, errors.New("the space uses the default field configuration, but Jira did not return one")
		}
		r.note("The space uses Jira's system default field configuration scheme (no scheme of its own).")
	} else {
		type mapping struct {
			IssueTypeID          string `json:"issueTypeId"`
			FieldConfigurationID string `json:"fieldConfigurationId"`
		}
		mappings, err := jira.GetAll[mapping](r.ctx, r.c, "/rest/api/3/fieldconfigurationscheme/mapping",
			url.Values{"fieldConfigurationSchemeId": {fcs.ID}})
		if err != nil {
			return nil, fmt.Errorf("reading the mappings of %q: %w", fcs.Name, err)
		}
		var ids []string
		for _, m := range mappings {
			if m.IssueTypeID == "default" {
				defaultConfig = m.FieldConfigurationID
			} else {
				configFor[m.IssueTypeID] = m.FieldConfigurationID
			}
			ids = util.AppendNew(ids, m.FieldConfigurationID)
		}
		configs, err = jira.GetAll[fieldConfiguration](r.ctx, r.c, "/rest/api/3/fieldconfiguration", url.Values{"id": ids})
		if err != nil {
			return nil, fmt.Errorf("reading field configurations: %w", err)
		}
		out.FieldConfigurationScheme = containerRef(fcs.ID, fcs.Name, fcs.Description)
	}

	type item struct {
		ID          string `json:"id"`
		IsHidden    bool   `json:"isHidden"`
		IsRequired  bool   `json:"isRequired"`
		Description string `json:"description"`
		Renderer    string `json:"renderer"`
	}
	itemsOf := map[string][]item{}
	configByID := map[string]fieldConfiguration{}
	for _, c := range configs {
		id := strconv.Itoa(c.ID)
		configByID[id] = c
		items, err := jira.GetAll[item](r.ctx, r.c, "/rest/api/3/fieldconfiguration/"+id+"/fields", url.Values{"maxResults": {"100"}})
		if err != nil {
			return nil, fmt.Errorf("reading the items of field configuration %q: %w", c.Name, err)
		}
		itemsOf[id] = items
	}

	// Collect, per field, its settings on each issue type where it is shown.
	type shown struct {
		issueType string
		settings  spec.FieldSettings
	}
	var order []string
	byField := map[string][]shown{}
	for i := range its.Len() {
		name := its.Space[i].Name
		cid, ok := configFor[its.ID(i)]
		if !ok {
			cid = defaultConfig
		}
		c, ok := configByID[cid]
		if !ok {
			return nil, fmt.Errorf("issue type %q uses field configuration %s, which Jira did not return", name, cid)
		}
		ref := containerRef(cid, c.Name, c.Description)
		ref.SiteDefault = c.IsDefault
		out.FieldConfigurations = append(out.FieldConfigurations, ref)
		for _, it := range itemsOf[cid] {
			if it.IsHidden {
				continue
			}
			if _, seen := byField[it.ID]; !seen {
				order = append(order, it.ID)
			}
			required := it.IsRequired
			byField[it.ID] = append(byField[it.ID], shown{name, settings(&required, it.Renderer, it.Description)})
		}
	}
	for _, id := range order {
		f := spec.SpaceField{Name: r.fieldName(id)}
		entries := byField[id]
		if len(entries) < its.Len() {
			for _, s := range entries {
				f.IssueTypes = append(f.IssueTypes, s.issueType)
			}
		}
		per := make([]spec.IssueTypeFieldSettings, len(entries))
		for i, s := range entries {
			per[i] = spec.IssueTypeFieldSettings{IssueType: s.issueType, FieldSettings: s.settings}
		}
		foldSettings(&f, per, its.Len())
		out.Fields = append(out.Fields, f)
	}
	return out, nil
}

// FieldTypeKey returns Jira's type key for a custom field type: the full
// key of a built-in type from its short name, or the key as written.
func FieldTypeKey(short string) string {
	for key, s := range customFieldTypes {
		if s == short {
			return builtinTypePrefix + key
		}
	}
	return short
}

// defaultSearchers are the searchers Jira documents for the built-in custom
// field types (POST /rest/api/3/field, searcherKey).
var defaultSearchers = map[string]string{
	"cascadingselect": "cascadingselectsearcher", "datepicker": "daterange", "datetime": "datetimerange",
	"float": "exactnumber", "grouppicker": "grouppickersearcher", "multigrouppicker": "grouppickersearcher",
	"labels": "labelsearcher", "multicheckboxes": "multiselectsearcher", "multiselect": "multiselectsearcher",
	"radiobuttons": "multiselectsearcher", "select": "multiselectsearcher", "project": "projectsearcher",
	"textarea": "textsearcher", "textfield": "textsearcher", "readonlyfield": "textsearcher", "url": "exacttextsearcher",
	"userpicker": "userpickergroupsearcher", "multiuserpicker": "userpickergroupsearcher",
	"version": "versionsearcher", "multiversion": "versionsearcher",
}

// DefaultSearcher returns the searcher a new field of a built-in type gets
// when the spec names none, or "" for other types.
func DefaultSearcher(typeKey string) string {
	if s, ok := defaultSearchers[strings.TrimPrefix(typeKey, builtinTypePrefix)]; ok && strings.HasPrefix(typeKey, builtinTypePrefix) {
		return builtinTypePrefix + s
	}
	return ""
}

// ownDescription is a field scheme's description of a field, or "" when it
// is the field's own description, which a scheme shows but does not hold:
// Jira's default scheme shows it, a new scheme does not, and on a
// locked field it cannot be set.
func (r *Reader) ownDescription(fieldID, description string) string {
	if f, ok := r.siteField(fieldID); ok && f.Description != "" && f.Description == description {
		return ""
	}
	return description
}

// inSpaceOrder puts issue type names in the space's order: Jira's order in
// a field's restriction means nothing.
func inSpaceOrder(its *IssueTypes, names []string) []string {
	if names == nil {
		return nil
	}
	var out []string
	for _, t := range its.Space {
		if slices.Contains(names, t.Name) {
			out = append(out, t.Name)
		}
	}
	return out
}
