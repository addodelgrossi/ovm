package stream

import (
	"testing"
	"time"
)

func TestNTPToTime(t *testing.T) {
	t.Parallel()
	want := time.Date(2026, 10, 10, 12, 0, 0, 500_000_000, time.UTC)
	ntp := uint64(want.Unix()+ntpEpochOffset)<<32 | 1<<31 //nolint:gosec // G115: a positive 2026 timestamp
	if got := ntpToTime(ntp); !got.Equal(want) {
		t.Fatalf("ntpToTime() = %v, want %v", got, want)
	}
}

func TestSenderClockKeepsNewestReport(t *testing.T) {
	t.Parallel()
	source := &RTSPSource{}
	if _, ok := source.SenderClock(); ok {
		t.Fatal("SenderClock() reported a sample before any report")
	}
	received := time.Date(2026, 10, 10, 12, 0, 1, 0, time.UTC)
	source.recordSenderReport(0, received)
	if _, ok := source.SenderClock(); ok {
		t.Fatal("a zero NTP timestamp was recorded")
	}
	ntp := uint64(time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC).Unix()+ntpEpochOffset) << 32 //nolint:gosec // G115: a positive 2026 timestamp
	source.recordSenderReport(ntp, received)
	sample, ok := source.SenderClock()
	if !ok || sample.ReceivedAt != received || sample.CameraTime.Sub(received) != -time.Second {
		t.Fatalf("SenderClock() = %+v, %v", sample, ok)
	}
}
