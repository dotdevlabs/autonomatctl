package gen

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dotdevlabs/autonomatctl/internal/spec"
)

// sample returns the CLI arguments and the JSON value the contract expects
// for one documented input.
func sample(name, typ, item string, enum []string) (args []string, want any) {
	flag := "--" + name
	if bf := bodyFlag(name); bf != "" {
		flag = "--" + bf
	}
	switch {
	case typ == "string" && len(enum) > 0:
		return []string{flag, enum[0]}, enum[0]
	case typ == "string":
		return []string{flag, "v-" + name}, "v-" + name
	case typ == "integer":
		return []string{flag, "7"}, float64(7)
	case typ == "number":
		return []string{flag, "1.5"}, 1.5
	case typ == "boolean":
		return []string{flag + "=true"}, true
	case typ == "array" && item == "string":
		return []string{flag, "a", flag, "b,c"}, []any{"a", "b,c"}
	case typ == "array":
		return []string{flag, `[1,2]`}, []any{float64(1), float64(2)}
	default:
		return []string{flag, `{"k":1}`}, map[string]any{"k": float64(1)}
	}
}

// flagOf is the CLI flag of a body field.
func flagOf(name string) string {
	if bf := bodyFlag(name); bf != "" {
		return bf
	}
	return name
}

func commandsByOperation(root *cobra.Command) map[string]*cobra.Command {
	out := map[string]*cobra.Command{}
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, s := range c.Commands() {
			if id := s.Annotations[AnnOperationID]; id != "" {
				out[id] = s
			}
			walk(s)
		}
	}
	walk(root)
	return out
}

