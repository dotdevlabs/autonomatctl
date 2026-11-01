package apiclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dotdevlabs/ctlkit/pkg/clierror"
)

func codeOf(err error) clierror.ErrorCode {
	var ce *clierror.CLIError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return -1
}

func newClient(t *testing.T, base string, mod func(*Options)) *Client {
	t.Helper()
	o := Options{BaseURL: base, Token: "secret-token", Backoff: -1}
	if mod != nil {
		mod(&o)
	}
	c, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestValidateBaseURL(t *testing.T) {
	if got, err := ValidateBaseURL("https://a.example"); err != nil || got != "https://a.example" {
		t.Errorf("valid: %q %v", got, err)
	}
	if _, err := ValidateBaseURL(""); codeOf(err) != clierror.CodeUsage {
		t.Errorf("empty: %v", err)
	}
	for _, bad := range []string{"ftp://x", "/relative", "http://", "::bad"} {
		if _, err := ValidateBaseURL(bad); codeOf(err) != clierror.CodeUsage {
			t.Errorf("%q accepted: %v", bad, err)
		}
	}
	if _, err := New(Options{BaseURL: "nope"}); codeOf(err) != clierror.CodeUsage {
		t.Errorf("New accepted a bad base: %v", err)
	}
}

func TestURLAndHeaders(t *testing.T) {
	c := newClient(t, "https://h.example/api/", nil)
	r := Request{Method: "GET", Path: "/work_items/a%2Fb", Query: url.Values{"q": {"x y"}, "a": {"1"}}}
	if got, want := c.URL(r), "https://h.example/api/work_items/a%2Fb?a=1&q=x+y"; got != want {
		t.Errorf("URL = %s, want %s", got, want)
	}
	h := c.Headers(Request{Body: []byte("{}")})
	if h["Authorization"] != "Bearer <redacted>" || h["Content-Type"] != MediaType || h["Accept"] != MediaType || h["User-Agent"] != "autonomatctl" {
		t.Errorf("headers = %v", h)
	}
	if _, ok := c.Headers(Request{})["Content-Type"]; ok {
		t.Error("content type without body")
	}
	if h := c.Headers(Request{Body: []byte("{}"), ContentType: "application/json"}); h["Content-Type"] != "application/json" {
		t.Errorf("explicit content type lost: %v", h)
	}
}

func TestDoSendsHeadersAndBody(t *testing.T) {
	var gotAuth, gotCT, gotAccept, gotUA, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotCT, gotAccept, gotUA = r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.Header.Get("Accept"), r.Header.Get("User-Agent")
		var b bytes.Buffer
		_, _ = b.ReadFrom(r.Body)
		gotBody = b.String()
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()
	c := newClient(t, srv.URL, func(o *Options) { o.UserAgent = "ua/1" })
	resp, err := c.Do(context.Background(), Request{Method: "POST", Path: "/x", Body: []byte(`{"a":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 201 || string(resp.Body) != `{"data":{}}` {
		t.Errorf("resp = %+v", resp)
	}
	if gotAuth != "Bearer secret-token" || gotCT != MediaType || gotAccept != MediaType || gotUA != "ua/1" || gotBody != `{"a":1}` {
		t.Errorf("sent %q %q %q %q %q", gotAuth, gotCT, gotAccept, gotUA, gotBody)
	}
}

func TestNoTokenIsAnErrorBeforeAnyRequest(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { atomic.AddInt32(&hits, 1) }))
	defer srv.Close()
	c := newClient(t, srv.URL, func(o *Options) { o.Token = "" })
	if _, err := c.Do(context.Background(), Request{Method: "GET", Path: "/"}); codeOf(err) != clierror.CodeUnauthorized {
		t.Errorf("err = %v", err)
	}
	if hits != 0 {
		t.Error("request sent without token")
	}
}

func TestRetry(t *testing.T) {
	cases := []struct {
		name     string
		method   string
		statuses []int
		want     int // final status
		calls    int
	}{
		{"GET 503 then ok", "GET", []int{503, 200}, 200, 2},
		{"GET 502 504 then ok", "GET", []int{502, 504, 200}, 200, 3},
		{"GET exhausts attempts", "GET", []int{503, 503, 503, 200}, 503, 3},
		{"POST 503 is not retried", "POST", []int{503, 200}, 503, 1},
		{"POST 429 is retried", "POST", []int{429, 200}, 200, 2},
		{"GET 500 is not retried", "GET", []int{500, 200}, 500, 1},
		{"GET 404 is not retried", "GET", []int{404, 200}, 404, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls int32
			var bodies []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := int(atomic.AddInt32(&calls, 1))
				var b bytes.Buffer
				_, _ = b.ReadFrom(r.Body)
				bodies = append(bodies, b.String())
				w.WriteHeader(tc.statuses[n-1])
			}))
			defer srv.Close()
			var verbose bytes.Buffer
			c := newClient(t, srv.URL, func(o *Options) { o.Verbose = &verbose })
			resp, err := c.Do(context.Background(), Request{Method: tc.method, Path: "/x", Body: []byte(`{"k":1}`)})
			if err != nil {
				t.Fatal(err)
			}
			if resp.Status != tc.want || int(calls) != tc.calls {
				t.Errorf("status %d after %d calls, want %d after %d", resp.Status, calls, tc.want, tc.calls)
			}
			for _, b := range bodies {
				if b != `{"k":1}` {
					t.Errorf("retried body = %q", b)
				}
			}
			if strings.Count(verbose.String(), "\n") != tc.calls || strings.Contains(verbose.String(), "secret-token") {
				t.Errorf("verbose = %q", verbose.String())
			}
		})
	}
}

func TestRetryAfterAndCancel(t *testing.T) {
	c := newClient(t, "http://x.example", func(o *Options) { o.Backoff = 10 * time.Millisecond })
	if d := c.delay(http.Header{"Retry-After": {"2"}}, 1); d != 2*time.Second {
		t.Errorf("Retry-After = %v", d)
	}
	if d := c.delay(http.Header{"Retry-After": {"9999"}}, 1); d != maxRetryAfter {
		t.Errorf("Retry-After cap = %v", d)
	}
	if d := c.delay(http.Header{"Retry-After": {"soon"}}, 3); d != 30*time.Millisecond {
		t.Errorf("fallback backoff = %v", d)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(503)
	}))
	defer srv.Close()
	c = newClient(t, srv.URL, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Do(ctx, Request{Method: "GET", Path: "/"}); codeOf(err) != clierror.CodeServerError {
		t.Errorf("cancel during retry wait: %v", err)
	}
	if err := sleep(context.Background(), 0); err != nil {
		t.Errorf("sleep(0) = %v", err)
	}
}

func TestTransportError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()
	c := newClient(t, base, nil)
	_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/x", Query: url.Values{"q": {"sekrit"}}})
	if codeOf(err) != clierror.CodeServerError {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "sekrit") {
		t.Errorf("error leaks request data: %v", err)
	}
	if got := redactErr(fmt.Errorf("plain")); got != "plain" {
		t.Errorf("redactErr = %q", got)
	}
	if _, err := c.Do(context.Background(), Request{Method: "BAD METHOD", Path: "/"}); codeOf(err) != clierror.CodeUsage {
		t.Errorf("bad method: %v", err)
	}
}

func TestBodyReadFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("short"))
	}))
	defer srv.Close()
	c := newClient(t, srv.URL, nil)
	if _, err := c.Do(context.Background(), Request{Method: "GET", Path: "/"}); codeOf(err) != clierror.CodeServerError {
		t.Errorf("truncated body: %v", err)
	}
}

// pager serves numbered pages; link builds the next link for page n.
type pager struct {
	srv  *httptest.Server
	uris []string
}

func newPager(t *testing.T, pages int, link func(n int, base string) string) *pager {
	p := &pager{}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.uris = append(p.uris, r.RequestURI)
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			w.WriteHeader(401)
			return
		}
		n := 1
		fmt.Sscanf(r.URL.Query().Get("page[number]"), "%d", &n)
		next := ""
		if n < pages {
			next = fmt.Sprintf(`,"next":%s`, link(n+1, p.srv.URL))
		}
		_, _ = fmt.Fprintf(w, `{"data":[{"id":"%d"}],"links":{"self":"s"%s},"meta":{"total":%d}}`, n, next, pages)
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func TestFetchAllFollowsLinksVerbatim(t *testing.T) {
	// page 2 is relative, page 3 absolute; the encoded brackets and an odd
	// extra parameter must reach the server exactly as the server wrote them.
	p := newPager(t, 3, func(n int, base string) string {
		if n == 2 {
			return `"/work_items?page%5Bnumber%5D=2&page%5Bsize%5D=1&zzz=%C3%A9"`
		}
		return fmt.Sprintf(`"%s/work_items?page%%5Bnumber%%5D=3&page%%5Bsize%%5D=1&zzz=%%C3%%A9"`, base)
	})
	c := newClient(t, p.srv.URL, nil)
	resp, err := c.FetchAll(context.Background(), Request{Method: "GET", Path: "/work_items", Query: url.Values{"q": {"status:open"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/work_items?q=status%3Aopen",
		"/work_items?page%5Bnumber%5D=2&page%5Bsize%5D=1&zzz=%C3%A9",
		"/work_items?page%5Bnumber%5D=3&page%5Bsize%5D=1&zzz=%C3%A9",
	}
	if strings.Join(p.uris, "\n") != strings.Join(want, "\n") {
		t.Errorf("requested URIs:\n%s\nwant:\n%s", strings.Join(p.uris, "\n"), strings.Join(want, "\n"))
	}
	if string(resp.Body) != `{"data":[{"id":"1"},{"id":"2"},{"id":"3"}],"links":{"self":"s"},"meta":{"total":3}}` {
		t.Errorf("merged = %s", resp.Body)
	}
}

func TestFetchAllSinglePageIsRaw(t *testing.T) {
	raw := "{ \"data\" : [ ] ,\n \"links\": {} }\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(raw)) }))
	defer srv.Close()
	resp, err := newClient(t, srv.URL, nil).FetchAll(context.Background(), Request{Method: "GET", Path: "/x"})
	if err != nil || string(resp.Body) != raw {
		t.Errorf("body = %q, err = %v", resp.Body, err)
	}
}

func TestFetchAllGuards(t *testing.T) {
	t.Run("cross-origin next is refused and the token is not sent", func(t *testing.T) {
		var evil int32
		other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&evil, 1) }))
		defer other.Close()
		p := newPager(t, 2, func(int, string) string { return `"` + other.URL + `/steal"` })
		_, err := newClient(t, p.srv.URL, nil).FetchAll(context.Background(), Request{Method: "GET", Path: "/x"})
		if codeOf(err) != clierror.CodeServerError || !strings.Contains(err.Error(), "another origin") {
			t.Errorf("err = %v", err)
		}
		if evil != 0 {
			t.Error("request reached the other origin")
		}
	})
	t.Run("repeated next is a loop", func(t *testing.T) {
		p := newPager(t, 99, func(int, string) string { return `"/x?page%5Bnumber%5D=2"` })
		_, err := newClient(t, p.srv.URL, nil).FetchAll(context.Background(), Request{Method: "GET", Path: "/x"})
		// page 2 links to itself via n+1=3 -> same fixed URL
		if err == nil || !strings.Contains(err.Error(), "repeats") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("page cap", func(t *testing.T) {
		p := newPager(t, 99, func(n int, _ string) string { return fmt.Sprintf(`"/x?page%%5Bnumber%%5D=%d"`, n) })
		_, err := newClient(t, p.srv.URL, func(o *Options) { o.MaxPages = 3 }).FetchAll(context.Background(), Request{Method: "GET", Path: "/x"})
		if err == nil || !strings.Contains(err.Error(), "stopped after 3 pages") {
			t.Errorf("err = %v", err)
		}
		if len(p.uris) != 3 {
			t.Errorf("fetched %d pages", len(p.uris))
		}
	})
	t.Run("invalid next", func(t *testing.T) {
		p := newPager(t, 2, func(int, string) string { return `"http://[::bad"` })
		_, err := newClient(t, p.srv.URL, nil).FetchAll(context.Background(), Request{Method: "GET", Path: "/x"})
		if err == nil || !strings.Contains(err.Error(), "invalid links.next") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("error on a later page is returned as is", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("p") == "2" {
				w.WriteHeader(503)
				_, _ = w.Write([]byte(`{"errors":[{"title":"down"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[],"links":{"next":"/x?p=2"}}`))
		}))
		defer srv.Close()
		resp, err := newClient(t, srv.URL, func(o *Options) { o.MaxAttempts = 1 }).FetchAll(context.Background(), Request{Method: "GET", Path: "/x"})
		if err != nil || resp.Status != 503 || !strings.Contains(string(resp.Body), "down") {
			t.Errorf("resp = %+v, err = %v", resp, err)
		}
	})
	t.Run("error on the first page", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
		defer srv.Close()
		resp, err := newClient(t, srv.URL, nil).FetchAll(context.Background(), Request{Method: "GET", Path: "/x"})
		if err != nil || resp.Status != 404 {
			t.Errorf("resp = %+v, err = %v", resp, err)
		}
	})
	t.Run("not a JSON document", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`<html>`)) }))
		defer srv.Close()
		if _, err := newClient(t, srv.URL, nil).FetchAll(context.Background(), Request{Method: "GET", Path: "/x"}); codeOf(err) != clierror.CodeServerError {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("transport error on a later page", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("p") == "2" {
				panic(http.ErrAbortHandler)
			}
			_, _ = w.Write([]byte(`{"data":[],"links":{"next":"/x?p=2"}}`))
		}))
		defer srv.Close()
		_, err := newClient(t, srv.URL, nil).FetchAll(context.Background(), Request{Method: "GET", Path: "/x"})
		if codeOf(err) != clierror.CodeServerError {
			t.Errorf("err = %v", err)
		}
	})
}

func TestNextLink(t *testing.T) {
	cases := map[string]string{
		`{"links":{"next":"/a"}}`:                "/a",
		`{"links":{"next":{"href":"/b"}}}`:       "/b",
		`{"links":{"next":null}}`:                "",
		`{"links":{"next":42}}`:                  "",
		`{"links":{"self":"/s"}}`:                "",
		`{"data":[]}`:                            "",
		`{"links":{"next":{"href":"/c","x":1}}}`: "/c",
	}
	for doc, want := range cases {
		if got, err := NextLink([]byte(doc)); err != nil || got != want {
			t.Errorf("NextLink(%s) = %q, %v; want %q", doc, got, err, want)
		}
	}
	if _, err := NextLink([]byte(`[1]`)); err == nil {
		t.Error("array document accepted")
	}
}

func TestMerge(t *testing.T) {
	page := func(s string) *Response { return &Response{Body: []byte(s)} }
	got, err := Merge([]*Response{
		page(`{"data":[{"id":"1"}],"included":[{"id":"i1"}],"links":{"self":"s","next":"n"},"meta":{"a":1},"jsonapi":{"version":"1.0"}}`),
		page(`{"data":[{"id":"2"}],"included":[{"id":"i2"}],"links":{"self":"s2"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"data":[{"id":"1"},{"id":"2"}],"included":[{"id":"i1"},{"id":"i2"}],"jsonapi":{"version":"1.0"},"links":{"self":"s"},"meta":{"a":1}}`
	if string(got) != want {
		t.Errorf("merged = %s\nwant     %s", got, want)
	}
	// no links member on the first page: none invented
	got, err = Merge([]*Response{page(`{"data":[]}`), page(`{"data":[{"id":"x"}]}`)})
	if err != nil || string(got) != `{"data":[{"id":"x"}]}` {
		t.Errorf("no links: %s %v", got, err)
	}
	for name, pages := range map[string][]*Response{
		"not json":         {page(`x`), page(`{}`)},
		"data not array":   {page(`{"data":{}}`), page(`{"data":[]}`)},
		"data missing":     {page(`{}`), page(`{}`)},
		"included invalid": {page(`{"data":[],"included":{}}`), page(`{"data":[]}`)},
	} {
		if _, err := Merge(pages); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
