# The spec format

What each key of a spec means, what the tool does with it, and what is
refused (`apiVersion: jira-space-config/v1alpha1`). Where this document and the
tool disagree, `validate` is the final word. Commands and their options are in
[cmdline.md](cmdline.md); automation rules in detail in
[automation.md](automation.md).

## 1. What a spec is

One spec describes **one existing company-managed Jira Cloud space**: its
issue types, fields, screens, workflows, permissions, notifications,
priorities, components, versions, filters, boards and automation rules, and
the site-wide objects (statuses, custom fields, and so on) it needs.

Properties of the tool that shape what you write:

- **It never creates a space.** The space (`space.key`) must exist. Create it
  in Jira first, from the *company-managed* software template.
- **It is conservative.** It creates what is missing, compares what exists,
  and changes only objects it created itself (marked with
  `[managed-by: jira-space-config]` in their description) and only while no
  other space uses them. Jira's own objects, and anything shared with other
  spaces, are never edited.
- **It refers to things by name**, not ID. IDs are optional cross-checks.
- **It compares only what you write.** A key you leave out is not compared
  and not changed, with a few listed exceptions (section 7).
- **Running it twice changes nothing the second time.** After a successful
  apply, a dry run of the same spec reports nothing to do.

## 2. Working on a spec

1. Start from `export` of a space you know, or write the spec by hand.
   An export is the best way to learn the format: it is exactly what the
   tool reads back.
2. `validate` until it reports `0 errors`.
3. `apply --dry-run` and read the plan: every item is `✓ exists`,
   `+ create`, `~ update`, `✗` (fix the spec or Jira first), `■ blocked`
   (existing work items prevent it) or `? manual` (by hand). A change that
   widens access or removes something needs its `--allow-update` or
   `--allow-delete` option, which the fix names.
4. Fix until nothing is `✗` or `■`, then `apply`. A dry run afterwards
   finds nothing to do.

## 3. File conventions

- **YAML, one document per file.** Anchors and aliases (`&`, `*`) are refused.
  Duplicate keys are refused. Comments are fine (the tool never rewrites your
  file).
- **Unknown keys are errors**, with a "did you mean" suggestion. A typo cannot
  silently do nothing.
- **Booleans are `true` or `false`** only (not `yes`/`no`). Other scalars are
  read as written, so `version: 1.10` and `name: yes` stay strings; quote
  anything YAML might read as a number or boolean (`"1.0"`, `"10042"`).
- **Required at the top**: `apiVersion` (exactly `jira-space-config/v1alpha1`),
  `kind` (exactly `SpaceConfig`), `metadata.name`, and `space` with `space.key`.
  Every other section is optional.
- **Sections can be in any order.** The tool applies them in its own fixed
  order (section 9).
- **Names are Jira's stored names**, as `export` writes them. Jira shows some
  names translated for your account language ("Issue colour" for the stored
  "Issue color"); the spec uses the stored name. `export` notes any it finds.

### Names, IDs and cross-checks

Everything is referred to by name. Any named object may also carry an `id`:

```yaml
statuses:
  - {name: Open, mode: reuse, id: "1"}
```

An `id` is a cross-check, never a command. If an object with that ID is
missing, or has a different name, the tool stops and says so, instead of
guessing. IDs only mean something on one site, so a spec containing IDs
needs `binding.site` and `binding.cloud_id`; `--ignore-ids` sets them aside
for a run. A hand-written spec normally has **no IDs at all**; they come from
`export`.

Names a spec uses without declaring them (a status in a workflow, a field on
a screen) are not errors: they must already exist on the site, and
`validate` says how many it is trusting. Declaring them in the spec with a
mode makes the tool check or create them.

### Keys any named object may carry

| Key | Meaning |
| --- | --- |
| `id` | The cross-check above |
| `manual: true` | Compare it where readable but never write it; shown as `? manual` |
| `notes` | A list of free-text strings for readers; ignored by the tool |

### Modes

Every object that can be created has a `mode`, which says who owns it:

