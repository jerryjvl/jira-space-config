package steps

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"

	"github.com/jerryjvl/jira-space-config/internal/jira"
	"github.com/jerryjvl/jira-space-config/internal/spec"
	"github.com/jerryjvl/jira-space-config/internal/util"
)

// Changes in place to a space's managed notification and permission schemes: their grants and recipients.

// grantChange is how a scheme's grants (or notifications) differ: the
// holders each permission (or event) gains and loses.
type grantChange struct {
	key         string // the permission, or the event
	add, remove []string
}

func grantChanges(want, got []spec.Grant) []grantChange {
	holders := func(l []spec.Grant, key string) []string {
		var out []string
		for _, g := range l {
			if g.Permission == key {
				out = append(out, g.Holders...)
			}
		}
		return out
	}
	var keys []string
	for _, g := range append(slices.Clone(want), got...) {
		keys = util.AppendNew(keys, g.Permission)
	}
	var out []grantChange
	for _, k := range keys {
		w, g := holders(want, k), holders(got, k)
		c := grantChange{key: k}
		c.add, c.remove = addedAndRemoved(w, g)
		if c.add != nil || c.remove != nil {
			out = append(out, c)
		}
	}
	return out
}

// planGrants fills in a change from grant differences: additions widen
// access (--allow-update=kind), removals take it away (--allow-delete=kind).
func planGrants(changes []grantChange, kind, noun string) change {
	ch := change{class: "additive", allow: kind, widenAllow: kind}
	for _, c := range changes {
		for _, h := range c.add {
			ch.widens = append(ch.widens, fmt.Sprintf("%s for %s", h, c.key))
		}
		for _, h := range c.remove {
			ch.removes = append(ch.removes, fmt.Sprintf("%s %s from %s", noun, h, c.key))
		}
	}
	if len(ch.removes) > 0 {
		ch.class = "removing"
	}
	return ch
}

// updateNotificationScheme gives a managed notification scheme's events
// the spec's recipients.
func updateNotificationScheme(env *Env, x resolver, id string, want, got []spec.Grant) change {
	changes := grantChanges(want, got)
	ch := planGrants(changes, "notifications", "recipient")
	adds, removes := 0, 0
	for _, c := range changes {
		adds, removes = adds+len(c.add), removes+len(c.remove)
	}
	var requests []string
	if adds > 0 {
		requests = append(requests, "PUT /rest/api/3/notificationscheme/{id}/notification")
	}
	for range removes {
		requests = append(requests, "DELETE /rest/api/3/notificationscheme/{notificationSchemeId}/notification/{notificationId}")
	}
	ch.requests = countRequests(requests)
	ch.do = func(ctx context.Context, snd Sender) (string, error) {
		var scheme struct {
			Events []struct {
				Event struct {
					ID jira.ID `json:"id"`
				} `json:"event"`
				Notifications []struct {
					ID        jira.ID `json:"id"`
					Type      string  `json:"notificationType"`
					Parameter string  `json:"parameter"`
				} `json:"notifications"`
			} `json:"notificationSchemeEvents"`
		}
		path := "/rest/api/3/notificationscheme/" + id
		if err := snd.Send(ctx, http.MethodGet, path, url.Values{"expand": {"all"}}, nil, &scheme); err != nil {
			return "", err
		}
		var events []map[string]any
		var gone []string
		for _, c := range changes {
			eid, err := x.event(c.key)
			if err != nil {
				return "", err
			}
			var to []map[string]string
			for _, h := range c.add {
				n, err := x.holderBody(h, spec.PlaceNotification)
				if err != nil {
					return "", err
				}
				to = append(to, n)
			}
			if to != nil {
				events = append(events, map[string]any{"event": map[string]string{"id": eid}, "notifications": to})
			}
			for _, h := range c.remove {
				typ, value, err := x.holder(h, spec.PlaceNotification)
				if err != nil {
					return "", err
				}
				found := ""
				for _, e := range scheme.Events {
					if string(e.Event.ID) != eid {
						continue
					}
					for _, n := range e.Notifications {
						if n.Type == typ && n.Parameter == value {
							found = string(n.ID)
						}
					}
				}
				if found == "" {
					return "", fmt.Errorf("the scheme has no %s recipient %s now; it was changed since this step was evaluated", c.key, h)
				}
				gone = append(gone, found)
			}
		}
		if events != nil {
			if err := snd.Send(ctx, http.MethodPut, path+"/notification", nil, map[string]any{"notificationSchemeEvents": events}, nil); err != nil {
				return "", err
			}
		}
		for _, n := range gone {
			if err := snd.Send(ctx, http.MethodDelete, path+"/notification/"+n, nil, nil, nil); err != nil {
				return "", err
			}
		}
		return id, nil
	}
	return ch
}

