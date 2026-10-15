package steps

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// Automation rules (step 28): created when missing, and changed while they
// cover this space alone, with the rule body passed through as Jira holds
// it (docs/automation.md).

func ruleObject(r spec.AutomationRule) spec.GlobalObject {
	return spec.GlobalObject{Meta: r.Meta, Name: r.Name, Mode: r.Mode, Description: r.Description}
}

func ruleItems(env *Env) ([]Item, error) {
	live, err := env.R.AutomationRules(env.P)
	if err != nil {
		return nil, err
	}
	ari, err := env.R.SpaceARI(env.P)
	if err != nil {
		return nil, err
	}
	return globals(env, kind[spec.AutomationRule]{
		noun: "automation rule", plural: "automation rules", shareable: true, anyone: true, object: ruleObject,
		compare: compareRules,
		create:  func(r spec.AutomationRule) string { return "new rule · " + cmp.Or(r.State, "enabled") },
		request: "POST /automation/public/jira/{cloudId}/rest/v1/rule",
		do:      func(r spec.AutomationRule) action { return writeRule(env, ari, r, "") },
		update: func(w, g spec.AutomationRule) change {
			ch := change{class: "altering", requests: []string{"PUT /automation/public/jira/{cloudId}/rest/v1/rule/{ruleUuid}",
				"PUT /automation/public/jira/{cloudId}/rest/v1/rule/{ruleUuid}/state"}}
			ch.do = writeRule(env, ari, w, g.ID)
			return ch
		},
		usage: func(g spec.AutomationRule) ([]string, error) {
			if g.Scope == "space" {
				return []string{env.P.ID}, nil
			}
			return []string{"other spaces, or the whole site"}, nil
		},
	}, env.Spec.AutomationRules, live)
}

// compareRules compares what the spec states: the state (enabled when not
// given), the scope, and the rule body, by its JSON, without its
// components' IDs.
func compareRules(want, got spec.AutomationRule) []Diff {
	var l diffList
	l.add(compareText("state", spec.Some(cmp.Or(want.State, "enabled")), spec.Some(got.State)))
	l.add(compareText("scope", spec.Some(cmp.Or(want.Scope, "space")), spec.Some(got.Scope)))
	if path, w, g, differs := firstDifference("rule", withoutIDs(spec.JSONValue(want.Rule)), withoutIDs(spec.JSONValue(got.Rule))); differs {
		l = append(l, Diff{Attribute: path, Spec: w, Jira: g})
	}
	return l
}

// firstDifference finds where two JSON values first differ, as a path
// ("rule.components[0].value.jql"), with each side's text there.
func firstDifference(path string, a, b any) (string, string, string, bool) {
	switch x := a.(type) {
	case map[string]any:
		if y, ok := b.(map[string]any); ok {
			keys := map[string]bool{}
			for k := range x {
				keys[k] = true
			}
			for k := range y {
				keys[k] = true
			}
			var sorted []string
			for k := range keys {
				sorted = append(sorted, k)
			}
			sort.Strings(sorted)
			for _, k := range sorted {
				if p, w, g, d := firstDifference(path+"."+k, x[k], y[k]); d {
					return p, w, g, d
				}
			}
			return "", "", "", false
		}
	case []any:
		if y, ok := b.([]any); ok && len(x) == len(y) {
			for i := range x {
				if p, w, g, d := firstDifference(fmt.Sprintf("%s[%d]", path, i), x[i], y[i]); d {
					return p, w, g, d
				}
			}
			return "", "", "", false
		}
	}
	wa, _ := json.Marshal(a)
	wb, _ := json.Marshal(b)
	if string(wa) == string(wb) {
		return "", "", "", false
	}
	text := func(s []byte) string {
		if string(s) == "null" {
			return "none"
		}
		return string(s)
	}
	return path, text(wa), text(wb), true
}

// rulePayload is a rule as Jira takes it: the opaque body, with the
// format's name, description (with the marker), state and the space as
// its scope; connections go beside the rule.
func rulePayload(ari string, r spec.AutomationRule) map[string]any {
	body, _ := spec.JSONValue(r.Rule).(map[string]any)
	rule := map[string]any{}
	for k, v := range body {
		if k != "connections" {
			rule[k] = v
		}
	}
	rule["name"] = r.Name
	rule["description"] = managedDescription(r.Description)
	rule["state"] = strings.ToUpper(cmp.Or(r.State, "enabled"))
	rule["ruleScopeARIs"] = []string{ari}
	out := map[string]any{"rule": rule}
	if c, ok := body["connections"]; ok {
		out["connections"] = c
	}
	return out
}

