package reader

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/spec"
	"github.com/jerryjvl/jira-space-config/internal/util"
)

// Slice 5: who may do what (permissions, notifications, issue security),
// role types and members, components, versions and space properties.

// holder is a permission or notification holder as Jira returns it.
type holder struct {
	Type      string `json:"type"`
	Parameter string `json:"parameter"`
	Value     string `json:"value"`
	Group     *struct {
		Name string `json:"name"`
	} `json:"group"`
}

// holderKinds maps Jira's holder, notification and security member types to
// the format's holder kinds, from the table apply writes with
// (spec.HolderTypes). Security members spell some types in lower case
// (projectrole), so lookups ignore case.
var holderKinds = func() map[string]struct {
	kind  string
	value bool
} {
	out := map[string]struct {
		kind  string
		value bool
	}{}
	for kind, t := range spec.HolderTypes {
		for _, name := range []string{t.Permission, t.Notification, t.Security} {
			if name != "" {
				out[strings.ToLower(name)] = struct {
					kind  string
					value bool
				}{kind, t.Value}
			}
		}
	}
	return out
}()

// holderText converts one holder to kind[:value], resolving role IDs and
// field IDs to names. For holders the format has no kind for, it notes them
// and reports false.
func (r *Reader) holderText(typ, parameter string, group string, where string) (string, bool) {
	k, ok := holderKinds[strings.ToLower(typ)]
	if !ok {
		r.note("%s grants to a holder of type %q, which the format has no name for; it is left out.", where, typ)
		return "", false
	}
	if !k.value || parameter == "" {
		return k.kind, true // application_role without a value: any user with product access
	}
	value := parameter
	switch k.kind {
	case "role":
		value = r.resolve(spec.RefRole, parameter, where)
	case "group":
		// Jira puts the group's name in parameter (its ID is in value).
		if group != "" {
			value = group
		}
	case "user_field", "group_field":
		value = r.resolve(spec.RefField, parameter, where)
	}
	return k.kind + ":" + value, true
}

// PermissionScheme reads the space's permission scheme.
func (r *Reader) PermissionScheme(p *Project) (*spec.Permissions, error) {
	var scheme permissionScheme
	if err := r.get("/rest/api/3/project/"+url.PathEscape(p.Key)+"/permissionscheme", url.Values{"expand": {"all"}}, &scheme); err != nil {
		return nil, fmt.Errorf("reading the permission scheme: %w", err)
	}
	return r.permissions(scheme)
}

// permissionScheme is a permission scheme as Jira returns it, with its grants.
type permissionScheme struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Permissions []struct {
		Permission string `json:"permission"`
		Holder     holder `json:"holder"`
	} `json:"permissions"`
}

func (r *Reader) permissions(scheme permissionScheme) (*spec.Permissions, error) {
	byPermission := map[string][]string{}
	for _, g := range scheme.Permissions {
		group := ""
		if g.Holder.Group != nil {
			group = g.Holder.Group.Name
		}
		where := fmt.Sprintf("Permission %s in scheme %q", g.Permission, scheme.Name)
		if h, ok := r.holderText(g.Holder.Type, g.Holder.Parameter, group, where); ok {
			byPermission[g.Permission] = util.AppendNew(byPermission[g.Permission], h)
		}
	}
	perms := spec.Permissions{Binding: containerRef(strconv.Itoa(scheme.ID), scheme.Name, scheme.Description)}
	for _, key := range sortedKeys(byPermission) {
		perms.Grants = append(perms.Grants, spec.Grant{Permission: key, Holders: byPermission[key]})
	}
	return &perms, r.done()
}

