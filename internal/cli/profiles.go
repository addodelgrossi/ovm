package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/addodelgrossi/ovm/internal/app"
	"github.com/spf13/cobra"
)

// newProfilesCommand builds "ovm profiles <host>". Authentication flags and
// downgrade warnings intentionally match the M1 info command exactly.
func newProfilesCommand(profiles app.MediaProfilesUseCase, lookup lookupEnv, intent jsonIntent) *cobra.Command {
	var (
		auth       credentialOptions
		timeout    time.Duration
		outputJSON bool
	)
	wantsJSON := func() bool { return intent.enabled(outputJSON) }
	emptyResult := func() app.MediaProfilesResult {
		return app.MediaProfilesResult{Profiles: []app.MediaProfileDTO{}}
	}

	cmd := &cobra.Command{
		Use:   "profiles <host>",
		Short: "List normalized ONVIF media profiles",
		Long: "List every media profile with service provenance, encoder settings,\n" +
			"and presentation-safe stream and snapshot URIs. URI queries and embedded\n" +
			"credentials are never printed.\n\n" +
			"Credentials are supplied only through an environment variable named by\n" +
			"--password-env; there is no plaintext password flag. Authentication\n" +
			"downgrades use the same explicit opt-ins and warnings as ovm info.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				marked := markUsageError(err)
				if wantsJSON() {
					usageErr := app.NewError(app.CategoryUsage, "exactly one camera endpoint is required")
					if writeErr := writeProfilesFailureJSON(cmd.OutOrStdout(), emptyResult(), usageErr); writeErr != nil {
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
					usageErr := app.NewError(app.CategoryUsage, "the profiles timeout must be positive")
					if writeErr := writeProfilesFailureJSON(cmd.OutOrStdout(), emptyResult(), usageErr); writeErr != nil {
						return writeErr
					}
				}
				return marked
			}

			credentials, err := auth.credentials(lookup)
			if err != nil {
				if wantsJSON() {
					usageErr := app.NewError(app.CategoryUsage, "the credential options are not valid")
					if writeErr := writeProfilesFailureJSON(cmd.OutOrStdout(), emptyResult(), usageErr); writeErr != nil {
						return writeErr
					}
				}
				return err
			}
			if err := auth.writeWarnings(cmd.ErrOrStderr(), wantsJSON()); err != nil {
				return err
			}
			if profiles == nil {
				err := app.NewInternalError("the media profiles dependency is not configured")
				if wantsJSON() {
					if writeErr := writeProfilesFailureJSON(cmd.OutOrStdout(), emptyResult(), err); writeErr != nil {
						return writeErr
					}
				}
				return err
			}

			result, err := profiles.Profiles(cmd.Context(), app.MediaProfilesRequest{
				Host:        args[0],
				Credentials: credentials,
				Timeout:     timeout,
			})
			if result.Profiles == nil {
				result.Profiles = []app.MediaProfileDTO{}
			}
			if err != nil {
				if wantsJSON() {
					if writeErr := writeProfilesFailureJSON(cmd.OutOrStdout(), result, err); writeErr != nil {
						return writeErr
					}
				} else if len(result.Profiles) > 0 {
					if writeErr := writeProfilesHuman(cmd.OutOrStdout(), result); writeErr != nil {
						return writeErr
					}
				}
				return err
			}
			if wantsJSON() {
				return writeProfilesJSON(cmd.OutOrStdout(), result)
			}
			return writeProfilesHuman(cmd.OutOrStdout(), result)
		},
	}

	auth.bind(cmd)
	cmd.Flags().DurationVar(&timeout, "timeout", defaultMediaCommandTimeout, "overall operation timeout")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "write machine-readable JSON")
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, _ error) error {
		marked := markUsageError(fmt.Errorf("the profiles flags are not valid"))
		if wantsJSON() {
			usageErr := app.NewError(app.CategoryUsage, "the profiles flags are not valid")
			if writeErr := writeProfilesFailureJSON(cmd.OutOrStdout(), emptyResult(), usageErr); writeErr != nil {
				return writeErr
			}
		}
		return marked
	})
	return cmd
}

func writeProfilesJSON(stdout io.Writer, result app.MediaProfilesResult) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(newProfilesEnvelope(result))
}

func writeProfilesFailureJSON(stdout io.Writer, result app.MediaProfilesResult, commandErr error) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(newProfilesFailureEnvelope(result, commandErr))
}

