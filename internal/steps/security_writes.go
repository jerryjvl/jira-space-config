package steps

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// Changes in place to a space's managed issue security scheme: its levels and their members.

// securityPlan is how a managed issue security scheme's levels change,
// planned on names; carryOut looks the level IDs up when it runs.
type securityPlan struct {
	scheme    string               // the scheme's ID
	added     []spec.SecurityLevel // levels the scheme gains
	members   []memberChange       // members existing levels gain or lose
	described []spec.SecurityLevel // levels whose description changes
	def       string               // the new default level, or "" when it stays
	removed   []string             // levels the scheme loses
}

// memberChange is the members one level gains and loses.
type memberChange struct {
	level       string
	add, remove []string
}

// updateSecurityScheme gives a managed issue security scheme the spec's
// levels: new levels are added; a member added to an existing level widens
// who sees its work items (--allow-update=issue_security); members and
// levels taken away need --allow-delete=issue_security, and step 19's
// counts block a level while work items have it. Descriptions and the
// default are changed as the spec gives them.
func updateSecurityScheme(env *Env, x resolver, id string, want, got []spec.SecurityLevel) change {
	for _, l := range want {
		for _, h := range l.Holders {
			if kind, _, _ := strings.Cut(h, ":"); spec.HolderTypes[kind].Security == "" {
				return change{problem: fmt.Sprintf("level %q has holder %s, which Jira does not accept in a security level", l.Name, h),
					fix: "Use another holder (a role, a group or a user), or set the level up by hand."}
			}
		}
	}
	ch := change{class: "additive", allow: "issue_security", widenAllow: "issue_security"}
	p := securityPlan{scheme: id}
	find := func(l []spec.SecurityLevel, name string) *spec.SecurityLevel {
		for i := range l {
			if l[i].Name == name {
				return &l[i]
			}
		}
		return nil
	}
	newDef, oldDef := "", ""
	for _, w := range want {
		if w.Default {
			newDef = w.Name
		}
		g := find(got, w.Name)
		if g == nil {
			p.added = append(p.added, w)
			continue
		}
		c := memberChange{level: w.Name}
		c.add, c.remove = addedAndRemoved(w.Holders, g.Holders)
		for _, h := range c.add {
			ch.widens = append(ch.widens, fmt.Sprintf("%s on level %q", h, w.Name))
		}
		for _, h := range c.remove {
			ch.removes = append(ch.removes, fmt.Sprintf("member %s of level %q", h, w.Name))
		}
		if c.add != nil || c.remove != nil {
			p.members = append(p.members, c)
		}
		if show(w.Description) != show(g.Description) {
			p.described = append(p.described, w)
			ch.class = "altering"
		}
	}
	for _, g := range got {
		if g.Default {
			oldDef = g.Name
		}
		if find(want, g.Name) == nil {
			p.removed = append(p.removed, g.Name)
			ch.removes = append(ch.removes, "level "+quote(g.Name))
		}
	}
	if newDef != oldDef && newDef != "" {
		p.def = newDef
		ch.class = "altering"
	}
	if len(ch.removes) > 0 {
		ch.class = "removing"
	}
	ch.requests = countRequests(p.requests())
	ch.do = func(ctx context.Context, snd Sender) (string, error) {
		return id, p.carryOut(ctx, snd, x)
	}
	return ch
}

// requests are the writes the plan makes, in the order carryOut makes them.
func (p securityPlan) requests() []string {
	base := "/rest/api/3/issuesecurityschemes/{schemeId}/level"
	var out []string
	if len(p.added) > 0 {
		out = append(out, "PUT "+base)
	}
	for _, c := range p.members {
		if c.add != nil {
			out = append(out, "PUT "+base+"/{levelId}/member")
		}
		for range c.remove {
			out = append(out, "DELETE "+base+"/{levelId}/member/{memberId}")
		}
	}
	for range p.described {
		out = append(out, "PUT "+base+"/{levelId}")
	}
	if p.def != "" {
		out = append(out, "PUT /rest/api/3/issuesecurityschemes/level/default")
	}
	for range p.removed {
		out = append(out, "DELETE "+base+"/{levelId}")
	}
	return out
}

