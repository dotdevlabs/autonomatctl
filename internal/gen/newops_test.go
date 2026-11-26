package gen

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dotdevlabs/autonomatctl/internal/spec"
)

// newOps are selected operations from the current contract. Each row pins the
// command, the request line and the exact attribute names the contract uses.
var newOps = []struct {
	id       string
	command  string
	method   string
	path     string
	dataType string
	required []string // flags that must be enforced
	attrs    []string // exact attribute (flag) names
	pathArgs []string // positional path parameters, in order
}{
	{"getAccount", "account get", "GET", "/account", "", nil, nil, nil},
	{"updateAccount", "account update", "PATCH", "/account", "", nil, []string{"name", "ceo_agent_id", "cpo_agent_id", "worker_address", "worker_ingest_token", "default_model_credential_id", "review_max_interval_days", "reconcile_settings", "reconcile_model_runs_daily_limit", "work_sync_interval_minutes", "work_item_status_map", "conversation_answer_settings", "work_item_procedure_key", "review_procedure_key", "external_ref_display_rules", "external_refs"}, nil},
	{"getHealthRules", "health-rules get", "GET", "/health_rules", "", nil, nil, nil},
	{"updateHealthRules", "health-rules update", "PATCH", "/health_rules", "", nil, []string{"window_days", "key_metrics", "standard", "state_rules"}, nil},
	{"getSpend", "spend get", "GET", "/spend", "", nil, nil, nil},
	{"getStatus", "status get", "GET", "/status", "", nil, nil, nil},
	{"listWorkItems", "work-items list", "GET", "/work_items", "", nil, nil, nil},
	{"listExecutions", "executions list", "GET", "/executions", "", nil, nil, nil},
	{"createWorkItem", "work-items create", "POST", "/work_items", "work-items", []string{"current_state_evidence", "desired_outcome", "responsible_agent_id", "description", "item_type", "title", "product_id", "workable_type", "workable_id"},
		[]string{"verification_check", "current_state_evidence", "desired_outcome", "acceptance_criteria", "verification", "responsible_agent_id", "description", "item_type", "title", "body", "tags", "source_kind", "source_ref", "status", "reason", "parent_id", "merged_into_id", "reconcile", "depends_on_ids", "model_credential_id", "agent_role_id", "product_id", "workable_type", "workable_id"}, nil},
	{"getWorkItem", "work-items get", "GET", "/work_items/{id}", "", nil, nil, []string{"w1"}},
	{"createExecution", "work-items executions create", "POST", "/work_items/{work_item_id}/executions", "", []string{"description"}, []string{"description"}, []string{"w1"}},
	{"sunsetProduct", "products sunset", "POST", "/products/{product_id}/sunset", "", nil, nil, []string{"p1"}},
	{"updateInboxItem", "inbox-items update", "PATCH", "/inbox_items/{id}", "inbox-items", nil, []string{"content_intent", "requested_operation", "title", "question", "context", "options", "steps", "request", "reason", "unblocks", "evidence", "recommendation", "continuation", "denial_continuation", "cost_of_waiting", "value_request"}, []string{"i1"}},
	{"createInboxItemResolution", "inbox-items resolution create", "POST", "/inbox_items/{inbox_item_id}/resolution", "inbox-item-resolutions", nil, []string{"evidence", "description"}, []string{"i1"}},
	{"createPendingCall", "pending-calls create", "POST", "/pending_calls", "pending-calls", []string{"capability", "arguments", "reason", "consequence"}, []string{"capability", "arguments", "title", "replaces", "replaces_entry", "request_id", "consequence", "reason"}, nil},
	{"dispatchWorkItem", "work-items dispatch create", "POST", "/work_items/{work_item_id}/dispatch", "", []string{"description"}, []string{"description"}, []string{"w1"}},
	{"createWorkItemSteering", "work-items steering create", "POST", "/work_items/{work_item_id}/steering", "steering-instructions", nil, []string{"instruction"}, []string{"w1"}},
	{"createInitiativeSteering", "initiatives steering create", "POST", "/initiatives/{initiative_id}/steering", "steering-instructions", nil, []string{"instruction"}, []string{"n1"}},
	{"createProductDocument", "products documents create", "POST", "/products/{product_id}/documents", "document-revisions", []string{"kind", "body"}, []string{"kind", "body", "summary"}, []string{"p1"}},
	{"createProductDocumentRevision", "products documents revisions create", "POST", "/products/{product_id}/documents/{document_id}/revisions", "document-revisions", nil, []string{"body", "summary"}, []string{"p1", "d1"}},
	{"createStandardRevision", "standards revisions create", "POST", "/standards/{standard_id}/revisions", "document-revisions", nil, []string{"body", "summary"}, []string{"s1"}},
	{"listActivityEvents", "activity-events list", "GET", "/activity_events", "", nil, nil, nil},
	{"getActivityEvent", "activity-events get", "GET", "/activity_events/{id}", "", nil, nil, []string{"a1"}},
	{"listAgentRoleCapabilities", "agent-roles relationships capabilities list", "GET", "/agent_roles/{agent_role_id}/relationships/capabilities", "", nil, nil, []string{"a1"}},
	{"addAgentRoleCapabilities", "agent-roles relationships capabilities create", "POST", "/agent_roles/{agent_role_id}/relationships/capabilities", "capabilities", []string{"ids"}, nil, []string{"a1"}},
	{"removeAgentRoleCapabilities", "agent-roles relationships capabilities delete", "DELETE", "/agent_roles/{agent_role_id}/relationships/capabilities", "capabilities", []string{"ids"}, nil, []string{"a1"}},
	{"listConversations", "conversations list", "GET", "/conversations", "", nil, nil, nil},
	{"createConversation", "conversations create", "POST", "/conversations", "conversations", []string{"title"}, []string{"title", "context_type", "context_id"}, nil},
	{"listConversationMessages", "conversations messages list", "GET", "/conversations/{conversation_id}/messages", "", nil, nil, []string{"a1"}},
	{"createConversationMessage", "conversations messages create", "POST", "/conversations/{conversation_id}/messages", "messages", []string{"body"}, []string{"body"}, []string{"a1"}},
	{"getConversation", "conversations get", "GET", "/conversations/{id}", "", nil, nil, []string{"a1"}},
	{"listInboxItems", "inbox-items list", "GET", "/inbox_items", "", nil, nil, nil},
	{"getInboxRequirements", "inbox-requirements get", "GET", "/inbox_requirements", "", nil, nil, nil},
	{"updateInboxRequirements", "inbox-requirements update", "PATCH", "/inbox_requirements", "", nil, []string{"decision", "approval", "access", "information", "review", "attestation", "external_action"}, nil},
	{"createInboxItem", "inbox-items create", "POST", "/inbox_items", "inbox-items", []string{"subject", "category", "request", "escalation_chain", "evidence", "options", "recommendation", "prepared_result", "unblocks", "money_at_stake"},
		[]string{"request_type", "work_item_id", "subject", "category", "escalation_chain", "options", "request", "unblocks", "evidence", "recommendation", "prepared_result", "money_at_stake", "automation_plan"}, nil},
	{"getInboxItem", "inbox-items get", "GET", "/inbox_items/{id}", "", nil, nil, []string{"a1"}},
	{"listInitiatives", "initiatives list", "GET", "/initiatives", "", nil, nil, nil},
	{"createInitiative", "initiatives create", "POST", "/initiatives", "", []string{"key", "title"}, []string{"external_refs", "responsible_agent_id", "description", "key", "title", "status", "automatic_dispatch_paused", "daily_budget_cents", "started_on", "target_on", "success_criteria", "concluded_on", "product_ids"}, nil},
	{"getInitiative", "initiatives get", "GET", "/initiatives/{id}", "", nil, nil, []string{"a1"}},
	{"updateInitiative", "initiatives update", "PATCH", "/initiatives/{id}", "", nil, []string{"external_refs", "responsible_agent_id", "title", "status", "description", "automatic_dispatch_paused", "daily_budget_cents", "started_on", "target_on", "success_criteria", "concluded_on"}, []string{"a1"}},
	{"archiveInitiative", "initiatives delete", "DELETE", "/initiatives/{id}", "", []string{"description"}, []string{"description"}, []string{"a1"}},
	{"listPolicies", "policies list", "GET", "/policies", "", nil, nil, nil},
	{"createPolicy", "policies create", "POST", "/policies", "policies", nil, []string{"key", "applies_when", "requires", "attempt_limit", "time_limit", "owner_node", "body", "enabled"}, nil},
	{"getPolicy", "policies get", "GET", "/policies/{id}", "", nil, nil, []string{"a1"}},
	{"updatePolicy", "policies update", "PATCH", "/policies/{id}", "policies", nil, []string{"key", "applies_when", "requires", "attempt_limit", "time_limit", "owner_node", "body", "enabled"}, []string{"a1"}},
	{"archivePolicy", "policies delete", "DELETE", "/policies/{id}", "", nil, nil, []string{"a1"}},
	{"listProducts", "products list", "GET", "/products", "", nil, nil, nil},
	{"getProduct", "products get", "GET", "/products/{id}", "", nil, nil, []string{"a1"}},
	{"updateProduct", "products update", "PATCH", "/products/{id}", "products", nil, []string{"owner_agent_id", "name", "purpose", "next_action", "daily_budget", "key_metrics", "excluded_standard_keys", "additional_standard_keys", "external_refs", "parent_id"}, []string{"a1"}},
	{"archiveProduct", "products delete", "DELETE", "/products/{id}", "", nil, nil, []string{"a1"}},
	{"listProductDocuments", "products documents list", "GET", "/products/{product_id}/documents", "", nil, nil, []string{"a1"}},
	{"listDocumentRevisions", "products documents revisions list", "GET", "/products/{product_id}/documents/{document_id}/revisions", "", nil, nil, []string{"a1", "a2"}},
	{"getDocumentRevision", "products documents revisions get", "GET", "/products/{product_id}/documents/{document_id}/revisions/{id}", "", nil, nil, []string{"a1", "a2", "a3"}},
	{"getProductDocument", "products documents get", "GET", "/products/{product_id}/documents/{id}", "", nil, nil, []string{"a1", "a2"}},
	{"listRefinements", "refinements list", "GET", "/refinements", "", nil, nil, nil},
	{"createRefinement", "refinements create", "POST", "/refinements", "", []string{"product_id", "title"}, []string{"responsible_agent_id", "description", "signal_ids", "product_id", "title", "status", "diagnosis", "bounded_scope", "expected_outcome", "execution_path", "outcome", "verification_notes"}, nil},
	{"getRefinement", "refinements get", "GET", "/refinements/{id}", "", nil, nil, []string{"a1"}},
	{"updateRefinement", "refinements update", "PATCH", "/refinements/{id}", "", nil, []string{"responsible_agent_id", "title", "status", "description", "diagnosis", "bounded_scope", "expected_outcome", "execution_path", "outcome", "verification_notes", "signal_ids"}, []string{"a1"}},
	{"archiveRefinement", "refinements delete", "DELETE", "/refinements/{id}", "", nil, nil, []string{"a1"}},
	{"listSchedules", "schedules list", "GET", "/schedules", "", nil, nil, nil},
	{"getSchedule", "schedules get", "GET", "/schedules/{id}", "", nil, nil, []string{"a1"}},
	{"updateSchedule", "schedules update", "PATCH", "/schedules/{id}", "schedules", nil, []string{"key", "subject_type", "subject_id", "procedure", "cron_expression", "timezone", "responsible_agent_id", "description", "state"}, []string{"a1"}},
	{"listSignals", "signals list", "GET", "/signals", "", nil, nil, nil},
	{"createSignal", "signals create", "POST", "/signals", "signals", []string{"kind"}, []string{"origin", "subject_tool", "subject_external_id", "subject_container_external_id", "subject_account_external_id", "subject_url", "observed_state", "observed_version", "observed_at", "kind", "body", "received_on", "product_id", "signal_source_id", "fields"}, nil},
	{"getSignal", "signals get", "GET", "/signals/{id}", "", nil, nil, []string{"a1"}},
	{"listStandards", "standards list", "GET", "/standards", "", nil, nil, nil},
	{"getStandard", "standards get", "GET", "/standards/{id}", "", nil, nil, []string{"a1"}},
	{"updateStandard", "standards update", "PATCH", "/standards/{id}", "standards", nil, []string{"standard_keys"}, []string{"s1"}},
	{"listStandardRevisions", "standards revisions list", "GET", "/standards/{standard_id}/revisions", "", nil, nil, []string{"a1"}},
	{"getStandardRevision", "standards revisions get", "GET", "/standards/{standard_id}/revisions/{id}", "", nil, nil, []string{"a1", "a2"}},
	{"updateWorkItem", "work-items update", "PATCH", "/work_items/{id}", "", nil, []string{"responsible_agent_id", "current_state_evidence", "desired_outcome", "acceptance_criteria", "verification", "verification_check", "observation_action_id", "title", "status", "description", "reason", "agent_role_id", "next_action", "reconcile", "depends_on_ids"}, []string{"a1"}},
	{"archiveWorkItem", "work-items delete", "DELETE", "/work_items/{id}", "", []string{"description"}, []string{"description"}, []string{"a1"}},
	{"createInboxItemAnswer", "inbox-items answer create", "POST", "/inbox_items/{inbox_item_id}/answer", "", []string{"option", "note", "plan_choice"}, []string{"option", "value", "note", "plan_choice", "automation_plan"}, []string{"i1"}},
	{"createInboxItemEscalation", "inbox-items escalation create", "POST", "/inbox_items/{inbox_item_id}/escalation", "", []string{"reason"}, []string{"reason"}, []string{"i1"}},
	{"withdrawInboxItem", "inbox-items withdrawal create", "POST", "/inbox_items/{inbox_item_id}/withdrawal", "inbox-items", []string{"description", "reason", "evidence"}, []string{"description", "reason", "evidence"}, []string{"i1"}},
	{"createRunExecution", "runs create-run-execution", "POST", "/runs/{run_id}/execution", "", nil, nil, []string{"r1"}},
	{"createRunResult", "runs result create", "POST", "/runs/{run_id}/result", "", []string{"answer"}, []string{"answer", "item_outcome"}, []string{"r1"}},
	{"listWatches", "watches list", "GET", "/watches", "", nil, nil, nil},
	{"getWatch", "watches get", "GET", "/watches/{id}", "", nil, nil, []string{"watch1"}},
	{"createWatch", "watches create", "POST", "/watches", "watches", []string{"owner_type", "owner_id", "capability_id", "tool", "external_id", "condition"}, []string{"owner_type", "owner_id", "capability_id", "signal_source_id", "tool", "external_id", "url", "expires_at", "condition"}, nil},
	{"createWatchCancellation", "watches cancellation create", "POST", "/watches/{watch_id}/cancellation", "", []string{"description"}, []string{"description"}, []string{"watch1"}},
	{"getSignalSource", "signal-sources get", "GET", "/signal_sources/{id}", "", nil, nil, []string{"source1"}},
	{"createSignalSource", "signal-sources create", "POST", "/signal_sources", "signal_sources", []string{"name", "kind"}, []string{"name", "kind", "summary", "reconciliation_cursor", "settings", "webhook_mode", "webhook_signature_header", "webhook_digest", "webhook_signature_prefix", "webhook_event_id_field", "webhook_secret"}, nil},
	{"updateSignalSource", "signal-sources update", "PATCH", "/signal_sources/{id}", "signal_sources", nil, []string{"name", "kind", "summary", "reconciliation_cursor", "settings", "webhook_mode", "webhook_signature_header", "webhook_digest", "webhook_signature_prefix", "webhook_event_id_field", "webhook_secret"}, []string{"source1"}},
	{"getNotificationChannel", "notification-channels get", "GET", "/notification_channels/{id}", "", nil, nil, []string{"pilot-alerts"}},
}

