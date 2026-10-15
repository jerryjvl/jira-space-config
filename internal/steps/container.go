package steps

import (
	"fmt"
	"slices"

	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// Containers are the schemes that tie a space's definition to Jira objects:
// which issue types it has, which screens and workflows each uses, which
// fields it has: the binding layer, which the spec need not name. A spec
// states the definition; the container is found or made to match it:
//
//   - the space's current container matches: nothing to do;
//   - it does not, and this tool made it: it is changed in place, if no
//     other space uses it (or reported as different, for a kind with no
//     update: field configuration schemes);
//   - otherwise the container the spec names (binding), or one with a
//     generated name, is used: created if missing, and step 25 switches the
//     space to it. Jira's own and shared schemes are never changed.

// line is one line of a definition: an issue type and its workflow, a
// field and its settings. Lines are compared by key.
type line struct{ key, value string }

// found is a container on the site and its definition.
type found struct {
	ID, Name string
	Managed  bool
	Lines    []line
}

// container is one container to evaluate.
type container struct {
	noun  string // "workflow scheme"
	title string // the item's name: "Workflow scheme"
	want  []line
	// current is the space's container now; its Name is how the space shows it.
	current found
	binding *spec.ContainerRef
	// generated is the name for a new one when the spec names none; "" when
	// the space has no managed_prefix to make one with.
	generated string
	find      func(name string) ([]found, error)
	// complete is set when the spec lists the whole definition (the fields
	// in the space): lines only Jira has then differ too.
	complete bool
	// create describes a new one: "5 issue types".
	create   string
	requests []string
	// do, when set, creates the container of the given name.
	do func(name string) action
	// safety, when set, lists what must be counted before the space can
	// switch away from its current container (step 25 counts it).
	safety func() ([]safety, error)
	// update, when set, plans changing the space's current container, when
	// the tool made it, to the spec's definition;
	// usage lists the spaces that use it, and must be set with it.
	update func() change
	usage  func() ([]string, error)
}

func evalContainer(env *Env, c container) (Item, error) {
	items, err := evalContainerItems(env, c)
	if err != nil {
		return Item{}, err
	}
	return items[0], nil
}

// evalContainerItems evaluates a container: its item, and the work item
// counts that block changing it in place.
func evalContainerItems(env *Env, c container) ([]Item, error) {
	item := Item{Name: c.title}
	diffs := compareLines(c.want, c.current.Lines, c.complete)
	namesOther := c.binding != nil && c.binding.Name != c.current.Name
	if len(diffs) > 0 && c.current.Managed && !namesOther && c.update != nil {
		return updateContainer(env, c, item, diffs)
	}
	item, err := containerItem(env, c, item, diffs, namesOther)
	return []Item{item}, err
}

func containerItem(env *Env, c container, item Item, diffs []Diff, namesOther bool) (Item, error) {
	switch {
	case len(diffs) == 0 && !namesOther:
		item.Kind = Exists
		item.Summary = fmt.Sprintf("the space's, %s, matches", describe(c.current))
		return item, nil
	case len(diffs) > 0 && c.current.Managed && !namesOther:
		item.Kind = Differs
		item.Summary = fmt.Sprintf("the space's, %s, does not match the spec:", describe(c.current))
		item.Diffs = diffs
		item.Fix = fmt.Sprintf("Change the spec to match, or change the %s in Jira by hand: this tool does not change "+
			"an existing %s.", c.noun, c.noun)
		return item, nil
	}

	name, mode := c.generated, spec.ModeManaged
	if c.binding != nil {
		name, mode = c.binding.Name, c.binding.Mode
	}
	if name == "" {
		item.Kind = Error
		item.Summary = fmt.Sprintf("the space's, %s, does not match, and a new %s needs a name", describe(c.current), c.noun)
		item.Diffs = diffs
		item.Fix = fmt.Sprintf("Set space.managed_prefix, so the new %s can be named after it, or name one in the spec's binding.", c.noun)
		return item, nil
	}
	candidates, err := c.find(name)
	if err != nil {
		return item, err
	}
	switch {
	case len(candidates) > 1:
		item.Kind = Error
		item.Summary = fmt.Sprintf("%d %ss on the site are named %q", len(candidates), c.noun, name)
		item.Fix = "Name another in the spec's binding, or rename one of them in Jira."
	case len(candidates) == 0 && mode == spec.ModeReuse:
		item.Kind = Error
		item.Summary = fmt.Sprintf("the spec's binding names %q, which does not exist; mode reuse needs it to", name)
		item.Fix = fmt.Sprintf("Check the name, or set the binding's mode to managed to have the %s created.", c.noun)
	case len(candidates) == 0 && !env.AdministerJira:
		item.Kind = Error
		item.Summary = fmt.Sprintf("a new %s %q is needed, and this account cannot create it", c.noun, name)
		item.Fix = fmt.Sprintf("Creating a %s needs the Administer Jira global permission. Ask a site admin to grant it.", c.noun)
	case len(candidates) == 0:
		if err := env.planSwitch(c, name); err != nil {
			return item, err
		}
		item.Kind = Create
		item.Summary = fmt.Sprintf("new %s %q · %s; step 25 switches the space to it from %q", c.noun, name, c.create, c.current.Name)
		item.Requests = c.requests
		if c.do != nil {
			item.Do = c.do(name)
		}
		item.Object = c.noun + " " + quote(name)
	case mode == spec.ModeManaged && !candidates[0].Managed:
		item.Kind = Error
		item.Summary = fmt.Sprintf("%s exists, but this tool did not create it", describe(candidates[0]))
		item.Fix = fmt.Sprintf("Name it in the spec's binding with mode: reuse to use it as it is, or name another %s.", c.noun)
	default:
		if d := compareLines(c.want, candidates[0].Lines, c.complete); len(d) > 0 {
			item.Kind = Differs
			item.Summary = fmt.Sprintf("%s exists but does not match the spec:", describe(candidates[0]))
			item.Diffs = d
			item.Fix = fmt.Sprintf("Change the spec to match, or name another %s in the binding.", c.noun)
			return item, nil
		}
		if err := env.planSwitch(c, name); err != nil {
			return item, err
		}
		item.Kind = Update
		item.Summary = fmt.Sprintf("%s matches; step 25 switches the space to it from %q", describe(candidates[0]), c.current.Name)
		item.Later = "step 25"
	}
	return item, nil
}

func describe(f found) string {
	if f.ID == "" {
		return fmt.Sprintf("%q", f.Name)
	}
	return fmt.Sprintf("%q (id %s)", f.Name, f.ID)
}

// compareLines compares definitions line by line: the spec's lines in its
// order, then, for a complete definition, lines only Jira has.
func compareLines(want, got []line, complete bool) []Diff {
	value := func(lines []line, key string) (string, bool) {
		for _, l := range lines {
			if l.key == key {
				return l.value, true
			}
		}
		return "none", false
	}
	var out []Diff
	for _, w := range want {
		if g, _ := value(got, w.key); g != w.value {
			out = append(out, Diff{Attribute: w.key, Spec: w.value, Jira: g})
		}
	}
	for _, g := range got {
		if complete && !slices.ContainsFunc(want, func(w line) bool { return w.key == g.key }) {
			out = append(out, Diff{Attribute: g.key, Spec: "none", Jira: g.value})
		}
	}
	return out
}

// planSwitch records that the space would switch to the named container,
// with the safety checks that switch needs.
func (e *Env) planSwitch(c container, name string) error {
	plan := switchPlan{step: e.step, noun: c.noun, from: c.current.Name, to: name}
	if c.safety != nil && e.WorkItems > 0 {
		checks, err := c.safety()
		if err != nil {
			return err
		}
		plan.safety = checks
	}
	e.switches = append(e.switches, plan)
	return nil
}

// updateContainer plans changing the space's managed container in place,
// if no other space uses it.
func updateContainer(env *Env, c container, item Item, diffs []Diff) ([]Item, error) {
	item.Diffs = diffs
	users, err := c.usage()
	if err != nil {
		return nil, err
	}
	if refused, shared := refuseIfShared(env, item, users, "the space's, "+describe(c.current)+", exists",
		fmt.Sprintf("A %s other spaces use is never changed. Change the spec to match, or name another %s in the spec's binding.", c.noun, c.noun)); shared {
		return []Item{refused}, nil
	}
	ch := c.update()
	if c.safety != nil && env.WorkItems > 0 {
		checks, err := c.safety()
		if err != nil {
			return nil, err
		}
		ch.safety = append(ch.safety, checks...)
	}
	items, err := changeItems(env, item, ch, "the space's, "+describe(c.current)+", exists", c.noun+" "+quote(c.current.Name))
	if err == nil && items[0].Kind == Update {
		items[0].Summary = fmt.Sprintf("the space's, %s, used by this space alone, is changed to match (%s):", describe(c.current), ch.class)
	}
	return items, err
}
