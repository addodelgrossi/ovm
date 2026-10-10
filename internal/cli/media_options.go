package cli

import (
	"io"
	"os"

	"github.com/addodelgrossi/ovm/internal/app"
	"github.com/spf13/cobra"
)

// defaultMediaCommandTimeout deliberately matches the established M1 info
// command contract. The application has its own fallback only for non-CLI
// callers that omit a timeout.
const defaultMediaCommandTimeout = defaultInfoTimeout

// credentialOptions binds the exact environment-only credential and downgrade
// policy shared by info, profiles, and snapshot. The M2 commands use this
// helper while the established info command retains the same underlying
// buildCredentials and writeDowngradeWarnings functions.
type credentialOptions struct {
	username           string
	passwordEnv        string
	allowBasic         bool
	allowInsecureBasic bool
	allowLegacyDigest  bool
}

func (options *credentialOptions) bind(command *cobra.Command) {
	flags := command.Flags()
	flags.StringVarP(&options.username, "user", "u", "", "authentication username")
	flags.StringVar(&options.passwordEnv, "password-env", "", "name of the environment variable holding the password")
	flags.BoolVar(&options.allowBasic, "allow-basic", false, "permit HTTP Basic authentication for HTTPS endpoints (requires credentials)")
	flags.BoolVar(&options.allowInsecureBasic, "allow-insecure-basic", false, "also permit Basic over plaintext HTTP (requires --allow-basic and credentials)")
	flags.BoolVar(&options.allowLegacyDigest, "allow-legacy-digest", false, "permit legacy no-qop (RFC 2069) Digest authentication (opt-in; requires credentials)")
}

func (options credentialOptions) credentials(lookup lookupEnv) (*app.Credentials, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	return buildCredentials(
		options.username,
		options.passwordEnv,
		options.allowBasic,
		options.allowInsecureBasic,
		options.allowLegacyDigest,
		lookup,
	)
}

func (options credentialOptions) writeWarnings(stderr io.Writer, jsonMode bool) error {
	return writeDowngradeWarnings(
		stderr,
		options.allowBasic,
		options.allowInsecureBasic,
		options.allowLegacyDigest,
		jsonMode,
	)
}
