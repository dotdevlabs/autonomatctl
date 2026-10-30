// Package spec loads the subset of an OpenAPI 3.0 document that autonomatctl
// needs to generate commands: operations, path and query parameters, and the
// attributes of JSON:API request bodies.
package spec

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// WebhookSecurity is the security scheme name used by webhook delivery
// operations. Operations secured only by it are not part of the day-to-day
// operator surface.
const WebhookSecurity = "webhookToken"

// Spec is the parsed contract.
type Spec struct {
	OpenAPI    string
	Title      string
	Version    string
	Operations []Operation
}

// Operation is one method on one path of the contract.
type Operation struct {
	ID         string
	Method     string // upper case
	Path       string // template, e.g. /work_items/{id}
	Summary    string
	PathParams []string // in order of appearance in Path
	Query      []Param
	Body       *Body
	Security   []string
	// Collection is true when the success response carries an array in data.
	Collection bool
}

// Webhook reports whether the operation is secured only by the webhook token.
func (o Operation) Webhook() bool {
	return len(o.Security) > 0 && allEqual(o.Security, WebhookSecurity)
}

// Param is a documented query parameter.
type Param struct {
	Name        string
	Type        string
	Description string
	Required    bool
	// Enum is the parameter schema's enum, empty when it has none.
	Enum []string
	// Qualifiers are the typed filter qualifiers the contract documents on the
	// parameter (x-qualifiers), in contract order.
	Qualifiers []Qualifier
}

// Qualifier types the contract uses in x-qualifiers.
const (
	QualEnum      = "enum"
	QualSort      = "sort"
	QualDate      = "date"
	QualReference = "reference"
	QualBoolean   = "boolean"
	QualNumber    = "number"
	QualText      = "text"
)

// Qualifier is one documented `key:value` filter of a list query.
type Qualifier struct {
	Key         string
	Type        string
	Values      []string
	Description string
}

// Body describes a JSON:API request document.
type Body struct {
	ContentType string
	DataType    string // enum value of data.type, empty when undocumented
	// Linkage marks a relationship request whose data is an array of resource
	// identifiers of DataType.
	Linkage    bool
	HasID      bool // data.id is documented
	IDRequired bool
	Fields     []Field // data.attributes properties, in contract order
	Required   bool
	// RequiredByCategory is the attributes schema's x-required-by-category: the
	// default required parts per category. Nil when undocumented.
	RequiredByCategory map[string][]string
}

// Field is one documented attribute.
type Field struct {
	Name        string
	Type        string // string, integer, number, boolean, array, object
	ItemType    string // for arrays
	Enum        []string
	Description string
	Required    bool
	// Default is the documented default value; HasDefault distinguishes an
	// absent default from a documented null.
	Default    any
	HasDefault bool
	// Schema is the JSON Schema of an object or array-of-non-string field with
	// local $refs inlined and x-* extensions dropped; nil for other fields.
	Schema json.RawMessage
}

// dedupeKeys removes repeated mapping keys, keeping the last definition as
// most YAML parsers do; yaml.v3 would otherwise reject the document. The
// contract repeats some response and link schemas under components, which
// generate no flags. A repeat among the paths or their methods could hide an
// operation, so the top-level paths mapping and its path items (strict > 0
// levels) reject repeats instead.
func dedupeKeys(n *yaml.Node, strict int, root bool) error {
	if n.Kind == yaml.MappingNode {
		last := map[string]int{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			last[n.Content[i].Value] = i
		}
		kept := make([]*yaml.Node, 0, len(n.Content))
		for i := 0; i+1 < len(n.Content); i += 2 {
			if last[n.Content[i].Value] != i {
				if strict > 0 {
					return fmt.Errorf("duplicate key %q", n.Content[i].Value)
				}
				continue
			}
			kept = append(kept, n.Content[i], n.Content[i+1])
		}
		n.Content = kept
	}
	for i, c := range n.Content {
		next := 0
		switch {
		case n.Kind == yaml.DocumentNode:
			root = true
		case strict > 1:
			next = strict - 1
		case n.Kind == yaml.MappingNode && root && i%2 == 1 && n.Content[i-1].Value == "paths":
			next = 2
		}
		if err := dedupeKeys(c, next, root && n.Kind == yaml.DocumentNode); err != nil {
			return err
		}
	}
	return nil
}

