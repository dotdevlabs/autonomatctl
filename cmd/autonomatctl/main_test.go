package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dotdevlabs/ctlkit/pkg/airef"

	"github.com/dotdevlabs/autonomatctl/internal/gen"
	"github.com/dotdevlabs/autonomatctl/internal/schema"
	"github.com/dotdevlabs/autonomatctl/internal/spec"
)

type recorded struct {
	Method, URI, Auth string
	Body              string
}

type server struct {
	*httptest.Server
	mu    sync.Mutex
	reqs  []recorded
	reply func(r *http.Request) (int, string)
}

func newServer(t *testing.T) *server {
	t.Helper()
	s := &server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.reqs = append(s.reqs, recorded{r.Method, r.RequestURI, r.Header.Get("Authorization"), string(b)})
		s.mu.Unlock()
		status, body := 200, `{"data":{"type":"status","id":"1","attributes":{"ok":true}},"links":{"self":"/status"}}`
		if s.reply != nil {
			status, body = s.reply(r)
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *server) last(t *testing.T) recorded {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reqs) == 0 {
		t.Fatal("no request reached the server")
	}
	return s.reqs[len(s.reqs)-1]
}

func (s *server) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reqs)
}

// sandbox isolates HOME (contexts live in ~/.config/atmt) and the
// AUTONOMATCTL_* environment.
func sandbox(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	for _, k := range []string{"AUTONOMATCTL_TOKEN", "AUTONOMATCTL_CONTEXT", "AUTONOMATCTL_BASE_URL"} {
		t.Setenv(k, "")
	}
}

func cli(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errb bytes.Buffer
	code = run(args, &out, &errb)
	return out.String(), errb.String(), code
}

func login(t *testing.T, name, url, token string, current bool) {
	t.Helper()
	args := []string{"auth", "login", "--name", name, "--url", url, "--token", token}
	if current {
		args = append(args, "--set-current")
	}
	if _, errOut, code := cli(t, args...); code != 0 {
		t.Fatalf("auth login: %d %s", code, errOut)
	}
}

func TestJSONOutputIsTheRawDocument(t *testing.T) {
	sandbox(t)
	srv := newServer(t)
	raw := "{\n \"data\": {\"id\":\"1\", \"type\":\"status\"},\n \"links\":{\"self\":\"/status\"}\n}"
	srv.reply = func(*http.Request) (int, string) { return 200, raw }
	login(t, "prod", srv.URL, "tok-prod", true)

	out, errOut, code := cli(t, "status", "get", "--json")
	if code != 0 || out != raw+"\n" || errOut != "" {
		t.Errorf("code %d\nstdout %q\nstderr %q", code, out, errOut)
	}
	if r := srv.last(t); r.Method != "GET" || r.URI != "/status" || r.Auth != "Bearer tok-prod" {
		t.Errorf("request = %+v", r)
	}

	out, _, code = cli(t, "status", "get")
	if code != 0 || out != "type: status\nid: 1\n" {
		t.Errorf("human output (code %d) = %q", code, out)
	}
}

func TestContextsAndEnvironment(t *testing.T) {
	sandbox(t)
	a, b := newServer(t), newServer(t)
	login(t, "a", a.URL, "tok-a", true)
	login(t, "b", b.URL, "tok-b", false)

	check := func(name string, srv *server, auth string, args ...string) {
		t.Helper()
		before := srv.count()
		if _, errOut, code := cli(t, args...); code != 0 {
			t.Fatalf("%s: exit %d %s", name, code, errOut)
		}
		if srv.count() != before+1 || srv.last(t).Auth != auth {
			t.Errorf("%s: request went elsewhere or with %q", name, srv.last(t).Auth)
		}
	}
	check("current context", a, "Bearer tok-a", "status", "get")
	check("--context", b, "Bearer tok-b", "status", "get", "--context", "b")

	t.Setenv("AUTONOMATCTL_CONTEXT", "b")
	check("AUTONOMATCTL_CONTEXT", b, "Bearer tok-b", "status", "get")
	check("--context beats AUTONOMATCTL_CONTEXT", a, "Bearer tok-a", "status", "get", "--context", "a")

	t.Setenv("AUTONOMATCTL_TOKEN", "run-scoped")
	check("AUTONOMATCTL_TOKEN", b, "Bearer run-scoped", "status", "get")

	t.Setenv("AUTONOMATCTL_BASE_URL", a.URL)
	check("AUTONOMATCTL_BASE_URL", a, "Bearer run-scoped", "status", "get")

	t.Setenv("AUTONOMATCTL_BASE_URL", "ftp://nope")
	if _, errOut, code := cli(t, "status", "get"); code != 8 || !strings.Contains(errOut, "invalid base URL") {
		t.Errorf("invalid base URL: %d %q", code, errOut)
	}

	if _, errOut, code := cli(t, "status", "get", "--context", "missing"); code != 1 || !strings.Contains(errOut, "not found") {
		t.Errorf("unknown context: %d %q", code, errOut)
	}
}

