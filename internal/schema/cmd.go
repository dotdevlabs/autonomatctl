package schema

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dotdevlabs/ctlkit/pkg/output"
)

// Info is the machine-readable description of the pinned contract.
type Info struct {
	Title      string `json:"title"`
	Version    string `json:"version"`
	OpenAPI    string `json:"openapi"`
	SHA        string `json:"sha"`
	Source     string `json:"source"`
	Operations int    `json:"operations"`
}

// NewCmd returns the schema command. It reports the contract this binary was
// generated from; it makes no network call.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Print the pinned API contract version and source commit SHA",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := Spec()
			if err != nil {
				return err
			}
			info := Info{Title: s.Title, Version: s.Version, OpenAPI: s.OpenAPI, SHA: SHA(), Source: Source, Operations: len(s.Operations)}
			if j, _ := cmd.Flags().GetBool("json"); j {
				return output.JSONTo(cmd.OutOrStdout(), info)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\nversion: %s\nopenapi: %s\nsha: %s\nsource: %s\noperations: %d\n",
				info.Title, info.Version, info.OpenAPI, info.SHA, info.Source, info.Operations)
			return err
		},
	}
	return cmd
}
