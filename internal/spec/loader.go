package spec

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const jsonAPIType = "application/vnd.api+json"

var pathParam = regexp.MustCompile(`\{([^}/]+)\}`)

type loader struct {
	doc *node
}

func (l *loader) operation(path, method string, r *rawOp) (Operation, error) {
	op := Operation{
		ID:      r.OperationID,
		Method:  strings.ToUpper(method),
		Path:    path,
		Summary: strings.TrimSpace(r.Summary),
	}
	if op.ID == "" {
		return op, fmt.Errorf("missing operationId")
	}
	for _, m := range pathParam.FindAllStringSubmatch(path, -1) {
		op.PathParams = append(op.PathParams, m[1])
	}
	for _, sec := range r.Security {
		names := make([]string, 0, len(sec))
		for k := range sec {
			names = append(names, k)
		}
		sort.Strings(names)
		op.Security = append(op.Security, names...)
	}

	for _, rp := range r.Parameters {
		p, err := l.param(rp)
		if err != nil {
			return op, err
		}
		if p.In != "query" {
			continue
		}
		typ := "string"
		var enum []string
		if p.Schema != nil {
			s, err := l.resolve(p.Schema)
			if err != nil {
				return op, err
			}
			if s.Type != "" {
				typ = s.Type
			}
			for _, e := range s.Enum {
				enum = append(enum, e.Value)
			}
		}
		quals, err := qualifiers(p.Qualifiers)
		if err != nil {
			return op, fmt.Errorf("parameter %s: %w", p.Name, err)
		}
		op.Query = append(op.Query, Param{Name: p.Name, Type: typ, Description: strings.TrimSpace(p.Description),
			Required: p.Required, Enum: enum, Qualifiers: quals})
	}

	if r.RequestBody != nil {
		b, err := l.body(r.RequestBody)
		if err != nil {
			return op, err
		}
		op.Body = b
	}

	coll, err := l.collection(r.Responses)
	if err != nil {
		return op, err
	}
	op.Collection = coll
	return op, nil
}

func (l *loader) param(p *param) (*param, error) {
	if p.Ref == "" {
		return p, nil
	}
	name, err := refName(p.Ref, "parameters")
	if err != nil {
		return nil, err
	}
	target, ok := l.doc.Components.Parameters[name]
	if !ok || target.Ref != "" {
		return nil, fmt.Errorf("unresolved parameter reference %q", p.Ref)
	}
	return target, nil
}

func (l *loader) body(rb *requestBody) (*Body, error) {
	ct := jsonAPIType
	mt, ok := rb.Content[ct]
	if !ok {
		cts := make([]string, 0, len(rb.Content))
		for k := range rb.Content {
			cts = append(cts, k)
		}
		sort.Strings(cts)
		if len(cts) == 0 {
			return &Body{Required: rb.Required}, nil
		}
		ct = cts[0]
		mt = rb.Content[ct]
	}
	b := &Body{ContentType: ct, Required: rb.Required}
	if mt == nil || mt.Schema == nil {
		return b, nil
	}
	doc, err := l.resolve(mt.Schema)
	if err != nil {
		return nil, err
	}
	dataRef, ok := doc.Properties.m["data"]
	if !ok {
		if len(doc.Properties.keys) > 0 {
			return nil, fmt.Errorf("request body is not a JSON:API document (no data member)")
		}
		return b, nil
	}
	data, err := l.resolve(dataRef)
	if err != nil {
		return nil, err
	}
	if data.Type == "array" && data.Items != nil {
		// Relationship linkage: data is a list of resource identifiers.
		item, err := l.resolve(data.Items)
		if err != nil {
			return nil, fmt.Errorf("linkage items: %w", err)
		}
		b.Linkage = true
		if t, ok := item.Properties.m["type"]; ok {
			ts, err := l.resolve(t)
			if err != nil {
				return nil, err
			}
			if len(ts.Enum) > 0 {
				b.DataType = ts.Enum[0].Value
			}
		}
		return b, nil
	}
	if t, ok := data.Properties.m["type"]; ok {
		ts, err := l.resolve(t)
		if err != nil {
			return nil, err
		}
		if len(ts.Enum) > 0 {
			b.DataType = ts.Enum[0].Value
		}
	}
	if _, ok := data.Properties.m["id"]; ok {
		b.HasID = true
		b.IDRequired = contains(data.Required, "id")
	}
	attrRef, ok := data.Properties.m["attributes"]
	if !ok {
		return b, nil
	}
	attrs, err := l.resolve(attrRef)
	if err != nil {
		return nil, err
	}
	b.RequiredByCategory = attrs.RequiredByCategory
	for _, name := range attrs.Properties.keys {
		fs, err := l.resolve(attrs.Properties.m[name])
		if err != nil {
			return nil, fmt.Errorf("attribute %s: %w", name, err)
		}
		f := Field{
			Name:        name,
			Type:        fs.Type,
			Description: strings.TrimSpace(fs.Description),
			Required:    contains(attrs.Required, name),
		}
		if f.Type == "" {
			f.Type = "object"
		}
		if fs.Default.Kind != 0 {
			if err := fs.Default.Decode(&f.Default); err != nil {
				return nil, fmt.Errorf("attribute %s default: %w", name, err)
			}
			f.HasDefault = true
		}
		for _, e := range fs.Enum {
			f.Enum = append(f.Enum, e.Value)
		}
		if fs.Type == "array" && fs.Items != nil {
			is, err := l.resolve(fs.Items)
			if err != nil {
				return nil, fmt.Errorf("attribute %s items: %w", name, err)
			}
			f.ItemType = is.Type
		}
		if f.Type == "object" || (f.Type == "array" && f.ItemType != "string") {
			sch, err := l.jsonSchema(fs.raw)
			if err != nil {
				return nil, fmt.Errorf("attribute %s schema: %w", name, err)
			}
			f.Schema = sch
		}
		b.Fields = append(b.Fields, f)
	}
	return b, nil
}

// collection reports whether the first 2xx response document has an array in data.
func (l *loader) collection(resps map[string]*response) (bool, error) {
	codes := make([]string, 0, len(resps))
	for c := range resps {
		if strings.HasPrefix(c, "2") {
			codes = append(codes, c)
		}
	}
	sort.Strings(codes)
	for _, c := range codes {
		for _, mt := range resps[c].Content {
			if mt == nil || mt.Schema == nil {
				continue
			}
			doc, err := l.resolve(mt.Schema)
			if err != nil {
				return false, err
			}
			data, ok := doc.Properties.m["data"]
			if !ok {
				continue
			}
			d, err := l.resolve(data)
			if err != nil {
				return false, err
			}
			return d.Type == "array", nil
		}
	}
	return false, nil
}

// resolve follows local $ref chains to a concrete schema.
func (l *loader) resolve(s *schema) (*schema, error) {
	seen := map[string]bool{}
	for s.Ref != "" {
		if seen[s.Ref] {
			return nil, fmt.Errorf("cyclic schema reference %q", s.Ref)
		}
		seen[s.Ref] = true
		name, err := refName(s.Ref, "schemas")
		if err != nil {
			return nil, err
		}
		next, ok := l.doc.Components.Schemas[name]
		if !ok || next == nil {
			return nil, fmt.Errorf("unresolved schema reference %q", s.Ref)
		}
		s = next
	}
	return s, nil
}

func refName(ref, kind string) (string, error) {
	prefix := "#/components/" + kind + "/"
	if !strings.HasPrefix(ref, prefix) {
		return "", fmt.Errorf("unsupported reference %q (only local %s are supported)", ref, prefix)
	}
	return strings.TrimPrefix(ref, prefix), nil
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