func TestEnvironmentOnlyAgentRun(t *testing.T) {
	sandbox(t) // no contexts at all
	srv := newServer(t)

	if _, errOut, code := cli(t, "status", "get"); code != 8 || !strings.Contains(errOut, "no context selected") {
		t.Errorf("without anything: %d %q", code, errOut)
	}
	t.Setenv("AUTONOMATCTL_BASE_URL", srv.URL)
	if _, _, code := cli(t, "status", "get"); code != 8 {
		t.Errorf("base URL alone must not be enough: %d", code)
	}
	t.Setenv("AUTONOMATCTL_TOKEN", "run-token")
	out, errOut, code := cli(t, "status", "get", "--json", "--verbose")
	if code != 0 || !strings.Contains(out, `"ok":true`) {
		t.Fatalf("env-only run: %d %q %q", code, out, errOut)
	}
	if r := srv.last(t); r.Auth != "Bearer run-token" {
		t.Errorf("auth = %q", r.Auth)
	}
	if !strings.Contains(errOut, "GET "+srv.URL+"/status -> 200") || strings.Contains(errOut, "run-token") {
		t.Errorf("verbose = %q", errOut)
	}
	// global flags still reach the command
	out, _, code = cli(t, "status", "get", "--dry-run", "--json")
	if code != 0 || !strings.Contains(out, `"method": "GET"`) || srv.count() != 1 {
		t.Errorf("dry-run: %d %q (requests %d)", code, out, srv.count())
	}
	// an explicit unknown --context is still an error, not a silent env fallback
	if _, _, code := cli(t, "status", "get", "--context", "nope"); code != 1 {
		t.Errorf("--context nope: %d", code)
	}
}

func TestErrorsAndExitCodes(t *testing.T) {
	sandbox(t)
	srv := newServer(t)
	login(t, "x", srv.URL, "t", true)
	cases := []struct {
		status int
		code   int
	}{{401, 2}, {403, 3}, {404, 1}, {409, 5}, {422, 4}, {500, 6}, {503, 6}}
	for _, tc := range cases {
		body := `{"errors":[{"title":"Broken thing","source":{"pointer":"/data/attributes/title"}}]}`
		srv.reply = func(*http.Request) (int, string) { return tc.status, body }
		out, errOut, code := cli(t, "work-items", "get", "9")
		if code != tc.code || out != "" || !strings.Contains(errOut, "Broken thing (at /data/attributes/title)") || !strings.Contains(errOut, "HTTP") {
			t.Errorf("%d: exit %d stdout %q stderr %q", tc.status, code, out, errOut)
		}
		out, _, code = cli(t, "work-items", "get", "9", "--json")
		if code != tc.code || out != body+"\n" {
			t.Errorf("%d --json: exit %d stdout %q", tc.status, code, out)
		}
	}
}

func TestPaginationAllThroughTheCLI(t *testing.T) {
	sandbox(t)
	srv := newServer(t)
	login(t, "x", srv.URL, "t", true)
	srv.reply = func(r *http.Request) (int, string) {
		switch r.URL.Query().Get("page[number]") {
		case "":
			return 200, `{"data":[{"id":"1"}],"links":{"next":"` + srv.URL + `/work_items?page%5Bnumber%5D=2"}}`
		case "2":
			return 200, `{"data":[{"id":"2"}],"links":{"next":"/work_items?page%5Bnumber%5D=3"}}`
		default:
			return 200, `{"data":[{"id":"3"}],"links":{}}`
		}
	}
	out, _, code := cli(t, "work-items", "list", "--all", "--json")
	if code != 0 {
		t.Fatal(code)
	}
	var doc struct {
		Data []struct{ ID string }
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.Data) != 3 || doc.Data[2].ID != "3" {
		t.Errorf("merged = %s (%v)", out, err)
	}
	var uris []string
	for _, r := range srv.reqs {
		uris = append(uris, r.URI)
	}
	if strings.Join(uris, " ") != "/work_items /work_items?page%5Bnumber%5D=2 /work_items?page%5Bnumber%5D=3" {
		t.Errorf("uris = %v", uris)
	}
	// default: exactly one page, links.next visible in the document
	n := srv.count()
	out, _, _ = cli(t, "work-items", "list", "--json")
	if srv.count() != n+1 || !strings.Contains(out, `"next"`) {
		t.Errorf("default list fetched %d pages: %s", srv.count()-n, out)
	}
}

