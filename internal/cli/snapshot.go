package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/addodelgrossi/ovm/internal/app"
	"github.com/spf13/cobra"
)

// newSnapshotCommand builds "ovm snapshot <host> -o FILE". JPEG bytes are
// owned entirely by the application use case and are never written to stdout.
func newSnapshotCommand(snapshot app.SnapshotUseCase, lookup lookupEnv, intent jsonIntent) *cobra.Command {
	var (
		auth         credentialOptions
		profileToken string
		outputPath   string
		force        bool
		timeout      time.Duration
		outputJSON   bool
	)
	wantsJSON := func() bool { return intent.enabled(outputJSON) }
	emptyMetadata := func() app.SnapshotMetadata { return app.SnapshotMetadata{} }
	writeUsageFailure := func(stdout io.Writer, message string) error {
		return writeSnapshotFailureJSON(stdout, emptyMetadata(), app.NewError(app.CategoryUsage, message))
	}

	cmd := &cobra.Command{
		Use:   "snapshot <host> -o FILE",
		Short: "Retrieve a JPEG snapshot atomically",
		Long: "Retrieve one JPEG snapshot and write its bytes only to the explicitly\n" +
			"selected output file. If --profile is omitted, the lexicographically\n" +
			"smallest non-empty profile token is selected. Existing files are refused\n" +
			"unless --force is supplied.\n\n" +
			"Credentials are supplied only through an environment variable named by\n" +
			"--password-env; there is no plaintext password flag. Authentication\n" +
			"downgrades use the same explicit opt-ins and warnings as ovm info.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				marked := markUsageError(err)
				if wantsJSON() {
					if writeErr := writeUsageFailure(cmd.OutOrStdout(), "exactly one camera endpoint is required"); writeErr != nil {
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
					if writeErr := writeUsageFailure(cmd.OutOrStdout(), "the snapshot timeout must be positive"); writeErr != nil {
						return writeErr
					}
				}
				return marked
			}
			if !cmd.Flags().Changed("output") || outputPath == "" {
				marked := markUsageError(fmt.Errorf("-o/--output is required and must name a file"))
				if wantsJSON() {
					if writeErr := writeUsageFailure(cmd.OutOrStdout(), "an explicit output path is required"); writeErr != nil {
						return writeErr
					}
				}
				return marked
			}
			if cmd.Flags().Changed("profile") {
				if validationErr := app.ValidateSnapshotProfileToken(profileToken); validationErr != nil {
					marked := markUsageError(validationErr)
					if wantsJSON() {
						if writeErr := writeUsageFailure(cmd.OutOrStdout(), validationErr.Error()); writeErr != nil {
							return writeErr
						}
					}
					return marked
				}
			}

			credentials, err := auth.credentials(lookup)
			if err != nil {
				if wantsJSON() {
					if writeErr := writeUsageFailure(cmd.OutOrStdout(), "the credential options are not valid"); writeErr != nil {
						return writeErr
					}
				}
				return err
			}
			if err := auth.writeWarnings(cmd.ErrOrStderr(), wantsJSON()); err != nil {
				return err
			}
			if snapshot == nil {
				err := app.NewInternalError("the snapshot dependency is not configured")
				if wantsJSON() {
					if writeErr := writeSnapshotFailureJSON(cmd.OutOrStdout(), emptyMetadata(), err); writeErr != nil {
						return writeErr
					}
				}
				return err
			}

			metadata, err := snapshot.Snapshot(cmd.Context(), app.SnapshotRequest{
				Host:         args[0],
				Credentials:  credentials,
				ProfileToken: profileToken,
				OutputPath:   outputPath,
				Overwrite:    force,
				Timeout:      timeout,
			})
			if err != nil {
				if wantsJSON() {
					if writeErr := writeSnapshotFailureJSON(cmd.OutOrStdout(), metadata, err); writeErr != nil {
						return writeErr
					}
				}
				return err
			}
			if wantsJSON() {
				return writeSnapshotJSON(cmd.OutOrStdout(), metadata)
			}
			return writeSnapshotHuman(cmd.OutOrStdout(), metadata)
		},
	}

	auth.bind(cmd)
	cmd.Flags().StringVar(&profileToken, "profile", "", "media profile token (default: deterministic primary profile)")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "destination JPEG file (required)")
	cmd.Flags().BoolVar(&force, "force", false, "atomically replace an existing destination")
	cmd.Flags().DurationVar(&timeout, "timeout", defaultMediaCommandTimeout, "overall operation timeout")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "write machine-readable metadata JSON")
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, _ error) error {
		marked := markUsageError(fmt.Errorf("the snapshot flags are not valid"))
		if wantsJSON() {
			usageErr := app.NewError(app.CategoryUsage, "the snapshot flags are not valid")
			if writeErr := writeSnapshotFailureJSON(cmd.OutOrStdout(), emptyMetadata(), usageErr); writeErr != nil {
				return writeErr
			}
		}
		return marked
	})
	return cmd
}

func writeSnapshotJSON(stdout io.Writer, result app.SnapshotMetadata) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(newSnapshotEnvelope(result))
}

func writeSnapshotFailureJSON(stdout io.Writer, result app.SnapshotMetadata, commandErr error) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(newSnapshotFailureEnvelope(result, commandErr))
}

// writeSnapshotHuman writes safe metadata only. Image bytes remain exclusively
// in the output file created by the application use case.
func writeSnapshotHuman(stdout io.Writer, result app.SnapshotMetadata) error {
	_, err := fmt.Fprintf(
		stdout,
		"profile token: %s\noutput path: %s\nbyte size: %d\ncontent type: %s\nsource: %s\n",
		displayMediaValue(result.ProfileToken),
		displayMediaValue(result.OutputPath),
		result.ByteSize,
		displayMediaValue(result.ContentType),
		displayMediaValue(string(result.Source)),
	)
	return err
}
