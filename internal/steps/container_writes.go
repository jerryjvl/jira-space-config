package steps

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// Changes to the space's managed schemes in place, when no other space uses
// them.

// updateIssueTypeScheme makes a managed issue type scheme's issue types,
// their order and its default the spec's. Removing one needs
// --allow-delete=issue_types; the switch's own count blocks it while work
// items have that issue type.
func updateIssueTypeScheme(env *Env, id string, gotNames []string, gotDef string, wantNames []string, wantDef string) change {
	ch := change{class: "additive", allow: "issue_types"}
	var add, remove []string
	for _, n := range wantNames {
		if !slices.Contains(gotNames, n) {
			add = append(add, n)
		}
	}
	var kept []string
	for _, n := range gotNames {
		if slices.Contains(wantNames, n) {
			kept = append(kept, n)
		} else {
			remove = append(remove, n)
			ch.removes = append(ch.removes, "issue type "+quote(n))
		}
	}
	reorder := !slices.Equal(append(kept, add...), wantNames)
	setDefault := wantDef != "" && wantDef != gotDef
	base := "/rest/api/3/issuetypescheme/{issueTypeSchemeId}"
	var requests []string
	if len(add) > 0 {
		requests = append(requests, "PUT "+base+"/issuetype")
	}
	if setDefault {
		requests = append(requests, "PUT "+base+" (default)")
	}
	for range remove {
		requests = append(requests, "DELETE "+base+"/issuetype/{issueTypeId}")
	}
	if reorder {
		requests = append(requests, "PUT "+base+"/issuetype/move")
	}
	switch {
	case len(remove) > 0:
		ch.class = "removing"
	case reorder || setDefault:
		ch.class = "altering"
	}
	ch.requests = countRequests(requests)
	ch.do = func(ctx context.Context, snd Sender) (string, error) {
		ids := func(names []string) ([]string, error) {
			var out []string
			for _, n := range names {
				tid, err := env.knownIssueTypeID(n)
				if err != nil {
					return nil, err
				}
				out = append(out, tid)
			}
			return out, nil
		}
		path := "/rest/api/3/issuetypescheme/" + id
		if len(add) > 0 {
			l, err := ids(add)
			if err != nil {
				return "", err
			}
			if err := snd.Send(ctx, http.MethodPut, path+"/issuetype", nil, map[string][]string{"issueTypeIds": l}, nil); err != nil {
				return "", err
			}
		}
		if setDefault {
			l, err := ids([]string{wantDef})
			if err != nil {
				return "", err
			}
			if err := snd.Send(ctx, http.MethodPut, path, nil, map[string]string{"defaultIssueTypeId": l[0]}, nil); err != nil {
				return "", err
			}
		}
		l, err := ids(remove)
		if err != nil {
			return "", err
		}
		for _, tid := range l {
			if err := snd.Send(ctx, http.MethodDelete, path+"/issuetype/"+tid, nil, nil, nil); err != nil {
				return "", err
			}
		}
		if reorder {
			l, err := ids(wantNames)
			if err != nil {
				return "", err
			}
			if err := snd.Send(ctx, http.MethodPut, path+"/issuetype/move", nil, map[string]any{"issueTypeIds": l, "position": "First"}, nil); err != nil {
				return "", err
			}
		}
		return id, nil
	}
	return ch
}

