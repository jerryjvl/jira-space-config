# Automation rules

A spec's `automation_rules` are the space's Jira Automation rules: "when
this happens, if this is true, do that" (assign new bugs, move a parent when
its subtasks are done, send a reminder). They are not workflow rules
(conditions, validators and post functions), which belong to workflows.

Export writes every rule whose scope includes the space. Apply (step 28)
creates a missing rule, and changes one the tool made (mode managed) while
it covers this space alone. A rule that covers other spaces too, or the
whole site, is compared and never changed.

## A rule entry

```yaml
automation_rules:
  - name: VULN Close duplicates     # managed names start with space.managed_prefix
    mode: managed                   # (the default) or reuse
    description: Closes duplicates  # optional
    state: enabled                  # (the default) or disabled
    scope: space                    # (the default): this space alone; or shared
    id: 01a13a2d-e70f-7dc8-bac9-4a19f6ed28b2   # the rule's UUID, a cross-check
    rule:                           # Jira's rule, passed through as it is
      trigger: {...}
      components: [...]
```

The fields above are the format's. Everything under `rule:` is Jira's own
rule object, passed through untouched in both directions: Atlassian adds to
it as Automation grows, so the tool does not model it. Write it in YAML; it
is sent as the JSON it stands for.

The tool takes care of three things under `rule:`:

- **Components' IDs.** Each component has an `id`, and children a
  `parentId` (or a `conditionParentId`) pointing at it. Jira assigns them: a
  new rule gets new ones whatever is sent, and a changed rule must keep its
  own. Export leaves them out, and apply fills them in: new ones for a new
  rule, the rule's own (matched by place and type) for a changed one.
- **The author** (`authorAccountId`). A new rule's is the account apply runs
  as; a changed rule keeps its own.
- **The space.** The rule's scope (`ruleScopeARIs`) is the space.

Rules hold the site's IDs: the space's resource name in trigger filters
(`ari:cloud:jira:<cloud-id>:project/<space-id>`), field and status IDs,
account IDs. A rule is therefore bound to its site; `--ignore-ids` does not
translate it. Copying a rule to another space on the same site means
changing the space's ID in its filters.

## What `rule:` takes

Every key Jira's rule object accepts (the format's own fields aside):

| Key | Values | Meaning |
| --- | --- | --- |
| `trigger` | a component, of kind `TRIGGER` | what starts the rule |
| `components` | a list of components | what the rule does, in order |
| `actor` | `{type: ACCOUNT_ID, actor: <account-id>}`, `{type: EVENT_INITIATOR}`, `{type: SMART_VALUE, actor: '{{...}}'}` | who the rule acts as |
| `notifyOnError` | `FIRSTERROR`, `EVERYERROR`, `NEVER` | when the rule's owner is told it failed |
| `writeAccessType` | `UNRESTRICTED`, `OWNER_ONLY`, `SPECIFIED_USERS` | who may edit the rule in Jira |
| `collaborators` | a list of account IDs | with `SPECIFIED_USERS`, who may edit it |
| `canOtherRuleTrigger` | `true`, `false` | whether other rules' actions can start this one |
| `labels` | Jira's labels for the rule, as it gives them | how the rule is grouped in Jira's list |
| `connections` | a list of `{id, accountId, connectionTargetKey, targetConfigJson, authType}` | links to other products (Confluence, GitHub); a component names one by `connectionId` |

An actor of type `SMART_VALUE` is accepted, but Jira gives it back without
its expression; set such an actor in Jira.

Every component has these keys:

| Key | Values |
| --- | --- |
| `component` | the kind: `TRIGGER`, `CONDITION`, `ACTION`, `BRANCH`, `CONDITION_BLOCK` (a branch of an if/else), `ORCHESTRATION` |
| `type` | what it is, such as `jira.issue.event.trigger:transitioned` or `jira.issue.transition`; the examples show the common ones |
| `schemaVersion` | the version of its `value`'s form; keep it as exported |
| `value` | its settings, in the form its type takes (see the examples) |
| `children` | for a branch or a block, the components inside it |
| `conditions` | conditions attached to it |
| `connectionId` | the connection it uses, or null |

