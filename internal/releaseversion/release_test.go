package releaseversion

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"
)

func TestCalculate(t *testing.T) {
	for _, tc := range []struct{ stage, kind, previous, want string }{
		{"development", "", "0.0.4", "0.0.5"},
		{"development", "fix", "0.0.4", "0.0.5"},
		{"development", "addition", "0.0.4", "0.0.5"},
		{"development", "breaking", "0.0.4", "0.0.5"},
		{"alpha", "fix", "0.2.3", "0.2.4"},
		{"alpha", "addition", "0.2.3", "0.3.0"},
		{"alpha", "breaking", "0.2.3", "0.3.0"},
		{"stable", "fix", "1.2.3", "1.2.4"},
		{"stable", "addition", "1.2.3", "1.3.0"},
		{"stable", "breaking", "1.2.3", "2.0.0"},
	} {
		got, err := Calculate(Entry{Version: tc.previous, Stage: tc.stage}, Intent{tc.stage, tc.kind})
		if err != nil || got != tc.want {
			t.Fatalf("%+v: %s %v", tc, got, err)
		}
	}
	for _, tc := range []struct {
		previous Entry
		intent   Intent
		want     string
	}{
		{Entry{}, Intent{"development", ""}, "0.0.1"},
		{Entry{Version: "0.0.9", Stage: "development"}, Intent{"alpha", "addition"}, "0.1.0"},
		{Entry{Version: "0.2.9", Stage: "alpha"}, Intent{"stable", "fix"}, "1.0.0"},
	} {
		got, err := Calculate(tc.previous, tc.intent)
		if err != nil || got != tc.want {
			t.Fatal(got, err)
		}
	}
	for _, intent := range []Intent{{"bad", ""}, {"alpha", ""}, {"stable", "wat"}} {
		if _, err := Calculate(Entry{}, intent); err == nil {
			t.Fatal(intent)
		}
	}
	for _, version := range []string{"x", "1.2", "01.2.3", "-1.2.3"} {
		if _, err := Calculate(Entry{Version: version, Stage: "stable"}, Intent{"stable", "fix"}); err == nil {
			t.Fatal(version)
		}
	}
	if _, err := Calculate(Entry{Version: "1.0.0", Stage: "stable"}, Intent{"development", ""}); err == nil {
		t.Fatal("backwards stage")
	}
}

func TestRepository(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	command := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s %v", args, out, err)
		}
	}
	write := func(path, value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	command("init", "-b", "main")
	command("config", "user.email", "test@example.com")
	command("config", "user.name", "Test")
	write("file", "original")
	command("add", ".")
	command("commit", "-m", "base")
	command("branch", "base")
	write("release-stage.json", `{"stage":"development"}`)
	command("add", ".")
	entry, err := Run("set", "base")
	if err != nil || entry.Version != "0.0.1" {
		t.Fatal(entry, err)
	}
	before, _ := os.ReadFile("version-log.json")
	if _, err := Run("set", "base"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile("version-log.json")
	if string(before) != string(after) {
		t.Fatal("double bump")
	}
	if _, err := Run("check", "base"); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"0.0.2\n", "0.0.8\n", "0.1.0\n", ""} {
		write("VERSION", version)
		if _, err := Run("check", "base"); err == nil {
			t.Fatal("accepted version", version)
		}
	}
	write("VERSION", "0.0.1\n")
	for _, log := range []string{"", "[]", "{}", string(before[:len(before)-2]) + ",{}]"} {
		write("version-log.json", log)
		if _, err := Run("check", "base"); err == nil {
			t.Fatal("accepted log", log)
		}
	}
	write("version-log.json", string(before))
	for _, stage := range []string{`{"stage":"alpha"}`, `{"stage":"stable","kind":"bad"}`, `{}`} {
		write("release-stage.json", stage)
		if _, err := Run("check", "base"); err == nil {
			t.Fatal("accepted stage", stage)
		}
	}
	write("release-stage.json", `{"stage":"development"}`)
	write("untracked", "ignored")
	if _, err := Run("current", "base"); err != nil {
		t.Fatal(err)
	}
	write("file", "changed")
	if _, err := Run("check", "base"); err == nil {
		t.Fatal("accepted digest")
	}
	changed, err := Run("set", "base")
	if err != nil || changed.Version != entry.Version || changed.Digest == entry.Digest {
		t.Fatal(changed, err)
	}
	digest, _ := Digest()
	if err := os.Chmod("file", 0755); err != nil {
		t.Fatal(err)
	}
	mode, _ := Digest()
	if digest == mode {
		t.Fatal("mode ignored")
	}
	if err := os.Remove("file"); err != nil {
		t.Fatal(err)
	}
	deleted, _ := Digest()
	if deleted == mode {
		t.Fatal("deletion ignored")
	}
	if err := os.Symlink("file", "link"); err != nil {
		t.Fatal(err)
	}
	command("add", "link")
	linked, err := Digest()
	if err != nil || linked == deleted {
		t.Fatal("symlink ignored", err)
	}
	if err := os.Remove("link"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other", "link"); err != nil {
		t.Fatal(err)
	}
	retargeted, err := Digest()
	if err != nil || retargeted == linked {
		t.Fatal("symlink target ignored", err)
	}
	write("file", "changed")
	command("add", ".")
	if _, err := Run("set", "base"); err != nil {
		t.Fatal(err)
	}
	command("add", ".")
	command("commit", "-m", "release")
	if _, err := Run("check", "HEAD^1"); err != nil {
		t.Fatal(err)
	}
	command("branch", "released")
	if _, err := Run("check", "released"); err == nil {
		t.Fatal("stale release accepted")
	}
	if _, err := Run("set", "released"); err != nil {
		t.Fatal(err)
	}
	var entries []Entry
	data, _ := os.ReadFile("version-log.json")
	_ = json.Unmarshal(data, &entries)
	if len(entries) != 2 || entries[1].Version != "0.0.2" {
		t.Fatal(entries)
	}
	entries[0].Digest = "modified"
	data, _ = json.Marshal(entries)
	write("version-log.json", string(data))
	if _, err := Run("check", "released"); err == nil {
		t.Fatal("history rewrite accepted")
	}
	if _, err := Run("check", "missing"); err == nil {
		t.Fatal("missing base accepted")
	}
	if _, err := Run("unknown", "base"); err == nil {
		t.Fatal("unknown operation")
	}
}