func TestWriteSendsOnlyDocumentedFields(t *testing.T) {
	sandbox(t)
	srv := newServer(t)
	login(t, "x", srv.URL, "t", true)
	_, errOut, code := cli(t, "work-items", "create", "--item_type", "report", "--title", "T", "--source_kind", "human", "--status", "open", "--tags", "a")
	if code != 0 {
		t.Fatalf("%d %s", code, errOut)
	}
	if r := srv.last(t); r.Method != "POST" || r.URI != "/work_items" ||
		r.Body != `{"data":{"type":"work-items","attributes":{"item_type":"report","source_kind":"human","status":"open","tags":["a"],"title":"T"}}}` {
		t.Errorf("request = %+v", r)
	}
	n := srv.count()
	for _, args := range [][]string{
		{"work-items", "create", "--title", "T", "--bogus", "x"},
		{"work-items", "create", "--title", "T"},
		{"work-items", "bogus"},
		{"bogus"},
		{"products", "sunset", "p1"},
	} {
		if _, _, code := cli(t, args...); code != 8 {
			t.Errorf("%v: exit %d, want 8", args, code)
		}
	}
	if srv.count() != n {
		t.Error("a request was sent for invalid input")
	}
}

func TestAIReference(t *testing.T) {
	sandbox(t) // no context: ai must work offline
	sp, err := schema.Spec()
	if err != nil {
		t.Fatal(err)
	}
	j1, errOut, code := cli(t, "ai", "--json")
	if code != 0 || errOut != "" {
		t.Fatalf("ai --json: %d %q", code, errOut)
	}
	j2, _, _ := cli(t, "ai", "--json")
	if j1 != j2 {
		t.Error("ai --json is not byte-stable")
	}
	var ref airef.Reference
	dec := json.NewDecoder(strings.NewReader(j1))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ref); err != nil {
		t.Fatalf("ai --json is not a ctlkit Reference: %v", err)
	}
	if ref.Product != product || len(ref.Commands) == 0 || !reflect.DeepEqual(ref.Workflows, workflows()) {
		t.Errorf("reference = %s %s, %d commands, %d workflows", ref.Product, ref.Version, len(ref.Commands), len(ref.Workflows))
	}

	// Every visible contract operation appears with its exact annotations.
	root, err := buildRoot(sp)
	if err != nil {
		t.Fatal(err)
	}
	hidden := map[string]bool{}
	var walkCmd func(*cobra.Command)
	walkCmd = func(c *cobra.Command) {
		for _, s := range c.Commands() {
			if s.Hidden {
				hidden[s.Annotations[gen.AnnOperationID]] = true
			}
			walkCmd(s)
		}
	}
	walkCmd(root)
	byOp := map[string]airef.CommandRef{}
	var walkRef func([]airef.CommandRef)
	walkRef = func(cs []airef.CommandRef) {
		for _, c := range cs {
			if id := c.Annotations[gen.AnnOperationID]; id != "" {
				byOp[id] = c
			}
			walkRef(c.Subcommands)
		}
	}
	walkRef(ref.Commands)
	for _, op := range sp.Operations {
		c, ok := byOp[op.ID]
		if !ok {
			if !hidden[op.ID] {
				t.Errorf("ai --json lacks visible operation %s", op.ID)
			}
			continue
		}
		if c.Annotations[gen.AnnMethod] != op.Method || c.Annotations[gen.AnnPath] != op.Path {
			t.Errorf("%s: annotations %v, want %s %s", op.ID, c.Annotations, op.Method, op.Path)
		}
	}
	var sawRequired bool
	for _, f := range byOp["createWorkItem"].Flags {
		if f.Name == "title" && f.Required {
			sawRequired = true
		}
	}
	if !sawRequired {
		t.Error("createWorkItem --title not marked required in the reference")
	}

	md, errOut, code := cli(t, "ai")
	if code != 0 || errOut != "" {
		t.Fatalf("ai: %d %q", code, errOut)
	}
	for _, want := range []string{"Common Workflows", "Triage work items", "work-items"} {
		if !strings.Contains(md, want) {
			t.Errorf("ai markdown lacks %q", want)
		}
	}
}