A type's `value` is open-ended: the best way to write one is to build the
rule in Jira once, export it, and change the copy.

## Examples

Each example is a rule as export writes it (account and cloud IDs
replaced). Together they cover every key above and every kind of component
except `ORCHESTRATION`.

### A trigger, a condition and an action

When a work item is linked as a duplicate, and is not done, it is closed as
a duplicate.

```yaml
- name: Close duplicate work items
  mode: reuse
  state: disabled
  id: <rule-uuid>
  rule:
    actor: {actor: '<account-id>', type: ACCOUNT_ID}
    canOtherRuleTrigger: false
    collaborators: []
    components:
      - children: []
        component: CONDITION
        conditions: []
        connectionId: null
        schemaVersion: 3
        type: jira.issue.condition
        value:
          compareValue: {modifier: null, multiValue: false, source: null, type: ID, value: "10010"}
          comparison: NOT_EQUAL
          selectedField: {type: ID, value: status}
          selectedFieldType: status
      - children: []
        component: ACTION
        conditions: []
        connectionId: null
        schemaVersion: 11
        type: jira.issue.transition
        value:
          advancedFields: null
          destinationStatus: {type: ID, value: "10010"}
          operations:
            - field: {type: ID, value: resolution}
              fieldType: resolution
              type: SET
              value: {type: NAME, value: Duplicate}
          sendNotifications: true
          transitionMatch: null
    labels: []
    notifyOnError: FIRSTERROR
    trigger:
      component: TRIGGER
      conditions: []
      connectionId: null
      schemaVersion: -1
      type: jira.issue.event.trigger:link
      value:
        eventFilters: ['ari:cloud:jira:<cloud-id>:project/10004']
        linkTypes: [Duplicate]
    writeAccessType: UNRESTRICTED
```

### A branch, with conditions and an action inside it

When a work item moves to Done, its parent is moved to Done if all of its
subtasks are done.

```yaml
- name: When all sub-tasks are done → move parent to done
  mode: reuse
  state: disabled
  id: <rule-uuid>
  rule:
    actor: {actor: '<account-id>', type: ACCOUNT_ID}
    canOtherRuleTrigger: false
    collaborators: []
    components:
      - children:
          - children: []
            component: CONDITION
            conditions: []
            connectionId: null
            schemaVersion: 3
            type: jira.issue.condition
            value:
              compareValue: {modifier: null, multiValue: false, source: null, type: NAME, value: Done}
              comparison: NOT_EQUAL
              selectedField: {type: ID, value: status}
              selectedFieldType: status
          - children: []
            component: CONDITION
            conditions: []
            connectionId: null
            schemaVersion: 1
            type: jira.issues.related.condition
            value:
              compareJql: status = "Done"
              compareValue: 0
              jql: null
              linkTypes: []
              operator: allmatch
              relatedJql: null
              relatedType: subtasks
              similarityLimit: 0
          - children: []
            component: ACTION
            conditions: []
            connectionId: null
            schemaVersion: 11
            type: jira.issue.transition
            value:
              advancedFields: null
              destinationStatus: {type: NAME, value: Done}
              operations:
                - field: {type: ID, value: resolution}
                  fieldType: resolution
                  type: SET
                  value: {type: COPY, value: trigger}
              sendNotifications: true
              transitionMatch: null
        component: BRANCH
        conditions: []
        connectionId: null
        schemaVersion: 1
        type: jira.issue.related
        value:
          compareValue: 0
          jql: null
          linkTypes: []
          onlyUpdatedIssues: false
          relatedType: parent
          similarityLimit: 0
    labels: []
    notifyOnError: FIRSTERROR
    trigger:
      component: TRIGGER
      conditions: []
      connectionId: null
      schemaVersion: -1
      type: jira.issue.event.trigger:transitioned
      value:
        eventFilters: ['ari:cloud:jira:<cloud-id>:project/10004']
        eventKey: jira:issue_updated
        fromStatus: []
        issueEvent: issue_generic
        toStatus:
          - type: NAME
            value: Done
    writeAccessType: UNRESTRICTED
```

