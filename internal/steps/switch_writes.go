package steps

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/jerryjvl/jira-space-config/internal/util"
)

// Switching a space to a scheme, or off one: finding the scheme by name, the
// write each kind takes, and what Jira needs with it.

// switchTo switches the space to the named scheme of a kind.
func switchTo(env *Env, x resolver, sw switchPlan) action {
	project := env.P.ID
	pid, _ := strconv.Atoi(project)
	return func(ctx context.Context, snd Sender) (string, error) {
		if sw.to == "" {
			return "", stopUsing(ctx, snd, env, sw.noun)
		}
		id, err := x.schemeByName(env, sw.noun, sw.to)
		if err != nil {
			return "", err
		}
		n, _ := strconv.Atoi(id)
		var method, path string
		var body any
		switch sw.noun {
		case "issue type scheme":
			method, path, body = http.MethodPut, "/rest/api/3/issuetypescheme/project", map[string]string{"issueTypeSchemeId": id, "projectId": project}
		case "issue type screen scheme":
			method, path, body = http.MethodPut, "/rest/api/3/issuetypescreenscheme/project", map[string]string{"issueTypeScreenSchemeId": id, "projectId": project}
		case "field scheme":
			method, path, body = http.MethodPut, "/rest/api/3/config/fieldschemes/projects", map[string]any{id: map[string]any{"projectIds": []int{pid}}}
		case "field configuration scheme":
			method, path, body = http.MethodPut, "/rest/api/3/fieldconfigurationscheme/project", map[string]string{"fieldConfigurationSchemeId": id, "projectId": project}
		case "workflow scheme":
			method, path, body = http.MethodPut, "/rest/api/3/workflowscheme/project", map[string]string{"workflowSchemeId": id, "projectId": project}
		case "permission scheme", "notification scheme", "issue security scheme":
			key := map[string]string{"permission scheme": "permissionScheme", "notification scheme": "notificationScheme",
				"issue security scheme": "issueSecurityScheme"}[sw.noun]
			method, path, body = http.MethodPut, "/rest/api/3/project/"+env.P.Key, map[string]any{key: n}
		case "priority scheme":
			mappings, err := priorityMappings(env, x, sw.to)
			if err != nil {
				return "", err
			}
			method, path, body = http.MethodPut, "/rest/api/3/priorityscheme/"+id, map[string]any{
				"projects": map[string]any{"add": map[string]any{"ids": []int{pid}}}, "mappings": map[string]any{"in": mappings}}
		default:
			// checkcover:unreachable switches are planned only by the scheme steps, for the kinds above.
			return "", fmt.Errorf("switching a %s is not supported yet", sw.noun)
		}
		if err := snd.Send(ctx, method, path, nil, body, nil); err != nil {
			return "", err
		}
		return id, nil
	}
}

// schemeByName finds the ID of a scheme by kind and name.
func (x resolver) schemeByName(env *Env, noun, name string) (string, error) {
	var ids []string
	var err error
	switch noun {
	case "issue type scheme":
		all, e := x.r.FindIssueTypeSchemes(name)
		for _, s := range all {
			ids = append(ids, s.ID)
		}
		err = e
	case "issue type screen scheme":
		all, e := x.r.FindIssueTypeScreenSchemes(name)
		for _, s := range all {
			ids = append(ids, s.ID)
		}
		err = e
	case "field scheme":
		its, e := env.issueTypes()
		if e != nil {
			return "", e
		}
		all, e := x.r.FindFieldSchemes(its, name)
		for _, s := range all {
			ids = append(ids, s.ID)
		}
		err = e
	case "field configuration scheme":
		its, e := env.issueTypes()
		if e != nil {
			return "", e
		}
		all, e := x.r.FindFieldConfigurationSchemes(its, name)
		for _, s := range all {
			ids = append(ids, s.ID)
		}
		err = e
	case "workflow scheme":
		all, e := x.r.FindWorkflowSchemes(name)
		for _, s := range all {
			ids = append(ids, s.ID)
		}
		err = e
	case "permission scheme":
		all, e := x.r.FindPermissionSchemes(name)
		for _, s := range all {
			ids = append(ids, s.Binding.ID)
		}
		err = e
	case "notification scheme":
		all, e := x.r.FindNotificationSchemes(name)
		for _, s := range all {
			ids = append(ids, s.Binding.ID)
		}
		err = e
	case "issue security scheme":
		all, e := x.r.FindIssueSecuritySchemes(name)
		for _, s := range all {
			ids = append(ids, s.Binding.ID)
		}
		err = e
	case "priority scheme":
		all, e := x.r.FindPrioritySchemes(name)
		for _, s := range all {
			ids = append(ids, s.Space.Binding.ID)
		}
		err = e
	}
	if err != nil {
		return "", err
	}
	if len(ids) != 1 {
		return "", errors.New(util.Plural(len(ids), noun, noun+"s") + " on the site are named " + quote(name) + "; one is needed")
	}
	return ids[0], nil
}

// priorityMappings map each priority the space would lose onto the new
// scheme's default: Jira requires a mapping even when no work item has it
// (step 20 has already counted that none does).
func priorityMappings(env *Env, x resolver, target string) (map[string]int, error) {
	schemes, err := x.r.FindPrioritySchemes(target)
	if err != nil {
		return nil, err
	}
	if len(schemes) != 1 {
		return nil, fmt.Errorf("%s on the site are named %q; one is needed", util.Plural(len(schemes), "priority scheme", "priority schemes"), target)
	}
	current, err := x.r.PriorityScheme(env.P)
	if err != nil {
		return nil, err
	}
	def := 0
	for _, p := range schemes[0].Definitions {
		if p.Name == schemes[0].Space.Default.V {
			def, _ = strconv.Atoi(p.ID)
		}
	}
	out := map[string]int{}
	for _, p := range current.Definitions {
		if !slices.Contains(schemes[0].Space.Priorities, p.Name) {
			out[p.ID] = def
		}
	}
	return out, nil
}

// stopUsing takes a notification or issue security scheme off the space:
// Jira's way is the scheme ID -1 (a null is accepted and ignored).
func stopUsing(ctx context.Context, snd Sender, env *Env, noun string) error {
	key, ok := map[string]string{"notification scheme": "notificationScheme", "issue security scheme": "issueSecurityScheme"}[noun]
	if !ok {
		// checkcover:unreachable only these two kinds can be left without a scheme (the spec says null).
		return fmt.Errorf("a space cannot stop using its %s", noun)
	}
	return snd.Send(ctx, http.MethodPut, "/rest/api/3/project/"+env.P.Key, nil, map[string]int{key: -1}, nil)
}
