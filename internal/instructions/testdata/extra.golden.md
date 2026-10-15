# Setting up EXT by hand

The rest of the format, for the instructions' tests.

Written from `extra.yaml` by jira-space-config test. Work through the sections in order: each needs only the ones before it. Only what has to be added or changed is listed; Jira's own objects (standard statuses, system fields, the default schemes) are used as they are. "Create if missing" means: if one of that name is already there, check it has these values instead. Menu paths were checked against Atlassian's documentation in October 2026 and may have moved since.

You need the Administer Jira global permission, and Atlassian organisation admin for groups.

## 1. Space

**Spaces › Create space**, then a company-managed template of the type below.

| Setting | Value |
| --- | --- |
| Key | EXT |
| Name | *not in the spec: choose one* |
| Type | *not in the spec: choose one* |
| Default assignee | Space lead |

If the space already exists, set these in **Space settings › Details** instead.

## 2. Project roles

Nothing to create. Already on the site, used as they are: Administrators, atlassian-addons-project-access.

## 3. Groups

**admin.atlassian.com › Directory › Groups › Create group** (Atlassian Administration).

|  | Group |
| --- | --- |
| Create if missing | ext-team |

- *ext-team:* Ask the organisation admin.

## 4. Statuses

**Settings › Work items › Statuses › Add status**.

|  | Status | Category |
| --- | --- | --- |
| Create if missing | Waiting | To do |

Already on the site, used as they are: Done.

## 5. Priorities

**Settings › Work items › Priorities**: fill in the form below the list.

|  | Priority | Colour | Icon |
| --- | --- | --- | --- |
| Create if missing | Blocker | #bf2600 | /images/icons/priorities/blocker.svg |

## 6. Work item link types

**Settings › Work items › Work item linking**: fill in the form below the list.

|  | Name | Outward description | Inward description |
| --- | --- | --- | --- |
| Create if missing | Causes | causes | is caused by |

## 7. Work item types

**Settings › Work items › Work item types › Add work item type**.

|  | Work item type | Level |
| --- | --- | --- |
| Create if missing | Check | Sub-task |
| Create if missing (by hand only: apply cannot) | Initiative | Hierarchy level 2 |
| Create if missing (by hand only: apply cannot) | Theme | Epic |

Already on the site, used as they are: Task.

## 8. Custom fields

**Settings › Work items › Fields › Create new field**.

|  | Field | Type |
| --- | --- | --- |
| Create if missing | Area | Select List (multiple choices) |
| Create if missing | Reviewer | User Picker (single user) |
| Create if missing | Starts | Date Picker |
| Create if missing | Ends | Date Picker |
| Create if missing (by hand only: apply cannot) | Score | app field `com.example:score` |
| Create if missing | Notes | Jira field type `textfield` |

Options and work types are set per context in the next section, not here. A new field is not added to any field scheme by itself.

## 9. Field contexts

**Settings › Work items › Fields**, then the field's **More actions › Contexts and default value**, then **Add new context**.

Each context applies to space EXT only, for the work types listed.

| Field | Context | Work types | Options, in order | Default |
| --- | --- | --- | --- | --- |
| Area | EXT Area | Task, Check | North, South | North, South |
| Reviewer | EXT Reviewer | All |  | None |

## 10. Screens

**Settings › Work items › Screens › Add screen**; add each tab and its fields in the order given.

| Screen | Tab | Fields, in order |
| --- | --- | --- |
| EXT Screen | Main | Summary, Area, Reviewer |

- *EXT Screen:* Keep it short.

## 11. Workflows

**Settings › Work items › Workflows › Create workflow**; add the statuses, then each transition with its rules.

### EXT Workflow

Statuses: Waiting, Done.

| Transition | From | To | Rules |
| --- | --- | --- | --- |
| Create | (create) | Waiting |  |
| Finish | Waiting | Done | Description: Every rule, \| in words<br>**Only if:** Area = North, South (compared as string) or Blocked unless related work items are in Done or Not the person who made the transition from Waiting to any status or Blocked while an approval is in progress or Never previously in Done or ((Restrict to permission BROWSE_PROJECTS or the user in Reviewer or not the user in Reviewer) and (Restrict to a group in Area or user allow-reporter))<br>**Validate:** Has permission RESOLVE_ISSUES; Blocked unless related work items are in Done; Previously in Waiting; Area has changed (except for ext-team), message "Change the area"; Area has a single value; Summary matches `^EXT-`, message "Use the prefix"; Starts < Ends; Starts is within 30 days of Ends; A form is attached; Every form is submitted<br>**Then:** Copy Area to Area from the parent; Clear Reviewer; Add North to Area; Trigger webhook 7; Ask agent 5b10a2844c20165700ede21g: Summarise it; Assign to selected user 5b10a2844c20165700ede21g; App rule `com.example:no-params` |

Already on the site, used as they are: Shared Workflow.

## 12. Work item type scheme

**Settings › Work items › Work item type schemes › Add work item type scheme**.

Create the work item type scheme **EXT Issue Type Scheme**:

| Work types, in order |
| --- |
| Task |
| Check |

Default work type: Task.

## 13. Screens of the work types

**Settings › Work items › Screen schemes › Add screen scheme**, then **Work item type screen schemes › Add work item type screen scheme**.

Create the screen schemes:

| Screen scheme | Create | Edit | View |
| --- | --- | --- | --- |
| EXT Task Screen Scheme | EXT Screen | EXT Screen | EXT Screen |

Create the work item type screen scheme **EXT Screens**:

| Work type | Screen scheme |
| --- | --- |
| Default | EXT Task Screen Scheme |

