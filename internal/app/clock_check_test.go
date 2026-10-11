package app

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
	"github.com/addodelgrossi/ovm/pkg/stream"
)

func TestMeasureClockReportsSkewWithinUncertainty(t *testing.T) {
	t.Parallel()
	srv := newUnstartedFake(t, func(base string) fakedevice.Config {
		return fakedevice.Config{
			HTTPAuth: fakedevice.HTTPAuthDigest, DigestAlgorithm: "SHA-256", RequireUsernameToken: true,
			Username: "admin", Password: "encoder-secret", ClockSkew: 30 * time.Second,
		}
	})
	request := encoderRequest(t, srv.host)
	measurement, err := (&productionMedia{}).MeasureClock(context.Background(), DeviceRequest{Host: srv.host, Credentials: request.Credentials, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("MeasureClock() error = %v", err)
	}
	estimate := measurement.Estimate
	if estimate == nil || estimate.Source != "onvif" || estimate.UncertaintySeconds < 0.5 || estimate.UncertaintySeconds > 1.5 {
		t.Fatalf("estimate = %+v", estimate)
	}
	if math.Abs(estimate.OffsetSeconds-30) > estimate.UncertaintySeconds {
		t.Fatalf("offset %.3f ± %.3f does not contain the 30 s skew", estimate.OffsetSeconds, estimate.UncertaintySeconds)
	}
	if clockVerdict(estimate) != ClockOutOfSync {
		t.Fatalf("verdict = %q", clockVerdict(estimate))
	}
}

func TestRTCPEstimateBracketsTheOneWayDelay(t *testing.T) {
	t.Parallel()
	received := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	sample := stream.SenderClock{CameraTime: received.Add(-time.Second), ReceivedAt: received}
	estimate := rtcpEstimate(sample, 200*time.Millisecond)
	if math.Abs(estimate.OffsetSeconds-(-0.9)) > 1e-9 || math.Abs(estimate.UncertaintySeconds-0.1) > 1e-9 {
		t.Fatalf("estimate = %+v", estimate)
	}
	if floor := rtcpEstimate(sample, 0); floor.UncertaintySeconds != minimumOneWayDelay.Seconds()/2 {
		t.Fatalf("minimum uncertainty = %v", floor.UncertaintySeconds)
	}
}

func TestClockVerdictIsHonestAboutUncertainty(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		estimates []*ClockEstimate
		want      string
	}{
		{"none", nil, ClockUncertain},
		{"in sync", []*ClockEstimate{{OffsetSeconds: 0.4, UncertaintySeconds: 0.6}}, ClockInSync},
		{"out of sync", []*ClockEstimate{{OffsetSeconds: -5, UncertaintySeconds: 0.6}}, ClockOutOfSync},
		{"straddles", []*ClockEstimate{{OffsetSeconds: 1.8, UncertaintySeconds: 0.6}}, ClockUncertain},
		{"most precise wins", []*ClockEstimate{{OffsetSeconds: 1.8, UncertaintySeconds: 0.6}, {OffsetSeconds: 1.2, UncertaintySeconds: 0.05}}, ClockInSync},
	}
	for _, tc := range cases {
		if got := clockVerdict(tc.estimates...); got != tc.want {
			t.Errorf("%s: verdict = %q, want %q", tc.name, got, tc.want)
		}
	}
}
