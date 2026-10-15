# Setting up OWN by hand

Written from `own.yaml` by jira-space-config test. Work through the sections in order: each needs only the ones before it. Only what has to be added or changed is listed; Jira's own objects (standard statuses, system fields, the default schemes) are used as they are. "Create if missing" means: if one of that name is already there, check it has these values instead. Menu paths were checked against Atlassian's documentation in October 2026 and may have moved since.

You need the Administer Jira global permission, and Atlassian organisation admin for groups.

## 1. Space

**Spaces › Create space**, then a company-managed template of the type below.

| Setting | Value |
| --- | --- |
| Key | OWN |
| Name | *not in the spec: choose one* |
| Type | *not in the spec: choose one* |

If the space already exists, set these in **Space settings › Details** instead.

## 2. Work item type scheme

**Settings › Work items › Work item type schemes › Add work item type scheme**.

Change the work item type scheme the space uses now to match (the spec names none, and has no managed_prefix).

| Work types, in order |
| --- |
| Task |

Default work type: Task.

## 3. Permissions

**Settings › Work items › Permission schemes › Add permission scheme**, then **Permissions** to grant.

Change the permission scheme the space uses now to match (the spec names none, and has no managed_prefix).

Grant exactly these, and revoke any grant Jira adds by itself (it adds the guest role to many):

| Permission | Granted to |
| --- | --- |
| Browse projects `BROWSE_PROJECTS` | Role Administrators |