// TestNewContractOperations pins command names, fields and wire requests for
// current contract additions and changed operations.
func TestNewContractOperations(t *testing.T) {
	sp := pinnedSpec(t)
	srv := newStub(t)
	byOp := commandsByOperation(testRoot(t, sp, srv.URL, fastCfg()))

	for _, tc := range newOps {
		t.Run(tc.id, func(t *testing.T) {
			cmd := byOp[tc.id]
			if cmd == nil {
				t.Fatalf("no command for %s", tc.id)
			}
			if got := strings.TrimPrefix(cmd.CommandPath(), "autonomatctl "); got != tc.command {
				t.Fatalf("command = %q, want %q", got, tc.command)
			}
			op := find(t, sp, tc.id)
			if op.Method != tc.method || op.Path != tc.path {
				t.Fatalf("contract = %s %s, want %s %s", op.Method, op.Path, tc.method, tc.path)
			}

			var names []string
			args := strings.Fields(tc.command)
			args = append(args, tc.pathArgs...)
			if op.Body != nil && !op.Body.Linkage {
				if op.Body.DataType != tc.dataType {
					t.Errorf("data.type = %q, want %q", op.Body.DataType, tc.dataType)
				}
				for _, f := range op.Body.Fields {
					names = append(names, f.Name)
					if cmd.Flags().Lookup(flagOf(f.Name)) == nil {
						t.Errorf("missing flag --%s", flagOf(f.Name))
					}
				}
			}
			if !reflect.DeepEqual(names, tc.attrs) && !(len(names) == 0 && len(tc.attrs) == 0) {
				t.Errorf("fields = %v, want %v", names, tc.attrs)
			}

			// required flags are enforced
			for _, r := range tc.required {
				if _, _, err := run(t, sp, srv.URL, args...); err == nil || !strings.Contains(err.Error(), `"`+r+`"`) {
					t.Errorf("missing --%s not reported: %v", r, err)
				}
			}

			// send the request with every field set
			if op.Body != nil && op.Body.Linkage {
				args = append(args, "--ids", "c1", "--ids", "c2")
			}
			want := map[string]any{}
			if op.Body != nil {
				for _, f := range op.Body.Fields {
					a, v := sample(f.Name, f.Type, f.ItemType, f.Enum)
					if f.Name == "settings" && (tc.id == "createSignalSource" || tc.id == "updateSignalSource") {
						const settings = `{"event_type_field":"type","message_mapping":{"deployment.updated":{"item":{"kind":"deployment","field":"deployment.id"},"container":{"kind":"project","field":"project.id"},"account":{"kind":"organization","field":"org.id"},"state":"deployment.state","version":"deployment.version","require":[{"paths":["deployment.environment","environment"],"equals":"pilot"},{"paths":["labels[*]"],"equals":"review"}]},"project.created":{"item":{"kind":"project","field":"project.id"}}}}`
						a, v = []string{"--settings", settings}, decode(t, []byte(settings))
					}
					switch f.Name {
					case "additional_standard_keys":
						a, v = []string{"--additional_standard_keys", `["review","deploy"]`}, []any{"review", "deploy"}
					case "excluded_standard_keys":
						a, v = []string{"--excluded_standard_keys", `["legacy"]`}, []any{"legacy"}
					}
					args = append(args, a...)
					want[f.Name] = v
				}
			}
			if cmd.Flags().Lookup("yes") != nil {
				args = append(args, "--yes")
			}
			before := len(srv.requests())
			if _, stderr, err := run(t, sp, srv.URL, args...); err != nil {
				t.Fatalf("%v: %v\n%s", args, err, stderr)
			}
			reqs := srv.requests()
			if len(reqs) != before+1 {
				t.Fatalf("requests = %d, want 1", len(reqs)-before)
			}
			got := reqs[len(reqs)-1]
			wantPath := tc.path
			for _, v := range tc.pathArgs {
				i := strings.Index(wantPath, "{")
				j := strings.Index(wantPath, "}")
				wantPath = wantPath[:i] + v + wantPath[j+1:]
			}
			if got.Method != tc.method || got.Path != wantPath {
				t.Errorf("request = %s %s, want %s %s", got.Method, got.Path, tc.method, wantPath)
			}
			if op.Body == nil {
				if len(got.Body) != 0 {
					t.Errorf("unexpected body %s", got.Body)
				}
				return
			}
			if op.Body.Linkage {
				ids, _ := decode(t, got.Body)["data"].([]any)
				if len(ids) != 2 {
					t.Fatalf("linkage data = %v, want two identifiers", ids)
				}
				if first, _ := ids[0].(map[string]any); first["type"] != tc.dataType || first["id"] != "c1" {
					t.Errorf("first identifier = %v", first)
				}
				return
			}
			data, _ := decode(t, got.Body)["data"].(map[string]any)
			if tc.dataType != "" && data["type"] != tc.dataType {
				t.Errorf("data.type = %v, want %s", data["type"], tc.dataType)
			}
			if attrs, _ := data["attributes"].(map[string]any); !reflect.DeepEqual(attrs, want) && len(want) > 0 {
				t.Errorf("attributes = %v, want %v", attrs, want)
			}
		})
	}
}

