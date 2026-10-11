package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

func eventsFakeHost(t *testing.T, eventsOrigin string) (*fakedevice.Device, string) {
	t.Helper()
	srv := newUnstartedFake(t, func(base string) fakedevice.Config {
		origin := base
		if eventsOrigin != "" {
			origin = eventsOrigin
		}
		return fakedevice.Config{
			HTTPAuth: fakedevice.HTTPAuthDigest, DigestAlgorithm: "SHA-256", RequireUsernameToken: true,
			Username: "admin", Password: "encoder-secret",
			Inventory: fakedevice.Inventory{Services: []fakedevice.ServiceData{
				{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
				{Namespace: eventsWSDLNamespace, XAddr: origin + "/onvif/events_service", VersionMajor: 25, VersionMinor: 6},
			}},
		}
	})
	return srv.device, srv.host
}

func eventsRequest(t *testing.T, host, filter string) EventsRequest {
	t.Helper()
	request := encoderRequest(t, host)
	return EventsRequest{DeviceRequest: DeviceRequest{Host: host, Credentials: request.Credentials, Timeout: 10 * time.Second}, TopicFilter: filter}
}

func TestSubscribeEventsPullsRenewsAndCloses(t *testing.T) {
	t.Parallel()
	device, host := eventsFakeHost(t, "")
	now := time.Now()
	media := &productionMedia{clock: func() time.Time { return now }}
	subscription, err := media.SubscribeEvents(context.Background(), eventsRequest(t, host, "tns1:VideoSource//."))
	if err != nil {
		t.Fatalf("SubscribeEvents() error = %v", err)
	}
	device.PublishEvent(fakedevice.Event{Topic: "VideoSource/MotionAlarm", Data: []fakedevice.EventItem{{Name: "State", Value: "true"}}})

	// Past the renewal point, Pull renews before pulling.
	now = now.Add(eventsRenewAfter + time.Second)
	received, err := subscription.Pull(context.Background())
	if err != nil || len(received) != 1 || received[0].Topic != "VideoSource/MotionAlarm" || received[0].Data[0].Value != "true" {
		t.Fatalf("Pull() = %+v, %v", received, err)
	}
	if got := subscription.(*pullPointSubscription).renewedAt; !got.Equal(now) {
		t.Fatalf("renewedAt = %v, want %v", got, now)
	}
	if err := subscription.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if device.ActiveSubscriptions() != 0 {
		t.Fatalf("subscription still active after Close")
	}
	if err := subscription.Close(context.Background()); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestSubscribeEventsRejectsCrossOriginServiceAndBadFilter(t *testing.T) {
	t.Parallel()
	_, host := eventsFakeHost(t, "http://198.51.100.7")
	_, err := (&productionMedia{}).SubscribeEvents(context.Background(), eventsRequest(t, host, ""))
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Category != CategoryProtocol {
		t.Fatalf("cross-origin error = %v", err)
	}
	_, err = (&productionMedia{}).SubscribeEvents(context.Background(), eventsRequest(t, host, "bad filter<"))
	if !errors.As(err, &appErr) || appErr.Category != CategoryUsage {
		t.Fatalf("bad filter error = %v", err)
	}
}
