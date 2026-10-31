package gen

import (
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dotdevlabs/ctlkit/pkg/airef"

	"github.com/dotdevlabs/autonomatctl/internal/spec"
)

func annotation(t *testing.T, c *cobra.Command, flag, key string) []string {
	t.Helper()
	f := c.Flags().Lookup(flag)
	if f == nil {
		t.Fatalf("%s: no --%s flag", c.CommandPath(), flag)
	}
	return f.Annotations[key]
}

func enumOf(t *testing.T, c *cobra.Command, flag string) []string {
	t.Helper()
	a := annotation(t, c, flag, airef.EnumAnnotation)
	if len(a) == 0 {
		return nil
	}
	var vals []string
	if err := json.Unmarshal([]byte(a[0]), &vals); err != nil {
		t.Fatal(err)
	}
	return vals
}

func TestEnumsAppearOnFlags(t *testing.T) {
	sp := pinnedSpec(t)
	byOp := commandsByOperation(testRoot(t, sp, "http://unused.invalid", fastCfg()))
	var n int
	for _, op := range sp.Operations {
		c := byOp[op.ID]
		for _, q := range op.Query {
			if got := enumOf(t, c, q.Name); !reflect.DeepEqual(got, q.Enum) {
				t.Errorf("%s --%s: enum = %v, want %v", op.ID, q.Name, got, q.Enum)
			}
			n += len(q.Enum)
		}
		if op.Body == nil {
			continue
		}
		for _, f := range op.Body.Fields {
			if got := enumOf(t, c, flagOf(f.Name)); !reflect.DeepEqual(got, f.Enum) {
				t.Errorf("%s --%s: enum = %v, want %v", op.ID, f.Name, got, f.Enum)
			}
			n += len(f.Enum)
		}
	}
	if n == 0 {
		t.Error("the pinned contract has no enum to check")
	}
}

func TestQualifierFlagsMatchContract(t *testing.T) {
	sp := pinnedSpec(t)
	byOp := commandsByOperation(testRoot(t, sp, "http://unused.invalid", fastCfg()))
	types := map[string]bool{}
	for _, op := range sp.Operations {
		c := byOp[op.ID]
		for _, q := range op.Query {
			if q.Name == "q" && c.Flags().Lookup("q") == nil {
				t.Errorf("%s: generic --q is gone", op.ID)
			}
			for _, ql := range q.Qualifiers {
				types[ql.Type] = true
				name := "q-" + ql.Key
				f := c.Flags().Lookup(name)
				if f == nil {
					t.Errorf("%s: no --%s flag", op.ID, name)
					continue
				}
				qi, _ := newQualInput(ql)
				switch {
				case qi.isBoolFlag():
					if f.Value.Type() != "bool" {
						t.Errorf("%s --%s: type %s, want bool", op.ID, name, f.Value.Type())
					}
				case f.Value.Type() != "string":
					t.Errorf("%s --%s: type %s, want string", op.ID, name, f.Value.Type())
				}
				wantEnum := ql.Values
				if qi.isBoolFlag() {
					wantEnum = nil
				}
				if got := enumOf(t, c, name); !reflect.DeepEqual(got, wantEnum) {
					t.Errorf("%s --%s: enum = %v, want %v", op.ID, name, got, wantEnum)
				}
			}
		}
	}
	for _, typ := range []string{"enum", "sort", "date", "reference", "boolean", "number", "text"} {
		if !types[typ] {
			t.Errorf("no %s qualifier in the pinned contract", typ)
		}
	}
}