// updatePermissionScheme gives a managed permission scheme the spec's
// grants: each one added widens access (--allow-update=permissions), each
// one taken away needs --allow-delete=permissions. Step 17's checks (the
// lock-out, grants to every user) apply as to a new scheme. Which spaces
// use a permission scheme is read space by space, so only with
// --check-usage=permission_scheme and Administer Jira.
func updatePermissionScheme(env *Env, x resolver, id string, want, got []spec.Grant) change {
	switch {
	case !slices.Contains(env.CheckUsage, "permission_scheme"):
		return change{problem: "which other spaces use it is not known",
			fix: "Jira cannot say which spaces use a permission scheme, so checking means reading every space's, one request " +
				"each. Run with --check-usage=permission_scheme to have it checked, or change the scheme by hand."}
	case !env.AdministerJira:
		return change{problem: "this account cannot see every space to check which use it",
			fix: "Checking which spaces use a permission scheme needs the Administer Jira global permission."}
	}
	changes := grantChanges(want, got)
	ch := planGrants(changes, "permissions", "grant to")
	adds, removes := 0, 0
	for _, c := range changes {
		adds, removes = adds+len(c.add), removes+len(c.remove)
	}
	var requests []string
	for range adds {
		requests = append(requests, "POST /rest/api/3/permissionscheme/{schemeId}/permission")
	}
	for range removes {
		requests = append(requests, "DELETE /rest/api/3/permissionscheme/{schemeId}/permission/{permissionId}")
	}
	ch.requests = countRequests(requests)
	ch.do = func(ctx context.Context, snd Sender) (string, error) {
		path := "/rest/api/3/permissionscheme/" + id + "/permission"
		var current struct {
			Permissions []struct {
				ID     jira.ID `json:"id"`
				Holder struct {
					Type      string `json:"type"`
					Parameter string `json:"parameter"`
					Value     string `json:"value"`
				} `json:"holder"`
				Permission string `json:"permission"`
			} `json:"permissions"`
		}
		if err := snd.Send(ctx, http.MethodGet, path, nil, nil, &current); err != nil {
			return "", err
		}
		for _, c := range changes {
			for _, h := range c.add {
				holder, err := x.holderBody(h, spec.PlacePermission)
				if err != nil {
					return "", err
				}
				if err := snd.Send(ctx, http.MethodPost, path, nil, map[string]any{"permission": c.key, "holder": holder}, nil); err != nil {
					return "", err
				}
			}
			for _, h := range c.remove {
				typ, value, err := x.holder(h, spec.PlacePermission)
				if err != nil {
					return "", err
				}
				found := ""
				for _, p := range current.Permissions {
					if p.Permission == c.key && p.Holder.Type == typ && (p.Holder.Value == value || p.Holder.Parameter == value) {
						found = string(p.ID)
					}
				}
				if found == "" {
					return "", fmt.Errorf("the scheme no longer grants %s to %s; it was changed since this step was evaluated", c.key, h)
				}
				if err := snd.Send(ctx, http.MethodDelete, path+"/"+found, nil, nil, nil); err != nil {
					return "", err
				}
			}
		}
		return id, nil
	}
	return ch
}