// NotificationScheme reads the space's notification scheme, or nil when it
// has none (Jira answers 404).
func (r *Reader) NotificationScheme(p *Project) (*spec.Notifications, error) {
	var scheme notificationScheme
	err := r.get("/rest/api/3/project/"+url.PathEscape(p.Key)+"/notificationscheme", url.Values{"expand": {"all"}}, &scheme)
	var apiErr *jira.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return nil, nil // a space created through the API may have none
	}
	if err != nil {
		return nil, fmt.Errorf("reading the notification scheme: %w", err)
	}
	return r.notifications(scheme)
}

// notificationScheme is a notification scheme as Jira returns it, with its events.
type notificationScheme struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Events      []struct {
		Event struct {
			Name string `json:"name"`
		} `json:"event"`
		Notifications []struct {
			Type      string `json:"notificationType"`
			Parameter string `json:"parameter"`
			Group     *struct {
				Name string `json:"name"`
			} `json:"group"`
		} `json:"notifications"`
	} `json:"notificationSchemeEvents"`
}

func (r *Reader) notifications(scheme notificationScheme) (*spec.Notifications, error) {
	n := spec.Notifications{Binding: containerRef(strconv.Itoa(scheme.ID), scheme.Name, scheme.Description)}
	for _, ev := range scheme.Events {
		g := spec.Grant{Permission: ev.Event.Name}
		for _, nt := range ev.Notifications {
			group := ""
			if nt.Group != nil {
				group = nt.Group.Name
			}
			where := fmt.Sprintf("Event %q in notification scheme %q", ev.Event.Name, scheme.Name)
			if h, ok := r.holderText(nt.Type, nt.Parameter, group, where); ok {
				g.Holders = util.AppendNew(g.Holders, h)
			}
		}
		n.Events = append(n.Events, g)
	}
	return &n, r.done()
}

// IssueSecurity reads the space's issue security scheme, or nil when it has
// none (Jira answers 404).
func (r *Reader) IssueSecurity(p *Project) (*spec.IssueSecurity, error) {
	var scheme securityScheme
	err := r.get("/rest/api/3/project/"+url.PathEscape(p.Key)+"/issuesecuritylevelscheme", nil, &scheme)
	var apiErr *jira.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the issue security scheme: %w", err)
	}
	return r.issueSecurityScheme(scheme)
}

// securityScheme names an issue security scheme.
type securityScheme struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// issueSecurityScheme reads an issue security scheme's levels and members.
func (r *Reader) issueSecurityScheme(scheme securityScheme) (*spec.IssueSecurity, error) {
	is := spec.IssueSecurity{Binding: containerRef(strconv.Itoa(scheme.ID), scheme.Name, scheme.Description)}
	schemeID := url.Values{"schemeId": {strconv.Itoa(scheme.ID)}}
	type level struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		IsDefault   bool   `json:"isDefault"`
	}
	levels, err := jira.GetAll[level](r.ctx, r.c, "/rest/api/3/issuesecurityschemes/level", schemeID)
	if err != nil {
		return nil, fmt.Errorf("reading the levels of issue security scheme %q: %w", scheme.Name, err)
	}
	type member struct {
		LevelID string `json:"issueSecurityLevelId"`
		Holder  holder `json:"holder"`
	}
	members, err := jira.GetAll[member](r.ctx, r.c, "/rest/api/3/issuesecurityschemes/level/member", schemeID)
	if err != nil {
		return nil, fmt.Errorf("reading the members of issue security scheme %q: %w", scheme.Name, err)
	}
	for _, l := range levels {
		sl := spec.SecurityLevel{Name: l.Name, Description: optText(l.Description), Default: l.IsDefault}
		for _, m := range members {
			if m.LevelID != l.ID {
				continue
			}
			group := ""
			if m.Holder.Group != nil {
				group = m.Holder.Group.Name
			}
			where := fmt.Sprintf("Security level %q of scheme %q", l.Name, scheme.Name)
			if h, ok := r.holderText(m.Holder.Type, m.Holder.Parameter, group, where); ok {
				sl.Holders = util.AppendNew(sl.Holders, h)
			}
		}
		is.Levels = append(is.Levels, sl)
	}
	return &is, r.done()
}

