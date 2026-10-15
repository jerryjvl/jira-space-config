// Package spec defines the jira-space-config file format: its types, a strict
// decoder with file:line:col diagnostics, offline validation, and the
// canonical writer. The format is described in docs/spec-format.md.
//
// Decoding and writing are both hand-written over yaml.Node rather than
// struct tags, so that:
//   - an explicit null ("must be empty") is distinguishable from an absent
//     key ("not compared");
//   - every value keeps its source position for diagnostics;
//   - unknown keys are errors, with suggestions;
//   - the canonical output's key order and style are fully controlled.
package spec

// APIVersion and Kind identify the format.
const (
	APIVersion = "jira-space-config/v1alpha1"
	Kind       = "SpaceConfig"
)

// Pos is a position in the source file (1-based). Zero means unknown.
type Pos struct {
	Line, Col int
}

// Opt is an optional value. Set is false when the key is absent (the value
// is not compared); Null is true for an explicit null (the value must be
// empty in Jira).
type Opt[T any] struct {
	Set  bool
	Null bool
	V    T
}

// Has reports whether o holds a value: set, and not null.
func (o Opt[T]) Has() bool { return o.Set && !o.Null }

// Some returns a set, non-null Opt.
func Some[T any](v T) Opt[T] { return Opt[T]{Set: true, V: v} }

// Mode says how the tool treats an object.
type Mode string

// Modes.
const (
	ModeReuse   Mode = "reuse"   // must exist; never created or changed
	ModeEnsure  Mode = "ensure"  // global objects: created if missing
	ModeManaged Mode = "managed" // shareable objects: created by this tool (the default)
)

// Meta is carried by every named object.
type Meta struct {
	Pos    Pos
	ID     string   // optional cross-check: renamed or deleted in Jira is an error
	Manual bool     // compared where readable, never written
	Notes  []string // free text, mainly with Manual
}

// Spec is a whole file.
type Spec struct {
	Pos        Pos
	APIVersion string
	Kind       string
	Metadata   Metadata
	Binding    *SiteBinding

	Roles          []Role
	Groups         []GlobalObject
	Statuses       []Status
	Resolutions    []Resolution
	Priorities     []Priority // in the site's priority order
	IssueLinkTypes []LinkType
	IssueTypes     []IssueType
	CustomFields   []CustomField

	Screens   []Screen
	Workflows []Workflow

	Space *Space

	Filters         []Filter
	Boards          []Board
	AutomationRules []AutomationRule
	IssueLayouts    []Raw

	Notes []string
}

// Metadata describes the file itself.
type Metadata struct {
	Pos         Pos
	Name        string
	Description Opt[string]
}

// SiteBinding ties IDs in the file to one site.
type SiteBinding struct {
	Pos          Pos
	Site         string
	CloudID      string
	WrittenAt    string
	ToolVersion  string
	FieldBackend string // field_configurations | field_schemes
}

// Field backends.
const (
	FieldBackendConfigurations = "field_configurations"
	FieldBackendSchemes        = "field_schemes"
)

// GlobalObject is a site-wide object identified by name: a role type,
// group, resolution or priority.
type GlobalObject struct {
	Meta
	Name        string
	Mode        Mode
	Description Opt[string]
}

// Role is a role type. DefaultMembers are added to the role in every new
// space (Jira's "default actors").
type Role struct {
	GlobalObject
	DefaultMembers []string
}

// Resolution is a global resolution.
type Resolution struct {
	GlobalObject
	SiteDefault bool // the site's default resolution
}

// Priority is a global priority.
type Priority struct {
	GlobalObject
	Colour      Opt[string] // #rrggbb
	Icon        Opt[string] // icon path on the site, e.g. /images/icons/priorities/high.svg
	SiteDefault bool        // the site's default priority
}

// Status is a global workflow status.
type Status struct {
	GlobalObject
	Category Opt[string] // todo | in_progress | done
}

// LinkType is an issue link type.
type LinkType struct {
	GlobalObject
	Inward  Opt[string]
	Outward Opt[string]
}

// IssueType is a global issue type.
type IssueType struct {
	GlobalObject
	Level  Opt[string] // subtask | standard | epic | a number above 1
	Avatar Opt[string] // avatar ID
	Icon   Opt[string] // icon path, for issue types without an avatar
}

// CustomField is a global custom field. Its per-space behaviour (options,
// defaults) lives in Space.FieldContexts.
type CustomField struct {
	GlobalObject
	Type     string // short name, or a full type key for app fields
	Searcher Opt[string]
	Locked   bool // managed by Jira or an app; cannot be edited
}

// Screen is a shareable screen.
type Screen struct {
	Meta
	Name        string
	Mode        Mode
	Description Opt[string]
	Tabs        []ScreenTab
}

// ScreenTab is one tab of a screen, with its fields in order.
type ScreenTab struct {
	Pos    Pos
	Name   string
	Fields []string
}

// Workflow is a shareable workflow.
type Workflow struct {
	Meta
	Name        string
	Mode        Mode
	Description Opt[string]
	Statuses    []WorkflowStatus
	Transitions []Transition
}

