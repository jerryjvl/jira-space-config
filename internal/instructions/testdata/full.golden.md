# Setting up Demo space (DEMO) by hand

Every section of the format, for tests.

Written from `full.yaml` by jira-space-config test. Work through the sections in order: each needs only the ones before it. Only what has to be added or changed is listed; Jira's own objects (standard statuses, system fields, the default schemes) are used as they are. "Create if missing" means: if one of that name is already there, check it has these values instead. Menu paths were checked against Atlassian's documentation in October 2026 and may have moved since.

You need the Administer Jira global permission, and Atlassian organisation admin for groups.

## 1. Space

**Spaces › Create space**, then a company-managed template of the type below.

| Setting | Value |
| --- | --- |
| Key | DEMO |
| Name | Demo space |
| Type | Software |
| Description | A space for tests |
| Lead | User 5b10a2844c20165700ede21g |
| Default assignee | Unassigned |
| Avatar | 10417 |

If the space already exists, set these in **Space settings › Details** instead.

## 2. Project roles

**Settings › System › Security › Project roles**: fill in the form at the bottom of the page.

|  | Role | Description | Default members |
| --- | --- | --- | --- |
| Create if missing | Triage Team | Assesses new items | Group team-alpha |

## 3. Groups

Nothing to create. Already on the site, used as they are: team-alpha.

## 4. Statuses

**Settings › Work items › Statuses › Add status**.

|  | Status | Category | Description |
| --- | --- | --- | --- |
| Create if missing | In Triage | In progress | Being assessed |

Already on the site, used as they are: Open, Closed.

## 5. Resolutions

**Settings › Work items › Resolutions**: fill in the form below the list.

|  | Resolution | Description |
| --- | --- | --- |
| Create if missing | Fixed | The problem was fixed |
| Create if missing | False Positive |  |

Already on the site, used as they are: Duplicate.

## 6. Priorities

Nothing to create. Already on the site, used as they are: High, Medium.

## 7. Work item link types

Nothing to create. Already on the site, used as they are: Duplicate.

## 8. Work item types

**Settings › Work items › Work item types › Add work item type**.

|  | Work item type | Level | Description |
| --- | --- | --- | --- |
| Create if missing | Finding | Standard | Something to assess |

Already on the site, used as they are: Epic.

## 9. Custom fields

**Settings › Work items › Fields › Create new field**.

|  | Field | Type | Description |
| --- | --- | --- | --- |
| Create if missing | Severity | Select List (single choice) | How bad it is |
| Create if missing | Target Type | Select List (cascading) |  |
| Create if missing | Source IDs | Labels |  |

Already on the site, used as they are: Sprint.

Options and work types are set per context in the next section, not here. A new field is not added to any field scheme by itself.

## 10. Field contexts

**Settings › Work items › Fields**, then the field's **More actions › Contexts and default value**, then **Add new context**.

Each context applies to space DEMO only, for the work types listed.

| Field | Context | Work types | Options, in order | Default |
| --- | --- | --- | --- | --- |
| Severity | DEMO Severity | All | 5 – Critical, 4 – High, 3 – Medium, Legacy (disabled) | 3 – Medium |

Already on the site, used as they are: Default Configuration Scheme for Target Type (Target Type).

## 11. Screens

**Settings › Work items › Screens › Add screen**; add each tab and its fields in the order given.

| Screen | Description | Tab | Fields, in order |
| --- | --- | --- | --- |
| DEMO View |  | Field Tab | Summary, Description, Severity |
| DEMO Create | Shown when creating | Field Tab | Summary, Severity |
|  |  | Details | Target Type, Source IDs |
| DEMO Triage |  | Field Tab | Severity |

Already on the site, used as they are: Default Screen.

## 12. Workflows

**Settings › Work items › Workflows › Create workflow**; add the statuses, then each transition with its rules.

### DEMO Workflow

Description: Triage flow

Statuses: Open, In Triage, Closed.

| Transition | From | To | Screen | Rules |
| --- | --- | --- | --- | --- |
| Create | (create) | Open |  |  |
| Start Triage | Open | In Triage | DEMO Triage | **Only if:** (Restrict to role Triage Team or group team-alpha) and (Previously in Open or Nobody (users))<br>**Validate:** Required: Severity, Target Type, message "Assess first"; App rule `forge:expression-validator` (key=ari:cloud:ecosystem::extension/x, config={})<br>**Then:** Set Resolution to Fixed; Assign to current user (also unknownParam=1)<br>Fires event: Issue Assigned<br>Properties: `jira.field.resolution.include` = False Positive, Duplicate; `opsbar-sequence` = 10 |
| Reassess | In Triage | (back to the same status) |  |  |
| Close | Any status | Closed |  |  |

## 13. Work item type scheme

**Settings › Work items › Work item type schemes › Add work item type scheme**.

Create the work item type scheme **DEMO Issue Type Scheme**:

