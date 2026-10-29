package render

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/dotdevlabs/ctlkit/pkg/clierror"
)

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestSuccessJSONIsByteFaithful(t *testing.T) {
	for _, body := range []string{"{ \"data\" :  {\"id\":\"1\"} }", "{\"data\":[]}\n"} {
		var out, errb bytes.Buffer
		if err := Success(&out, &errb, Options{JSON: true}, 200, []byte(body)); err != nil {
			t.Fatal(err)
		}
		if got, want := out.String(), strings.TrimSuffix(body, "\n")+"\n"; got != want {
			t.Errorf("got %q want %q", got, want)
		}
		if errb.Len() != 0 {
			t.Errorf("stderr = %q", errb.String())
		}
	}
	var out, errb bytes.Buffer
	if err := Success(&out, &errb, Options{JSON: true}, 204, nil); err != nil || out.Len() != 0 {
		t.Errorf("empty body: %q %v", out.String(), err)
	}
	if err := Success(failWriter{}, &errb, Options{JSON: true}, 200, []byte("{}")); err == nil {
		t.Error("write error swallowed")
	}
	if err := Success(failWriter{}, &errb, Options{JSON: true}, 200, []byte("{}\n")); err == nil {
		t.Error("write error swallowed")
	}
}

func TestSuccessHuman(t *testing.T) {
	render := func(body string) (string, string) {
		var out, errb bytes.Buffer
		if err := Success(&out, &errb, Options{}, 200, []byte(body)); err != nil {
			t.Fatal(err)
		}
		return out.String(), errb.String()
	}

	out, _ := render(`{"data":{"type":"work-items","id":"7","attributes":{"title":"Hi","tags":["a"],"body":null,"n":3}}}`)
	if out != "type: work-items\nid: 7\nbody: -\nn: 3\ntags: [\"a\"]\ntitle: Hi\n" {
		t.Errorf("resource = %q", out)
	}

	out, hint := render(`{"data":[{"id":"1","attributes":{"title":"First","status":"open","zeta":"z","nested":{"a":1}}},{"id":"2","attributes":{"title":"` + strings.Repeat("long ", 30) + `","status":null,"a":"x","b":"x","c":"x","d":"x"}}],"links":{"next":"/more"}}`)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 {
		t.Fatalf("table = %q", out)
	}
	if !strings.HasPrefix(lines[0], "ID") || !strings.Contains(lines[0], "title") || !strings.Contains(lines[0], "status") || strings.Contains(lines[0], "nested") {
		t.Errorf("header = %q", lines[0])
	}
	if !strings.Contains(lines[3], "…") || !strings.Contains(lines[3], "-") {
		t.Errorf("row 2 = %q", lines[3])
	}
	if !strings.Contains(hint, "--all") {
		t.Errorf("hint = %q", hint)
	}
	if out, hint := render(`{"data":[],"links":{"next":null}}`); out != "No results.\n" || hint != "" {
		t.Errorf("empty = %q / %q", out, hint)
	}
	if out, _ := render(``); out != "OK (HTTP 200)\n" {
		t.Errorf("no body = %q", out)
	}
	if out, _ := render(`not json`); out != "not json\n" {
		t.Errorf("non-json = %q", out)
	}
	if out, _ := render(`{"meta":{"a":1}}`); out != "{\"meta\":{\"a\":1}}\n" {
		t.Errorf("no data = %q", out)
	}
}

func TestSuccessTemplate(t *testing.T) {
	var out, errb bytes.Buffer
	err := Success(&out, &errb, Options{Format: `{{.data.id}}`}, 200, []byte(`{"data":{"id":"42"}}`))
	if err != nil || strings.TrimSpace(out.String()) != "42" {
		t.Errorf("template = %q, %v", out.String(), err)
	}
}

func TestFailure(t *testing.T) {
	body := `{"errors":[{"title":"Invalid","detail":"can't be blank","source":{"pointer":"/data/attributes/title"}},{"code":"X1","source":{"parameter":"q"}},{"title":"Only title"}]}`
	var out bytes.Buffer
	err := Failure(&out, Options{JSON: true}, 422, []byte(body))
	var ce *clierror.CLIError
	if !errors.As(err, &ce) || ce.Code != clierror.CodeBadRequest {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{
		"HTTP 422 Unprocessable Entity",
		"- Invalid: can't be blank (at /data/attributes/title)",
		"- error [X1] (parameter q)",
		"- Only title",
	} {
		if !strings.Contains(ce.Message, want) {
			t.Errorf("message lacks %q:\n%s", want, ce.Message)
		}
	}
	if out.String() != body+"\n" {
		t.Errorf("--json stdout = %q", out.String())
	}

	out.Reset()
	if err := Failure(&out, Options{}, 500, []byte("<html>boom</html>")); !strings.Contains(err.Error(), "<html>boom</html>") || out.Len() != 0 {
		t.Errorf("raw body: %v / stdout %q", err, out.String())
	}
	long := strings.Repeat("é", 400)
	if m := Message(502, []byte(long)); !strings.HasSuffix(m, "…") || len(m) > 700 {
		t.Errorf("long body not truncated: %d bytes", len(m))
	}
	if m := Message(404, nil); m != "HTTP 404 Not Found" {
		t.Errorf("empty body = %q", m)
	}
	if err := Failure(failWriter{}, Options{JSON: true}, 400, []byte("{}")); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("write error: %v", err)
	}
}

func TestExitCode(t *testing.T) {
	for status, want := range map[int]clierror.ErrorCode{
		401: clierror.CodeUnauthorized, 403: clierror.CodeForbidden, 404: clierror.CodeNotFound, 409: clierror.CodeConflict,
		400: clierror.CodeBadRequest, 422: clierror.CodeBadRequest, 429: clierror.CodeBadRequest,
		500: clierror.CodeServerError, 503: clierror.CodeServerError, 302: clierror.CodeServerError,
	} {
		if got := ExitCode(status); got != want {
			t.Errorf("ExitCode(%d) = %d, want %d", status, got, want)
		}
	}
}

func TestDryRun(t *testing.T) {
	info := DryRunInfo{Method: "POST", URL: "http://h/x", Headers: map[string]string{"B": "2", "A": "1"}, Body: []byte(`{"a":1}`)}
	var out bytes.Buffer
	if err := DryRun(&out, Options{}, info); err != nil {
		t.Fatal(err)
	}
	if out.String() != "POST http://h/x\nA: 1\nB: 2\n\n{\"a\":1}\n" {
		t.Errorf("human = %q", out.String())
	}
	out.Reset()
	info.Body = nil
	if err := DryRun(&out, Options{}, info); err != nil || strings.Contains(out.String(), "\n\n") {
		t.Errorf("no body = %q %v", out.String(), err)
	}
	out.Reset()
	if err := DryRun(&out, Options{JSON: true}, info); err != nil || !strings.Contains(out.String(), `"method": "POST"`) {
		t.Errorf("json = %q %v", out.String(), err)
	}
	_ = io.Discard
}
