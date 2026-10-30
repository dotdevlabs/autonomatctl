// Command autonomatctl is the CLI for Autonomat. Its resource commands are
// generated from the pinned OpenAPI contract at startup.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dotdevlabs/ctlkit/pkg/airef"
	"github.com/dotdevlabs/ctlkit/pkg/clierror"
	"github.com/dotdevlabs/ctlkit/pkg/config"
	"github.com/dotdevlabs/ctlkit/pkg/ctxutil"
	"github.com/dotdevlabs/ctlkit/pkg/root"
	"github.com/dotdevlabs/ctlkit/pkg/version"

	"github.com/dotdevlabs/autonomatctl/internal/apiclient"
	"github.com/dotdevlabs/autonomatctl/internal/gen"
	"github.com/dotdevlabs/autonomatctl/internal/schema"
	"github.com/dotdevlabs/autonomatctl/internal/spec"
)

const product = "autonomatctl"

// builtins are the top-level names owned by ctlkit and this package.
var builtins = []string{"auth", "context", "version", "ai", "schema", "help", "completion"}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the CLI and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	cmd, err := newRoot()
	if err != nil {
		return clierror.HandleErr(err, stderr)
	}
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	if err := cmd.Execute(); err != nil {
		return clierror.HandleErr(normalize(err), stderr)
	}
	return 0
}

// normalize maps errors raised by cobra itself to the usage exit code.
func normalize(err error) error {
	if _, ok := err.(*clierror.CLIError); ok {
		return err
	}
	if strings.HasPrefix(err.Error(), "required flag(s)") {
		return clierror.New(clierror.CodeUsage, err.Error(), "see the command's --help")
	}
	if strings.HasPrefix(err.Error(), "unknown command") {
		return clierror.New(clierror.CodeUsage, err.Error(), "run 'autonomatctl ai' for the command reference")
	}
	return err
}

func newRoot() (*cobra.Command, error) {
	sp, err := schema.Spec()
	if err != nil {
		return nil, fmt.Errorf("pinned contract is invalid: %w", err)
	}
	return buildRoot(sp)
}

func buildRoot(sp *spec.Spec) (*cobra.Command, error) {
	ver := version.Current(product)
	cmds, err := gen.Build(sp, gen.Config{Reserved: builtins, Version: ver.Version})
	if err != nil {
		return nil, fmt.Errorf("cannot generate commands from the contract: %w", err)
	}
	cmd := root.New(root.BuildConfig{
		Product:   product,
		Short:     "CLI for Autonomat, generated from its OpenAPI contract",
		Version:   ver,
		Commands:  append(cmds, schema.NewCmd()),
		Workflows: workflows(),
	})
	if f := cmd.PersistentFlags().Lookup("json"); f != nil {
		f.Usage = "Print the raw JSON:API response document (errors body on failure)"
	}
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return clierror.New(clierror.CodeUsage, err.Error(), "see '"+c.CommandPath()+" --help'")
	})

	prev := cmd.PersistentPreRunE
	cmd.PersistentPreRunE = func(c *cobra.Command, args []string) error {
		if offline(c) {
			return nil
		}
		err := prev(c, args)
		if err == nil {
			return nil
		}
		// An agent run needs no stored context: base URL and token from the
		// environment are enough.
		ce, ok := err.(*clierror.CLIError)
		base, tok := os.Getenv(apiclient.BaseURLEnv), os.Getenv("AUTONOMATCTL_TOKEN")
		if !ok || ce.Code != clierror.CodeUsage || base == "" || tok == "" {
			return err
		}
		c.SetContext(envContext(c, base, tok))
		return nil
	}

	return cmd, nil
}

// offline reports commands that need no API context: groups only print help,
// schema and ai read the embedded contract.
func offline(c *cobra.Command) bool {
	if c.HasSubCommands() {
		return true
	}
	for p := c; p != nil; p = p.Parent() {
		switch p.Name() {
		case "schema", "help", "completion", "__complete":
			return true
		}
	}
	return config.SkipContextResolution(c)
}

func envContext(c *cobra.Command, base, tok string) (ctx context.Context) {
	f := c.Flags()
	gf := ctxutil.GlobalFlags{}
	gf.Context, _ = f.GetString("context")
	gf.JSON, _ = f.GetBool("json")
	gf.Format, _ = f.GetString("format")
	gf.DryRun, _ = f.GetBool("dry-run")
	gf.Verbose, _ = f.GetBool("verbose")
	ctx = ctxutil.WithGlobalFlags(c.Context(), gf)
	return ctxutil.WithActiveContext(ctx, &config.Context{BaseURL: base, Token: tok})
}

func workflows() []airef.Workflow {
	return []airef.Workflow{
		{
			Name:        "Triage work items",
			Description: "List open work items as JSON, then read one in full.",
			Steps: []string{
				`autonomatctl work-items list --q 'status:open' --json`,
				`autonomatctl work-items get <id> --json`,
			},
		},
		{
			Name:        "Create a work item and start an execution",
			Description: "Create a work item, then create an execution for it. The work item id is in data.id of the first response.",
			Steps: []string{
				`autonomatctl work-items create --item_type report --title "Quarterly review" --source_kind human --status open --json`,
				`autonomatctl work-items executions create <work_item_id> --state pending --json`,
			},
		},
		{
			Name:        "Read every page of a collection",
			Description: "List commands return one page; --all follows links.next (the CLI never builds page URLs) and merges the pages into one document.",
			Steps: []string{
				`autonomatctl work-items list --all --json`,
			},
		},
		{
			Name:        "Preview a write",
			Description: "--dry-run prints the method, URL and body that would be sent, with the token redacted, and sends nothing.",
			Steps: []string{
				`autonomatctl work-items create --item_type report --title "Draft" --source_kind human --status open --dry-run`,
			},
		},
		{
			Name:        "Run as an agent with a run-scoped token",
			Description: "No stored context is needed when the base URL and token come from the environment.",
			Steps: []string{
				`AUTONOMATCTL_BASE_URL=https://autonomat.example AUTONOMATCTL_TOKEN=<run-token> autonomatctl status get --json`,
			},
		},
		{
			Name:        "Check which contract this binary speaks",
			Description: "Prints the pinned API contract version and the commit it was synced from.",
			Steps: []string{
				`autonomatctl schema --json`,
			},
		},
	}
}
