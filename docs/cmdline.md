# Command line

Every command and option, with examples. The spec file itself is described in
[spec-format.md](spec-format.md).

```console
$ jira-space-config <command> [options] [FILE]
$ jira-space-config <command> --help
$ jira-space-config --version
```

| Command | Contacts Jira | Changes Jira |
| --- | --- | --- |
| [`whoami`](#whoami) | Yes | No |
| [`validate`](#validate) | No | No |
| [`export`](#export) | Yes | No |
| [`apply --dry-run`](#apply) | Yes | No |
| [`apply`](#apply) | Yes | **Yes** |
| [`instructions`](#instructions) | No | No |

Any failure exits with status 1 and says what failed and how to fix it.

## Credentials and connection

Credentials come only from the environment:

| Variable | Value |
| --- | --- |
| `JIRA_EMAIL` | The Atlassian account's email address |
| `JIRA_API_TOKEN` | An [API token](https://id.atlassian.com/manage-profile/security/api-tokens) |

The token is never printed, logged or written to a file. The site is never
read from the environment: every command that contacts Jira takes it as
`--site`.

Options for every command that contacts Jira:

| Option | Meaning |
| --- | --- |
| `--site URL` | The site, e.g. `https://example.atlassian.net` (required) |
| `--api site\|gateway` | `site` (default) for classic API tokens; `gateway` for [scoped API tokens](https://support.atlassian.com/atlassian-account/docs/manage-api-tokens-for-your-atlassian-account/), sent through `https://api.atlassian.com/ex/jira/<cloudId>`. A token with read scopes only is a good safeguard for the read-only commands |
| `--max-requests N` | Stop with an explanation after N requests (default 2000) |

## whoami

Checks the credentials and shows whether the account has the permissions the
tool needs. Read-only.

| Option | Meaning |
| --- | --- |
| `--space KEY` | Also check this space |

```console
$ jira-space-config whoami --site https://example.atlassian.net --space DEMO
Site      https://example.atlassian.net
Cloud ID  0e1c…
API       site (https://example.atlassian.net)
User      Example User (account 5b10…)
Plan      jira-software: PAID
Space     DEMO "Demo space" (company-managed, id 10042)

Permissions
  Administer Jira (global)          yes  needed to read schemes, workflows, screens and fields
  Administer projects (space DEMO)  yes  needed to read the space's configuration
  Browse projects (space DEMO)      yes  needed to see the space and count its work items
```

## validate

Checks a spec offline and reports every problem as `file:line:col` messages.

| Option | Meaning |
| --- | --- |
| `-v`, `--verbose` | Also list the names the spec uses without declaring them; these must exist on the site |
| `--canonical` | Print the spec in canonical form (as `export` writes it) on standard output; messages go to standard error |

```console
$ jira-space-config validate spec.yaml
spec.yaml:5:29: error: statuses[0]: unknown key "catgory" (did you mean "category"?)
spec.yaml:7:3: error: space.key: "demo" is not a valid space key: a capital letter, then capital letters, digits or underscores
spec.yaml: 2 errors, 0 warnings
$ jira-space-config validate --canonical spec.yaml > canonical.yaml
```

## export

Writes a space's configuration as a spec, with IDs, in canonical form.
Read-only. Objects the space shares with the rest of the site are written as
`mode: reuse`; anything the format cannot hold is listed under `notes:`.

| Option | Meaning |
| --- | --- |
| `--space KEY` | The space (required) |
| `-o`, `--output FILE` | Write to FILE instead of standard output |
| `--force` | Overwrite FILE if it exists |

```console
$ jira-space-config export --site https://example.atlassian.net --space DEMO -o demo.yaml
Exported space DEMO: 5 issue types, 4 statuses, … 100 requests made, all read-only.
```

## apply

Compares a spec with the space in 28 fixed steps (site-wide objects first,
then schemes, then the space itself) and makes the space match: it creates
what is missing and changes only objects the tool made that no other space
uses. Spaces are never created; the space must exist.

```console
$ jira-space-config apply --dry-run --site https://example.atlassian.net --space DEMO spec.yaml
$ jira-space-config apply           --site https://example.atlassian.net --space DEMO spec.yaml
```

`--space` must equal `space.key` in the spec.

| Option | Meaning |
| --- | --- |
| `--space KEY` | The space (required) |
| `--dry-run` | Evaluate every step and print the plan; read-only, never prompts |
| `-o`, `--output FILE` | Live: where the resulting spec (an export, with IDs) is written; default `<spec>.applied.yaml`. The input is never rewritten |
| `--ignore-ids` | Find objects by name only. Needed to apply a spec written for another site: one whose `binding.cloud_id` is not this site's is refused |
| `--show-requests` | List the API call behind each action |
| `--no-colour` | No colour (`NO_COLOR` is honoured too) |
| `--log-dir DIR` | Live: where the lock and audit log are kept; default `$XDG_STATE_HOME/jira-space-config`, `~/.local/state/jira-space-config`, or `%LOCALAPPDATA%\jira-space-config` |
| `--allow-update`, `--allow-delete`, `--check-usage` | Allow particular changes; see below |

The exit status is 1 when something must be fixed before the spec can be
applied.

### What each step shows

| Mark | Meaning |
| --- | --- |
| `✓ exists` | Exists and matches the spec |
| `+ create` | Missing; will be created |
| `~ update` | A change to an object the tool made that only this space uses |
| `✗ differs` / `✗ error` | Exists but differs, is shared, or cannot be checked; fix it first |
| `■ blocked` | The change would affect existing work items; shows how many and what to do |
| `? manual` | Cannot be set through the API; set it in Jira by hand |

### A live run

Each step with changes is shown, then you choose:

- `p` proceed: make the step's changes (offered only when nothing must be fixed);
- `r` re-evaluate: read the site again, for example after fixing something in Jira;
- `a` abort: stop here.

Protections:

- Every write goes through one guard, the only code that can change Jira.
  It sends a write only if the item being carried out planned that method
  and path, and refuses any write that names another space in its path or
  body. Every other command, and a dry run, use a client that can only read.
- Before the first write, you type the space key.
- A step that creates more than 5 site-wide objects, or (for a spec applied
  before) more than half of its objects, asks you to type the count.
- Every prompt names the site and the space.
- One live run per site and space at a time (a lock file in the log
  directory).
- Ctrl-C never interrupts a write; the run stops between two writes and says
  what it did.
- Every write is recorded in a JSONL audit log in the log directory, with the
  spec's SHA-256.
- When a run stops, the summary lists what was done. Running again
  picks up where it stopped: what was created is found and reused.

### Allowing particular changes

Changes that widen access, or remove something, are made only when allowed,
by kind. A removal is still blocked while work items would be affected.

| Option | Allows |
| --- | --- |
| `--allow-update=role_members` | Adding members to the space's roles |
| `--allow-update=permissions,notifications,issue_security` | Adding grants (including to every user), recipients, or security level members |
| `--allow-update=filters` | Adding viewers or editors to a filter the tool made |
| `--allow-delete=workflows` | A workflow losing a status or transition |
| `--allow-delete=contexts` | The space's own context losing an option or issue type, or being deleted |
| `--allow-delete=issue_types,fields,priorities` | The space losing an issue type, field or priority |
| `--allow-delete=permissions,notifications,issue_security` | Taking away grants, recipients, or security levels and members |
| `--allow-delete=filters` | Taking viewers or editors away from a filter the tool made |
| `--check-usage=permission_scheme` | Finding which spaces use a permission scheme, one request per space (Jira has no lookup). Needed to change one; needs Administer Jira |

## instructions

Writes the steps to set the spec's space up by hand, as Markdown: a section
per kind of object, in the order `apply` works, each with where in Jira it is
done and a table of values. Offline, for sites the tool cannot reach.

| Option | Meaning |
| --- | --- |
| `-o`, `--output FILE` | Write to FILE instead of standard output |
| `--force` | Overwrite FILE if it exists |

```console
$ jira-space-config instructions spec.yaml -o setup.md
```

What the spec reuses is only named; what it ensures is marked "Create if
missing"; the rest is to be created. A spec with errors is refused, except
for objects only Jira's API cannot create (a work item type above Epic, an
app's field type), which are listed to be created by hand.

## A typical session

```console
$ export JIRA_EMAIL=you@example.com JIRA_API_TOKEN=...
$ jira-space-config whoami   --site https://example.atlassian.net --space DEMO
$ jira-space-config export   --site https://example.atlassian.net --space DEMO -o before.yaml
$ jira-space-config validate spec.yaml
$ jira-space-config apply --dry-run --site https://example.atlassian.net --space DEMO spec.yaml
$ jira-space-config apply           --site https://example.atlassian.net --space DEMO spec.yaml -o after.yaml
$ jira-space-config apply --dry-run --site https://example.atlassian.net --space DEMO after.yaml   # nothing to do
```
