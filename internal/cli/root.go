// Package cli composes the ovm command-line interface.
package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/internal/buildinfo"
	"github.com/spf13/cobra"
)

// Dependencies carries the injectable application seams and environment lookup
// used by the command tree. The zero value is not usable; use Default or
// construct it explicitly in tests.
type Dependencies struct {
	// Discovery performs discovery rounds. Required for the discover command.
	Discovery app.DiscoveryUseCase
	// Inventory retrieves device inventories. Required for the info command.
	Inventory app.InventoryUseCase
	// MediaProfiles retrieves normalized profile data. Required for profiles.
	MediaProfiles app.MediaProfilesUseCase
	// Snapshot retrieves one JPEG atomically. Required for snapshot.
	Snapshot app.SnapshotUseCase
	// Probe measures a stream for a fixed duration. Required for probe.
	Probe app.ProbeUseCase
	// Diagnostics backs the clock, events, and report commands.
	Diagnostics app.HostDiagnostics
	// LookupEnv resolves environment variables for credential input. A nil
	// value defaults to os.LookupEnv.
	LookupEnv func(name string) (string, bool)
}

// Default returns the production dependencies backed by the real pkg/onvif
// adapters and the process environment.
func Default() Dependencies {
	return Dependencies{
		Discovery:     app.NewProductionDiscovery(),
		Inventory:     app.NewProductionInventory(),
		MediaProfiles: app.NewProductionMediaProfiles(),
		Snapshot:      app.NewProductionSnapshot(),
		Probe:         app.NewProductionProbe(),
		Diagnostics:   app.NewProductionHostDiagnostics(),
		LookupEnv:     nil,
	}
}

// NewRootCommand creates an isolated command tree suitable for execution and
// tests. The optional rawArgs value enables order-independent JSON failure
// envelopes when normal flag parsing stops early; process entry points should
// pass their exact argument slice.
func NewRootCommand(info buildinfo.Info, deps Dependencies, rawArgs ...[]string) *cobra.Command {
	var arguments []string
	if len(rawArgs) > 0 {
		arguments = rawArgs[0]
	}
	root := &cobra.Command{
		Use:           "ovm",
		Short:         "Manage and diagnose ONVIF-compatible cameras",
		SilenceErrors: true,
		SilenceUsage:  true,
		// An unknown command is a usage error. Cobra resolves an unknown
		// subcommand to the root command with the leftover args and then runs
		// the root's Args validator, so tagging the rejection here maps it to
		// the usage exit code (2) instead of the generic code.
		Args: rootUsageArgs,
		// A bare invocation (no subcommand) is also a usage error rather than a
		// silent success.
		RunE: func(cmd *cobra.Command, _ []string) error {
			return markUsageError(fmt.Errorf("a command is required; run \"ovm --help\" for usage"))
		},
	}

	// A flag-parsing failure is a usage error. Tagging it here (and inheriting
	// to subcommands) lets ExitCodeForError map it to the usage exit code.
	root.SetFlagErrorFunc(func(_ *cobra.Command, _ error) error {
		return markUsageError(fmt.Errorf("the command flags are not valid"))
	})

	root.SetOut(io.Discard)
	root.AddCommand(newVersionCommand(info))
	root.AddCommand(newDiscoverCommand(deps.Discovery, detectJSONIntent(arguments, "discover", "--iface", "--timeout")))
	root.AddCommand(newInfoCommand(deps.Inventory, deps.LookupEnv, detectJSONIntent(arguments, "info", "--user", "-u", "--password-env", "--timeout")))
	root.AddCommand(newProfilesCommand(deps.MediaProfiles, deps.LookupEnv, detectJSONIntent(arguments, "profiles", "--user", "-u", "--password-env", "--timeout")))
	root.AddCommand(newProbeCommand(deps.Probe, deps.LookupEnv, detectJSONIntent(arguments, "probe", "--user", "-u", "--password-env", "--profile", "--transport", "--duration", "--timeout")))
	root.AddCommand(newSnapshotCommand(deps.Snapshot, deps.LookupEnv, detectJSONIntent(arguments, "snapshot", "--user", "-u", "--password-env", "--profile", "--output", "-o", "--timeout")))
	credentialFlags := []string{"--user", "-u", "--password-env", "--timeout"}
	root.AddCommand(newClockCommand(deps.Diagnostics, deps.LookupEnv, detectJSONIntent(arguments, "clock", append(credentialFlags, "--profile", "--rtcp-wait")...)))
	root.AddCommand(newStabilityCommand(deps.Probe, deps.LookupEnv, detectJSONIntent(arguments, "stability", append(credentialFlags, "--profile", "--transport", "--cycles", "--duration", "--pause")...)))
	root.AddCommand(newEventsCommand(deps.Diagnostics, deps.LookupEnv, detectJSONIntent(arguments, "events", append(credentialFlags, "--filter", "--duration")...)))
	root.AddCommand(newReportCommand(deps.Diagnostics, info, deps.LookupEnv, detectJSONIntent(arguments, "report", append(credentialFlags, "--format")...)))
	return root
}

// rootUsageArgs rejects an unknown subcommand on the root command, tagging the
// failure as a usage error. It mirrors Cobra's default subcommand validity
// check for a root command that has subcommands, but routes the error through
// the usage classification.
//
// The rejected token is deliberately NOT echoed back verbatim. A user who
// mistypes a command — or who accidentally passes a credential-bearing value as
// the first positional argument — must not have that raw token reflected into
// an error string that may reach a terminal, a log, or a CI transcript. The
// message names only that an unknown command was supplied and how to get help.
func rootUsageArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return markUsageError(fmt.Errorf("unknown command; run %q for usage", cmd.CommandPath()+" --help"))
	}
	return nil
}

func newVersionCommand(info buildinfo.Info) *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print build version information",
		Args:  usageArgs(cobra.NoArgs),
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
