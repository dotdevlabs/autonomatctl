# autonomatctl

CLI for Autonomat, built on [ctlkit](https://github.com/dotdevlabs/ctlkit). One static Go binary. Every
operation of Autonomat's published OpenAPI contract is a subcommand, **generated from the contract at
startup** rather than mapped by hand.

## Install

Download a release from GitHub Releases (linux, darwin, windows; amd64, arm64; `checksums.txt` included),
or `brew install dotdevlabs/tap/autonomatctl`.

## Use

```sh
autonomatctl auth login --name prod --url https://autonomat.example --token <token> --set-current
autonomatctl work-items list --q 'status:open'
autonomatctl work-items create --item_type report --title "Review" --source_kind human --status open
autonomatctl work-items executions create 42 --state pending --json
autonomatctl ai            # ctlkit command reference for agents (markdown); `ai --json` for the structured reference
autonomatctl schema        # pinned contract version and source commit SHA
```

Commands are `<resource> [<sub-resource>] <verb>`: `list`, `get`, `create`, `update`, `replace`, `delete`, or
an action named after the path (`products sunset <id>`). Path parameters are positional arguments. Flags carry
the contract's exact field names (`--item_type`, not `--item-type`) and query parameter names. Only documented
fields can be sent: there is no free-form body. Required attributes are enforced before any request.
Destructive operations (`delete`, `products sunset`) need `--yes`. Operations that authenticate with the
webhook token are hidden from `--help` and omitted from the `ai` reference, but remain callable.
Every generated command carries `operation_id`, `method` and `path` cobra annotations, which `ai --json` exposes
under each command's `annotations`; required request fields are required flags (`required: true` in the reference).

### Output and exit codes

- `--json` prints the raw JSON:API document exactly as the server sent it. On failure it prints the
  server's `errors` document to stdout.
- Without `--json`, output is readable text; failures print every `errors` entry to stderr.
- List commands return one page. `--all` follows `links.next` verbatim (the CLI never builds page URLs, and
  never sends the token to another origin) and merges the pages into one document.
- `--dry-run` prints the request (token redacted) and sends nothing. `--verbose` logs method, URL and status.

| Exit | Meaning |
|-----:|---------|
| 0 | success |
| 1 | not found (HTTP 404, unknown context) |
| 2 | unauthorized (HTTP 401, no token) |
| 3 | forbidden (HTTP 403) |
| 4 | other HTTP 4xx, e.g. 422 validation errors |
| 5 | conflict (HTTP 409) |
| 6 | HTTP 5xx or transport failure |
| 8 | usage error: unknown flag or command, missing required flag, bad value |

### Contexts and environment

Named contexts (base URL and token) live in `~/.config/atmt/autonomatctl.yaml`; pick one with `--context`.

| Variable | Effect |
|----------|--------|
| `AUTONOMATCTL_TOKEN` | token, overrides the context's. Agent runs pass their run-scoped token here |
| `AUTONOMATCTL_CONTEXT` | context to use when `--context` is absent |
| `AUTONOMATCTL_BASE_URL` | base URL, overrides the context's. With `AUTONOMATCTL_TOKEN` also set, no context is needed |

## The contract

The authoritative contract is `docs/api_spec.yaml` in the private repository `dotdevlabs/autonomat`. A pinned
copy is kept at `internal/schema/testdata/api_spec.yaml`, with the commit it came from in
`internal/schema/testdata/api_spec.sha`; both are embedded in the binary.

```sh
GITHUB_TOKEN=<token> scripts/sync_spec.sh           # update the pinned copy and SHA
GITHUB_TOKEN=<token> scripts/sync_spec.sh --check   # fail if the pinned copy is stale
```

The token (or a logged-in `gh`) needs read access to `dotdevlabs/autonomat`. Without one, `--check` fails;
`AUTONOMATCTL_ALLOW_SPEC_SKIP=1` skips it explicitly. After a sync, rebuild: an operation added to the contract
appears as a command with no hand-written code. Generation fails loudly if two operations map to the same
command, a query parameter collides with a CLI flag, or a name collides with a built-in command.

A body field named like a reserved CLI flag (the inbox part `context` vs the global `--context`) is exposed as
`--attr-<name>` (`--attr-context`) and still sent under its contract name.

Relationship operations whose body is a list of resource identifiers take a repeatable `--ids` flag; the
command sends each id as `{type, id}` in `data`. The loader tolerates duplicate mapping keys in the contract
(the last definition wins) but rejects them under `paths`.

## Development

```sh
bin/ci    # gofmt, spec check, go vet, golangci-lint, go test -race with an 80% coverage gate, go build
```

The Go module `github.com/dotdevlabs/ctlkit` is private: set `GOPRIVATE=github.com/dotdevlabs/*`. CI needs the
repository secret `SPEC_READ_TOKEN` (read access to `dotdevlabs/ctlkit` and `dotdevlabs/autonomat`).

`internal/gen`'s conformance test builds a request for every operation of the pinned contract through its
generated command and compares method, path, query names and body fields with the contract.

## Release

Push a tag `vX.Y.Z`; the Release workflow runs goreleaser: static (`CGO_ENABLED=0`) binaries for
linux/darwin/windows × amd64/arm64, `checksums.txt`, a GitHub Release, and the Homebrew formula
(needs `HOMEBREW_TAP_GITHUB_TOKEN`). Check locally with `goreleaser check` and
`goreleaser build --snapshot --clean`.
