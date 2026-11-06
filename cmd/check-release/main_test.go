package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dotdevlabs/autonomatctl/internal/schema"
)

func TestRun(t *testing.T) {
	if run(nil) != 1 {
		t.Fatal("missing arguments passed")
	}
	binary := filepath.Join(t.TempDir(), "candidate")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho '{\"version\":\""+schema.Version()+"\"}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if run([]string{"v" + schema.Version(), binary}) != 0 {
		t.Fatal("matching release failed")
	}
	if run([]string{"v999.0.0", binary}) != 1 {
		t.Fatal("mismatch passed")
	}
}
