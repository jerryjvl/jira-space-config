# API inventory

Every Atlassian endpoint the tool depends on, grouped by apply step. Checked
against the vendored OpenAPI specs in [openapi/](openapi/) (see
[README.md](README.md) for versions). Paths are relative to
`https://<site>.atlassian.net` unless stated.

## Stability policy

| Label | Meaning in the spec | How we treat it |
| --- | --- | --- |
| **GA** | No flags | Use freely |
| **Exp** | `x-experimental: true` | Available on all sites, but Atlassian may change the contract. Usable; the tool must tolerate missing or extra fields, and tests pin the shapes we rely on. Atlassian applies this flag to many long-established endpoints (for example `POST /screens`) |
| **Depr** | `deprecated: true` | Usable while no generally available replacement exists. Each use is listed under "Deprecation watch" below |
| **Beta** | Opt-in programme (only some sites have it) | **Never the only path.** May be detected and reported; any support must sit alongside a GA path |

The tool never uses endpoints that create, delete, archive or restore spaces
(`POST/DELETE /rest/api/3/project…`, `/project-template`, `…/archive`,
`…/delete`, `…/restore`), even though they exist.

## Read-only POST allow-list

The read-only client sends `GET` only, except for these documented read
operations that use `POST`:

| Endpoint | Purpose |
| --- | --- |
| `POST /rest/api/3/workflows` | Bulk get workflows |
| `POST /rest/api/3/workflows/create/validation` | Validate a workflow create payload (no write) |
| `POST /rest/api/3/workflows/update/validation` | Validate a workflow update payload (no write) |
| `POST /rest/api/3/workflowscheme/read` | Bulk get workflow schemes |
| `POST /rest/api/3/workflowscheme/update/mappings` | Compute the status mappings a scheme change would need |
| `POST /rest/api/3/field/{fieldId}/context/mapping` | Contexts for project/issue type pairs |
| `POST /rest/api/3/permissions/check` | Bulk permission check (preflight) |
| `POST /rest/api/3/search/approximate-count` | Count work items in the space (JQL `project = <id>`) |
| `POST /automation/public/jira/{cloudId}/rest/v1/rule/summary` | Search automation rule summaries (by scope); on the site under `/gateway/api` |
| `POST /rest/api/3/priorityscheme/mappings` | Suggested priority mappings |

## 1. Preflight and identity

