package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/internal/buildinfo"
	"github.com/spf13/cobra"
)

// Diagnostic envelope kinds.
const (
	kindClockCheck = "clock_check"
	kindStability  = "stability"
	kindEvents     = "events"
	kindReport     = "device_report"
	// maxCLIEvents bounds the events kept for one JSON envelope.
	maxCLIEvents = 10000
)

// diagnosticEnvelope is the stable JSON envelope of the diagnostic commands.
type diagnosticEnvelope struct {
	SchemaVersion int            `json:"schema_version"`
	Kind          string         `json:"kind"`
	Result        any            `json:"result"`
	Error         *errorEnvelope `json:"error,omitempty"`
}

func writeDiagnosticJSON(stdout io.Writer, kind string, result any, commandErr error) error {
	envelope := diagnosticEnvelope{SchemaVersion: schemaVersion, Kind: kind, Result: result}
	if commandErr != nil {
		envelope.Error = newErrorEnvelope(commandErr)
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(envelope)
}

// diagnosticSpec describes one host diagnostic command.
type diagnosticSpec struct {
	use, short, long, kind string
	// bind adds command-specific flags.
	bind func(cmd *cobra.Command)
	// validate checks command-specific flags; it returns a safe message.
	validate func(cmd *cobra.Command) (string, error)
	// run executes the diagnostic. human writes the human output.
	run func(cmd *cobra.Command, request app.DeviceRequest, jsonMode bool) (result any, human func(io.Writer) error, err error)
}

// newDiagnosticCommand builds a "<command> <host>" diagnostic with the shared
// credential, timeout, and JSON handling of the media commands.
func newDiagnosticCommand(spec diagnosticSpec, lookup lookupEnv, intent jsonIntent) *cobra.Command {
	var (
		auth       credentialOptions
		timeout    time.Duration
		outputJSON bool
	)
	wantsJSON := func() bool { return intent.enabled(outputJSON) }
	fail := func(cmd *cobra.Command, message string, cause error) error {
		marked := markUsageError(cause)
		if wantsJSON() {
			if writeErr := writeDiagnosticJSON(cmd.OutOrStdout(), spec.kind, nil, app.NewError(app.CategoryUsage, message)); writeErr != nil {
				return writeErr
			}
		}
		return marked
	}
	cmd := &cobra.Command{
		Use:   spec.use,
		Short: spec.short,
		Long: spec.long + "\n\nCredentials are supplied only through an environment variable named by\n" +
			"--password-env; there is no plaintext password flag.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return fail(cmd, "exactly one camera endpoint is required", err)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if timeout <= 0 {
				return fail(cmd, "the timeout must be positive", fmt.Errorf("--timeout must be positive"))
			}
			if spec.validate != nil {
				if message, err := spec.validate(cmd); err != nil {
					return fail(cmd, message, err)
				}
			}
			credentials, err := auth.credentials(lookup)
			if err != nil {
				if wantsJSON() {
					if writeErr := writeDiagnosticJSON(cmd.OutOrStdout(), spec.kind, nil, app.NewError(app.CategoryUsage, "the credential options are not valid")); writeErr != nil {
						return writeErr
					}
				}
				return err
			}
			if err := auth.writeWarnings(cmd.ErrOrStderr(), wantsJSON()); err != nil {
				return err
			}
			result, human, err := spec.run(cmd, app.DeviceRequest{Host: args[0], Credentials: credentials, Timeout: timeout}, wantsJSON())
			if wantsJSON() {
				if writeErr := writeDiagnosticJSON(cmd.OutOrStdout(), spec.kind, result, err); writeErr != nil {
					return writeErr
				}
				return err
			}
			if human != nil {
				if writeErr := human(cmd.OutOrStdout()); writeErr != nil {
					return writeErr
				}
			}
			return err
		},
	}
	auth.bind(cmd)
	if spec.bind != nil {
		spec.bind(cmd)
	}
	cmd.Flags().DurationVar(&timeout, "timeout", defaultMediaCommandTimeout, "per-request timeout")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "write machine-readable JSON")
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, _ error) error {
		return fail(cmd, "the command flags are not valid", fmt.Errorf("the command flags are not valid"))
	})
	return cmd
}

