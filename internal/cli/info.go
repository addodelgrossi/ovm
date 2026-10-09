package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"

	"github.com/addodelgrossi/ovm/internal/app"
	"github.com/spf13/cobra"
)

// defaultInfoTimeout bounds an info use case when the user supplies none.
const defaultInfoTimeout = 15 * time.Second

// Downgrade warning text. Each warning is a fixed, credential-free sentence
// emitted exactly once to stderr when the corresponding opt-in is used. It
// names neither the endpoint, the user, nor any secret.
const (
	allowBasicWarning         = "warning: HTTP Basic authentication is enabled for HTTPS endpoints; it sends reusable credentials and should be used only when Digest is unavailable."
	allowInsecureBasicWarning = "warning: HTTP Basic authentication over plaintext HTTP is enabled; the username and password are exposed in a trivially decodable form to anyone on the network path."
	allowLegacyDigestWarning  = "warning: legacy no-qop (RFC 2069) Digest authentication is enabled; it is weaker than qop=auth and should be avoided. This is a separate downgrade from HTTP Basic."
)

// envVarNamePattern validates a POSIX-style environment variable name so a
// malformed --password-env value is rejected before any lookup.
var envVarNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// lookupEnv is the environment lookup seam, overridable in tests so a password
// value never has to be placed in the real process environment.
type lookupEnv func(name string) (string, bool)

// newInfoCommand builds "ovm info <host>". The inventory use case and the
// environment lookup are injected so tests exercise the full path without a
// real network or a real environment variable.
//
// M1 deliberately supports environment-variable credentials only. There is no
// plaintext -p flag; secure terminal and keychain input are later milestones.
func newInfoCommand(inventory app.InventoryUseCase, lookup lookupEnv, intent jsonIntent) *cobra.Command {
	var (
		username           string
		passwordEnv        string
		allowBasic         bool
		allowInsecureBasic bool
		allowLegacyDigest  bool
		timeout            time.Duration
		outputJSON         bool
	)
	wantsJSON := func() bool { return intent.enabled(outputJSON) }
	if lookup == nil {
		lookup = os.LookupEnv
	}

	cmd := &cobra.Command{
		Use:   "info <host>",
		Short: "Retrieve a device inventory from an ONVIF camera",
		Long: "Retrieve device information, capabilities, services, scopes,\n" +
			"hostname, and network interfaces from a single camera.\n\n" +
			"Credentials are supplied only through an environment variable named\n" +
			"by --password-env; there is no plaintext password flag. Secure\n" +
			"terminal and system-keychain input are planned for a later milestone.\n\n" +
			"Authentication is secure by default: Basic, Basic over plaintext HTTP,\n" +
			"and legacy no-qop Digest are separate opt-ins. Use --allow-basic for\n" +
			"HTTPS only, add --allow-insecure-basic for HTTP, or use\n" +
			"--allow-legacy-digest for RFC 2069. Each downgrade prints a warning.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				marked := markUsageError(err)
				if wantsJSON() {
					usageErr := app.NewError(app.CategoryUsage, "exactly one camera endpoint is required")
					if writeErr := writeInventoryFailureJSON(cmd.OutOrStdout(), app.Inventory{Operations: []app.OperationStatus{}}, usageErr); writeErr != nil {
						return writeErr
					}
				}
				return marked
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if timeout <= 0 {
				marked := markUsageError(fmt.Errorf("--timeout must be positive"))
				if wantsJSON() {
					usageErr := app.NewError(app.CategoryUsage, "the inventory timeout must be positive")
					if writeErr := writeInventoryFailureJSON(cmd.OutOrStdout(), app.Inventory{Operations: []app.OperationStatus{}}, usageErr); writeErr != nil {
						return writeErr
					}
				}
				return marked
			}
			if inventory == nil {
				return app.NewInternalError("the inventory dependency is not configured")
			}
			host := args[0]

			credentials, buildErr := buildCredentials(
				username,
				passwordEnv,
				allowBasic,
				allowInsecureBasic,
				allowLegacyDigest,
				lookup,
			)
			if buildErr != nil {
				if wantsJSON() {
					usageErr := app.NewError(app.CategoryUsage, "the credential options are not valid")
					if writeErr := writeInventoryFailureJSON(cmd.OutOrStdout(), app.Inventory{Operations: []app.OperationStatus{}}, usageErr); writeErr != nil {
						return writeErr
					}
				}
				return buildErr
			}

			// Emit a single explicit warning to stderr for each enabled
			// downgrade before any work starts. A writer error is propagated so
			// the operator is never silently denied the warning.
			if writeErr := writeDowngradeWarnings(
				cmd.ErrOrStderr(),
				allowBasic,
				allowInsecureBasic,
				allowLegacyDigest,
				wantsJSON(),
			); writeErr != nil {
				return writeErr
			}

			request := app.InventoryRequest{
				Host:        host,
				Credentials: credentials,
				Timeout:     timeout,
			}
			result, err := inventory.Inventory(cmd.Context(), request)
			if err != nil {
				// Preserve the state-only inventory on total failure. JSON includes
				// it in the versioned envelope; human mode prints the safe endpoint
				// and operation map before the process reports the nonzero error.
				if wantsJSON() {
					if writeErr := writeInventoryFailureJSON(cmd.OutOrStdout(), result, err); writeErr != nil {
						return writeErr
					}
				} else if len(result.Operations) > 0 {
					if writeErr := writeInventoryHuman(cmd.OutOrStdout(), result); writeErr != nil {
						return writeErr
					}
				}
				return err
			}
			if wantsJSON() {
				return writeInventoryJSON(cmd.OutOrStdout(), result)
			}
			return writeInventoryHuman(cmd.OutOrStdout(), result)
		},
	}

	cmd.Flags().StringVarP(&username, "user", "u", "", "authentication username")
	cmd.Flags().StringVar(&passwordEnv, "password-env", "", "name of the environment variable holding the password")
	cmd.Flags().BoolVar(&allowBasic, "allow-basic", false, "permit HTTP Basic authentication for HTTPS endpoints (requires credentials)")
	cmd.Flags().BoolVar(&allowInsecureBasic, "allow-insecure-basic", false, "also permit Basic over plaintext HTTP (requires --allow-basic and credentials)")
	cmd.Flags().BoolVar(&allowLegacyDigest, "allow-legacy-digest", false, "permit legacy no-qop (RFC 2069) Digest authentication (opt-in; requires credentials)")
	cmd.Flags().DurationVar(&timeout, "timeout", defaultInfoTimeout, "overall operation timeout")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "write machine-readable JSON")
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		marked := markUsageError(err)
		if wantsJSON() {
			usageErr := app.NewError(app.CategoryUsage, "the inventory flags are not valid")
			if writeErr := writeInventoryFailureJSON(cmd.OutOrStdout(), app.Inventory{Operations: []app.OperationStatus{}}, usageErr); writeErr != nil {
				return writeErr
			}
		}
		return marked
	})
	return cmd
}