### Smart values

When someone comments, every work item key mentioned in the comment is
linked; `{{...}}` are Jira's smart values, kept as written.

```yaml
- name: Link work items that are mentioned in the comments
  mode: reuse
  state: disabled
  id: <rule-uuid>
  rule:
    actor: {actor: '<account-id>', type: ACCOUNT_ID}
    canOtherRuleTrigger: false
    collaborators: []
    components:
      - children: []
        component: CONDITION
        conditions: []
        connectionId: null
        schemaVersion: 1
        type: jira.comparator.condition
        value: {first: '{{comment.body}}', operator: REGEX_CONTAINS, second: '[A-Z]+-\d+'}
      - children: []
        component: ACTION
        conditions: []
        connectionId: null
        schemaVersion: 2
        type: jira.issue.link
        value:
          issue: {type: SMART, value: '{{comment.body.match("([A-Z]+-\d+)")}}'}
          linkType: outward:10003
    labels: []
    notifyOnError: FIRSTERROR
    trigger:
      component: TRIGGER
      conditions: []
      connectionId: null
      schemaVersion: 1
      type: jira.issue.event.trigger:commented
      value:
        eventFilters: ['ari:cloud:jira:<cloud-id>:project/10004']
        eventTypes: []
    writeAccessType: UNRESTRICTED
```

### A field change, and editing fields

When the summary changes, hashtags in it become labels.

```yaml
- name: When work item summary is updated → Add labels based on hashtags
  mode: reuse
  description: Speed up work item labelling. When a hashtag is added to a work item summary, this rule will automatically convert it to a label and add it to the work item.
  state: disabled
  id: <rule-uuid>
  rule:
    actor: {actor: '<account-id>', type: ACCOUNT_ID}
    canOtherRuleTrigger: false
    collaborators: []
    components:
      - children: []
        component: CONDITION
        conditions: []
        connectionId: null
        schemaVersion: 3
        type: jira.issue.condition
        value:
          compareValue: {modifier: null, multiValue: false, source: null, type: VALUE, value: '#'}
          comparison: CONTAINS
          selectedField: {type: ID, value: summary}
          selectedFieldType: summary
      - children:
          - children: []
            component: ACTION
            conditions: []
            connectionId: null
            schemaVersion: 12
            type: jira.issue.edit
            value:
              advancedFields: null
              operations:
                - field: {type: ID, value: labels}
                  fieldType: labels
                  type: ADDREMOVE
                  value:
                    ADD:
                      - type: SMART
                        value: '{{hashtags}}'
              sendNotifications: false
        component: BRANCH
        conditions: []
        connectionId: null
        schemaVersion: -1
        type: jira.smart.values.branch
        value:
          id: label-branch-id
          lazy: false
          name: {type: FREE, value: hashtags}
          query: {type: SMART, value: '{{issue.summary.match("#([^\s]+)")}}'}
          type: SMART
      - children: []
        component: ACTION
        conditions: []
        connectionId: null
        schemaVersion: 12
        type: jira.issue.edit
        value:
          advancedFields: null
          operations:
            - field: {type: ID, value: summary}
              fieldType: summary
              type: SET
              value: '{{issue.summary.replaceAll("#([^\s]+)", "")}}'
          sendNotifications: false
    labels: []
    notifyOnError: FIRSTERROR
    trigger:
      component: TRIGGER
      conditions: []
      connectionId: null
      schemaVersion: 2
      type: jira.issue.field.changed
      value:
        actions: []
        changeType: ANY_CHANGE
        eventFilters: ['ari:cloud:jira:<cloud-id>:project/10004']
        fields:
          - type: field
            value: summary
    writeAccessType: UNRESTRICTED

```

### A scheduled trigger, and creating a work item

Every Monday at 9:00, a work item is created.

