# Contract workflows

Commands and flags are generated from the embedded Autonomat contract. Use
`autonomatctl schema` to inspect its version and source commit, and
`autonomatctl ai --json` for the current command reference, required flags,
enums and nested JSON schemas. The synchronized contract is version 0.0.19.

`autonomatctl version` reports the embedded contract's `info.version`, along with
build metadata. Use `autonomatctl version --json` to read the `version` field in
scripts. The CLI and its `ai --json` reference use this same contract version;
these inspection commands work offline.

When moving from v9.0.0 or a historical v0.1.0–v0.1.9 installation, follow
the [reinstall instructions](../release-notice.md) once v0.0.19 is available.
The contract retires the `routing-rules`, `work-trackers` and `schedule-runs`
command groups. Check `autonomatctl ai --json` when updating scripts that used
these groups.

## Create a product document

With a run token granted access to the product and operation:

```sh
autonomatctl products documents create "$PRODUCT_ID" --kind lrd --body '# LRD: Initial draft' --summary 'Clarify scope' --json
```

This creates a document and proposes its first revision through
`POST /products/{product_id}/documents`. `--kind` and `--body` are required;
`--summary` is optional. Supported kinds are `icp`, `prd`, `drd`, `trd`, `mrd`,
`rrd`, `ord` and `lrd`. The request uses the `document-revisions` resource type.
An account API token cannot perform this operation. To propose another revision
of an existing document, use `products documents revisions create PRODUCT_ID DOCUMENT_ID`.

## File and answer an inbox escalation

An asking agent files an escalation to its manager. Set `ASKING_AGENT_ID` to the
authenticated agent's numeric ID, `CAPABILITY_ID` to the ID of the capability in
the proposed grant, and `PLAN_EXPIRES_AT` to a future RFC 3339 timestamp. The
create command uses `--automation_plan` for the proposed change. Remove the
retired `--automation_assessment` flag from create scripts.

```sh
autonomatctl inbox-items create --subject 'Approve the pilot?' --category decision \
  --request '{"action":"enable","target":"pilot","exact_change":"Enable one test project","duration":"one day","values":{"enabled":true}}' \
  --escalation_chain "[{\"agent_id\":${ASKING_AGENT_ID},\"reason\":\"The pilot exceeds my approval limit\"}]" \
  --evidence 'The test suite passed against the pilot configuration' \
  --options '[{"key":"go","label":"Proceed","consequence":{"continues":"Run the pilot","spent":"$10","not_done":"Full rollout"}},{"key":"wait","label":"Wait","consequence":{"continues":"Review next week","spent":"$0","not_done":"Pilot"}}]' \
  --recommendation go --prepared_result 'The test configuration is ready' \
  --unblocks 'Limited rollout to the test project' \
  --money_at_stake '{"amount_minor":1000,"currency":"USD"}' \
  --automation_plan "{\"action\":\"grant\",\"target\":\"Capability:${CAPABILITY_ID}\",\"summary\":\"Permit the next pilot run\",\"exact_change\":{\"scope\":{\"project\":\"pilot\"},\"uses\":1,\"expires_at\":\"${PLAN_EXPIRES_AT}\"}}" \
  --json
```

Choose the action that applies to the pending item. The authorized recipient
can answer or escalate to their manager. An authorized run can withdraw an
erroneous item with a description, reason and evidence.

```sh
autonomatctl inbox-items answer create "$INBOX_ITEM_ID" --option go --note 'Approve the one-day pilot' --plan_choice approve_only
autonomatctl inbox-items escalation create "$INBOX_ITEM_ID" --reason 'The pilot exceeds my delegated budget'
autonomatctl inbox-items withdrawal create "$INBOX_ITEM_ID" --description 'Withdraw the duplicate request and continue with the original' --reason 'Duplicate request' --evidence 'The original request is already pending'
```

`inbox-items update` uses its own run-scoped schema. It accepts partial updates,
such as `--evidence 'Pilot checks passed'`. Its `--options` takes entries with a
text `consequence`, such as `[{"key":"go","label":"Proceed","consequence":"Run the pilot"}]`.
The update command's `required_by_category` metadata describes the default minimum
parts the resulting item must retain.