// updateScreenSchemes maps each of the spec's issue types whose screens
// differ to a screen scheme with those screens, named as a new issue type
// screen scheme's would be (screenScheme says how one is found or made).
func updateScreenSchemes(env *Env, x resolver, itss string, want, current []line) change {
	ch := change{class: "additive", requests: []string{
		"POST /rest/api/3/issuetypescreenscheme/{issueTypeScreenSchemeId}/mapping/remove (an issue type mapped elsewhere)",
		"PUT /rest/api/3/issuetypescreenscheme/{issueTypeScreenSchemeId}/mapping",
		"POST /rest/api/3/screenscheme (when the site has no screen scheme of the name)",
		"PUT /rest/api/3/screenscheme/{screenSchemeId} (when the tool's has other screens, and only this space uses it)",
	}}
	var changed []spec.SpaceIssueType
	for _, t := range env.Spec.Space.IssueTypes {
		if t.Screens == nil {
			continue
		}
		i := slices.IndexFunc(current, func(l line) bool { return l.key == t.Name })
		switch {
		case i < 0:
			changed = append(changed, t)
		case current[i].value != screenSetText(*t.Screens):
			changed = append(changed, t)
			ch.class = "altering"
		}
	}
	nameFor := func(text string) string {
		for _, t := range env.Spec.Space.IssueTypes {
			if t.Screens != nil && screenSetText(*t.Screens) == text {
				if t.Binding != nil && t.Binding.ScreenScheme != nil && t.Binding.ScreenScheme.Name != "" {
					return t.Binding.ScreenScheme.Name
				}
				return env.Spec.Space.ManagedPrefix + " " + t.Name + " Screen Scheme"
			}
		}
		return ""
	}
	ch.do = func(ctx context.Context, snd Sender) (string, error) {
		mapped, err := pagedGet[itssMapping](ctx, snd, "/rest/api/3/issuetypescreenscheme/mapping", url.Values{"issueTypeScreenSchemeId": {itss}})
		if err != nil {
			return "", err
		}
		if _, err := env.issueTypeID(""); err != nil { // reads the site's issue types for issueTypeName
			return "", err
		}
		var mappings []map[string]string
		var remapped []string
		for _, t := range changed {
			text := screenSetText(*t.Screens)
			// A screen scheme other issue types keep is not changed under them.
			mayChange := func(id string) error {
				for _, m := range mapped {
					if m.ScreenSchemeID != id {
						continue
					}
					if m.IssueTypeID == "default" {
						return fmt.Errorf("screen scheme %s is the default of the issue type screen scheme; name another for %q in its binding", id, t.Name)
					}
					name := env.issueTypeName(m.IssueTypeID)
					if !slices.ContainsFunc(changed, func(c spec.SpaceIssueType) bool { return c.Name == name && screenSetText(*c.Screens) == text }) {
						return fmt.Errorf("screen scheme %s is also used for issue type %q, whose screens stay; name another for %q in its binding", id, name, t.Name)
					}
				}
				return nil
			}
			sid, err := screenScheme(ctx, snd, x, nameFor(text), *t.Screens, itss, mayChange)
			if err != nil {
				return "", err
			}
			tid, err := env.knownIssueTypeID(t.Name)
			if err != nil {
				return "", err
			}
			now := slices.IndexFunc(mapped, func(m itssMapping) bool { return m.IssueTypeID == tid })
			switch {
			case now >= 0 && mapped[now].ScreenSchemeID == sid:
				continue // already mapped to it (its screens may just have changed)
			case now >= 0:
				remapped = append(remapped, tid) // Jira maps an issue type once: the old mapping goes first
			}
			mappings = append(mappings, map[string]string{"issueTypeId": tid, "screenSchemeId": sid})
		}
		path := "/rest/api/3/issuetypescreenscheme/" + itss + "/mapping"
		if len(remapped) > 0 {
			if err := snd.Send(ctx, http.MethodPost, path+"/remove", nil, map[string][]string{"issueTypeIds": remapped}, nil); err != nil {
				return "", err
			}
		}
		if len(mappings) > 0 {
			if err := snd.Send(ctx, http.MethodPut, path, nil, map[string]any{"issueTypeMappings": mappings}, nil); err != nil {
				return "", err
			}
		}
		return itss, nil
	}
	return ch
}