```yaml
- name: Schedule a recurring ticket
  mode: reuse
  description: Speed up work flow by making recurring ticket creation automatic
  state: disabled
  id: <rule-uuid>
  rule:
    actor: {actor: '<account-id>', type: ACCOUNT_ID}
    canOtherRuleTrigger: false
    collaborators: []
    components:
      - children: []
        component: ACTION
        conditions: []
        connectionId: null
        schemaVersion: 12
        type: jira.issue.create
        value:
          advancedFields: null
          operations:
            - field: {type: ID, value: project}
              fieldType: project
              type: SET
              value: {type: ID, value: "10004"}
            - field: {type: ID, value: summary}
              fieldType: summary
              type: SET
              value: Recurring ticket
            - field: {type: ID, value: issuetype}
              fieldType: issuetype
              type: SET
              value: {type: FREE}
            - field: {type: ID, value: description}
              fieldType: description
              type: SET
              value: Fill in the ticket description here
          sendNotifications: false
    labels: []
    notifyOnError: FIRSTERROR
    trigger:
      component: TRIGGER
      conditions: []
      connectionId: null
      schemaVersion: 1
      type: jira.jql.scheduled
      value:
        executionMode: nosearch
        jql: null
        onlyUpdatedIssues: false
        schedule:
          cronExpression: ""
          method: BASIC
          rRule: |-
            BEGIN:VEVENT
            DTSTART;TZID=Australia/Sydney:20221207T172502
            RRULE:FREQ=WEEKLY;BYDAY=MO;BYHOUR=9;BYMINUTE=0;BYSECOND=0;INTERVAL=1
            END:VEVENT
          rate: 0
          rateInterval: 86400
    writeAccessType: UNRESTRICTED
```

### A scheduled search, a lookup, and an email

Work items due soon are looked up, and their assignees emailed.

```yaml
- name: When a task is near due → send email reminder
  mode: reuse
  description: Never miss a deadline with email reminders. When a task is near due, this rule will automatically send email notification to the assignee.
  state: disabled
  id: <rule-uuid>
  rule:
    actor: {actor: '<account-id>', type: ACCOUNT_ID}
    canOtherRuleTrigger: false
    collaborators: []
    components:
      - children: []
        component: ACTION
        conditions: []
        connectionId: null
        schemaVersion: -1
        type: jira.create.variable
        value:
          id: number-Of-Days-Before-Due-Inclusion
          lazy: false
          name: {type: FREE, value: numberOfDaysBeforeDueInclusion}
          query: {type: SMART, value: "3"}
          type: SMART
      - children: []
        component: ACTION
        conditions: []
        connectionId: null
        schemaVersion: -1
        type: jira.lookup.issues
        value:
          id: outer-lookup-id
          lazy: false
          name: {type: FREE, value: lookupIssues}
          query:
            type: SMART
            value: duedate <= endOfDay({{numberOfDaysBeforeDueInclusion}}) AND statusCategory != done AND assignee != empty
          type: JQL
      - children:
          - children: []
            component: ACTION
            conditions: []
            connectionId: null
            schemaVersion: -1
            type: jira.lookup.issues
            value:
              id: inner-lookup-id
              lazy: false
              name: {type: FREE, value: lookupIssues}
              query:
                type: SMART
                value: duedate <= endOfDay({{numberOfDaysBeforeDueInclusion}}) AND statusCategory != done AND assignee = {{distinctAssignee}}
              type: JQL
          - children: []
            component: ACTION
            conditions: []
            connectionId: null
            schemaVersion: 5
            type: jira.issue.outgoing.email
            value:
              bcc: []
              body: |-
                Hello {{distinctAssignee.displayName}},

                The following work items are due within next 3 days:
                {{#lookupIssues}}
                <a href="{{url}}">{{key}} - {{summary}}</a>
                {{/}}

                Regards,

                Jira Automation
              cc: []
              convertLineBreaks: true
              mimeType: text/html
              subject: Issues Due Within 3 Days
              to:
                - type: SMART
                  value: '{{distinctAssignee.emailAddress}}'
        component: BRANCH
        conditions: []
        connectionId: null
        schemaVersion: -1
        type: jira.smart.values.branch
        value:
          id: assignee-branch-id
          lazy: false
          name: {type: FREE, value: distinctAssignee}
          query: {type: SMART, value: '{{lookupIssues.assignee.distinct}}'}
          type: SMART
    labels: []
    notifyOnError: FIRSTERROR
    trigger:
      component: TRIGGER
      conditions: []
      connectionId: null
      schemaVersion: 1
      type: jira.jql.scheduled
      value:
        executionMode: nosearch
        jql: null
        onlyUpdatedIssues: false
        schedule:
          cronExpression: ""
          method: BASIC
          rRule: |-
            BEGIN:VEVENT
            DTSTART;TZID=Australia/Sydney:20221207T172502
            RRULE:FREQ=DAILY;BYHOUR=8;BYMINUTE=0;BYSECOND=0;INTERVAL=1
            END:VEVENT
          rate: 0
          rateInterval: 86400
    writeAccessType: UNRESTRICTED
```

