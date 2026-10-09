package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/addodelgrossi/ovm/internal/app"
	"github.com/spf13/cobra"
)

// defaultDiscoverTimeout bounds a discover round when the user supplies none.
const defaultDiscoverTimeout = 3 * time.Second

// newDiscoverCommand builds "ovm discover". The discovery use case is injected
// so tests can supply a deterministic seam with no multicast dependency.
func newDiscoverCommand(discovery app.DiscoveryUseCase, intent jsonIntent) *cobra.Command {
	var (
		iface      string
		timeout    time.Duration
		outputJSON bool
	)
	wantsJSON := func() bool { return intent.enabled(outputJSON) }

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover ONVIF cameras on the local network",
		Long: "Discover ONVIF cameras using interface-aware WS-Discovery.\n\n" +
			"Diagnostics for an empty or failed round are written to stderr in\n" +
			"both human and JSON modes.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				marked := markUsageError(err)
				if wantsJSON() {
					usageErr := app.NewError(app.CategoryUsage, "the discovery request is not valid")
					if writeErr := writeDiscoveryFailureJSON(cmd.OutOrStdout(), cmd.ErrOrStderr(), app.DiscoveryResult{Devices: []app.DiscoveredDevice{}}, usageErr); writeErr != nil {
						return writeErr
					}
				}
				return marked
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if timeout <= 0 {
				marked := markUsageError(fmt.Errorf("--timeout must be positive"))
				if wantsJSON() {
					usageErr := app.NewError(app.CategoryUsage, "the discovery timeout must be positive")
					if writeErr := writeDiscoveryFailureJSON(cmd.OutOrStdout(), cmd.ErrOrStderr(), app.DiscoveryResult{Devices: []app.DiscoveredDevice{}}, usageErr); writeErr != nil {
						return writeErr
					}
				}
				return marked
			}
			if discovery == nil {
				return app.NewInternalError("the discovery dependency is not configured")
			}
			request := app.DiscoveryRequest{InterfaceName: iface, Timeout: timeout}
			result, err := discovery.Discover(cmd.Context(), request)
			if err != nil {
				return presentDiscoveryFailure(cmd.OutOrStdout(), cmd.ErrOrStderr(), wantsJSON(), result, err)
			}
			if wantsJSON() {
				return writeDiscoveryJSON(cmd.OutOrStdout(), cmd.ErrOrStderr(), result)
			}
			return writeDiscoveryHuman(cmd.OutOrStdout(), cmd.ErrOrStderr(), result)
		},
	}

	cmd.Flags().StringVar(&iface, "iface", "", "network interface name to probe (default: all usable interfaces)")
	cmd.Flags().DurationVar(&timeout, "timeout", defaultDiscoverTimeout, "discovery round timeout")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "write machine-readable JSON to stdout")
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		marked := markUsageError(err)
		if wantsJSON() {
			usageErr := app.NewError(app.CategoryUsage, "the discovery flags are not valid")
			if writeErr := writeDiscoveryFailureJSON(cmd.OutOrStdout(), cmd.ErrOrStderr(), app.DiscoveryResult{Devices: []app.DiscoveredDevice{}}, usageErr); writeErr != nil {
				return writeErr
			}
		}
		return marked
	})
	return cmd
}

// presentDiscoveryFailure renders a nonzero discovery result and returns a
// presented error so the process entry point maps the exit code without
// printing a duplicate message.
//
// A cancelled or failed round can still carry partial devices aggregated before
// the context ended, or a diagnostic explaining an empty round. Both are
// surfaced before the error is returned:
//
//   - JSON mode always writes the versioned failure envelope, which embeds the
//     partial result (devices and/or diagnostic) alongside the credential-free
//     error object. Because the envelope renders the error, the returned error
//     is marked presented so it is not printed again.
//   - Human mode renders any partial devices to stdout exactly as a successful
//     round would, so a cancelled discovery still shows what it found, then
//     returns the unpresented error for the process entry point to print once.
//     When there are no devices, an available diagnostic is written to stderr
//     and the error is marked presented. When there is neither a device nor a
//     diagnostic, the raw error is returned.
func presentDiscoveryFailure(stdout, stderr io.Writer, wantsJSON bool, result app.DiscoveryResult, commandErr error) error {
	if wantsJSON {
		if werr := writeDiscoveryFailureJSON(stdout, stderr, result, commandErr); werr != nil {
			return werr
		}
		return markPresented(commandErr)
	}

	if len(result.Devices) > 0 {
		// Partial devices discovered before the failure are shown just like a
		// successful round. The error itself has not been rendered, so leave it
		// unmarked for the process entry point to print exactly once.
		if werr := writeDiscoveryHuman(stdout, stderr, result); werr != nil {
			return werr
		}
		return commandErr
	}

	if result.Diagnostic != nil {
		if werr := writeDiscoveryDiagnostic(stderr, result); werr != nil {
			return werr
		}
		return markPresented(commandErr)
	}

	return commandErr
}