// Roles are the role types used in the space and their members in it.
type Roles struct {
	Definitions []spec.Role // global role types, reused
	Members     []spec.RoleMembers
}

// Roles reads the role types used in the space and the members each has in it.
func (r *Reader) Roles(p *Project) (*Roles, error) {
	var links map[string]string // role name → self URL ending in /role/{id}
	if err := r.get("/rest/api/3/project/"+url.PathEscape(p.Key)+"/role", nil, &links); err != nil {
		return nil, fmt.Errorf("reading the space's roles: %w", err)
	}
	all, err := r.loadRoleTypes()
	if err != nil {
		return nil, fmt.Errorf("reading role types: %w", err)
	}
	out := &Roles{}
	for _, name := range sortedKeys(links) {
		// Role names are not unique, even among global roles, so the space's
		// own links (ending in /role/{id}) decide which role is meant.
		m := roleIDPattern.FindStringSubmatch(links[name])
		if m == nil {
			return nil, fmt.Errorf("role %q has an unexpected link %q", name, links[name])
		}
		for _, rt := range all {
			if strconv.Itoa(rt.ID) != m[1] {
				if rt.Name == name && rt.Scope == nil {
					r.note("Role type %q (id %d) has the same name as role %s used here; the ID in the spec tells them apart.", rt.Name, rt.ID, m[1])
				}
				continue
			}
			description, mode := globalOwnership(rt.Description)
			out.Definitions = append(out.Definitions, spec.Role{
				GlobalObject:   spec.GlobalObject{Meta: spec.Meta{ID: strconv.Itoa(rt.ID)}, Name: rt.Name, Mode: mode, Description: description},
				DefaultMembers: r.actorHolders(rt.Actors, fmt.Sprintf("Default members of role %q", rt.Name)),
			})

			var role struct {
				Actors []roleActor `json:"actors"`
			}
			if err := r.get(fmt.Sprintf("/rest/api/3/project/%s/role/%d", url.PathEscape(p.Key), rt.ID), nil, &role); err != nil {
				return nil, fmt.Errorf("reading members of role %q: %w", rt.Name, err)
			}
			holders := r.actorHolders(role.Actors, fmt.Sprintf("Role %q in the space", rt.Name))
			if len(holders) > 0 {
				out.Members = append(out.Members, spec.RoleMembers{Role: rt.Name, Holders: holders})
			}
		}
	}
	return out, r.done()
}

// Components reads the space's Jira components.
func (r *Reader) Components(p *Project) ([]spec.Component, error) {
	type component struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Description  string `json:"description"`
		AssigneeType string `json:"assigneeType"`
		Lead         *struct {
			AccountID string `json:"accountId"`
		} `json:"lead"`
	}
	// Jira components explicitly: the endpoint can also return Compass components.
	all, err := jira.GetAll[component](r.ctx, r.c, "/rest/api/3/project/"+url.PathEscape(p.Key)+"/component",
		url.Values{"componentSource": {"jira"}})
	if err != nil {
		return nil, fmt.Errorf("reading components: %w", err)
	}
	var out []spec.Component
	for _, c := range all {
		sc := spec.Component{Meta: spec.Meta{ID: c.ID}, Name: c.Name, Description: optText(c.Description), Lead: nullable("")}
		if c.Lead != nil && c.Lead.AccountID != "" {
			sc.Lead = spec.Some("user:" + c.Lead.AccountID)
		}
		if t, ok := componentAssignee[c.AssigneeType]; ok {
			sc.AssigneeType = spec.Some(t)
		}
		out = append(out, sc)
	}
	return out, nil
}

var componentAssignee = map[string]string{
	"PROJECT_DEFAULT": "space_default", "COMPONENT_LEAD": "component_lead",
	"PROJECT_LEAD": "space_lead", "UNASSIGNED": "unassigned",
}

