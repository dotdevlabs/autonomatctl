package gen

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dotdevlabs/ctlkit/pkg/clierror"
	"github.com/dotdevlabs/ctlkit/pkg/ctxutil"

	"github.com/dotdevlabs/autonomatctl/internal/spec"
)

const kinds = `
openapi: "3.0.3"
info: {title: K, version: "1"}
components:
  schemas:
    Doc:
      type: object
      properties:
        data:
          type: object
          properties:
            type: {type: string, enum: [widgets]}
            id: {type: string}
            attributes:
              type: object
              required: [name]
              properties:
                name: {type: string, description: "  A   multi
                  line name  "}
                kind: {type: string, enum: [big, small]}
                count: {type: integer}
                ratio: {type: number}
                on: {type: boolean}
                tags: {type: array, items: {type: string}}
                nums: {type: array, items: {type: integer}}
                extra: {type: object}
paths:
  /widgets:
    get:
      operationId: listWidgets
      parameters:
        - {name: q, in: query, schema: {type: string}}
        - {name: limit, in: query, schema: {type: integer}}
        - {name: ratio, in: query, schema: {type: number}}
        - {name: flag, in: query, schema: {type: boolean}}
        - {name: ids, in: query, schema: {type: array, items: {type: string}}}
        - {name: filter, in: query, schema: {type: object}}
        - {name: must, in: query, required: true, schema: {type: string}}
      responses:
        "200": {content: {application/vnd.api+json: {schema: {properties: {data: {type: array}}}}}}
    post:
      operationId: createWidget
      requestBody: {required: true, content: {application/vnd.api+json: {schema: {$ref: "#/components/schemas/Doc"}}}}
      responses: {"201": {description: ok}}
  /widgets/{id}:
    get:
      operationId: getWidget
      responses: {"200": {description: ok}}
    delete:
      operationId: deleteWidget
      responses: {"204": {description: gone}}
  /widgets/{id}/retire:
    post:
      summary: Retire a widget (irreversible)
      operationId: retireWidget
      responses: {"200": {description: ok}}
`

