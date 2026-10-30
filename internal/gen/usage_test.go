package gen

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func norm(s string) string { return space.ReplaceAllString(strings.TrimSpace(s), " ") }

// TestFlagUsageNotTruncated checks that every documented query parameter and
// body field keeps its whole contract description in the flag usage, and that
// no usage in the command tree carries a truncation marker of its own.
func TestFlagUsageNotTruncated(t *testing.T) {
	sp := pinnedSpec(t)
	root := testRoot(t, sp, "http://unused", fastCfg())
	byOp := commandsByOperation(root)
	descs := map[string]bool{}
	checked := 0
	check := func(op, name, desc string) {
		desc = norm(desc)
		if desc == "" {
			return
		}
		descs[desc] = true
		f := byOp[op].Flags().Lookup(name)
		if f == nil {
			return
		}
		checked++
		if len(f.Usage) < len(desc) || !strings.HasPrefix(f.Usage, desc) {
			t.Errorf("%s --%s: usage %q is shorter than or differs from contract description %q", op, name, f.Usage, desc)
		}
	}
	for _, op := range sp.Operations {
		for _, q := range op.Query {
			check(op.ID, q.Name, q.Description)
		}
		if op.Body != nil {
			for _, f := range op.Body.Fields {
				check(op.ID, f.Name, f.Description)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no flag was checked")
	}
	var visit func(c *cobra.Command)
	visit = func(c *cobra.Command) {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if strings.HasSuffix(f.Usage, "...") && !descEndsWithEllipsis(f.Usage, descs) {
				t.Errorf("%s --%s: usage ends in %q", c.CommandPath(), f.Name, "...")
			}
		})
		for _, s := range c.Commands() {
			visit(s)
		}
	}
	visit(root)
}

func descEndsWithEllipsis(usage string, descs map[string]bool) bool {
	for d := range descs {
		if strings.HasSuffix(d, "...") && strings.HasPrefix(usage, d) {
			return true
		}
	}
	return false
}

// TestQUsageEqualsContract compares one --q usage with the contract text.
func TestQUsageEqualsContract(t *testing.T) {
	sp := pinnedSpec(t)
	op := find(t, sp, "listActivityEvents")
	var want string
	for _, q := range op.Query {
		if q.Name == "q" {
			want = norm(q.Description)
		}
	}
	if len(want) <= 160 {
		t.Fatalf("contract q description is %d chars; expected a long one", len(want))
	}
	f := commandsByOperation(testRoot(t, sp, "http://unused", fastCfg()))["listActivityEvents"].Flags().Lookup("q")
	if f == nil {
		t.Fatal("missing --q")
	}
	if f.Usage != want {
		t.Errorf("--q usage = %q, want %q", f.Usage, want)
	}
}

func TestUsageHelper(t *testing.T) {
	long := strings.Repeat("word ", 80)
	tests := []struct {
		name string
		desc string
		in   input
		want string
	}{
		{"long multi-line kept whole", "  a\n\n  b\t" + long, input{}, "a b " + strings.TrimSpace(long)},
		{"all suffixes in order", "d", input{kind: kJSON, typ: "object", enum: []string{"x", "y"}, sendDef: true, def: "x", required: true},
			`d (JSON object) (one of: x, y) (default "x") (required)`},
	}
	for _, tc := range tests {
		if got := usage(tc.desc, tc.in); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
