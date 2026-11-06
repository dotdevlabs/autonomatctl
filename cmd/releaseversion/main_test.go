package main

import "testing"

func TestInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"--version", "1.0.0"}, {"set", "extra"}, {"--base", "nonexistent", "check"}} {
		if err := run(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
