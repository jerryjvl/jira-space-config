package steps

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strconv"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// Changes in place to a space's managed priority scheme, when no other space uses it.

// updatePriorityScheme makes a managed priority scheme's priorities and
// default the spec's. A priority it loses (with --allow-delete=priorities;
// step 20's counts block it while work items have it) is mapped onto the
// default, as Jira requires.
func updatePriorityScheme(env *Env, x resolver, id string, want, got spec.SpacePriorities) change {
	ch := change{class: "additive", allow: "priorities", requests: []string{"PUT /rest/api/3/priorityscheme/{schemeId}"}}
	var add, remove []string
	for _, p := range want.Priorities {
		if !slices.Contains(got.Priorities, p) {
			add = append(add, p)
		}
	}
	for _, p := range got.Priorities {
		if !slices.Contains(want.Priorities, p) {
			remove = append(remove, p)
			ch.removes = append(ch.removes, "priority "+quote(p))
		}
	}
	def := cmp.Or(want.Default.V, want.Priorities[0])
	switch {
	case len(remove) > 0:
		ch.class = "removing"
	case def != got.Default.V:
		ch.class = "altering"
	}
	ch.do = func(ctx context.Context, snd Sender) (string, error) {
		ids := func(names []string) ([]int, error) {
			var out []int
			for _, n := range names {
				pid, err := x.priority(n)
				if err != nil {
					return nil, err
				}
				i, _ := strconv.Atoi(pid)
				out = append(out, i)
			}
			return out, nil
		}
		added, err := ids(add)
		if err != nil {
			return "", err
		}
		removed, err := ids(remove)
		if err != nil {
			return "", err
		}
		d, err := ids([]string{def})
		if err != nil {
			return "", err
		}
		body := map[string]any{"defaultPriorityId": d[0]}
		priorities := map[string]any{}
		if len(added) > 0 {
			priorities["add"] = map[string]any{"ids": added}
		}
		if len(removed) > 0 {
			priorities["remove"] = map[string]any{"ids": removed}
			in := map[string]int{}
			for _, r := range removed {
				in[strconv.Itoa(r)] = d[0]
			}
			body["mappings"] = map[string]any{"in": in}
		}
		if len(priorities) > 0 {
			body["priorities"] = priorities
		}
		if err := snd.Send(ctx, http.MethodPut, "/rest/api/3/priorityscheme/"+id, nil, body, nil); err != nil {
			return "", err
		}
		return id, nil
	}
	return ch
}