## 14. Fields in the space

**Settings › Work items › Field schemes**, create the scheme, then **Add fields**.

Create the field scheme **EXT Field Scheme**:

Add these fields, and remove any other that Jira added and allows to be removed.

| Field | Required | Help text in this space |
| --- | --- | --- |
| Summary | Yes |  |
| Area |  | Where it is |
| ↳ for Check |  | Where to check |

## 15. Workflows of the work types

**Settings › Work items › Workflow schemes › Add workflow scheme**.

Use the existing workflow scheme **Shared Workflow Scheme**; it is not changed.

## 16. Permissions

**Settings › Work items › Permission schemes › Add permission scheme**, then **Permissions** to grant.

Create the permission scheme **EXT Permission Scheme**:

Grant exactly these, and revoke any grant Jira adds by itself (it adds the guest role to many):

| Permission | Granted to |
| --- | --- |
| Browse projects `BROWSE_PROJECTS` | Role atlassian-addons-project-access (id 10003), Any application access, Application access jira-software, Anyone, Space lead |
| Manage watchers `MANAGE_WATCHERS` | User in field Reviewer, Email someone@example.com |

## 17. Notifications

**Settings › Work items › Notification schemes › Add notification scheme**.

Create the notification scheme **EXT Notification Scheme**:

| Event | Notify |
| --- | --- |
| Issue Resolved | Reporter, All watchers, Group in field Area, Current user, Component lead |

## 18. Work item security

**Settings › Work items › Work item security schemes › Add work item security scheme**, then **Security levels**.

Create the work item security scheme **EXT Issue Security Scheme**:

| Security level | Description | Members |
| --- | --- | --- |
| Private | Team only | Group ext-team, Reporter |
| Public |  | Any application access |

Default level: Private.

## 19. Priorities in the space

**Settings › Work items › Priority schemes › Add priority scheme**.

Use the existing priority scheme **Shared Priority Scheme**; it is not changed.

## 20. Switch the space to its schemes

**Space settings**, then the page named in each row, **Actions › Use a different scheme**.

Jira may ask how to map existing work items' statuses, priorities or security levels; with a new space there are none.

| Scheme | Use | Space settings page |
| --- | --- | --- |
| Work item type scheme | EXT Issue Type Scheme | Work items › Types |
| Work item type screen scheme | EXT Screens | Work items › Screens |
| Field scheme | EXT Field Scheme | Work items › Fields |
| Workflow scheme | Shared Workflow Scheme | Work items › Workflows |
| Permission scheme | EXT Permission Scheme | Permissions |
| Notification scheme | EXT Notification Scheme | Notifications |
| Work item security scheme | EXT Issue Security Scheme | Work item security |
| Priority scheme | Shared Priority Scheme | Priorities |

## 21. Filters

**Filters › Search work items**: enter the JQL, **Save filter**, then **Details › Edit permissions**.

| Filter | JQL | Viewers | Editors |
| --- | --- | --- | --- |
| EXT Everything | `project = EXT` | Any logged-in user, Public, User 5b10a2844c20165700ede21g | Nobody else |

- *EXT Everything:* For the board.

Already on the site, used as they are: Shared filter.

## 22. Boards

**Search › Go to all: Boards › Create board**, choose Scrum or Kanban, then **Board from an existing Saved Filter**, with the space as its location.

| Board | Type | Filter |
| --- | --- | --- |
| EXT Scrum | Scrum | EXT Everything |

- *EXT Scrum:* Add a sprint.

Already on the site, used as they are: Old board.

## 23. Automation rules

**Space settings › Automation › Create rule**.

Build each rule in the rule editor from its outline; the types named are the components' types as Jira's Automation REST API names them. The JSON is the whole rule in that API's form: use it to check a rule built by hand, or to create the rule through the API. It is not a file for **Settings › System › Automation flows › More actions › Import flows**: Atlassian does not document that file's format, so export a rule there first to see it.

### EXT Close done checks

Disabled, this space only. Closes a check when its parent is done.

*Note:* Test it on a copy first.

- **When:** `jira.issue.event.trigger:transitioned`
- **If:** `jira.condition.if.block`
  - **Block:** `jira.condition.if.block.if`
    - **If:** `jira.issue.condition`
    - **Then:** `jira.issue.transition`
- **For:** `jira.issue.related`
  - **Then:** `jira.issue.comment`
- **NEWKIND:** `x`

<details><summary>Rule JSON</summary>

```json
{
  "components": [
    {
      "children": [
        {
          "children": [
            {
              "children": [],
              "component": "ACTION",
              "conditions": [],
              "type": "jira.issue.transition"
            }
          ],
          "component": "CONDITION_BLOCK",
          "conditions": [
            {
              "children": [],
              "component": "CONDITION",
              "conditions": [],
              "type": "jira.issue.condition"
            }
          ],
          "type": "jira.condition.if.block.if"
        }
      ],
      "component": "CONDITION",
      "type": "jira.condition.if.block"
    },
    {
      "children": [
        {
          "component": "ACTION",
          "type": "jira.issue.comment"
        }
      ],
      "component": "BRANCH",
      "type": "jira.issue.related"
    },
    {
      "component": "NEWKIND",
      "type": "x"
    }
  ],
  "trigger": {
    "children": [],
    "component": "TRIGGER",
    "conditions": [],
    "type": "jira.issue.event.trigger:transitioned"
  }
}
```

</details>

Already on the site, used as they are: Site rule.

## 24. Work item layouts

**Space settings › Work items › Layout**, then the work type.

### Task layout

- Put Area first.

```yaml
sections:
  - name: Details
    fields:
      - Area
```
