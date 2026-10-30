package gen

import (
	"encoding/json"
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
	opts := `[{"key":"a","label":"A","consequence":"x"}]`
	if _, stderr, err := run(t, sp, srv.URL, "inbox-items", "create", "--category", "decision", "--options", opts); err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	reqs := srv.requests()
	var doc struct {
		Data struct {
			Attributes struct {
				Options []map[string]string `json:"options"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(reqs[len(reqs)-1].Body), &doc); err != nil {
		t.Fatal(err)
	}
	if o := doc.Data.Attributes.Options; len(o) != 1 || o[0]["key"] != "a" || o[0]["consequence"] != "x" {
		t.Errorf("options = %v", o)
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
