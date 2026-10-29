package gen

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dotdevlabs/autonomatctl/internal/spec"
)

// newOps are the operations added to the contract in 1.2.0. Each row pins the
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
	{"createWorkItem", "work-items create", "POST", "/work_items", "work-items", []string{"item_type", "title", "source_kind", "status"},
		[]string{"item_type", "title", "body", "tags", "source_kind", "source_ref", "status", "dispatch_reason", "ref", "intake_status", "parent_id", "merged_into_id", "connection_id", "agent_role_id", "product_id", "workable_type", "workable_id", "created_at", "updated_at"}, nil},
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
}

// TestNewContractOperations covers every operation that the 1.2.0 contract
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
			if op.Body != nil {
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
				if _, _, err := run(t, sp, srv.URL, args...); err == nil || !strings.Contains(err.Error(), "--"+r) {
					t.Errorf("missing --%s not reported: %v", r, err)
				}
			}

			// send the request with every field set
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
