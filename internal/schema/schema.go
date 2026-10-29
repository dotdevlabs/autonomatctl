// Package schema embeds the pinned copy of Autonomat's OpenAPI contract and
// the commit SHA it was synced from, and exposes the schema command.
package schema

import (
	_ "embed"
	"strings"
	"sync"

	"github.com/dotdevlabs/autonomatctl/internal/spec"
)

// Source is where the authoritative contract lives.
const Source = "dotdevlabs/autonomat docs/api_spec.yaml"

//go:embed testdata/api_spec.yaml
var specYAML []byte

//go:embed testdata/api_spec.sha
var specSHA string

var (
	once   sync.Once
	parsed *spec.Spec
	perr   error
)

// Raw returns the pinned contract document.
func Raw() []byte { return specYAML }

// SHA returns the commit of Source the pinned copy was taken from.
func SHA() string { return strings.TrimSpace(specSHA) }

// Spec returns the parsed pinned contract.
func Spec() (*spec.Spec, error) {
	once.Do(func() { parsed, perr = spec.Load(specYAML) })
	return parsed, perr
}

// Version is the contract's info.version.
func Version() string {
	s, err := Spec()
	if err != nil {
		return ""
	}
	return s.Version
}
