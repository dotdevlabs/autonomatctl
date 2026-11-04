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
	for _, id := range []string{"createInboxItem", "updateInboxItem"} {
		c := byOp[id]
		if f := c.Flags().Lookup("attr-context"); f == nil || !strings.Contains(f.Usage, "context") {
			t.Fatalf("%s: no --attr-context flag with a mapping note", id)
		}
		if c.Flags().Lookup("context") != nil {
			t.Errorf("%s: local --context shadows the global flag", id)
		}
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
		for _, args := range [][]string{{"inbox-items", "create", "--category", "decision", "--automation_assessment", `{"disposition":"exception","evidence":"Operator decision needed","exception":{"reason_key":"human_judgment","explanation":"Requires operator judgment","evidence":"No automated decision available","remaining_human_action":"Choose the release name","why_partial_automation_cannot_help":"The operator owns this choice","reconsideration_conditions":"Reconsider when naming rules exist"}}`}, {"inbox-items", "update", "i1"}} {
			args = append(args, "--options", opts)
			if _, stderr, err := run(t, sp, srv.URL, args...); err != nil {
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
