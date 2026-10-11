package app

import (
	"context"
	"math"
	"time"

	"github.com/addodelgrossi/ovm/pkg/stream"
)

const (
	// ClockToleranceSeconds is the offset the clock check accepts as in sync.
	ClockToleranceSeconds = 2.0
	// clockSamples is how many ONVIF clock reads the check takes; the one with
	// the shortest round trip wins.
	clockSamples = 3
	// DefaultRTCPWait is how long the check waits for an RTCP Sender Report.
	// Many cameras send one every 5 to 10 seconds.
	DefaultRTCPWait = 12 * time.Second
	// minimumOneWayDelay bounds RTCP uncertainty when no round trip is known.
	minimumOneWayDelay = 50 * time.Millisecond
)

// Clock check verdicts.
const (
	ClockInSync    = "in_sync"
	ClockOutOfSync = "out_of_sync"
	// ClockUncertain means the measurement cannot tell: the uncertainty
	// interval straddles the tolerance.
	ClockUncertain = "uncertain"
)

// ClockEstimate is one measurement of the camera clock minus this computer's
// clock. The true offset lies within OffsetSeconds ± UncertaintySeconds.
type ClockEstimate struct {
	Source             string  `json:"source"`
	OffsetSeconds      float64 `json:"offset_seconds"`
	UncertaintySeconds float64 `json:"uncertainty_seconds"`
	RoundTripMs        float64 `json:"round_trip_ms,omitempty"`
}

// ClockMeasurement is the ONVIF part of a clock check.
type ClockMeasurement struct {
	Estimate     *ClockEstimate `json:"estimate,omitempty"`
	DateTimeType string         `json:"date_time_type"`
	NTPServers   []string       `json:"ntp_servers"`
}

// ClockCheckResult combines the ONVIF clock read with the RTCP Sender Report
// clock. RTCP is finer grained but only reflects the system clock when the
// camera stamps reports with it, so both are shown.
type ClockCheckResult struct {
	CheckedAt        string         `json:"checked_at"`
	ToleranceSeconds float64        `json:"tolerance_seconds"`
	Verdict          string         `json:"verdict"`
	DateTimeType     string         `json:"date_time_type"`
	NTPServers       []string       `json:"ntp_servers"`
	ONVIF            *ClockEstimate `json:"onvif,omitempty"`
	RTCP             *ClockEstimate `json:"rtcp,omitempty"`
	// RTCPUnavailable explains a missing RTCP estimate.
	RTCPUnavailable string `json:"rtcp_unavailable,omitempty"`
}

// ClockUseCase measures the camera clock over ONVIF.
type ClockUseCase interface {
	MeasureClock(ctx context.Context, request DeviceRequest) (ClockMeasurement, error)
}

// NewProductionClock returns the production ClockUseCase.
func NewProductionClock() ClockUseCase { return &productionMedia{} }

// MeasureClock reads the camera clock several times. ONVIF reports whole
// seconds, so the camera time lies in [reported, reported+1s): the estimate
// takes the middle of that second and adds half the round trip of the best
// sample to the uncertainty.
func (p *productionMedia) MeasureClock(ctx context.Context, request DeviceRequest) (ClockMeasurement, error) {
	seam, opCtx, cancel, err := p.openDevice(ctx, request)
	if err != nil {
		return ClockMeasurement{}, err
	}
	defer cancel()
	view, err := p.readTime(opCtx, seam)
	if err != nil {
		return ClockMeasurement{}, mapConfigError(err)
	}
	measurement := ClockMeasurement{DateTimeType: view.DateTimeType, NTPServers: view.NTPServers}
	for range clockSamples {
		sent := p.now()
		current, err := seam.SystemDateAndTime(opCtx)
		if err != nil {
			return ClockMeasurement{}, mapConfigError(err)
		}
		received := p.now()
		if !current.UTCValid {
			continue
		}
		roundTrip := received.Sub(sent)
		midpoint := sent.Add(roundTrip / 2)
		estimate := ClockEstimate{
			Source:             "onvif",
			OffsetSeconds:      current.UTC.Add(500 * time.Millisecond).Sub(midpoint).Seconds(),
			UncertaintySeconds: 0.5 + roundTrip.Seconds()/2,
			RoundTripMs:        float64(roundTrip.Microseconds()) / 1000,
		}
		if measurement.Estimate == nil || estimate.UncertaintySeconds < measurement.Estimate.UncertaintySeconds {
			measurement.Estimate = &estimate
		}
	}
	return measurement, nil
}

