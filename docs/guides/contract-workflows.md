# Contract workflows

Commands and flags are generated from the embedded Autonomat contract. Use
`autonomatctl schema` to inspect its version and source commit, and
`autonomatctl ai --json` for the current command reference, required flags,
enums and nested JSON schemas. The synchronized contract is version 0.0.43.

`autonomatctl version` reports the embedded contract's `info.version`, along with
build metadata. Use `autonomatctl version --json` to read the `version` field in
scripts. The CLI and its `ai --json` reference use this same contract version;
these inspection commands work offline.

When moving from v9.0.0 or a historical v0.1.0–v0.1.9 installation, follow
the [reinstall instructions](../release-notice.md) once v0.0.43 is available.
The contract retires the `routing-rules`, `work-trackers` and `schedule-runs`
command groups. Check `autonomatctl ai --json` when updating scripts that used
these groups.

## Submit a run result

When you use `work-items executions create WORK_ITEM_ID --description TEXT`,
the API starts preparation for an unprepared item or selects pending
verification once external watches and deliveries finish. Preparation supplies
the item's outcome definition before work starts.

Use the target run's own token with a grant for `createRunResult`. Read
`data.attributes.kind` from the execution response to identify the run kind.
Submit a nonblank answer while the run is running. For a `verify` run, omit
`--item_outcome` and describe any remaining verification work in the answer.

```sh
AUTONOMATCTL_TOKEN="$RUN_TOKEN" autonomatctl runs execution get "$RUN_ID" --json
AUTONOMATCTL_TOKEN="$RUN_TOKEN" autonomatctl runs result create "$RUN_ID" \
  --answer 'Verification is complete.' --json
```

Both `work` and `prepare` runs require `--item_outcome`. Its `state` reports
execution progress and can be `completed`, `interrupted`, `stopped`, or
`failed`. Use `note` to explain an unsuccessful outcome. The answer and note
cannot contain NUL characters.

```sh
AUTONOMATCTL_TOKEN="$WORK_RUN_TOKEN" autonomatctl runs result create "$WORK_RUN_ID" \
  --answer 'The report could not be generated.' \
  --item_outcome '{"state":"failed","note":"The ledger export was unavailable."}' --json
```

HTTP 204 means the result was accepted or the same result had already been
submitted. The command exits successfully with empty stdout even with
`--json`. The agent service's terminal report ends the run and uses the stored
result. A retry with different content returns HTTP 409.

If the agent service's terminal verification report has neither an accepted
answer nor a valid legacy answer, the item keeps waiting for verification of
the same completed work. Waiting-item recovery retries verification until
technical verification failures for that work reach the effective work-run
limit. If verification ends with a final answer and no verified transition,
the item returns to preparation and the answer becomes its next action. At
the work-run limit, the item fails and retains the answer as its note.

If a Prepare run creates or updates its own still-open operator Inbox question
on the work item, submit an `interrupted` outcome with a note. When the agent
service reports success, the API acknowledges the run as `succeeded` and
retains the interrupted outcome and note. The work item stays unprepared
while it waits for the operator's answer.

```sh
AUTONOMATCTL_TOKEN="$PREPARE_RUN_TOKEN" autonomatctl runs result create "$PREPARE_RUN_ID" \
  --answer 'Waiting for the operator to choose the pilot date.' \
  --item_outcome '{"state":"interrupted","note":"The start date is still open in the Inbox."}' --json
```

## Update product standards

Use an authorized account context to replace a product's ordered additional
standards. The API appends these keys to a nonempty procedure standards list
for the product or its directly linked work items. Additional keys must be
active in the product's account and must be distinct from excluded keys.

```sh
autonomatctl --context "$ACCOUNT_CONTEXT" products update "$PRODUCT_ID" \
  --additional_standard_keys '["deployment-review","release-check"]' \
  --excluded_standard_keys '["legacy-check"]' --json
```

Pass `--additional_standard_keys '[]'` to clear the additional list. Omit the
flag to preserve it. The excluded list has the same replace, clear and omit
behavior.

## Create a product document

With a run token granted access to the product and operation:

```sh
autonomatctl products documents create "$PRODUCT_ID" --kind lrd --body '# LRD: Initial draft' --summary 'Clarify scope' --json
```

`--kind`, `--body` and `--summary` are required. Supported kinds are `icp`,
`prd`, `drd`, `trd`, `mrd`, `rrd`, `ord` and `lrd`. The request uses
the `document-revisions` resource type. An eligible run with write authority
creates the document and commits revision 1 immediately (HTTP 201). When the
operation requires approval, HTTP 202 means the text is waiting in Inbox.
The API creates the document on approval.

## Read and write revisions

Use `architecture-decisions get ID` or `product-documents get ID` to read the
current architecture decision or account strategy text. The revision groups
below provide `list`, `get` and `create`. To write a revision, use a run token
and supply `--body` and `--summary`.