// Load parses an OpenAPI document.
func Load(data []byte) (*Spec, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}
	if err := dedupeKeys(&root, 0, false); err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}
	var doc node
	if err := root.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}
	if doc.OpenAPI == "" || len(doc.Paths) == 0 {
		return nil, fmt.Errorf("parse spec: not an OpenAPI document with paths")
	}
	l := &loader{doc: &doc}
	s := &Spec{OpenAPI: doc.OpenAPI, Title: doc.Info.Title, Version: doc.Info.Version}

	paths := make([]string, 0, len(doc.Paths))
	for p := range doc.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		item := doc.Paths[p]
		for _, m := range []string{"get", "post", "put", "patch", "delete"} {
			n, ok := item[m]
			if !ok {
				continue
			}
			var raw rawOp
			if err := n.Decode(&raw); err != nil {
				return nil, fmt.Errorf("%s %s: %w", strings.ToUpper(m), p, err)
			}
			if pn, ok := item["parameters"]; ok {
				var shared []*param
				if err := pn.Decode(&shared); err != nil {
					return nil, fmt.Errorf("%s: parameters: %w", p, err)
				}
				raw.Parameters = append(shared, raw.Parameters...)
			}
			op, err := l.operation(p, m, &raw)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", strings.ToUpper(m), p, err)
			}
			s.Operations = append(s.Operations, op)
		}
	}
	return s, nil
}

func allEqual(ss []string, v string) bool {
	for _, s := range ss {
		if s != v {
			return false
		}
	}
	return true
}

// ---- raw document model ----

type node struct {
	OpenAPI string `yaml:"openapi"`
	Info    struct {
		Title   string `yaml:"title"`
		Version string `yaml:"version"`
	} `yaml:"info"`
	Paths      map[string]map[string]yaml.Node `yaml:"paths"`
	Components struct {
		Schemas    map[string]*schema `yaml:"schemas"`
		Parameters map[string]*param  `yaml:"parameters"`
	} `yaml:"components"`
}

type rawOp struct {
	Summary     string                 `yaml:"summary"`
	OperationID string                 `yaml:"operationId"`
	Security    []map[string]yaml.Node `yaml:"security"`
	Parameters  []*param               `yaml:"parameters"`
	RequestBody *requestBody           `yaml:"requestBody"`
	Responses   map[string]*response   `yaml:"responses"`
}

type param struct {
	Ref         string  `yaml:"$ref"`
	Name        string  `yaml:"name"`
	In          string  `yaml:"in"`
	Required    bool    `yaml:"required"`
	Description string  `yaml:"description"`
	Schema      *schema `yaml:"schema"`

	Qualifiers []qualifier `yaml:"x-qualifiers"`
}

type qualifier struct {
	Key         string   `yaml:"key"`
	Type        string   `yaml:"type"`
	Values      []string `yaml:"values"`
	Description string   `yaml:"description"`
}

type requestBody struct {
	Required bool                  `yaml:"required"`
	Content  map[string]*mediaType `yaml:"content"`
}

type response struct {
	Content map[string]*mediaType `yaml:"content"`
}

type mediaType struct {
	Schema *schema `yaml:"schema"`
}

type schema struct {
	Ref         string      `yaml:"$ref"`
	Type        string      `yaml:"type"`
	Description string      `yaml:"description"`
	Enum        []yaml.Node `yaml:"enum"`
	Required    []string    `yaml:"required"`
	Properties  props       `yaml:"properties"`
	Items       *schema     `yaml:"items"`
	Default     yaml.Node   `yaml:"default"`

	RequiredByCategory map[string][]string `yaml:"x-required-by-category"`

	// raw is the schema's own YAML, kept to emit it as JSON Schema.
	raw *yaml.Node
}

// UnmarshalYAML decodes the typed view and keeps the raw node.
func (s *schema) UnmarshalYAML(n *yaml.Node) error {
	type plain schema
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
	*s = schema(p)
	s.raw = n
	return nil
}

// props is an ordered property map: flag order follows the contract.
type props struct {
	keys []string
	m    map[string]*schema
}

func (p *props) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("properties must be a mapping")
	}
	p.m = make(map[string]*schema, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		var s schema
		if err := n.Content[i+1].Decode(&s); err != nil {
			return err
		}
		k := n.Content[i].Value
		p.keys = append(p.keys, k)
		p.m[k] = &s
	}
	return nil
}