// rtcpEstimate turns a Sender Report sample into an estimate. The report
// travelled for an unknown one-way delay d >= 0, so the true offset lies in
// [measured, measured+d]; d is bounded by the ONVIF round trip when known.
func rtcpEstimate(sample stream.SenderClock, oneWayBound time.Duration) ClockEstimate {
	oneWayBound = max(oneWayBound, minimumOneWayDelay)
	measured := sample.CameraTime.Sub(sample.ReceivedAt)
	return ClockEstimate{
		Source:             "rtcp",
		OffsetSeconds:      (measured + oneWayBound/2).Seconds(),
		UncertaintySeconds: (oneWayBound / 2).Seconds(),
	}
}

// clockVerdict classifies the most precise estimate against the tolerance.
func clockVerdict(estimates ...*ClockEstimate) string {
	var best *ClockEstimate
	for _, estimate := range estimates {
		if estimate != nil && (best == nil || estimate.UncertaintySeconds < best.UncertaintySeconds) {
			best = estimate
		}
	}
	if best == nil {
		return ClockUncertain
	}
	offset := math.Abs(best.OffsetSeconds)
	switch {
	case offset+best.UncertaintySeconds <= ClockToleranceSeconds:
		return ClockInSync
	case offset-best.UncertaintySeconds > ClockToleranceSeconds:
		return ClockOutOfSync
	default:
		return ClockUncertain
	}
}

type senderClockSource interface {
	SenderClock() (stream.SenderClock, bool)
}

// ClockCheck measures the camera clock over ONVIF and, when profileToken is
// set, from the RTCP Sender Reports of that profile's stream.
func (m *Manager) ClockCheck(ctx context.Context, deviceID, profileToken string, rtcpWait time.Duration) (ClockCheckResult, error) {
	measurement, err := withDevice(ctx, m, deviceID, m.clockCheck != nil, "clock check", func(host string, credentials *Credentials) (ClockMeasurement, error) {
		return m.clockCheck.MeasureClock(ctx, newDeviceRequest(host, credentials))
	})
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
	result.RTCP, result.RTCPUnavailable = m.rtcpClock(ctx, deviceID, profileToken, rtcpWait, measurement.Estimate)
	result.Verdict = clockVerdict(result.ONVIF, result.RTCP)
	return result, nil
}

func (m *Manager) rtcpClock(ctx context.Context, deviceID, profileToken string, wait time.Duration, onvif *ClockEstimate) (*ClockEstimate, string) {
	if profileToken == "" || m.liveStreams == nil {
		return nil, "no stream was selected"
	}
	if wait <= 0 {
		wait = DefaultRTCPWait
	}
	source, err := m.OpenLiveStream(ctx, StreamSelection{DeviceID: deviceID, ProfileToken: profileToken, Transport: StreamTransportTCP})
	if err != nil {
		return nil, "the stream could not be opened"
	}
	defer source.Close()
	reporter, ok := source.(senderClockSource)
	if !ok {
		return nil, "the stream does not expose RTCP"
	}
	oneWay := time.Duration(0)
	if onvif != nil {
		oneWay = time.Duration(onvif.RoundTripMs * float64(time.Millisecond))
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	poll := time.NewTicker(100 * time.Millisecond)
	defer poll.Stop()
	for {
		if sample, ok := reporter.SenderClock(); ok {
			estimate := rtcpEstimate(sample, oneWay)
			return &estimate, ""
		}
		select {
		case <-ctx.Done():
			return nil, "the check was cancelled"
		case <-deadline.C:
			return nil, "the camera sent no RTCP Sender Report in time"
		case <-poll.C:
		}
	}
}
