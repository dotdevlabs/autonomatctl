// Package apiclient is the HTTP boundary of autonomatctl. It sends requests
// whose path and query come from the contract, retries transient failures,
// and follows JSON:API links.next verbatim without ever building page URLs.
package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dotdevlabs/ctlkit/pkg/clierror"
)

const (
	// MediaType is the JSON:API media type sent as Accept and Content-Type.
	MediaType = "application/vnd.api+json"

	defaultMaxPages    = 500
	defaultAttempts    = 3
	defaultBackoff     = 500 * time.Millisecond
	maxRetryAfter      = 30 * time.Second
	maxResponseBytes   = 64 << 20
	defaultHTTPTimeout = 60 * time.Second
)

// Options configure a Client.
type Options struct {
	BaseURL    string
	Token      string
	UserAgent  string
	HTTPClient *http.Client
	// Verbose, when set, receives one line per HTTP exchange. It never
	// receives credentials.
	Verbose     io.Writer
	MaxAttempts int           // total tries for a retryable request; default 3
	Backoff     time.Duration // base delay between tries; default 500ms
	MaxPages    int           // bound for FetchAll; default 500
}

// Client talks to one Autonomat deployment.
type Client struct {
	base *url.URL
	opts Options
	http *http.Client
}

// Request is one call: Path is the already expanded contract path.
type Request struct {
	Method          string
	Path            string
	Query           url.Values
	Body            []byte
	ContentType     string
	signatureHeader string
	signature       string
}

var (
	headerName = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")
	jsonString = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
)

// SignDelivery configures the source's signature credential for this request.
func (r *Request) SignDelivery(header, signature string) error {
	name := strings.ToLower(header)
	switch name {
	case "accept", "accept-encoding", "authorization", "connection", "cookie", "cookie2", "expect",
		"forwarded", "host", "keep-alive", "origin", "referer", "set-cookie", "te", "trailer",
		"transfer-encoding", "upgrade", "user-agent", "via", "www-authenticate":
		return clierror.New(clierror.CodeUsage, "signature header is reserved", "")
	}
	if strings.HasPrefix(name, "content-") {
		return clierror.New(clierror.CodeUsage, "signature header is reserved", "")
	}
	if !headerName.MatchString(header) || strings.HasPrefix(name, "proxy-") || strings.HasPrefix(name, "x-forwarded-") {
		return clierror.New(clierror.CodeUsage, "invalid signature header", "")
	}
	if strings.TrimSpace(signature) == "" || strings.ContainsFunc(signature, func(r rune) bool { return r < 32 || r == 127 }) {
		return clierror.New(clierror.CodeUsage, "invalid or missing AUTONOMATCTL_SIGNAL_SIGNATURE", "")
	}
	r.signatureHeader, r.signature = http.CanonicalHeaderKey(header), signature
	return nil
}

// Response is a raw HTTP response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// ValidateBaseURL checks the base URL already resolved by ctlkit (context,
// AUTONOMATCTL_BASE_URL override or built-in default).
func ValidateBaseURL(base string) (string, error) {
	if base == "" {
		return "", clierror.New(clierror.CodeUsage, "no base URL configured",
			"set one with 'auth login --name <ctx> --url <url>'")
	}
	if _, err := parseBase(base); err != nil {
		return "", err
	}
	return base, nil
}

func parseBase(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, clierror.New(clierror.CodeUsage, fmt.Sprintf("invalid base URL %q", raw),
			"it must be an absolute http(s) URL")
	}
	return u, nil
}

// New validates the options and returns a Client.
func New(o Options) (*Client, error) {
	base, err := parseBase(o.BaseURL)
	if err != nil {
		return nil, err
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = defaultAttempts
	}
	if o.Backoff < 0 {
		o.Backoff = 0
	} else if o.Backoff == 0 {
		o.Backoff = defaultBackoff
	}
	if o.MaxPages <= 0 {
		o.MaxPages = defaultMaxPages
	}
	if o.UserAgent == "" {
		o.UserAgent = "autonomatctl"
	}
	return &Client{base: base, opts: o, http: o.HTTPClient}, nil
}

// URL returns the absolute URL of r.
func (c *Client) URL(r Request) string {
	base := *c.base
	if r.signatureHeader != "" {
		base.User = nil
	}
	u := strings.TrimRight(base.String(), "/") + r.Path
	if len(r.Query) > 0 {
		u += "?" + r.Query.Encode()
	}
	return u
}