| Mode | Applies to | Meaning |
| --- | --- | --- |
| `reuse` | everything | Must already exist. Compared, never created or changed |
| `ensure` | site-wide objects (section 5) | Created if missing. Once it exists it is only compared, never changed, because other spaces may use it |
| `managed` | screens, workflows, schemes, filters, boards, automation rules (the **default** for these) | Created by this tool for this space (named with the space's `managed_prefix`); changed in place only while this space alone uses it |

A `managed` object whose name is taken by something the tool did not create
is an error ("exists, but this tool did not create it"): name yours
differently, or set `mode: reuse` to use the existing one as it is.

`space.managed_prefix` is required as soon as anything is `managed`: every
managed name must start with the prefix followed by a space (`VULN Workflow`).
The tool also generates names from it (section 9).

### Comparison: what counts as a difference

For an object that exists, **only the attributes you write are compared**:
`{name: Open, mode: reuse}` accepts any category; adding `category: todo` makes
the category part of the check. A difference on a `reuse` or `ensure` object is
an error to fix in the spec (or in Jira): the tool does not edit shared objects.

`null` is different from absent. Where a key accepts it, `null` means
"must be none" (`category: null` takes the space out of its category), while
leaving the key out means "don't care".

### Holders: who a rule is about

Permissions, notifications, issue security levels, filters, role members and
transitions name people as **`kind:value`** strings, split on the first colon:

| Kind | Value | Meaning |
| --- | --- | --- |
| `role` | role name | A project role: `role:Administrators` |
| `group` | group name | `group:engineering` |
| `user` | **account ID** | `user:5b10ac8d82e05b22cc7d4ef5` (never an email address) |
| `user_field`, `group_field` | custom field name | The user or group in a field |
| `email` | address | Notification recipient by email address |
| `reporter`, `current_assignee`, `current_user`, `space_lead`, `component_lead`, `all_watchers` | none | The person in that capacity |
| `anyone` | none | Everyone, including logged-out users |
| `application_role` | optional | Anyone with access to a product (without a value, any logged-in user) |

Which places accept which kinds is in the section for each. `anyone` and
`application_role` in a permission scheme widen access the furthest, so apply
refuses them unless `--allow-update=permissions` is given.

## 4. Top-level keys

```yaml
apiVersion: jira-space-config/v1alpha1
kind: SpaceConfig
metadata:
  name: vulnerability-management      # required; a label for the file
  description: Optional free text.
binding:                              # written by export; omit it in a hand-written spec
  site: https://example.atlassian.net
  cloud_id: 00000000-0000-0000-0000-000000000000
  written_at: 2026-10-13T08:20:00Z
  tool_version: 0.1.0
  field_backend: field_schemes        # or field_configurations
space: {key: VULN}                  # required
```

`binding` ties a spec's IDs to one site (section 3). A spec with no IDs needs
no `binding`. `field_backend` records which of Jira's two field systems the
space uses; on current sites it is `field_schemes`, and a hand-written spec
leaves it out.

The remaining top-level keys are sections 5 to 8:

| Key | What it is |
| --- | --- |
| `roles`, `groups`, `statuses`, `resolutions`, `priorities`, `issue_link_types`, `issue_types`, `custom_fields` | Site-wide objects (section 5) |
| `screens`, `workflows` | Shareable objects (section 6) |
| `space` | The space itself (section 7) |
| `filters`, `boards`, `automation_rules`, `issue_layouts` | Views and automation (section 8) |
| `notes` | Free text; `export` uses it to list what it could not represent |

## 5. Site-wide objects

Roles, groups, statuses, resolutions, priorities, link types, issue types and
custom fields are shared by every space on the site. The tool therefore
**never changes one**, whoever created it. Each takes `mode: reuse` or
`mode: ensure` (there is no default: say which), plus `description` and `id`.

- `reuse`: it must exist; the attributes you write are compared.
- `ensure`: created if missing, with the attributes you write; compared if it
  exists. Creating needs the Administer Jira permission.

A spec only needs to declare a site-wide object to **create** it or to
**check** it. Using an existing one in a workflow or on a screen needs no
declaration.

| Section | Keys beyond `name`, `mode`, `description`, `id` | To create (`ensure`) it needs |
| --- | --- | --- |
| `roles` | `default_members`: holders added to the role in every new space | name |
| `groups` | | name |
| `statuses` | `category`: `todo`, `in_progress` or `done` | `category` |
| `resolutions` | `site_default: true` (compared only) | name |
| `priorities` | `colour`: `#rrggbb`; `icon`: path on the site, such as `/images/icons/priorities/medium.svg`; `site_default` (compared only) | `colour` |
| `issue_link_types` | `inward`, `outward`: the two descriptions | `inward` and `outward` |
| `issue_types` | `level`: `subtask`, `standard`, `epic` or a number above 1; `avatar`: avatar ID; `icon` | name (level `standard` or `subtask` only) |
| `custom_fields` | `type`: see below; `searcher`: search template key, or `default`; `locked: true` for fields Jira owns | `type` |

Role types and custom fields can share a name in Jira (a site may have several
role types called `atlassian-addons-project-access`, for instance), and a name
used twice is ambiguous: declare the one you mean with its `id`, or the
reference to it is an error.

### Custom field types

`type` is one of these short names:

| Short name | Jira field |
| --- | --- |
| `text` | Short text |
| `paragraph` | Paragraph (long text) |
| `number` | Number |
| `date`, `datetime` | Date picker, date-time picker |
| `url` | URL |
| `labels` | Labels |
| `select`, `radio`, `multiselect`, `checkboxes` | Single-choice and multi-choice lists |
| `cascading` | Cascading select (a parent option with child options) |
| `user`, `multiuser` | User picker, multiple user picker |
| `group`, `multigroup` | Group picker, multiple group picker |
| `version`, `multiversion` | Version picker, multiple version picker |
| `project` | Project picker |
| `readonly` | Read-only text |

An app-provided field has a full type key
(`com.atlassian.jira.plugin.system.customfieldtypes:...`); those can be
`reuse`d but not created through the API. The options and defaults of a
select-type field live in the space, under `space.field_contexts` (section 7).

## 6. Screens and workflows

Both are *shareable* in Jira: a screen or workflow can serve several spaces.
Their mode is `managed` (the default: created for this space) or `reuse`.

### Screens

```yaml
screens:
  - name: VULN Finding Screen          # managed names start with the prefix
    description: Used to create, edit and view findings
    tabs:
      - {name: Details, fields: [Summary, Description, Severity]}
      - {name: Planning, fields: [Assignee, Priority, Due by]}
```

- A managed screen needs at least one tab. Fields are listed by name, in
  order, and a field may appear once per screen.
- Field names are Jira's: system fields as shown in Jira (`Summary`,
  `Description`, `Assignee`, `Linked Issues` or `Linked work items`, `Fix
  versions`, ...), custom fields as declared in `custom_fields` or already on
  the site.
