package gen

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dotdevlabs/ctlkit/pkg/config"
	"github.com/dotdevlabs/ctlkit/pkg/ctxutil"

	"github.com/dotdevlabs/autonomatctl/internal/apiclient"
	"github.com/dotdevlabs/autonomatctl/internal/schema"
	"github.com/dotdevlabs/autonomatctl/internal/spec"
)

// seen is one request as the stub server received it.
type seen struct {
	Method      string
	Path        string // escaped
	Query       url.Values
	Body        []byte
	ContentType string
	Accept      string
	Auth        string
	UA          string
}

type stub struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []seen
	// reply decides the response; the default is 200 {"data":{}}.
	reply func(r *http.Request, s seen) (status int, body string)
}

func newStub(t *testing.T) *stub {
	t.Helper()
	s := &stub{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec := seen{
			Method: r.Method, Path: r.URL.EscapedPath(), Query: r.URL.Query(), Body: body,
			ContentType: r.Header.Get("Content-Type"), Accept: r.Header.Get("Accept"),
			Auth: r.Header.Get("Authorization"), UA: r.Header.Get("User-Agent"),
		}
		s.mu.Lock()
		s.reqs = append(s.reqs, rec)
		s.mu.Unlock()
		status, out := 200, `{"data":{}}`
		if s.reply != nil {
			status, out = s.reply(r, rec)
		}
		w.Header().Set("Content-Type", apiclient.MediaType)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, out)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *stub) requests() []seen {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]seen(nil), s.reqs...)
}

func pinnedSpec(t *testing.T) *spec.Spec {
	t.Helper()
	sp, err := schema.Spec()
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

// testRoot mimics ctlkit's root: the same persistent flags, and a pre-run
// that puts the active context and global flags on the command context.
func testRoot(t *testing.T, sp *spec.Spec, baseURL string, cfg Config) *cobra.Command {
	t.Helper()
	cmds, err := Build(sp, cfg)
	if err != nil {
		t.Fatal(err)
	}
	root := &cobra.Command{Use: "autonomatctl", SilenceUsage: true, SilenceErrors: true}
	pf := root.PersistentFlags()
	pf.String("context", "", "")
	pf.Bool("json", false, "")
	pf.String("format", "", "")
	pf.Bool("dry-run", false, "")
	pf.Bool("verbose", false, "")
	root.PersistentPreRunE = func(c *cobra.Command, _ []string) error {
		var gf ctxutil.GlobalFlags
		gf.JSON, _ = c.Flags().GetBool("json")
		gf.Format, _ = c.Flags().GetString("format")
		gf.DryRun, _ = c.Flags().GetBool("dry-run")
		gf.Verbose, _ = c.Flags().GetBool("verbose")
		ctx := ctxutil.WithGlobalFlags(c.Context(), gf)
		ctx = ctxutil.WithActiveContext(ctx, &config.Context{BaseURL: baseURL, Token: "tok-123"})
		c.SetContext(ctx)
		return nil
	}
	for _, c := range cmds {
		root.AddCommand(c)
	}
	return root
}

func fastCfg() Config {
	return Config{
		Version: "test",
		Tune:    func(o *apiclient.Options) { o.Backoff = -1 },
	}
}

// run executes args against a fresh root built from sp.
func run(t *testing.T, sp *spec.Spec, baseURL string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := testRoot(t, sp, baseURL, fastCfg())
	var out, errb bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errb.String(), err
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("body %q is not JSON: %v", b, err)
	}
	return m
}

func rawPinned(t *testing.T) []byte {
	t.Helper()
	return schema.Raw()
}
