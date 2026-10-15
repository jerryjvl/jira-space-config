package steps

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/spec"
	"github.com/jerryjvl/jira-space-config/internal/util"
)

// The changes to managed objects this space alone uses. Each reads what it changes again when it runs, and changes
// only what differs.

// updateScreen changes a managed screen's description, tabs and fields to
// the spec's. The changes are planned on names now, so the step lists
// exactly the writes it makes, and made with the IDs read when it runs.
func updateScreen(x resolver, want, got spec.Screen) change {
	_, description := compareText("description", want.Description, got.Description)
	ops := planScreen(want, got)
	var requests []string
	if description {
		requests = append(requests, "PUT /rest/api/3/screens/{screenId} (description)")
	}
	for _, op := range ops {
		requests = append(requests, op.request())
	}
	return change{class: screenClass(want, got), requests: countRequests(requests), do: func(ctx context.Context, snd Sender) (string, error) {
		if description {
			body := map[string]string{"description": managedDescription(want.Description)}
			if err := snd.Send(ctx, http.MethodPut, "/rest/api/3/screens/"+got.ID, nil, body, nil); err != nil {
				return "", err
			}
		}
		return got.ID, screenWrites(ctx, snd, x, got.ID, ops)
	}}
}

// screenClass is "additive" when the spec keeps every tab and field Jira
// has, in the same order, and only adds; otherwise "altering".
func screenClass(want, got spec.Screen) string {
	// A description where there was none only adds.
	if _, differs := compareText("description", want.Description, got.Description); differs && show(got.Description) != "none" {
		return "altering"
	}
	if want.Tabs == nil {
		return "additive"
	}
	keeps := func(w, g []string) bool { // g is w with some entries left out, in order
		i := 0
		for _, v := range w {
			if i < len(g) && v == g[i] {
				i++
			}
		}
		return i == len(g)
	}
	if !keeps(tabNames(want.Tabs), tabNames(got.Tabs)) {
		return "altering"
	}
	for _, g := range got.Tabs {
		i := slices.IndexFunc(want.Tabs, func(w spec.ScreenTab) bool { return w.Name == g.Name })
		if !keeps(want.Tabs[i].Fields, g.Fields) {
			return "altering"
		}
	}
	return "additive"
}

func tabNames(tabs []spec.ScreenTab) []string {
	var out []string
	for _, t := range tabs {
		out = append(out, t.Name)
	}
	return out
}

// screenOp is one write to a screen's tabs, by name.
type screenOp struct {
	kind  string // removeField, addTab, removeTab, addField, moveField, moveTab
	tab   string
	field string // the field, for field operations
	after string // moveField: the field it goes after; "" for first
	pos   int    // moveTab: the tab's position, from 0
}

func (op screenOp) request() string {
	tab := "/rest/api/3/screens/{screenId}/tabs"
	return map[string]string{
		"removeField": "DELETE " + tab + "/{tabId}/fields/{id}",
		"addTab":      "POST " + tab,
		"removeTab":   "DELETE " + tab + "/{tabId}",
		"addField":    "POST " + tab + "/{tabId}/fields",
		"moveField":   "POST " + tab + "/{tabId}/fields/{id}/move",
		"moveTab":     "POST " + tab + "/{tabId}/move/{pos}",
	}[op.kind]
}

// countRequests folds repeated requests into one line: "… × 3".
func countRequests(l []string) []string {
	var out []string
	n := map[string]int{}
	for _, r := range l {
		if n[r] == 0 {
			out = append(out, r)
		}
		n[r]++
	}
	for i, r := range out {
		if n[r] > 1 {
			out[i] = fmt.Sprintf("%s × %d", r, n[r])
		}
	}
	return out
}