- A managed screen that differs from the spec is changed to match (tabs and
  field order) while only this space uses it. A `reuse` screen is compared:
  write `tabs` to have them checked, leave them out to accept any.
- Which screen each issue type uses is set under `space.issue_types`.

### Workflows

```yaml
workflows:
  - name: VULN Workflow
    description: Findings are triaged before work starts
    statuses:
      - Open                             # a bare status name
      - {name: In Triage, layout: {x: 120, y: 40}, properties: {jira.issue.editable: "true"}}
      - Done
    transitions:
      - {name: Create, type: initial, to: Open}
      - name: Start triage
        from: [Open]
        to: In Triage
        screen: VULN Finding Screen
        conditions:
          all:
            - {type: restrict_to, roles: [Triage Team]}
        validators:
          - {type: fields_required, fields: [Severity], error_message: Set the severity first}
        post_functions:
          - {type: assign, to: current_user}
      - {name: Finish, type: global, to: Done}
```

**Statuses** are the workflow's statuses, each a name or a mapping with:
`layout: {x, y}` (canvas position), `properties: {key: "value"}` (status
properties), and `deprecated: true` (compared only: Jira's API cannot create a
workflow with a deprecated status). Every status named must exist, or be
declared in `statuses`.

**Transitions** need a `name` and, depending on `type`:

| `type` | Meaning | Needs |
| --- | --- | --- |
| `initial` | How new work items enter the workflow. Exactly one per workflow | `to`; no `from`, no `looped` |
| `directed` (the default) | From specific statuses to a status | `from: [statuses]` and `to` |
| `global` | Available from every status | `to`; no `from` |

`looped: true` makes a transition that returns to the status it started from
(use it instead of `to`). Other keys:

| Key | Meaning |
| --- | --- |
| `id` | Jira's transition ID (optional; apply numbers new ones 1, 11, 21, ...) |
| `description` | Free text |
| `screen` | A screen name shown during the transition |
| `event` | The notification event the transition fires, by name (`Issue Resolved`) |
| `properties` | Transition properties, `key: value`. A property that lists resolutions (`jira.field.resolution.include`, `...exclude`) takes a list of resolution **names** |
| `conditions` | Who/what may use it: a group, below |
| `validators` | Checks before it happens: a list of rules |
| `post_functions` | Things done after it: a list of rules |
| `triggers` | Triggers: only the opaque form below |

Transitions are matched by name; names may repeat (Jira allows it) but are
easy to confuse, so `validate` warns. A `reuse` workflow with no
`transitions` is only checked to exist.

**Conditions** are a group: `all: [...]` or `any: [...]` (never both), whose
items are rules or nested groups:

```yaml
conditions:
  all:
    - {type: restrict_to, roles: [Triage Team]}
    - any:
        - {type: restrict_to, groups: [engineering]}
        - {type: previous_status, status: Open}
```

**Rules** (conditions, validators, post functions) are typed. Each type has
named parameters; a required one that is missing is an error, and `extra:` is
a `{key: value}` map for any further Jira parameter the type does not name.