func TestProductAdditionalStandardKeysClearAndOmit(t *testing.T) {
	sp := pinnedSpec(t)
	for _, tc := range []struct {
		name  string
		flags []string
		attrs map[string]any
	}{
		{"clear", []string{"--additional_standard_keys", "[]"}, map[string]any{"additional_standard_keys": []any{}}},
		{"omit", []string{"--purpose", "Pilot deployments"}, map[string]any{"purpose": "Pilot deployments"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newStub(t)
			args := append([]string{"products", "update", "pilot"}, tc.flags...)
			if _, _, err := run(t, sp, srv.URL, args...); err != nil {
				t.Fatal(err)
			}
			reqs := srv.requests()
			if len(reqs) != 1 || reqs[0].Method != "PATCH" || reqs[0].Path != "/products/pilot" {
				t.Fatalf("requests = %+v, want PATCH /products/pilot", reqs)
			}
			data := decode(t, reqs[0].Body)
			want := map[string]any{"data": map[string]any{"type": "products", "attributes": tc.attrs}}
			if !reflect.DeepEqual(data, want) {
				t.Errorf("body = %v, want %v", data, want)
			}
		})
	}
}

func TestCapabilityNameAndReviewRules(t *testing.T) {
	sp := pinnedSpec(t)
	const configuration = `{"levels":["item"],"kinds":{"deployment":"item","release":"item"},"review_on":{"deployment":{"states":["succeeded","failed"]},"release":{"version":true}}}`
	for _, tc := range []struct {
		args   []string
		method string
		path   string
	}{
		{[]string{"capabilities", "create"}, "POST", "/capabilities"},
		{[]string{"capabilities", "update", "deploy"}, "PATCH", "/capabilities/deploy"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			srv := newStub(t)
			args := append(tc.args, "--key", "deploy", "--name", "Pilot deployments", "--watch_configuration", configuration)
			if _, _, err := run(t, sp, srv.URL, args...); err != nil {
				t.Fatal(err)
			}
			reqs := srv.requests()
			if len(reqs) != 1 || reqs[0].Method != tc.method || reqs[0].Path != tc.path {
				t.Fatalf("requests = %+v, want %s %s", reqs, tc.method, tc.path)
			}
			want := map[string]any{"data": map[string]any{
				"type": "capabilities",
				"attributes": map[string]any{
					"key": "deploy", "name": "Pilot deployments", "watch_configuration": decode(t, []byte(configuration)),
				},
			}}
			if got := decode(t, reqs[0].Body); !reflect.DeepEqual(got, want) {
				t.Errorf("body = %v, want %v", got, want)
			}
		})
	}
}

