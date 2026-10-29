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
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dotdevlabs/ctlkit/pkg/clierror"
)

// BaseURLEnv overrides the base URL of the active context.
const BaseURLEnv = "AUTONOMATCTL_BASE_URL"

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
	Method      string
	Path        string
	Query       url.Values
	Body        []byte
	ContentType string
}

// Response is a raw HTTP response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// ResolveBaseURL applies the AUTONOMATCTL_BASE_URL override to the base URL of
// the active context and validates the result.
func ResolveBaseURL(configured string) (string, error) {
	base := configured
	if env := os.Getenv(BaseURLEnv); env != "" {
		base = env
	}
	if base == "" {
		return "", clierror.New(clierror.CodeUsage, "no base URL configured",
			"set one with 'auth login --name <ctx> --url <url>' or "+BaseURLEnv)
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
	u := strings.TrimRight(c.base.String(), "/") + r.Path
	if len(r.Query) > 0 {
		u += "?" + r.Query.Encode()
	}
	return u
}

// Headers returns the request headers that would be sent, with the token
// redacted. It backs --dry-run.
func (c *Client) Headers(r Request) map[string]string {
	h := map[string]string{"Accept": MediaType, "User-Agent": c.opts.UserAgent, "Authorization": "Bearer <redacted>"}
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
	return c.send(ctx, r.Method, c.URL(r), r.Body, contentType(r))
}

func (c *Client) send(ctx context.Context, method, target string, body []byte, ctype string) (*Response, error) {
	if c.opts.Token == "" {
		return nil, clierror.New(clierror.CodeUnauthorized, "no API token configured",
			"run 'auth login --name <ctx> --url <url> --token <token>' or set AUTONOMATCTL_TOKEN")
	}
	var lastStatus int
	for attempt := 1; ; attempt++ {
		var rdr io.Reader
		if len(body) > 0 {
			rdr = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, rdr)
		if err != nil {
			return nil, clierror.New(clierror.CodeUsage, "cannot build request: "+err.Error(), "")
		}
		req.Header.Set("Accept", MediaType)
		req.Header.Set("Authorization", "Bearer "+c.opts.Token)
		req.Header.Set("User-Agent", c.opts.UserAgent)
		if len(body) > 0 {
			req.Header.Set("Content-Type", ctype)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, clierror.New(clierror.CodeServerError, "request failed: "+redactErr(err), "")
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		_ = resp.Body.Close()
		if err != nil {
			return nil, clierror.New(clierror.CodeServerError, "reading response failed: "+err.Error(), "")
		}
		if c.opts.Verbose != nil {
			_, _ = fmt.Fprintf(c.opts.Verbose, "%s %s -> %d\n", method, target, resp.StatusCode)
		}
		lastStatus = resp.StatusCode
		out := &Response{Status: resp.StatusCode, Header: resp.Header, Body: data}
		if attempt >= c.opts.MaxAttempts || !retryable(method, lastStatus) {
			return out, nil
		}
		if err := sleep(ctx, c.delay(resp.Header, attempt)); err != nil {
			return nil, clierror.New(clierror.CodeServerError, "request cancelled: "+err.Error(), "")
		}
	}
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
	resp, err := c.send(ctx, http.MethodGet, cur, nil, "")
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
		resp, err = c.send(ctx, http.MethodGet, cur, nil, "")
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
