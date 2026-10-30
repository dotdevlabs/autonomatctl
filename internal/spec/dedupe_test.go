package spec

import (
	"strings"
	"testing"
)

const dupDoc = `openapi: 3.0.3
info: {title: T, version: "1"}
paths:
  /things:
    get:
      operationId: listThings
      responses:
        "200":
          content:
            application/vnd.api+json:
              schema: {$ref: "#/components/schemas/Doc"}
components:
  schemas:
    Links: {type: object}
    Links: {type: array}
    Doc:
      type: object
      properties:
        data: {type: array, items: {type: object}}
`

func TestLoadToleratesDuplicateComponentKeys(t *testing.T) {
	s, err := Load([]byte(dupDoc))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Operations) != 1 || !s.Operations[0].Collection {
		t.Fatalf("operations = %+v", s.Operations)
	}
}

func TestLoadRejectsDuplicatePathKeys(t *testing.T) {
	head := "openapi: 3.0.3\ninfo: {title: T, version: \"1\"}\npaths:\n"
	for name, doc := range map[string]string{
		"path":   head + "  /a: {get: {operationId: a}}\n  /a: {get: {operationId: b}}\n",
		"method": head + "  /a:\n    get: {operationId: a}\n    get: {operationId: b}\n",
	} {
		_, err := Load([]byte(doc))
		if err == nil || !strings.Contains(err.Error(), "duplicate key") {
			t.Errorf("%s: err = %v, want duplicate key", name, err)
		}
	}
}

func TestLoadRejectsInvalidYAML(t *testing.T) {
	if _, err := Load([]byte("paths: [unclosed")); err == nil {
		t.Fatal("invalid YAML accepted")
	}
}

func TestLoadRejectsMistypedDocument(t *testing.T) {
	if _, err := Load([]byte("openapi: 3.0.3\npaths: nope\n")); err == nil {
		t.Fatal("mistyped paths accepted")
	}
}

func TestLoadLinkageBody(t *testing.T) {
	doc := `openapi: 3.0.3
info: {title: T, version: "1"}
paths:
  /a:
    post:
      operationId: link
      requestBody:
        content:
          application/vnd.api+json:
            schema: {$ref: "#/components/schemas/Req"}
components:
  schemas:
    Req:
      type: object
      properties:
        data: {type: array, items: {$ref: "#/components/schemas/Ident"}}
    Ident:
      type: object
      properties:
        type: {type: string, enum: [things]}
        id: {type: string}
`
	s, err := Load([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	b := s.Operations[0].Body
	if !b.Linkage || b.DataType != "things" || len(b.Fields) != 0 {
		t.Fatalf("body = %+v", b)
	}
}
