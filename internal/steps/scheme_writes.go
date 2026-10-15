package steps

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/reader"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// The writes that create schemes (steps 13 to 20) and switch the space to
// them (step 25). A new scheme is never the space's until step 25.

// schemeID reads the ID from the answer to a create, which Jira names
// differently per kind of scheme.
type schemeID struct {
	ID                jira.ID `json:"id"`
	IssueTypeSchemeID jira.ID `json:"issueTypeSchemeId"`
}

func (s schemeID) String() string {
	if s.ID != "" {
		return string(s.ID)
	}
	return string(s.IssueTypeSchemeID)
}

func postScheme(path string, body any) action {
	return func(ctx context.Context, snd Sender) (string, error) {
		var out schemeID
		if err := snd.Send(ctx, http.MethodPost, path, nil, body, &out); err != nil {
			return "", err
		}
		if out.String() == "" {
			return "", fmt.Errorf("the answer to POST %s does not say what was created", path)
		}
		return out.String(), nil
	}
}

// issueTypeIDs are the site IDs of issue types by name, in order.
func (e *Env) issueTypeIDs(names []string) ([]string, error) {
	var out []string
	for _, n := range names {
		id, err := e.knownIssueTypeID(n)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func createIssueTypeScheme(env *Env, name string) action {
	return func(ctx context.Context, snd Sender) (string, error) {
		var names []string
		def := ""
		for _, t := range env.Spec.Space.IssueTypes {
			names = append(names, t.Name)
			if t.Default {
				def = t.Name
			}
		}
		typeIDs, err := env.issueTypeIDs(names)
		if err != nil {
			return "", err
		}
		body := map[string]any{"name": name, "description": managedDescription(spec.Opt[string]{}), "issueTypeIds": typeIDs}
		if def != "" {
			id, _ := env.issueTypeID(def)
			body["defaultIssueTypeId"] = id
		}
		return postScheme("/rest/api/3/issuetypescheme", body)(ctx, snd)
	}
}

// mostCommon returns the value most issue types have, for a scheme's
// default; ties go to the first.
func mostCommon(values []string) string {
	best, count := "", 0
	for _, v := range values {
		n := 0
		for _, w := range values {
			if w == v {
				n++
			}
		}
		if n > count {
			best, count = v, n
		}
	}
	return best
}

func createWorkflowScheme(env *Env, name string) action {
	return func(ctx context.Context, snd Sender) (string, error) {
		var workflows []string
		for _, t := range env.Spec.Space.IssueTypes {
			workflows = append(workflows, t.Workflow.V)
		}
		def := mostCommon(workflows)
		mappings := map[string]string{}
		for _, t := range env.Spec.Space.IssueTypes {
			if t.Workflow.V == def {
				continue
			}
			id, err := env.issueTypeID(t.Name)
			if err != nil {
				return "", err
			}
			mappings[id] = t.Workflow.V
		}
		return postScheme("/rest/api/3/workflowscheme", map[string]any{"name": name, "description": managedDescription(spec.Opt[string]{}),
			"defaultWorkflow": def, "issueTypeMappings": mappings})(ctx, snd)
	}
}

// createScreenSchemes creates a screen scheme for each set of screens the
// space's issue types use (or reuses one of that name made by an earlier
// run), then the issue type screen scheme that maps them.
func createScreenSchemes(env *Env, x resolver, name string) action {
	return func(ctx context.Context, snd Sender) (string, error) {
		type set struct {
			screens spec.ScreenSet
			name    string
		}
		var sets []set
		setOf := map[string]int{} // issue type → index in sets
		var texts []string
		for _, t := range env.Spec.Space.IssueTypes {
			if t.Screens == nil {
				continue
			}
			text := screenSetText(*t.Screens)
			texts = append(texts, text)
			i := slices.IndexFunc(sets, func(s set) bool { return screenSetText(s.screens) == text })
			if i < 0 {
				n := env.Spec.Space.ManagedPrefix + " " + t.Name + " Screen Scheme"
				if t.Binding != nil && t.Binding.ScreenScheme != nil && t.Binding.ScreenScheme.Name != "" {
					n = t.Binding.ScreenScheme.Name
				}
				sets = append(sets, set{*t.Screens, n})
				i = len(sets) - 1
			}
			setOf[t.Name] = i
		}
		ids := make([]string, len(sets))
		for i, s := range sets {
			id, err := screenScheme(ctx, snd, x, s.name, s.screens, "", nil)
			if err != nil {
				return "", err
			}
			ids[i] = id
		}
		def := mostCommon(texts)
		var mappings []map[string]string
		for i, s := range sets {
			if screenSetText(s.screens) == def {
				mappings = append(mappings, map[string]string{"issueTypeId": "default", "screenSchemeId": ids[i]})
			}
		}
		for _, t := range env.Spec.Space.IssueTypes {
			i, ok := setOf[t.Name]
			if !ok || screenSetText(sets[i].screens) == def {
				continue
			}
			id, err := env.issueTypeID(t.Name)
			if err != nil {
				return "", err
			}
			mappings = append(mappings, map[string]string{"issueTypeId": id, "screenSchemeId": ids[i]})
		}
		return postScheme("/rest/api/3/issuetypescreenscheme", map[string]any{"name": name,
			"description": managedDescription(spec.Opt[string]{}), "issueTypeMappings": mappings})(ctx, snd)
	}
}

// screenScheme returns the screen scheme of that name with these screens:
// the existing one when its screens match; changed to them when the tool
// made it and no issue type screen scheme but itss (the one being changed,
// or "" for a new one) uses it, and mayChange (when set) agrees; otherwise created.
func screenScheme(ctx context.Context, snd Sender, x resolver, name string, screens spec.ScreenSet, itss string,
	mayChange func(id string) error) (string, error) {
	ids := map[string]string{}
	for op, screen := range map[string]string{"create": screens.Create, "edit": screens.Edit, "view": screens.View} {
		id, err := x.screen(screen)
		if err != nil {
			return "", err
		}
		ids[op] = id
	}
	ids["default"] = ids["view"]
	var found struct {
		Values []struct {
			ID          int            `json:"id"`
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Screens     map[string]int `json:"screens"`
		} `json:"values"`
	}
	if err := snd.Send(ctx, http.MethodGet, "/rest/api/3/screenscheme", url.Values{"queryString": {name}}, nil, &found); err != nil {
		return "", err
	}
	for _, f := range found.Values {
		if f.Name != name {
			continue
		}
		id := strconv.Itoa(f.ID)
		same := true
		for op, sid := range ids {
			same = same && strconv.Itoa(f.Screens[op]) == sid
		}
		if same {
			return id, nil
		}
		if !strings.Contains(f.Description, reader.Marker) {
			return "", fmt.Errorf("screen scheme %q has other screens, and this tool did not create it; name another in the spec's binding", name)
		}
		users, err := screenSchemeUsers(ctx, snd, id)
		if err != nil {
			return "", err
		}
		for _, u := range users {
			if u != itss {
				return "", fmt.Errorf("screen scheme %q has other screens, and issue type screen scheme %s uses it too; name another in the spec's binding", name, u)
			}
		}
		if mayChange != nil {
			if err := mayChange(id); err != nil {
				return "", err
			}
		}
		if err := snd.Send(ctx, http.MethodPut, "/rest/api/3/screenscheme/"+id, nil, map[string]any{"screens": ids}, nil); err != nil {
			return "", err
		}
		return id, nil
	}
	return postScheme("/rest/api/3/screenscheme", map[string]any{"name": name, "description": managedDescription(spec.Opt[string]{}),
		"screens": ids})(ctx, snd)
}

func (x resolver) screen(name string) (string, error) {
	found, err := x.r.FindScreens(name)
	if err != nil {
		return "", err
	}
	for _, f := range found {
		if f.Name == name {
			return f.ID, nil
		}
	}
	return "", fmt.Errorf("no screen on the site is named %q", name)
}

var rendererKeys = map[string]string{"wiki": "atlassian-wiki-renderer", "plain": "jira-text-renderer"}

func fieldParameters(s spec.FieldSettings) map[string]any {
	p := map[string]any{}
	if s.Required.Set {
		p["isRequired"] = s.Required.V
	}
	if s.Renderer.Set {
		p["rendererType"] = rendererKeys[s.Renderer.V]
	}
	if s.Description.Set {
		p["description"] = s.Description.V
	}
	return p
}

// createFieldScheme creates a field scheme with the space's fields: the
// scheme, the fields (at most 100 a request), then their settings.
func createFieldScheme(env *Env, x resolver, name string) action {
	return func(ctx context.Context, snd Sender) (string, error) {
		// Every name is resolved before the first write, so a name that fails
		// leaves nothing half made.
		fieldIDs := map[string]string{}
		for _, f := range inherited(env, env.Spec.Space.Fields.V) {
			fid, err := x.field(f.Name)
			if err != nil {
				return "", err
			}
			fieldIDs[f.Name] = fid
			if _, err := env.issueTypeIDs(f.IssueTypes); err != nil {
				return "", err
			}
			for _, b := range f.ByIssueType {
				if _, err := env.issueTypeIDs([]string{b.IssueType}); err != nil {
					return "", err
				}
			}
		}
		id, err := postScheme("/rest/api/3/config/fieldschemes", map[string]string{"name": name,
			"description": managedDescription(spec.Opt[string]{})})(ctx, snd)
		if err != nil {
			return "", err
		}
		scheme, _ := strconv.Atoi(id)
		fields := map[string]any{}
		params := map[string]any{}
		for _, f := range inherited(env, env.Spec.Space.Fields.V) {
			fid := fieldIDs[f.Name]
			item, p, err := fieldSchemeEntry(env, scheme, f)
			if err != nil {
				return "", err
			}
			fields[fid] = []any{item}
			if len(p["parameters"].(map[string]any)) == 0 {
				delete(p, "parameters") // Jira refuses an empty one ("At least one of the parameters is required")
			}
			if p["parameters"] != nil || p["workTypeParameters"] != nil {
				params[fid] = []any{p}
			}
			if len(fields) == 100 {
				if err := snd.Send(ctx, http.MethodPut, "/rest/api/3/config/fieldschemes/fields", nil, fields, nil); err != nil {
					return "", err
				}
				fields = map[string]any{}
			}
		}
		if len(fields) > 0 {
			if err := snd.Send(ctx, http.MethodPut, "/rest/api/3/config/fieldschemes/fields", nil, fields, nil); err != nil {
				return "", err
			}
		}
		if len(params) > 0 {
			if err := snd.Send(ctx, http.MethodPut, "/rest/api/3/config/fieldschemes/fields/parameters", nil, params, nil); err != nil {
				return "", err
			}
		}
		if err := removeOtherFields(ctx, snd, scheme, fieldIDs); err != nil {
			return "", err
		}
		return id, nil
	}
}

// holder writes a holder as Jira's type and value: IDs for roles, fields
// and (in permissions) groups; group names elsewhere, which is what
// notification schemes accept.
func (x resolver) holder(h string, place int) (string, string, error) {
	kind, value, _ := strings.Cut(h, ":")
	t, ok := spec.HolderTypes[kind]
	if !ok || t.In(place) == "" {
		return "", "", fmt.Errorf("%q cannot be written here", h)
	}
	var err error
	switch kind {
	case "role":
		value, err = x.role(value)
	case "group":
		if place == 0 {
			value, err = x.group(value)
		}
	case "user_field", "group_field":
		value, err = x.field(value)
	}
	return t.In(place), value, err
}

// holderKeys are the body keys of a holder's type and value in each place
// (spec.PlacePermission, PlaceNotification, PlaceSecurity): Jira names them
// differently in each.
var holderKeys = [...][2]string{{"type", "value"}, {"notificationType", "parameter"}, {"type", "parameter"}}

// holderBody is a holder as a request body in a place takes it.
func (x resolver) holderBody(h string, place int) (map[string]string, error) {
	typ, value, err := x.holder(h, place)
	if err != nil {
		return nil, err
	}
	keys := holderKeys[place]
	body := map[string]string{keys[0]: typ}
	if value != "" {
		body[keys[1]] = value
	}
	return body, nil
}

// event is the ID of the notification event named name.
func (x resolver) event(name string) (string, error) {
	id, ok, err := x.r.EventID(name)
	switch {
	case err != nil:
		return "", err
	case !ok:
		return "", fmt.Errorf("no event on the site is named %q", name)
	}
	return id, nil
}

func createPermissionScheme(x resolver, p spec.Permissions, name string) action {
	return func(ctx context.Context, snd Sender) (string, error) {
		var grants []map[string]any
		for _, g := range p.Grants {
			for _, h := range g.Holders {
				holder, err := x.holderBody(h, spec.PlacePermission)
				if err != nil {
					return "", err
				}
				grants = append(grants, map[string]any{"permission": g.Permission, "holder": holder})
			}
		}
		id, err := postScheme("/rest/api/3/permissionscheme", map[string]any{"name": name,
			"description": managedDescription(spec.Opt[string]{}), "permissions": grants})(ctx, snd)
		if err != nil {
			return "", err
		}
		return id, removeOtherGrants(ctx, snd, id, grants)
	}
}

// grantSettle is how long Jira is given to finish adding its grants to a new
// permission scheme: it adds the guest role's after the scheme is returned.
var grantSettle = 3 * time.Second

// removeOtherGrants takes out of a new permission scheme the grants Jira
// added that were not asked for (it adds the guest role to many), as the
// spec's grants are the space's complete list. Jira adds some a moment
// after answering, so the scheme is read again until a read finds none new.
func removeOtherGrants(ctx context.Context, snd Sender, id string, asked []map[string]any) error {
	path := "/rest/api/3/permissionscheme/" + id + "/permission"
	removed := map[jira.ID]bool{}
	for pass := 0; pass < 4; pass++ {
		if pass > 0 {
			select {
			case <-time.After(grantSettle):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		var current struct {
			Permissions []struct {
				ID     jira.ID `json:"id"`
				Holder struct {
					Type  string `json:"type"`
					Value string `json:"value"`
				} `json:"holder"`
				Permission string `json:"permission"`
			} `json:"permissions"`
		}
		if err := snd.Send(ctx, http.MethodGet, path, nil, nil, &current); err != nil {
			return err
		}
		fresh := false
		for _, p := range current.Permissions {
			wanted := slices.ContainsFunc(asked, func(g map[string]any) bool {
				h := g["holder"].(map[string]string)
				return g["permission"] == p.Permission && h["type"] == p.Holder.Type && h["value"] == p.Holder.Value
			})
			if wanted || removed[p.ID] {
				continue
			}
			if err := snd.Send(ctx, http.MethodDelete, path+"/"+string(p.ID), nil, nil, nil); err != nil {
				return err
			}
			removed[p.ID], fresh = true, true
		}
		if pass > 0 && !fresh {
			return nil
		}
	}
	return nil
}

func createNotificationScheme(env *Env, x resolver, n spec.Notifications, name string) action {
	return func(ctx context.Context, snd Sender) (string, error) {
		var events []map[string]any
		for _, e := range n.Events {
			id, err := x.event(e.Permission)
			if err != nil {
				return "", err
			}
			var to []map[string]string
			for _, h := range e.Holders {
				nt, err := x.holderBody(h, spec.PlaceNotification)
				if err != nil {
					return "", err
				}
				to = append(to, nt)
			}
			events = append(events, map[string]any{"event": map[string]string{"id": id}, "notifications": to})
		}
		return postScheme("/rest/api/3/notificationscheme", map[string]any{"name": name,
			"description": managedDescription(spec.Opt[string]{}), "notificationSchemeEvents": events})(ctx, snd)
	}
}

func createSecurityScheme(x resolver, is spec.IssueSecurity, name string) action {
	return func(ctx context.Context, snd Sender) (string, error) {
		var levels []map[string]any
		for _, l := range is.Levels {
			var members []map[string]string
			for _, h := range l.Holders {
				m, err := x.holderBody(h, spec.PlaceSecurity)
				if err != nil {
					return "", err
				}
				members = append(members, m)
			}
			level := map[string]any{"name": l.Name, "isDefault": l.Default, "members": members}
			if l.Description.Has() {
				level["description"] = l.Description.V
			}
			levels = append(levels, level)
		}
		return postScheme("/rest/api/3/issuesecurityschemes", map[string]any{"name": name,
			"description": managedDescription(spec.Opt[string]{}), "levels": levels})(ctx, snd)
	}
}

func (x resolver) priority(name string) (string, error) {
	all, err := x.r.SitePriorities()
	if err != nil {
		return "", err
	}
	for _, p := range all {
		if p.Name == name {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("no priority on the site is named %q", name)
}

func createPriorityScheme(x resolver, p spec.SpacePriorities, name string) action {
	return func(ctx context.Context, snd Sender) (string, error) {
		var ids []int
		for _, n := range p.Priorities {
			id, err := x.priority(n)
			if err != nil {
				return "", err
			}
			i, _ := strconv.Atoi(id)
			ids = append(ids, i)
		}
		def, err := x.priority(cmp.Or(p.Default.V, p.Priorities[0]))
		if err != nil {
			return "", err
		}
		d, _ := strconv.Atoi(def)
		return postScheme("/rest/api/3/priorityscheme", map[string]any{"name": name,
			"description": managedDescription(spec.Opt[string]{}), "priorityIds": ids, "defaultPriorityId": d})(ctx, snd)
	}
}

// removeOtherFields takes out of a new field scheme the fields Jira put in
// it that the spec does not list (Jira adds some to every new scheme), as
// space.fields is the space's complete list.
func removeOtherFields(ctx context.Context, snd Sender, scheme int, keep map[string]string) error {
	wanted := map[string]bool{}
	for _, id := range keep {
		wanted[id] = true
	}
	path := "/rest/api/3/config/fieldschemes/" + strconv.Itoa(scheme) + "/fields"
	others := map[string]any{}
	fields, err := pagedGet[struct {
		FieldID string   `json:"fieldId"`
		Allowed []string `json:"allowedOperations"`
	}](ctx, snd, path, nil)
	if err != nil {
		return err
	}
	for _, v := range fields {
		if !wanted[v.FieldID] && slices.Contains(v.Allowed, "REMOVE") { // Jira will not remove the others
			others[v.FieldID] = map[string]any{"schemeIds": []int{scheme}}
		}
	}
	if len(others) == 0 {
		return nil
	}
	return snd.Send(ctx, http.MethodDelete, "/rest/api/3/config/fieldschemes/fields", nil, others, nil)
}

// screenSchemeUsers are the issue type screen schemes that map an issue
// type to a screen scheme, from every page of the mappings.
func screenSchemeUsers(ctx context.Context, snd Sender, id string) ([]string, error) {
	mappings, err := pagedGet[struct {
		ITSS   string `json:"issueTypeScreenSchemeId"`
		Scheme string `json:"screenSchemeId"`
	}](ctx, snd, "/rest/api/3/issuetypescreenscheme/mapping", nil)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range mappings {
		if m.Scheme == id && !slices.Contains(out, m.ITSS) {
			out = append(out, m.ITSS)
		}
	}
	return out, nil
}

// fieldSchemeEntry is a field as a field scheme holds it: its association
// (with the issue types it is restricted to) and its settings, overall and
// per issue type.
func fieldSchemeEntry(env *Env, scheme int, f spec.SpaceField) (map[string]any, map[string]any, error) {
	item := map[string]any{"schemeIds": []int{scheme}}
	if f.IssueTypes != nil {
		typeIDs, err := env.issueTypeIDs(f.IssueTypes)
		if err != nil {
			return nil, nil, err
		}
		var ints []int
		for _, t := range typeIDs {
			n, _ := strconv.Atoi(t)
			ints = append(ints, n)
		}
		item["restrictedToWorkTypes"] = ints
	}
	p := map[string]any{"schemeIds": []int{scheme}, "parameters": fieldParameters(f.FieldSettings)}
	var per []map[string]any
	for _, b := range f.ByIssueType {
		tid, err := env.issueTypeID(b.IssueType)
		if err != nil {
			return nil, nil, err
		}
		w := fieldParameters(b.FieldSettings)
		w["workTypeId"], _ = strconv.Atoi(tid)
		per = append(per, w)
	}
	if per != nil {
		p["workTypeParameters"] = per
	}
	return item, p, nil
}

// knownIssueTypeID is the site ID of an issue type by name; unlike
// issueTypeID, an issue type the site lacks (and no earlier step makes) is
// an error, which is what a write needs.
func (e *Env) knownIssueTypeID(name string) (string, error) {
	id, err := e.issueTypeID(name)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("no issue type on the site is named %q", name)
	}
	return id, nil
}
