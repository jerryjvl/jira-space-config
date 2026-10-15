package steps

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// kind describes one type of object for the shared evaluation: a global
// object (mode reuse or ensure), or a shareable one (mode reuse or managed).
type kind[T any] struct {
	noun, plural string
	// shareable objects are created by this tool for the space (mode
	// managed); global ones are created for the whole site (mode ensure).
	shareable bool
	object    func(T) spec.GlobalObject
	// compare returns the stated attributes of want that live does not match.
	compare func(want, live T) []Diff
	// create describes the creation of want: "new status · category todo".
	create func(want T) string
	// request is the call that creates one ("POST /rest/api/3/statuses");
	// requests, when set, lists them all for objects that take several.
	request  string
	requests func(want T) []string
	// do, when set, returns the action that creates want.
	do func(want T) func(ctx context.Context, s Sender) (string, error)
	// check, when set, says why want cannot be created as written (what it
	// refers to does not exist and will not be created), or "".
	check func(want T) (problem, fix string)
	// noDescription is set for objects Jira keeps no description for (groups):
	// a description in the spec is not compared.
	noDescription bool
	// update, when set, plans changing a managed object to match want;
	// usage lists the spaces that use it, and
	// must be set with it: only an object this space alone uses is changed.
	update func(want, live T) change
	usage  func(live T) ([]string, error)
	// anyone is set for objects any account may create (filters, boards):
	// creating them needs no Administer Jira.
	anyone bool
	// fixed, when set, is the fix for an object that differs and that
	// Jira's API cannot change.
	fixed string
}

// globals evaluates the spec's objects of one type against the live ones
// found for them (by name, near-miss name and ID).
func globals[T any](env *Env, k kind[T], wanted, live []T) ([]Item, error) {
	var items []Item
	for _, w := range wanted {
		l, err := global(env, k, w, live)
		if err != nil {
			return nil, err
		}
		items = append(items, l...)
	}
	return items, nil
}

func global[T any](env *Env, k kind[T], w T, live []T) ([]Item, error) {
	want := k.object(w)
	id := want.ID
	if env.IgnoreIDs {
		id = ""
	}
	item := Item{Name: want.Name}

	var match *T
	if id != "" {
		for i := range live {
			if k.object(live[i]).ID == id {
				match = &live[i]
			}
		}
		switch {
		case match == nil:
			item.Kind = Error
			item.Summary = fmt.Sprintf("has id %s in the spec, but Jira has no %s with that ID", id, k.noun)
			item.Fix = "If it was deleted in Jira, remove the id from the spec so it is found, or created, by name. " +
				"If the spec was written for another site, run with --ignore-ids."
			return []Item{item}, nil
		case k.object(*match).Name != want.Name:
			item.Kind = Error
			item.Summary = fmt.Sprintf("id %s is named %q in Jira", id, k.object(*match).Name)
			item.Fix = fmt.Sprintf("If it was renamed in Jira, change the name in the spec to %q. "+
				"If the name in the spec is the one you want, rename it in Jira, or remove the id.", k.object(*match).Name)
			return []Item{item}, nil
		}
	} else {
		var exact []int
		for i := range live {
			if k.object(live[i]).Name == want.Name {
				exact = append(exact, i)
			}
		}
		switch {
		case len(exact) > 1:
			var ids []string
			for _, i := range exact {
				ids = append(ids, k.object(live[i]).ID)
			}
			item.Kind = Error
			item.Summary = fmt.Sprintf("%d %s on the site have this name (ids %s)", len(exact), k.plural, strings.Join(ids, ", "))
			item.Fix = "Add the id of the one you mean to the spec."
			return []Item{item}, nil
		case len(exact) == 1:
			match = &live[exact[0]]
		}
	}

	if match == nil {
		return []Item{missing(env, k, w, live)}, nil
	}
	got := k.object(*match)
	if k.shareable && want.Mode == spec.ModeManaged && got.Mode != spec.ModeManaged {
		item.Kind = Error
		item.Summary = fmt.Sprintf("exists (id %s), but this tool did not create it (its description has no %s marker)", got.ID, "[managed-by]")
		item.Fix = fmt.Sprintf("Set mode: reuse to use the %s as it is, or give the managed one another name.", k.noun)
		return []Item{item}, nil
	}
	diffs := k.compare(w, *match)
	if d, ok := compareText("description", want.Description, got.Description); ok && !k.noDescription {
		diffs = append([]Diff{d}, diffs...)
	}
	switch {
	case len(diffs) == 0:
		item.Kind = Exists
		item.Summary = fmt.Sprintf("matches (id %s)", got.ID)
	case want.Manual:
		item.Kind = Manual
		item.Summary = fmt.Sprintf("exists (id %s); marked manual, so these differences are yours to resolve:", got.ID)
		item.Diffs = diffs
		item.Fix = "Change it in Jira by hand, or change the spec to match."
	case k.update != nil && want.Mode == spec.ModeManaged:
		item.Diffs = diffs
		return updateManaged(env, k, w, *match, item)
	default:
		item.Kind = Differs
		item.Summary = fmt.Sprintf("exists (id %s) but does not match the spec:", got.ID)
		item.Diffs = diffs
		var attrs []string
		for _, d := range diffs {
			attrs = append(attrs, d.Attribute)
		}
		change := "Change " + strings.Join(attrs, " and ") + " in the spec to match Jira"
		switch {
		case !k.shareable:
			item.Fix = fmt.Sprintf("%s, or use a different name. Jira's %s is shared by every space on the site and will not be changed.", change, k.noun)
		case k.fixed != "":
			item.Fix = change + ". " + k.fixed
		case want.Mode == spec.ModeReuse:
			item.Fix = fmt.Sprintf("%s. A reused %s is used as it is and never changed.", change, k.noun)
		default:
			item.Fix = fmt.Sprintf("%s, or change it in Jira by hand: this tool does not change an existing %s.", change, k.noun)
		}
	}
	return []Item{item}, nil
}