// carryOut makes the changes: new levels first, then members, descriptions
// and the default of existing ones, and last the levels the scheme loses.
// Every level is found by name as the scheme is now; one that has gone since
// the step was evaluated stops the change.
func (p securityPlan) carryOut(ctx context.Context, snd Sender, x resolver) error {
	path := "/rest/api/3/issuesecurityschemes/" + p.scheme + "/level"
	if len(p.added) > 0 {
		var levels []map[string]any
		for _, l := range p.added {
			m, err := membersOf(x, l.Holders)
			if err != nil {
				return err
			}
			level := map[string]any{"name": l.Name, "isDefault": false, "members": m}
			if l.Description.Has() {
				level["description"] = l.Description.V
			}
			levels = append(levels, level)
		}
		if err := snd.Send(ctx, http.MethodPut, path, nil, map[string]any{"levels": levels}, nil); err != nil {
			return err
		}
	}
	ids, err := p.levelIDs(ctx, snd)
	if err != nil {
		return err
	}
	level := func(name string) (string, error) {
		if lid, ok := ids[name]; ok {
			return lid, nil
		}
		return "", fmt.Errorf("the scheme has no level %q now; it was changed since this step was evaluated", name)
	}
	for _, c := range p.members {
		lid, err := level(c.level)
		if err != nil {
			return err
		}
		if err := p.changeMembers(ctx, snd, x, lid, c); err != nil {
			return err
		}
	}
	for _, l := range p.described {
		lid, err := level(l.Name)
		if err != nil {
			return err
		}
		if err := snd.Send(ctx, http.MethodPut, path+"/"+lid, nil, map[string]string{"description": l.Description.V}, nil); err != nil {
			return err
		}
	}
	if p.def != "" {
		lid, err := level(p.def)
		if err != nil {
			return err
		}
		body := map[string]any{"defaultValues": []map[string]string{{"issueSecuritySchemeId": p.scheme, "defaultLevelId": lid}}}
		if err := snd.Send(ctx, http.MethodPut, "/rest/api/3/issuesecurityschemes/level/default", nil, body, nil); err != nil {
			return err
		}
	}
	for _, name := range p.removed {
		lid, err := level(name)
		if err != nil {
			return err
		}
		// Jira deletes a level in a task (303), done when the level is gone.
		if err := snd.Send(ctx, http.MethodDelete, path+"/"+lid, nil, nil, nil); err != nil && !taskStarted(err) {
			return err
		}
		if err := waitUntil(ctx, fmt.Sprintf("deleting level %q", name), func() (bool, error) {
			now, err := p.levelIDs(ctx, snd)
			_, there := now[name]
			return !there, err
		}); err != nil {
			return err
		}
	}
	return nil
}

// levelIDs are the scheme's level IDs by name, as Jira has them now.
func (p securityPlan) levelIDs(ctx context.Context, snd Sender) (map[string]string, error) {
	levels, err := pagedGet[struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}](ctx, snd, "/rest/api/3/issuesecurityschemes/level", url.Values{"schemeId": {p.scheme}})
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, l := range levels {
		out[l.Name] = l.ID
	}
	return out, nil
}

// changeMembers adds and removes one level's members. A member to remove is
// matched to Jira's by type and by value, parameter or group name, as Jira
// reports holders in all three ways.
func (p securityPlan) changeMembers(ctx context.Context, snd Sender, x resolver, lid string, c memberChange) error {
	path := "/rest/api/3/issuesecurityschemes/" + p.scheme + "/level/" + lid + "/member"
	if c.add != nil {
		m, err := membersOf(x, c.add)
		if err != nil {
			return err
		}
		if err := snd.Send(ctx, http.MethodPut, path, nil, map[string]any{"members": m}, nil); err != nil {
			return err
		}
	}
	if c.remove == nil {
		return nil
	}
	members, err := pagedGet[struct {
		ID     string `json:"id"`
		Holder struct {
			Type      string `json:"type"`
			Parameter string `json:"parameter"`
			Value     string `json:"value"`
			Group     *struct {
				Name string `json:"name"`
			} `json:"group"`
		} `json:"holder"`
	}](ctx, snd, "/rest/api/3/issuesecurityschemes/level/member", url.Values{"schemeId": {p.scheme}, "levelId": {lid}})
	if err != nil {
		return err
	}
	for _, h := range c.remove {
		typ, value, err := x.holder(h, spec.PlaceSecurity)
		if err != nil {
			return err
		}
		found := ""
		for _, m := range members {
			group := ""
			if m.Holder.Group != nil {
				group = m.Holder.Group.Name
			}
			if strings.EqualFold(m.Holder.Type, typ) && (value == "" || m.Holder.Parameter == value || m.Holder.Value == value || group == value) {
				found = m.ID
			}
		}
		if found == "" {
			return fmt.Errorf("level %q no longer has member %s; it was changed since this step was evaluated", c.level, h)
		}
		if err := snd.Send(ctx, http.MethodDelete, path+"/"+found, nil, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// membersOf are holders as a security level's members.
func membersOf(x resolver, holders []string) ([]map[string]string, error) {
	var out []map[string]string
	for _, h := range holders {
		m, err := x.holderBody(h, spec.PlaceSecurity)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}