// writeRule creates a rule (id "") or replaces one; the state is set as
// well, as a rule's state has an endpoint of its own.
func writeRule(env *Env, ari string, r spec.AutomationRule, id string) action {
	return func(ctx context.Context, snd Sender) (string, error) {
		payload := rulePayload(ari, r)
		rule := payload["rule"].(map[string]any)
		if id == "" {
			path, err := env.R.AutomationPath("/rule")
			if err != nil {
				return "", err
			}
			// Jira requires the author; a new rule's is the account apply runs as.
			var me struct {
				AccountID string `json:"accountId"`
			}
			if err := snd.Send(ctx, http.MethodGet, "/rest/api/3/myself", nil, nil, &me); err != nil {
				return "", err
			}
			rule["authorAccountId"] = me.AccountID
			withIDs(rule, nil)
			renumberConnections(payload, nil)
			var out struct {
				UUID string `json:"ruleUuid"`
			}
			if err := snd.Send(ctx, http.MethodPost, path, nil, payload, &out); err != nil {
				return "", err
			}
			return out.UUID, nil
		}
		path, err := env.R.AutomationPath("/rule/" + id)
		if err != nil {
			return "", err
		}
		// A changed rule keeps its author, and its components' IDs.
		var current struct {
			Rule        map[string]any `json:"rule"`
			Connections []struct {
				ID string `json:"id"`
			} `json:"connections"`
		}
		if err := snd.Send(ctx, http.MethodGet, path, nil, nil, &current); err != nil {
			return "", err
		}
		rule["authorAccountId"] = current.Rule["authorAccountId"]
		withIDs(rule, current.Rule)
		have := map[string]bool{}
		for _, c := range current.Connections {
			have[c.ID] = true
		}
		renumberConnections(payload, have)
		if err := snd.Send(ctx, http.MethodPut, path, nil, payload, nil); err != nil {
			return "", err
		}
		state := map[string]string{"value": strings.ToUpper(cmp.Or(r.State, "enabled"))}
		if err := snd.Send(ctx, http.MethodPut, path+"/state", nil, state, nil); err != nil {
			return "", err
		}
		return id, nil
	}
}

// componentKeys are a rule component's links within its rule: its own ID,
// and its parent's. Jira gives them (a new rule gets new ones whatever is
// sent; a changed rule must keep its own), so the format leaves them out.
var componentKeys = []string{"id", "parentId", "conditionParentId"}

// withoutIDs is a rule body without its components' IDs, to compare by.
func withoutIDs(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		_, isComponent := x["component"]
		_, isConnection := x["connectionTargetKey"]
		for k, val := range x {
			switch {
			case isComponent && slices.Contains(componentKeys, k), isConnection && k == "id":
				continue
			case isComponent && k == "connectionId" && val != nil:
				out[k] = "<connection>" // which one is Jira's to number
			default:
				out[k] = withoutIDs(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = withoutIDs(item)
		}
		return out
	}
	return v
}

// withIDs gives a rule body's components IDs for a write: the existing
// rule's where its components are of the same type in the same place, new
// ones elsewhere, and each child its parent's.
func withIDs(want, existing map[string]any) {
	var walk func(w, g map[string]any, parent, conditionParent any)
	walk = func(w, g map[string]any, parent, conditionParent any) {
		id := newUUID()
		if g != nil && g["type"] == w["type"] && g["id"] != nil {
			id = g["id"].(string)
		}
		w["id"], w["parentId"], w["conditionParentId"] = id, parent, conditionParent
		for _, list := range []struct {
			key             string
			parent, condPar any
		}{{"children", id, nil}, {"conditions", nil, id}} {
			ws, _ := w[list.key].([]any)
			var gs []any
			if g != nil {
				gs, _ = g[list.key].([]any)
			}
			for i, c := range ws {
				cw, ok := c.(map[string]any)
				if !ok {
					continue
				}
				var cg map[string]any
				if i < len(gs) {
					cg, _ = gs[i].(map[string]any)
				}
				walk(cw, cg, list.parent, list.condPar)
			}
		}
	}
	if t, ok := want["trigger"].(map[string]any); ok {
		g, _ := existing["trigger"].(map[string]any)
		walk(t, g, nil, nil)
		delete(t, "parentId")
		delete(t, "conditionParentId")
	}
	ws, _ := want["components"].([]any)
	gs, _ := existing["components"].([]any)
	for i, c := range ws {
		cw, ok := c.(map[string]any)
		if !ok {
			continue
		}
		var cg map[string]any
		if i < len(gs) {
			cg, _ = gs[i].(map[string]any)
		}
		walk(cw, cg, nil, nil)
	}
}

// newUUID is a random (version 4) UUID.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// renumberConnections gives a rule's connections IDs for a write, and points
// the components that use them at the new IDs. Jira wants the caller to make
// them; a connection the rule already has (keep) keeps its own and is not
// listed again, as Jira refuses it as a new one. The server's
// bookkeeping fields are left out.
func renumberConnections(payload map[string]any, keep map[string]bool) {
	conns, _ := payload["connections"].([]any)
	renamed := map[string]string{}
	listed := []any{} // the new ones
	for _, c := range conns {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		delete(m, "container")
		delete(m, "createdAt")
		delete(m, "updatedAt")
		old, _ := m["id"].(string)
		if keep[old] {
			continue // the rule has it; the components name it, and it is not listed again
		}
		fresh := newUUID()
		m["id"] = fresh
		renamed[old] = fresh
		listed = append(listed, c)
	}
	if conns != nil {
		payload["connections"] = listed
	}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if id, ok := x["connectionId"].(string); ok && renamed[id] != "" {
				x["connectionId"] = renamed[id]
			}
			for _, val := range x {
				walk(val)
			}
		case []any:
			for _, item := range x {
				walk(item)
			}
		}
	}
	walk(payload["rule"])
}
