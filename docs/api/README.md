# Jira API dependency

This directory is the authoritative record of the Atlassian APIs the tool
depends on.

- [inventory.md](inventory.md): every endpoint we use, per object type, with
  its stability and the permission it needs.
- [openapi/](openapi/): the published OpenAPI specifications the inventory
  was checked against. These are vendored copies; do not edit them.
- [export-coverage.md](export-coverage.md): every field Jira returns to
  `export` and whether it is captured, deliberately dropped, or a known gap;
  enforced by a test against the recorded responses.

| File | Source | `info.version` | Retrieved |
| --- | --- | --- | --- |
| `openapi/jira-platform-v3.json` | <https://developer.atlassian.com/cloud/jira/platform/swagger-v3.v3.json> | `1001.0.0-SNAPSHOT-ea70632e…` | 2026-10-13 |
| `openapi/jira-software-agile.json` | <https://developer.atlassian.com/cloud/jira/software/swagger.v3.json> | `1001.0.0` | 2026-10-13 |
| `openapi/automation.json` | <https://developer.atlassian.com/cloud/automation/swagger.v3.json> | `1.1` | 2026-10-13 |

## Recording fixtures

Test fixtures for `export` are real responses, recorded from a sandbox site
with the development tool `cmd/jsc-record`. It runs `export` through the
same read-only client and guard, and saves every response with a manifest
(`routes.json`) of the request that produced it; tests load the manifest as
their fixture routes. Record into a new directory, scrub, then move it into
`internal/export/testdata/<name>/`:

```sh
go run ./cmd/jsc-record --site "$JIRA_SITE" --space KEY --out DIR [--dry-run]
go run ./cmd/jsc-scrub --site "$JIRA_SITE" --accounts ACCOUNTMAP DIR
```

With `--dry-run` the recording also holds every response a dry run of the
export reads, and the export itself as `spec.yaml`; `internal/apply/testdata/`
holds such recordings. `jsc-scrub` does the scrubbing below, in place, and
keeps the account mapping in a file outside the repository, so every
recording uses the same fake for the same account.

The recording holds the site's real data. Before it goes into the
repository, replace the site's host and bare name, the cloud ID, every
account ID (each with its own numbered fake), display names, email
addresses and user avatars; `TestFixturesAreScrubbed` checks the result for
leftovers it can recognise.

## Refreshing

Atlassian updates these specs continuously. To refresh, download them again
to the same filenames, update the table above, and run `mise run test`. The test
in `internal/apiinventory` checks that every endpoint named in an `inventory.md` table still
exists in the vendored spec with the same method, and flags any that have
become deprecated.

Useful queries:

```sh
# Every operation: method, path, deprecated flag, tag, summary
jq -r '.paths|to_entries[]|.key as $p|.value|to_entries[]
  |select(.key|test("get|post|put|delete"))
  |"\(.key|ascii_upcase)\t\($p)\t\(if .value.deprecated then "DEPRECATED" else "" end)\t\(.value.tags[0])\t\(.value.summary)"' \
  docs/api/openapi/jira-platform-v3.json

# Full description of one operation
jq -r '.paths["/rest/api/3/workflows/capabilities"].get.description' docs/api/openapi/jira-platform-v3.json
```