// Versions reads the space's versions.
func (r *Reader) Versions(p *Project) ([]spec.Version, error) {
	type version struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		StartDate   string `json:"startDate"`
		ReleaseDate string `json:"releaseDate"`
		Released    bool   `json:"released"`
		Archived    bool   `json:"archived"`
	}
	all, err := jira.GetAll[version](r.ctx, r.c, "/rest/api/3/project/"+url.PathEscape(p.Key)+"/version", nil)
	if err != nil {
		return nil, fmt.Errorf("reading versions: %w", err)
	}
	var out []spec.Version
	for _, v := range all {
		out = append(out, spec.Version{
			Meta: spec.Meta{ID: v.ID}, Name: v.Name, Description: optText(v.Description),
			StartDate: nullable(v.StartDate), ReleaseDate: nullable(v.ReleaseDate),
			Released: spec.Some(v.Released), Archived: spec.Some(v.Archived),
		})
	}
	return out, nil
}

// Properties reads the space's entity properties, values as written by
// whoever set them (apps, Jira itself).
func (r *Reader) Properties(p *Project) ([]spec.RawProperty, error) {
	var keys struct {
		Keys []struct {
			Key string `json:"key"`
		} `json:"keys"`
	}
	base := "/rest/api/3/project/" + url.PathEscape(p.Key) + "/properties"
	if err := r.get(base, nil, &keys); err != nil {
		return nil, fmt.Errorf("reading space properties: %w", err)
	}
	var out []spec.RawProperty
	for _, k := range keys.Keys {
		var prop struct {
			Value any `json:"value"`
		}
		if err := r.get(base+"/"+url.PathEscape(k.Key), nil, &prop); err != nil {
			return nil, fmt.Errorf("reading space property %q: %w", k.Key, err)
		}
		out = append(out, spec.RawProperty{Key: k.Key, Value: rawValue(prop.Value)})
	}
	return out, nil
}

// rawValue turns decoded JSON into the format's raw value forms, with object
// keys sorted so exports are stable.
func rawValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		return spec.ScalarValue{Tag: "!!str", Value: x}
	case bool:
		return spec.ScalarValue{Tag: "!!bool", Value: strconv.FormatBool(x)}
	case float64:
		if x == float64(int64(x)) {
			return spec.ScalarValue{Tag: "!!int", Value: strconv.FormatInt(int64(x), 10)}
		}
		return spec.ScalarValue{Tag: "!!float", Value: strconv.FormatFloat(x, 'f', -1, 64)}
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = rawValue(item)
		}
		return out
	case map[string]any:
		out := make([]spec.KVAny, 0, len(x))
		for _, k := range sortedKeys(x) {
			out = append(out, spec.KVAny{Key: k, Value: rawValue(x[k])})
		}
		return out
	}
	b, _ := json.Marshal(v)
	return spec.ScalarValue{Tag: "!!str", Value: string(b)}
}

var roleIDPattern = regexp.MustCompile(`/role/(\d+)$`)

// roleActor is a member of a role, in a space or as a default member.
type roleActor struct {
	Type      string `json:"type"`
	ActorUser *struct {
		AccountID string `json:"accountId"`
	} `json:"actorUser"`
	ActorGroup *struct {
		Name string `json:"name"`
	} `json:"actorGroup"`
}

func (r *Reader) actorHolders(actors []roleActor, where string) []string {
	var holders []string
	for _, a := range actors {
		switch {
		case a.ActorUser != nil && a.ActorUser.AccountID != "":
			holders = util.AppendNew(holders, "user:"+a.ActorUser.AccountID)
		case a.ActorGroup != nil && a.ActorGroup.Name != "":
			holders = util.AppendNew(holders, "group:"+a.ActorGroup.Name)
		default:
			r.note("%s include a member of type %q, which the spec cannot represent.", where, a.Type)
		}
	}
	return holders
}