func validateProfileFlag(cmd *cobra.Command, token string) (string, error) {
	if cmd.Flags().Changed("profile") {
		if err := app.ValidateSnapshotProfileToken(token); err != nil {
			return err.Error(), err
		}
	}
	return "", nil
}

func newClockCommand(diagnostics app.HostDiagnostics, lookup lookupEnv, intent jsonIntent) *cobra.Command {
	var (
		profileToken string
		noRTCP       bool
		rtcpWait     time.Duration
	)
	return newDiagnosticCommand(diagnosticSpec{
		use:   "clock <host>",
		short: "Check whether the camera clock is in sync",
		long: "Measure the camera clock against this computer's clock over ONVIF and,\n" +
			"unless --no-rtcp is set, from the RTCP Sender Reports of a stream. Each\n" +
			"estimate carries its uncertainty; the verdict is \"uncertain\" when the\n" +
			"measurement cannot tell. RTCP reflects the system clock only when the\n" +
			"camera stamps its reports with it.",
		kind: kindClockCheck,
		bind: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&profileToken, "profile", "", "media profile for the RTCP measurement (default: first usable profile)")
			cmd.Flags().BoolVar(&noRTCP, "no-rtcp", false, "measure over ONVIF only")
			cmd.Flags().DurationVar(&rtcpWait, "rtcp-wait", app.DefaultRTCPWait, "how long to wait for an RTCP Sender Report")
		},
		validate: func(cmd *cobra.Command) (string, error) {
			if rtcpWait < time.Second || rtcpWait > time.Minute {
				return "the RTCP wait must be between 1s and 1m", fmt.Errorf("--rtcp-wait must be between 1s and 1m")
			}
			return validateProfileFlag(cmd, profileToken)
		},
		run: func(cmd *cobra.Command, request app.DeviceRequest, _ bool) (any, func(io.Writer) error, error) {
			token := profileToken
			if noRTCP {
				token = "-"
			}
			result, err := diagnostics.ClockCheck(cmd.Context(), request, token, rtcpWait)
			if err != nil {
				return nil, nil, err
			}
			return result, func(w io.Writer) error { return writeClockHuman(w, result) }, nil
		},
	}, lookup, intent)
}

