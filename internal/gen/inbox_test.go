package gen

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// The inbox part "context" collides with ctlkit's global --context, so the
// body field is exposed as --attr-context and still sent as "context".
func TestReservedBodyFieldGetsAttrFlag(t *testing.T) {
	sp := pinnedSpec(t)
	srv := newStub(t)
	byOp := commandsByOperation(testRoot(t, sp, srv.URL, fastCfg()))
	c := byOp["updateInboxItem"]
	if f := c.Flags().Lookup("attr-context"); f == nil || !strings.Contains(f.Usage, "context") {
		t.Fatal("update has no --attr-context flag with a mapping note")
	}
	if c.Flags().Lookup("context") != nil {
		t.Error("local --context shadows the global flag")
	}
	args := []string{"inbox-items", "update", "i1", "--attr-context", "why now"}
	if _, stderr, err := run(t, sp, srv.URL, args...); err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	reqs := srv.requests()
	var doc struct {
		Data struct {
			Attributes map[string]any `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(reqs[len(reqs)-1].Body), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Data.Attributes["context"] != "why now" || len(doc.Data.Attributes) != 1 {
		t.Errorf("attributes = %v", doc.Data.Attributes)
	}
}

func TestInboxOptionsTakeJSONArray(t *testing.T) {
	sp := pinnedSpec(t)
	srv := newStub(t)
	for _, opts := range []string{
		`[{"key":"a","label":"A","consequence":"Continue","call":{"capability":"deploy","arguments":{"subcommand":"deploy","settings":{"regions":["us","eu"],"enabled":true}},"reason":"Ship approved revision"},"value_request":{"kind":"input","label":"Release name","type":"text","required":true}},{"key":"b","label":"Wait","consequence":"Defer"}]`,
		`[{"key":"a","label":"A","consequence":"Continue"}]`,
	} {
		if _, stderr, err := run(t, sp, srv.URL, "inbox-items", "update", "i1", "--options", opts); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		reqs := srv.requests()
		data := decode(t, reqs[len(reqs)-1].Body)["data"].(map[string]any)
		attrs := data["attributes"].(map[string]any)
		var want any
		if err := json.Unmarshal([]byte(opts), &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(attrs["options"], want) {
			t.Errorf("options = %v, want %v", attrs["options"], want)
		}
	}
	n := len(srv.requests())
	_, _, err := run(t, sp, srv.URL, "inbox-items", "update", "i1", "--options", `{"key":"a"}`)
	if err == nil || !strings.Contains(err.Error(), "--options") {
		t.Errorf("non-array options: %v", err)
	}
	if len(srv.requests()) != n {
		t.Error("a request was sent for invalid options")
	}
}

func TestInboxEscalationCreate(t *testing.T) {
	sp := pinnedSpec(t)
	srv := newStub(t)
	base := []string{"inbox-items", "create"}
	for _, invalid := range [][]string{{"--category", "decision"}, {"--automation_assessment", "{}"}} {
		if _, _, err := run(t, sp, srv.URL, append(base, invalid...)...); err == nil {
			t.Fatalf("accepted incomplete or obsolete arguments %v", invalid)
		}
	}
	if len(srv.requests()) != 0 {
		t.Fatal("sent a request with incomplete or obsolete arguments")
	}
	options := `[{"key":"go","label":"Proceed","consequence":{"continues":"Pilot","spent":"$10","not_done":"Full rollout"}},{"key":"wait","label":"Wait","consequence":{"continues":"Review","spent":"$0","not_done":"Pilot"}}]`
	plan := `{"action":"grant","target":"Capability:7","exact_change":{"scope":{"project":"pilot"},"expires_at":"2030-01-01T00:00:00Z","uses":1},"summary":"Allow future pilot runs"}`
	args := append(base, "--subject", "Approve the pilot?", "--category", "decision",
		"--escalation_chain", `[{"agent_id":5,"reason":"Approval is required"}]`,
		"--request", `{"action":"enable","target":"pilot","exact_change":"enable one project","duration":"one day","values":{"enabled":true}}`,
		"--evidence", "Pilot checks passed", "--options", options, "--recommendation", "go",
		"--prepared_result", "Pilot configuration is ready", "--unblocks", "Limited rollout",
		"--money_at_stake", `{"amount_minor":1000,"currency":"USD"}`, "--automation_plan", plan)
	if _, _, err := run(t, sp, srv.URL, args...); err != nil {
		t.Fatal(err)
	}
	got := decode(t, srv.requests()[0].Body)["data"].(map[string]any)
	attrs := got["attributes"].(map[string]any)
	if got["type"] != "inbox-items" || len(attrs) != 11 ||
		!reflect.DeepEqual(attrs["automation_plan"], decode(t, []byte(plan))) ||
		!reflect.DeepEqual(attrs["options"], decode(t, []byte(`{"options":`+options+`}`))["options"]) {
		t.Errorf("unexpected create document %v", got)
	}
}

func TestWorkItemCreateRequiresWorkable(t *testing.T) {
	sp := pinnedSpec(t)
	byOp := commandsByOperation(testRoot(t, sp, "http://unused.invalid", fastCfg()))
	c := byOp["createWorkItem"]
	for _, n := range []string{"workable_type", "workable_id", "product_id"} {
		f := c.Flags().Lookup(n)
		if f == nil || len(f.Annotations[cobra.BashCompOneRequiredFlag]) != 1 {
			t.Errorf("--%s is not required", n)
		}
	}
}

func TestPendingCallOptionalReferences(t *testing.T) {
	sp := pinnedSpec(t)
	srv := newStub(t)
	for _, extra := range [][]string{nil, {"--title", "Deploy release"}, {"--title", "Deploy release", "--replaces", "42"}} {
		args := []string{"pending-calls", "create", "--capability", "deploy", "--arguments", `{"subcommand":"deploy"}`, "--reason", "Ship revision"}
		args = append(args, extra...)
		if _, stderr, err := run(t, sp, srv.URL, args...); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		reqs := srv.requests()
		data := decode(t, reqs[len(reqs)-1].Body)["data"].(map[string]any)
		want := map[string]any{"capability": "deploy", "arguments": map[string]any{"subcommand": "deploy"}, "reason": "Ship revision"}
		for i := 0; i < len(extra); i += 2 {
			want[strings.TrimPrefix(extra[i], "--")] = extra[i+1]
		}
		if !reflect.DeepEqual(data["attributes"], want) {
			t.Errorf("attributes = %v, want %v", data["attributes"], want)
		}
	}
}