// buildCredentials validates credential flags and resolves the password from
// the named environment variable. Basic over plaintext HTTP is a separate,
// higher-risk opt-in that is invalid unless Basic itself is enabled. Every
// downgrade requires credentials, and returned errors never include secrets.
func buildCredentials(
	username string,
	passwordEnv string,
	allowBasic bool,
	allowInsecureBasic bool,
	allowLegacyDigest bool,
	lookup lookupEnv,
) (*app.Credentials, error) {
	if allowInsecureBasic && !allowBasic {
		return nil, markUsageError(fmt.Errorf("--allow-insecure-basic requires --allow-basic"))
	}
	if username == "" && passwordEnv == "" {
		switch {
		case allowBasic:
			return nil, markUsageError(fmt.Errorf("--allow-basic requires --user and --password-env"))
		case allowInsecureBasic:
			return nil, markUsageError(fmt.Errorf("--allow-insecure-basic requires --user and --password-env"))
		case allowLegacyDigest:
			return nil, markUsageError(fmt.Errorf("--allow-legacy-digest requires --user and --password-env"))
		default:
			return nil, nil
		}
	}
	if username == "" {
		return nil, markUsageError(fmt.Errorf("--password-env requires --user"))
	}
	if passwordEnv == "" {
		return nil, markUsageError(fmt.Errorf("--user requires --password-env (there is no plaintext password flag)"))
	}
	if !envVarNamePattern.MatchString(passwordEnv) {
		return nil, markUsageError(fmt.Errorf("--password-env is not a valid environment variable name"))
	}
	password, ok := lookup(passwordEnv)
	if !ok {
		return nil, markUsageError(fmt.Errorf("the environment variable named by --password-env is not set"))
	}
	credentials, err := app.NewCredentialsWithPolicy(username, password, app.Policy{
		AllowBasic:         allowBasic,
		AllowBasicOverHTTP: allowInsecureBasic,
		AllowLegacyDigest:  allowLegacyDigest,
	})
	if err != nil {
		return nil, markUsageError(fmt.Errorf("the supplied credentials are not valid"))
	}
	return credentials, nil
}

// writeDowngradeWarnings emits one fixed, credential-free warning per enabled
// downgrade to stderr. Warnings never pollute JSON stdout, and write failures
// are propagated so the operator is not silently denied a warning.
func writeDowngradeWarnings(
	stderr io.Writer,
	allowBasic bool,
	allowInsecureBasic bool,
	allowLegacyDigest bool,
	jsonMode bool,
) error {
	_ = jsonMode
	if allowBasic {
		if _, err := fmt.Fprintln(stderr, allowBasicWarning); err != nil {
			return err
		}
	}
	if allowInsecureBasic {
		if _, err := fmt.Fprintln(stderr, allowInsecureBasicWarning); err != nil {
			return err
		}
	}
	if allowLegacyDigest {
		if _, err := fmt.Fprintln(stderr, allowLegacyDigestWarning); err != nil {
			return err
		}
	}
	return nil
}

// writeInventoryJSON writes the stable inventory envelope to stdout. The
// encoder safely escapes every string value, so no sanitization is applied:
// machine consumers require the original values. Any writer error is
// propagated.
func writeInventoryJSON(stdout io.Writer, result app.Inventory) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(newInventoryEnvelope(result))
}

