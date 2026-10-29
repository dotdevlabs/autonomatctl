package schema

import (
	"fmt"
	"net/http"
	"net/http/httptest"
)

// newGitHubStub answers the two GitHub API calls sync_spec.sh makes. It reads
// the spec and SHA through pointers so tests can change what is "live".
func newGitHubStub(spec, sha *string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t0ken" {
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/repos/dotdevlabs/autonomat/contents/docs/api_spec.yaml":
			_, _ = fmt.Fprint(w, *spec)
		case "/repos/dotdevlabs/autonomat/commits":
			if r.URL.Query().Get("path") != "docs/api_spec.yaml" {
				http.NotFound(w, r)
				return
			}
			if *sha == "" {
				_, _ = fmt.Fprint(w, `[]`)
				return
			}
			_, _ = fmt.Fprintf(w, "[\n  {\n    \"sha\": %q,\n    \"commit\": {\"tree\": {\"sha\": %q}}\n  }\n]\n", *sha, "0123456789012345678901234567890123456789")
		default:
			http.NotFound(w, r)
		}
	}))
}