// WorkflowStatus is a status as placed in one workflow.
type WorkflowStatus struct {
	Pos        Pos
	Name       string
	Layout     *Layout
	Properties []KV
	Deprecated bool
}

// Layout is a position on the workflow canvas.
type Layout struct {
	X, Y float64
}

// KV is an ordered key/value pair of strings.
type KV struct {
	Key, Value string
}

// Transition types.
const (
	TransitionInitial  = "initial"
	TransitionDirected = "directed"
	TransitionGlobal   = "global"
)

// Transition is one workflow transition.
type Transition struct {
	Pos           Pos
	ID            string
	Name          string
	Type          string // initial | directed | global; directed if From is set
	From          []string
	To            string
	Looped        bool
	Description   Opt[string]
	Screen        Opt[string]
	Event         Opt[string] // the event the transition fires, by name
	Conditions    *Condition
	Validators    []Rule
	PostFunctions []Rule
	Triggers      []Rule
	Properties    []Property
}

// Property is a transition property; known ID-bearing keys hold lists of names.
type Property struct {
	Pos    Pos
	Key    string
	Value  string
	List   []string
	IsList bool
}

// Condition is either a group (Op set, Items) or a single rule (Rule set).
type Condition struct {
	Pos   Pos
	Op    string // all | any
	Items []Condition
	Rule  *Rule
}

// Rule is a typed workflow rule (see rules.go) or, with Type "rule", an
// opaque one carrying Jira's rule key and parameters unchanged.
type Rule struct {
	Pos    Pos
	Type   string
	Params []Param // in the order the registry defines
}

// Param is one rule parameter: a string, a list of strings, or (for the
// opaque rule's parameters and extra) a string map.
type Param struct {
	Key   string
	Kind  ParamKind
	Str   string
	List  []string
	Map   []KV
	Value Pos
}

// ParamKind is the shape of a rule parameter.
type ParamKind int

// Parameter kinds.
const (
	KindString ParamKind = iota
	KindList
	KindBool
	KindMap
)

// GeneratedSchemes are the kinds of scheme apply names after the prefix:
// "VULN Workflow Scheme". Export reads the prefix back from these names.
var GeneratedSchemes = []string{"Issue Type Scheme", "Issue Type Screen Scheme", "Field Scheme",
	"Field Configuration Scheme", "Workflow Scheme", "Permission Scheme", "Notification Scheme",
	"Issue Security Scheme", "Priority Scheme"}

// Space is the target space and everything configured for it.
type Space struct {
	Pos           Pos
	Key           string
	ID            string
	ManagedPrefix string
	Details       *Details

	IssueTypes    []SpaceIssueType
	Fields        Section[[]SpaceField] // the complete set of fields; absent means not managed
	FieldContexts []FieldContextSet

	Permissions   Section[Permissions]
	Notifications Section[Notifications]
	IssueSecurity Section[IssueSecurity]
	Priorities    Section[SpacePriorities]

	Binding *SpaceBinding

	RoleMembers []RoleMembers
	Components  []Component
	Versions    []Version
	Properties  []RawProperty
}

// Section is an optional, nullable block: absent (not compared), null (must
// not exist; only meaningful for issue security) or present.
type Section[T any] struct {
	Set  bool
	Null bool
	Pos  Pos
	V    T
}

// Has reports whether the section is present: set, and not null.
func (s Section[T]) Has() bool { return s.Set && !s.Null }

// Details are the space's own attributes.
type Details struct {
	Pos          Pos
	Name         Opt[string]
	Type         Opt[string] // software | business | service_desk | product_discovery
	Avatar       Opt[string] // avatar ID
	Description  Opt[string]
	Lead         Opt[string] // user:<accountId>
	AssigneeType Opt[string] // unassigned | space_lead
	Category     Opt[string]
	URL          Opt[string]
}

// SpaceIssueType is how one issue type behaves in the space.
type SpaceIssueType struct {
	Pos      Pos
	Name     string
	Default  bool
	Workflow Opt[string]
	Screens  *ScreenSet
	Binding  *IssueTypeBinding
}

// ScreenSet names the screens for each operation. Short is true when the
// file gave a single name for all three.
type ScreenSet struct {
	Pos                Pos
	Create, Edit, View string
	Short              bool
}

// SpaceField is one field available in the space.
type SpaceField struct {
	Pos        Pos
	Name       string
	IssueTypes []string // nil means all of the space's issue types
	FieldSettings
	ByIssueType []IssueTypeFieldSettings
}

// FieldSettings are how a field behaves; unset values are not compared.
type FieldSettings struct {
	Required    Opt[bool]
	Renderer    Opt[string] // wiki | plain
	Description Opt[string] // help text in this space
}

// IssueTypeFieldSettings override a field's settings for one issue type.
type IssueTypeFieldSettings struct {
	Pos       Pos
	IssueType string
	FieldSettings
}

// IssueTypeBinding names the containers behind one issue type.
type IssueTypeBinding struct {
	Pos                Pos
	ScreenScheme       *ContainerRef
	FieldConfiguration *ContainerRef
}