// writeInventoryFailureJSON emits the state-only result and a safe error in
// the same versioned envelope used for successful inventory output.
func writeInventoryFailureJSON(stdout io.Writer, result app.Inventory, commandErr error) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(newInventoryFailureEnvelope(result, commandErr))
}

// writeInventoryHuman writes a deterministic human summary to stdout. It prints
// the per-operation capability states first, then any successful payload
// fields. Every camera-controlled string is sanitized to a single printable
// line, every writer error is propagated (including the late device-information
// and hostname writes), and it never prints credentials.
func writeInventoryHuman(stdout io.Writer, result app.Inventory) error {
	write := func(format string, args ...any) error {
		_, err := fmt.Fprintf(stdout, format, args...)
		return err
	}
	if err := write("endpoint: %s\n", sanitizeLine(result.Endpoint)); err != nil {
		return err
	}
	if err := write("operations:\n"); err != nil {
		return err
	}
	for _, status := range result.Operations {
		line := fmt.Sprintf("  %-22s %s", status.Operation, status.State)
		if status.Detail != "" {
			line += " (" + sanitizeLine(status.Detail) + ")"
		}
		if err := write("%s\n", line); err != nil {
			return err
		}
	}
	if info := result.DeviceInformation; info != nil {
		if err := write("device information:\n"); err != nil {
			return err
		}
		fields := []struct{ label, value string }{
			{"manufacturer", info.Manufacturer},
			{"model", info.Model},
			{"firmware", info.FirmwareVersion},
			{"serial", info.SerialNumber},
			{"hardware id", info.HardwareID},
		}
		for _, field := range fields {
			if err := write("  %s: %s\n", field.label, sanitizeLine(field.value)); err != nil {
				return err
			}
		}
	}
	if clock := result.SystemDateAndTime; clock != nil {
		if err := write("system time:\n  mode: %s\n  daylight savings: %t\n", sanitizeLine(clock.DateTimeType), clock.DaylightSavings); err != nil {
			return err
		}
		if clock.UTC != "" {
			if err := write("  utc: %s\n", sanitizeLine(clock.UTC)); err != nil {
				return err
			}
		}
		if clock.TimeZone != "" {
			if err := write("  time zone: %s\n", sanitizeLine(clock.TimeZone)); err != nil {
				return err
			}
		}
	}
	if host := result.Hostname; host != nil {
		if err := write("hostname: %s (from DHCP: %t)\n", sanitizeLine(host.Name), host.FromDHCP); err != nil {
			return err
		}
	}
	if capabilities := result.Capabilities; capabilities != nil {
		if err := write("capability endpoints:\n"); err != nil {
			return err
		}
		endpoints := []struct{ label, value string }{
			{"device", capabilities.DeviceXAddr},
			{"media", capabilities.MediaXAddr},
			{"events", capabilities.EventsXAddr},
			{"imaging", capabilities.ImagingXAddr},
			{"ptz", capabilities.PTZXAddr},
			{"analytics", capabilities.AnalyticsXAddr},
		}
		for _, endpoint := range endpoints {
			if endpoint.value != "" {
				if err := write("  %s: %s\n", endpoint.label, sanitizeLine(endpoint.value)); err != nil {
					return err
				}
			}
		}
	}
	if len(result.Services) > 0 {
		if err := write("services:\n"); err != nil {
			return err
		}
		for _, service := range result.Services {
			version := "unknown"
			if service.HasVersion {
				version = fmt.Sprintf("%d.%d", service.VersionMajor, service.VersionMinor)
			}
			if err := write("  %s (v%s): %s\n", sanitizeLine(service.Namespace), version, sanitizeLine(service.XAddr)); err != nil {
				return err
			}
		}
	}
	if len(result.Scopes) > 0 {
		if err := write("scopes:\n"); err != nil {
			return err
		}
		for _, scope := range result.Scopes {
			if err := write("  %s: %s\n", sanitizeLine(scope.Definition), sanitizeLine(scope.Item)); err != nil {
				return err
			}
		}
	}
	if len(result.NetworkInterfaces) > 0 {
		if err := write("network interfaces:\n"); err != nil {
			return err
		}
		for _, network := range result.NetworkInterfaces {
			if err := write("  %s (%s): enabled=%t\n", sanitizeLine(network.Token), sanitizeLine(network.Name), network.Enabled); err != nil {
				return err
			}
			if network.HwAddress != "" {
				if err := write("    hardware address: %s\n", sanitizeLine(network.HwAddress)); err != nil {
					return err
				}
			}
			if network.HasMTU {
				if err := write("    mtu: %d\n", network.MTU); err != nil {
					return err
				}
			}
			if network.HasIPv4 {
				if err := write("    ipv4 enabled=%t dhcp=%t\n", network.IPv4Enabled, network.DHCP); err != nil {
					return err
				}
			}
			for _, address := range network.IPv4Addresses {
				if err := write("    address: %s/%d (from DHCP: %t)\n", sanitizeLine(address.Address), address.PrefixLength, address.FromDHCP); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
