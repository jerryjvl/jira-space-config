package reader

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/spec"
	"github.com/jerryjvl/jira-space-config/internal/util"
)

// resolutionProperties are transition properties holding resolution IDs,
// exported as names (docs/api/inventory.md, section 7).
var resolutionProperties = []string{"jira.field.resolution.include", "jira.field.resolution.exclude"}

// WorkflowAssignment is the space's workflow scheme flattened to one
// workflow per issue type.
type WorkflowAssignment struct {
	Binding *spec.ContainerRef
	// Workflows is the workflow of each issue type, in the order of the
	// IssueTypes given.
	Workflows []string
	// Names are the workflows the space uses: the scheme's default first,
	// then the others in issue type order.
	Names []string
}

// WorkflowScheme reads which workflow each of the space's issue types uses.
func (r *Reader) WorkflowScheme(p *Project, its *IssueTypes) (*WorkflowAssignment, error) {
	var assoc struct {
		Values []struct {
			WorkflowScheme struct {
				ID                int               `json:"id"`
				Name              string            `json:"name"`
				Description       string            `json:"description"`
				DefaultWorkflow   string            `json:"defaultWorkflow"`
				IssueTypeMappings map[string]string `json:"issueTypeMappings"`
			} `json:"workflowScheme"`
		} `json:"values"`
	}
	if err := r.get("/rest/api/3/workflowscheme/project", url.Values{"projectId": {p.ID}}, &assoc); err != nil {
		return nil, fmt.Errorf("reading the workflow scheme: %w", err)
	}
	if len(assoc.Values) != 1 {
		return nil, fmt.Errorf("expected one workflow scheme for space %s, Jira returned %d", p.Key, len(assoc.Values))
	}
	ws := assoc.Values[0].WorkflowScheme

	out := &WorkflowAssignment{
		Binding: containerRef(strconv.Itoa(ws.ID), ws.Name, ws.Description),
		Names:   []string{ws.DefaultWorkflow},
	}
	for i := range its.Len() {
		name := ws.DefaultWorkflow
		if wf, ok := ws.IssueTypeMappings[its.ID(i)]; ok {
			name = wf
			out.Names = util.AppendNew(out.Names, wf)
		}
		out.Workflows = append(out.Workflows, name)
	}
	for id := range ws.IssueTypeMappings {
		if _, inSpace := its.Name(id); !inSpace {
			r.note("Workflow scheme %q maps issue type %s, which is not in this space; that mapping is left out.", ws.Name, id)
		}
	}
	return out, nil
}

// WorkflowDefinitions are workflows read by name.
type WorkflowDefinitions struct {
	Workflows []spec.Workflow
	// Screens are the screens the workflows' transitions show, in the order
	// first shown, with their tabs and fields.
	Screens []spec.Screen
}

// Workflows reads workflows by name, through the bulk workflow API.
func (r *Reader) Workflows(names []string) (*WorkflowDefinitions, error) {
	var read struct {
		Workflows []workflow `json:"workflows"`
		Statuses  []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Ref  string `json:"statusReference"`
		} `json:"statuses"`
	}
	if err := r.c.PostRead(r.ctx, "/rest/api/3/workflows", nil, map[string][]string{"workflowNames": names}, &read); err != nil {
		return nil, fmt.Errorf("reading workflows %s: %w", strings.Join(names, ", "), err)
	}
	refNames := map[string]string{}
	for _, s := range read.Statuses {
		// Stored names, read by SpaceStatuses, win over displayed ones.
		if _, known := r.statusNames[s.ID]; !known {
			r.statusNames[s.ID] = s.Name
		}
		refNames[s.Ref] = r.statusNames[s.ID]
	}
	if len(read.Workflows) != len(names) {
		return nil, fmt.Errorf("asked Jira for %d workflows (%s), got %d", len(names), strings.Join(names, ", "), len(read.Workflows))
	}
	out := &WorkflowDefinitions{}
	for _, wf := range read.Workflows {
		def, err := r.workflow(wf, refNames, out)
		if err != nil {
			return nil, err
		}
		out.Workflows = append(out.Workflows, def)
	}
	return out, r.done()
}