func writeClockHuman(w io.Writer, result app.ClockCheckResult) error {
	var b strings.Builder
	fmt.Fprintf(&b, "verdict: %s (tolerance ±%.0f s)\nmode: %s\n", result.Verdict, result.ToleranceSeconds, displayMediaValue(result.DateTimeType))
	if len(result.NTPServers) > 0 {
		fmt.Fprintf(&b, "ntp servers: %s\n", strings.Join(result.NTPServers, ", "))
	}
	for _, estimate := range []*app.ClockEstimate{result.ONVIF, result.RTCP} {
		if estimate != nil {
			fmt.Fprintf(&b, "%s offset: %+.3f s ± %.3f s\n", estimate.Source, estimate.OffsetSeconds, estimate.UncertaintySeconds)
		}
	}
	if result.RTCPUnavailable != "" {
		fmt.Fprintf(&b, "rtcp: %s\n", result.RTCPUnavailable)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func newStabilityCommand(probe app.ProbeUseCase, lookup lookupEnv, intent jsonIntent) *cobra.Command {
	var (
		profileToken string
		transport    string
		cycles       int
		duration     time.Duration
		pause        time.Duration
	)
	return newDiagnosticCommand(diagnosticSpec{
		use:   "stability <host>",
		short: "Repeatedly connect to a stream and measure it",
		long: "Run --cycles connect-measure-disconnect cycles of --duration each and\n" +
			"report pass/fail per cycle with time to first frame, frame rate,\n" +
			"bitrate, and packet loss. The release gate is 100 cycles of 20s.\n" +
			"Progress is written to standard error.",
		kind: kindStability,
		bind: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&profileToken, "profile", "", "media profile token (default: first profile with a supported stream)")
			cmd.Flags().StringVar(&transport, "transport", string(app.StreamTransportTCP), "RTP transport: tcp or udp")
			cmd.Flags().IntVar(&cycles, "cycles", app.DefaultStabilityCycles, "number of cycles (1 to 1000)")
			cmd.Flags().DurationVar(&duration, "duration", app.DefaultStabilityCycleLength, "duration of each cycle (1s to 10m)")
			cmd.Flags().DurationVar(&pause, "pause", time.Second, "pause between cycles (0 to 1m)")
		},
		validate: func(cmd *cobra.Command) (string, error) {
			if transport != string(app.StreamTransportTCP) && transport != string(app.StreamTransportUDP) {
				return "the transport must be tcp or udp", fmt.Errorf("--transport must be tcp or udp")
			}
			if err := app.ValidateStabilityOptions(app.StabilityOptions{Cycles: cycles, CycleDuration: duration, Pause: pause}); err != nil {
				return err.Error(), err
			}
			return validateProfileFlag(cmd, profileToken)
		},
		run: func(cmd *cobra.Command, request app.DeviceRequest, _ bool) (any, func(io.Writer) error, error) {
			if probe == nil {
				return nil, nil, app.NewInternalError("the probe dependency is not configured")
			}
			stderr := cmd.ErrOrStderr()
			report, err := app.RunStability(cmd.Context(), app.StabilityOptions{Cycles: cycles, CycleDuration: duration, Pause: pause},
				func(ctx context.Context) (app.ProbeResult, error) {
					return probe.Probe(ctx, app.ProbeRequest{
						Host: request.Host, Credentials: request.Credentials, ProfileToken: profileToken,
						Transport: app.StreamTransport(transport), Duration: duration, Timeout: request.Timeout,
					})
				},
				func(cycle app.StabilityCycle) {
					status := "pass"
					if !cycle.Passed {
						status = "FAIL: " + cycle.Error
					}
					_, _ = fmt.Fprintf(stderr, "cycle %d/%d: %s\n", cycle.Index, cycles, status)
				})
			if err != nil {
				return nil, nil, err
			}
			if cmd.Context().Err() != nil {
				err = app.NewError(app.CategoryCancelled, "the stability test was cancelled")
			} else if report.Verdict != app.StabilityPass {
				err = app.NewError(app.CategoryNetwork, "the stability test failed")
			}
			return report, func(w io.Writer) error { return writeStabilityHuman(w, report) }, err
		},
	}, lookup, intent)
}

func writeStabilityHuman(w io.Writer, report app.StabilityReport) error {
	_, err := fmt.Fprintf(w,
		"verdict: %s\ncycles: %d passed, %d failed of %d requested\nprofile: %s (%s)\n"+
			"time to first frame: min %d ms, median %d ms, max %d ms\nmean frame rate: %.2f fps\n"+
			"mean bitrate: %.0f kbps\nlost packets: %d\n",
		report.Verdict, report.Passed, report.Failed, report.CyclesRequested,
		displayMediaValue(report.ProfileToken), displayMediaValue(report.Transport),
		report.MinTTFFMillis, report.MedianTTFFMillis, report.MaxTTFFMillis,
		report.MeanFPS, report.MeanBitrateKbps, report.LostPackets)
	return err
}

// eventsResult is the JSON result of "ovm events".
type eventsResult struct {
	TopicFilter string         `json:"topic_filter,omitempty"`
	Events      []app.EventDTO `json:"events"`
	Truncated   bool           `json:"truncated"`
}

