# Export coverage

What `export` does with every field Jira returns: **captured** (and where it
goes), **dropped** (and why), or a **gap**. `TestExportCoverage` matches every
field in the recordings under `internal/export/testdata/` against a row here,
and every row against a recorded field, so a new field cannot be ignored
silently. "(not recorded yet)" marks a row no recording contains yet.

Endpoints leave out `/rest/api/3` and write IDs as `{id}`, `{key}`, `{fieldId}`
or `{propertyKey}`. In field paths, `[]` is any list item and `*` any run of
characters; rows for endpoint `*` apply everywhere.

## Rules that apply everywhere

| Endpoint | Field path | Disposition |
| --- | --- | --- |
| * | `isLast`, `maxResults`, `startAt`, `total`, `nextPage` | dropped: pagination, handled by the client |
| * | `*.isLast`, `*.maxResults`, `*.startAt`, `*.total` | dropped: pagination of an embedded list |
| * | `self`, `*.self` | dropped: API link |
| * | `expand`, `*.expand` | dropped: API metadata |
| * | `*.displayName` | dropped: a user's display name; users are identified by account ID |
| * | `avatarUrls.16x16`, `avatarUrls.24x24`, `avatarUrls.32x32`, `*.avatarUrls.16x16`, `*.avatarUrls.24x24`, `*.avatarUrls.32x32` | dropped: other sizes of the 48x48 avatar |
| * | `errorMessages.[]`, `errors.*` | dropped: an error answer, handled where it is expected (see the response's own rows) |

## Space and global objects

| Endpoint | Field path | Disposition |
| --- | --- | --- |
| GET /_edge/tenant_info | `cloudId` | captured: `binding.cloud_id` |
| GET /project/{key} | `id`, `key`, `name` | captured: `space.id`, `space.key`, `space.details.name` |
| GET /project/{key} | `description` | captured: `space.details.description` (omitted when empty) |
| GET /project/{key} | `assigneeType` | captured: `space.details.assignee_type` |
| GET /project/{key} | `projectTypeKey` | captured: `space.details.type` |
| GET /project/{key} | `avatarUrls.48x48` | captured: `space.details.avatar` (the avatar ID in the URL) |
| GET /project/{key} | `lead.accountId` | captured: `space.details.lead` |
| GET /project/{key} | `lead.active` | dropped: state of the user, not configuration |
| GET /project/{key} | `style` | captured: checked; only `classic` (company-managed) is exported |
| GET /project/{key} | `simplified` | dropped: always false for company-managed spaces (`style` is checked) |
| GET /project/{key} | `isPrivate` | dropped: derived by Jira from the permission scheme, which is exported |
| GET /project/{key} | `entityId`, `uuid` | dropped: internal identifiers; `id` is the space's ID |
| GET /project/{key} | `issueTypes.[].*` | dropped: duplicates `issuetype_project`, which is exported |
| GET /project/{key} | `roles.*` | dropped: duplicates `project_role`, which is exported |
| GET /project/{key} | `components.[].*`, `versions.[].*`, `properties.*` | dropped: summaries; exported from their own endpoints |
| GET /issuetypescheme/project | `values.[].issueTypeScheme.id`, `.name`, `.description` | captured: `space.binding.issue_type_scheme` |
| GET /issuetypescheme/project | `values.[].issueTypeScheme.defaultIssueTypeId` | captured: `space.issue_types[].default` |
| GET /issuetypescheme/project | `values.[].projectIds.[]` | dropped: always this space (the request filters by it) |
| GET /issuetypescheme/mapping | `values.[].issueTypeId` | captured: `space.issue_types` (order) |
| GET /issuetypescheme/mapping | `values.[].issueTypeSchemeId` | dropped: always the scheme asked for |
| GET /issuetype/project | `[].id`, `[].description`, `[].hierarchyLevel` | captured: `issue_types[].id`, `.description`, `.level` |
| GET /issuetype/project | `[].untranslatedName` | captured: `issue_types[].name` (the stored name) |
| GET /issuetype/project | `[].name` | captured: as the stored name when equal; otherwise noted (it is translated for the exporting account) |
| GET /issuetype/project | `[].avatarId` | captured: `issue_types[].avatar` |
| GET /issuetype/project | `[].iconUrl` | captured: `issue_types[].icon` when there is no avatar ID; otherwise it is the avatar's URL |
| GET /issuetype/project | `[].subtask` | dropped: implied by `hierarchyLevel` -1 |
| GET /issuetype/project | `[].scope.*` | dropped: set only for team-managed spaces' own issue types (not recorded yet) |
| GET /project/{key}/statuses | `[].id`, `[].name`, `[].subtask` | dropped: the issue type each list belongs to; statuses are exported once each |
| GET /project/{key}/statuses | `[].statuses.[].id` | captured: `statuses[].id`, and which statuses the space uses |
| GET /project/{key}/statuses | `[].statuses.[].description` | dropped: worded differently from the statuses API for Jira's own statuses; the description comes from `GET /statuses` |
| GET /project/{key}/statuses | `[].statuses.[].untranslatedName` | captured: `statuses[].name` (the stored name) |
| GET /project/{key}/statuses | `[].statuses.[].name` | captured: as the stored name when equal; otherwise noted |
| GET /project/{key}/statuses | `[].statuses.[].statusCategory.key` | captured: `statuses[].category` |
| GET /project/{key}/statuses | `[].statuses.[].statusCategory.*` | dropped: the category's ID, name and colour follow from its key |
| GET /project/{key}/statuses | `[].statuses.[].iconUrl` | dropped: legacy status icon, not configurable in current Jira |
| GET /statuses | `[].id`, `[].description` | captured: `statuses[].description` (the statuses API is what apply compares with) |
| GET /statuses | `[].name`, `[].statusCategory` | dropped: the stored name and category come from `GET /project/{key}/statuses` |
| GET /statuses | `[].scope.type` | dropped: always `GLOBAL` for a company-managed space's statuses |
| GET /resolution/search | `values.[].id`, `.name`, `.description` | captured: `resolutions[]` |
| GET /resolution/search | `values.[].isDefault` | captured: `resolutions[].site_default` |
| GET /priorityscheme | `values.[].id`, `.name`, `.description` | captured: `space.priorities.binding` |
| GET /priorityscheme | `values.[].isDefault` | captured: `space.priorities.binding.site_default` |
| GET /priorityscheme | `values.[].defaultPriorityId` | captured: `space.priorities.default` |
| GET /priorityscheme | `values.[].prioritiesLimit` | dropped: a Jira limit, not configuration |
| GET /priorityscheme/{id}/projects | `values.[].id` | captured: whether a non-default scheme is the space's (the list cannot be filtered by space) |
| GET /priorityscheme/{id}/projects | `values.[].key`, `.name`, `.projectTypeKey`, `.simplified`, `.prioritiesLimit`, `.avatarUrls.48x48` | dropped: a summary of the space, exported from `GET /project/{key}` |
| GET /priorityscheme/{id}/priorities | `values.[].id`, `.name`, `.description` | captured: `priorities[]`, and `space.priorities.priorities` (in order) |
| GET /priorityscheme/{id}/priorities | `values.[].statusColor` | captured: `priorities[].colour` |
| GET /priorityscheme/{id}/priorities | `values.[].iconUrl` | captured: `priorities[].icon` (path on the site) |
| GET /priorityscheme/{id}/priorities | `values.[].isDefault` | captured: `priorities[].site_default` |
| GET /priorityscheme/{id}/priorities | `values.[].sequence` | captured: the order of `priorities` |

## Screens and fields

| Endpoint | Field path | Disposition |
| --- | --- | --- |
| GET /issuetypescreenscheme/project | `values.[].issueTypeScreenScheme.id`, `.name`, `.description` | captured: `space.binding.issue_type_screen_scheme` |
| GET /issuetypescreenscheme/project | `values.[].issueTypeScreenScheme.projects.*` | dropped: an empty embedded list (pagination only) |
| GET /issuetypescreenscheme/project | `values.[].projectIds.[]` | dropped: always this space |
| GET /issuetypescreenscheme/mapping | `values.[].issueTypeId`, `.screenSchemeId` | captured: each issue type's `screens` and `binding.screen_scheme` |
| GET /issuetypescreenscheme/mapping | `values.[].issueTypeScreenSchemeId` | dropped: always the scheme asked for |
| GET /screenscheme | `values.[].id`, `.name`, `.description` | captured: `space.issue_types[].binding.screen_scheme` |
| GET /screenscheme | `values.[].screens.*` | captured: `space.issue_types[].screens` (create/edit/view, falling back to default) |
| GET /screens | `values.[].id`, `.name`, `.description` | captured: `screens[]` |
| GET /screens/{id}/tabs | `[].name` | captured: `screens[].tabs[].name` |
| GET /screens/{id}/tabs | `[].id` | dropped: tab ID; tabs are identified by name within their screen |
| GET /screens/{id}/tabs/{id}/fields | `[].id` | captured: `screens[].tabs[].fields` (as the field's stored name) |
| GET /screens/{id}/tabs/{id}/fields | `[].name` | dropped: the displayed name; the stored name comes from `field` |
| GET /field | `[].id` | captured: `custom_fields[].id` for custom fields; resolves references everywhere |
| GET /field | `[].untranslatedName` | captured: field names everywhere (the stored name) |
| GET /field | `[].name` | captured: as the stored name when equal; otherwise noted |
| GET /field | `[].custom`, `[].schema.custom` | captured: whether to declare in `custom_fields`, and `custom_fields[].type` |
| GET /field | `[].key`, `[].clauseNames.[]`, `[].navigable`, `[].orderable`, `[].searchable` | dropped: derived by Jira from the field and its type |
| GET /field | `[].schema.type`, `[].schema.system`, `[].schema.items`, `[].schema.customId` | dropped: derived from the field's type and ID |
| GET /field | `[].schema.configuration.*` | dropped: per-type settings of Atlassian fields (Team), not editable |
| GET /field/search | `values.[].description` | captured: `custom_fields[].description` |
| GET /field/search | `values.[].searcherKey` | captured: `custom_fields[].searcher` |
| GET /field/search | `values.[].isLocked` | captured: `custom_fields[].locked` |
| GET /field/search | `values.[].id` | dropped: matches the field already declared |
| GET /field/search | `values.[].name`, `.translatedName`, `.translatedDescription`, `.typeDisplayName` | dropped: names come from `field`; translations follow the exporting account |
| GET /field/search | `values.[].schema.*` | dropped: as for `field` |
| GET /field/search | `values.[].areOptionsSupported`, `.isOptionsCountOverLimit` | dropped: derived from the type and option count |
| GET /config/fieldschemes/projects | `values.[].schemeId` | captured: which field scheme the space uses (and that field schemes apply) |
| GET /config/fieldschemes/projects | `values.[].projectId` | dropped: always this space |
| GET /config/fieldschemes/{id} | `id`, `name`, `description` | captured: `space.binding.field_scheme` |
| GET /config/fieldschemes/{id} | `isDefault` | captured: `space.binding.field_scheme.site_default` |
| GET /config/fieldschemes/{id} | `fieldsCount`, `links.*` | dropped: derived count and API links |
| GET /config/fieldschemes/{id}/fields | `values.[].fieldId` | captured: `space.fields[].name` |
| GET /config/fieldschemes/{id}/fields | `values.[].parameters.*` | captured: `space.fields[]` required, renderer, description |
| GET /config/fieldschemes/{id}/fields | `values.[].restrictedToWorkTypes.[]` | captured: `space.fields[].issue_types` |
| GET /config/fieldschemes/{id}/fields | `values.[].workTypeParameters.[].*` | captured: `space.fields[]` settings, or `by_issue_type` when they differ |
| GET /config/fieldschemes/{id}/fields | `values.[].allowedOperations.[]` | dropped: what Jira lets an admin do with the field (for example, Summary cannot be removed); to be used by apply's checks, not configuration |
| GET /fieldconfigurationscheme/project | `values.[].fieldConfigurationScheme.*` | captured: `space.binding.field_configuration_scheme` (legacy backend) (not recorded yet) |
| GET /fieldconfigurationscheme/project | `values.[].projectIds.[]` | dropped: always this space (not recorded yet) |
| GET /fieldconfiguration | `values.[].id`, `.name`, `.description` | captured: `space.issue_types[].binding.field_configuration` (legacy) |
| GET /fieldconfiguration | `values.[].isDefault` | captured: `binding.field_configuration.site_default` (legacy) |
| GET /fieldconfiguration/{id}/fields | `values.[].id`, `.isHidden`, `.isRequired`, `.description`, `.renderer` | captured: `space.fields[]` (legacy backend) |

## Workflows

| Endpoint | Field path | Disposition |
| --- | --- | --- |
| GET /workflowscheme/project | `values.[].workflowScheme.id`, `.name`, `.description` | captured: `space.binding.workflow_scheme` |
| GET /workflowscheme/project | `values.[].workflowScheme.defaultWorkflow`, `.issueTypeMappings.*` | captured: `space.issue_types[].workflow` |
| GET /workflowscheme/project | `values.[].projectIds.[]` | dropped: always this space |
| GET /events | `[].id`, `[].name` | captured: names the events transitions fire (`workflows[].transitions[].event`) |
| GET /group/bulk | `values.[].groupId`, `values.[].name` | captured: names groups referred to by ID in workflow rules |
| POST /workflows | `statuses.[].id`, `.name`, `.statusReference` | captured: resolves workflow status references (stored names win) |
| POST /workflows | `statuses.[].description`, `.statusCategory`, `.scope.*` | dropped: duplicates `project_statuses`, which is exported |
| POST /workflows | `workflows.[].id`, `.name`, `.description` | captured: `workflows[]` |
| POST /workflows | `workflows.[].scope.type` | captured: checked; a non-global workflow is noted |
| POST /workflows | `workflows.[].isEditable` | dropped: whether this account may edit it, not configuration |
| POST /workflows | `workflows.[].version.*` | dropped: version for optimistic locking; apply reads it fresh |
| POST /workflows | `workflows.[].startPointLayout.*`, `workflows.[].loopedTransitionContainerLayout.*` | dropped: canvas positions of the start point and the looped-transition box (design section 9.3) |
| POST /workflows | `workflows.[].statuses.[].statusReference`, `.properties.*`, `.layout.*`, `.deprecated` | captured: `workflows[].statuses[]` |
| POST /workflows | `workflows.[].transitions.[].id`, `.name`, `.type`, `.description` | captured: `workflows[].transitions[]` |
| POST /workflows | `workflows.[].transitions.[].toStatusReference`, `.links.[].fromStatusReference` | captured: `to`, `from` (and `looped`); a link with no source status is an arrow anchor only |
| POST /workflows | `workflows.[].transitions.[].links.[].fromPort`, `workflows.[].transitions.[].links.[].toPort` | dropped: which side of a status box an arrow attaches to; cosmetic (decision D35) |
| POST /workflows | `workflows.[].transitions.[].properties.*` | captured: `properties` (resolution IDs as names) |
| POST /workflows | `workflows.[].transitions.[].*.[].ruleKey`, `workflows.[].transitions.[].*.[].parameters.*` | captured: typed rules, or `type: rule` with every parameter |
| POST /workflows | `workflows.[].transitions.[].conditions.*` | captured: `conditions` (groups and rules) |
| POST /workflows | `workflows.[].transitions.[].transitionScreen.*` | captured: `screen` |
| POST /workflows | `workflows.[].transitions.[].*.[].id` | dropped: ID of one rule instance; rules are identified by their content |
| POST /workflows | `workflows.[].transitions.[].customIssueEventId` | captured: `workflows[].transitions[].event` (the event's name) |

## Field contexts

| Endpoint | Field path | Disposition |
| --- | --- | --- |
| POST /field/{fieldId}/context/mapping | `values.[].contextId`, `.issueTypeId` | captured: which context applies to which issue types (`space.field_contexts[].issue_types`) |
| POST /field/{fieldId}/context/mapping | `values.[].projectId` | dropped: always this space |
| GET /field/{fieldId}/context/defaultValues | `values.[].contextId` | dropped: matches the context asked for |
| GET /field/{fieldId}/context/defaultValues | `values.[].defaultValues.[].*` | captured: `space.field_contexts[].default`; gap: per-issue-type defaults are noted, not yet exported |
| GET /field/{fieldId}/context/{id}/option | `values.[].value`, `.disabled`, `.optionId` | captured: `space.field_contexts[].options` (children nested by `optionId`) |
| GET /field/{fieldId}/context/{id}/option | `values.[].id` | dropped: option ID; options are identified by their value within the context (defaults that refer to IDs are converted to values) |
| GET /field/{fieldId}/context | `values.[].id`, `.name`, `.description` | captured: `space.field_contexts[].binding` |
| GET /field/{fieldId}/context | `values.[].isGlobalContext` | captured: `binding.scope` (shared) |
| GET /field/{fieldId}/context | `values.[].isAnyIssueType` | dropped: implied by the issue types the context applies to in the space |
| GET /field/{fieldId}/context/projectmapping | `values.[].projectId`, `values.[].contextId` | captured: `binding.scope` (space, when the context is only for this space) |
| GET /field/{fieldId}/context/defaultValues | `errorMessages.[]` | captured: a 400 "not supported" means no default; a 403 "early access" switches to the deprecated endpoint |
| GET /field/{fieldId}/context/defaultValue | `values.[].*` | captured: `space.field_contexts[].default`, on sites where the newer endpoint is early access only |

## Access, roles and the space

| Endpoint | Field path | Disposition |
| --- | --- | --- |
| GET /project/{key}/permissionscheme | `id`, `name`, `description` | captured: `space.permissions.binding` |
| GET /project/{key}/permissionscheme | `permissions.[].permission`, `permissions.[].holder.type`, `permissions.[].holder.parameter` | captured: `space.permissions.grants` |
| GET /project/{key}/permissionscheme | `permissions.[].holder.value` | dropped: duplicates `parameter` |
| GET /project/{key}/permissionscheme | `permissions.[].holder.projectRole.*`, `permissions.[].holder.group.*`, `permissions.[].holder.field.*`, `permissions.[].holder.user.*` | dropped: expanded copies of the holder; the role and group names come from their own lookups |
| GET /project/{key}/permissionscheme | `permissions.[].id` | dropped: ID of one grant; grants are identified by permission and holder |
| GET /project/{key}/notificationscheme | `id`, `name`, `description` | captured: `space.notifications.binding` |
| GET /project/{key}/notificationscheme | `notificationSchemeEvents.[].event.name` | captured: `space.notifications.events` keys |
| GET /project/{key}/notificationscheme | `notificationSchemeEvents.[].event.id`, `.description` | dropped: events are identified by name; their descriptions are Jira's |
| GET /project/{key}/notificationscheme | `notificationSchemeEvents.[].notifications.[].notificationType`, `.parameter` | captured: `space.notifications.events` holders |
| GET /project/{key}/notificationscheme | `notificationSchemeEvents.[].notifications.[].id` | dropped: ID of one recipient entry |
| GET /project/{key}/notificationscheme | `notificationSchemeEvents.[].notifications.[].recipient` | dropped: repeats `parameter` (for groups, as the group ID) |
| GET /project/{key}/notificationscheme | `notificationSchemeEvents.[].notifications.[].group.*`, `notificationSchemeEvents.[].notifications.[].projectRole.*`, `notificationSchemeEvents.[].notifications.[].user.*`, `notificationSchemeEvents.[].notifications.[].field.*` | dropped: expanded copies of the recipient; names come from their own lookups |
| GET /project/{key}/issuesecuritylevelscheme | `errorMessages.[]` | captured: 404 means `issue_security: null` |
| GET /project/{key}/issuesecuritylevelscheme | `id`, `name`, `description` | captured: `space.issue_security.binding` |
| GET /project/{key}/issuesecuritylevelscheme | `levels.[].*`, `defaultSecurityLevelId` | dropped: summary; levels and the default come from the level endpoints |
| GET /issuesecurityschemes/level | `values.[].name`, `.description`, `.isDefault` | captured: `space.issue_security.levels[]` |
| GET /issuesecurityschemes/level | `values.[].id`, `values.[].issueSecuritySchemeId` | dropped: level and scheme IDs; levels are identified by name within the scheme |
| GET /issuesecurityschemes/level/member | `values.[].issueSecurityLevelId`, `values.[].holder.type`, `values.[].holder.parameter` | captured: `space.issue_security.levels[].holders` |
| GET /issuesecurityschemes/level/member | `values.[].holder.value`, `values.[].id`, `values.[].issueSecuritySchemeId` | dropped: repeats `parameter`; member and scheme IDs |
| GET /role | `[].id`, `[].name`, `[].description` | captured: `roles[]` (for the roles the space uses) |
| GET /role | `[].actors.[].actorUser.accountId`, `[].actors.[].actorGroup.*`, `[].actors.[].type` | captured: `roles[].default_members` |
| GET /role | `[].actors.[].id` | dropped: ID of one member entry |
| GET /role | `[].scope.*` | dropped: marks team-managed spaces' own roles, which are not exported |
| GET /project/{key}/role | `*` | captured: which roles the space has, by ID |
| GET /project/{key}/role/{id} | `actors.[].actorUser.accountId`, `actors.[].actorGroup.*`, `actors.[].type` | captured: `space.role_members` |
| GET /project/{key}/role/{id} | `id`, `name`, `description` | dropped: duplicates `role` |
| GET /project/{key}/role/{id} | `actors.[].id` | dropped: ID of one member entry |
| GET /project/{key}/role/{id} | `actors.[].name`, `actors.[].user` | dropped: legacy names of the member; `actorUser` and `actorGroup` identify it |
| GET /project/{key}/component | `values.[].id`, `.name`, `.description`, `.assigneeType`, `.lead.accountId` | captured: `space.components[]` (assignee type as configured) |
| GET /project/{key}/component | `values.[].realAssigneeType`, `values.[].isAssigneeTypeValid` | dropped: the assignee type Jira actually applies, derived from the configured one and whether the lead can be assigned |
| GET /project/{key}/component | `values.[].componentBean.*` | dropped: a nested copy of the same component |
| GET /project/{key}/component | `values.[].assignee.*`, `values.[].realAssignee.*`, `values.[].lead.accountType`, `values.[].lead.active` | dropped: the resolved assignee and user details; the lead is identified by account ID |
| GET /project/{key}/component | `values.[].project`, `values.[].projectId`, `values.[].issueCount` | dropped: always this space; a usage count |
| GET /project/{key}/version | `values.[].id`, `.name`, `.description`, `.startDate`, `.releaseDate`, `.released`, `.archived` | captured: `space.versions[]` (in Jira's order) |
| GET /project/{key}/version | `values.[].userStartDate`, `values.[].userReleaseDate`, `values.[].overdue` | dropped: the dates formatted for this account, and a derived flag |
| GET /project/{key}/version | `values.[].projectId` | dropped: always this space |
| GET /project/{key}/properties | `keys.[].key` | captured: `space.properties` keys |
| GET /project/{key}/properties/{propertyKey} | `key`, `value`, `value.*` | captured: `space.properties` |

## Boards and filters

| Endpoint | Field path | Disposition |
| --- | --- | --- |
| GET /rest/agile/1.0/board | `values.[].id`, `values.[].name`, `values.[].type` | captured: `boards[].id`, `name`, `type` |
| GET /rest/agile/1.0/board | `values.[].location.projectId` | captured: only boards located in the space are exported |
| GET /rest/agile/1.0/board | `values.[].location.*`, `values.[].isPrivate` | dropped: the location's names and avatar; whether only its owner sees it |
| GET /rest/agile/1.0/board/{id}/configuration | `filter.id` | captured: `boards[].filter` (by the filter's name) |
| GET /rest/agile/1.0/board/{id}/configuration | `id`, `name`, `type`, `location.*` | dropped: duplicates the board list |
| GET /rest/agile/1.0/board/{id}/configuration | `columnConfig.*`, `ranking.*`, `subQuery.*` | gap: columns, ranking and the Kanban sub-filter are set on the board by hand |
| GET /filter/{id} | `id`, `name`, `jql` | captured: `filters[].id`, `name`, `jql` |
| GET /filter/{id} | `sharePermissions.[].type`, `sharePermissions.[].project.id`, `sharePermissions.[].project.key` | captured: `filters[].viewers` (a share with another space is noted and left out) |
| GET /filter/{id} | `sharePermissions.[].id`, `sharePermissions.[].project.*` | dropped: the share's own ID, and the space's other details |
| GET /filter/{id} | `owner.*` | dropped: the owner is the account that made it; apply makes filters as the account it runs as |
| GET /filter/{id} | `favourite`, `favouritedCount`, `approximateLastUsed`, `isWritable`, `searchUrl`, `viewUrl`, `sharedUsers.*`, `subscriptions.*` | dropped: about the account reading it, use, and links |
| GET /filter/search | `values.[].id`, `values.[].name`, `values.[].jql` | captured: `filters[].id`, `name`, `jql` |
| GET /filter/search | `values.[].sharePermissions.[].type`, `values.[].sharePermissions.[].project.id`, `values.[].sharePermissions.[].project.key` | captured: `filters[].viewers` |
| GET /filter/search | `values.[].sharePermissions.[].id`, `values.[].sharePermissions.[].project.*` | dropped: the share's own ID, and the space's other details |

## Automation rules

| Endpoint | Field path | Disposition |
| --- | --- | --- |
| POST /gateway/api/automation/public/jira/{id}/rest/v1/rule/summary | `links.*` | dropped: pagination, followed by the reader (the cursor in `next`) |

## Not used by export

| Endpoint | Field path | Disposition |
| --- | --- | --- |
| GET /workflows/capabilities | `*` | dropped: used only by the test that checks the rule registry against the site |