func TestObjectFlagsCarryContractSchema(t *testing.T) {
	sp := pinnedSpec(t)
	byOp := commandsByOperation(testRoot(t, sp, "http://unused.invalid", fastCfg()))
	var n int
	for _, op := range sp.Operations {
		if op.Body == nil {
			continue
		}
		c := byOp[op.ID]
		for _, f := range op.Body.Fields {
			got := annotation(t, c, flagOf(f.Name), airef.SchemaAnnotation)
			if kindOf(f.Type, f.ItemType) != kJSON {
				if got != nil || f.Schema != nil {
					t.Errorf("%s --%s: unexpected schema", op.ID, f.Name)
				}
				continue
			}
			n++
			if len(got) != 1 {
				t.Errorf("%s --%s: no schema annotation", op.ID, f.Name)
				continue
			}
			var a, b any
			if json.Unmarshal([]byte(got[0]), &a) != nil || json.Unmarshal(f.Schema, &b) != nil || !reflect.DeepEqual(a, b) {
				t.Errorf("%s --%s: schema %s != contract %s", op.ID, f.Name, got[0], f.Schema)
			}
		}
	}
	if n == 0 {
		t.Error("the pinned contract has no object field to check")
	}
}

// qOf dry-runs a command and returns the decoded q parameter, and whether it
// was sent at all.
func qOf(t *testing.T, args ...string) (string, bool, error) {
	t.Helper()
	srv := newStub(t)
	all := append([]string{"work-items", "list", "--dry-run", "--json"}, args...)
	stdout, _, err := run(t, pinnedSpec(t), srv.URL, all...)
	if err != nil {
		return "", false, err
	}
	var m struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(stdout), &m); err != nil {
		t.Fatalf("dry-run output %q: %v", stdout, err)
	}
	u, err := url.Parse(m.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(srv.requests()) != 0 {
		t.Fatal("a dry run reached the server")
	}
	vals, ok := u.Query()["q"]
	if !ok {
		return "", false, nil
	}
	return vals[0], true, nil
}

// TestNegationFlagsMatchContract pins the --q-not-<key> flags to the
// contract: one exists exactly for the qualifiers whose description documents
// "-<key>:" (never for sort or the boolean flag filter).
func TestNegationFlagsMatchContract(t *testing.T) {
	sp := pinnedSpec(t)
	byOp := commandsByOperation(testRoot(t, sp, "http://unused.invalid", fastCfg()))
	n := 0
	for _, op := range sp.Operations {
		c := byOp[op.ID]
		for _, q := range op.Query {
			for _, ql := range q.Qualifiers {
				want := ql.Type != spec.QualSort && ql.Type != spec.QualBoolean &&
					strings.Contains(ql.Description, "`-"+ql.Key+":`")
				got := c.Flags().Lookup("q-not-"+ql.Key) != nil
				if got != want {
					t.Errorf("%s --q-not-%s: exists = %v, want %v", op.ID, ql.Key, got, want)
				}
				if want {
					n++
				}
			}
		}
	}
	if n == 0 {
		t.Error("the pinned contract documents no negatable qualifier")
	}
}

func TestQualifierJoin(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
		sent bool
	}{
		{"typed", []string{"--q-status", "open", "--q-product", "12"}, "status:open product:12", true},
		{"order follows contract not argv", []string{"--q-product", "12", "--q-status", "open"}, "status:open product:12", true},
		{"free text appended", []string{"--q-status", "open", "--q", "foo"}, "status:open foo", true},
		{"free only unchanged", []string{"--q", "status:open foo"}, "status:open foo", true},
		{"nothing", nil, "", false},
		{"comma list", []string{"--q-status", "open,failed"}, "status:open,failed", true},
		{"reference key", []string{"--q-product", "my-product"}, "product:my-product", true},
		{"negated enum", []string{"--q-not-status", "failed"}, "-status:failed", true},
		{"negated enum list", []string{"--q-not-status", "open,failed"}, "-status:open,failed", true},
		{"negated reference", []string{"--q-not-product", "12"}, "-product:12", true},
		{"positive then negated", []string{"--q-not-status", "failed", "--q-status", "open"}, "status:open -status:failed", true},
		{"negated in contract order", []string{"--q-not-product", "12", "--q-product", "7", "--q-status", "open"}, "status:open product:7 -product:12", true},
		{"boolean on", []string{"--q-is"}, "is:blocked", true},
		{"boolean off", []string{"--q-is=false"}, "-is:blocked", true},
		{"date", []string{"--q-since", "2026-10-01"}, "since:2026-10-01", true},
		{"date comparator", []string{"--q-since", ">=2026-10-01"}, "since:>=2026-10-01", true},
		{"date range", []string{"--q-since", "2026-10-01..2026-10-31"}, "since:2026-10-01..2026-10-31", true},
		{"sort", []string{"--q-sort", "updated-desc"}, "sort:updated-desc", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, sent, err := qOf(t, tc.args...)
			if err != nil || got != tc.want || sent != tc.sent {
				t.Fatalf("q = %q (sent %v), err %v; want %q (sent %v)", got, sent, err, tc.want, tc.sent)
			}
		})
	}
}