// workflow is the subset of the bulk workflow API's workflow that is exported.
type workflow struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Statuses    []struct {
		Ref        string            `json:"statusReference"`
		Layout     map[string]any    `json:"layout"`
		Properties map[string]string `json:"properties"`
		Deprecated bool              `json:"deprecated"`
	} `json:"statuses"`
	Transitions []transition `json:"transitions"`
	Scope       struct {
		Type string `json:"type"`
	} `json:"scope"`
}

type transition struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	To          string `json:"toStatusReference"`
	Links       []struct {
		From string `json:"fromStatusReference"`
	} `json:"links"`
	Event      string            `json:"customIssueEventId"`
	Screen     *jiraRule         `json:"transitionScreen"`
	Conditions *conditionGroup   `json:"conditions"`
	Validators []jiraRule        `json:"validators"`
	Actions    []jiraRule        `json:"actions"`
	Triggers   []jiraRule        `json:"triggers"`
	Properties map[string]string `json:"properties"`
}

type conditionGroup struct {
	Operation  string           `json:"operation"`
	Conditions []jiraRule       `json:"conditions"`
	Groups     []conditionGroup `json:"conditionGroups"`
}

func (r *Reader) workflow(wf workflow, refNames map[string]string, out *WorkflowDefinitions) (spec.Workflow, error) {
	description, mode := ownership(wf.Description)
	def := spec.Workflow{Meta: spec.Meta{ID: wf.ID}, Name: wf.Name, Mode: mode, Description: description}
	if wf.Scope.Type != "" && wf.Scope.Type != "GLOBAL" {
		r.note("Workflow %q has scope %q; company-managed spaces use global workflows, so it may not be read correctly.", wf.Name, wf.Scope.Type)
	}
	if strings.Contains(wf.Description, "managed internally by Jira") {
		r.note("Workflow %q is a simplified workflow that Jira manages through the board's columns; change it there, not here.", wf.Name)
	}
	statusName := func(ref string) (string, error) {
		if n, ok := refNames[ref]; ok {
			return n, nil
		}
		return "", fmt.Errorf("workflow %q refers to status reference %q, which Jira did not describe", wf.Name, ref)
	}
	for _, s := range wf.Statuses {
		name, err := statusName(s.Ref)
		if err != nil {
			return def, err
		}
		ws := spec.WorkflowStatus{Name: name, Properties: sortedKV(s.Properties), Deprecated: s.Deprecated}
		if x, okX := number(s.Layout["x"]); okX {
			if y, okY := number(s.Layout["y"]); okY {
				ws.Layout = &spec.Layout{X: x, Y: y}
			}
		}
		def.Statuses = append(def.Statuses, ws)
	}
	for _, t := range wf.Transitions {
		where := fmt.Sprintf("Transition %q of workflow %q", t.Name, wf.Name)
		st := spec.Transition{ID: t.ID, Name: t.Name, Type: strings.ToLower(t.Type), Description: optText(t.Description)}
		for _, l := range t.Links {
			if l.From == "" {
				continue // an arrow anchor only (initial transitions have one), not a source status
			}
			from, err := statusName(l.From)
			if err != nil {
				return def, err
			}
			st.From = append(st.From, from)
		}
		if t.To == "" && st.Type != spec.TransitionInitial {
			// The tool writes a loop as a transition to its own status, so a
			// missing target is only what Jira's own editor might produce; the
			// reading is unconfirmed (docs/api/inventory.md).
			st.Looped = true
			r.note("%s has no target status and is read as looped; this reading of the API is unconfirmed.", where)
		} else {
			to, err := statusName(t.To)
			if err != nil {
				return def, err
			}
			st.To = to
		}
		if t.Screen != nil {
			name, err := r.transitionScreen(*t.Screen, where, out)
			if err != nil {
				return def, err
			}
			st.Screen = spec.Some(name)
		}
		if t.Event != "" {
			st.Event = spec.Some(r.eventName(t.Event, where))
		}
		if t.Conditions != nil {
			if c := r.conditions(*t.Conditions, where); c != nil {
				st.Conditions = c
			}
		}
		for _, jr := range t.Validators {
			st.Validators = append(st.Validators, r.rule(spec.ListValidators, jr, where))
		}
		for _, jr := range t.Actions {
			st.PostFunctions = append(st.PostFunctions, r.rule(spec.ListPostFunctions, jr, where))
		}
		for _, jr := range t.Triggers {
			st.Triggers = append(st.Triggers, r.rule(spec.ListTriggers, jr, where))
		}
		st.Properties = r.transitionProperties(t.Properties)
		def.Transitions = append(def.Transitions, st)
	}
	return def, nil
}