func newEventsCommand(diagnostics app.HostDiagnostics, lookup lookupEnv, intent jsonIntent) *cobra.Command {
	var (
		filter   string
		duration time.Duration
	)
	return newDiagnosticCommand(diagnosticSpec{
		use:   "events <host>",
		short: "Follow camera events through a PullPoint subscription",
		long: "Subscribe to the camera's events and print them for --duration. A\n" +
			"--filter such as \"tns1:VideoSource//.|tns1:Device//.\" limits the\n" +
			"topics. The subscription is renewed while it runs and removed at the end.",
		kind: kindEvents,
		bind: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&filter, "filter", "", "ConcreteSet topic expression")
			cmd.Flags().DurationVar(&duration, "duration", 30*time.Second, "how long to follow events (1s to 24h)")
		},
		validate: func(*cobra.Command) (string, error) {
			if duration < time.Second || duration > 24*time.Hour {
				return "the duration must be between 1s and 24h", fmt.Errorf("--duration must be between 1s and 24h")
			}
			return "", nil
		},
		run: func(cmd *cobra.Command, request app.DeviceRequest, jsonMode bool) (any, func(io.Writer) error, error) {
			ctx := cmd.Context()
			subscription, err := diagnostics.Events.SubscribeEvents(ctx, app.EventsRequest{DeviceRequest: request, TopicFilter: filter})
			if err != nil {
				return nil, nil, err
			}
			defer func() { _ = subscription.Close(context.WithoutCancel(ctx)) }()
			result := eventsResult{TopicFilter: filter, Events: []app.EventDTO{}}
			window, cancel := context.WithTimeout(ctx, duration)
			defer cancel()
			for window.Err() == nil {
				received, pullErr := subscription.Pull(window)
				if pullErr != nil {
					if window.Err() != nil && ctx.Err() == nil {
						break
					}
					return result, nil, pullErr
				}
				for _, event := range received {
					if !jsonMode {
						if _, writeErr := io.WriteString(cmd.OutOrStdout(), formatEventLine(event)+"\n"); writeErr != nil {
							return result, nil, writeErr
						}
					}
					if len(result.Events) < maxCLIEvents {
						result.Events = append(result.Events, event)
					} else {
						result.Truncated = true
					}
				}
			}
			return result, nil, nil
		},
	}, lookup, intent)
}

func formatEventLine(event app.EventDTO) string {
	var items []string
	for _, item := range append(append([]app.EventItemDTO(nil), event.Source...), event.Data...) {
		items = append(items, item.Name+"="+item.Value)
	}
	when := event.UTCTime
	if when == "" {
		when = "-"
	}
	return strings.TrimSpace(fmt.Sprintf("%s %s %s %s", when, event.Topic, event.PropertyOperation, strings.Join(items, " ")))
}

func newReportCommand(diagnostics app.HostDiagnostics, info buildinfo.Info, lookup lookupEnv, intent jsonIntent) *cobra.Command {
	var format string
	return newDiagnosticCommand(diagnosticSpec{
		use:   "report <host>",
		short: "Write a credential-free device report",
		long: "Read device information, media profiles, image, time, network, and\n" +
			"account settings and write a deterministic report without passwords.\n" +
			"--format selects markdown (default) or json; --json wraps the report in\n" +
			"the standard envelope. Sections the camera does not provide are listed\n" +
			"as not available.",
		kind: kindReport,
		bind: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&format, "format", "markdown", "report format: markdown or json")
		},
		validate: func(*cobra.Command) (string, error) {
			if format != "markdown" && format != "json" {
				return "the format must be markdown or json", fmt.Errorf("--format must be markdown or json")
			}
			return "", nil
		},
		run: func(cmd *cobra.Command, request app.DeviceRequest, _ bool) (any, func(io.Writer) error, error) {
			report := diagnostics.Report(cmd.Context(), request, app.ReportOptions{Generator: "ovm " + info.Version})
			if err := cmd.Context().Err(); err != nil {
				return report, nil, app.NewError(app.CategoryCancelled, "the report was cancelled")
			}
			return report, func(w io.Writer) error {
				if format == "json" {
					encoder := json.NewEncoder(w)
					encoder.SetEscapeHTML(false)
					encoder.SetIndent("", "  ")
					return encoder.Encode(report)
				}
				_, err := io.WriteString(w, app.RenderReportMarkdown(report))
				return err
			}, nil
		},
	}, lookup, intent)
}