func TestQualifierRejectsBadValues(t *testing.T) {
	cases := map[string][]string{
		"enum":                         {"--q-status", "blocked"},
		"enum in list":                 {"--q-status", "open,blocked"},
		"empty":                        {"--q-status", " "},
		"sort":                         {"--q-sort", "nope"},
		"date":                         {"--q-since", "yesterday"},
		"date impossible":              {"--q-since", "2026-13-40"},
		"date range":                   {"--q-since", "2026-10-01..2026-99-01"},
		"reference":                    {"--q-product", "a b"},
		"reference list":               {"--q-product", "12,,"},
		"reference comma list":         {"--q-product", "my-product,12"},
		"sort comma list":              {"--q-sort", "updated-desc,created-asc"},
		"negated enum":                 {"--q-not-status", "blocked"},
		"negated enum in list":         {"--q-not-status", "open,blocked"},
		"negated empty":                {"--q-not-status", " "},
		"negated reference":            {"--q-not-product", "a b"},
		"negated reference comma list": {"--q-not-product", "my-product,12"},
		"no negated sort":              {"--q-not-sort", "updated-desc"},
		"no negated boolean":           {"--q-not-is"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := qOf(t, args...)
			if err == nil || !strings.Contains(err.Error(), "--q-") {
				t.Fatalf("err = %v, want a usage error naming the flag", err)
			}
		})
	}
}

// qualCmd builds a throwaway list command so every qualifier type is
// exercised regardless of which ones the pinned contract uses where.
func qualCmd(t *testing.T, quals ...spec.Qualifier) (*cobra.Command, []qualInput) {
	t.Helper()
	cmd := &cobra.Command{Use: "x"}
	var out []qualInput
	for _, q := range quals {
		qi, in := newQualInput(q)
		if err := addFlag(cmd, in, q.Description); err != nil {
			t.Fatal(err)
		}
		out = append(out, qi)
		if nqi, nin, ok := qi.negation(); ok {
			if err := addFlag(cmd, nin, q.Description); err != nil {
				t.Fatal(err)
			}
			out = append(out, nqi)
		}
	}
	return cmd, out
}

func TestQualifierTypes(t *testing.T) {
	quals := []spec.Qualifier{
		{Key: "n", Type: spec.QualNumber},
		{Key: "t", Type: spec.QualText},
		{Key: "e", Type: spec.QualEnum},
		{Key: "b", Type: spec.QualBoolean},
		{Key: "tf", Type: spec.QualBoolean, Values: []string{"true", "false"}},
		{Key: "m", Type: spec.QualBoolean, Values: []string{"a", "b"}},
		{Key: "one", Type: spec.QualBoolean, Values: []string{"paused"}},
	}
	cases := []struct {
		args []string
		want string
		err  bool
	}{
		{[]string{"--q-n", "5"}, "n:5", false},
		{[]string{"--q-n", ">=5"}, "n:>=5", false},
		{[]string{"--q-n", "-5..7"}, "n:-5..7", false},
		{[]string{"--q-n", "x"}, "", true},
		{[]string{"--q-t", "plain"}, "t:plain", false},
		{[]string{"--q-t", `two "words"`}, `t:"two \"words\""`, false},
		{[]string{"--q-e", "anything"}, "e:anything", false},
		{[]string{"--q-b"}, "b:true", false},
		{[]string{"--q-b=false"}, "b:false", false},
		{[]string{"--q-tf=false"}, "tf:false", false},
		{[]string{"--q-m", "a"}, "m:a", false},
		{[]string{"--q-m", "c"}, "", true},
		{[]string{"--q-one"}, "one:paused", false},
		{[]string{"--q-one=false"}, "-one:paused", false},
	}
	for _, tc := range cases {
		cmd, qs := qualCmd(t, quals...)
		if err := cmd.ParseFlags(tc.args); err != nil {
			t.Fatal(err)
		}
		got, ok, err := joinQuery(cmd.Flags(), qs, "")
		if tc.err != (err != nil) || got != tc.want || ok == (tc.want == "") && !tc.err {
			t.Errorf("%v: q = %q ok=%v err=%v; want %q err=%v", tc.args, got, ok, err, tc.want, tc.err)
		}
	}
}

