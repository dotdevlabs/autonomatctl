package spec

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

var qualifierTypes = map[string]bool{
	QualEnum: true, QualSort: true, QualDate: true, QualReference: true,
	QualBoolean: true, QualNumber: true, QualText: true,
}

// qualifiers validates and converts a parameter's x-qualifiers.
func qualifiers(raw []qualifier) ([]Qualifier, error) {
	var out []Qualifier
	seen := map[string]bool{}
	for i, q := range raw {
		if q.Key == "" {
			return nil, fmt.Errorf("x-qualifiers[%d]: missing key", i)
		}
		if seen[q.Key] {
			return nil, fmt.Errorf("x-qualifiers: duplicate key %q", q.Key)
		}
		seen[q.Key] = true
		if !qualifierTypes[q.Type] {
			return nil, fmt.Errorf("x-qualifiers %q: unknown type %q", q.Key, q.Type)
		}
		out = append(out, Qualifier{Key: q.Key, Type: q.Type, Values: q.Values, Description: strings.TrimSpace(q.Description)})
	}
	return out, nil
}

// jsonSchema renders a schema node as JSON Schema: local $refs are inlined,
// x-* extensions are dropped and everything else is kept as written.
func (l *loader) jsonSchema(n *yaml.Node) (json.RawMessage, error) {
	b := &schemaBuild{defs: map[string]any{}}
	v, err := l.schemaValue(b, n, nil)
	if err != nil {
		return nil, err
	}
	if m, ok := v.(map[string]any); ok && len(b.defs) > 0 {
		m["$defs"] = b.defs
	}
	return json.Marshal(v)
}

// schemaBuild collects the definitions of recursive schemas: a schema that
// refers back to itself is emitted once under $defs and referenced from there.
type schemaBuild struct {
	defs map[string]any
}

// schemaKeys whose value is a schema, a list of schemas or a map of schemas.
var (
	schemaValued = map[string]bool{"items": true, "additionalProperties": true, "not": true}
	schemaList   = map[string]bool{"oneOf": true, "anyOf": true, "allOf": true}
)

func (l *loader) schemaValue(b *schemaBuild, n *yaml.Node, stack []string) (any, error) {
	if n.Kind == yaml.AliasNode {
		return l.schemaValue(b, n.Alias, stack)
	}
	if n.Kind != yaml.MappingNode {
		return plain(n)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value != "$ref" {
			continue
		}
		ref := n.Content[i+1].Value
		name, err := refName(ref, "schemas")
		if err != nil {
			return nil, err
		}
		target, ok := l.doc.Components.Schemas[name]
		if !ok || target == nil || target.raw == nil {
			return nil, fmt.Errorf("unresolved schema reference %q", ref)
		}
		for _, s := range stack {
			if s == name {
				if _, done := b.defs[name]; !done {
					b.defs[name] = nil // placeholder: the definition refers to itself
					def, err := l.schemaValue(b, target.raw, []string{name})
					if err != nil {
						return nil, err
					}
					b.defs[name] = def
				}
				return map[string]any{"$ref": "#/$defs/" + name}, nil
			}
		}
		return l.schemaValue(b, target.raw, append(stack, name))
	}
	out := map[string]any{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i].Value, n.Content[i+1]
		if strings.HasPrefix(k, "x-") {
			continue
		}
		var val any
		var err error
		switch {
		case k == "properties" && v.Kind == yaml.MappingNode:
			props := map[string]any{}
			for j := 0; j+1 < len(v.Content); j += 2 {
				if props[v.Content[j].Value], err = l.schemaValue(b, v.Content[j+1], stack); err != nil {
					return nil, err
				}
			}
			val = props
		case schemaValued[k]:
			val, err = l.schemaValue(b, v, stack)
		case schemaList[k] && v.Kind == yaml.SequenceNode:
			list := make([]any, 0, len(v.Content))
			for _, c := range v.Content {
				item, ierr := l.schemaValue(b, c, stack)
				if ierr != nil {
					return nil, ierr
				}
				list = append(list, item)
			}
			val = list
		default:
			val, err = plain(v)
		}
		if err != nil {
			return nil, err
		}
		out[k] = val
	}
	return out, nil
}

// plain decodes a YAML node as data.
func plain(n *yaml.Node) (any, error) {
	var v any
	if err := n.Decode(&v); err != nil {
		return nil, err
	}
	return jsonable(v), nil
}

// jsonable turns yaml's map[string]any / map[any]any results into JSON-safe values.
func jsonable(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, e := range t {
			t[k] = jsonable(e)
		}
		return t
	case map[any]any:
		m := make(map[string]any, len(t))
		for k, e := range t {
			m[fmt.Sprint(k)] = jsonable(e)
		}
		return m
	case []any:
		for i, e := range t {
			t[i] = jsonable(e)
		}
		return t
	}
	return v
}