### A manual trigger, and another product

The rule is run by hand from a work item, and creates a Confluence page.

```yaml
- name: Create Confluence page from work item
  mode: reuse
  description: Creates a Confluence page from a work item.
  state: disabled
  id: <rule-uuid>
  rule:
    actor: {actor: '<account-id>', type: ACCOUNT_ID}
    canOtherRuleTrigger: false
    collaborators: []
    components:
      - children: []
        component: ACTION
        conditions: []
        connectionId: null
        schemaVersion: 2
        type: confluence.create-page.action
        value:
          destinationPageTitle: '{{userInputs.pageName}}'
          destinationSpace:
            containsExternalCollaborators: false
            id: ID
            isUnknownSpace: true
            key: SimpleValue{type=ID, value='null', additional='null'}
            label: ""
          isPageBodyAdf: false
    labels: []
    notifyOnError: FIRSTERROR
    trigger:
      component: TRIGGER
      conditions: []
      connectionId: null
      schemaVersion: 3
      type: jira.manual.trigger.issue
      value:
        groups: []
        inputFromUsers: true
        inputPrompts:
          - defaultValue: Technical Specifications
            displayName: Page Name
            inputType: TEXT
            required: true
            variableName: pageName
        issueFilter: null
        jQLFilter: null
    writeAccessType: UNRESTRICTED
```

### A rule other spaces use

A rule whose scope is more than this space is exported as `scope: shared`
and `mode: reuse`: apply compares it, and never changes it.

```yaml
- name: Assign new bugs to the triage lead
  mode: reuse
  state: enabled
  scope: shared
  id: <rule-uuid>
  rule: {...}
```

### If/else

An if/else is a component of kind `CONDITION`, type
`jira.condition.container.block`, whose children are its branches: each a
`CONDITION_BLOCK` of type `jira.condition.if.block`, with its tests in
`conditions` and what it does in `children`. The else branch is a block with
no conditions; an "else if" is a block with conditions after the first.

When the work item is linked as a duplicate and is not done, it is closed as
a duplicate; then, if it is done, a line is logged, and otherwise a line is
added to its last comment. (A log action's message must not be empty: Jira
refuses the rule.)