// Headers returns the request headers that would be sent, with the token
// redacted. It backs --dry-run.
func (c *Client) Headers(r Request) map[string]string {
	h := map[string]string{"Accept": MediaType, "User-Agent": c.opts.UserAgent, "Authorization": "Bearer <redacted>"}
	if r.signatureHeader != "" {
		delete(h, "Authorization")
		h[r.signatureHeader] = "<redacted>"
	}
	if len(r.Body) > 0 {
		h["Content-Type"] = contentType(r)
	}
	return h
}

func contentType(r Request) string {
	if r.ContentType != "" {
		return r.ContentType
	}
	return MediaType
}

// Do sends r and returns the response whatever its status. Only transport
// failures are errors.
func (c *Client) Do(ctx context.Context, r Request) (*Response, error) {
	return c.send(ctx, c.URL(r), r)
}

func (c *Client) send(ctx context.Context, target string, r Request) (*Response, error) {
	if c.opts.Token == "" && r.signatureHeader == "" {
		return nil, clierror.New(clierror.CodeUnauthorized, "no API token configured",
			"run 'auth login --name <ctx> --url <url> --token <token>' or set AUTONOMATCTL_TOKEN")
	}
	client := c.http
	if r.signatureHeader != "" {
		// A source credential and the exact bytes it authenticates belong to
		// the configured endpoint, even if that endpoint returns a redirect.
		signed := *client
		signed.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client = &signed
	}
	var lastStatus int
	for attempt := 1; ; attempt++ {
		var rdr io.Reader
		if len(r.Body) > 0 {
			rdr = bytes.NewReader(r.Body)
		}
		req, err := http.NewRequestWithContext(ctx, r.Method, target, rdr)
		if err != nil {
			return nil, clierror.New(clierror.CodeUsage, "cannot build request: "+err.Error(), "")
		}
		req.Header.Set("Accept", MediaType)
		if r.signatureHeader != "" {
			req.Header.Set(r.signatureHeader, r.signature)
		} else {
			req.Header.Set("Authorization", "Bearer "+c.opts.Token)
		}
		req.Header.Set("User-Agent", c.opts.UserAgent)
		if len(r.Body) > 0 {
			req.Header.Set("Content-Type", contentType(r))
		}

		resp, err := client.Do(req)
		if err != nil {
			if r.signatureHeader != "" {
				return nil, clierror.New(clierror.CodeServerError, "signed delivery request failed", "")
			}
			return nil, clierror.New(clierror.CodeServerError, "request failed: "+redactErr(err), "")
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		_ = resp.Body.Close()
		if err != nil {
			if r.signatureHeader != "" {
				return nil, clierror.New(clierror.CodeServerError, "reading signed delivery response failed", "")
			}
			return nil, clierror.New(clierror.CodeServerError, "reading response failed: "+err.Error(), "")
		}
		if r.signatureHeader != "" && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
			// Preserve the server's error document while masking an echoed credential.
			data = redactSignature(data, r.signature)
		}
		if c.opts.Verbose != nil {
			_, _ = fmt.Fprintf(c.opts.Verbose, "%s %s -> %d\n", r.Method, target, resp.StatusCode)
		}
		lastStatus = resp.StatusCode
		out := &Response{Status: resp.StatusCode, Header: resp.Header, Body: data}
		if attempt >= c.opts.MaxAttempts || !retryable(r.Method, lastStatus) {
			return out, nil
		}
		if err := sleep(ctx, c.delay(resp.Header, attempt)); err != nil {
			return nil, clierror.New(clierror.CodeServerError, "request cancelled: "+err.Error(), "")
		}
	}
}

func redactSignature(body []byte, signature string) []byte {
	if !json.Valid(body) {
		return bytes.ReplaceAll(body, []byte(signature), []byte("<redacted>"))
	}
	return jsonString.ReplaceAllFunc(body, func(raw []byte) []byte {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil || !strings.Contains(s, signature) {
			return raw
		}
		masked, _ := json.Marshal(strings.ReplaceAll(s, signature, "<redacted>"))
		return masked
	})
}

// redactErr drops the URL that net/http embeds in transport errors so that a
// query string is not echoed twice; the cause is kept.
func redactErr(err error) string {
	if ue, ok := err.(*url.Error); ok {
		return ue.Err.Error()
	}
	return err.Error()
}

// retryable: 429 is safe for any method (the server did not act); gateway
// errors are retried only for reads.
func retryable(method string, status int) bool {
	switch status {
	case http.StatusTooManyRequests:
		return true
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return method == http.MethodGet
	}
	return false
}

