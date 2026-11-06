package releasecheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompare(t *testing.T) {
	for _, tc := range []struct {
		tag, reported, contract string
		pass                    bool
	}{
		{"v0.0.1", "0.0.1", "0.0.1", true},
		{"v0.0.2", "0.0.1", "0.0.1", false},
		{"v0.0.1", "0.0.2", "0.0.1", false},
		{"v0.0.1", "0.0.1", "0.0.2", false},
		{"", "0.0.1", "0.0.1", false},
		{"0.0.1", "0.0.1", "0.0.1", false},
		{"v0.0.1", "0.0.1", "", false},
	} {
		r := Compare(tc.tag, tc.reported, tc.contract)
		if (r.Error == "") != tc.pass {
			t.Fatalf("%+v: %+v", tc, r)
		}
		if !tc.pass {
			for _, value := range []string{r.Tag, r.Reported, r.Contract} {
				if !strings.Contains(r.Error, value) {
					t.Errorf("missing %q: %s", value, r.Error)
				}
			}
		}
	}
}

func TestCheck(t *testing.T) {
	for _, tc := range []struct {
		script, raw string
		pass        bool
	}{
		{"echo '{\"version\":\"0.0.1\"}'", "info: {version: 0.0.1}", true},
		{"echo '{}'", "info: {version: 0.0.1}", false},
		{"echo invalid", "info: {version: 0.0.1}", false},
		{"exit 2", "info: {version: 0.0.1}", false},
		{"echo '{\"version\":\"0.0.1\"}'", "info: {}", false},
		{"exit 0", "[", false},
	} {
		binary := filepath.Join(t.TempDir(), "candidate")
		if err := os.WriteFile(binary, []byte("#!/bin/sh\n"+tc.script+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
		r := Check("v0.0.1", binary, []byte(tc.raw))
		if (r.Error == "") != tc.pass {
			t.Fatalf("%+v: %+v", tc, r)
		}
	}
}
