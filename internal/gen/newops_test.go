package gen

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dotdevlabs/autonomatctl/internal/spec"
)

// newOps are the operations added to the contract in 1.2.0, 1.7.0 and 1.12.0. Each row pins the
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
	{"updateAccount", "account update", "PATCH", "/account", "", nil, []string{"name", "worker_address", "worker_ingest_token", "default_connection_id"}, nil},
	{"getHealthRules", "health-rules get", "GET", "/health_rules", "", nil, nil, nil},
	{"updateHealthRules", "health-rules update", "PATCH", "/health_rules", "", nil, []string{"window_days", "key_metrics", "standard", "state_rules"}, nil},
	{"getSpend", "spend get", "GET", "/spend", "", nil, nil, nil},
	{"getStatus", "status get", "GET", "/status", "", nil, nil, nil},
	{"listWorkItems", "work-items list", "GET", "/work_items", "", nil, nil, nil},
	{"listExecutions", "executions list", "GET", "/executions", "", nil, nil, nil},
	{"createWorkItem", "work-items create", "POST", "/work_items", "work-items", []string{"item_type", "title", "source_kind", "status"},
		[]string{"item_type", "title", "body", "tags", "source_kind", "source_ref", "status", "dispatch_reason", "next_action", "ref", "intake_status", "parent_id", "merged_into_id", "connection_id", "agent_role_id", "product_id", "workable_type", "workable_id", "created_at", "updated_at"}, nil},
	{"getWorkItem", "work-items get", "GET", "/work_items/{id}", "", nil, nil, []string{"w1"}},
	{"createExecution", "work-items executions create", "POST", "/work_items/{work_item_id}/executions", "executions", []string{"state"},
		[]string{"state", "agent_role_id", "started_at", "finished_at", "usage", "connection_id", "applied_correction_ids", "hatchet_run_id", "requested_action_errors"}, []string{"w1"}},
	{"sunsetProduct", "products sunset", "POST", "/products/{product_id}/sunset", "", nil, nil, []string{"p1"}},
	{"updateInboxItem", "inbox-items update", "PATCH", "/inbox_items/{id}", "inbox-items", nil, []string{"reason", "evidence"}, []string{"i1"}},
	{"createInboxItemResolution", "inbox-items resolution create", "POST", "/inbox_items/{inbox_item_id}/resolution", "inbox-item-resolutions", nil, []string{"evidence"}, []string{"i1"}},
	{"createPendingCall", "pending-calls create", "POST", "/pending_calls", "pending-calls", nil, []string{"capability", "arguments", "reason"}, nil},
	{"dispatchWorkItem", "work-items dispatch", "POST", "/work_items/{work_item_id}/dispatch", "", nil, nil, []string{"w1"}},
	{"createWorkItemSteering", "work-items steering create", "POST", "/work_items/{work_item_id}/steering", "steering-instructions", nil, []string{"instruction"}, []string{"w1"}},
	{"createInitiativeSteering", "initiatives steering create", "POST", "/initiatives/{initiative_id}/steering", "steering-instructions", nil, []string{"instruction"}, []string{"n1"}},
	{"createProductDocumentRevision", "products documents revisions create", "POST", "/products/{product_id}/documents/{document_id}/revisions", "document-revisions", nil, []string{"body", "summary"}, []string{"p1", "d1"}},
	{"createStandardRevision", "standards revisions create", "POST", "/standards/{standard_id}/revisions", "document-revisions", nil, []string{"body", "summary"}, []string{"s1"}},
	{"listActivityEvents", "activity-events list", "GET", "/activity_events", "", nil, nil, nil},
	{"getActivityEvent", "activity-events get", "GET", "/activity_events/{id}", "", nil, nil, []string{"a1"}},
	{"listAgentRoleCapabilities", "agent-roles relationships capabilities list", "GET", "/agent_roles/{agent_role_id}/relationships/capabilities", "", nil, nil, []string{"a1"}},
	{"addAgentRoleCapabilities", "agent-roles relationships capabilities create", "POST", "/agent_roles/{agent_role_id}/relationships/capabilities", "capabilities", []string{"ids"}, nil, []string{"a1"}},
	{"removeAgentRoleCapabilities", "agent-roles relationships capabilities delete", "DELETE", "/agent_roles/{agent_role_id}/relationships/capabilities", "capabilities", []string{"ids"}, nil, []string{"a1"}},
	{"listConnections", "connections list", "GET", "/connections", "", nil, nil, nil},
	{"createConnection", "connections create", "POST", "/connections", "connections", nil, []string{"name", "kind", "provider", "credential_kind", "expires_at", "endpoint_url", "credential", "secret", "refresh_value", "request_headers"}, nil},
	{"getConnection", "connections get", "GET", "/connections/{id}", "", nil, nil, []string{"a1"}},
	{"updateConnection", "connections update", "PATCH", "/connections/{id}", "connections", nil, []string{"name", "kind", "provider", "credential_kind", "expires_at", "endpoint_url", "credential", "secret", "refresh_value", "request_headers"}, []string{"a1"}},
	{"archiveConnection", "connections delete", "DELETE", "/connections/{id}", "", nil, nil, []string{"a1"}},
	{"listConversations", "conversations list", "GET", "/conversations", "", nil, nil, nil},
	{"createConversation", "conversations create", "POST", "/conversations", "conversations", []string{"title"}, []string{"title", "context_type", "context_id"}, nil},
	{"listConversationMessages", "conversations messages list", "GET", "/conversations/{conversation_id}/messages", "", nil, nil, []string{"a1"}},
	{"createConversationMessage", "conversations messages create", "POST", "/conversations/{conversation_id}/messages", "messages", []string{"body"}, []string{"body"}, []string{"a1"}},
	{"getConversation", "conversations get", "GET", "/conversations/{id}", "", nil, nil, []string{"a1"}},
	{"listInboxItems", "inbox-items list", "GET", "/inbox_items", "", nil, nil, nil},
	{"getInboxItem", "inbox-items get", "GET", "/inbox_items/{id}", "", nil, nil, []string{"a1"}},
	{"listInitiatives", "initiatives list", "GET", "/initiatives", "", nil, nil, nil},
	{"createInitiative", "initiatives create", "POST", "/initiatives", "", []string{"key", "title"}, []string{"key", "title", "status", "automatic_dispatch_paused", "daily_budget_cents", "started_on", "target_on", "concluded_on", "product_ids"}, nil},
	{"getInitiative", "initiatives get", "GET", "/initiatives/{id}", "", nil, nil, []string{"a1"}},
	{"updateInitiative", "initiatives update", "PATCH", "/initiatives/{id}", "", nil, []string{"title", "status", "automatic_dispatch_paused", "daily_budget_cents", "started_on", "target_on", "concluded_on"}, []string{"a1"}},
	{"archiveInitiative", "initiatives delete", "DELETE", "/initiatives/{id}", "", nil, nil, []string{"a1"}},
	{"listPolicies", "policies list", "GET", "/policies", "", nil, nil, nil},
	{"createPolicy", "policies create", "POST", "/policies", "policies", nil, []string{"key", "applies_when", "requires", "attempt_limit", "time_limit", "owner_node", "body", "enabled"}, nil},
	{"getPolicy", "policies get", "GET", "/policies/{id}", "", nil, nil, []string{"a1"}},
	{"updatePolicy", "policies update", "PATCH", "/policies/{id}", "policies", nil, []string{"key", "applies_when", "requires", "attempt_limit", "time_limit", "owner_node", "body", "enabled"}, []string{"a1"}},
	{"archivePolicy", "policies delete", "DELETE", "/policies/{id}", "", nil, nil, []string{"a1"}},
	{"listProducts", "products list", "GET", "/products", "", nil, nil, nil},
	{"getProduct", "products get", "GET", "/products/{id}", "", nil, nil, []string{"a1"}},
	{"updateProduct", "products update", "PATCH", "/products/{id}", "products", nil, []string{"name", "purpose", "next_action", "daily_budget", "key_metrics", "parent_id"}, []string{"a1"}},
	{"archiveProduct", "products delete", "DELETE", "/products/{id}", "", nil, nil, []string{"a1"}},
	{"listProductDocuments", "products documents list", "GET", "/products/{product_id}/documents", "", nil, nil, []string{"a1"}},
	{"listDocumentRevisions", "products documents revisions list", "GET", "/products/{product_id}/documents/{document_id}/revisions", "", nil, nil, []string{"a1", "a2"}},
	{"getDocumentRevision", "products documents revisions get", "GET", "/products/{product_id}/documents/{document_id}/revisions/{id}", "", nil, nil, []string{"a1", "a2", "a3"}},
	{"getProductDocument", "products documents get", "GET", "/products/{product_id}/documents/{id}", "", nil, nil, []string{"a1", "a2"}},
	{"listRefinements", "refinements list", "GET", "/refinements", "", nil, nil, nil},
	{"createRefinement", "refinements create", "POST", "/refinements", "", []string{"product_id", "title"}, []string{"product_id", "title", "status", "diagnosis", "bounded_scope", "expected_outcome", "execution_path", "outcome", "verification_notes"}, nil},
	{"getRefinement", "refinements get", "GET", "/refinements/{id}", "", nil, nil, []string{"a1"}},
	{"updateRefinement", "refinements update", "PATCH", "/refinements/{id}", "", nil, []string{"title", "status", "diagnosis", "bounded_scope", "expected_outcome", "execution_path", "outcome", "verification_notes"}, []string{"a1"}},
	{"archiveRefinement", "refinements delete", "DELETE", "/refinements/{id}", "", nil, nil, []string{"a1"}},
	{"listSchedules", "schedules list", "GET", "/schedules", "", nil, nil, nil},
	{"getSchedule", "schedules get", "GET", "/schedules/{id}", "", nil, nil, []string{"a1"}},
	{"updateSchedule", "schedules update", "PATCH", "/schedules/{id}", "schedules", nil, []string{"enabled", "max_concurrent_runs", "at", "timezone", "description", "steps"}, []string{"a1"}},
	{"listSignals", "signals list", "GET", "/signals", "", nil, nil, nil},
	{"createSignal", "signals create", "POST", "/signals", "signals", []string{"kind"}, []string{"kind", "body", "received_on", "product_id", "signal_source_id", "fields"}, nil},
	{"getSignal", "signals get", "GET", "/signals/{id}", "", nil, nil, []string{"a1"}},
	{"listStandards", "standards list", "GET", "/standards", "", nil, nil, nil},
	{"getStandard", "standards get", "GET", "/standards/{id}", "", nil, nil, []string{"a1"}},
	{"listStandardRevisions", "standards revisions list", "GET", "/standards/{standard_id}/revisions", "", nil, nil, []string{"a1"}},
	{"getStandardRevision", "standards revisions get", "GET", "/standards/{standard_id}/revisions/{id}", "", nil, nil, []string{"a1", "a2"}},
	{"updateWorkItem", "work-items update", "PATCH", "/work_items/{id}", "", nil, []string{"title", "status", "agent_role_id", "next_action"}, []string{"a1"}},
	{"archiveWorkItem", "work-items delete", "DELETE", "/work_items/{id}", "", nil, nil, []string{"a1"}},
}

// TestNewContractOperations covers every operation that the pinned contract
// added: the subcommand, exact field names, required flags, and the request
// sent on the wire.
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
					if cmd.Flags().Lookup(f.Name) == nil {
						t.Errorf("missing flag --%s", f.Name)
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
		if len(op.Query) == 0 {
			t.Errorf("%s: no query parameters documented", tc.id)
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