func TestSignalsListPreservesTypedExternalID(t *testing.T) {
	sp := pinnedSpec(t)
	srv := newStub(t)
	const query = `owner:"product:42" tool:deploy external_id:"deployment:Deploy:42"`
	if _, _, err := run(t, sp, srv.URL, "signals", "list", "--q", query); err != nil {
		t.Fatal(err)
	}
	reqs := srv.requests()
	if len(reqs) != 1 || reqs[0].Query.Get("q") != query {
		t.Fatalf("requests = %+v, want exact query %q", reqs, query)
	}
	id := "deployment:Deploy:42" + `\Path"QA"` + "\nNext"
	if _, _, err := run(t, sp, srv.URL, "signals", "list", "--q-external_id", id); err != nil {
		t.Fatal(err)
	}
	want := `external_id:"deployment:Deploy:42\\Path\"QA\"` + "\nNext\""
	reqs = srv.requests()
	if len(reqs) != 2 || reqs[1].Query.Get("q") != want {
		t.Fatalf("requests = %+v, want escaped query %q", reqs, want)
	}
}

func TestRunExecutionRetryCount(t *testing.T) {
	sp := pinnedSpec(t)
	srv := newStub(t)
	if _, _, err := run(t, sp, srv.URL, "runs", "create-run-execution", "r1", "--retry_count", "0"); err != nil {
		t.Fatal(err)
	}
	got := srv.requests()[0]
	if got.Method != "POST" || got.Path != "/runs/r1/execution" || got.Query.Get("retry_count") != "0" || len(got.Body) != 0 || got.ContentType != "" {
		t.Errorf("unexpected run admission request %+v", got)
	}
}