// missing is an object that is not on the site.
func missing[T any](env *Env, k kind[T], w T, live []T) Item {
	want := k.object(w)
	item := Item{Name: want.Name}
	for _, l := range live {
		if got := k.object(l); normalise(got.Name) == normalise(want.Name) {
			item.Kind = Error
			item.Summary = fmt.Sprintf("not found, but Jira has %q (id %s)", got.Name, got.ID)
			item.Fix = fmt.Sprintf("If you mean that %s, use its exact name. Creating %q as well would give the site "+
				"two %s that differ only in case or spacing.", k.noun, want.Name, k.plural)
			return item
		}
	}
	switch {
	case want.Mode == spec.ModeReuse:
		item.Kind = Error
		item.Summary = "not found; mode reuse needs it to exist"
		if k.shareable {
			item.Fix = fmt.Sprintf("Check the spelling, create the %s in Jira, or set mode: managed to have one created for the space.", k.noun)
		} else {
			item.Fix = fmt.Sprintf("Check the spelling, create the %s in Jira, or set mode: ensure to have it created.", k.noun)
		}
	case !env.AdministerJira && !k.anyone:
		item.Kind = Error
		item.Summary = "missing, and this account cannot create it"
		item.Fix = fmt.Sprintf("Creating a %s needs the Administer Jira global permission. Ask a site admin to grant it, "+
			"or to create the %s by hand.", k.noun, k.noun)
	case k.check != nil && checkFails(k, w, &item):
	default:
		item.Kind = Create
		item.Summary = k.create(w)
		item.SiteWide = !k.shareable
		item.Requests = []string{k.request}
		if k.requests != nil {
			item.Requests = k.requests(w)
		}
		if k.do != nil {
			item.Do = k.do(w)
		}
	}
	return item
}