## Pending calls

```sh
autonomatctl pending-calls create --capability deploy --arguments '{"subcommand":"deploy"}' --reason 'Ship the approved revision' --title 'Deploy release' --replaces "$INBOX_ITEM_ID"
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

Execution creation takes a lifecycle description. Admit a queued run with an
account API token. Supply `--retry_count` when reporting the native retry count.
The admission request has an empty body. With the run's own authorized token,
`runs execution get` reads the admitted execution.

```sh
autonomatctl work-items executions create "$WORK_ITEM_ID" --description 'Queue the execution after reviewing the work item' --json
autonomatctl runs create-run-execution "$RUN_ID" --retry_count 0 --json
autonomatctl executions get "$EXECUTION_ID" --json
```

Execution detail includes the stored run-level `usage` object. Use `--json` to
retain the full response. `usage_notional` represents subscription usage valued
at API prices, separately from actual cost.

The contract replaces the `connections` commands with `model-credentials`
commands. Requests that previously used `connection_id` now use
`model_credential_id`; account updates use `default_model_credential_id`.
Inspect the generated reference for credential fields and the new
`model-api-prices` commands before migrating scripts.

## Watch a signal source

Use an operator context to configure signal sources and manage watches. This
example links a watch to an Inbox external action and an existing capability.
Set the source kind and external identifiers for your provider.

```sh
autonomatctl signal-sources create --name 'Deployment events' --kind "$SOURCE_KIND" --webhook_mode bearer --json
autonomatctl signal-sources get "$SIGNAL_SOURCE_ID" --json
autonomatctl signal-sources update "$SIGNAL_SOURCE_ID" --summary 'Deployment events for the pilot'
autonomatctl watches create --owner_type InboxItem --owner_id "$INBOX_ITEM_ID" \
  --capability_id "$CAPABILITY_ID" --signal_source_id "$SIGNAL_SOURCE_ID" \
  --tool "$SOURCE_TOOL" --external_id "$EXTERNAL_ID" --condition '{"satisfying_states":["succeeded"],"failing_states":["failed"]}'
autonomatctl watches list --json
autonomatctl watches get "$WATCH_ID" --json
autonomatctl watches cancellation create "$WATCH_ID" --description 'Stop watching because the deployment was superseded'
```

The delivery command is callable directly. A bearer source uses its webhook
token and receives the provider message in `data.attributes`.

```sh
AUTONOMATCTL_TOKEN="$WEBHOOK_TOKEN" autonomatctl webhooks signal-sources signals create "$SIGNAL_SOURCE_ID" \
  --body '{"id":"event-42","status":"succeeded"}' --json
```

A signed source receives a plain JSON object. Include a nonblank string or an
integer in the configured top-level event ID field. Compute the HMAC over the
exact bytes in `PROVIDER_JSON` using the source's configured digest (SHA256,
SHA384 or SHA512). Encode the HMAC as lowercase hex, prepend any configured
literal prefix, and pass the result through `AUTONOMATCTL_SIGNAL_SIGNATURE`.
Set `--signature-header` to the source's configured header. The CLI sends the
original bytes as `application/json` and redacts the signature in dry-run output.
Select the destination using the usual context or `AUTONOMATCTL_BASE_URL`.
Signed delivery omits the `Authorization` header. For bearer delivery, leave
both `AUTONOMATCTL_SIGNAL_SIGNATURE` and `--signature-header` unset.

```sh
AUTONOMATCTL_SIGNAL_SIGNATURE="$PROVIDER_SIGNATURE" autonomatctl webhooks signal-sources signals create "$SIGNAL_SOURCE_ID" \
  --signature-header "$SOURCE_SIGNATURE_HEADER" --body "$PROVIDER_JSON" --json
```

## Plain JSON request bodies

For operations documented with a plain `application/json` request object,
`--body` accepts that entire object. The CLI sends it directly with the
documented content type, without a JSON:API envelope. Its schema, including
`oneOf` alternatives, is exposed in `ai --json`. Other operations continue to
use their generated attribute flags; for example, document creation's `--body`
is a string attribute inside a JSON:API request.