| Purpose | Endpoint | Status | Notes |
| --- | --- | --- | --- |
| Current user | `GET /rest/api/3/myself` | GA | |
| Site identity | `GET /_edge/tenant_info` | Not in spec | Returns `cloudId`; widely used but undocumented. Always requested from the site itself. Needed for `--api=gateway`: scoped API tokens only work via `https://api.atlassian.com/ex/jira/{cloudId}` ([Atlassian](https://support.atlassian.com/atlassian-account/docs/manage-api-tokens-for-your-atlassian-account/)) |
| Permissions | `GET /rest/api/3/mypermissions?projectKey=…&permissions=ADMINISTER,ADMINISTER_PROJECTS,BROWSE_PROJECTS` | GA | Most steps need the global *Administer Jira* permission |
| Plan level | `GET /rest/api/3/instance/license` | Exp | Detect Free plan (no permission schemes, issue security) |
| Target space | `GET /rest/api/3/project/{key}` | GA | Must exist and have `style: classic` (company-managed) |
| Work items in space | `POST /rest/api/3/search/approximate-count` | GA | Old `/rest/api/3/search` is being removed: never use |
| All spaces (usage analysis) | `GET /rest/api/3/project/search?status=live,archived,deleted&action=view` | GA (`status` is Exp) | Admins see all spaces with `action=view` |

## 2. Global objects

| Step | Read | Create | Status | Notes |
| --- | --- | --- | --- | --- |
| Role types | `GET /rest/api/3/role` | `POST /rest/api/3/role` | GA | |
| Groups | `GET /rest/api/3/group/bulk` | `POST /rest/api/3/group` | Exp / GA | The unpaginated group lookup is deprecated: not used. Groups are referenced by name, sent by `groupId` |
| Statuses | `GET /rest/api/3/statuses/search`, `GET /rest/api/3/statuses` (by ID) | `POST /rest/api/3/statuses` (scope `GLOBAL`) | GA | Filter to global scope; team-managed spaces have same-named project-scoped statuses. |
| Resolutions | `GET /rest/api/3/resolution/search` | `POST /rest/api/3/resolution` | Exp | The unpaginated list is deprecated: not used |
| Priorities | `GET /rest/api/3/priority/search` | `POST /rest/api/3/priority` | GA | The unpaginated list is deprecated: not used |
| Issue link types | `GET /rest/api/3/issueLinkType` | `POST /rest/api/3/issueLinkType` | GA | |
| Issue types | `GET /rest/api/3/issuetype`, `GET /rest/api/3/issuetype/project?projectId=` | `POST /rest/api/3/issuetype` | GA | Create supports `hierarchyLevel` `0` (standard) and `-1` (sub-task) only; other levels are reuse-only |
| Custom fields | `GET /rest/api/3/field/search` (paginated, `type=custom`) | `POST /rest/api/3/field` | GA | Needs `type` and `searcherKey` keys (see "Custom field types"). Names are not unique |

## 3. Custom field contexts and options

| Purpose | Endpoint | Status |
| --- | --- | --- |
| List contexts | `GET /rest/api/3/field/{fieldId}/context` | GA |
| Context scope | `GET /rest/api/3/field/{fieldId}/context/projectmapping` | GA |
| Create context (with `projectIds`, `issueTypeIds`) | `POST /rest/api/3/field/{fieldId}/context` | GA |
| Options | `GET` / `POST /rest/api/3/field/{fieldId}/context/{contextId}/option` | GA |
| Option order | `PUT /rest/api/3/field/{fieldId}/context/{contextId}/option/move` | GA |
| Defaults | `GET` / `PUT /rest/api/3/field/{fieldId}/context/defaultValues` | GA (the singular `defaultValue` is deprecated) |

Cascading options: child options are created with `optionId` of the parent.

## 4. Screens

| Purpose | Endpoint | Status |
| --- | --- | --- |
| Screens | `GET` / `POST /rest/api/3/screens` | GA / Exp |
| Tabs | `GET` / `POST /rest/api/3/screens/{screenId}/tabs` | GA / Exp |
| Tab fields | `GET` / `POST /rest/api/3/screens/{screenId}/tabs/{tabId}/fields`, `POST /rest/api/3/screens/{screenId}/tabs/{tabId}/fields/{id}/move` | GA |
| Screen schemes | `GET` / `POST /rest/api/3/screenscheme` | GA |
| Issue type screen schemes | `GET` / `POST /rest/api/3/issuetypescreenscheme`, `GET /rest/api/3/issuetypescreenscheme/mapping` | GA |
| ITSS usage | `GET /rest/api/3/issuetypescreenscheme/{id}/project` | GA |
| ITSS association | `GET` / `PUT /rest/api/3/issuetypescreenscheme/project` | GA |

Screen usage comes from screen schemes **and** workflow transitions
(`transitionScreen` rule, see section 7). There is no direct "which schemes use
this screen" endpoint, so usage is computed from a full read of screen schemes
and workflows.

## 5. Fields per space

Field schemes (Exp), which replace field configurations: which fields a space
has, optionally per issue type, with their settings.

| Purpose | Endpoint | Status |
| --- | --- | --- |
| Schemes | `GET` / `POST /rest/api/3/config/fieldschemes`, `GET /rest/api/3/config/fieldschemes/{id}` | Exp |
| Fields | `GET /rest/api/3/config/fieldschemes/{id}/fields`, `PUT` / `DELETE /rest/api/3/config/fieldschemes/fields` | Exp |
| Field settings | `PUT` / `DELETE /rest/api/3/config/fieldschemes/fields/parameters` | Exp |
| Usage | `GET /rest/api/3/config/fieldschemes/{id}/projects` | Exp |
| Association | `GET` / `PUT /rest/api/3/config/fieldschemes/projects` | Exp |

Legacy field configurations (deprecated) are read, and a space can be
switched to an existing scheme, but they are not written: sites are moved to
field schemes and cannot go back.

| Purpose | Endpoint | Status |
| --- | --- | --- |
| Field configurations | `GET /rest/api/3/fieldconfiguration` | Depr |
| Items (hidden, required, description, renderer) | `GET /rest/api/3/fieldconfiguration/{id}/fields` | Depr |
| Schemes | `GET /rest/api/3/fieldconfigurationscheme` | Depr |
| Scheme mapping | `GET /rest/api/3/fieldconfigurationscheme/mapping` | Depr |
| Association | `GET` / `PUT /rest/api/3/fieldconfigurationscheme/project` | Depr |

## 6. Issue type scheme

| Purpose | Endpoint | Status |
| --- | --- | --- |
| Schemes | `GET` / `POST /rest/api/3/issuetypescheme`, `GET /rest/api/3/issuetypescheme/mapping` | GA |
| Association (read/write) | `GET` / `PUT /rest/api/3/issuetypescheme/project` | GA |

## 7. Workflows

New bulk workflow API. The old `POST /rest/api/3/workflow` no longer exists in
the spec; `GET /rest/api/3/workflow/search` is deprecated.

| Purpose | Endpoint | Status |
| --- | --- | --- |
| Search | `GET /rest/api/3/workflows/search` | GA |
| Bulk read | `POST /rest/api/3/workflows` | GA (read-only POST) |
| Rule catalogue for the site | `GET /rest/api/3/workflows/capabilities` | GA |
| Validate | `POST /rest/api/3/workflows/create/validation` | GA (read-only POST) |
| Create | `POST /rest/api/3/workflows/create` (scope `GLOBAL`, max 20 per call) | GA |
| Usage | `GET /rest/api/3/workflow/{id}/projectUsages` | GA |

Model (from `WorkflowCreate` / `TransitionUpdateDTO`):

- Statuses are listed with a `statusReference` and `layout {x, y}` plus
  per-status `properties`.
- Transitions: `type` `INITIAL | GLOBAL | DIRECTED`; `links[]` of
  `fromStatusReference` (multiple sources per transition); `toStatusReference`;
  `transitionScreen` is a rule; `conditions` is a group tree (`operation:
  ALL | ANY`, nested `conditionGroups`); `validators`, `actions` (post
  functions) and `triggers` are rule lists `{ruleKey, parameters}`;
  `properties` is a string map.

Documented Atlassian rule keys (`workflows/capabilities` description) that the
spec's typed rules map to:

| Spec rule | Rule key | ID-bearing parameters (resolved from names) |
| --- | --- | --- |
| `restrict_to` | `system:restrict-issue-transition` | `roleIds`, `groupIds`, `permissionKeys`, `groupCustomFields`, `allowUserCustomFields`, `denyUserCustomFields` (comma-separated) |
| `fields_required` | `system:validate-field-value` (`ruleType: fieldRequired`) | `fieldsRequired` |
| `field_changed`, `field_matches`, `date_compare` | `system:validate-field-value` (other `ruleType`s) | `fieldKey`, `date1FieldKey`, `date2FieldKey`, `groupsExemptFromValidation` |
| `permission` | `system:check-permission-validator` | none |
| `previous_status` | `system:previous-status-condition` / `-validator` | `previousStatusIds` |
| `field_value` | `system:check-field-value` | `fieldId`, option IDs in `fieldValue` |
| `separation_of_duties` | `system:separation-of-duties` | `fromStatusId`, `toStatusId` |
| `block_all` | `system:restrict-from-all-users` | none |
| `set_field` | `system:update-field` | `field` (and option IDs in `value`) |
| `copy_field` | `system:copy-value-from-other-field` | `sourceFieldKey`, `targetFieldKey` |
| `assign` | `system:change-assignee` | `accountId` |
| transition `screen:` | `system:transition-screen` | `screenId` |

`clear_field` is `system:update-field` with an empty `value` and `mode`.
Any other key (`connect:*`, `forge:*`, undocumented `system:*`) round-trips
as an opaque `type: rule`.

Transition property `jira.field.resolution.include` / `…exclude` holds
comma-separated resolution IDs; the spec writes names.

## 8. Workflow scheme

| Purpose | Endpoint | Status |
| --- | --- | --- |
| Read | `GET /rest/api/3/workflowscheme`, `POST /rest/api/3/workflowscheme/read` | GA |
| Create | `POST /rest/api/3/workflowscheme` | GA |
| Usage | `GET /rest/api/3/workflowscheme/{id}/projectUsages` | GA |
| Association read | `GET /rest/api/3/workflowscheme/project?projectId=` | GA |
| Associate (space has **no** work items) | `PUT /rest/api/3/workflowscheme/project` | GA: "performed only when there are no issues in the project" |
| Change | `POST /rest/api/3/workflowscheme/update`; the mappings it needs from `POST /rest/api/3/workflowscheme/update/mappings` | Exp; may run as a task |

## 9. Permission scheme

| Purpose | Endpoint | Status |
| --- | --- | --- |
| Read | `GET /rest/api/3/permissionscheme?expand=all`, `GET /rest/api/3/permissionscheme/{schemeId}/permission` | GA |
| Permission keys | `GET /rest/api/3/permissions` | GA |
| Create (with grants) | `POST /rest/api/3/permissionscheme` | GA |
| Association | `GET` / `PUT /rest/api/3/project/{key}/permissionscheme` | GA |

There is **no scheme-side usage lookup**. Usage is computed by reading
`GET /rest/api/3/project/{key}/permissionscheme` for every space (live,
archived and deleted). If any space can't be read, usage is treated as unknown.
Not available on the Free plan.

## 10. Notification scheme

| Purpose | Endpoint | Status |
| --- | --- | --- |
| Read | `GET /rest/api/3/notificationscheme?expand=all`, `GET /rest/api/3/notificationscheme/{id}` | GA |
| Usage | `GET /rest/api/3/notificationscheme/project` | GA |
| Events | `GET /rest/api/3/events` | Exp. Custom events cannot be created via the API |
| Create | `POST /rest/api/3/notificationscheme` | Exp |
| Association | `PUT /rest/api/3/project/{key}` (`notificationScheme` field); read via `GET /rest/api/3/project/{key}/notificationscheme` | GA |

Notification types are strings such as `CurrentAssignee`, `Reporter`,
`Group`, `ProjectRole`, `EmailAddress`, `UserCustomField`,
`GroupCustomField`, `AllWatchers`. Event names are matched to what `/events`
returns.

## 11. Issue security scheme

| Purpose | Endpoint | Status |
| --- | --- | --- |
| Read | `GET /rest/api/3/issuesecurityschemes`, `GET /rest/api/3/issuesecurityschemes/level`, `GET /rest/api/3/issuesecurityschemes/level/member` | Exp |
| Usage | `GET /rest/api/3/issuesecurityschemes/project` | Exp |
| Create (with levels and members) | `POST /rest/api/3/issuesecurityschemes` | Exp |
| Association | `PUT /rest/api/3/issuesecurityschemes/project` | Exp, async, remaps levels |

Not available on the Free plan.

## 12. Priority scheme

| Purpose | Endpoint | Status |
| --- | --- | --- |
| Read | `GET /rest/api/3/priorityscheme`, `GET /rest/api/3/priorityscheme/{schemeId}/priorities` | Exp |
| Usage | `GET /rest/api/3/priorityscheme/{id}/projects` | Exp |
| Create | `POST /rest/api/3/priorityscheme` | Exp |
| Association | `PUT /rest/api/3/priorityscheme/{id}` (`projects` add), may need `mappings` | Exp |

## 13. The space itself

| Purpose | Endpoint | Status |
| --- | --- | --- |
| Details | `GET` / `PUT /rest/api/3/project/{key}` | GA. Update body: `name`, `description`, `leadAccountId`, `assigneeType`, `categoryId`, `url`, `permissionScheme`, `notificationScheme`, `issueSecurityScheme`. **`key` is never sent** |
| Categories | `GET /rest/api/3/projectCategory` | GA. Reuse only |
| Components | `GET /rest/api/3/project/{key}/component`, `POST /rest/api/3/component` | GA |
| Versions | `GET /rest/api/3/project/{key}/version`, `POST /rest/api/3/version` | GA |
| Role members | `GET /rest/api/3/project/{key}/role/{id}`, `POST /rest/api/3/project/{key}/role/{id}` (add actors) | GA. `PUT` (replace) is never used |
| Properties | `GET` / `PUT /rest/api/3/project/{key}/properties/{propertyKey}` | GA |

## 14. Filters and boards

| Purpose | Endpoint | Status |
| --- | --- | --- |
| Filters, with their viewers and editors | `GET /rest/api/3/filter/search`, `GET /rest/api/3/filter/{id}`, `POST /rest/api/3/filter`, `PUT /rest/api/3/filter/{id}` | GA |
| Boards (Agile API) | `GET /rest/agile/1.0/board?projectKeyOrId=`, `POST /rest/agile/1.0/board` | GA |
| Board configuration | `GET /rest/agile/1.0/board/{id}/configuration` | GA. **Read-only**: columns, estimation and ranking can't be written: MANUAL |

## 16. Automation rules

A separate API, at `https://<site>/gateway/api/automation/public/jira/{cloudId}`
(or `https://api.atlassian.com/automation/public/jira/{cloudId}`); it accepts API token
authentication. Paths are written from `/automation`.

| Purpose | Endpoint |
| --- | --- |
| The rules of a scope | `POST /automation/public/jira/{cloudId}/rest/v1/rule/summary` (a read) |
| Read one | `GET /automation/public/jira/{cloudId}/rest/v1/rule/{ruleUuid}` |
| Create | `POST /automation/public/jira/{cloudId}/rest/v1/rule` |
| Change | `PUT /automation/public/jira/{cloudId}/rest/v1/rule/{ruleUuid}` |
| Enable or disable | `PUT /automation/public/jira/{cloudId}/rest/v1/rule/{ruleUuid}/state` |

A rule's trigger and components are Jira's own JSON, passed through as
opaque ([automation.md](../automation.md)); they hold site IDs, so a rule is bound to its
site.

## 17. Not available through the API

- Issue layouts (issue view field arrangement).
- Board column, swimlane, card layout and quick filter configuration.
- Custom notification events.
- Issue types above standard level (epic or custom hierarchy levels).
- App-provided custom field types (can be reused, not created).

## Custom field types

The `POST /rest/api/3/field` description lists these
`com.atlassian.jira.plugin.system.customfieldtypes:` keys: `cascadingselect`,
`datepicker`, `datetime`, `float`, `grouppicker`, `importid`, `labels`,
`multicheckboxes`, `multigrouppicker`, `multiversion`, `project`,
`radiobuttons`, `readonlyfield`, `select`, `textarea`, `textfield`, `url`,
`userpicker`, `version`, plus `multiselect` and `multiuserpicker`.

## Cross-cutting concerns

- **Pagination:** most lists use `startAt` / `maxResults` / `isLast`; the
  space search uses `nextPageToken`.
- **Tasks:** some changes answer 303 and finish in a task; the tool reads
  the object again until the task is done.
- **Rate limiting:** 429 and 503 honour `Retry-After`. Reads are retried;
  writes only on 429, which Jira answers before doing anything.
- **Errors:** 401 means credentials; 403 means a permission is missing
  (preflight names it); 404 on an admin endpoint can also mean a missing
  permission.

## Deprecation watch

| Endpoint | Replacement | Status |
| --- | --- | --- |
| `/rest/api/3/fieldconfiguration*`, `/fieldconfigurationscheme*` | Field schemes | Used: read, for sites not yet moved |
| `GET /rest/api/3/resolution`, `GET /rest/api/3/priority`, `GET /rest/api/3/group`, `GET /rest/api/3/project`, `GET /rest/api/3/workflow/search` | `…/search`, `…/bulk`, `/project/search`, `/workflows/search` | Not used |
| `/rest/api/3/search` | `/search/jql`, `/search/approximate-count` | Not used |
| `…/context/defaultValue` | `…/context/defaultValues` | Not used |

## Jira behaviours the code depends on

Not in Atlassian's documentation, or contrary to it. Each was seen on a real
site and is pinned by a recording or a test.

**Reading**

- `GET /field` leaves out custom fields not on a legacy field configuration
  or screen; custom fields come from `GET /field/search?type=custom` (at most
  50 a page, whatever `maxResults` says). A field scheme may hold a field no
  field API lists (`dataclassification`): it is written as `id:<key>`.
- Unknown query parameters are silently ignored (`GET /priorityscheme` has no
  space filter): every recorded request's parameters are checked against the
  OpenAPI spec.
- Names are translated for the reading account; the stored name is used.
  The two status APIs word Jira's own statuses' descriptions differently;
  descriptions come from the statuses API (`GET /statuses`), which apply
  compares with.
- Status categories come back as `new`, `indeterminate`, `done`.
- Role type names are not unique (`atlassian-addons-project-access` four
  times on a site). Roles are followed by the IDs in
  `GET /project/{key}/role`; a grant to the wrong one is refused ("Please
  select a valid project role").
- Holders: a group carries its name in `parameter` and its ID in `value`;
  security level members spell `projectrole` in lower case.
- 404 means "none" for `…/issuesecuritylevelscheme` and
  `…/notificationscheme` of a space (a space made through the API has no
  notification scheme).
- Boards need Browse projects: without it `GET /agile/1.0/board` answers 400.
  A filter shared with a space the account cannot see has share type
  `project-unknown`.
- Field scheme IDs are numbers in the list and strings when one is read.

**Statuses, priorities, issue types**

- A workflow create updates the statuses it lists: one sent without its
  description has it cleared site-wide. Statuses are sent as Jira holds them.
- Creating a priority needs `iconUrl` or `avatarId` (400 otherwise), though
  the spec marks neither required. Jira gives a priority named Blocker its own
  description. There is no site default priority: `isDefault` in a priority
  scheme's list is that scheme's default.
- A priority scheme keeps its priorities in the site's order. Switching a
  space to one needs `mappings.in` for every priority it loses, even with no
  work items.
- Groups have no description.

**Fields and contexts**

- A field may have only one context specific to a space ("These projects are
  already associated with a context"); a global context cannot be limited to
  issue types.
- Adding issue types to an all-types context restricts it; taking them all
  away makes it cover all. A context for every issue type the space has reads
  back as one for all issue types.
- A context made for an issue type not yet in the space is found only by
  asking about that issue type.
- `GET …/context/defaultValues` answers 400 "not supported" for app and
  special fields: read as no default. It gives "the current date" as a fixed
  date-time; the deprecated `GET …/defaultValue` says `useCurrent: true`, so
  date defaults are read from it. `PUT …/defaultValues` is early access
  (403); `PUT …/defaultValue` works. A default is taken away by writing its
  type with no value. A user default needs `userFilter`.
- `GET …/option` answers 400 for a field without options.
- A new field scheme gets fields of its own (Project and others); those with
  `REMOVE` in `allowedOperations` are removed, the rest are fixed. A field
  scheme makes Reporter required, and shows a field's own description,
  which a new scheme does not take and a locked field cannot set. A field
  sent without `restrictedToWorkTypes` loses its restriction.
  `…/fields/parameters` refuses empty `parameters`.

**Screens and schemes**

- A new screen gets one tab, "General". Template screen schemes set only
  `default`.
- Screens have no reverse lookup: usage is read through every screen scheme
  and every active workflow's transitions.
- An issue type is mapped once in an issue type screen scheme (400 "can only
  be mapped once"): its mapping is removed first.
- `GET /issuetypescheme?expand=projects` lists a scheme's spaces (the
  expansion is not in the spec).
- `PUT /issuesecurityschemes/project` cannot attach a scheme to a space with
  no secured work items; `PUT /project/{key}` with `issueSecurityScheme` can.
  A scheme is taken off with `-1`; `null` is answered 200 and ignored, so
  nulls are never sent.
- A workflow scheme update needs `statusMappingsByIssueTypeOverride` for each
  issue type moving to a workflow without some of its statuses, even with no
  work items; it answers 303 to a task. Deleting a security level is a task
  too.

**Workflows**

- Transition IDs are required on create, though the spec does not say so.
  New ones are numbered as Jira's classic workflows are: 1, 11, 21, …
- A transition with no `toStatusReference` is answered 500 on create
  (validation accepts it). A loop is written as one transition per `from`
  status, back to itself. A self-loop made in Jira's editor is an ordinary
  directed transition; a transition read with no target is taken as looped
  (Jira's own "any status to itself" form is unconfirmed).
- An initial transition's `links` holds an anchor with no source status.
- An update keeps transitions by ID, and must still list a dropped status,
  with a `statusMappings` migration for each issue type of each space using
  the workflow, even with no work items in it. In Jira's messages,
  "expected" is what was sent.
- Jira fills in rule defaults (`ignoreContext: false`), so false flags are
  never written. `set_field` reads back with `mode: replace`. Clearing a
  field is `system:update-field` with empty `value` and `mode`.
- `workflows/capabilities` lists rules the documentation does not
  (`system:set-security-level-from-role`, `system:development-triggers`);
  they pass through as opaque rules.
- Templates use a simplified workflow ("managed internally by Jira") that
  the board's columns drive.

**Permissions and security**

- A new permission scheme gets grants Jira adds (the guest role on many
  permissions), some after the `POST` has answered: the scheme is read again
  until nothing new appears, and the extras are deleted.
- A permission grant for one user needs the account ID in `parameter`.
- Security level members cannot be `projectLead`, `groupCustomField` or
  `userCustomField` ("isn't a valid type").
- Notification schemes refuse `EmailAddress` recipients and take group names,
  not IDs.
- A space whose permission scheme grants Browse to nobody can be browsed by
  no one, its creator included; filters on it are then refused.

**Filters and boards**

- A role share is type `projectRole`; written as `project` with a role, Jira
  shares with the whole space without a word. Only a role's members may share
  with it.
- An edit share implies view, and Jira keeps no separate view share.
- `GET /agile/1.0/board?filterId=` lists a filter's boards; a board in a
  user's location has no `projectId`.

**Automation**

- A rule's connection ID is made by the caller and sent with the rule on
  create; on update, one the rule has is not listed again ("already
  exists"). Jira keeps connections a change stops using.
- An actor of type `SMART_VALUE` is accepted but read back without its
  expression.

**The space**

- `PUT /project/{key}` takes `categoryId: -1` to clear the category.