// TestConformance builds a request for every operation of the pinned
// contract through its generated command and compares the request on the wire
// with the contract.
func TestConformance(t *testing.T) {
	sp := pinnedSpec(t)
	if len(sp.Operations) == 0 {
		t.Fatal("pinned contract has no operations")
	}
	srv := newStub(t)
	root := testRoot(t, sp, srv.URL, fastCfg())
	byOp := commandsByOperation(root)

	// every operation has a command, and command paths are unique
	paths := map[string]string{}
	for _, op := range sp.Operations {
		c, ok := byOp[op.ID]
		if !ok {
			t.Fatalf("operation %s (%s %s) has no command", op.ID, op.Method, op.Path)
		}
		if c.Annotations[AnnMethod] != op.Method || c.Annotations[AnnPath] != op.Path {
			t.Errorf("%s: annotations %v do not match %s %s", op.ID, c.Annotations, op.Method, op.Path)
		}
		if prev, dup := paths[c.CommandPath()]; dup {
			t.Errorf("command %q generated for both %s and %s", c.CommandPath(), prev, op.ID)
		}
		paths[c.CommandPath()] = op.ID
	}
	if len(byOp) != len(sp.Operations) {
		t.Errorf("%d commands for %d operations", len(byOp), len(sp.Operations))
	}

	for _, op := range sp.Operations {
		t.Run(op.ID, func(t *testing.T) {
			before := len(srv.requests())
			cmd := byOp[op.ID]

			args := strings.Fields(strings.TrimPrefix(cmd.CommandPath(), "autonomatctl "))
			wantPath := op.Path
			for i, p := range op.PathParams {
				v := fmt.Sprintf("v%d a/b", i)
				args = append(args, v)
				wantPath = strings.Replace(wantPath, "{"+p+"}", url.PathEscape(v), 1)
			}
			wantQuery := map[string]bool{}
			for _, q := range op.Query {
				a, _ := sample(q.Name, q.Type, "string", nil)
				args = append(args, a...)
				wantQuery[q.Name] = true
			}
			wantAttrs := map[string]any{}
			var wantID string
			if op.Body != nil && op.Body.Linkage {
				args = append(args, "--ids", "x1", "--ids", "x2")
			} else if op.Body != nil {
				if op.Body.HasID {
					args = append(args, "--id", "res-1")
					wantID = "res-1"
				}
				for _, f := range op.Body.Fields {
					a, want := sample(f.Name, f.Type, f.ItemType, f.Enum)
					args = append(args, a...)
					wantAttrs[f.Name] = want
				}
			}
			if cmd.Flags().Lookup("yes") != nil {
				args = append(args, "--yes")
			}

			fresh := testRoot(t, sp, srv.URL, fastCfg())
			srv.reply = nil
			if op.Collection {
				srv.reply = func(*http.Request, seen) (int, string) { return 200, `{"data":[]}` }
			}
			var stdout, stderr strings.Builder
			fresh.SetOut(&stdout)
			fresh.SetErr(&stderr)
			fresh.SetArgs(args)
			if err := fresh.Execute(); err != nil {
				t.Fatalf("%v: %v\nstderr: %s", args, err, stderr.String())
			}
			reqs := srv.requests()
			if len(reqs) != before+1 {
				t.Fatalf("expected exactly one request, got %d", len(reqs)-before)
			}
			got := reqs[len(reqs)-1]

			if got.Method != op.Method {
				t.Errorf("method = %s, want %s", got.Method, op.Method)
			}
			if got.Path != wantPath {
				t.Errorf("path = %s, want %s", got.Path, wantPath)
			}
			var gotQuery []string
			for k := range got.Query {
				gotQuery = append(gotQuery, k)
			}
			var wq []string
			for k := range wantQuery {
				wq = append(wq, k)
			}
			sort.Strings(gotQuery)
			sort.Strings(wq)
			if !reflect.DeepEqual(gotQuery, wq) {
				t.Errorf("query names = %v, want %v", gotQuery, wq)
			}
			if got.Auth != "Bearer tok-123" || got.Accept != "application/vnd.api+json" {
				t.Errorf("auth/accept = %q / %q", got.Auth, got.Accept)
			}

			if op.Body == nil {
				if len(got.Body) != 0 {
					t.Errorf("unexpected body %s", got.Body)
				}
				return
			}
			if got.ContentType != op.Body.ContentType {
				t.Errorf("content type = %q, want %q", got.ContentType, op.Body.ContentType)
			}
			doc := decode(t, got.Body)
			if len(doc) != 1 {
				t.Errorf("body members = %v, want only data", doc)
			}
			if op.Body.Linkage {
				want := []any{
					map[string]any{"type": op.Body.DataType, "id": "x1"},
					map[string]any{"type": op.Body.DataType, "id": "x2"},
				}
				if !reflect.DeepEqual(doc["data"], want) {
					t.Errorf("linkage data = %v, want %v", doc["data"], want)
				}
				return
			}
			data, _ := doc["data"].(map[string]any)
			if op.Body.DataType != "" && data["type"] != op.Body.DataType {
				t.Errorf("data.type = %v, want %s", data["type"], op.Body.DataType)
			}
			if op.Body.DataType == "" && data["type"] != nil {
				t.Errorf("data.type = %v, want none", data["type"])
			}
			if wantID != "" && data["id"] != wantID {
				t.Errorf("data.id = %v, want %s", data["id"], wantID)
			}
			if wantID == "" && data["id"] != nil {
				t.Errorf("unexpected data.id %v", data["id"])
			}
			for k := range data {
				if k != "type" && k != "id" && k != "attributes" {
					t.Errorf("undocumented data member %q", k)
				}
			}
			attrs, _ := data["attributes"].(map[string]any)
			if !reflect.DeepEqual(attrs, wantAttrs) && !(len(attrs) == 0 && len(wantAttrs) == 0) {
				t.Errorf("attributes = %v, want %v", attrs, wantAttrs)
			}
		})
	}
}

