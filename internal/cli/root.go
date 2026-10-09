// Package cli composes the ovm command-line interface.
package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/addodelgrossi/ovm/internal/buildinfo"
	"github.com/spf13/cobra"
)

// NewRootCommand creates an isolated command tree suitable for execution and tests.
func NewRootCommand(info buildinfo.Info) *cobra.Command {
	root := &cobra.Command{
		Use:           "ovm",
		Short:         "Manage and diagnose ONVIF-compatible cameras",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	root.SetOut(io.Discard)
	root.AddCommand(newVersionCommand(info))
	return root
}

func newVersionCommand(info buildinfo.Info) *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print build version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if outputJSON {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetEscapeHTML(false)
				return encoder.Encode(info)
			}

			_, err := fmt.Fprintf(
				cmd.OutOrStdout(),
				"ovm %s (commit %s, built %s)\n",
				info.Version,
				info.Commit,
				info.Date,
			)
			return err
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "write machine-readable JSON")
	return cmd
}