// writeProfilesHuman emits every profile and every requested presentation
// field in a fixed order. Camera-controlled strings are line-sanitized; URI
// values have already crossed the application's presentation-safe boundary.
func writeProfilesHuman(stdout io.Writer, result app.MediaProfilesResult) error {
	write := func(format string, args ...any) error {
		_, err := fmt.Fprintf(stdout, format, args...)
		return err
	}
	if err := write("media2 available: %t\nmedia1 available: %t\nprofiles:\n", result.Media2Available, result.Media1Available); err != nil {
		return err
	}
	for index, profile := range result.Profiles {
		if err := write("profile %d:\n", index+1); err != nil {
			return err
		}
		fixed := "unavailable"
		if profile.HasFixed {
			fixed = strconv.FormatBool(profile.Fixed)
		}
		fields := []struct {
			label string
			value string
		}{
			{"token", displayMediaValue(profile.Token)},
			{"name", displayMediaValue(profile.Name)},
			{"fixed", fixed},
			{"source", displayMediaValue(string(profile.Source))},
			{"video source config token", displayMediaValue(profile.VideoSourceConfigToken)},
			{"video source token", displayMediaValue(profile.VideoSourceToken)},
			{"video encoder config token", displayMediaValue(profile.VideoEncoderConfigToken)},
		}
		for _, field := range fields {
			if err := write("  %s: %s\n", field.label, field.value); err != nil {
				return err
			}
		}

		video := profile.Video
		if err := write("  video:\n"); err != nil {
			return err
		}
		videoFields := []struct {
			label string
			value string
		}{
			{"config token", "unavailable"},
			{"codec", "unavailable"},
			{"resolution", "unavailable"},
			{"frame rate", "unavailable"},
			{"bitrate", "unavailable"},
			{"gop length", "unavailable"},
			{"codec profile", "unavailable"},
		}
		if video != nil {
			videoFields[0].value = displayMediaValue(video.ConfigToken)
			videoFields[1].value = displayMediaValue(video.Codec)
			if video.Width > 0 && video.Height > 0 {
				videoFields[2].value = fmt.Sprintf("%dx%d", video.Width, video.Height)
			}
			if video.HasFrameRate {
				videoFields[3].value = strconv.FormatFloat(video.FrameRateFPS, 'f', -1, 64) + " fps"
			}
			if video.BitrateKbps > 0 {
				videoFields[4].value = strconv.Itoa(video.BitrateKbps) + " kbps"
			}
			if video.GOPLength > 0 {
				videoFields[5].value = strconv.Itoa(video.GOPLength)
			}
			videoFields[6].value = displayMediaValue(video.Profile)
		}
		for _, field := range videoFields {
			if err := write("    %s: %s\n", field.label, field.value); err != nil {
				return err
			}
		}

		if err := write("  audio:\n"); err != nil {
			return err
		}
		audioFields := []struct {
			label string
			value string
		}{
			{"config token", "unavailable"},
			{"codec", "unavailable"},
			{"bitrate", "unavailable"},
			{"sample rate", "unavailable"},
		}
		if audio := profile.Audio; audio != nil {
			audioFields[0].value = displayMediaValue(audio.ConfigToken)
			audioFields[1].value = displayMediaValue(audio.Codec)
			if audio.BitrateKbps > 0 {
				audioFields[2].value = strconv.Itoa(audio.BitrateKbps) + " kbps"
			}
			if audio.SampleRateKHz > 0 {
				audioFields[3].value = strconv.Itoa(audio.SampleRateKHz) + " kHz"
			}
		}
		for _, field := range audioFields {
			if err := write("    %s: %s\n", field.label, field.value); err != nil {
				return err
			}
		}

		tcpURI, udpURI := "unavailable", "unavailable"
		if profile.Stream != nil {
			tcpURI = displayMediaValue(profile.Stream.TCPURI)
			udpURI = displayMediaValue(profile.Stream.UDPURI)
		}
		snapshotURI := "unavailable"
		if profile.Snapshot != nil {
			snapshotURI = displayMediaValue(profile.Snapshot.URI)
		}
		if err := write("  stream tcp uri: %s\n  stream udp uri: %s\n  snapshot uri: %s\n", tcpURI, udpURI, snapshotURI); err != nil {
			return err
		}
		if err := write("  operations:\n"); err != nil {
			return err
		}
		for _, status := range profile.Operations {
			line := fmt.Sprintf(
				"    %s: %s (source=%s, fallback=%t)",
				sanitizeLine(string(status.Operation)),
				sanitizeLine(string(status.State)),
				displayMediaValue(string(status.Source)),
				status.FellBack,
			)
			if status.Detail != "" {
				line += " - " + sanitizeLine(status.Detail)
			}
			if err := write("%s\n", line); err != nil {
				return err
			}
		}
	}
	return nil
}

func displayMediaValue(value string) string {
	if value == "" {
		return "unavailable"
	}
	return sanitizeLine(value)
}
