package steps

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/reader"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// Changes in place to a space's managed workflow scheme, and waiting for the task Jira starts for it.

// updateWorkflowScheme gives the issue types whose workflow differs the
// spec's, through Jira's workflow scheme update. Step 16's counts block it
// while work items are in a status the new workflow lacks.
func updateWorkflowScheme(env *Env, x resolver, id string, want, current []line) change {
	ch := change{class: "altering", requests: []string{"POST /rest/api/3/workflowscheme/read (reads its version)",
		"POST /rest/api/3/workflowscheme/update"}}
	moved := map[string]string{} // issue type → its new workflow
	was := map[string]string{}   // issue type → its workflow now
	for _, w := range want {
		i := slices.IndexFunc(current, func(l line) bool { return l.key == w.key })
		if i < 0 || current[i].value != w.value {
			moved[w.key] = w.value
			if i >= 0 {
				was[w.key] = current[i].value
			}
		}
	}
	ch.do = func(ctx context.Context, snd Sender) (string, error) {
		type workflow struct {
			ID string `json:"id"`
		}
		var read []struct {
			ID          string         `json:"id"`
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Version     map[string]any `json:"version"`
			Default     workflow       `json:"defaultWorkflow"`
			ForTypes    []struct {
				IssueTypeIDs []string `json:"issueTypeIds"`
				Workflow     workflow `json:"workflow"`
			} `json:"workflowsForIssueTypes"`
		}
		if err := snd.Send(ctx, http.MethodPost, "/rest/api/3/workflowscheme/read", nil,
			map[string][]string{"workflowSchemeIds": {id}}, &read); err != nil {
			return "", err
		}
		if len(read) != 1 {
			return "", fmt.Errorf("workflow scheme %s was not returned; it was changed since this step was evaluated", id)
		}
		typesOf := map[string][]string{} // workflow ID → issue type IDs
		var mappings []map[string]any
		for _, f := range read[0].ForTypes {
			typesOf[f.Workflow.ID] = append(typesOf[f.Workflow.ID], f.IssueTypeIDs...)
		}
		for _, name := range sortedKeys(moved) {
			tid, err := env.knownIssueTypeID(name)
			if err != nil {
				return "", err
			}
			found, err := env.R.FindWorkflows(moved[name])
			if err != nil {
				return "", err
			}
			i := slices.IndexFunc(found, func(f reader.Found) bool { return f.Name == moved[name] })
			if i < 0 {
				return "", fmt.Errorf("no workflow on the site is named %q", moved[name])
			}
			for w := range typesOf {
				typesOf[w] = slices.DeleteFunc(typesOf[w], func(t string) bool { return t == tid })
			}
			typesOf[found[i].ID] = append(typesOf[found[i].ID], tid)
			m, err := lostStatuses(env, x, was[name], moved[name])
			if err != nil {
				return "", err
			}
			if len(m) > 0 { // Jira wants them by issue type; by workflow is not enough
				mappings = append(mappings, map[string]any{"issueTypeId": tid, "statusMappings": m})
			}
		}
		var forTypes []map[string]any
		for _, w := range sortedKeys(typesOf) {
			if len(typesOf[w]) > 0 {
				forTypes = append(forTypes, map[string]any{"workflowId": w, "issueTypeIds": typesOf[w]})
			}
		}
		body := map[string]any{"id": id, "name": read[0].Name, "description": read[0].Description, "version": read[0].Version,
			"defaultWorkflowId": read[0].Default.ID, "workflowsForIssueTypes": forTypes}
		if mappings != nil {
			body["statusMappingsByIssueTypeOverride"] = mappings
		}
		// Jira answers 303 when it starts a task to migrate work items: the
		// update was accepted, and the scheme names the task until it is done.
		if err := snd.Send(ctx, http.MethodPost, "/rest/api/3/workflowscheme/update", nil, body, nil); err != nil && !taskStarted(err) {
			return "", err
		}
		return id, waitForScheme(ctx, snd, id)
	}
	return ch
}

// lostStatuses map the statuses of workflow from that workflow to lacks
// onto the status to starts work items in. Jira wants them even when no
// work item is in them (step 16 has counted that none is).
func lostStatuses(env *Env, x resolver, from, to string) ([]map[string]string, error) {
	if from == "" {
		return nil, nil
	}
	defs, err := env.R.Workflows([]string{from, to})
	if err != nil {
		return nil, err
	}
	var old, next *spec.Workflow
	for i := range defs.Workflows {
		switch defs.Workflows[i].Name {
		case from:
			old = &defs.Workflows[i]
		case to:
			next = &defs.Workflows[i]
		}
	}
	if old == nil || next == nil {
		// checkcover:unreachable Workflows fails unless Jira returns every workflow asked for.
		return nil, fmt.Errorf("workflows %q and %q were not both returned", from, to)
	}
	start := ""
	for _, t := range next.Transitions {
		if t.Type == spec.TransitionInitial {
			start = t.To
		}
	}
	target, err := x.status(start)
	if err != nil {
		return nil, err
	}
	var out []map[string]string
	for _, s := range old.Statuses {
		if slices.ContainsFunc(next.Statuses, func(n spec.WorkflowStatus) bool { return n.Name == s.Name }) {
			continue
		}
		st, err := x.status(s.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]string{"oldStatusId": st.ID, "newStatusId": target.ID})
	}
	return out, nil
}

// waitForScheme waits while Jira runs the task a workflow scheme update
// started.
func waitForScheme(ctx context.Context, snd Sender, id string) error {
	return waitUntil(ctx, fmt.Sprintf("the migration Jira runs for workflow scheme %s", id), func() (bool, error) {
		var read []struct {
			TaskID string `json:"taskId"`
		}
		err := snd.Send(ctx, http.MethodPost, "/rest/api/3/workflowscheme/read", nil,
			map[string][]string{"workflowSchemeIds": {id}}, &read)
		return len(read) == 1 && read[0].TaskID == "", err
	})
}

// waitUntil asks done every schemeWait, up to a minute or so, until it says
// yes; what says what was waited for, if it never does. Jira does some
// changes in a task of its own (it answers 303), which is only finished when
// what it changed can be read.
func waitUntil(ctx context.Context, what string, done func() (bool, error)) error {
	for range 60 {
		ok, err := done()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(schemeWait):
		}
	}
	return fmt.Errorf("%s did not finish in time; check it in Jira, then run again", what)
}

// schemeWait is how long waitUntil waits between reads; tests shorten it.
var schemeWait = 2 * time.Second

// taskStarted reports whether Jira answered 303: it started a task.
func taskStarted(err error) bool {
	var apiErr *jira.APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusSeeOther
}
