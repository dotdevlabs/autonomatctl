package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/dotdevlabs/autonomatctl/internal/releaseversion"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"error": err.Error()})
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("releaseversion", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	base := flags.String("base", "origin/main", "prior main snapshot (main push: HEAD^1)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	op := "check"
	if flags.NArg() > 1 {
		return fmt.Errorf("expected one operation: set, check or current")
	}
	if flags.NArg() == 1 {
		op = flags.Arg(0)
	}
	entry, err := releaseversion.Run(op, *base)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(entry)
}