// normalise is how near-miss names are found: the same apart from case and
// spacing.
func normalise(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// show is how a value is displayed in a difference.
func show(o spec.Opt[string]) string {
	if !o.Has() || o.V == "" {
		return "none"
	}
	return o.V
}

// compareText compares a stated text attribute. null in the spec means
// "none".
func compareText(attr string, want, got spec.Opt[string]) (Diff, bool) {
	if !want.Set {
		return Diff{}, false
	}
	if show(want) == show(got) {
		return Diff{}, false
	}
	return Diff{Attribute: attr, Spec: show(want), Jira: show(got)}, true
}

// compareFlag compares a flag the spec can only state as true.
func compareFlag(attr string, want, got bool) (Diff, bool) {
	if !want || got {
		return Diff{}, false
	}
	return Diff{Attribute: attr, Spec: "true", Jira: "false"}, true
}

// compareSet compares a list stated in the spec as a set.
func compareSet(attr string, want, got []string) (Diff, bool) {
	if want == nil {
		return Diff{}, false
	}
	a, b := slices.Clone(want), slices.Clone(got)
	slices.Sort(a)
	slices.Sort(b)
	if slices.Equal(a, b) {
		return Diff{}, false
	}
	list := func(l []string) string {
		if len(l) == 0 {
			return "none"
		}
		return strings.Join(l, ", ")
	}
	return Diff{Attribute: attr, Spec: list(want), Jira: list(got)}, true
}

// diffList collects differences: l.add(compareText(...)).
type diffList []Diff

func (l *diffList) add(d Diff, found bool) {
	if found {
		*l = append(*l, d)
	}
}

func checkFails[T any](k kind[T], w T, item *Item) bool {
	problem, fix := k.check(w)
	if problem == "" {
		return false
	}
	item.Kind, item.Summary, item.Fix = Error, problem, fix
	return true
}

// change is how a managed object would be changed to match the spec.
type change struct {
	// class is how the change is described:
	// "additive" when it only adds, "altering" when it changes or takes
	// away.
	class    string
	requests []string
	do       action
	// removes lists what the change takes away, which is made only with
	// --allow-delete=<allow>.
	removes []string
	allow   string
	// widens lists what the change gives access to, which is made only with
	// --allow-update=<widenAllow>.
	widens     []string
	widenAllow string
	// safety are the work item counts that block the change.
	safety []safety
	// problem, when set, says why the change cannot be made as written.
	problem, fix string
}

// updateManaged plans changing a managed object to match the spec, if this
// space alone uses it: the usage is read now,
// just before the change would be made.
func updateManaged[T any](env *Env, k kind[T], w, got T, item Item) ([]Item, error) {
	obj := k.object(got)
	users, err := k.usage(got)
	if err != nil {
		return nil, err
	}
	what := fmt.Sprintf("exists (id %s)", obj.ID)
	if refused, shared := refuseIfShared(env, item, users, what, fmt.Sprintf("A %s other spaces use is never changed. Change the spec to match, "+
		"or give the %s another name, so a new one is made for this space alone.", k.noun, k.noun)); shared {
		return []Item{refused}, nil
	}
	return changeItems(env, item, k.update(w, got), what, k.noun+" "+quote(obj.Name))
}

// changeItems turns a planned change into the step's items: the change,
// and any work item counts that block it. what says what the object is:
// "exists (id 10100)".
func changeItems(env *Env, item Item, ch change, what, object string) ([]Item, error) {
	if ch.problem != "" {
		item.Kind, item.Summary, item.Fix = Error, fmt.Sprintf("%s but does not match the spec, and %s:", what, ch.problem), ch.fix
		return []Item{item}, nil
	}
	if len(ch.removes) > 0 && !slices.Contains(env.AllowDelete, ch.allow) {
		item.Kind = Error
		item.Summary = fmt.Sprintf("%s but does not match the spec, and matching it removes %s:", what, someOf(ch.removes))
		item.Fix = fmt.Sprintf("Removing is done only when allowed explicitly: run with --allow-delete=%s, or keep them in the spec.", ch.allow)
		return []Item{item}, nil
	}
	if len(ch.widens) > 0 && !slices.Contains(env.AllowUpdate, ch.widenAllow) {
		item.Kind = Error
		item.Summary = fmt.Sprintf("%s but does not match the spec, and matching it widens access: %s:", what, someOf(ch.widens))
		item.Fix = fmt.Sprintf("Changes that widen access are made only when allowed explicitly: run with --allow-update=%s, "+
			"or make the change by hand.", ch.widenAllow)
		return []Item{item}, nil
	}
	blocked, err := runSafety(env, ch.safety)
	if err != nil {
		return nil, err
	}
	item.Kind = Update
	item.Summary = fmt.Sprintf("%s, used by this space alone; it is changed to match (%s):", what, ch.class)
	item.Fix = "" // the Differs item's fix, if it was one, no longer applies
	item.Requests, item.Do, item.Object = ch.requests, ch.do, object
	return append([]Item{item}, blocked...), nil
}

// otherSpaces says which other spaces use an object: "another space uses
// it too (id 10077)".
func otherSpaces(ids []string) string {
	if len(ids) == 1 {
		return fmt.Sprintf("another space uses it too (id %s)", ids[0])
	}
	return fmt.Sprintf("%d other spaces use it too (ids %s)", len(ids), strings.Join(ids, ", "))
}

// refuseIfShared is the sharing check before an
// object is changed in place: when other spaces use it, the item says so, as
// an error, and the object is left alone. what says what the object is
// ("exists (id 10100)"), and fix what to do instead.
func refuseIfShared(env *Env, item Item, users []string, what, fix string) (Item, bool) {
	others := slices.DeleteFunc(slices.Clone(users), func(id string) bool { return id == env.P.ID })
	if len(others) == 0 {
		return item, false
	}
	item.Kind = Error
	item.Summary = fmt.Sprintf("%s but does not match the spec, and %s:", what, otherSpaces(others))
	item.Fix = fix
	return item, true
}
