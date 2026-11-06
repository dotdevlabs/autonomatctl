// Command check-release validates a tagged CLI candidate before publication.
package main

import (
	"encoding/json"
	"os"

	"github.com/dotdevlabs/autonomatctl/internal/releasecheck"
	"github.com/dotdevlabs/autonomatctl/internal/schema"
)

func run(args []string) int {
	r := releasecheck.Result{Error: "usage: check-release vTAG BINARY"}
	if len(args) == 2 {
		r = releasecheck.Check(args[0], args[1], schema.Raw())
	}
	if err := json.NewEncoder(os.Stdout).Encode(r); err != nil {
		return 1
	}
	if r.Error != "" {
		return 1
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:])) }