func number(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

// conditions converts a condition group; an empty top-level group is no
// conditions at all.
func (r *Reader) conditions(g conditionGroup, where string) *spec.Condition {
	if len(g.Conditions) == 0 && len(g.Groups) == 0 {
		return nil
	}
	c := &spec.Condition{Op: strings.ToLower(g.Operation)}
	for _, jr := range g.Conditions {
		rule := r.rule(spec.ListConditions, jr, where)
		c.Items = append(c.Items, spec.Condition{Rule: &rule})
	}
	for _, sub := range g.Groups {
		if sc := r.conditions(sub, where); sc != nil {
			c.Items = append(c.Items, *sc)
		}
	}
	return c
}

// transitionScreen names the screen of a system:transition-screen rule and
// adds the screen to out.
func (r *Reader) transitionScreen(rule jiraRule, where string, out *WorkflowDefinitions) (string, error) {
	id := rule.params()["screenId"]
	if rule.RuleKey != "system:transition-screen" || id == "" {
		r.note("%s has a transition screen rule %q this tool does not understand; it is left out.", where, rule.RuleKey)
		return "", nil
	}
	screens, err := r.Screens([]string{id})
	if err != nil {
		return "", err
	}
	if !containsScreen(out.Screens, id) {
		out.Screens = append(out.Screens, screens[0])
	}
	return screens[0].Name, nil
}

func (r *Reader) transitionProperties(props map[string]string) []spec.Property {
	var out []spec.Property
	for _, kv := range sortedKV(props) {
		p := spec.Property{Key: kv.Key, Value: kv.Value}
		if slices.Contains(resolutionProperties, kv.Key) {
			p = spec.Property{Key: kv.Key, IsList: true}
			for _, id := range strings.Split(kv.Value, ",") {
				if id = strings.TrimSpace(id); id != "" {
					p.List = append(p.List, r.resolutionName(id))
				}
			}
		}
		out = append(out, p)
	}
	return out
}

func (r *Reader) resolutionName(id string) string {
	all, err := r.Resolutions()
	if err != nil {
		r.lookupFailed(err)
	}
	for _, res := range all {
		if res.ID == id {
			return res.Name
		}
	}
	r.note("A transition property refers to resolution %s, which is not on the site; it is kept as id:%s.", id, id)
	return "id:" + id
}

// eventName names the event a transition fires. The site's events are read
// once, on first use, so spaces whose transitions fire none make no request.
func (r *Reader) eventName(id, where string) string {
	if err := r.loadEvents(); err != nil {
		r.lookupFailed(err)
	}
	if name, ok := r.eventNames[id]; ok {
		return name
	}
	r.note("%s fires event %s, which is not on the site; it is kept as id:%s.", where, id, id)
	return "id:" + id
}

// EventID returns the ID of an event by name, for writing a transition
// that fires it; ok is false when the site has no such event.
func (r *Reader) EventID(name string) (string, bool, error) {
	if err := r.loadEvents(); err != nil {
		return "", false, err
	}
	for id, n := range r.eventNames {
		if n == name {
			return id, true, nil
		}
	}
	return "", false, nil
}

// loadEvents reads the site's events once.
func (r *Reader) loadEvents() error {
	if r.eventNames != nil {
		return nil
	}
	var events []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := r.get("/rest/api/3/events", nil, &events); err != nil {
		return fmt.Errorf("reading the site's events to name them: %w", err)
	}
	r.eventNames = map[string]string{}
	for _, ev := range events {
		r.eventNames[strconv.Itoa(ev.ID)] = ev.Name
	}
	return nil
}
