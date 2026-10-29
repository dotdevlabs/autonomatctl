package gen

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode"

	"github.com/dotdevlabs/autonomatctl/internal/spec"
)

// derive maps an operation to its command: the resource path and the verb.
//
//	GET    /work_items                          -> work-items list
//	GET    /work_items/{id}                     -> work-items get
//	POST   /work_items/{work_item_id}/executions -> work-items executions create
//	POST   /products/{product_id}/sunset (no body) -> products sunset
//	PATCH  /account                             -> account update
//
// The names come from the path and method alone; nothing is mapped by hand.
func derive(op spec.Operation) (group []string, verb string, err error) {
	segs := strings.Split(strings.Trim(op.Path, "/"), "/")
	var names []string
	for _, s := range segs {
		if !isParam(s) {
			names = append(names, kebab(s))
		}
	}
	if len(names) == 0 {
		return nil, "", fmt.Errorf("%s %s: path has no resource segment", op.Method, op.Path)
	}
	last := segs[len(segs)-1]
	// An action: a body-less POST on a member, e.g. /products/{id}/sunset.
	if op.Method == http.MethodPost && op.Body == nil && !isParam(last) &&
		len(segs) >= 3 && isParam(segs[len(segs)-2]) {
		return names[:len(names)-1], names[len(names)-1], nil
	}
	switch op.Method {
	case http.MethodGet:
		if op.Collection {
			verb = "list"
		} else {
			verb = "get"
		}
	case http.MethodPost:
		verb = "create"
	case http.MethodPatch:
		verb = "update"
	case http.MethodPut:
		verb = "replace"
	case http.MethodDelete:
		verb = "delete"
	default:
		return nil, "", fmt.Errorf("%s %s: unsupported method", op.Method, op.Path)
	}
	return names, verb, nil
}

func isParam(seg string) bool { return strings.HasPrefix(seg, "{") }

var wordBreak = regexp.MustCompile(`[_\s]+`)

// kebab converts snake_case and camelCase identifiers to kebab-case.
func kebab(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i, r := range rs {
		if unicode.IsUpper(r) {
			prevLower := i > 0 && (unicode.IsLower(rs[i-1]) || unicode.IsDigit(rs[i-1]))
			acronymEnd := i > 0 && unicode.IsUpper(rs[i-1]) && i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if prevLower || acronymEnd {
				b.WriteByte('-')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return wordBreak.ReplaceAllString(b.String(), "-")
}