// TestNewOperationNeedsNoCode proves the generator is driven by the contract:
// an operation added to the YAML becomes a working command.
func TestNewOperationNeedsNoCode(t *testing.T) {
	data := string(rawPinned(t)) + `
  /gizmos/{gizmo_id}/frobnications:
    post:
      summary: Frobnicate a gizmo
      operationId: createFrobnication
      security:
        - apiToken: []
      parameters:
        - name: gizmo_id
          in: path
          required: true
          schema:
            type: string
        - name: dry
          in: query
          schema:
            type: boolean
      requestBody:
        required: true
        content:
          application/vnd.api+json:
            schema:
              type: object
              properties:
                data:
                  type: object
                  properties:
                    type:
                      type: string
                      enum: [frobnications]
                    attributes:
                      type: object
                      required: [level]
                      properties:
                        level:
                          type: integer
                        label:
                          type: string
      responses:
        "201":
          description: ok
`
	sp, err := spec.Load([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	srv := newStub(t)
	stdout, _, err := run(t, sp, srv.URL, "gizmos", "frobnications", "create", "g9", "--level", "3", "--label", "x", "--dry=true", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != `{"data":{}}`+"\n" {
		t.Errorf("stdout = %q", stdout)
	}
	got := srv.requests()[0]
	if got.Method != "POST" || got.Path != "/gizmos/g9/frobnications" || got.Query["dry"][0] != "true" {
		t.Errorf("request = %+v", got)
	}
	if string(got.Body) != `{"data":{"type":"frobnications","attributes":{"label":"x","level":3}}}` {
		t.Errorf("body = %s", got.Body)
	}
	if _, _, err := run(t, sp, srv.URL, "gizmos", "frobnications", "create", "g9"); err == nil || !strings.Contains(err.Error(), `"level"`) {
		t.Errorf("required --level not enforced: %v", err)
	}
}

// TestOperationAnnotations pins the annotations ctlkit's ai reference prints:
// exactly operation_id, method and path (plus webhook) from the contract.
func TestOperationAnnotations(t *testing.T) {
	sp := pinnedSpec(t)
	byOp := commandsByOperation(testRoot(t, sp, "http://unused.invalid", fastCfg()))
	for _, op := range sp.Operations {
		c, ok := byOp[op.ID]
		if !ok {
			t.Fatalf("operation %s has no command", op.ID)
		}
		want := map[string]string{AnnOperationID: op.ID, AnnMethod: op.Method, AnnPath: op.Path}
		if c.Hidden {
			want[AnnWebhook] = "true"
		}
		if op.Body != nil && len(op.Body.RequiredByCategory) > 0 {
			enc, _ := json.Marshal(op.Body.RequiredByCategory)
			want[AnnRequiredByCategory] = string(enc)
		}
		if !reflect.DeepEqual(c.Annotations, want) {
			t.Errorf("%s: annotations = %v, want %v", op.ID, c.Annotations, want)
		}
	}
}

// TestRequiredInputsAreRequiredFlags checks that required query parameters,
// body fields and the linkage ids carry cobra's required annotation, and that
// optional and defaulted ones do not.
func TestRequiredInputsAreRequiredFlags(t *testing.T) {
	isRequired := func(c *cobra.Command, name string) bool {
		f := c.Flags().Lookup(name)
		if f == nil {
			t.Fatalf("%s: no --%s flag", c.CommandPath(), name)
		}
		return len(f.Annotations[cobra.BashCompOneRequiredFlag]) == 1
	}
	sp := pinnedSpec(t)
	byOp := commandsByOperation(testRoot(t, sp, "http://unused.invalid", fastCfg()))
	var required int
	for _, op := range sp.Operations {
		c := byOp[op.ID]
		for _, q := range op.Query {
			if got := isRequired(c, q.Name); got != q.Required {
				t.Errorf("%s --%s: required = %v, want %v", op.ID, q.Name, got, q.Required)
			}
		}
		if op.Body == nil {
			continue
		}
		if op.Body.HasID && isRequired(c, "id") {
			t.Errorf("%s: --id must not be required", op.ID)
		}
		if op.Body.Linkage && !isRequired(c, linkageFlag) {
			t.Errorf("%s: --%s must be required", op.ID, linkageFlag)
		}
		for _, f := range op.Body.Fields {
			want := f.Required && !f.HasDefault
			if got := isRequired(c, flagOf(f.Name)); got != want {
				t.Errorf("%s --%s: required = %v, want %v", op.ID, f.Name, got, want)
			}
			if want {
				required++
			}
		}
	}
	if required == 0 {
		t.Error("the pinned contract has no required body field to check")
	}
}