// updateFieldScheme makes a managed field scheme's fields the spec's: new
// fields are added with their settings, changed ones get the settings that
// differ (in space.fields an unset setting means its default, not "not
// compared"), and fields the spec
// leaves out are taken out (with --allow-delete=fields; fields Jira will
// not take out stay, as on a new scheme). The switch's own counts block a
// field leaving while work items have a value in it, and one becoming
// required while unresolved work items have it empty.
func updateFieldScheme(env *Env, x resolver, id string, want, got []spec.SpaceField, fixed []string) change {
	ch := change{class: "additive", allow: "fields"}
	find := func(l []spec.SpaceField, name string) *spec.SpaceField {
		for i := range l {
			if l[i].Name == name {
				return &l[i]
			}
		}
		return nil
	}
	var add, alter []spec.SpaceField
	params := map[string]map[string]any{}       // field → the parameters to send
	dropped := map[string]map[string][]string{} // field → issue type → the overrides it loses
	for _, w := range want {
		g := find(got, w.Name)
		if g == nil {
			add = append(add, w)
			continue
		}
		p := map[string]any{}
		if w.Required.V != g.Required.V {
			p["isRequired"] = w.Required.V
		}
		if cmp.Or(w.Renderer.V, "plain") != cmp.Or(g.Renderer.V, "plain") {
			p["rendererType"] = rendererKeys[cmp.Or(w.Renderer.V, "plain")]
		}
		if w.Description.V != g.Description.V {
			p["description"] = w.Description.V
		}
		restricted := !slices.Equal(w.IssueTypes, g.IssueTypes)
		if spec.Text(w) == spec.Text(*g) {
			continue
		}
		if gone := overridesGone(w, *g); len(gone) > 0 {
			dropped[w.Name] = gone
		}
		if len(p) > 0 || restricted || len(w.ByIssueType) > 0 || len(dropped[w.Name]) > 0 {
			alter = append(alter, w)
			params[w.Name] = p
		}
	}
	var remove []string
	for _, g := range got {
		if find(want, g.Name) == nil && !slices.Contains(fixed, g.Name) {
			remove = append(remove, g.Name)
			ch.removes = append(ch.removes, "field "+quote(g.Name))
		}
	}
	switch {
	case len(remove) > 0:
		ch.class = "removing"
	case len(alter) > 0:
		ch.class = "altering"
	}
	var requests []string
	if len(add) > 0 || len(alter) > 0 {
		requests = append(requests, "PUT /rest/api/3/config/fieldschemes/fields", "PUT /rest/api/3/config/fieldschemes/fields/parameters")
	}
	if len(dropped) > 0 {
		requests = append(requests, "DELETE /rest/api/3/config/fieldschemes/fields/parameters (settings per issue type the spec leaves out)")
	}
	if len(remove) > 0 {
		requests = append(requests, "DELETE /rest/api/3/config/fieldschemes/fields")
	}
	ch.requests = requests
	ch.do = func(ctx context.Context, snd Sender) (string, error) {
		scheme, _ := strconv.Atoi(id)
		fields := map[string]any{}
		all := map[string]any{}
		for _, list := range [][]spec.SpaceField{add, alter} {
			for _, f := range list {
				fid, err := x.field(f.Name)
				if err != nil {
					return "", err
				}
				item, p, err := fieldSchemeEntry(env, scheme, f)
				if err != nil {
					return "", err
				}
				fields[fid] = []any{item}
				if changed, ok := params[f.Name]; ok {
					for k, v := range changed {
						p["parameters"].(map[string]any)[k] = v
					}
				}
				if len(p["parameters"].(map[string]any)) == 0 {
					delete(p, "parameters") // Jira refuses an empty one ("At least one of the parameters is required")
				}
				if p["parameters"] != nil || p["workTypeParameters"] != nil {
					all[fid] = []any{p}
				}
			}
		}
		if len(fields) > 0 {
			if err := snd.Send(ctx, http.MethodPut, "/rest/api/3/config/fieldschemes/fields", nil, fields, nil); err != nil {
				return "", err
			}
		}
		if len(all) > 0 {
			if err := snd.Send(ctx, http.MethodPut, "/rest/api/3/config/fieldschemes/fields/parameters", nil, all, nil); err != nil {
				return "", err
			}
		}
		if len(dropped) > 0 {
			body := map[string]any{}
			for _, name := range sortedKeys(dropped) {
				fid, err := x.field(name)
				if err != nil {
					return "", err
				}
				var entries []map[string]any
				for _, t := range sortedKeys(dropped[name]) {
					tid, err := env.issueTypeIDs([]string{t})
					if err != nil {
						return "", err
					}
					n, _ := strconv.Atoi(tid[0])
					entries = append(entries, map[string]any{"schemeId": scheme, "workTypeIds": []int{n}, "parameters": dropped[name][t]})
				}
				body[fid] = entries
			}
			if err := snd.Send(ctx, http.MethodDelete, "/rest/api/3/config/fieldschemes/fields/parameters", nil, body, nil); err != nil {
				return "", err
			}
		}
		if len(remove) > 0 {
			gone := map[string]any{}
			for _, name := range remove {
				fid, err := x.field(name)
				if err != nil {
					return "", err
				}
				gone[fid] = map[string]any{"schemeIds": []int{scheme}}
			}
			if err := snd.Send(ctx, http.MethodDelete, "/rest/api/3/config/fieldschemes/fields", nil, gone, nil); err != nil {
				return "", err
			}
		}
		return id, nil
	}
	return ch
}

// itssMapping is one mapping of an issue type screen scheme.
type itssMapping struct {
	IssueTypeID    string `json:"issueTypeId"`
	ScreenSchemeID string `json:"screenSchemeId"`
}

// overridesGone are the settings Jira's per issue type overrides of a field
// have and the spec's do not, by issue type: the override is taken away,
// and the issue type has the field's own setting again.
func overridesGone(want, got spec.SpaceField) map[string][]string {
	out := map[string][]string{}
	for _, g := range got.ByIssueType {
		var w spec.FieldSettings
		for _, b := range want.ByIssueType {
			if b.IssueType == g.IssueType {
				w = b.FieldSettings
			}
		}
		var names []string
		if g.Required.Set && !w.Required.Set {
			names = append(names, "isRequired")
		}
		if g.Renderer.Set && !w.Renderer.Set {
			names = append(names, "rendererType")
		}
		if g.Description.Set && !w.Description.Set {
			names = append(names, "description")
		}
		if names != nil {
			out[g.IssueType] = names
		}
	}
	return out
}
