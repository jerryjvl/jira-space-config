// Package steps evaluates a spec against a site, one object type per step,
// in the fixed order of All. Evaluation only reads: it says, for every
// object in the spec, whether it exists and matches, would be created, or
// has a problem that must be fixed first. Carrying out the creations is the
// apply runner's job, through a separate write client.
//
// Comparison follows the spec's rules (docs/spec-format.md): only
// attributes the spec states are compared; IDs are cross-checks; names are
// stored names, not those translated for the account.
package steps

import (
	"context"
	"net/url"
	"slices"

	"github.com/jerryjvl/jira-space-config/internal/reader"
	"github.com/jerryjvl/jira-space-config/internal/spec"
)

// Kind is what a step will do with one object, or why it cannot.
type Kind int

// The kinds, in the order a step's items are shown.
const (
	Exists  Kind = iota // exists and matches (INFO)
	Create              // missing; will be created
	Update              // exists; will be changed (the space's own, or the tool's unshared objects)
	Differs             // exists but does not match the spec (ERROR)
	Error               // cannot be applied as written (ERROR)
	Blocked             // a change that work items prevent (BLOCKED)
	Manual              // marked manual: compared, never changed, never blocks
)

// Blocking reports whether an item of this kind stops its step.
func (k Kind) Blocking() bool { return k == Differs || k == Error || k == Blocked }

// Diff is one attribute that differs, in the spec's terms.
type Diff struct {
	Attribute, Spec, Jira string
}

// Item is one object of a step.
type Item struct {
	Kind    Kind
	Name    string
	Summary string // one line: "matches (id 1)", "new status · category todo"
	Diffs   []Diff
	Fix     string // for blocking and manual items: what the operator can do
	// SiteWide is set on creations of objects every space on the site shares.
	SiteWide bool
	// Object names what the item creates or changes, for the run's summary and
	// audit log, when its name alone does not: "workflow scheme \"JSC3 Workflow Scheme\"".
	Object string
	// Requests are the API calls the item's action makes (--show-requests).
	Requests []string
	// Do carries the item out, through the apply runner's write client, and
	// returns the ID of what it created; nil for items this tool cannot
	// carry out. Every write it sends must be one of Requests.
	Do func(ctx context.Context, s Sender) (string, error)
	// Later, when set, says the item is carried out by a later step ("step 25"),
	// not this one.
	Later string
}

// Sender sends one write (the apply runner's jira.Writer). Steps never
// create one: they only describe the writes, and the runner sends them.
type Sender interface {
	Send(ctx context.Context, method, apiPath string, query url.Values, body, out any) error
}

// Step is one evaluated step.
type Step struct {
	Number int
	Title  string
	Intro  string // what the step is about and what it can and cannot change
	Noun   string // one object, for counts: "status"
	Plural string // "statuses"
	Items  []Item
	Notes  []string // what the readers noted while evaluating it
}

// Count returns how many items are of kind k.
func (s *Step) Count(k Kind) int {
	n := 0
	for _, it := range s.Items {
		if it.Kind == k {
			n++
		}
	}
	return n
}

// Blocking returns how many items stop the step.
func (s *Step) Blocking() int {
	n := 0
	for _, it := range s.Items {
		if it.Kind.Blocking() {
			n++
		}
	}
	return n
}

// Env is what steps are evaluated with.
type Env struct {
	R    *reader.Reader
	Spec *spec.Spec
	// IgnoreIDs drops the spec's IDs (--ignore-ids): objects are found by
	// name only, as for a spec written for another site.
	IgnoreIDs bool
	// AdministerJira says whether the account may create site-wide objects.
	AdministerJira bool
	// Browse says whether the account may browse the space, which reading its
	// boards needs.
	Browse bool
	// AllowUpdate are the kinds of change the operator allowed explicitly
	// (--allow-update): those that widen access.
	AllowUpdate []string
	// AllowDelete are the kinds of removal the operator allowed explicitly
	// (--allow-delete).
	AllowDelete []string
	// CheckUsage are the kinds of object whose usage may be checked the slow
	// way, one request per space (--check-usage).
	CheckUsage []string

	// FreePlan is set on the Free plan, where Jira's permissions and issue
	// security are fixed and their schemes cannot be set.
	FreePlan bool

	// P is the space.
	P *reader.Project
	// Site is the site's address, for the URLs some writes need.
	Site string

	its       *reader.IssueTypes // the space's issue types now, read on first use
	siteTypes map[string]string  // the site's issue type IDs by name, read on first use

	// planned are the objects earlier steps would create, by noun and name:
	// later steps may refer to them, as if the creations succeeded.
	planned map[string]bool

	// WorkItems is how many work items the space has; with none, no change
	// can affect one, and no safety count is needed.
	WorkItems int

	// switches are the scheme changes earlier steps decided, for step 25.
	switches []switchPlan
	// step is the step being evaluated.
	step int
}