func TestQualifierNegationTypes(t *testing.T) {
	neg := func(key string) string { return "`-" + key + ":` negates." }
	quals := []spec.Qualifier{
		{Key: "d", Type: spec.QualDate, Description: neg("d")},
		{Key: "n", Type: spec.QualNumber, Description: neg("n")},
		{Key: "t", Type: spec.QualText, Description: neg("t")},
		{Key: "b", Type: spec.QualBoolean, Description: neg("b")},
		{Key: "s", Type: spec.QualSort, Values: []string{"a", "b"}, Description: neg("s")},
		{Key: "plain", Type: spec.QualText, Description: "no negation documented"},
	}
	cases := []struct {
		args []string
		want string
		err  bool
	}{
		{[]string{"--q-not-d", ">=2026-10-01"}, "-d:>=2026-10-01", false},
		{[]string{"--q-not-d", "yesterday"}, "", true},
		{[]string{"--q-not-n", "5"}, "-n:5", false},
		{[]string{"--q-not-n", "x"}, "", true},
		{[]string{"--q-not-t", "two words"}, `-t:"two words"`, false},
		{[]string{"--q-t", "a", "--q-not-t", "b"}, "t:a -t:b", false},
		{[]string{"--q-b=false"}, "b:false", false},
	}
	for _, tc := range cases {
		cmd, qs := qualCmd(t, quals...)
		if err := cmd.ParseFlags(tc.args); err != nil {
			t.Fatal(err)
		}
		got, _, err := joinQuery(cmd.Flags(), qs, "")
		if tc.err != (err != nil) || got != tc.want {
			t.Errorf("%v: q = %q err=%v; want %q err=%v", tc.args, got, err, tc.want, tc.err)
		}
	}
	cmd, _ := qualCmd(t, quals...)
	for _, k := range []string{"b", "s", "plain"} {
		if cmd.Flags().Lookup("q-not-"+k) != nil {
			t.Errorf("--q-not-%s must not exist", k)
		}
	}
}

func TestQualifierGenerationErrors(t *testing.T) {
	doc := func(extra string) string {
		return `
openapi: "3.0.3"
paths:
  /things:
    get:
      operationId: listThings
      parameters:
        - name: q
          in: query
          schema: {type: string}
          x-qualifiers:
            - {key: status, type: text, description: "\x60-status:\x60 negates"}
` + extra + `
      responses:
        "200": {content: {application/vnd.api+json: {schema: {properties: {data: {type: array}}}}}}
`
	}
	for name, extra := range map[string]string{
		"flag collision":     "        - {name: q-status, in: query, schema: {type: string}}",
		"negation collision": "        - {name: q-not-status, in: query, schema: {type: string}}",
		"duplicate enum":     "        - {name: kind, in: query, schema: {type: string, enum: [a, a]}}",
		"empty enum member":  "        - {name: kind, in: query, schema: {type: string, enum: [\"\"]}}",
	} {
		t.Run(name, func(t *testing.T) {
			sp, err := spec.Load([]byte(doc(extra)))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Build(sp, fastCfg()); err == nil {
				t.Fatal("expected a generation error")
			}
		})
	}
}
