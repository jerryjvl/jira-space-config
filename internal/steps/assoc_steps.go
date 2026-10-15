package steps

import (
	"fmt"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// Step 25 switches the space to the schemes earlier steps decided on, after
// the safety counts; step 28 lists what this tool does not apply.

const switchIntro = "The space is switched to the schemes the steps above decided on, one kind at a time. A switch " +
	"is blocked while it would affect work items: their count is shown, with what to do first."

// associations are the calls that switch a space to a scheme, by kind.
var associations = map[string]string{
	"issue type scheme":          "PUT /rest/api/3/issuetypescheme/project",
	"issue type screen scheme":   "PUT /rest/api/3/issuetypescreenscheme/project",
	"field scheme":               "PUT /rest/api/3/config/fieldschemes/projects",
	"field configuration scheme": "PUT /rest/api/3/fieldconfigurationscheme/project",
	"workflow scheme":            "PUT /rest/api/3/workflowscheme/project",
	"permission scheme":          "PUT /rest/api/3/project/{key} (permissionScheme)",
	"notification scheme":        "PUT /rest/api/3/project/{key} (notificationScheme)",
	"issue security scheme":      "PUT /rest/api/3/project/{key} (issueSecurityScheme)",
	"priority scheme":            "PUT /rest/api/3/priorityscheme/{id} (projects)",
}

func schemeSwitches(env *Env) (*Step, error) {
	return evaluateWith(env, 25, "Scheme switches", "scheme switch", "scheme switches", switchIntro, func() ([]Item, error) {
		if len(env.switches) == 0 {
			return []Item{{Kind: Exists, Name: "Schemes", Summary: "the space keeps every scheme it uses"}}, nil
		}
		var items []Item
		for _, sw := range env.switches {
			item := Item{Kind: Update, Name: capitalFirst(sw.noun), Requests: []string{associations[sw.noun]}}
			item.Do = switchTo(env, resolver{env.R, env.Spec}, sw)
			if sw.to == "" {
				item.Summary = fmt.Sprintf("the space stops using %q", sw.from)
			} else {
				item.Summary = fmt.Sprintf("from %q to %q", sw.from, sw.to)
			}
			blocked, err := runSafety(env, sw.safety)
			if err != nil {
				return nil, err
			}
			items = append(append(items, item), blocked...)
		}
		return items, nil
	})
}

const automationIntro = "Automation rules are compared as Jira holds them: their name, state, scope and the " +
	"rule itself (docs/automation.md). A managed rule (mode managed, the default) is created for this space when " +
	"missing, and changed to match while it covers this space alone; a shared one is never changed. Issue layouts " +
	"cannot be read or written through the API, and are listed to be set up by hand."

func rawItems(raws []spec.Raw, summary string) []Item {
	var out []Item
	for _, r := range raws {
		out = append(out, Item{Kind: Manual, Name: r.Name, Summary: summary,
			Fix: "Set it up in Jira by hand, or check that it is as the spec describes."})
	}
	return out
}

func automationRules(env *Env) (*Step, error) {
	return evaluateWith(env, 28, "Automation rules", "automation rule", "automation rules", automationIntro, func() ([]Item, error) {
		var items []Item
		if len(env.Spec.AutomationRules) > 0 {
			rules, err := ruleItems(env)
			if err != nil {
				return nil, err
			}
			items = rules
		}
		items = append(items, rawItems(env.Spec.IssueLayouts, "issue layout: set in Jira by hand; the API cannot read or write it")...)
		return items, nil
	})
}
