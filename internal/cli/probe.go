package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/addodelgrossi/ovm/internal/app"
	"github.com/spf13/cobra"
)

// newProbeCommand builds "ovm probe <host>", a timed RTSP stream measurement.
func newProbeCommand(probe app.ProbeUseCase, lookup lookupEnv, intent jsonIntent) *cobra.Command {
	var (
		auth         credentialOptions
		profileToken string
		transport    string
		duration     time.Duration
		timeout      time.Duration
		outputJSON   bool
	)
	wantsJSON := func() bool { return intent.enabled(outputJSON) }
	fail := func(cmd *cobra.Command, message string, cause error) error {
		marked := markUsageError(cause)
		if wantsJSON() {
			if writeErr := writeProbeFailureJSON(cmd.OutOrStdout(), app.ProbeResult{}, app.NewError(app.CategoryUsage, message)); writeErr != nil {
				return writeErr
			}
		}
		return marked
	}

	cmd := &cobra.Command{
		Use:   "probe <host>",
		Short: "Measure a camera stream for a fixed duration",
		Long: "Connect to a profile's RTSP stream and measure codec, resolution, frame\n" +
			"rate, encoded bitrate, GOP, time to first frame, and packet loss for\n" +
			"--duration (default 30s). If --profile is omitted, the lexicographically\n" +
			"smallest profile with a supported stream is measured.\n\n" +
			"Credentials are supplied only through an environment variable named by\n" +
			"--password-env; there is no plaintext password flag.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return fail(cmd, "exactly one camera endpoint is required", err)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if timeout <= 0 {
				return fail(cmd, "the probe timeout must be positive", fmt.Errorf("--timeout must be positive"))
			}
			if duration < time.Second || duration > app.MaxProbeDuration {
				return fail(cmd, "the probe duration must be between 1s and 10m", fmt.Errorf("--duration must be between 1s and 10m"))
			}
			if transport != string(app.StreamTransportTCP) && transport != string(app.StreamTransportUDP) {
				return fail(cmd, "the transport must be tcp or udp", fmt.Errorf("--transport must be tcp or udp"))
			}
			if cmd.Flags().Changed("profile") {
				if validationErr := app.ValidateSnapshotProfileToken(profileToken); validationErr != nil {
					return fail(cmd, validationErr.Error(), validationErr)
				}
			}
			credentials, err := auth.credentials(lookup)
			if err != nil {
				if wantsJSON() {
					if writeErr := writeProbeFailureJSON(cmd.OutOrStdout(), app.ProbeResult{}, app.NewError(app.CategoryUsage, "the credential options are not valid")); writeErr != nil {
						return writeErr
					}
				}
				return err
			}
			if err := auth.writeWarnings(cmd.ErrOrStderr(), wantsJSON()); err != nil {
				return err
			}
			if probe == nil {
				err := app.NewInternalError("the probe dependency is not configured")
				if wantsJSON() {
					if writeErr := writeProbeFailureJSON(cmd.OutOrStdout(), app.ProbeResult{}, err); writeErr != nil {
						return writeErr
					}
				}
				return err
			}

			result, err := probe.Probe(cmd.Context(), app.ProbeRequest{
				Host:         args[0],
				Credentials:  credentials,
				ProfileToken: profileToken,
				Transport:    app.StreamTransport(transport),
				Duration:     duration,
				Timeout:      timeout,
			})
			if err != nil {
				if wantsJSON() {
					if writeErr := writeProbeFailureJSON(cmd.OutOrStdout(), result, err); writeErr != nil {
						return writeErr
					}
				} else if result.Frames > 0 {
					if writeErr := writeProbeHuman(cmd.OutOrStdout(), result); writeErr != nil {
						return writeErr
					}
				}
				return err
			}
			if wantsJSON() {
				return writeProbeJSON(cmd.OutOrStdout(), result)
			}
			return writeProbeHuman(cmd.OutOrStdout(), result)
		},
	}

	auth.bind(cmd)
	cmd.Flags().StringVar(&profileToken, "profile", "", "media profile token (default: first profile with a supported stream)")
	cmd.Flags().StringVar(&transport, "transport", string(app.StreamTransportTCP), "RTP transport: tcp or udp")
	cmd.Flags().DurationVar(&duration, "duration", app.DefaultProbeDuration, "measurement duration (1s to 10m)")
	cmd.Flags().DurationVar(&timeout, "timeout", defaultMediaCommandTimeout, "profile resolution and connection timeout")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "write machine-readable JSON")
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, _ error) error {
		return fail(cmd, "the probe flags are not valid", fmt.Errorf("the probe flags are not valid"))
	})
	return cmd
}

func writeProbeJSON(stdout io.Writer, result app.ProbeResult) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(newProbeEnvelope(result))
}

func writeProbeFailureJSON(stdout io.Writer, result app.ProbeResult, commandErr error) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(newProbeFailureEnvelope(result, commandErr))
}

func writeProbeHuman(stdout io.Writer, result app.ProbeResult) error {
	gop := "unknown"
	if result.GOPFrames > 0 {
		gop = fmt.Sprintf("%d frames (%.2f s)", result.GOPFrames, result.GOPSeconds)
	}
	_, err := fmt.Fprintf(
		stdout,
		"profile token: %s\ntransport: %s\ncodec: %s\nresolution: %dx%d\nframe rate: %.2f fps\n"+
			"bitrate: %.0f kbps\ngop: %s\ntime to first frame: %d ms\nframes: %d\nkeyframes: %d\n"+
			"lost packets: %d\nduration: %.1f s\ncomplete: %t\n",
		displayMediaValue(result.ProfileToken),
		displayMediaValue(result.Transport),
		displayMediaValue(result.Codec),
		result.Width, result.Height,
		result.FramesPerSecond,
		result.BitrateKbps,
		gop,
		result.TimeToFirstFrameMillis,
		result.Frames,
		result.Keyframes,
		result.LostPackets,
		float64(result.DurationMillis)/1000,
		result.Complete,
	)
	return err
}
