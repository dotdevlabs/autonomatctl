package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dotdevlabs/ctlkit/pkg/airef"
	"github.com/dotdevlabs/ctlkit/pkg/output"

	"github.com/dotdevlabs/autonomatctl/internal/apiclient"
	"github.com/dotdevlabs/autonomatctl/internal/gen"
	"github.com/dotdevlabs/autonomatctl/internal/schema"
)

// reference is the structured command reference printed by `ai --json`. It is
// derived from the live command tree, so it cannot drift from the binary, and
// carries no timestamps: two runs print identical bytes.
type reference struct {
	Product     string           `json:"product"`
	Version     string           `json:"version"`
	Contract    contractRef      `json:"contract"`
	GlobalFlags []flagRef        `json:"global_flags"`
	Environment []envRef         `json:"environment"`
	ExitCodes   []exitRef        `json:"exit_codes"`
	Commands    []commandRef     `json:"commands"`
	Workflows   []airef.Workflow `json:"workflows"`
}

type contractRef struct {
	Version string `json:"version"`
	SHA     string `json:"sha"`
}

type envRef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type exitRef struct {
	Code    int    `json:"code"`
	Meaning string `json:"meaning"`
}

type flagRef struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Required    bool     `json:"required,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Description string   `json:"description"`
}

type commandRef struct {
	Command     string    `json:"command"`
	Usage       string    `json:"usage"`
	Summary     string    `json:"summary"`
	Method      string    `json:"method,omitempty"`
	Path        string    `json:"path,omitempty"`
	OperationID string    `json:"operation_id,omitempty"`
	Webhook     bool      `json:"webhook,omitempty"`
	Flags       []flagRef `json:"flags,omitempty"`
}

var environment = []envRef{
	{"AUTONOMATCTL_TOKEN", "Bearer token; overrides the token of the active context. Agent runs pass their run-scoped token here."},
	{"AUTONOMATCTL_CONTEXT", "Name of the context to use when --context is not given."},
	{apiclient.BaseURLEnv, "Base URL; overrides the base URL of the active context. With AUTONOMATCTL_TOKEN also set, no context is needed."},
}

var exitCodes = []exitRef{
	{0, "success"},
	{1, "not found (HTTP 404, unknown context)"},
	{2, "unauthorized (HTTP 401, or no token configured)"},
	{3, "forbidden (HTTP 403)"},
	{4, "request rejected (other HTTP 4xx, e.g. 422 validation errors)"},
	{5, "conflict (HTTP 409)"},
	{6, "server or transport failure (HTTP 5xx, network error)"},
	{8, "usage error (unknown flag or command, missing required flag, bad value)"},
}

func buildReference(root *cobra.Command, ver string, wfs []airef.Workflow) reference {
	ref := reference{
		Product:     "autonomatctl",
		Version:     ver,
		Contract:    contractRef{Version: schema.Version(), SHA: schema.SHA()},
		GlobalFlags: flagRefs(root.PersistentFlags()),
		Environment: environment,
		ExitCodes:   exitCodes,
		Workflows:   wfs,
	}
	walk(root, &ref.Commands)
	sort.SliceStable(ref.Commands, func(i, j int) bool { return ref.Commands[i].Command < ref.Commands[j].Command })
	return ref
}

func walk(c *cobra.Command, out *[]commandRef) {
	for _, sub := range c.Commands() {
		if sub.Name() == "help" || sub.Name() == "completion" {
			continue
		}
		if sub.Runnable() && !sub.HasSubCommands() {
			*out = append(*out, commandRefOf(sub))
		}
		walk(sub, out)
	}
}

func commandRefOf(c *cobra.Command) commandRef {
	path := strings.TrimPrefix(c.CommandPath(), c.Root().Name()+" ")
	return commandRef{
		Command:     path,
		Usage:       c.UseLine(),
		Summary:     c.Short,
		Method:      c.Annotations[gen.AnnMethod],
		Path:        c.Annotations[gen.AnnPath],
		OperationID: c.Annotations[gen.AnnOperationID],
		Webhook:     c.Annotations[gen.AnnWebhook] == "true",
		Flags:       flagRefs(c.LocalNonPersistentFlags()),
	}
}

func flagRefs(fs *pflag.FlagSet) []flagRef {
	var refs []flagRef
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" {
			return
		}
		refs = append(refs, flagRef{
			Name:        f.Name,
			Type:        f.Value.Type(),
			Required:    len(f.Annotations[gen.FlagRequired]) > 0,
			Enum:        f.Annotations[gen.FlagEnum],
			Description: f.Usage,
		})
	})
	return refs
}

// newAICmd replaces ctlkit's ai command: stable output, markdown by default,
// JSON with --json, built from the command tree.
func newAICmd(ver string, wfs []airef.Workflow) *cobra.Command {
	return &cobra.Command{
		Use:   "ai",
		Short: "Print the command reference for AI agents (markdown; --json for structured)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ref := buildReference(cmd.Root(), ver, wfs)
			if j, _ := cmd.Flags().GetBool("json"); j {
				return output.JSONTo(cmd.OutOrStdout(), ref)
			}
			return writeMarkdown(cmd.OutOrStdout(), ref)
		},
	}
}

type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) f(format string, a ...any) {
	if e.err == nil {
		_, e.err = fmt.Fprintf(e.w, format, a...)
	}
}

func writeMarkdown(w io.Writer, ref reference) error {
	e := &errWriter{w: w}
	e.f("# %s command reference\n\n", ref.Product)
	e.f("Version: %s  \nAPI contract: version %s, source commit `%s`\n\n", ref.Version, ref.Contract.Version, ref.Contract.SHA)
	e.f("Every API operation is a `<resource> [<sub-resource>] <verb>` command generated from the contract; flags carry the contract's exact field names.\n\n")

	e.f("## Output\n\n")
	e.f("- `--json` prints the raw JSON:API document exactly as the server sent it. On failure it prints the server's `errors` body to stdout.\n")
	e.f("- Without `--json` the output is readable text; failures print the `errors` entries to stderr.\n")
	e.f("- `--all` on list commands follows `links.next` and merges the pages into one document.\n")
	e.f("- `--dry-run` prints the request and sends nothing.\n\n")

	e.f("## Global flags\n\n")
	flagTable(e, ref.GlobalFlags)
	e.f("## Environment\n\n")
	for _, v := range ref.Environment {
		e.f("- `%s`: %s\n", v.Name, v.Description)
	}
	e.f("\n## Exit codes\n\n")
	for _, x := range ref.ExitCodes {
		e.f("- %d: %s\n", x.Code, x.Meaning)
	}
	e.f("\n## Commands\n\n")
	for _, c := range ref.Commands {
		e.f("### %s\n\n%s\n\n", c.Command, c.Summary)
		if c.Method != "" {
			e.f("`%s %s` (operationId `%s`)", c.Method, c.Path, c.OperationID)
			if c.Webhook {
				e.f(" - webhook delivery, authenticates with the webhook token")
			}
			e.f("\n\n")
		}
		e.f("Usage: `%s`\n\n", c.Usage)
		flagTable(e, c.Flags)
	}
	e.f("## Common workflows\n\n")
	for _, wf := range ref.Workflows {
		e.f("### %s\n\n%s\n\n", wf.Name, wf.Description)
		for i, s := range wf.Steps {
			e.f("%d. `%s`\n", i+1, s)
		}
		e.f("\n")
	}
	return e.err
}

func flagTable(e *errWriter, flags []flagRef) {
	if len(flags) == 0 {
		return
	}
	e.f("| Flag | Type | Required | Description |\n|------|------|----------|-------------|\n")
	for _, f := range flags {
		req := ""
		if f.Required {
			req = "yes"
		}
		e.f("| --%s | %s | %s | %s |\n", f.Name, f.Type, req, strings.ReplaceAll(f.Description, "|", "\\|"))
	}
	e.f("\n")
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
