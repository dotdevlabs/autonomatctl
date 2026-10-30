package spec

import (
	"os"
	"strings"
	"testing"
)

func pinned(t *testing.T) *Spec {
	t.Helper()
	data, err := os.ReadFile("../schema/testdata/api_spec.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Load(data)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func find(t *testing.T, s *Spec, id string) Operation {
	t.Helper()
	for _, o := range s.Operations {
		if o.ID == id {
			return o
		}
	}
	t.Fatalf("operation %s not found", id)
	return Operation{}
}

func TestLoadPinned(t *testing.T) {
	s := pinned(t)
	if s.Title != "Autonomat API" || s.Version == "" || s.OpenAPI == "" {
		t.Fatalf("bad header: %+v", s)
	}
	if len(s.Operations) != 100 {
		t.Fatalf("operations = %d", len(s.Operations))
	}
	ids := map[string]bool{}
	for _, o := range s.Operations {
		if ids[o.ID] {
			t.Fatalf("duplicate operationId %s", o.ID)
		}
		ids[o.ID] = true
	}

	list := find(t, s, "listWorkItems")
	if !list.Collection || len(list.Query) != 1 || list.Query[0].Name != "q" || list.Query[0].Type != "string" {
		t.Fatalf("listWorkItems = %+v", list)
	}
	get := find(t, s, "getWorkItem")
	if get.Collection || len(get.PathParams) != 1 || get.PathParams[0] != "id" {
		t.Fatalf("getWorkItem = %+v", get)
	}
	create := find(t, s, "createWorkItem")
	if create.Body == nil || create.Body.DataType != "work-items" || create.Body.ContentType != jsonAPIType {
		t.Fatalf("createWorkItem body = %+v", create.Body)
	}
	var tags, title Field
	for _, f := range create.Body.Fields {
		switch f.Name {
		case "tags":
			tags = f
		case "title":
			title = f
		}
	}
	if tags.Type != "array" || tags.ItemType != "string" || !title.Required || title.Type != "string" {
		t.Fatalf("fields tags=%+v title=%+v", tags, title)
	}
	if create.Webhook() {
		t.Fatal("createWorkItem is not a webhook op")
	}
	hook := find(t, s, "deliverAgentRunResults")
	if !hook.Webhook() || !hook.Body.HasID || !hook.Body.IDRequired || hook.Body.DataType != "run-results" {
		t.Fatalf("webhook op = %+v body=%+v", hook, hook.Body)
	}
	if find(t, s, "sunsetProduct").Body != nil {
		t.Fatal("sunsetProduct should have no body")
	}
	if find(t, s, "getAccount").Collection {
		t.Fatal("getAccount is a singleton")
	}
}

const mini = `
openapi: "3.0.3"
info: {title: T, version: "7"}
components:
  parameters:
    Page:
      name: page
      in: query
      required: true
      schema: {type: integer}
  schemas:
    Doc:
      type: object
      properties:
        data:
          type: object
          required: [id]
          properties:
            type: {type: string, enum: [things]}
            id: {type: string}
            attributes:
              $ref: "#/components/schemas/Attrs"
    Attrs:
      type: object
      required: [b]
      properties:
        b: {type: boolean}
        a: {$ref: "#/components/schemas/Alias", nullable: true}
        n: {type: number}
        o: {type: object}
        l: {type: array, items: {type: integer}, default: [1]}
        e: {type: string, enum: [x, y]}
    Alias: {$ref: "#/components/schemas/Real"}
    Real: {type: string}
paths:
  /things:
    parameters: []
    post:
      operationId: createThing
      security: [{apiToken: []}]
      parameters:
        - $ref: "#/components/parameters/Page"
        - {name: h, in: header, schema: {type: string}}
      requestBody:
        required: true
        content:
          application/json:
            schema: {$ref: "#/components/schemas/Doc"}
      responses:
        "201": {description: ok}
`

func TestLoadMini(t *testing.T) {
	s, err := Load([]byte(mini))
	if err != nil {
		t.Fatal(err)
	}
	op := s.Operations[0]
	if len(op.Query) != 1 || op.Query[0].Name != "page" || op.Query[0].Type != "integer" || !op.Query[0].Required {
		t.Fatalf("query = %+v", op.Query)
	}
	b := op.Body
	if b.ContentType != "application/json" || !b.Required || b.DataType != "things" || !b.HasID {
		t.Fatalf("body = %+v", b)
	}
	var names []string
	for _, f := range b.Fields {
		names = append(names, f.Name+":"+f.Type)
	}
	if got := strings.Join(names, ","); got != "b:boolean,a:string,n:number,o:object,l:array,e:string" {
		t.Fatalf("fields in contract order = %s", got)
	}
	if !b.Fields[4].HasDefault || b.Fields[4].Default.([]any)[0] != 1 || b.Fields[0].HasDefault {
		t.Fatalf("unexpected defaults: %+v", b.Fields)
	}
	if b.Fields[5].Enum[1] != "y" || b.Fields[4].ItemType != "integer" || !b.Fields[0].Required {
		t.Fatalf("field details = %+v", b.Fields)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]string{
		"not yaml":        "a: [",
		"not openapi":     "foo: bar",
		"no operationId":  "openapi: 3.0.3\npaths:\n  /a:\n    get:\n      responses: {}\n",
		"unresolved ref":  strings.Replace(mini, `{$ref: "#/components/schemas/Alias", nullable: true}`, `{$ref: "#/components/schemas/Missing"}`, 1),
		"external ref":    strings.Replace(mini, `{$ref: "#/components/schemas/Alias", nullable: true}`, `{$ref: "other.yaml#/X"}`, 1),
		"cyclic ref":      strings.Replace(mini, `Real: {type: string}`, `Real: {$ref: "#/components/schemas/Alias"}`, 1),
		"missing param":   strings.Replace(mini, `#/components/parameters/Page`, `#/components/parameters/Nope`, 1),
		"bad properties":  strings.Replace(mini, "properties:\n        b:", "properties: [1]\n      x:\n        b:", 1),
		"no data member":  strings.Replace(mini, "      properties:\n        data:\n          type: object\n          required: [id]", "      properties:\n        other:\n          type: object\n          required: [id]", 1),
		"bad nested prop": strings.Replace(mini, "e: {type: string, enum: [x, y]}", "e: [1]", 1),
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load([]byte(doc)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestBodyVariants(t *testing.T) {
	doc := `
openapi: "3.0.3"
paths:
  /a:
    post:
      operationId: noContent
      requestBody: {required: false, content: {}}
      responses: {}
    put:
      operationId: noSchema
      requestBody: {content: {text/plain: {}}}
      responses: {}
    patch:
      operationId: emptySchema
      requestBody: {content: {application/vnd.api+json: {schema: {type: object}}}}
      responses: {"200": {content: {application/vnd.api+json: {schema: {properties: {meta: {type: object}}}}}}}
    delete:
      operationId: noAttrs
      requestBody: {content: {application/vnd.api+json: {schema: {properties: {data: {type: object, properties: {type: {type: string}}}}}}}}
      responses: {"204": {description: gone}}
`
	s, err := Load([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range s.Operations {
		if o.Body == nil || len(o.Body.Fields) != 0 || o.Body.DataType != "" {
			t.Fatalf("%s: body = %+v", o.ID, o.Body)
		}
		if o.Collection {
			t.Fatalf("%s: unexpected collection", o.ID)
		}
	}
}