func (c *Client) delay(h http.Header, attempt int) time.Duration {
	if ra := h.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil && secs >= 0 {
			d := time.Duration(secs) * time.Second
			if d > maxRetryAfter {
				d = maxRetryAfter
			}
			return d
		}
	}
	return c.opts.Backoff * time.Duration(attempt)
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// FetchAll sends the GET r and then follows links.next verbatim until there is
// none. A single page is returned byte for byte; several pages are merged into
// one JSON:API document. If any page answers with an error status, that
// response is returned unmerged.
func (c *Client) FetchAll(ctx context.Context, r Request) (*Response, error) {
	cur := c.URL(r)
	resp, err := c.send(ctx, cur, Request{Method: http.MethodGet})
	if err != nil || resp.Status >= 400 {
		return resp, err
	}
	pages := []*Response{resp}
	seen := map[string]bool{cur: true}
	for {
		next, err := NextLink(resp.Body)
		if err != nil {
			return nil, err
		}
		if next == "" {
			break
		}
		base, _ := url.Parse(cur)
		ref, err := url.Parse(next)
		if err != nil {
			return nil, clierror.New(clierror.CodeServerError, fmt.Sprintf("invalid links.next %q", next), "")
		}
		target := base.ResolveReference(ref)
		if !strings.EqualFold(target.Scheme, c.base.Scheme) || !strings.EqualFold(target.Host, c.base.Host) {
			return nil, clierror.New(clierror.CodeServerError,
				fmt.Sprintf("refusing to follow links.next to another origin (%s)", target.Host),
				"the token is only sent to the configured base URL")
		}
		cur = target.String()
		if seen[cur] {
			return nil, clierror.New(clierror.CodeServerError, "links.next repeats an earlier page: "+cur, "")
		}
		if len(pages) >= c.opts.MaxPages {
			return nil, clierror.New(clierror.CodeServerError,
				fmt.Sprintf("stopped after %d pages", c.opts.MaxPages), "narrow the query")
		}
		seen[cur] = true
		resp, err = c.send(ctx, cur, Request{Method: http.MethodGet})
		if err != nil || resp.Status >= 400 {
			return resp, err
		}
		pages = append(pages, resp)
	}
	if len(pages) == 1 {
		return pages[0], nil
	}
	merged, err := Merge(pages)
	if err != nil {
		return nil, err
	}
	return &Response{Status: pages[0].Status, Header: pages[0].Header, Body: merged}, nil
}

// NextLink returns links.next of a JSON:API document: a string, or an object
// with an href. A missing, null or unparsable document yields "" and no error
// unless the body is JSON that is not an object.
func NextLink(doc []byte) (string, error) {
	var d struct {
		Links map[string]json.RawMessage `json:"links"`
	}
	if err := json.Unmarshal(doc, &d); err != nil {
		return "", clierror.New(clierror.CodeServerError, "response is not a JSON document: "+err.Error(), "")
	}
	raw, ok := d.Links["next"]
	if !ok {
		return "", nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	var o struct {
		Href string `json:"href"`
	}
	if json.Unmarshal(raw, &o) == nil {
		return o.Href, nil
	}
	return "", nil
}

// Merge combines paged collection documents: data and included are
// concatenated, links come from the first page without next, every other
// member is taken from the first page.
func Merge(pages []*Response) ([]byte, error) {
	var first map[string]json.RawMessage
	var data, included []json.RawMessage
	for i, p := range pages {
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(p.Body, &doc); err != nil {
			return nil, clierror.New(clierror.CodeServerError, "response is not a JSON document: "+err.Error(), "")
		}
		if i == 0 {
			first = doc
		}
		var d []json.RawMessage
		if err := json.Unmarshal(doc["data"], &d); err != nil {
			return nil, clierror.New(clierror.CodeServerError, "cannot merge pages: data is not an array", "")
		}
		data = append(data, d...)
		if raw, ok := doc["included"]; ok {
			var inc []json.RawMessage
			if err := json.Unmarshal(raw, &inc); err != nil {
				return nil, clierror.New(clierror.CodeServerError, "cannot merge pages: included is not an array", "")
			}
			included = append(included, inc...)
		}
	}
	out := map[string]any{}
	for k, v := range first {
		out[k] = v
	}
	out["data"] = data
	if included != nil {
		out["included"] = included
	}
	var links map[string]json.RawMessage
	if err := json.Unmarshal(first["links"], &links); err == nil {
		delete(links, "next")
		out["links"] = links
	}
	return json.Marshal(out)
}