func kindsSpec(t *testing.T) *spec.Spec {
	t.Helper()
	sp, err := spec.Load([]byte(kinds))
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

func exitCode(err error) int {
	var ce *clierror.CLIError
	if errors.As(err, &ce) {
		return ce.ExitCode()
	}
	return -1
}

func TestFlagKinds(t *testing.T) {
	srv := newStub(t)
	sp := kindsSpec(t)

	_, _, err := run(t, sp, srv.URL, "widgets", "create", "--id", "w1", "--name", "n", "--kind", "big", "--count", "3",
		"--ratio", "0.25", "--on=true", "--tags", "x", "--tags", "y", "--nums", "[1,2]", "--extra", `{"a":[1]}`)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"data":{"type":"widgets","id":"w1","attributes":{"count":3,"extra":{"a":[1]},"kind":"big","name":"n","nums":[1,2],"on":true,"ratio":0.25,"tags":["x","y"]}}}`
	if got := string(srv.requests()[0].Body); got != want {
		t.Errorf("body = %s\nwant   %s", got, want)
	}

	_, _, err = run(t, sp, srv.URL, "widgets", "list", "--must", "m", "--q", "a b", "--limit", "5", "--ratio", "1000000",
		"--flag", "--ids", "i1", "--ids", "i2", "--filter", "raw")
	if err != nil {
		t.Fatal(err)
	}
	q := srv.requests()[1].Query
	if q.Get("must") != "m" || q.Get("q") != "a b" || q.Get("limit") != "5" || q.Get("ratio") != "1000000" ||
		q.Get("flag") != "true" || strings.Join(q["ids"], ",") != "i1,i2" || q.Get("filter") != "raw" {
		t.Errorf("query = %v", q)
	}
}

func TestUsageErrors(t *testing.T) {
	srv := newStub(t)
	sp := kindsSpec(t)
	cases := map[string][]string{
		"missing required flag":  {"widgets", "create"},
		"missing required query": {"widgets", "list"},
		"bad enum":               {"widgets", "create", "--name", "n", "--kind", "huge"},
		"invalid json":           {"widgets", "create", "--name", "n", "--extra", "{"},
		"json wrong kind obj":    {"widgets", "create", "--name", "n", "--extra", "[1]"},
		"json wrong kind arr":    {"widgets", "create", "--name", "n", "--nums", "{}"},
		"too few args":           {"widgets", "get"},
		"too many args":          {"widgets", "get", "a", "b"},
		"empty arg":              {"widgets", "get", ""},
		"destructive w/o --yes":  {"widgets", "delete", "w1"},
		"action w/o --yes":       {"widgets", "retire", "w1"},
		"unknown group arg":      {"widgets", "nonsense"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			before := len(srv.requests())
			_, _, err := run(t, sp, srv.URL, args...)
			if err == nil {
				t.Fatal("expected an error")
			}
			if c := exitCode(err); c != int(clierror.CodeUsage) && !strings.HasPrefix(err.Error(), "required flag") {
				t.Errorf("exit code = %d (%v), want %d", c, err, clierror.CodeUsage)
			}
			if len(srv.requests()) != before {
				t.Error("a request was sent despite the usage error")
			}
		})
	}

	// null is accepted for JSON-typed values
	if _, _, err := run(t, sp, srv.URL, "widgets", "create", "--name", "n", "--extra", "null", "--nums", "null"); err != nil {
		t.Errorf("null: %v", err)
	}
}

func TestDestructiveWithYes(t *testing.T) {
	srv := newStub(t)
	sp := kindsSpec(t)
	if _, _, err := run(t, sp, srv.URL, "widgets", "delete", "w1", "--yes"); err != nil {
		t.Fatal(err)
	}
	if r := srv.requests()[0]; r.Method != "DELETE" || r.Path != "/widgets/w1" || len(r.Body) != 0 {
		t.Errorf("request = %+v", r)
	}
	// --yes is a CLI guard: never part of the request
	if _, _, err := run(t, sp, srv.URL, "widgets", "retire", "w1", "--yes"); err != nil {
		t.Fatal(err)
	}
	if r := srv.requests()[1]; r.Path != "/widgets/w1/retire" || len(r.Query) != 0 || len(r.Body) != 0 {
		t.Errorf("request = %+v", r)
	}
}

func TestDryRun(t *testing.T) {
	srv := newStub(t)
	sp := kindsSpec(t)
	out, _, err := run(t, sp, srv.URL, "widgets", "create", "--name", "n", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"POST " + srv.URL + "/widgets", "Authorization: Bearer <redacted>", `{"data":{"type":"widgets","attributes":{"name":"n"}}}`} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "tok-123") {
		t.Error("dry-run leaked the token")
	}
	out, _, err = run(t, sp, srv.URL, "widgets", "delete", "w1", "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if m := decode(t, []byte(out)); m["method"] != "DELETE" || m["url"] != srv.URL+"/widgets/w1" || m["body"] != nil {
		t.Errorf("dry-run json = %s", out)
	}
	if len(srv.requests()) != 0 {
		t.Error("dry-run sent a request")
	}
}

func TestRunErrors(t *testing.T) {
	sp := kindsSpec(t)
	srv := newStub(t)

	// no active context
	root := testRoot(t, sp, srv.URL, fastCfg())
	root.PersistentPreRunE = nil
	root.SetArgs([]string{"widgets", "get", "1"})
	if err := root.Execute(); exitCode(err) != int(clierror.CodeUsage) {
		t.Errorf("no context: %v", err)
	}

	// invalid / missing base URL
	for _, base := range []string{"", "ftp://x", "not a url"} {
		if _, _, err := run(t, sp, base, "widgets", "get", "1"); exitCode(err) != int(clierror.CodeUsage) {
			t.Errorf("base %q: %v", base, err)
		}
	}

	// transport failure
	dead := newStub(t)
	url := dead.URL
	dead.Close()
	if _, _, err := run(t, sp, url, "widgets", "get", "1"); exitCode(err) != int(clierror.CodeServerError) {
		t.Errorf("transport: %v", err)
	}
}

func TestStatusHandling(t *testing.T) {
	sp := kindsSpec(t)
	srv := newStub(t)
	cases := []struct {
		status int
		code   clierror.ErrorCode
	}{
		{401, clierror.CodeUnauthorized}, {403, clierror.CodeForbidden}, {404, clierror.CodeNotFound},
		{409, clierror.CodeConflict}, {422, clierror.CodeBadRequest}, {500, clierror.CodeServerError},
		{302, clierror.CodeServerError},
	}
	for _, tc := range cases {
		srv.reply = func(*http.Request, seen) (int, string) {
			return tc.status, `{"errors":[{"title":"Nope","source":{"pointer":"/data/attributes/name"}}]}`
		}
		stdout, _, err := run(t, sp, srv.URL, "widgets", "create", "--name", "n", "--json")
		if exitCode(err) != int(tc.code) {
			t.Errorf("%d: exit %d (%v), want %d", tc.status, exitCode(err), err, tc.code)
		}
		if !strings.Contains(err.Error(), "Nope (at /data/attributes/name)") {
			t.Errorf("%d: error does not show the errors body: %v", tc.status, err)
		}
		if !strings.Contains(stdout, `"errors"`) {
			t.Errorf("%d: --json stdout lacks the raw error body: %q", tc.status, stdout)
		}
	}
}

func TestAllFollowsNext(t *testing.T) {
	sp := kindsSpec(t)
	srv := newStub(t)
	srv.reply = func(r *http.Request, _ seen) (int, string) {
		if r.URL.Query().Get("page") == "2" {
			return 200, `{"data":[{"id":"2"}],"links":{"self":"x"}}`
		}
		return 200, `{"data":[{"id":"1"}],"links":{"next":"/widgets?must=m&page=2"}}`
	}
	out, _, err := run(t, sp, srv.URL, "widgets", "list", "--must", "m", "--all", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if out != `{"data":[{"id":"1"},{"id":"2"}],"links":{}}`+"\n" {
		t.Errorf("merged output = %s", out)
	}
	reqs := srv.requests()
	if len(reqs) != 2 || reqs[1].Query.Get("page") != "2" {
		t.Errorf("requests = %+v", reqs)
	}
	// without --all: one page, and a hint on stderr
	_, errOut, err := run(t, sp, srv.URL, "widgets", "list", "--must", "m")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "--all") {
		t.Errorf("no pagination hint: %q", errOut)
	}
	if len(srv.requests()) != 3 {
		t.Errorf("requests = %d, want 3", len(srv.requests()))
	}
}

func TestVerboseLogsWithoutToken(t *testing.T) {
	sp := kindsSpec(t)
	srv := newStub(t)
	_, errOut, err := run(t, sp, srv.URL, "widgets", "get", "7", "--verbose")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "GET "+srv.URL+"/widgets/7 -> 200") || strings.Contains(errOut, "tok-123") {
		t.Errorf("verbose = %q", errOut)
	}
	if ua := srv.requests()[0].UA; ua != "autonomatctl/test" {
		t.Errorf("user agent = %q", ua)
	}
}

func TestMissingToken(t *testing.T) {
	sp := kindsSpec(t)
	srv := newStub(t)
	root := testRoot(t, sp, srv.URL, fastCfg())
	pre := root.PersistentPreRunE
	root.PersistentPreRunE = func(c *cobra.Command, a []string) error {
		_ = pre(c, a)
		ac := ctxutil.ActiveContextFrom(c.Context())
		ac.Token = ""
		return nil
	}
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"widgets", "get", "1"})
	if err := root.Execute(); exitCode(err) != int(clierror.CodeUnauthorized) {
		t.Errorf("missing token: %v", err)
	}
	if len(srv.requests()) != 0 {
		t.Error("request sent without a token")
	}
}

func TestBuildErrors(t *testing.T) {
	op := func(path, method, id string) string {
		return "  " + path + ":\n    " + method + ":\n      operationId: " + id + "\n      responses: {}\n"
	}
	doc := func(parts ...string) string {
		return "openapi: 3.0.3\npaths:\n" + strings.Join(parts, "")
	}
	cases := map[string]struct {
		doc string
		cfg Config
	}{
		"reserved resource": {doc(op("/schema", "get", "getSchema")), Config{Reserved: []string{"schema"}}},
		"no resource":       {doc(op("/{id}", "get", "getId")), Config{}},
		"verb and fallback taken": {doc(
			op("/t", "get", "x"), op("/t/{id}", "get", "y"), op("/t/{a}/{b}", "get", "y")), Config{}},
		"leaf vs group":    {doc(op("/a", "post", "createA"), op("/a/{a_id}/create/{x}", "get", "z")), Config{}},
		"group vs leaf":    {doc(op("/a/{id}/get/b", "get", "x"), op("/a/{id}", "get", "getA")), Config{}},
		"reserved flag":    {"openapi: 3.0.3\npaths:\n  /a:\n    get:\n      operationId: g\n      parameters: [{name: json, in: query, schema: {type: string}}]\n      responses: {}\n", Config{}},
		"query/body clash": {"openapi: 3.0.3\npaths:\n  /a:\n    post:\n      operationId: g\n      parameters: [{name: x, in: query, schema: {type: string}}]\n      requestBody: {content: {application/vnd.api+json: {schema: {properties: {data: {properties: {attributes: {properties: {x: {type: string}}}}}}}}}}\n      responses: {}\n", Config{}},
		"id clash":         {"openapi: 3.0.3\npaths:\n  /a:\n    post:\n      operationId: g\n      parameters: [{name: id, in: query, schema: {type: string}}]\n      requestBody: {content: {application/vnd.api+json: {schema: {properties: {data: {properties: {id: {type: string}}}}}}}}\n      responses: {}\n", Config{}},
		"body field clash": {"openapi: 3.0.3\npaths:\n  /a:\n    post:\n      operationId: g\n      requestBody: {content: {application/vnd.api+json: {schema: {properties: {data: {properties: {attributes: {properties: {yes: {type: string}, attr-yes: {type: string}}}}}}}}}}\n      responses: {}\n", Config{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			sp, err := spec.Load([]byte(tc.doc))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Build(sp, tc.cfg); err == nil {
				t.Fatal("expected a generation error")
			}
		})
	}
}

func TestNameFallbackToOperationID(t *testing.T) {
	doc := `
openapi: 3.0.3
paths:
  /things:
    get: {operationId: listThings, responses: {}}
  /things/{id}:
    get: {operationId: getThing, responses: {}}
  /things/{a}/{b}:
    get: {operationId: getThingPair, responses: {}}
`
	sp, err := spec.Load([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	cmds, err := Build(sp, Config{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range cmds[0].Commands() {
		names = append(names, c.Name())
	}
	if strings.Join(names, ",") != "get,get-thing,get-thing-pair" {
		t.Errorf("names = %v", names)
	}
}

func TestDerive(t *testing.T) {
	cases := []struct {
		op    spec.Operation
		group string
		verb  string
	}{
		{spec.Operation{Method: "GET", Path: "/work_items", Collection: true}, "work-items", "list"},
		{spec.Operation{Method: "GET", Path: "/work_items/{id}"}, "work-items", "get"},
		{spec.Operation{Method: "GET", Path: "/account"}, "account", "get"},
		{spec.Operation{Method: "PATCH", Path: "/account"}, "account", "update"},
		{spec.Operation{Method: "PUT", Path: "/x/{id}"}, "x", "replace"},
		{spec.Operation{Method: "DELETE", Path: "/x/{id}"}, "x", "delete"},
		{spec.Operation{Method: "POST", Path: "/a/{a_id}/bs", Body: &spec.Body{}}, "a bs", "create"},
		{spec.Operation{Method: "POST", Path: "/a/{a_id}/do_it"}, "a", "do-it"},
		{spec.Operation{Method: "POST", Path: "/a/b"}, "a b", "create"},
	}
	for _, tc := range cases {
		g, v, err := derive(tc.op)
		if err != nil || strings.Join(g, " ") != tc.group || v != tc.verb {
			t.Errorf("%s %s -> %v %s (%v), want %s %s", tc.op.Method, tc.op.Path, g, v, err, tc.group, tc.verb)
		}
	}
	if _, _, err := derive(spec.Operation{Method: "TRACE", Path: "/a"}); err == nil {
		t.Error("unsupported method accepted")
	}
}

func TestKebab(t *testing.T) {
	for in, want := range map[string]string{
		"work_items": "work-items", "createWorkItem": "create-work-item", "getHTTPServer": "get-http-server",
		"plain": "plain", "a b": "a-b", "v2Thing": "v2-thing",
	} {
		if got := kebab(in); got != want {
			t.Errorf("kebab(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWebhookCommandsHidden(t *testing.T) {
	sp := pinnedSpec(t)
	root := testRoot(t, sp, "http://x", fastCfg())
	byOp := commandsByOperation(root)
	c := byOp["deliverAgentRunResults"]
	if !c.Hidden || c.Annotations[AnnWebhook] != "true" || !c.Parent().Hidden || !c.Parent().Parent().Hidden {
		t.Errorf("webhook command and its groups should be hidden")
	}
	if byOp["listWorkItems"].Hidden || byOp["listWorkItems"].Parent().Hidden {
		t.Error("work-items list must stay visible")
	}
}

func TestRequiredAttributeWithDefault(t *testing.T) {
	doc := `
openapi: 3.0.3
paths:
  /runs:
    post:
      operationId: createRun
      requestBody:
        content:
          application/vnd.api+json:
            schema:
              properties:
                data:
                  properties:
                    type: {type: string, enum: [runs]}
                    attributes:
                      required: [ids, label]
                      properties:
                        ids: {type: array, items: {type: integer}, default: []}
                        label: {type: string, default: "x"}
                        note: {type: string, default: "unsent"}
      responses: {}
`
	sp, err := spec.Load([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	srv := newStub(t)
	if _, _, err := run(t, sp, srv.URL, "runs", "create"); err != nil {
		t.Fatal(err)
	}
	if got := string(srv.requests()[0].Body); got != `{"data":{"type":"runs","attributes":{"ids":[],"label":"x"}}}` {
		t.Errorf("body = %s", got)
	}
	if _, _, err := run(t, sp, srv.URL, "runs", "create", "--label", "mine", "--ids", "[3]"); err != nil {
		t.Fatal(err)
	}
	if got := string(srv.requests()[1].Body); got != `{"data":{"type":"runs","attributes":{"ids":[3],"label":"mine"}}}` {
		t.Errorf("body = %s", got)
	}
}