// Planned reports whether an earlier step would create the named object.
func (e *Env) Planned(noun, name string) bool { return e.planned[noun+":"+name] }

// Definition is one step of the fixed order.
type Definition struct {
	Number int
	Title  string
	// Has reports whether the spec has anything for the step; steps without
	// are left out of the plan.
	Has func(*spec.Spec) bool
	// Eval evaluates the step.
	Eval func(*Env) (*Step, error)
}

// Total is the number of steps in the fixed order, preflight included.
const Total = 28

// All are the steps after preflight, in order.
var All = []Definition{
	{2, "Role types", func(s *spec.Spec) bool { return len(s.Roles) > 0 }, roleTypes},
	{3, "Groups", func(s *spec.Spec) bool { return len(s.Groups) > 0 }, groups},
	{4, "Statuses", func(s *spec.Spec) bool { return len(s.Statuses) > 0 }, statuses},
	{5, "Resolutions", func(s *spec.Spec) bool { return len(s.Resolutions) > 0 }, resolutions},
	{6, "Priorities", func(s *spec.Spec) bool { return len(s.Priorities) > 0 }, priorities},
	{7, "Issue link types", func(s *spec.Spec) bool { return len(s.IssueLinkTypes) > 0 }, linkTypes},
	{8, "Issue types", func(s *spec.Spec) bool { return len(s.IssueTypes) > 0 }, issueTypes},
	{9, "Custom fields", func(s *spec.Spec) bool { return len(s.CustomFields) > 0 }, customFields},
	{10, "Field contexts", func(s *spec.Spec) bool { return len(s.Space.FieldContexts) > 0 }, fieldContexts},
	{11, "Screens", func(s *spec.Spec) bool { return len(s.Screens) > 0 }, screens},
	{12, "Workflows", func(s *spec.Spec) bool { return len(s.Workflows) > 0 }, workflows},
	{13, "Issue type scheme", func(s *spec.Spec) bool { return len(s.Space.IssueTypes) > 0 }, issueTypeScheme},
	{14, "Screens of the issue types", func(s *spec.Spec) bool {
		return slices.ContainsFunc(s.Space.IssueTypes, func(t spec.SpaceIssueType) bool { return t.Screens != nil })
	}, screenSchemes},
	{15, "Fields in the space", func(s *spec.Spec) bool { return s.Space.Fields.Set }, spaceFields},
	{16, "Workflows of the issue types", func(s *spec.Spec) bool {
		return slices.ContainsFunc(s.Space.IssueTypes, func(t spec.SpaceIssueType) bool { return t.Workflow.Set })
	}, workflowScheme},
	{17, "Permissions", func(s *spec.Spec) bool { return s.Space.Permissions.Set }, permissions},
	{18, "Notifications", func(s *spec.Spec) bool { return s.Space.Notifications.Set }, notifications},
	{19, "Issue security", func(s *spec.Spec) bool { return s.Space.IssueSecurity.Set }, issueSecurity},
	{20, "Priorities in the space", func(s *spec.Spec) bool { return s.Space.Priorities.Set }, prioritySchemeStep},
	{21, "Space details", func(s *spec.Spec) bool { return s.Space.Details != nil }, details},
	{22, "Components", func(s *spec.Spec) bool { return len(s.Space.Components) > 0 }, components},
	{23, "Versions", func(s *spec.Spec) bool { return len(s.Space.Versions) > 0 }, versions},
	{24, "Role members", func(s *spec.Spec) bool { return len(s.Space.RoleMembers) > 0 }, roleMembers},
	{25, "Scheme switches", func(*spec.Spec) bool { return true }, schemeSwitches},
	{26, "Filters", func(s *spec.Spec) bool { return len(s.Filters) > 0 }, filters},
	{27, "Boards", func(s *spec.Spec) bool { return len(s.Boards) > 0 }, boards},
	{28, "Automation rules", func(s *spec.Spec) bool {
		return len(s.IssueLayouts)+len(s.AutomationRules) > 0
	}, automationRules},
}

