package schema

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestPinnedFiles(t *testing.T) {
	sha, err := os.ReadFile("testdata/api_spec.sha")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}\n$`).Match(sha) {
		t.Errorf("api_spec.sha = %q, want a 40-hex commit SHA and a newline", sha)
	}
	if SHA() != strings.TrimSpace(string(sha)) {
		t.Errorf("SHA() = %q", SHA())
	}
	raw, err := os.ReadFile("testdata/api_spec.yaml")
	if err != nil || !bytes.Equal(raw, Raw()) {
		t.Errorf("embedded spec differs from the file on disk (%v)", err)
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(Version()) {
		t.Errorf("contract version %q is not a semantic version", Version())
	}
	if Version() != "3.0.0" {
		t.Errorf("contract version = %q, want 3.0.0", Version())
	}
	sp, err := Spec()
	if err != nil || len(sp.Operations) == 0 {
		t.Fatalf("Spec() = %v, %v", sp, err)
	}
}

func runSchema(t *testing.T, args ...string) string {
	t.Helper()
	root := &cobra.Command{Use: "autonomatctl", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().Bool("json", false, "")
	root.AddCommand(NewCmd())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs(append([]string{"schema"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestSchemaCommand(t *testing.T) {
	human := runSchema(t)
	for _, want := range []string{"version: " + Version(), "sha: " + SHA(), "source: " + Source} {
		if !strings.Contains(human, want) {
			t.Errorf("human output lacks %q:\n%s", want, human)
		}
	}
	var info Info
	if err := json.Unmarshal([]byte(runSchema(t, "--json")), &info); err != nil {
		t.Fatal(err)
	}
	sp, _ := Spec()
	if info.Version != Version() || info.SHA != SHA() || info.Operations != len(sp.Operations) || info.OpenAPI == "" || info.Source != Source {
		t.Errorf("info = %+v", info)
	}
}

// TestSyncScript drives scripts/sync_spec.sh against a stub of the GitHub API.
func TestSyncScript(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available")
	}
	script, _ := filepath.Abs("../../scripts/sync_spec.sh")

	liveSHA := strings.Repeat("a", 40)
	liveSpec := "openapi: 3.0.3\ninfo: {title: T, version: \"9\"}\npaths: {}\n"
	srv := newGitHubStub(&liveSpec, &liveSHA)
	defer srv.Close()

	setup := func(t *testing.T, spec, sha string) (dest string) {
		t.Helper()
		dir := t.TempDir()
		dest = filepath.Join(dir, "api_spec.yaml")
		if err := os.WriteFile(dest, []byte(spec), 0o600); err != nil {
			t.Fatal(err)
		}
		if sha != "" {
			if err := os.WriteFile(filepath.Join(dir, "api_spec.sha"), []byte(sha+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return dest
	}
	run := func(dest string, env []string, args ...string) (string, int) {
		cmd := exec.Command(bash, append([]string{script}, args...)...)
		cmd.Env = append(env, "SPEC_API_BASE="+srv.URL, "SPEC_DEST="+dest, "HOME="+t.TempDir())
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), code
	}
	path := "PATH=" + os.Getenv("PATH")
	withToken := []string{path, "GITHUB_TOKEN=t0ken"}

	t.Run("in sync", func(t *testing.T) {
		if out, code := run(setup(t, liveSpec, liveSHA), withToken, "--check"); code != 0 {
			t.Errorf("exit %d: %s", code, out)
		}
	})
	t.Run("content drift", func(t *testing.T) {
		out, code := run(setup(t, liveSpec+"# old\n", liveSHA), withToken, "--check")
		if code != 1 || !strings.Contains(out, "out of sync") {
			t.Errorf("exit %d: %s", code, out)
		}
	})
	t.Run("sha drift", func(t *testing.T) {
		out, code := run(setup(t, liveSpec, strings.Repeat("b", 40)), withToken, "--check")
		if code != 1 || !strings.Contains(out, "does not match") {
			t.Errorf("exit %d: %s", code, out)
		}
	})
	t.Run("missing sha file", func(t *testing.T) {
		if out, code := run(setup(t, liveSpec, ""), withToken, "--check"); code != 1 {
			t.Errorf("exit %d: %s", code, out)
		}
	})
	t.Run("bad token", func(t *testing.T) {
		out, code := run(setup(t, liveSpec, liveSHA), []string{path, "GITHUB_TOKEN=wrong"}, "--check")
		if code != 1 || !strings.Contains(out, "failed") {
			t.Errorf("exit %d: %s", code, out)
		}
	})
	t.Run("sync writes spec and sha", func(t *testing.T) {
		dest := setup(t, "stale", "")
		out, code := run(dest, withToken)
		if code != 0 {
			t.Fatalf("exit %d: %s", code, out)
		}
		got, _ := os.ReadFile(dest)
		sha, _ := os.ReadFile(filepath.Join(filepath.Dir(dest), "api_spec.sha"))
		if string(got) != liveSpec || string(sha) != liveSHA+"\n" {
			t.Errorf("spec %q sha %q", got, sha)
		}
		if out, code := run(dest, withToken, "--check"); code != 0 {
			t.Errorf("check after sync: exit %d: %s", code, out)
		}
	})
	t.Run("usage", func(t *testing.T) {
		if _, code := run(setup(t, liveSpec, liveSHA), withToken, "--bogus"); code != 2 {
			t.Errorf("exit %d", code)
		}
	})
	t.Run("unreadable commit sha", func(t *testing.T) {
		bad := ""
		saved := liveSHA
		liveSHA = bad
		defer func() { liveSHA = saved }()
		out, code := run(setup(t, liveSpec, saved), withToken, "--check")
		if code != 1 || !strings.Contains(out, "could not read the commit SHA") {
			t.Errorf("exit %d: %s", code, out)
		}
	})

	// No token anywhere: PATH holds only the tools the script needs, no gh.
	bin := t.TempDir()
	for _, tool := range []string{"bash", "curl", "sed", "head", "tr", "cmp", "mktemp", "rm", "cat", "dirname", "mkdir", "cp", "printf"} {
		if p, err := exec.LookPath(tool); err == nil {
			_ = os.Symlink(p, filepath.Join(bin, tool))
		}
	}
	noToken := []string{"PATH=" + bin}
	t.Run("no token fails the check", func(t *testing.T) {
		out, code := run(setup(t, liveSpec, liveSHA), noToken, "--check")
		if code != 1 || !strings.Contains(out, "GITHUB_TOKEN is not set") {
			t.Errorf("exit %d: %s", code, out)
		}
	})
	t.Run("no token fails the sync", func(t *testing.T) {
		if _, code := run(setup(t, liveSpec, liveSHA), noToken); code != 1 {
			t.Errorf("exit %d", code)
		}
	})
	t.Run("no token with explicit skip", func(t *testing.T) {
		out, code := run(setup(t, "anything", ""), append([]string{"AUTONOMATCTL_ALLOW_SPEC_SKIP=1"}, noToken...), "--check")
		if code != 0 || !strings.Contains(out, "skipped") {
			t.Errorf("exit %d: %s", code, out)
		}
	})
	t.Run("skip does not apply to sync", func(t *testing.T) {
		if _, code := run(setup(t, liveSpec, liveSHA), append([]string{"AUTONOMATCTL_ALLOW_SPEC_SKIP=1"}, noToken...)); code != 1 {
			t.Errorf("exit %d", code)
		}
	})
}