| Record | Revision write command |
| --- | --- |
| Product document | `products documents revisions create PRODUCT_ID DOCUMENT_ID` |
| Initiative | `initiatives revisions create INITIATIVE_ID` |
| Architecture decision | `architecture-decisions revisions create DECISION_ID` |
| Account strategy | `product-documents revisions create DOCUMENT_ID` |
| Standard | `standards revisions create STANDARD_ID` |

HTTP 201 returns the committed current revision. HTTP 202 means the revision
awaits Inbox approval. Approval commits the change.

Use `standards update STANDARD_ID --standard_keys '[]'` to clear a procedure's
ordered standards list. To set the list, pass a JSON array of existing standard
keys from the same account in the desired order. Omit the flag to preserve the
list.

## Save a work item verification check

```sh
autonomatctl work-items update "$WORK_ITEM_ID" \
  --verification_check '{"path":"checks[0].status","expected":"ready"}' --json
```

The check path supports dot-separated keys and bracketed array indices.
`expected` accepts any JSON value. Pass `--verification_check null` to clear the
check, or omit the flag to preserve it.

Save changes to the check in a separate update. To mark the item verified, use
its running verification execution's token and submit the recorded action ID
from a successful granted read after the run's last change. The API compares
the action's structured output at the saved path with the expected value.

```sh
AUTONOMATCTL_TOKEN="$VERIFY_RUN_TOKEN" autonomatctl work-items update "$WORK_ITEM_ID" \
  --status verified --description 'The saved readiness check passed.' \
  --observation_action_id "$ACTION_ID" --json
```

## File and answer an Inbox escalation

Use `--request_type question` for a decision without an automation plan.
Question options omit executable calls. The API defaults to `question` when
the flag is omitted. Work runs may omit `--work_item_id` to use their own item.
Review runs must name an item in their account and node. Pass a numeric ID as
`--work_item_id 42` or preserve a JSON string with `--work_item_id '"42"'`.

```sh
autonomatctl inbox-items create --request_type question --work_item_id 42 \
  --subject 'Which date should the pilot start?' --category decision \
  --request '{"action":"choose","target":"pilot schedule","exact_change":"Set the pilot start date","duration":"one week","values":{"first":"Monday","second":"Wednesday"}}' \
  --escalation_chain "[{\"agent_id\":${ASKING_AGENT_ID},\"reason\":\"The pilot needs a start date\"}]" \
  --evidence 'The team is available both days' \
  --options '[{"key":"monday","label":"Monday","consequence":{"continues":"Start Monday","spent":"$0","not_done":"Wednesday start"}},{"key":"wednesday","label":"Wednesday","consequence":{"continues":"Start Wednesday","spent":"$0","not_done":"Monday start"}}]' \
  --recommendation monday --prepared_result 'Both schedules are drafted' \
  --unblocks 'Publishing the pilot schedule' \
  --money_at_stake '{"amount_minor":0,"currency":"USD"}' --json
```

Set `ASKING_AGENT_ID` in both examples to the authenticated asking agent's
numeric ID. To request permission, supply `--request_type permission` and a
bounded `--automation_plan`. Its `action` must name a request kind configured
for the installation. The request kind defines the allowed target, scope and
bound. The example assumes a `grant` kind that accepts the illustrated
capability scope and counted bound. Set `CAPABILITY_ID` to the capability in
the proposed grant and `PLAN_EXPIRES_AT` to a future RFC 3339 timestamp.

```sh
autonomatctl inbox-items create --request_type permission --work_item_id 42 \
  --subject 'Approve the pilot?' --category decision \
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

For an escalation, the response's `required_parts` lists the top-level
creation fields, including `automation_plan` for permission requests. Supply
the escalation reason in the `escalation_chain` entry. An optional typed value
request belongs inside an option. `missing_parts` lists required fields that
are absent or blank and is empty for a valid escalation. Other Inbox items
use the applicable account or workable permission requirements.

Use `inbox-requirements get` for the account's configured requirements by
category for operator-owned pending items. Read `required_parts` on the
individual Inbox item to determine which fields apply to an escalation or a
workable permission item.

`inbox-items update` uses its own run-scoped schema. It accepts partial updates,
such as `--evidence 'Pilot checks passed'`. Its `--options` takes entries with a
text `consequence`, such as `[{"key":"go","label":"Proceed","consequence":"Run the pilot"}]`.
The update command's `required_by_category` metadata describes the default minimum
parts the resulting item must retain.

## Pending calls

```sh
autonomatctl pending-calls create --capability deploy --arguments '{"subcommand":"deploy"}' \
  --reason 'Ship the approved revision' --consequence 'Deploy the release to production' \
  --title 'Deploy release' --replaces_entry "$ENTRY_ID"
