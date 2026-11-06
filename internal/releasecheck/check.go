// Package releasecheck verifies release candidates before publication.
package releasecheck

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var semver = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

// Result contains all independently observed release versions.
type Result struct {
	Tag      string `json:"tag_version"`
	Reported string `json:"reported_version"`
	Contract string `json:"contract_version"`
	Error    string `json:"error,omitempty"`
}

// Compare rejects invalid or inconsistent versions.
func Compare(tag, reported, contract string) Result {
	r := Result{Tag: strings.TrimPrefix(tag, "v"), Reported: reported, Contract: contract}
	if !strings.HasPrefix(tag, "v") || !semver.MatchString(r.Tag) || !semver.MatchString(reported) || !semver.MatchString(contract) || r.Tag != reported || reported != contract {
		r.Error = fmt.Sprintf("release version mismatch: tag=%q reported=%q contract=%q", r.Tag, reported, contract)
	}
	return r
}

// Check executes the candidate and independently reads the embedded YAML metadata.
func Check(tag, binary string, raw []byte) Result {
	var contract struct {
		Info struct {
			Version string `yaml:"version"`
		} `yaml:"info"`
	}
	err := yaml.Unmarshal(raw, &contract)
	if err != nil {
		return Result{Tag: strings.TrimPrefix(tag, "v"), Error: fmt.Sprintf("invalid embedded contract: %v", err)}
	}
	// The executable path is supplied by the release workflow, never by API input.
	out, err := exec.Command(binary, "version", "--json").Output() // #nosec G204
	var reported struct {
		Version string `json:"version"`
	}
	if err == nil {
		err = json.Unmarshal(out, &reported)
	}
	r := Compare(tag, reported.Version, contract.Info.Version)
	if err != nil {
		r.Error += fmt.Sprintf("; reading candidate version: %v", err)
	}
	return r
}
