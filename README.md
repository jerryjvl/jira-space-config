# jira-space-config

Describe one existing **company-managed Jira Cloud space** in a YAML file
(work types, fields, screens, workflows, permissions, filters, automation)
and make the space match it, cautiously:

- It **creates only what is missing**, in dependency order, one confirmed
  step at a time.
- What exists is **compared**. A difference is reported, not overwritten.
- Site-wide objects (statuses, custom fields, …) and anything another space
  uses are **never changed**. Objects the tool made are changed only while
  this space alone uses them.
- A change that would affect existing work items is **blocked**, with a count
  and what to do.
- Running it again is always safe. It never creates or deletes spaces.

## Install

Download the binary for your platform from the
[releases page](https://github.com/jerryjvl/jira-space-config/releases). It
is a single file with no dependencies.

## Credentials

```console
$ export JIRA_EMAIL=you@example.com
$ export JIRA_API_TOKEN=...      # https://id.atlassian.com/manage-profile/security/api-tokens
```

Credentials come only from these variables, and the token is never printed
or logged. The site is always given explicitly with `--site`. The account
needs Administer Jira for site-wide objects and schemes, and Browse projects
in the space.

## Commands

| Command | Does |
| --- | --- |
| `whoami --site URL [--space KEY]` | Checks the credentials and the permissions the tool needs |
| `validate spec.yaml` | Checks a spec offline, with `file:line:col` messages |
| `export --site URL --space KEY [-o file.yaml]` | Writes a space as a spec (read-only) |
| `apply --dry-run --site URL --space KEY spec.yaml` | Shows what applying would do (read-only) |
| `apply --site URL --space KEY spec.yaml` | Makes the space match, one confirmed step at a time |
| `instructions spec.yaml [-o setup.md]` | Writes the set-up as Markdown steps to follow by hand (offline) |

The usual loop: `export` the space as a starting point, edit the spec,
`validate`, `apply --dry-run`, then `apply`. Afterwards, a dry run of the same
spec finds nothing to do.

All options, the prompts and protections of a live run, and the
`--allow-update` / `--allow-delete` overrides are in
[docs/cmdline.md](docs/cmdline.md).

## The spec

A spec says what the space should have; Jira's schemes are worked out by the
tool. Objects are named, not numbered.

```yaml
apiVersion: jira-space-config/v1alpha1
kind: SpaceConfig
metadata:
  name: demo

statuses:
  - { name: Open, mode: reuse }                                # must already exist
  - { name: In Review, mode: ensure, category: in_progress }   # created if missing
  - { name: Done, mode: reuse }

screens:
  - name: DEMO Request                                         # made by the tool
    tabs:
      - { name: Field Tab, fields: [Summary, Description] }

workflows:
  - name: DEMO Request Flow
    statuses: [Open, In Review, Done]
    transitions:
      - { name: Create, type: initial, to: Open }
      - { name: Review, from: [Open], to: In Review }
      - { name: Finish, from: [In Review], to: Done }

space:
  key: DEMO
  managed_prefix: DEMO            # names of what the tool makes start with this
  issue_types:
    - { name: Task, default: true, workflow: DEMO Request Flow, screens: DEMO Request }
  fields:                         # the space's complete field list
    - { name: Summary, required: true }
    - { name: Description }
```

Every object has a mode:

| Mode | For | Meaning |
| --- | --- | --- |
| `reuse` | anything | Must already exist; never created or changed |
| `ensure` | site-wide objects | Created if missing; otherwise compared |
| `managed` | screens, workflows, schemes, filters, boards, automation (default) | Made by the tool for this space; changed while only this space uses it |

`export` and `apply` also write each object's ID. An ID is a cross-check: if
the object was renamed or deleted in Jira, the tool stops instead of creating
another. A spec with IDs is bound to its site.

The full format, with what is compared, what is refused and the tool's
limits, is in [docs/spec-format.md](docs/spec-format.md); automation rules
are in [docs/automation.md](docs/automation.md).

## Licence

MIT; see [LICENSE](LICENSE).