func TestMissingRequiredFlagIsUsageError(t *testing.T) {
	sandbox(t)
	srv := newServer(t)
	login(t, "t", srv.URL, "tok", true)
	n := srv.count()
	_, errOut, code := cli(t, "work-items", "create")
	if code != 8 || !strings.Contains(errOut, "required flag") {
		t.Errorf("exit %d, stderr %q; want 8 with a required-flag message", code, errOut)
	}
	if srv.count() != n {
		t.Error("a request was sent for a missing required flag")
	}
}

func TestWorkflowsReferenceRealCommands(t *testing.T) {
	root, err := newRoot()
	if err != nil {
		t.Fatal(err)
	}
	for _, wf := range workflows() {
		for _, step := range wf.Steps {
			fields := strings.Fields(step)
			for len(fields) > 0 && strings.Contains(fields[0], "=") { // env prefix
				fields = fields[1:]
			}
			if len(fields) < 2 || fields[0] != "autonomatctl" {
				t.Errorf("%s: step %q does not start with autonomatctl", wf.Name, step)
				continue
			}
			c, rest, err := root.Find(fields[1:])
			if err != nil || !c.Runnable() || c.HasSubCommands() {
				t.Errorf("%s: step %q does not resolve to a command (%v)", wf.Name, step, err)
				continue
			}
			// every --flag in the step exists on the command
			for _, f := range rest {
				if name, ok := strings.CutPrefix(f, "--"); ok {
					name, _, _ = strings.Cut(name, "=")
					if c.Flags().Lookup(name) == nil && root.PersistentFlags().Lookup(name) == nil {
						t.Errorf("%s: step %q uses unknown flag --%s", wf.Name, step, name)
					}
				}
			}
		}
	}
}

func TestOfflineCommands(t *testing.T) {
	sandbox(t)
	out, _, code := cli(t, "schema", "--json")
	if code != 0 || !strings.Contains(out, schema.SHA()) {
		t.Errorf("schema: %d %q", code, out)
	}
	if out, _, code := cli(t, "work-items"); code != 0 || !strings.Contains(out, "Available Commands") {
		t.Errorf("group help: %d %q", code, out)
	}
	if out, _, code := cli(t, "help", "work-items", "create"); code != 0 || !strings.Contains(out, "--item_type") {
		t.Errorf("help: %d", code)
	}
	if _, _, code := cli(t, "completion", "bash"); code != 0 {
		t.Errorf("completion: %d", code)
	}
	out, _, code = cli(t, "version")
	if code != 0 || !strings.HasPrefix(out, "autonomatctl version") {
		t.Errorf("version: %d %q", code, out)
	}
	if out, _, code := cli(t, "context", "list"); code != 0 {
		t.Errorf("context list: %d %q", code, out)
	}
}

func TestHelpShowsContractFieldNames(t *testing.T) {
	sandbox(t)
	out, _, _ := cli(t, "work-items", "create", "--help")
	for _, f := range []string{"--item_type", "--source_kind", "--tags", "--title", "--status"} {
		if !strings.Contains(out, f) {
			t.Errorf("help lacks %s", f)
		}
	}
	out, _, _ = cli(t, "--help")
	if strings.Contains(out, "webhooks") {
		t.Error("webhook group should be hidden from the default help")
	}
	if !strings.Contains(out, "work-items") {
		t.Error("root help lacks work-items")
	}
}

func TestGenerationFailureIsReported(t *testing.T) {
	sp, err := spec.Load([]byte("openapi: 3.0.3\npaths:\n  /auth:\n    get:\n      operationId: getAuth\n      responses: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildRoot(sp); err == nil || !strings.Contains(err.Error(), "built-in") {
		t.Errorf("collision with a built-in not reported: %v", err)
	}
}

func TestReservedNamesStayInSync(t *testing.T) {
	root, err := newRoot()
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, name := range builtins {
		if !have[name] && name != "help" && name != "completion" {
			t.Errorf("reserved name %q is not a command", name)
		}
	}
}
