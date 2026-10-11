package app

import (
	"context"
	"time"

	"github.com/addodelgrossi/ovm/pkg/stream"
)

// HostDiagnostics runs the diagnostics against an endpoint and credentials
// supplied directly, as the CLI does, instead of a stored device.
type HostDiagnostics struct {
	Inventory InventoryUseCase
	Media     MediaProfilesUseCase
	Imaging   ImagingUseCase
	Time      TimeUseCase
	Network   NetworkUseCase
	Users     UsersUseCase
	Clock     ClockUseCase
	Events    EventsUseCase
	Live      LiveStreamOpener
}

// NewProductionHostDiagnostics returns HostDiagnostics over the production
// adapters.
func NewProductionHostDiagnostics() HostDiagnostics {
	services := NewProductionMediaServices(ProductionOptions{})
	return HostDiagnostics{
		Inventory: NewProductionInventory(), Media: services, Imaging: services, Time: services,
		Network: services, Users: services, Clock: services, Events: services, Live: NewProductionLiveStreams(),
	}
}

// Report builds a device report for an endpoint.
func (h HostDiagnostics) Report(ctx context.Context, request DeviceRequest, options ReportOptions) DeviceReport {
	endpoint := request.Host
	if canonical, err := canonicalizeOperationalDeviceEndpoint(request.Host); err == nil {
		canonical.RawQuery = ""
		endpoint = canonical.String()
	}
	sources := reportSources{
		device: ReportDevice{Endpoint: endpoint},
		inventory: func() (Inventory, error) {
			return h.Inventory.Inventory(ctx, InventoryRequest{Host: request.Host, Credentials: request.Credentials, Timeout: request.Timeout})
		},
		profiles: func() (MediaProfilesResult, error) {
			return h.Media.Profiles(ctx, MediaProfilesRequest(request))
		},
		imaging: func(token string) (ImagingView, error) {
			return h.Imaging.ReadImaging(ctx, EncoderRequest{Host: request.Host, Credentials: request.Credentials, ProfileToken: token, Timeout: request.Timeout})
		},
		time:    func() (TimeView, error) { return h.Time.ReadTime(ctx, request) },
		network: func() (NetworkView, error) { return h.Network.ReadNetwork(ctx, request) },
		users:   func() (UsersView, error) { return h.Users.ReadUsers(ctx, request) },
	}
	return buildReport(sources, options)
}

// ClockCheck measures an endpoint's clock over ONVIF and, unless
// profileToken is "-", from the RTCP Sender Reports of a profile's stream
// (the first usable profile when profileToken is empty).
func (h HostDiagnostics) ClockCheck(ctx context.Context, request DeviceRequest, profileToken string, rtcpWait time.Duration) (ClockCheckResult, error) {
	measurement, err := h.Clock.MeasureClock(ctx, request)
	if err != nil {
		return ClockCheckResult{}, err
	}
	result := ClockCheckResult{
		CheckedAt: time.Now().UTC().Format(time.RFC3339), ToleranceSeconds: ClockToleranceSeconds,
		DateTimeType: measurement.DateTimeType, NTPServers: measurement.NTPServers, ONVIF: measurement.Estimate,
	}
	if result.NTPServers == nil {
		result.NTPServers = []string{}
	}
	if profileToken == "-" {
		result.RTCPUnavailable = "no stream was selected"
	} else {
		result.RTCP, result.RTCPUnavailable = measureRTCP(ctx, rtcpWait, measurement.Estimate, func(ctx context.Context) (stream.LiveSource, error) {
			profiles, err := h.Media.Profiles(ctx, MediaProfilesRequest(request))
			if err != nil {
				return nil, err
			}
			profile, err := probeProfile(profiles, profileToken, StreamTransportTCP)
			if err != nil {
				return nil, err
			}
			options := stream.RTSPOptions{URL: profile.Stream.TCPURI, Transport: stream.TransportTCP, Timeout: request.Timeout}
			if request.Credentials != nil {
				options.Username, options.Password = request.Credentials.username, request.Credentials.password
			}
			return h.Live(ctx, options)
		})
	}
	result.Verdict = clockVerdict(result.ONVIF, result.RTCP)
	return result, nil
}
