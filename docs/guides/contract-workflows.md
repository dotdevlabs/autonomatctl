# Contract workflows

Commands and flags are generated from the embedded Autonomat contract. Use
`autonomatctl schema` to inspect its version and source commit, and
`autonomatctl ai --json` for the current command reference, required flags,
enums and nested JSON schemas. The synchronized contract is version 11.2.0.

## Create a product document

With a run token granted access to the product and operation:

```sh
autonomatctl products documents create PRODUCT_ID --kind lrd --body '# LRD: Initial draft' --summary 'Clarify scope' --json
```

This creates a document and proposes its first revision through
`POST /products/{product_id}/documents`. `--kind` and `--body` are required;
`--summary` is optional. Supported kinds are `icp`, `prd`, `drd`, `trd`, `mrd`,
`rrd`, `ord` and `lrd`. The request uses the `document-revisions` resource type.
An account API token cannot perform this operation. To propose another revision
of an existing document, use `products documents revisions create PRODUCT_ID DOCUMENT_ID`.

## Decision options

The `--options` flag on `inbox-items create` and `inbox-items update` accepts a
JSON array. Each option requires `key`, `label` and `consequence`, and can also
include a nested `call` and `value_request`:

```sh
autonomatctl inbox-items update INBOX_ITEM_ID --options '[{"key":"deploy","label":"Deploy","consequence":"Deploy the approved revision","call":{"capability":"deploy","arguments":{"subcommand":"deploy"},"reason":"Ship the approved revision"},"value_request":{"kind":"input","label":"Release name","type":"text","required":true}},{"key":"wait","label":"Wait","consequence":"Defer deployment"}]'
```

A call requires `capability`, `arguments` and `reason`; CLI capability arguments
include `subcommand`. The server checks the originating run and capability
authorization. A supplied value is stored on the resolution and does not rewrite
the call arguments. Both nested fields may be omitted. Use credential screens
for secrets; value requests must not ask for secrets as input.

Creating an inbox item also requires `--automation_assessment` and `--category`,
plus the fields required for that category. Inspect `ai --json` for the assessment
schema and the command's `required_by_category` annotation.

Review and information requests require a `value_request` with `kind: input`
and `required: true`; use `type: long_text` for content by default. The
`--content_intent` flag accepts `brief`, `review`, `facts`, `edits` or `reasons`
and requires content input in any category. When decision choices request
content, each option needs its own required input. A `place` value request
does not satisfy a request for content.

## Withdraw an erroneous Inbox request

```sh
autonomatctl inbox-items withdrawal create INBOX_ITEM_ID --reason 'Erroneous request' --evidence 'Existing implementation is tracked' --json
```

Both `--reason` and `--evidence` must be nonblank. The run needs an explicit
operation grant for `withdrawInboxItem`, an enabled role and access to the
item's node; assessment runs cannot withdraw requests. Withdrawal preserves
an immutable audit and executes no resolution, saved call or continuation.
An identical retry by the same actor returns the existing withdrawal;
different retries and requests that are no longer pending fail validation.

## Pending calls

```sh
autonomatctl pending-calls create --capability deploy --arguments '{"subcommand":"deploy"}' --reason 'Ship the approved revision' --title 'Deploy release' --replaces INBOX_ITEM_ID
```

`--title` supplies a nonblank, single-line title when operation metadata is
unavailable. The CLI permits omission; the server returns 422 if it needs a
title. Metadata-derived titles take precedence.

`--replaces` is optional and identifies a pending approval call's inbox item.
The server requires the same account and work item, or the same execution when
neither item has a work item. Replacement atomically creates a fresh pending
item and completes the old one, preserving its call and history and linking the
replacement. Neither call is dispatched by this operation. Invalid, resolved
or differently owned references return 422. Omit `--replaces` for ordinary
submission with deduplication.

## Execution usage and credential changes

Execution creation accepts `--usage` as a JSON object, including nested metrics.
Execution detail responses expose the latest stored run-level `usage` object,
or an empty object when none has been stored. Use `--json` to retain the server's
full response. `usage_notional` represents subscription usage valued at API
prices, separately from actual cost.

The contract replaces the `connections` commands with `model-credentials`
commands. Requests that previously used `connection_id` now use
`model_credential_id`; account updates use `default_model_credential_id`.
Inspect the generated reference for credential fields and the new
`model-api-prices` commands before migrating scripts.

## Plain JSON request bodies

For operations documented with a plain `application/json` request object,
`--body` accepts that entire object. The CLI sends it directly with the
documented content type, without a JSON:API envelope. Its schema, including
`oneOf` alternatives, is exposed in `ai --json`. Other operations continue to
use their generated attribute flags; for example, document creation's `--body`
is a string attribute inside a JSON:API request.
