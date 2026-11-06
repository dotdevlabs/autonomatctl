package releasecheck

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestReleaseWiring(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On   struct{ Push struct{ Tags []string } }
		Jobs map[string]struct {
			Steps []struct {
				Uses, Run string
				Continue  bool `yaml:"continue-on-error"`
			}
		}
	}
	if err := yaml.Unmarshal(b, &workflow); err != nil {
		t.Fatal(err)
	}
	if len(workflow.On.Push.Tags) != 1 || workflow.On.Push.Tags[0] != "v*" {
		t.Fatal("missing tag trigger")
	}
	guard := false
	for _, step := range workflow.Jobs["release"].Steps {
		if strings.Contains(step.Run, "go run ./cmd/check-release") {
			if step.Continue || !strings.Contains(step.Run, "set -euo pipefail") || !strings.Contains(step.Run, "go build -trimpath") || !strings.Contains(step.Run, "cat docs/release-notice.md") {
				t.Fatal("guard is not blocking or notice missing")
			}
			guard = true
		}
		if strings.Contains(step.Uses, "goreleaser") && !guard {
			t.Fatal("publication precedes guard")
		}
	}
	if !guard {
		t.Fatal("missing release guard")
	}
	b, err = os.ReadFile("../../.goreleaser.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"RELEASE_NOTICE", "version.Commit=", "version.Date="} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(string(b), "version.Version=") {
		t.Fatal("tag overrides contract")
	}
}