func TestInboxAnswerAcceptsScalarValue(t *testing.T) {
	sp := pinnedSpec(t)
	srv := newStub(t)
	if _, _, err := run(t, sp, srv.URL, "inbox-items", "answer", "create", "i1",
		"--option", "go", "--note", "Approve the pilot", "--plan_choice", "approve_only", "--value", `"pilot"`); err != nil {
		t.Fatal(err)
	}
	got := decode(t, srv.requests()[0].Body)["data"].(map[string]any)["attributes"]
	want := map[string]any{"option": "go", "note": "Approve the pilot", "plan_choice": "approve_only", "value": "pilot"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("answer attributes = %v, want %v", got, want)
	}
}

// TestEveryOperationHasCommand compares the contract's operationIds with the
// generated commands in both directions.
func TestEveryOperationHasCommand(t *testing.T) {
	sp := pinnedSpec(t)
	byOp := commandsByOperation(testRoot(t, sp, "http://unused", fastCfg()))
	ids := map[string]bool{}
	for _, op := range sp.Operations {
		ids[op.ID] = true
		if byOp[op.ID] == nil {
			t.Errorf("operation %s has no command", op.ID)
		}
	}
	for id := range byOp {
		if !ids[id] {
			t.Errorf("command for unknown operation %s", id)
		}
	}
	for _, tc := range newOps {
		if !ids[tc.id] {
			t.Errorf("new operation %s missing from the pinned contract", tc.id)
		}
	}
}