// planScreen plans the writes that make the screen's tabs and fields the
// spec's: fields not where the spec puts them are taken off first (a field
// can be on one tab only), then tabs are added (before any is removed, so
// the screen always has one) and removed, fields added, and both put in
// order.
func planScreen(want, got spec.Screen) []screenOp {
	if want.Tabs == nil {
		return nil
	}
	tabOf := map[string]string{}
	for _, t := range want.Tabs {
		for _, f := range t.Fields {
			tabOf[f] = t.Name
		}
	}
	var ops []screenOp
	have := map[string][]string{}
	for _, t := range got.Tabs {
		for _, f := range t.Fields {
			if tabOf[f] != t.Name {
				ops = append(ops, screenOp{kind: "removeField", tab: t.Name, field: f})
				continue
			}
			have[t.Name] = append(have[t.Name], f)
		}
	}
	gotTabs := tabNames(got.Tabs)
	for _, t := range want.Tabs {
		if !slices.Contains(gotTabs, t.Name) {
			ops = append(ops, screenOp{kind: "addTab", tab: t.Name})
		}
	}
	wantTabs := tabNames(want.Tabs)
	var order []string // the tabs in Jira's order once added and removed
	for _, t := range gotTabs {
		if slices.Contains(wantTabs, t) {
			order = append(order, t)
		} else {
			ops = append(ops, screenOp{kind: "removeTab", tab: t})
		}
	}
	for _, t := range want.Tabs {
		order = util.AppendNew(order, t.Name)
		current := have[t.Name]
		for _, f := range t.Fields {
			if !slices.Contains(current, f) {
				ops = append(ops, screenOp{kind: "addField", tab: t.Name, field: f})
				current = append(current, f)
			}
		}
		for i, f := range t.Fields {
			if current[i] == f {
				continue
			}
			op := screenOp{kind: "moveField", tab: t.Name, field: f}
			if i > 0 {
				op.after = t.Fields[i-1]
			}
			ops = append(ops, op)
			current = slices.Insert(slices.DeleteFunc(current, func(x string) bool { return x == f }), i, f)
		}
	}
	for i, t := range wantTabs {
		if order[i] != t {
			ops = append(ops, screenOp{kind: "moveTab", tab: t, pos: i})
			order = slices.Insert(slices.DeleteFunc(order, func(x string) bool { return x == t }), i, t)
		}
	}
	return ops
}

type screenTab struct {
	ID   jira.ID `json:"id"`
	Name string  `json:"name"`
}