| List | Type | Parameters (R = required) |
| --- | --- | --- |
| conditions | `restrict_to` | `roles`, `groups`, `permissions`, `group_fields`, `user_fields`, `deny_user_fields`, `accounts` (all lists; **at least one**) |
| conditions | `field_value` | `field` R; `values` R (list); `comparator` R (`>` `>=` `=` `<=` `<` `!=`); `comparison` R (`date`, `date_without_time`, `number`, `option`, `string`) |
| conditions | `previous_status` | `status` R; `not`, `most_recent_only`, `include_current`, `ignore_loops` (booleans) |
| conditions | `parent_child_blocking` | `statuses` R (list) |
| conditions | `separation_of_duties` | `from_status`, `to_status` (at least one) |
| conditions | `block_all` | `mode` R (`users` or `users_and_api`) |
| conditions | `block_in_progress_approval` | (none) |
| validators | `fields_required` | `fields` R (list); `ignore_context` (bool); `error_message` |
| validators | `field_changed` | `field` R; `exempt_groups` (list); `error_message` |
| validators | `field_matches` | `field` R; `regexp` R; `error_message` |
| validators | `field_single_value` | `field` R; `exclude_subtasks` (bool) |
| validators | `date_compare` | `field` R; `comparator` R; `other_field` R; `include_time` (bool) |
| validators | `date_window` | `field` R; `other_field` R; `days` R |
| validators | `permission` | `permission` R (a permission key such as `EDIT_ISSUES`) |
| validators | `previous_status` | `status` R; `most_recent_only` (bool) |
| validators | `parent_child_blocking` | `statuses` R (list) |
| validators | `forms_attached`, `forms_submitted` | (none) |
| post_functions | `assign` | `to` R (`current_user`, `default_user`, `selected_user`, `unassigned`); `user` (with `selected_user`) |
| post_functions | `set_field` | `field` R; `value` R; `mode` (`append` or `replace`) |
| post_functions | `clear_field` | `field` R |
| post_functions | `copy_field` | `from` R; `to` R; `source` (`parent` or `same`) |
| post_functions | `trigger_webhook` | `webhook` R (identifies the webhook; copy it from an export) |
| post_functions | `trigger_agent` | `agent` R (an agent's account ID); `prompt` |

Fields, statuses, roles and groups in rule parameters are written as names,
and the tool resolves them to IDs.

**Anything else** (an app's rule, or a Jira rule the table lacks) uses the
opaque form, which works in every list and is sent to Jira as written:

```yaml
post_functions:
  - {type: rule, key: "forge:workflow-post-function", parameters: {setting: "value"}}
```

**Changes to a managed workflow** are applied in place while only this space
uses it. Statuses or transitions it *loses* need `--allow-delete=workflows`,
and losing a status is blocked while work items of the space's issue types
are in it. A workflow in use by other spaces is never changed.

## 7. The space

`space` is required and describes the space itself. Every key in it is
optional except `key`; **a part you leave out is not managed**, so the tool
neither compares nor changes it. A part you include describes that part *in
full*: it is the whole answer, not an addition to what Jira has (the specific
rules are below).

```yaml
space:
  key: VULN                    # required: capitals, digits, underscores; starts with a letter
  id: "10042"                  # optional cross-check
  managed_prefix: VULN         # names of managed objects start with "VULN "
  details: {...}
  issue_types: [...]
  fields: [...]
  field_contexts: {...}
  permissions: {...}
  notifications: {...}
  issue_security: {...}
  priorities: {...}
  binding: {...}               # which schemes to use, by name (section 9)
  role_members: {...}
  components: [...]
  versions: [...]
  properties: {...}
```

### details

```yaml
details:
  name: Vulnerability Management
  description: Vulnerabilities found by the security team
  lead: "user:5b10ac8d82e05b22cc7d4ef5"
  assignee_type: unassigned     # unassigned | space_lead
  category: Security            # a space category on the site; null takes the space out of it
  url: https://example.com      # null removes it
  avatar: "10417"               # an avatar ID
  type: software                # software | business | service_desk | product_discovery
```

Each key is compared if written and updated if it differs. `type` cannot be
changed through the API: a different type is an error.

### issue_types

The issue types the space offers, **in the order they appear**. This list
becomes the space's *issue type scheme*; exactly one may be `default: true`.

```yaml
issue_types:
  - name: Vulnerability
    default: true
    workflow: VULN Workflow                      # by name
    screens: VULN Finding Screen                 # one screen for create, edit and view...
  - name: Task
    workflow: VULN Workflow
    screens: {create: VULN Create, edit: VULN Edit, view: VULN View}   # ...or one for each
    binding:                                     # optional, section 9
      screen_scheme: {name: VULN Task Screens, mode: managed}
```

- Each name is an issue type declared in `issue_types` (section 5) or
  already on the site.
- `workflow` gives the workflow that issue type uses; the set of these is the
  space's *workflow scheme*.
- `screens` gives the screens the issue type uses; the set is the space's
  *issue type screen scheme*. Issue types with identical screens share one
  screen scheme.
- Fields are **not** listed here: they are listed once for the whole space,
  under `fields` (writing `fields` here is an error).
- Leave `workflow` or `screens` out of every issue type and that scheme is
  not managed.

### fields

`fields` is the **complete list of fields available in the space**: a field
not listed is not in the space. Leaving `fields` out altogether means fields
are not managed.

```yaml
fields:
  - name: Summary
    required: true
  - name: Description
    renderer: wiki               # wiki | plain
  - name: Severity
    required: true
    description: How bad is it?  # help text shown in this space
    issue_types: [Vulnerability] # only on these of the space's issue types (default: all)
  - name: Story Points
    by_issue_type:
      Story: {required: true}    # settings for one issue type, over the entry's own
```

| Key | Meaning |
| --- | --- |
| `name` | A system field or a custom field, by name |
| `issue_types` | Only these of the space's issue types have the field |
| `required` | `true` or `false` |
| `renderer` | `wiki` or `plain`, for text fields |
| `description` | Help text for the field **in this space** (the field's own description is separate) |
| `by_issue_type` | `{issue type: {required, renderer, description}}`, for issue types in `issue_types` |

Each entry is authoritative: a setting you do not write has its default (not
required, `plain` renderer, no description). Settings in `by_issue_type`
inherit the entry's unless they write their own.

A few fields are in every space and cannot be removed from it; they do not
need to be listed to avoid a difference. Taking a field *out* of the space
needs `--allow-delete=fields` and is blocked while work items have a value
in it. Making a field required is blocked while unresolved work items have it
empty.

### field_contexts

The options and default values of select-type custom fields, **in this space**.

```yaml
field_contexts:
  Severity:                      # a custom field, by name
    - issue_types: [Vulnerability]    # required: "all", or a list of the space's issue types
      options: [Critical, High, Medium, Low, {value: Legacy, disabled: true}]
      default: Medium
  Target Type:
    - issue_types: all
      options:
        - {value: Host, children: [Linux, Windows]}      # cascading: parent and children
        - {value: Application, children: [Web, Mobile]}
      default: [Host, Linux]                             # a cascading default: [parent, child]
  Due by:
    - issue_types: all
      default: current           # a date field: today's date when the work item is created
```

- `options` are plain values or `{value, disabled, children}`, in order.
- `default` is a value; a list for multiple options (`[A, B]`), cascading
  (`[parent, child]`) or several users; `current` for a date or date-time
  meaning "when the work item is created" (or a date as `YYYY-MM-DD`);
  `user:<accountId>` for a user; or
  `null` for none. A default that is an option must be one of `options`.
- Contexts of one field may not overlap, and a space may have only one
  context of a field of its own.
- A context may carry `binding: {name, id, scope: space | shared, mode}`.
  `scope: shared` marks a context other spaces use too (Jira's *default
  configuration scheme* for the field is one); it must be `mode: reuse` and is
  never changed.
- A context of this space alone is changed in place: options added, enabled,
  disabled and ordered, defaults set, issue types added and removed.
  Removing an option, an issue type or a whole context needs
  `--allow-delete=contexts` and is blocked while work items have a value it
  would affect.

### permissions

```yaml
permissions:
  grants:
    BROWSE_PROJECTS: ["role:Administrators", "role:Triage Team", "group:engineering"]
    CREATE_ISSUES: ["role:Triage Team"]
    ADMINISTER_PROJECTS: ["role:Administrators"]
  binding: {name: VULN Permission Scheme, mode: managed}    # optional
```

- Keys are Jira's permission keys. The complete list is Jira's; the ones in a
  default software scheme are: `ADD_COMMENTS`, `ADMINISTER_PROJECTS`,
  `ARCHIVE_ISSUES`, `ASSIGNABLE_USER`, `ASSIGN_ISSUES`, `BROWSE_PROJECTS`,
  `CLOSE_ISSUES`, `CREATE_ATTACHMENTS`, `CREATE_ISSUES`,
  `DELETE_ALL_ATTACHMENTS`, `DELETE_ALL_COMMENTS`, `DELETE_ALL_WORKLOGS`,
  `DELETE_ISSUES`, `DELETE_OWN_ATTACHMENTS`, `DELETE_OWN_COMMENTS`,
  `DELETE_OWN_WORKLOGS`, `EDIT_ALL_COMMENTS`, `EDIT_ALL_WORKLOGS`,
  `EDIT_ISSUES`, `EDIT_ISSUE_LAYOUT`, `EDIT_OWN_COMMENTS`,
  `EDIT_OWN_WORKLOGS`, `EDIT_WORKFLOW`, `LINK_ISSUES`,
  `MANAGE_SPRINTS_PERMISSION`, `MANAGE_VERSIONS`, `MANAGE_WATCHERS`,
  `MODIFY_REPORTER`, `MOVE_ISSUES`, `RESOLVE_ISSUES`, `SCHEDULE_ISSUES`,
  `SET_ISSUE_SECURITY`, `TRANSITION_ISSUES`, `UNARCHIVE_ISSUES`,
  `VIEW_AGGREGATED_DATA`, `VIEW_DEV_TOOLS`, `VIEW_READONLY_WORKFLOW`,
  `VIEW_VOTERS_AND_WATCHERS`, `WORK_ON_ISSUES`.
- Holders: any kind except `email`, `component_lead`, `all_watchers` and
  `current_user`. Roles and groups are by name.
- `grants` is the **complete** scheme: a permission or holder Jira has and the
  spec does not is a difference.
- `permissions` cannot be `null`; every space has a permission scheme.
- **Safeguards**: a new scheme that grants `ADMINISTER_PROJECTS` to no one is
  refused (it would lock everyone out); one that grants anything to `anyone`
  or `application_role` is refused unless `--allow-update=permissions`.
  Jira's Free plan cannot have a custom permission scheme: a spec that needs
  one is an error there.
- A permission scheme the tool made is changed in place only with
  `--check-usage=permission_scheme` (Jira cannot say which spaces use one, so
  the tool must ask each space); grants added need
  `--allow-update=permissions`, grants removed `--allow-delete=permissions`.

### notifications

```yaml
notifications:
  events:
    Issue Created: ["role:Triage Team", reporter]
    Issue Resolved: [reporter, current_assignee, all_watchers]
  binding: {name: VULN Notification Scheme, mode: managed}
```

- Event names are Jira's, as listed in its notification scheme editor:
  `Generic Event`, `Issue Assigned`, `Issue Closed`, `Issue Comment Edited`,
  `Issue Commented`, `Issue Created`, `Issue Deleted`, `Issue Moved`,
  `Issue Reopened`, `Issue Resolved`, `Issue Updated`, `Issue Worklog Deleted`,
  `Issue Worklog Updated`, `Work Logged On Issue`, `Work Started On Issue`,
  `Work Stopped On Issue` (plus custom events, which the API cannot create).
- Recipients: `role`, `group`, `user`, `email`, `user_field`, `group_field`,
  `reporter`, `current_assignee`, `current_user`, `space_lead`,
  `component_lead`, `all_watchers`.
- `notifications: null` means the space has **no** notification scheme (a
  space created through the API starts that way). Leaving the key out means
  not managed.
- Recipients added need `--allow-update=notifications`; removed,
  `--allow-delete=notifications`.

### issue_security

```yaml
issue_security:
  levels:
    - name: Internal
      description: Staff only
      default: true                              # at most one default
      holders: [reporter, "role:Administrators"]
    - name: Partners
      holders: ["role:Triage Team", "group:partners"]
  binding: {name: VULN Issue Security Scheme, mode: managed}
```

- Holders: `role`, `group`, `user`, `reporter`, `current_assignee`,
  `application_role`. **Not** `space_lead`, `user_field` or `group_field`:
  Jira refuses them in a security level ("Type groupCustomField isn't a valid
  type"), and `validate` reports them as errors. To show work items to a
  team chosen per work item, use a role or group per team.
- `issue_security: null` means no issue security scheme; leaving it out means
  not managed.
- A scheme switch that would leave work items without their security level is
  blocked. Holders added need `--allow-update=issue_security`; holders or
  levels removed, `--allow-delete=issue_security`.

### priorities

```yaml
priorities:
  priorities: [Highest, High, Medium, Low]
  default: Medium
  binding: {name: VULN Priority Scheme, mode: managed}
```

The priorities the space offers, in order, and the default (one of them). The
priorities themselves are site-wide (section 5). `priorities` cannot be
`null`. A change that would drop a priority work items have is blocked;
priorities removed need `--allow-delete=priorities`.

### role_members

```yaml
role_members:
  Triage Team: ["group:engineering", "user:5b10ac8d82e05b22cc7d4ef5"]
```

Adds users and groups to a project role **in this space**. It only adds:
members Jira has that the spec does not list are left alone and noted, and
adding needs `--allow-update=role_members` (it widens access). The role is by
name (`Triage Team`).

### components and versions

```yaml
components:
  - {name: API, description: The public API, lead: "user:5b10ac...", assignee_type: space_default}
versions:
  - {name: "1.0", description: First release, start_date: "2026-09-01", release_date: "2026-12-01",
     released: false, archived: false}
```

- Component `assignee_type`: `space_default`, `component_lead`, `space_lead`,
  `unassigned`. `lead` is a `user:` holder.
- Version dates are `YYYY-MM-DD`; `null` clears one. Quote version names that
  look like numbers.
- Matched by `id`, else by name. A missing one is created; one that differs
  is updated; **nothing is deleted**: a component or version Jira has that the
  spec lacks is left alone and noted.

### properties

`properties` holds space entity properties (JSON values). They are exported
and kept, but **not applied**.

## 8. Filters, boards, automation rules

### filters

```yaml
filters:
  - name: VULN Open findings
    mode: managed                 # (default) or reuse
    description: What triage works through
    jql: project = VULN AND resolution = Unresolved ORDER BY Rank ASC
    viewers: [space, "group:vuln-team"]
    editors: ["role:Administrators"]
```

- `jql` is required. `viewers` and `editors` are the filter's complete lists:
  `space` (everyone who can browse the space), `role:<name>`, `group:<name>`,
  `user:<accountId>`, `logged_in` (any logged-in user), `anyone`. Editors can
  view too, so list a holder in one or the other.
- Jira lets only members of a role share with it.
- A managed filter is created when missing, and changed in place while no
  other space's board shows it. Viewers or editors added need
  `--allow-update=filters`; taken away, `--allow-delete=filters`.
- The filters listed are the ones a board of this space shows and those
  shared with the space.

### boards

```yaml
boards:
  - {name: VULN Triage board, type: kanban, filter: VULN Open findings}
```

`type` is `kanban` or `scrum`; `filter` is the name of a filter **in this
spec's `filters`**. A missing board is created in the space. An existing board
is only compared: Jira's API cannot change a board's type or filter, and
columns, swimlanes and quick filters can only be set by hand. Reading boards
needs the *Browse projects* permission on the space.

### automation_rules

Jira Automation rules. The format types only the name, mode, description,
state (`enabled` | `disabled`) and scope (`space` | `shared`); the rule
itself is Jira's JSON, written as YAML under `rule:` and passed through.
[automation.md](automation.md) is the reference, with an example of each
kind of component.

### issue_layouts

Listed as `? manual` only: Jira's API cannot read or write issue layouts.
Write `- {name: Bug layout}` to have the step mention it.

## 9. How apply decides

### The order

`apply` works through 28 steps, always in this order, skipping any with
nothing in the spec. Dependencies come first, so a status declared in the
spec exists by the time a workflow uses it.

| # | Step | # | Step |
| --- | --- | --- | --- |
| 1 | Preflight (credentials, permissions, the space exists and is company-managed) | 15 | Fields in the space |
| 2 | Role types | 16 | Workflows of the issue types (workflow scheme) |
| 3 | Groups | 17 | Permissions |
| 4 | Statuses | 18 | Notifications |
| 5 | Resolutions | 19 | Issue security |
| 6 | Priorities | 20 | Priorities in the space |
| 7 | Issue link types | 21 | Space details |
| 8 | Issue types | 22 | Components |
| 9 | Custom fields | 23 | Versions |
| 10 | Field contexts | 24 | Role members |
| 11 | Screens | 25 | Scheme switches |
| 12 | Workflows | 26 | Filters |
| 13 | Issue type scheme | 27 | Boards |
| 14 | Screens of the issue types (screen schemes) | 28 | Automation rules (and issue layouts, listed) |

Step 25 is where the space is actually switched to the new schemes; steps
13–20 create or choose them.

### Schemes: how the space gets its configuration

In Jira a space's configuration lives in **schemes** (issue type scheme,
issue type screen scheme with screen schemes, field scheme, workflow scheme,
permission scheme, notification scheme, issue security scheme, priority
scheme). You never write a scheme as such: you write what the space should
*have* (its issue types, screens, workflows, fields, grants...), and the tool
works out the schemes. For each kind, it asks:

1. **Does the space's current scheme already match the spec?** Then it is
   kept; nothing to do.
2. **Did this tool make that scheme, and does only this space use it?** Then
   it is changed in place to match.
3. **Otherwise** (a Jira default, or a scheme shared with other spaces, which
   is never edited) the tool uses another scheme: the one the spec names in a
   `binding`, or a **new one it creates**, and step 25 switches the space to
   it.

A brand-new space starts on Jira's shared default schemes, so a first apply
usually creates a full set of new managed schemes and switches the space over.
That is normal. The switch is blocked while it would strand existing work
items (a priority, security level, issue type or workflow status they have
that the new scheme lacks); the plan shows the count and what to do first.
A space with no work items has no such blocks.

### Generated names and `binding`

New schemes are named from `space.managed_prefix`:

| Scheme | Generated name |
| --- | --- |
| Issue type scheme | `<prefix> Issue Type Scheme` |
| Issue type screen scheme | `<prefix> Issue Type Screen Scheme` |
| A screen scheme (one per distinct set of screens) | `<prefix> <issue type> Screen Scheme` |
| Field scheme | `<prefix> Field Scheme` |
| Workflow scheme | `<prefix> Workflow Scheme` |
| Permission, notification, issue security, priority scheme | `<prefix> Permission Scheme`, `<prefix> Notification Scheme`, `<prefix> Issue Security Scheme`, `<prefix> Priority Scheme` |
| A field context | `<prefix> <field name>` |

Without a `managed_prefix` the tool cannot name a new scheme and says so.

A `binding` block overrides the choice. It names a scheme and how to treat it:

```yaml
permissions:
  grants: {...}          # must then match that scheme exactly, or the plan says how it differs
  binding: {name: Default software scheme, mode: reuse}   # use Jira's own, as it is
space:
  binding:                            # one block for the space's schemes
    issue_type_scheme:        {name: VULN Issue Type Scheme, mode: managed}
    issue_type_screen_scheme: {name: VULN Issue Type Screen Scheme, mode: managed}
    field_scheme:             {name: VULN Field Scheme, mode: managed}
    workflow_scheme:          {name: VULN Workflow Scheme, mode: managed}
```

A binding's keys are `name` (required), `mode` (`managed` by default, or
`reuse`), `id`, `description`, `site_default: true` (marks Jira's default
scheme of its kind), and, in a field context only, `scope`. With `mode: reuse`
the scheme must exist, the spec's definition must match it, and it is never
changed. Hand-written specs rarely need bindings; they matter when you want
to name schemes yourself or to adopt an existing one.

### What is created, changed and refused

| Object | Created if missing | Changed if it differs | Never |
| --- | --- | --- | --- |
| Site-wide objects (section 5) | `ensure` only | No: an error to fix | Edited, deleted |
| Screens, workflows | `managed` | In place, while only this space uses it | Shared ones; deleted |
| Schemes | Yes (generated or bound name) | In place, if this tool made it and only this space uses it | Jira's defaults; shared ones |
| Field contexts | This space's own | Options, defaults, issue types, in place | Shared (`scope: shared`) ones |
| Components, versions | Yes | Yes | Deleted |
| Space details | (the space exists) | Yes | `type` changed |
| Role members | Added | Added only | Removed |
| Filters, automation rules | `managed` | In place, while unshared | Shared; deleted |
| Boards | `managed` | **No** (Jira's API cannot) | Changed |

Every change is shown in the plan before it is made, with the exact
differences. Anything that widens access or removes something needs the
matching option ([cmdline.md](cmdline.md)). Nothing the tool does is hidden: the audit log of
a live run lists each write.

## 10. Limits and things to know

What the tool does not do. Where it matters, set it up in Jira by hand.

**Outside the tool's scope, by decision**

- **Dashboards.** The format has no key for them; export and apply do
  not touch them.
- **Legacy field configuration schemes** are read, and a space can be
  switched to an existing one, but they are not created or changed. Sites are
  moved to field schemes and cannot go back.
- **Who is in a group.** Groups are created (`mode: ensure`), never filled:
  membership is people, managed in Atlassian Administration. Who holds a role
  in the space is in scope (`role_members`).
- **The site's order of priorities.** A priority scheme lists its priorities in
  the site's order, and a new priority comes last. Reorder them on
  **Settings › Work items › Priorities** if it matters.
- **Creating the space.** Create it in Jira (company-managed) first, then run
  `apply`; `instructions` starts with it.

**Not possible through Jira's API**

- **Issue layouts, and a board's columns, swimlanes and quick filters.** The
  spec can name and describe them (`issue_layouts`, notes); apply lists them
  to be done by hand.
- **Changing a board's type or filter** after it is made. Give the board a new
  name in the spec to have a new one made.
- **Defaults for one issue type only**: the endpoint is in early access.
- **Work item types above Epic level, and app-provided field types**, cannot be
  created; for `apply`, declare them with `mode: reuse`. `instructions` accepts
  them with `mode: ensure` and lists them to be created by hand.
- **Security level members** cannot be `space_lead`, `user_field` or
  `group_field`: Jira refuses them, and `validate` reports them as errors.
- **Free plan**: custom permission schemes and issue security are not
  available; a spec that needs them is an error there, not skipped.

**Not in the format**

- **Default values of some field types**: a context's `default` can be set for
  select, radio, multi-select, checkboxes, cascading, text, paragraph, number,
  URL, labels, date, date-time and user fields. For group, version and
  project pickers, read-only text and app fields it cannot: export writes a
  note instead, and apply refuses such a default. Set it on the field's
  context in Jira by hand.

Behaviours that surprise people:

- **`ensure` creates, then never changes.** To change a status or custom field
  that exists, change it in Jira; the spec then reports the difference until
  it is updated to match.
- **A new field scheme comes with a few fields Jira adds** (Project, and
  others) which cannot be removed; they are not counted as differences.
- **A new permission scheme loses the guest-role grants Jira adds to it**,
  because `grants` is the complete list.
- **Notification events and permission keys are Jira's exact strings**; a
  misspelt one is an error from Jira at apply time, not from `validate`.
- **`validate` cannot see the site.** It checks syntax, references among the
  declared objects and the rules above. Whether `Triage Team` exists, or an
  account ID is real, only a dry run finds out.

## 11. A complete example

It declares what a small vulnerability-tracking space needs. It is
an example of the *format*, not a recommendation: a real one would use your
own roles, statuses and permission keys.

```yaml
apiVersion: jira-space-config/v1alpha1
kind: SpaceConfig
metadata:
  name: vulnerability-management
  description: How the VULN space is set up.

# ── Site-wide objects: mode reuse (must exist) or ensure (created if missing) ──
roles:
  - {name: Administrators, mode: reuse}
  - {name: Triage Team, mode: ensure, description: People who triage findings}
groups:
  - {name: engineering, mode: reuse}
statuses:
  - {name: Open, mode: reuse}
  - {name: In Triage, mode: ensure, category: in_progress, description: Being assessed}
  - {name: Done, mode: reuse}
resolutions:
  - {name: Fixed, mode: reuse}
  - {name: False Positive, mode: ensure}
priorities:
  - {name: Highest, mode: reuse}
  - {name: High, mode: reuse}
  - {name: Medium, mode: reuse}
  - {name: Low, mode: reuse}
issue_types:
  - {name: Vulnerability, mode: ensure, level: standard, description: A finding to assess and fix}
  - {name: Task, mode: reuse}
custom_fields:
  - {name: Severity, mode: ensure, type: select, description: How bad the finding is}
  - {name: Target Type, mode: ensure, type: cascading}
  - {name: Due by, mode: ensure, type: date}

# ── Screens and workflows: mode managed (the default) or reuse ──
screens:
  - name: VULN Finding Screen
    description: Used to create, edit and view findings
    tabs:
      - {name: Details, fields: [Summary, Description, Severity, Target Type]}
      - {name: Planning, fields: [Assignee, Priority, Due by]}
workflows:
  - name: VULN Workflow
    description: Findings are triaged before work starts
    statuses:
      - Open
      - In Triage
      - Done
    transitions:
      - {name: Create, type: initial, to: Open}
      - name: Start triage
        from: [Open]
        to: In Triage
        screen: VULN Finding Screen
        conditions:
          all:
            - {type: restrict_to, roles: [Triage Team]}
        validators:
          - {type: fields_required, fields: [Severity], error_message: Set the severity first}
      - {name: Finish, type: global, to: Done}

# ── The space itself (it must already exist; the tool never creates one) ──
space:
  key: VULN
  managed_prefix: VULN          # managed names must start with "VULN "
  details:
    description: Vulnerabilities found by the security team
    assignee_type: unassigned
  issue_types:                  # the space's issue types, in order
    - name: Vulnerability
      default: true
      workflow: VULN Workflow
      screens: VULN Finding Screen
    - name: Task
      workflow: VULN Workflow
      screens: VULN Finding Screen
  fields:                       # the complete list of fields in the space
    - name: Summary
      required: true
    - name: Description
      renderer: wiki
    - name: Assignee
    - name: Priority
    - name: Severity
      required: true
      issue_types: [Vulnerability]
    - name: Target Type
      issue_types: [Vulnerability]
    - name: Due by
      by_issue_type:
        Vulnerability: {required: true}
  field_contexts:
    Severity:
      - issue_types: [Vulnerability]
        options: [Critical, High, Medium, Low]
        default: Medium
    Target Type:
      - issue_types: [Vulnerability]
        options:
          - {value: Host, children: [Linux, Windows]}
          - {value: Application, children: [Web, Mobile]}
    Due by:
      - issue_types: all
        default: current
  permissions:
    grants:
      BROWSE_PROJECTS: ["role:Administrators", "role:Triage Team", "group:engineering"]
      CREATE_ISSUES: ["role:Triage Team"]
      EDIT_ISSUES: ["role:Triage Team"]
      ADMINISTER_PROJECTS: ["role:Administrators"]
  notifications:
    events:
      Issue Created: ["role:Triage Team", reporter]
      Issue Resolved: [reporter, current_assignee]
  issue_security: null          # no issue security scheme
  priorities:
    priorities: [Highest, High, Medium, Low]
    default: Medium
  role_members:
    Triage Team: ["group:engineering"]
  components:
    - {name: API, description: The public API}
    - {name: Web}
  versions:
    - {name: "1.0", description: First release, release_date: "2026-12-01"}

# ── Views and automation ──
filters:
  - name: VULN Open findings
    description: What triage works through
    jql: project = VULN AND resolution = Unresolved ORDER BY Rank ASC
    viewers: [space]
    editors: ["role:Administrators"]
boards:
  - {name: VULN Triage board, type: kanban, filter: VULN Open findings}
```

The smallest valid spec only names the space:

```yaml
apiVersion: jira-space-config/v1alpha1
kind: SpaceConfig
metadata: {name: minimal}
space: {key: VULN}
```

A spec that only **checks** the space and changes nothing marks everything
`reuse`:

```yaml
apiVersion: jira-space-config/v1alpha1
kind: SpaceConfig
metadata: {name: check-only}
statuses:
  - {name: Open, mode: reuse}
  - {name: Done, mode: reuse, category: done}
space:
  key: VULN
  priorities:
    priorities: [Highest, High, Medium, Low, Lowest]
    default: Medium
    binding: {name: Default priority scheme, mode: reuse, site_default: true}
```

Run `export` against a configured space for the best examples of all: it
writes every section above for the space as it really is.
