package gen

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/dotdevlabs/ctlkit/pkg/clierror"

	"github.com/dotdevlabs/autonomatctl/internal/spec"
)

// queryParam is the contract's list filter parameter; its x-qualifiers become
// --q-<key> flags that the command joins into it.
const queryParam = "q"

// qualFlagPrefix prefixes every qualifier flag.
const qualFlagPrefix = "q-"

// qualInput is one --q-<key> flag.
type qualInput struct {
	spec.Qualifier
	flag string
	// boolean qualifiers: the value emitted for true (and, negated, for false);
	// empty for the true/false grammar.
	boolValue string
}

var (
	comparator = `(?:>=|>|<=|<)?`
	dateRE     = regexp.MustCompile(`^` + comparator + `\d{4}-\d{2}-\d{2}$`)
	dateRange  = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\.\.(\d{4}-\d{2}-\d{2})$`)
	numberRE   = regexp.MustCompile(`^` + comparator + `-?\d+$`)
	numRange   = regexp.MustCompile(`^-?\d+\.\.-?\d+$`)
	refRE      = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

// newQualInput maps a documented qualifier to its flag. A boolean qualifier
// whose values are neither true/false nor a single flag name has no sensible
// bool mapping and is exposed as an enum string instead.
func newQualInput(q spec.Qualifier) (qualInput, input) {
	qi := qualInput{Qualifier: q, flag: qualFlagPrefix + q.Key}
	in := input{name: qi.flag, kind: kString, typ: "string", query: true}
	switch q.Type {
	case spec.QualEnum, spec.QualSort:
		in.enum = q.Values
	case spec.QualBoolean:
		if qi.isBoolFlag() {
			in.kind, in.typ = kBool, "boolean"
			if len(q.Values) == 1 {
				qi.boolValue = q.Values[0]
			}
		} else {
			in.enum = q.Values
		}
	}
	return qi, in
}

// term renders the flag's value as a q-grammar term; ok is false when the flag
// was not given.
func (qi qualInput) term(fs *pflag.FlagSet) (string, bool, error) {
	if !fs.Changed(qi.flag) {
		return "", false, nil
	}
	bad := func(msg string) error {
		return clierror.New(clierror.CodeUsage, fmt.Sprintf("--%s: %s", qi.flag, msg), "")
	}
	if qi.isBoolFlag() {
		on, _ := fs.GetBool(qi.flag)
		switch {
		case qi.boolValue == "":
			return fmt.Sprintf("%s:%t", qi.Key, on), true, nil
		case on:
			return qi.Key + ":" + qi.boolValue, true, nil
		}
		return "-" + qi.Key + ":" + qi.boolValue, true, nil
	}
	v, _ := fs.GetString(qi.flag)
	if strings.TrimSpace(v) == "" {
		return "", false, bad("value must not be empty")
	}
	switch qi.Type {
	case spec.QualEnum, spec.QualSort, spec.QualBoolean:
		if len(qi.Values) > 0 {
			for _, e := range strings.Split(v, ",") {
				if !contains(qi.Values, e) {
					return "", false, bad(fmt.Sprintf("%q is not one of: %s", e, strings.Join(qi.Values, ", ")))
				}
			}
		}
	case spec.QualDate:
		if err := validDate(v); err != nil {
			return "", false, bad(err.Error())
		}
	case spec.QualNumber:
		if !numberRE.MatchString(v) && !numRange.MatchString(v) {
			return "", false, bad(fmt.Sprintf("%q is not an integer (optionally with >=, >, <=, < or a from..to range)", v))
		}
	case spec.QualReference:
		for _, r := range strings.Split(v, ",") {
			if !refRE.MatchString(r) {
				return "", false, bad(fmt.Sprintf("%q is not a record id or key", r))
			}
		}
	case spec.QualText:
		if strings.ContainsAny(v, " \t\"") {
			v = `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
		}
	}
	return qi.Key + ":" + v, true, nil
}

// isBoolFlag reports whether the flag is a bool flag.
func (qi qualInput) isBoolFlag() bool {
	return qi.Type == spec.QualBoolean && (len(qi.Values) < 2 || (len(qi.Values) == 2 && contains(qi.Values, "true") && contains(qi.Values, "false")))
}

func validDate(v string) error {
	if m := dateRange.FindStringSubmatch(v); m != nil {
		for _, d := range m[1:] {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				return fmt.Errorf("%q is not a valid date", d)
			}
		}
		return nil
	}
	if !dateRE.MatchString(v) {
		return fmt.Errorf("%q is not a date (YYYY-MM-DD, optionally with >=, >, <=, < or a from..to range)", v)
	}
	d := strings.TrimLeft(v, "<>=")
	if _, err := time.Parse("2006-01-02", d); err != nil {
		return fmt.Errorf("%q is not a valid date", d)
	}
	return nil
}

// joinQuery builds the single q parameter: qualifier terms in contract order,
// then the free text. ok is false when no qualifier flag was given.
func joinQuery(fs *pflag.FlagSet, quals []qualInput, free string) (string, bool, error) {
	var terms []string
	for _, qi := range quals {
		t, ok, err := qi.term(fs)
		if err != nil {
			return "", false, err
		}
		if ok {
			terms = append(terms, t)
		}
	}
	if len(terms) == 0 {
		return "", false, nil
	}
	if strings.TrimSpace(free) != "" {
		terms = append(terms, free)
	}
	return strings.Join(terms, " "), true, nil
}
