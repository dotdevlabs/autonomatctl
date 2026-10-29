// Package render turns API responses into CLI output: the raw JSON:API
// document for --json, readable text otherwise, and the errors body on
// failure.
package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/dotdevlabs/ctlkit/pkg/clierror"
	"github.com/dotdevlabs/ctlkit/pkg/output"
)

const (
	maxCell      = 60
	maxRawError  = 500
	maxTableCols = 5
)

// Options select the output mode.
type Options struct {
	JSON   bool
	Format string // Go template over the decoded document
}

// Success writes a 2xx response. With JSON the body is written byte for byte
// (plus a trailing newline when missing); an empty body writes nothing. Hints
// for the human go to errW so stdout stays the document.
func Success(w, errW io.Writer, o Options, status int, body []byte) error {
	if o.JSON {
		if len(body) == 0 {
			return nil
		}
		return writeRaw(w, body)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		_, err := fmt.Fprintf(w, "OK (HTTP %d)\n", status)
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return writeRaw(w, body)
	}
	if o.Format != "" {
		return output.New(false, o.Format, w, errW).Render(nil, nil, doc)
	}
	if links, ok := doc["links"].(map[string]any); ok && links["next"] != nil {
		_, _ = fmt.Fprintln(errW, "more results available: re-run with --all to fetch every page, or --json to see links.next")
	}
	switch data := doc["data"].(type) {
	case []any:
		return table(w, data)
	case map[string]any:
		return resource(w, data)
	default:
		return writeRaw(w, body)
	}
}

func writeRaw(w io.Writer, body []byte) error {
	if _, err := w.Write(body); err != nil {
		return err
	}
	if !bytes.HasSuffix(body, []byte("\n")) {
		_, err := io.WriteString(w, "\n")
		return err
	}
	return nil
}

func resource(w io.Writer, r map[string]any) error {
	var b strings.Builder
	for _, k := range []string{"type", "id"} {
		if v, ok := r[k]; ok {
			fmt.Fprintf(&b, "%s: %s\n", k, cell(v, 0))
		}
	}
	attrs, _ := r["attributes"].(map[string]any)
	for _, k := range sortedKeys(attrs) {
		fmt.Fprintf(&b, "%s: %s\n", k, cell(attrs[k], 0))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func table(w io.Writer, rows []any) error {
	if len(rows) == 0 {
		_, err := io.WriteString(w, "No results.\n")
		return err
	}
	res := make([]map[string]any, 0, len(rows))
	counts := map[string]int{}
	for _, r := range rows {
		m, _ := r.(map[string]any)
		res = append(res, m)
		attrs, _ := m["attributes"].(map[string]any)
		for k, v := range attrs {
			switch v.(type) {
			case map[string]any, []any:
			default:
				counts[k]++
			}
		}
	}
	cols := chooseColumns(counts)
	headers := []output.Column{{Header: "ID"}}
	for _, c := range cols {
		headers = append(headers, output.Column{Header: c})
	}
	out := make([][]string, 0, len(res))
	for _, m := range res {
		row := []string{cell(m["id"], maxCell)}
		attrs, _ := m["attributes"].(map[string]any)
		for _, c := range cols {
			row = append(row, cell(attrs[c], maxCell))
		}
		out = append(out, row)
	}
	return output.New(false, "", w, io.Discard).Render(headers, out, nil)
}

// chooseColumns prefers identifying attributes, then the most commonly
// present scalars in name order.
func chooseColumns(counts map[string]int) []string {
	var cols []string
	for _, k := range []string{"name", "title", "status"} {
		if counts[k] > 0 {
			cols = append(cols, k)
		}
	}
	for _, k := range sortedIntKeys(counts) {
		if len(cols) >= maxTableCols {
			break
		}
		if !contains(cols, k) {
			cols = append(cols, k)
		}
	}
	return cols
}

func cell(v any, limit int) string {
	var s string
	switch t := v.(type) {
	case nil:
		return "-"
	case string:
		s = strings.Join(strings.Fields(t), " ")
	default:
		b, _ := json.Marshal(t)
		s = string(b)
	}
	if limit > 0 && utf8.RuneCountInString(s) > limit {
		r := []rune(s)
		s = string(r[:limit-1]) + "…"
	}
	return s
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedIntKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

// Failure writes the error body to stdout when JSON is requested and returns
// the error to exit with: its message carries the status and every JSON:API
// error object, its code maps from the HTTP status.
func Failure(w io.Writer, o Options, status int, body []byte) error {
	if o.JSON && len(body) > 0 {
		if err := writeRaw(w, body); err != nil {
			return err
		}
	}
	return clierror.New(ExitCode(status), Message(status, body), "")
}

// ExitCode maps an HTTP status to the stable exit code.
func ExitCode(status int) clierror.ErrorCode {
	switch {
	case status == http.StatusUnauthorized:
		return clierror.CodeUnauthorized
	case status == http.StatusForbidden:
		return clierror.CodeForbidden
	case status == http.StatusNotFound:
		return clierror.CodeNotFound
	case status == http.StatusConflict:
		return clierror.CodeConflict
	case status >= 400 && status < 500:
		return clierror.CodeBadRequest
	default:
		return clierror.CodeServerError
	}
}

// Message describes a failed response: the status line followed by one line
// per entry of the top-level errors array, or the truncated raw body when the
// response is not a JSON:API error document.
func Message(status int, body []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP %d %s", status, http.StatusText(status))
	var doc struct {
		Errors []struct {
			Status string `json:"status"`
			Code   string `json:"code"`
			Title  string `json:"title"`
			Detail string `json:"detail"`
			Source struct {
				Pointer   string `json:"pointer"`
				Parameter string `json:"parameter"`
			} `json:"source"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &doc) == nil && len(doc.Errors) > 0 {
		for _, e := range doc.Errors {
			parts := []string{}
			for _, p := range []string{e.Title, e.Detail} {
				if p != "" {
					parts = append(parts, p)
				}
			}
			line := strings.Join(parts, ": ")
			if line == "" {
				line = "error"
			}
			if e.Code != "" {
				line += " [" + e.Code + "]"
			}
			if e.Source.Pointer != "" {
				line += " (at " + e.Source.Pointer + ")"
			} else if e.Source.Parameter != "" {
				line += " (parameter " + e.Source.Parameter + ")"
			}
			b.WriteString("\n  - " + line)
		}
		return b.String()
	}
	if raw := strings.TrimSpace(string(body)); raw != "" {
		if len(raw) > maxRawError {
			raw = strings.ToValidUTF8(raw[:maxRawError], "") + "…"
		}
		b.WriteString("\n  " + raw)
	}
	return b.String()
}

// DryRunInfo is the request that --dry-run would have sent.
type DryRunInfo struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// DryRun prints the request without sending it: JSON with --json, readable
// text otherwise.
func DryRun(w io.Writer, o Options, info DryRunInfo) error {
	if o.JSON {
		return output.JSONTo(w, info)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", info.Method, info.URL)
	for _, k := range sortedStringKeys(info.Headers) {
		fmt.Fprintf(&b, "%s: %s\n", k, info.Headers[k])
	}
	if len(info.Body) > 0 {
		b.WriteString("\n" + string(info.Body) + "\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