// writeDiscoveryJSON writes the stable discovery envelope to stdout and any
// diagnostic to stderr. The diagnostic is duplicated to stderr even in JSON
// mode so a consumer piping stdout still sees why a round was empty.
//
// Every writer error is propagated. When both the stderr diagnostic write and
// the stdout envelope write fail, the stderr failure takes deterministic
// precedence because it is attempted first; this keeps behavior stable for a
// caller observing a broken pipe on either stream.
func writeDiscoveryJSON(stdout, stderr io.Writer, result app.DiscoveryResult) error {
	if result.Diagnostic != nil {
		if err := writeDiscoveryDiagnostic(stderr, result); err != nil {
			return err
		}
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(newDiscoveryEnvelope(result))
}

// writeDiscoveryFailureJSON preserves the same versioned envelope shape on a
// nonzero runtime result and includes a fixed, credential-free error object.
// The partial result (any devices aggregated before the failure, plus any
// diagnostic) is embedded in the envelope. A concrete discovery diagnostic is
// still written to stderr first so a consumer piping stdout still sees it.
func writeDiscoveryFailureJSON(stdout, stderr io.Writer, result app.DiscoveryResult, commandErr error) error {
	if result.Diagnostic != nil {
		if err := writeDiscoveryDiagnostic(stderr, result); err != nil {
			return err
		}
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(newDiscoveryFailureEnvelope(result, commandErr))
}

// writeDiscoveryHuman writes a deterministic human table to stdout and routes
// an empty-round diagnostic to stderr. Every network-controlled field is
// sanitized to a single printable line, and every writer error is propagated.
func writeDiscoveryHuman(stdout, stderr io.Writer, result app.DiscoveryResult) error {
	if len(result.Devices) == 0 {
		if result.Diagnostic != nil {
			if _, err := fmt.Fprintf(stderr, "No cameras found (%s).\n", diagnosticText(result.Diagnostic)); err != nil {
				return err
			}
		} else if _, err := fmt.Fprintln(stderr, "No cameras found."); err != nil {
			return err
		}
		return nil
	}
	for _, device := range result.Devices {
		if _, err := fmt.Fprintf(stdout, "%s\n", sanitizeLine(device.EndpointReference)); err != nil {
			return err
		}
		for _, address := range device.Addresses {
			if _, err := fmt.Fprintf(stdout, "  address: %s\n", sanitizeLine(address)); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeDiscoveryDiagnostic writes a single sanitized diagnostic line to stderr
// and propagates any writer error. The diagnostic text is produced by the
// application layer and is credential-free, but it is sanitized anyway because
// its detail may echo a device-influenced condition.
func writeDiscoveryDiagnostic(stderr io.Writer, result app.DiscoveryResult) error {
	_, err := fmt.Fprintf(stderr, "discovery diagnostic: %s\n", diagnosticText(result.Diagnostic))
	return err
}

// diagnosticText renders a discovery diagnostic as safe, English, single-line
// text. Both the category and the detail are sanitized before rendering.
func diagnosticText(diagnostic *app.DiscoveryDiagnostic) string {
	category := sanitizeLine(diagnostic.Category)
	if diagnostic.Detail == "" {
		return category
	}
	return category + ": " + sanitizeLine(diagnostic.Detail)
}
