# Working on jira-space-config

A Go CLI that manages one existing company-managed Jira Cloud space from a
YAML spec. What it does for users is in [README.md](README.md),
[docs/cmdline.md](docs/cmdline.md) and [docs/spec-format.md](docs/spec-format.md);
read those before changing behaviour. This file is the map and the rules.

## Map

| Where | What |
| --- | --- |
| `cmd/jira-space-config` | Entry point |
| `internal/cli` | Commands and flags; no logic |
| `internal/spec` | The format: model, strict decoder, `Validate`, canonical writer, the typed workflow rule registry (`rules.go`) and holder kinds (`holders.go`) |
| `internal/jira` | HTTP client. Read-only by construction (a guard allows `GET` and a listed set of read-only `POST`s); `Writer` sends only the writes a step planned |
| `internal/reader` | Reads a site into spec values, one reader per kind of object. Export and apply share it, so apply compares with exactly what export writes |
| `internal/export` | Calls the readers and assembles a spec |
| `internal/steps` | Apply's 28 steps, in the fixed order of `steps.All`: evaluation (`*_steps.go`) and writes (`*_writes.go`) |
| `internal/apply` | Runs the steps: dry run, live run, prompts, lock, audit log, rendering |
| `internal/instructions` | A spec as Markdown steps for setting a space up by hand |
| `internal/whoami`, `internal/credentials` | Account and permission report; the redacted token |
| `internal/apiinventory` | Test only: checks `docs/api/inventory.md` against Atlassian's OpenAPI specs |
| `cmd/jsc-*` | Development tools: `jsc-checkcover` (the gate below), `jsc-record` and `jsc-scrub` (fixtures, see [docs/api/README.md](docs/api/README.md)), `jsc-sandbox` (sandbox chores) |
| `*/testdata/` | Scrubbed recordings of real Jira responses (`routes.json` manifests) and golden outputs |
| [docs/api/inventory.md](docs/api/inventory.md) | Every endpoint used, and the Jira behaviours the code depends on |
| [docs/api/export-coverage.md](docs/api/export-coverage.md) | What export does with every field Jira returns |

## What must stay true

- **Conservative.** Create what is missing; compare what exists. Change only
  objects the tool made (the `[managed-by: jira-space-config]` marker in
  their description) and only after a sharing check finds no other space
  uses them. Never change site-wide or shared objects; never create or
  delete spaces.
- **Writes are guarded at the lowest level.** `jira.Writer` is the only
  way to change Jira: it sends a write only if the item being carried out
  planned its method and path template, and refuses one that names another
  space in its path or body. The template does not bind object IDs, so
  which object an item may change is decided by its step (the sharing
  check, just before the write); keep that check in the step.
- **Safe changes only.** A change that widens access or removes something
  needs its `--allow-update` / `--allow-delete` kind; one that would affect
  work items is blocked with a count.
- **Round trip.** An export, dry-run against the same space, finds nothing to
  do; after an apply, a dry run of the same spec finds nothing to do.
  Exporting twice gives the same bytes.
- **Nothing is skipped silently.** What the plan, the API or the format
  cannot do is an error, a `notes:` entry or a `manual` item, never quietly
  left out. A spec the Free plan cannot satisfy fails.
- **Credentials only from the environment** (`JIRA_EMAIL`, `JIRA_API_TOKEN`);
  `--site` and `--space` always explicit; the token never printed, logged or
  written.

## Conventions

- **Tests never touch a real Jira.** Use recordings (`testdata/`) and
  synthetic routes. Golden files: `go test ./internal/<pkg> -update`, then
  review the diff.
- **Every check is made to fire by a test**, or carries
  `// checkcover:unreachable <why>`. A check is a diagnostic, an error the
  code creates, an export note, a panic, or an item of kind Error, Differs,
  Blocked or Manual. `mise run checkcover` fails otherwise, and also on a
  marker that tests do reach.
- **`mise run check` passes before every commit** (tests, lint, cross-build,
  checkcover). Tools are pinned in `.mise.toml`.
- **Docs follow the code**: a format change updates `docs/spec-format.md`
  (and its limits section); a command or option change, `docs/cmdline.md`; a
  new endpoint, `docs/api/inventory.md` (tests check it); a Jira behaviour the
  code relies on, the inventory's behaviours list. Docs are for users: terse,
  current, no history or plans.
- **Messages say what the tool does now**, what failed and how to fix it.
  No "not supported yet".
- **Australian English** (`colour`, `licence`, `organisation`); Jira's own
  names keep their spelling. Match the surrounding code's naming and comment
  density; keep files small, split by kind of object.
- Commit subjects are short and plain, often `area: what changed`.

## Releasing

Push a tag `vX.Y.Z` (`vX.Y.Z-rc.N` for a pre-release) on `main`.
`.github/workflows/release.yml` checks the code as CI does, then GoReleaser
(`.goreleaser.yaml`) builds every target and publishes a GitHub release with
checksums and build provenance. `mise run release-check` builds the same
release into `dist/` without publishing it. Workflow actions are pinned by
commit; Dependabot proposes updates.

## Live calls

The tool and the development tools need these in the environment:

| Variable | Value |
| --- | --- |
| `JIRA_EMAIL` | The Atlassian account's email address |
| `JIRA_API_TOKEN` | Its API token; never print it |
| `JIRA_SITE` | The site URL, by convention, passed as `--site "$JIRA_SITE"` (the tool never reads the site from the environment) |

Before any live call, say which site and which requests. Write only to a
sandbox site that holds no real data. In shell, never `rm` a path built from a variable that could be
empty.