// screenWrites carries out the planned writes, with the tab IDs read now
// and the fields' IDs on the site.
func screenWrites(ctx context.Context, snd Sender, x resolver, id string, ops []screenOp) error {
	base := "/rest/api/3/screens/" + id + "/tabs"
	var tabs []screenTab
	if len(ops) > 0 {
		if err := snd.Send(ctx, http.MethodGet, base, nil, nil, &tabs); err != nil {
			return err
		}
	}
	tabID := map[string]string{}
	for _, t := range tabs {
		tabID[t.Name] = string(t.ID)
	}
	for _, op := range ops {
		if op.kind == "addTab" {
			var created screenTab
			if err := snd.Send(ctx, http.MethodPost, base, nil, map[string]string{"name": op.tab}, &created); err != nil {
				return err
			}
			tabID[op.tab] = string(created.ID)
			continue
		}
		tab, ok := tabID[op.tab]
		if !ok {
			return fmt.Errorf("the screen has no tab %q now; it was changed since this step was evaluated", op.tab)
		}
		var fid, after string
		var err error
		if op.field != "" {
			if fid, err = x.field(op.field); err != nil {
				return err
			}
		}
		if op.after != "" {
			if after, err = x.field(op.after); err != nil {
				return err
			}
		}
		fields := base + "/" + tab + "/fields"
		switch op.kind {
		case "removeField":
			err = snd.Send(ctx, http.MethodDelete, fields+"/"+fid, nil, nil, nil)
		case "removeTab":
			err = snd.Send(ctx, http.MethodDelete, base+"/"+tab, nil, nil, nil)
		case "addField":
			err = snd.Send(ctx, http.MethodPost, fields, nil, map[string]string{"fieldId": fid}, nil)
		case "moveField":
			move := map[string]string{"position": "First"}
			if after != "" {
				move = map[string]string{"after": after}
			}
			err = snd.Send(ctx, http.MethodPost, fields+"/"+fid+"/move", nil, move, nil)
		case "moveTab":
			err = snd.Send(ctx, http.MethodPost, base+"/"+tab+"/move/"+strconv.Itoa(op.pos), nil, nil, nil)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

var workflowUpdateRequests = []string{
	"POST /rest/api/3/workflows (reads the workflow's version)",
	"POST /rest/api/3/workflows/update/validation (read-only check)",
	"POST /rest/api/3/workflows/update",
}

// updateWorkflow changes a managed workflow's statuses and transitions to
// the spec's, through Jira's workflow update, which Jira checks first.
// Transitions keep their IDs. A status the workflow loses is removed only
// with --allow-delete=workflows and when no work item of the space's issue
// types that use the workflow is in it. Jira still wants it listed, and a
// mapping for each of those issue types onto the status new work items
// start in (docs/api/inventory.md).
func updateWorkflow(env *Env, x resolver, want, got spec.Workflow) change {
	want = keepTransitionIDs(want, got)
	ch := change{class: "additive", allow: "workflows", requests: workflowUpdateRequests}
	var gone []string
	for _, s := range got.Statuses {
		if !slices.ContainsFunc(want.Statuses, func(w spec.WorkflowStatus) bool { return w.Name == s.Name }) {
			gone = append(gone, s.Name)
			ch.removes = append(ch.removes, "status "+quote(s.Name))
		}
	}
	for _, d := range compareWorkflows(want, got) {
		switch {
		case d.Attribute == "statuses": // added ones only add; lost ones are counted above
		case d.Spec == "none":
			ch.removes = append(ch.removes, d.Attribute)
		case d.Jira != "none":
			ch.class = "altering"
		}
	}
	if len(ch.removes) > 0 {
		ch.class = "removing"
	}
	types := typesUsing(env, want.Name)
	for _, s := range gone {
		jql := fmt.Sprintf("project = %s AND status = %s", env.P.ID, jqlString(s))
		if len(types) > 0 {
			var q []string
			for _, t := range types {
				q = append(q, jqlString(t))
			}
			jql = fmt.Sprintf("project = %s AND issuetype in (%s) AND status = %s", env.P.ID, strings.Join(q, ", "), jqlString(s))
		}
		ch.safety = append(ch.safety, safety{name: "status " + quote(s), jql: jql,
			summary: "are in this status, which the workflow would lose",
			fix:     "Move them to another status first, or keep the status in the workflow."})
	}
	ch.do = func(ctx context.Context, snd Sender) (string, error) {
		payload, err := workflowRequest(env, x, want)
		if err != nil {
			return "", err
		}
		var read struct {
			Workflows []struct {
				ID      string         `json:"id"`
				Version map[string]any `json:"version"`
			} `json:"workflows"`
		}
		if err := snd.Send(ctx, http.MethodPost, "/rest/api/3/workflows", nil, map[string][]string{"workflowIds": {got.ID}}, &read); err != nil {
			return "", err
		}
		if len(read.Workflows) != 1 {
			return "", fmt.Errorf("workflow %s was not returned; it was changed since this step was evaluated", got.ID)
		}
		wf := payload["workflows"].([]map[string]any)[0]
		delete(wf, "name")
		wf["id"], wf["version"] = got.ID, read.Workflows[0].Version
		statuses := payload["statuses"].([]map[string]string)
		if len(gone) > 0 {
			mappings, lost, err := statusMappings(env, x, want, gone, types)
			if err != nil {
				return "", err
			}
			wf["statusMappings"], statuses = mappings, append(statuses, lost...)
		}
		update := map[string]any{"statuses": statuses, "workflows": []map[string]any{wf}}
		if err := validateWorkflow(ctx, snd, "/rest/api/3/workflows/update/validation", update); err != nil {
			return "", err
		}
		if err := snd.Send(ctx, http.MethodPost, "/rest/api/3/workflows/update", nil, update, nil); err != nil {
			return "", err
		}
		return got.ID, nil
	}
	return ch
}

// keepTransitionIDs gives the spec's transitions without an ID the ID of
// Jira's transition of the same name (the n-th for the n-th), so an
// update keeps them; new ones get the next free.
func keepTransitionIDs(want, got spec.Workflow) spec.Workflow {
	seen := map[string]int{}
	ts := expandLoops(want.Transitions)
	for i, t := range ts {
		n := seen[t.Name]
		seen[t.Name]++
		if t.ID != "" {
			continue
		}
		for _, g := range got.Transitions {
			if g.Name == t.Name {
				if n == 0 {
					ts[i].ID = g.ID
					break
				}
				n--
			}
		}
	}
	want.Transitions = ts
	return want
}

// typesUsing are the space's issue types the spec gives a workflow.
func typesUsing(env *Env, workflow string) []string {
	var out []string
	for _, t := range env.Spec.Space.IssueTypes {
		if t.Workflow.Set && t.Workflow.V == workflow {
			out = append(out, t.Name)
		}
	}
	return out
}

// statusMappings map each status the workflow loses onto the status new
// work items start in, for each of the space's issue types that use it,
// and list the lost statuses as Jira holds them: the update must still
// name them.
func statusMappings(env *Env, x resolver, w spec.Workflow, gone, types []string) ([]map[string]any, []map[string]string, error) {
	start := ""
	for _, t := range w.Transitions {
		if t.Type == spec.TransitionInitial {
			start = t.To
		}
	}
	to, err := x.status(start)
	if err != nil {
		return nil, nil, err
	}
	var migrations []map[string]string
	var lost []map[string]string
	for _, s := range gone {
		from, err := x.status(s)
		if err != nil {
			return nil, nil, err
		}
		migrations = append(migrations, map[string]string{"oldStatusReference": from.ID, "newStatusReference": to.ID})
		lost = append(lost, statusEntry(from))
	}
	var out []map[string]any
	for _, t := range types {
		id, err := env.knownIssueTypeID(t)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, map[string]any{"issueTypeId": id, "projectId": env.P.ID, "statusMigrations": migrations})
	}
	return out, lost, nil
}