| Work types, in order |
| --- |
| Finding |
| Epic |

Default work type: Finding.

## 14. Screens of the work types

**Settings › Work items › Screen schemes › Add screen scheme**, then **Work item type screen schemes › Add work item type screen scheme**.

Create the screen schemes:

| Screen scheme | Create | Edit | View |
| --- | --- | --- | --- |
| DEMO Screen Scheme | DEMO Create | DEMO View | DEMO View |
| DEMO Epic Screen Scheme | Default Screen | Default Screen | Default Screen |

Create the work item type screen scheme **DEMO Issue Type Screen Scheme**:

| Work type | Screen scheme |
| --- | --- |
| Default | DEMO Screen Scheme |
| Epic | DEMO Epic Screen Scheme |

## 15. Fields in the space

**Settings › Work items › Fields › Field configurations › Add field configuration**, then **Field configuration schemes**.

Create the field configuration scheme **DEMO Field Configuration Scheme**:

Its field configuration shows these fields and hides every other.

| Field | Only for work types | Required | Renderer | Help text in this space |
| --- | --- | --- | --- | --- |
| Summary |  | Yes |  |  |
| Description |  |  | wiki | What was found |
| Severity |  | No |  |  |
| ↳ for Finding |  | Yes |  |  |
| Target Type | Finding |  |  |  |
| Source IDs |  |  |  |  |

## 16. Workflows of the work types

**Settings › Work items › Workflow schemes › Add workflow scheme**.

Create the workflow scheme **DEMO Workflow Scheme**:

| Work type | Workflow |
| --- | --- |
| Finding | DEMO Workflow |

## 17. Permissions

**Settings › Work items › Permission schemes › Add permission scheme**, then **Permissions** to grant.

Create the permission scheme **DEMO Permission Scheme**:

Grant exactly these, and revoke any grant Jira adds by itself (it adds the guest role to many):

| Permission | Granted to |
| --- | --- |
| Schedule issues `SCHEDULE_ISSUES` | Role atlassian-addons-project-access |
| Browse projects `BROWSE_PROJECTS` | Role Triage Team, Group team-alpha, Role Administrators |

## 18. Notifications

**Settings › Work items › Notification schemes › Add notification scheme**.

Use the existing notification scheme **Default Notification Scheme**; it is not changed.

## 19. Work item security

**Space settings › Work item security**.

The space uses no work item security scheme: remove the one set there, if any.

## 20. Priorities in the space

**Settings › Work items › Priority schemes › Add priority scheme**.

Create the priority scheme **DEMO Priority Scheme**:

Priorities: High, Medium.

Default priority: Medium.

Jira keeps them in the site's order (**Settings › Work items › Priorities**), not the order given here.

## 21. Components

**Space settings › Components › Create component**.

| Component | Description | Default assignee |
| --- | --- | --- |
| API | The API | Space default |

## 22. Versions

**Space settings › Versions › Create version** (the page may be called Releases).

| Version | Start date | Release date | Released | Archived |
| --- | --- | --- | --- | --- |
| 1.10 |  |  | No |  |
| 1.9 | 2026-01-01 | 2026-03-31 | Yes | No |

## 23. Role members

**Space settings › People › Add people**.

| Role | Add |
| --- | --- |
| Triage Team | Group team-alpha, User 5b10a2844c20165700ede21g |

## 24. Switch the space to its schemes

**Space settings**, then the page named in each row, **Actions › Use a different scheme**.

Jira may ask how to map existing work items' statuses, priorities or security levels; with a new space there are none.

| Scheme | Use | Space settings page |
| --- | --- | --- |
| Work item type scheme | DEMO Issue Type Scheme | Work items › Types |
| Work item type screen scheme | DEMO Issue Type Screen Scheme | Work items › Screens |
| Field configuration scheme | DEMO Field Configuration Scheme | Work items › Fields |
| Workflow scheme | DEMO Workflow Scheme | Work items › Workflows |
| Permission scheme | DEMO Permission Scheme | Permissions |
| Notification scheme | Default Notification Scheme | Notifications |
| Priority scheme | DEMO Priority Scheme | Priorities |

## 25. Filters

**Filters › Search work items**: enter the JQL, **Save filter**, then **Details › Edit permissions**.

| Filter | JQL | Viewers | Editors |
| --- | --- | --- | --- |
| DEMO Open findings | `project = DEMO AND status = Open` | Space DEMO, Group demo-team | Space DEMO, role Administrators |

## 26. Boards

**Search › Go to all: Boards › Create board**, choose Scrum or Kanban, then **Board from an existing Saved Filter**, with the space as its location.

| Board | Type | Filter |
| --- | --- | --- |
| DEMO board | Kanban | DEMO Open findings |

## 27. Work item layouts

**Space settings › Work items › Layout**, then the work type.

### Finding layout

## Notes

- Written by hand for tests.
- Space properties (`example.setting`) can be set only through Jira's REST API.