func find(t *testing.T, sp *spec.Spec, id string) spec.Operation {
	t.Helper()
	for _, o := range sp.Operations {
		if o.ID == id {
			return o
		}
	}
	t.Fatalf("operation %s not found", id)
	return spec.Operation{}
}

// TestListFiltersAreTyped checks that every documented query parameter of the
// new collection operations is a flag of the type the contract declares.
func TestListFiltersAreTyped(t *testing.T) {
	sp := pinnedSpec(t)
	byOp := commandsByOperation(testRoot(t, sp, "http://unused", fastCfg()))
	flagType := map[string]string{"string": "string", "integer": "int64"}
	for _, tc := range newOps {
		if !strings.HasPrefix(tc.id, "list") {
			continue
		}
		op := find(t, sp, tc.id)
		if (len(op.Query) == 0) != (tc.id == "listWatches") {
			t.Errorf("%s: unexpected query parameters %v", tc.id, op.Query)
		}
		for _, q := range op.Query {
			f := byOp[tc.id].Flags().Lookup(q.Name)
			if f == nil {
				t.Errorf("%s: missing --%s", tc.id, q.Name)
			} else if f.Value.Type() != flagType[q.Type] {
				t.Errorf("%s: --%s is %s, contract says %s", tc.id, q.Name, f.Value.Type(), q.Type)
			}
		}
	}
}
