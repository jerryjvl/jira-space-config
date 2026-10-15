package spec

import (
	"strings"
	"testing"
)

const header = "apiVersion: jira-space-config/v1alpha1\nkind: SpaceConfig\nmetadata: {name: t}\n"

func diagnose(src string) Diagnostics {
	s, diags := Parse([]byte(src))
	if !diags.HasErrors() {
		diags = append(diags, Validate(s)...)
	}
	return diags.Sorted()
}

func TestDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		src  string
		line int // 0: don't check
		want string
	}{
		// Automation rules.
		{"rule state", header + "space: {key: T, managed_prefix: T}\nautomation_rules:\n  - {name: T R, state: paused, rule: {trigger: {}}}\n", 6, `"paused" is not a rule state`},
		{"rule scope", header + "space: {key: T, managed_prefix: T}\nautomation_rules:\n  - {name: T R, scope: site, rule: {trigger: {}}}\n", 6, `"site" is not a rule scope`},
		{"shared rule managed", header + "space: {key: T, managed_prefix: T}\nautomation_rules:\n  - {name: T R, scope: shared, rule: {trigger: {}}}\n", 6, "a shared rule is used by other spaces too"},
		{"rule without body", header + "space: {key: T, managed_prefix: T}\nautomation_rules:\n  - {name: T R}\n", 6, "a rule needs its body"},
		{"rule without trigger", header + "space: {key: T, managed_prefix: T}\nautomation_rules:\n  - {name: T R, rule: {components: []}}\n", 6, "the rule has no trigger"},
		{"group description", header + "space: {key: T}\ngroups:\n  - {name: g, mode: ensure, description: text}\n", 0, "Jira keeps no description for a group"},
		// Filters and boards.
		{"filter without jql", header + "space: {key: T, managed_prefix: T}\nfilters:\n  - {name: T F, jql: ' '}\n", 6, "must not be empty"},
		{"viewer with a value", header + "space: {key: T, managed_prefix: T}\nfilters:\n  - {name: T F, jql: x, viewers: ['space:T']}\n", 6, `"space:T" takes no value`},
		{"board type", header + "space: {key: T, managed_prefix: T}\nfilters:\n  - {name: T F, jql: x}\nboards:\n  - {name: T B, type: list, filter: T F}\n", 8, `"list" is not a board type`},
		{"board filter", header + "space: {key: T, managed_prefix: T}\nboards:\n  - {name: T B, type: scrum, filter: T G}\n", 6, `"T G" is not one of the spec's filters`},
		// Structure.
		{"empty file", "", 0, "the file is empty"},
		{"bad yaml", "a: [", 0, "not valid YAML"},
		{"two documents", header + "space: {key: T}\n---\na: 1\n", 0, "more than one YAML document"},
		{"alias", header + "x: &a 1\ny: *a\n", 0, "anchors and aliases"},
		{"wrong kind", "apiVersion: jira-space-config/v1alpha1\nkind: Space\nmetadata: {name: t}\nspace: {key: T}\n", 2, `must be "SpaceConfig"`},
		{"missing metadata", "apiVersion: jira-space-config/v1alpha1\nkind: SpaceConfig\nspace: {key: T}\n", 0, `missing required key "metadata"`},
		{"empty required value", "apiVersion: jira-space-config/v1alpha1\nkind: SpaceConfig\nmetadata: {name: ''}\nspace: {key: T}\n", 3, "must not be empty"},
		{"mapping expected", header + "space: [T]\n", 4, "expected a mapping (key: value lines), found a list"},
		{"single value expected", header + "space: {key: [T]}\n", 4, "expected a single value, found a list"},
		{"list expected", header + "space: {key: T}\nstatuses: {name: A}\n", 5, "expected a list, found a mapping"},
		{"mapping expected for named entries", header + "space:\n  key: T\n  properties: [a]\n", 6, "expected a mapping, found a list"},
		{"number expected", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [{name: A, layout: {x: left, y: 1}}]\n    transitions: [{name: C, type: initial, to: A}]\n", 7, `expected a number, found "left"`},
		{"number missing", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [{name: A, layout: {x: 1}}]\n    transitions: [{name: C, type: initial, to: A}]\n", 7, `missing required key "y"`},
		{"bad field backend", "apiVersion: jira-space-config/v1alpha1\nkind: SpaceConfig\nmetadata: {name: t}\nbinding: {site: https://x.atlassian.net, cloud_id: c, field_backend: both}\nspace: {key: T}\n", 4, "must be field_configurations or field_schemes"},
		{"condition group with all and any", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [A]\n    transitions:\n      - name: C\n        type: initial\n        to: A\n        conditions: {all: [], any: []}\n", 12, "either all or any, not both"},

		{"two site default priorities", header + "space: {key: T}\npriorities:\n  - {name: P, mode: reuse, site_default: true}\n  - {name: Q, mode: reuse, site_default: true}\n", 0, "only one priority can be the site default, found 2"},
		{"bad issue type level", header + "space: {key: T}\nissue_types: [{name: E, mode: reuse, level: top}]\n", 5, "must be subtask, standard, epic or a number above 1"},
		{"level 1 is not above 1", header + "space: {key: T}\nissue_types: [{name: E, mode: reuse, level: '1'}]\n", 5, "must be subtask, standard, epic or a number above 1"},
		{"created field needs a type", header + "space: {key: T}\ncustom_fields: [{name: F, mode: ensure}]\n", 5, "needs a type, so it can be created"},
		{"managed screen without tabs", header + "space: {key: T, managed_prefix: T}\nscreens: [{name: T S}]\n", 5, "needs at least one tab"},
		{"field twice on a screen", header + "space: {key: T, managed_prefix: T}\nscreens:\n  - name: T S\n    tabs: [{name: A, fields: [Summary]}, {name: B, fields: [Summary]}]\n", 0, `field "Summary" is on this screen more than once`},
		{"initial with from", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [A]\n    transitions: [{name: C, type: initial, from: [A], to: A}]\n", 0, "an initial transition has a to status and no from"},
		{"initial without to", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [A]\n    transitions: [{name: C, type: initial}]\n", 0, "an initial transition has a to status and no from"},
		{"global with from", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [A]\n    transitions:\n      - {name: C, type: initial, to: A}\n      - {name: X, type: global, from: [A], to: A}\n", 0, "available from every status, so it has no from"},
		{"bad transition type", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [A]\n    transitions:\n      - {name: C, type: initial, to: A}\n      - {name: X, type: loop, to: A}\n", 0, "must be initial, directed or global"},
		{"transition without to", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [A]\n    transitions:\n      - {name: C, type: initial, to: A}\n      - {name: X, type: global}\n", 0, "needs a to status, or looped: true"},
		{"prefix with spaces", header + "space: {key: T, managed_prefix: ' T'}\n", 4, "must not start or end with spaces"},
		{"duplicate option", header + "space:\n  key: T\n  field_contexts:\n    F: [{issue_types: all, options: [x, x]}]\n", 0, `option "x" appears more than once`},
		{"duplicate child option", header + "space:\n  key: T\n  field_contexts:\n    F: [{issue_types: all, options: [{value: x, children: [a, a]}]}]\n", 0, `option "a" appears more than once under "x"`},
		{"contexts must be a list", header + "space:\n  key: T\n  field_contexts:\n    F: {issue_types: all}\n", 7, "expected a list of contexts, found a mapping"},
		{"fields cannot be null", header + "space:\n  key: T\n  fields: null\n", 6, "omit fields to leave them unmanaged"},
		{"context needs issue types", header + "space:\n  key: T\n  field_contexts:\n    F: [{options: [x]}]\n", 7, `missing required key "issue_types"`},
		{"bad context scope", header + "space:\n  key: T\n  field_contexts:\n    F: [{issue_types: all, binding: {name: C, mode: reuse, scope: site}}]\n", 7, "must be space or shared"},
		{"notifications may be null", header + "space:\n  key: T\n  notifications: null\n", 0, ""},
		{"priorities cannot be null", header + "space:\n  key: T\n  priorities: null\n", 6, "omit priorities instead"},
		{"two default security levels", header + "space:\n  key: T\n  issue_security:\n    levels: [{name: A, default: true}, {name: B, default: true}]\n", 0, "only one security level can be the default, found 2"},
		{"field holder in a security level", header + "space:\n  key: T\n  issue_security:\n    levels: [{name: A, holders: [\"group_field:F\"]}]\n", 0, "does not accept \"group_field\" as a member of a security level"},
		{"lead must be a user", header + "space:\n  key: T\n  details: {lead: 'group:admins'}\n", 6, "only user holders are allowed here"},
		{"empty context issue types", header + "space:\n  key: T\n  field_contexts:\n    F: [{issue_types: [], options: [x]}]\n", 7, "use all, or list at least one issue type"},
		{"repeated transition name warns", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [A]\n    transitions:\n      - {name: C, type: initial, to: A}\n      - {name: X, type: global, to: A}\n      - {name: X, type: global, to: A}\n", 0, `another transition is also named "X"`},
		{"duplicate top-level key", header + "space: {key: T}\nspace: {key: U}\n", 5, `key "space" appears more than once (first on line 4)`},
		{"duplicate nested key", header + "space:\n  key: T\n  key: U\n", 6, `key "key" appears more than once`},
		{"duplicate key in a key-to-value mapping", header + "space:\n  key: T\n  properties: {a: 1, a: 2}\n", 6, `key "a" appears more than once`},
		{"duplicate key inside a property value", header + "space:\n  key: T\n  properties:\n    a: {b: 1, b: 2}\n", 7, `key "b" appears more than once`},
		{"wrong version", "apiVersion: v0\nkind: SpaceConfig\nmetadata: {name: t}\nspace: {key: T}\n", 1, "unsupported version"},
		{"unknown top-level key with suggestion", header + "space: {key: T}\nstatus: []\n", 5, `unknown key "status" (did you mean "statuses"?)`},
		{"unknown nested key", header + "space: {key: T}\nstatuses:\n  - {name: A, mode: reuse, catgory: todo}\n", 6, `did you mean "category"?`},
		{"missing space", header, 0, `missing required key "space"`},
		{"mode required on globals", header + "space: {key: T}\nstatuses: [{name: A}]\n", 5, `missing required key "mode"`},
		{"bad mode", header + "space: {key: T}\nstatuses: [{name: A, mode: managed}]\n", 5, "must be one of reuse, ensure"},
		{"yes is not a bool", header + "space:\n  key: T\n  issue_types:\n    - {name: A, default: yes}\n", 7, "expected true or false"},
		{"list required", header + "space: {key: T}\nscreens:\n  - {name: X, mode: reuse, tabs: [{name: F, fields: Summary}]}\n", 6, "expected a list"},

		// Global objects.
		{"duplicate name", header + "space: {key: T}\nstatuses:\n  - {name: A, mode: reuse}\n  - {name: A, mode: reuse}\n", 7, `"A" appears more than once`},
		{"ensure status needs category", header + "space: {key: T}\nstatuses: [{name: A, mode: ensure}]\n", 5, "needs a category"},
		{"bad category", header + "space: {key: T}\nstatuses: [{name: A, mode: reuse, category: doing}]\n", 5, "todo, in_progress or done"},
		{"epic cannot be created", header + "space: {key: T}\nissue_types: [{name: E, mode: ensure, level: epic}]\n", 5, "cannot be created through the API"},
		{"field type typo", header + "space: {key: T}\ncustom_fields: [{name: F, mode: ensure, type: slect}]\n", 5, `did you mean "select"?`},
		{"any type key for a reused field", header + "space: {key: T}\ncustom_fields: [{name: F, mode: reuse, type: read-only-string-issue-field}]\n", 0, ""},
		{"app field not creatable", header + "space: {key: T}\ncustom_fields: [{name: F, mode: ensure, type: com.example:thing}]\n", 5, "app-provided field types"},
		{"link type needs both descriptions", header + "space: {key: T}\nissue_link_types: [{name: L, mode: ensure, inward: x}]\n", 5, "inward and outward"},

		// Managed names.
		{"managed needs prefix", header + "space: {key: T}\nscreens: [{name: X, tabs: [{name: F}]}]\n", 5, "space.managed_prefix must be set"},
		{"managed name lacks prefix", header + "space: {key: T, managed_prefix: T}\nscreens: [{name: Other, tabs: [{name: F}]}]\n", 5, `must start with the prefix "T"`},
		{"prefix without space", header + "space: {key: T, managed_prefix: T}\nscreens: [{name: TScreen, tabs: [{name: F}]}]\n", 5, "followed by a space"},
		{"reuse needs no prefix", header + "space: {key: T}\nscreens: [{name: Default Screen, mode: reuse}]\n", 0, ""},

		// Workflows.
		{"no initial", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [A]\n    transitions: [{name: X, type: global, to: A}]\n", 6, "exactly one initial transition"},
		{"status not in workflow", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [A]\n    transitions:\n      - {name: C, type: initial, to: A}\n      - {name: X, from: [A], to: B}\n", 10, `status "B" is not one of this workflow's statuses`},
		{"directed needs from", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [A]\n    transitions:\n      - {name: C, type: initial, to: A}\n      - {name: X, to: A}\n", 10, "needs from"},
		{"looped with to", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [A]\n    transitions:\n      - {name: C, type: initial, to: A}\n      - {name: X, from: [A], to: A, looped: true}\n", 10, "has no to"},
		{"unknown rule type", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [A]\n    transitions:\n      - name: C\n        type: initial\n        to: A\n        validators: [{type: fields_requird, fields: [X]}]\n", 12, `did you mean "fields_required"?`},
		{"rule in wrong list", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [A]\n    transitions:\n      - name: C\n        type: initial\n        to: A\n        post_functions: [{type: fields_required, fields: [X]}]\n", 12, "unknown post functions rule type"},
		{"rule missing param", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [A]\n    transitions:\n      - name: C\n        type: initial\n        to: A\n        post_functions: [{type: set_field, field: X}]\n", 12, `needs "value"`},
		{"rule bad enum", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [A]\n    transitions:\n      - name: C\n        type: initial\n        to: A\n        conditions: {all: [{type: block_all, mode: everyone}]}\n", 12, "users, users_and_api"},
		{"restrict_to needs something", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [A]\n    transitions:\n      - name: C\n        type: initial\n        to: A\n        conditions: {all: [{type: restrict_to}]}\n", 12, "at least one of roles"},
		{"condition group needs op", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [A]\n    transitions:\n      - name: C\n        type: initial\n        to: A\n        conditions: {}\n", 12, "needs all: [...] or any: [...]"},
		{"list property only for known keys", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [A]\n    transitions:\n      - name: C\n        type: initial\n        to: A\n        properties: {other: [x]}\n", 12, "take a list of names"},

		// Space.
		{"bad space key", header + "space: {key: t-1}\n", 4, "not a valid space key"},
		{"two defaults", header + "space:\n  key: T\n  issue_types:\n    - {name: A, default: true}\n    - {name: B, default: true}\n", 5, "only one issue type can be the default"},
		{"fields per issue type moved", header + "space:\n  key: T\n  issue_types:\n    - name: A\n      fields: {Summary: {required: true}}\n", 8, "listed once for the whole space"},
		{"field issue type not in space", header + "space:\n  key: T\n  issue_types: [{name: A}]\n  fields:\n    - {name: Summary, issue_types: [B]}\n", 8, `"B" is not one of the space's issue types`},
		{"by_issue_type outside restriction", header + "space:\n  key: T\n  issue_types: [{name: A}, {name: B}]\n  fields:\n    - {name: Summary, issue_types: [A], by_issue_type: {B: {required: true}}}\n", 8, "limited to A, so it has no settings"},
		{"duplicate field", header + "space:\n  key: T\n  fields:\n    - {name: Summary}\n    - {name: Summary}\n", 8, `"Summary" appears more than once`},
		{"bad renderer", header + "space:\n  key: T\n  fields:\n    - {name: Description, renderer: rich}\n", 7, "wiki, plain"},
		{"empty field restriction", header + "space:\n  key: T\n  fields:\n    - {name: Description, issue_types: []}\n", 7, "list at least one issue type"},
		{"context issue type not in space", header + "space:\n  key: T\n  issue_types: [{name: A}]\n  field_contexts:\n    F: [{issue_types: [B], options: [x]}]\n", 8, `"B" is not one of the space's issue types`},
		{"overlapping contexts", header + "space:\n  key: T\n  issue_types: [{name: A}]\n  field_contexts:\n    F:\n      - {issue_types: all, options: [x]}\n      - {issue_types: [A], options: [y]}\n", 10, "overlaps another"},
		{"two space contexts for one field", header + "space:\n  key: T\n  issue_types: [{name: A}, {name: B}]\n  field_contexts:\n    F:\n      - {issue_types: [A], options: [x]}\n      - {issue_types: [B], options: [y]}\n", 10, "one context of a field to be specific to a space"},
		{"space context plus shared context is fine", header + "space:\n  key: T\n  issue_types: [{name: A}, {name: B}]\n  field_contexts:\n    F:\n      - {issue_types: [A], options: [x]}\n      - {issue_types: [B], options: [y], binding: {name: Global, scope: shared, mode: reuse}}\n", 0, ""},
		{"default not an option", header + "space:\n  key: T\n  field_contexts:\n    F: [{issue_types: all, options: [x], default: y}]\n", 7, `default "y" is not one of the options`},
		{"shared context must be reuse", header + "space:\n  key: T\n  managed_prefix: T\n  field_contexts:\n    F: [{issue_types: all, binding: {name: T C, scope: shared}}]\n", 8, "must be mode reuse"},
		{"permissions cannot be null", header + "space:\n  key: T\n  permissions: null\n", 6, "omit permissions"},
		{"bad holder kind", header + "space:\n  key: T\n  permissions: {grants: {BROWSE_PROJECTS: [rol:Admins]}}\n", 6, `did you mean "role"?`},
		{"holder needs value", header + "space:\n  key: T\n  permissions: {grants: {BROWSE_PROJECTS: ['group:']}}\n", 6, "needs a value"},
		{"holder takes no value", header + "space:\n  key: T\n  permissions: {grants: {BROWSE_PROJECTS: ['reporter:x']}}\n", 6, "takes no value"},
		{"user by email", header + "space:\n  key: T\n  details: {lead: 'user:me@example.com'}\n", 6, "account ID, not email"},
		{"anyone warns", header + "space:\n  key: T\n  permissions: {grants: {BROWSE_PROJECTS: [anyone]}}\n", 6, "apply refuses it unless explicitly overridden"},
		{"permission key form", header + "space:\n  key: T\n  permissions: {grants: {Browse Projects: [reporter]}}\n", 6, "written as Jira's keys"},
		{"priority default not listed", header + "space:\n  key: T\n  priorities: {priorities: [High], default: Low}\n", 6, "not one of the listed priorities"},
		{"bad colour", header + "space: {key: T}\npriorities: [{name: P, mode: reuse, colour: red}]\n", 5, "#rrggbb"},
		{"created priority needs a colour", header + "space: {key: T}\npriorities: [{name: P, mode: ensure}]\n", 5, "a priority with mode ensure needs a colour"},
		{"bad space type", header + "space:\n  key: T\n  details: {type: kanban}\n", 6, "software, business"},
		{"bad date", header + "space:\n  key: T\n  versions: [{name: '1', start_date: 1/2/2026}]\n", 6, "YYYY-MM-DD"},
		{"both field backends", header + "space:\n  key: T\n  binding:\n    field_configuration_scheme: {name: X, mode: reuse}\n    field_scheme: {name: Y, mode: reuse}\n", 7, "not both"},

		// IDs.
		{"ids need a binding", header + "space: {key: T, id: '1'}\n", 4, "add binding.site and binding.cloud_id"},

		// External references are info, not errors.
		{"external status", header + "space: {key: T, managed_prefix: T}\nworkflows:\n  - name: T W\n    statuses: [Done]\n    transitions: [{name: C, type: initial, to: Done}]\n", 7, `status "Done" is not declared in this spec`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diags := diagnose(tc.src)
			if tc.want == "" {
				if diags.HasErrors() {
					t.Errorf("unexpected errors: %v", diags)
				}
				return
			}
			var found *Diagnostic
			for i := range diags {
				if strings.Contains(diags[i].Message, tc.want) {
					found = &diags[i]
					break
				}
			}
			if found == nil {
				var all []string
				for _, d := range diags {
					all = append(all, d.Format("t"))
				}
				t.Fatalf("no diagnostic contains %q; got:\n%s", tc.want, strings.Join(all, "\n"))
			}
			if tc.line != 0 && found.Pos.Line != tc.line {
				t.Errorf("diagnostic %q on line %d; want line %d", found.Message, found.Pos.Line, tc.line)
			}
		})
	}
}

func TestDiagnosticFormat(t *testing.T) {
	d := Diagnostic{Severity: SeverityError, Pos: Pos{Line: 3, Col: 5}, Path: "space.key", Message: "bad"}
	if got := d.Format("spec.yaml"); got != "spec.yaml:3:5: error: space.key: bad" {
		t.Errorf("Format = %q", got)
	}
}