// globalIntro introduces the steps for global objects; it is shown once,
// before the first of them.
const globalIntro = "Site-wide objects (role types, groups, statuses, resolutions, priorities, issue link types, issue types " +
	"and custom fields) are shared by every space on the site, so existing ones are compared, never changed. " +
	"Missing ones with mode ensure are created; missing ones with mode reuse must be created first."

// evaluate evaluates a step that uses the global objects' introduction.
func evaluate(env *Env, number int, title, noun, plural string, items func() ([]Item, error)) (*Step, error) {
	return evaluateWith(env, number, title, noun, plural, globalIntro, items)
}

// evaluateWith evaluates one step: its items, and what the readers noted.
func evaluateWith(env *Env, number int, title, noun, plural, intro string, items func() ([]Item, error)) (*Step, error) {
	// A step evaluated again (re-evaluate, or just before carrying it out)
	// decides its switches afresh.
	env.step = number
	env.switches = slices.DeleteFunc(env.switches, func(s switchPlan) bool { return s.step == number })
	before := len(env.R.Notes())
	list, err := items()
	if err != nil {
		return nil, err
	}
	if env.planned == nil {
		env.planned = map[string]bool{}
	}
	for _, it := range list {
		if it.Kind == Create {
			env.planned[noun+":"+it.Name] = true
		}
	}
	return &Step{
		Number: number, Title: title, Noun: noun, Plural: plural, Items: list,
		Intro: intro,
		Notes: slices.Clone(env.R.Notes()[before:]),
	}, nil
}

// issueTypes returns the space's issue types as they are now.
func (e *Env) issueTypes() (*reader.IssueTypes, error) {
	if e.its == nil {
		its, err := e.R.IssueTypeScheme(e.P)
		if err != nil {
			return nil, err
		}
		e.its = its
	}
	return e.its, nil
}

// issueTypeID returns the ID of a site issue type by name; "" for one an
// earlier step would create.
func (e *Env) issueTypeID(name string) (string, error) {
	if e.siteTypes == nil {
		all, err := e.R.SiteIssueTypes()
		if err != nil {
			return "", err
		}
		e.siteTypes = map[string]string{}
		for _, t := range all {
			e.siteTypes[t.Name] = t.ID
		}
	}
	return e.siteTypes[name], nil
}

// evaluateWithNotes is evaluateWith for steps that add notes of their own
// to what the readers noted.
func evaluateWithNotes(env *Env, number int, title, noun, plural, intro string, items func() ([]Item, []string, error)) (*Step, error) {
	var notes []string
	st, err := evaluateWith(env, number, title, noun, plural, intro, func() ([]Item, error) {
		list, n, err := items()
		notes = n
		return list, err
	})
	if err != nil {
		return nil, err
	}
	st.Notes = append(st.Notes, notes...)
	return st, nil
}

// switchPlan is a scheme the space would stop using, for another (or none),
// with what must be counted before the switch is safe.
type switchPlan struct {
	step           int // the step that decided it
	noun, from, to string
	safety         []safety
}

// safety is one dangerous condition: the work
// items a JQL query finds would be affected by a change.
type safety struct {
	name    string // what is affected: status "In Triage" (issue type "Bug")
	jql     string
	summary string // after the count: "are in this status, which the new workflow lacks"
	fix     string
}

// cancelSwitch withdraws the switch to a kind of scheme, when the step that
// planned it finds it blocked or not allowed.
func (e *Env) cancelSwitch(noun string) {
	e.switches = slices.DeleteFunc(e.switches, func(s switchPlan) bool { return s.noun == noun })
}

// Refresh makes the next evaluation read Jira afresh through r, so it sees
// what the steps before it have created.
func (e *Env) Refresh(r *reader.Reader) {
	e.R, e.its, e.siteTypes = r, nil, nil
}