```

`--consequence` describes what approval will do. Supply a nonblank, single-line
`--title` for work-bound calls or when operation metadata is unavailable.

Use `--replaces_entry` for an open saved call in the same work item. The API
retires the old entry and appends the replacement atomically, preserving both.
The legacy `--replaces` flag names an Inbox item. For a work-bound call, that
item must identify exactly one open entry in the same work item. For a
standalone call, it must identify an open call from the same execution. Use
`--request_id` to distinguish an intentional repeat of an identical work-bound
call. Resubmitting with the same identity returns the same entry.

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

## Configure capability review and look up notification channels

Use an authorized account context for capability configuration and channel
lookup. Supply `--name` to set the capability's display name. Creation defaults
to the humanized key when the name is omitted or blank. On update, omission
preserves the name and a supplied name must be nonblank.

Set `review_on` under `--watch_configuration` to request immediate product
review. Key each rule by the signal item's external kind and specify either
the triggering states or `"version":true` for version changes.

```sh
autonomatctl --context "$ACCOUNT_CONTEXT" capabilities update "$CAPABILITY_ID" \
  --key "$CAPABILITY_KEY" --name 'Pilot deployments' \
  --watch_configuration '{"levels":["item"],"kinds":{"deployment":"item","release":"item"},"review_on":{"deployment":{"states":["succeeded","failed"]},"release":{"version":true}}}' --json
autonomatctl --context "$ACCOUNT_CONTEXT" notification-channels get "$CHANNEL_ID_OR_KEY" --json
```

The channel lookup returns an active channel in the account by ID or key.
Read `data.attributes.name` for the display name. The API uses the humanized
key if the saved name is blank. Run tokens receive HTTP 403.

In responses from `activity-events list` and `activity-events get`, the API
adds the associated tool or channel display name to the activity `summary`
when the name is available and absent from the recorded text.

## Watch a signal source

Use an operator context to configure signal sources and manage watches. This
example links a watch to an Inbox external action and an existing capability.
Set the source kind and tool for your provider. Use `kind:id` for external IDs.
The text after the first colon is case-sensitive. Each exact event type has
its own mapping, with a required `item` and optional `container` and `account`
kind and field pairs. A mapping's optional `require` array applies to webhook
and reconciled messages. Every condition must find an exact string match at
one or more of its `paths`. Use `[*]` to visit every element of an array.

When creating a signal directly with `signals create`, use an account token
to set external subject fields. Supply typed IDs in `--subject_external_id`,
`--subject_container_external_id`, and `--subject_account_external_id` as
applicable. For webhook delivery, send the provider's original IDs and let
the source mapping supply the kinds.

```sh
autonomatctl signal-sources create --name 'Deployment events' --kind "$SOURCE_KIND" \
  --webhook_mode bearer \
  --settings '{"tool":"deploy","event_type_field":"type","message_mapping":{"deployment.updated":{"item":{"kind":"deployment","field":"deployment_id"},"container":{"kind":"project","field":"project_id"},"state":"status","version":"version","require":[{"paths":["environment","deployment.environment"],"equals":"pilot"}]}}}' --json
autonomatctl signal-sources get "$SIGNAL_SOURCE_ID" --json
autonomatctl signal-sources update "$SIGNAL_SOURCE_ID" --summary 'Deployment events for the pilot'
autonomatctl watches create --owner_type InboxItem --owner_id "$INBOX_ITEM_ID" \
  --capability_id "$CAPABILITY_ID" --signal_source_id "$SIGNAL_SOURCE_ID" \
  --tool deploy --external_id 'deployment:Deploy-42' --condition '{"satisfying_states":["succeeded"],"failing_states":["failed"]}'
autonomatctl watches list --json
autonomatctl watches get "$WATCH_ID" --json
autonomatctl watches cancellation create "$WATCH_ID" --description 'Stop watching because the deployment was superseded'
autonomatctl signals list --q 'owner:"product:42" tool:deploy external_id:"deployment:Deploy-42"' --json
```

The delivery command is callable directly. A bearer source uses its webhook
token and receives the provider message in `data.attributes`.

```sh
AUTONOMATCTL_TOKEN="$WEBHOOK_TOKEN" autonomatctl webhooks signal-sources signals create "$SIGNAL_SOURCE_ID" \
  --body '{"id":"event-42","type":"deployment.updated","deployment_id":"Deploy-42","project_id":"Project-7","status":"succeeded","version":1,"environment":"pilot"}' --json
```

A signed source receives a plain JSON object. Include a nonblank string or an
integer in the configured top-level event ID field. Set `PROVIDER_JSON` to the
same event shape shown above for a signed source with the same mapping. Compute
the HMAC over the exact bytes in `PROVIDER_JSON` using the configured digest (SHA256,
SHA384 or SHA512). Encode the HMAC as lowercase hex, prepend any configured
literal prefix, and pass the result through `AUTONOMATCTL_SIGNAL_SIGNATURE`.
Set `--signature-header` to the source's configured header. The CLI sends the
original bytes as `application/json` and redacts the signature in dry-run output.
Select the destination using the usual context or `AUTONOMATCTL_BASE_URL`.
Signed delivery omits the `Authorization` header. For bearer delivery, leave
both `AUTONOMATCTL_SIGNAL_SIGNATURE` and `--signature-header` unset.

If the payload fails any mapping condition, the API filters the delivery and
returns HTTP 204. The command exits successfully with empty stdout, including
with `--json`.

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