// ContainerRef identifies a Jira container object (scheme, field
// configuration, context) in a binding.
type ContainerRef struct {
	Pos         Pos
	Name        string
	ID          string
	Mode        Mode
	Scope       string // contexts only: space | shared
	Description Opt[string]
	SiteDefault bool // the site's default of its kind, used by spaces without their own
}

// Context scopes.
const (
	ScopeSpace  = "space"
	ScopeShared = "shared"
)

// FieldContextSet holds the contexts of one custom field that apply to the space.
type FieldContextSet struct {
	Pos      Pos
	Field    string
	Contexts []FieldContext
}

// FieldContext is one context: its options and default for some (or all)
// of the space's issue types.
type FieldContext struct {
	Pos        Pos
	IssueTypes []string // nil means all
	Options    []Option
	Default    Opt[Value]
	Binding    *ContainerRef
}

// Option is one select option, possibly with cascading children.
type Option struct {
	Pos      Pos
	Value    string
	Disabled bool
	Children []Option
}

// Value is a single string or a list of strings.
type Value struct {
	Str    string
	List   []string
	IsList bool
}

// Permissions are the space's permission grants.
type Permissions struct {
	Grants  []Grant
	Binding *ContainerRef
}

// Grant gives a permission to holders (kind:value strings).
type Grant struct {
	Pos        Pos
	Permission string
	Holders    []string
}

// Notifications are the space's notification rules.
type Notifications struct {
	Events  []Grant // Permission holds the event name
	Binding *ContainerRef
}

// IssueSecurity is the space's issue security scheme.
type IssueSecurity struct {
	Levels  []SecurityLevel
	Binding *ContainerRef
}

// SecurityLevel is one issue security level.
type SecurityLevel struct {
	Pos         Pos
	Name        string
	Description Opt[string]
	Default     bool
	Holders     []string
}

// SpacePriorities are the priorities available in the space.
type SpacePriorities struct {
	Priorities []string
	Default    Opt[string]
	Binding    *ContainerRef
}

// SpaceBinding names the containers keyed by issue type.
type SpaceBinding struct {
	Pos                      Pos
	IssueTypeScheme          *ContainerRef
	IssueTypeScreenScheme    *ContainerRef
	FieldConfigurationScheme *ContainerRef
	FieldScheme              *ContainerRef
	WorkflowScheme           *ContainerRef
}

// RoleMembers lists holders added to a role in the space.
type RoleMembers struct {
	Pos     Pos
	Role    string
	Holders []string
}

// Component is a space component.
type Component struct {
	Meta
	Name         string
	Description  Opt[string]
	Lead         Opt[string]
	AssigneeType Opt[string] // space_default | component_lead | space_lead | unassigned
}

// Version is a space version.
type Version struct {
	Meta
	Name        string
	Description Opt[string]
	StartDate   Opt[string] // YYYY-MM-DD
	ReleaseDate Opt[string]
	Released    Opt[bool]
	Archived    Opt[bool]
}

// RawProperty is a space entity property with an arbitrary JSON value.
type RawProperty struct {
	Pos   Pos
	Key   string
	Value any // ScalarValue, []any or []KVAny, as written
}

// Raw is an object the format records but does not model (automation
// rules, issue layouts). Name is required; the rest is kept as written.
type Raw struct {
	Pos   Pos
	Name  string
	Value any // ScalarValue, []any or []KVAny, as written
}

// Filter is a saved filter the space uses: one its boards show, or one
// shared with the space. A managed filter is made by the tool (mode
// managed, the default); a reused one must exist.
type Filter struct {
	Meta
	Name        string
	Mode        Mode
	Description Opt[string]
	JQL         string
	Viewers     []string // who may see it: space, role:<name>, group:<name>, user:<id>, logged_in, anyone
	Editors     []string // who may change it, the same kinds
}

// Board is an agile board of the space. Jira's API cannot change a board
// once made, so an existing one is compared only.
type Board struct {
	Meta
	Name   string
	Mode   Mode
	Type   string // scrum | kanban
	Filter string // the name of a filter in filters
}

// BoardTypes are the kinds of board.
var BoardTypes = []string{"scrum", "kanban"}

// ShareKinds are the holder kinds a filter's viewers and editors take.
var ShareKinds = []string{"space", "role", "group", "user", "logged_in", "anyone"}

// AutomationRule is a Jira Automation rule the space uses. Its name, mode,
// description, state and scope are the format's; Rule is the rest
// of Jira's rule object (trigger, components, actor and so on), passed
// through as it is (docs/automation.md): Jira adds to it as Automation
// grows, so it is not modelled.
type AutomationRule struct {
	Meta        // ID is the rule's UUID
	Name        string
	Mode        Mode // managed (the default): made by the tool; reuse: compared only
	Description Opt[string]
	State       string // enabled | disabled
	Scope       string // space: this space alone; shared: other spaces too, or the whole site
	Rule        any    // a mapping, in the raw forms (KVAny, []any, ScalarValue)
}

// RuleStates and RuleScopes are the values of an automation rule's state and
// scope.
var (
	RuleStates = []string{"enabled", "disabled"}
	RuleScopes = []string{"space", "shared"}
)
