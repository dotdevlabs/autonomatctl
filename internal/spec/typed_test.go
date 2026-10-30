package spec

import (
	"encoding/json"
	"strings"
	"testing"
)

const typedDoc = `
openapi: "3.0.3"
info: {title: T, version: "1"}
components:
  schemas:
    Doc:
      type: object
      properties:
        data:
          type: object
          properties:
            type: {type: string, enum: [things]}
            attributes:
              type: object
              x-required-by-category:
                decision: [question]
              properties:
                obj: {$ref: "#/components/schemas/Obj"}
                list:
                  type: array
                  items: {$ref: "#/components/schemas/Obj"}
                tags: {type: array, items: {type: string}}
                free: {type: object}
    Obj:
      type: object
      x-internal: drop
      required: [a]
      properties:
        a: {type: string, enum: [x, y], default: x}
        b: {type: array, items: {type: integer}}
        c: {oneOf: [{type: string}, {type: integer}], nullable: true}
        d: {type: object, additionalProperties: {type: string}}
        e: {allOf: [{$ref: "#/components/schemas/Leaf"}]}
    Leaf: {type: string, description: leaf}
paths:
  /things:
    get:
      operationId: listThings
      parameters:
        - name: q
          in: query
          schema: {type: string}
          x-qualifiers:
            - {key: status, type: enum, values: [open, closed], description: " One of "}
            - {key: sort, type: sort, values: [a-asc]}
            - {key: since, type: date}
            - {key: product, type: reference}
            - {key: is, type: boolean, values: [blocked]}
            - {key: step, type: number}
            - {key: name, type: text}
        - name: kind
          in: query
          schema: {type: string, enum: [a, b]}
      responses: {}
    post:
      operationId: createThing
      requestBody:
        content:
          application/vnd.api+json:
            schema: {$ref: "#/components/schemas/Doc"}
      responses: {}
`

func TestTypedQualifiersAndSchemas(t *testing.T) {
	s, err := Load([]byte(typedDoc))
	if err != nil {
		t.Fatal(err)
	}
	var list, create Operation
	for _, o := range s.Operations {
		if o.ID == "listThings" {
			list = o
		} else {
			create = o
		}
	}
	q := list.Query[0].Qualifiers
	var types []string
	for _, x := range q {
		types = append(types, x.Key+":"+x.Type)
	}
	if got := strings.Join(types, ","); got != "status:enum,sort:sort,since:date,product:reference,is:boolean,step:number,name:text" {
		t.Fatalf("qualifiers = %s", got)
	}
	if q[0].Description != "One of" || strings.Join(q[0].Values, ",") != "open,closed" {
		t.Fatalf("status = %+v", q[0])
	}
	if got := strings.Join(list.Query[1].Enum, ","); got != "a,b" || list.Query[0].Enum != nil {
		t.Fatalf("param enum = %v", list.Query[1].Enum)
	}
	if got := create.Body.RequiredByCategory["decision"]; len(got) != 1 || got[0] != "question" {
		t.Fatalf("required by category = %v", create.Body.RequiredByCategory)
	}
	f := map[string]Field{}
	for _, x := range create.Body.Fields {
		f[x.Name] = x
	}
	if f["tags"].Schema != nil {
		t.Fatal("string arrays carry no schema")
	}
	var obj map[string]any
	if err := json.Unmarshal(f["obj"].Schema, &obj); err != nil {
		t.Fatal(err)
	}
	if _, ok := obj["x-internal"]; ok || obj["type"] != "object" {
		t.Fatalf("obj schema = %s", f["obj"].Schema)
	}
	props := obj["properties"].(map[string]any)
	if props["e"].(map[string]any)["allOf"].([]any)[0].(map[string]any)["description"] != "leaf" {
		t.Fatalf("allOf ref not inlined: %s", f["obj"].Schema)
	}
	if props["a"].(map[string]any)["default"] != "x" || props["c"].(map[string]any)["nullable"] != true {
		t.Fatalf("scalars lost: %s", f["obj"].Schema)
	}
	var arr map[string]any
	if err := json.Unmarshal(f["list"].Schema, &arr); err != nil || arr["items"].(map[string]any)["type"] != "object" {
		t.Fatalf("list schema = %s (%v)", f["list"].Schema, err)
	}
	if string(f["free"].Schema) != `{"type":"object"}` {
		t.Fatalf("free schema = %s", f["free"].Schema)
	}
}

func TestTypedErrors(t *testing.T) {
	q := func(entry string) string {
		return strings.Replace(typedDoc, "- {key: name, type: text}", entry, 1)
	}
	cases := map[string]string{
		"missing key":       q("- {type: text}"),
		"duplicate key":     q("- {key: status, type: text}"),
		"unknown type":      q("- {key: name, type: wat}"),
		"schema unresolved": strings.Replace(typedDoc, `allOf: [{$ref: "#/components/schemas/Leaf"}]`, `allOf: [{$ref: "#/components/schemas/Nope"}]`, 1),
		"schema external":   strings.Replace(typedDoc, `allOf: [{$ref: "#/components/schemas/Leaf"}]`, `allOf: [{$ref: "x.yaml#/Y"}]`, 1),
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load([]byte(doc)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestPinnedTypedContract(t *testing.T) {
	s := pinned(t)
	n := 0
	for _, o := range s.Operations {
		for _, p := range o.Query {
			n += len(p.Qualifiers)
		}
	}
	if n == 0 {
		t.Fatal("pinned contract has no typed qualifiers")
	}
	if find(t, s, "createInboxItem").Body.RequiredByCategory["decision"] == nil {
		t.Fatal("createInboxItem lacks x-required-by-category")
	}
}

func TestRecursiveSchemaUsesDefs(t *testing.T) {
	doc := strings.Replace(typedDoc, "Leaf: {type: string, description: leaf}",
		"Leaf: {type: object, properties: {back: {$ref: \"#/components/schemas/Leaf\"}, other: {$ref: \"#/components/schemas/Leaf\"}}}", 1)
	s, err := Load([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	var sch string
	for _, o := range s.Operations {
		if o.Body != nil {
			for _, f := range o.Body.Fields {
				if f.Name == "obj" {
					sch = string(f.Schema)
				}
			}
		}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(sch), &m); err != nil {
		t.Fatal(err)
	}
	defs, ok := m["$defs"].(map[string]any)
	if !ok || defs["Leaf"] == nil || !strings.Contains(sch, `"$ref":"#/$defs/Leaf"`) {
		t.Fatalf("recursive schema = %s", sch)
	}
}