```yaml
- name: VULN Close duplicates, or log
  state: disabled
  id: <rule-uuid>
  rule:
    actor: {actor: '<account-id>', type: ACCOUNT_ID}
    canOtherRuleTrigger: false
    collaborators: []
    components:
      - children: []
        component: CONDITION
        conditions: []
        connectionId: null
        schemaVersion: 3
        type: jira.issue.condition
        value:
          compareValue: {modifier: null, multiValue: false, source: null, type: ID, value: "10010"}
          comparison: NOT_EQUAL
          selectedField: {type: ID, value: status}
          selectedFieldType: status
      - children: []
        component: ACTION
        conditions: []
        connectionId: null
        schemaVersion: 11
        type: jira.issue.transition
        value:
          advancedFields: null
          destinationStatus: {type: ID, value: "10010"}
          operations:
            - field: {type: ID, value: resolution}
              fieldType: resolution
              type: SET
              value: {type: NAME, value: Duplicate}
          sendNotifications: true
          transitionMatch: null
      - children:
          - children:
              - children: []
                component: ACTION
                conditions: []
                connectionId: null
                schemaVersion: 1
                type: codebarrel.action.log
                value: Already done
            component: CONDITION_BLOCK
            conditions:
              - children: []
                component: CONDITION
                conditions: []
                connectionId: null
                schemaVersion: 3
                type: jira.issue.condition
                value:
                  compareValue: {modifier: null, multiValue: true, source: null, type: NAME, value: '["Done"]'}
                  comparison: ONE_OF
                  selectedField: {type: ID, value: status}
                  selectedFieldType: status
            connectionId: null
            schemaVersion: 1
            type: jira.condition.if.block
            value: {conditionMatchType: ALL}
          - children:
              - children: []
                component: ACTION
                conditions: []
                connectionId: null
                schemaVersion: 2
                type: jira.issue.comment.update
                value:
                  comment: |-
                    Appended by the else branch.

                    {{currentComment}}
                  commentId: {type: REFERENCE, value: LAST_COMMENT}
                  commentVisibility: null
                  editVisibility: false
                  publicComment: null
                  sendNotifications: true
            component: CONDITION_BLOCK
            conditions: []
            connectionId: null
            schemaVersion: 1
            type: jira.condition.if.block
            value: {conditionMatchType: ALL}
        component: CONDITION
        conditions: []
        connectionId: null
        schemaVersion: 1
        type: jira.condition.container.block
        value: {}
    labels: []
    notifyOnError: FIRSTERROR
    trigger:
      component: TRIGGER
      conditions: []
      connectionId: null
      schemaVersion: -1
      type: jira.issue.event.trigger:link
      value:
        eventFilters: ['ari:cloud:jira:<cloud-id>:project/10004']
        linkTypes: [Duplicate]
    writeAccessType: UNRESTRICTED
```

### A connection to another product

The Claude action uses a connection, a stored login to another product; the
rule lists it under `connections`, and the action names it by
`connectionId`. This rule is run every Wednesday at 9:00 (a scheduled
trigger with a weekly `rRule`).

```yaml
- name: VULN Ask Claude about the repo
  state: disabled
  id: <rule-uuid>
  rule:
    actor: {actor: '<account-id>', type: ACCOUNT_ID}
    canOtherRuleTrigger: false
    collaborators: []
    components:
      - children: []
        component: ACTION
        conditions: []
        connectionId: <connection-uuid>
        schemaVersion: 1
        type: rovo3p.use-claude.action
        value:
          prompt: Explain the structure of the repo
          repoUrl: https://github.com/jerryjvl/jira-space-config
    connections:
      - accountId: <account-id>
        authType: ASAP
        connectionTargetKey: com.atlassian.rovo
        id: <connection-uuid>
        targetConfigJson: '{"product":"jira","cloudId":"<cloud-id>"}'
    labels: []
    notifyOnError: FIRSTERROR
    trigger:
      component: TRIGGER
      conditions: []
      connectionId: null
      schemaVersion: 1
      type: jira.jql.scheduled
      value:
        executionMode: nosearch
        jql: ""
        onlyUpdatedIssues: true
        schedule:
          cronExpression: ""
          method: BASIC
          rRule: |
            BEGIN:VEVENT
            DTSTART;TZID=Australia/Sydney:20261014T233347
            RRULE:FREQ=WEEKLY;BYHOUR=9;BYMINUTE=0;BYSECOND=0;BYDAY=WE;INTERVAL=1
            END:VEVENT
          rate: 0
          rateInterval: 86400
    writeAccessType: OWNER_ONLY
```

The connection's own `id` is Jira's to number: apply gives a new rule
fresh ones (and points the components at them), and a changed rule keeps the
ones it has, which it does not list again. Export leaves out the connection's
bookkeeping (`container`, `createdAt`, `updatedAt`). Jira never removes a
connection a change stops using, so if a rule was copied from another and
its connection was swapped, the old connection stays on the rule until it is
removed in Jira. Connections hold the site's IDs (`targetConfigJson` names
the cloud ID), so a rule with one is bound to its site like any other.

`ORCHESTRATION` components pass through like any other.
