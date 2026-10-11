package fakedevice_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
	"github.com/addodelgrossi/ovm/pkg/onvif/events"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

func eventsClient(t *testing.T) (*fakedevice.Device, *events.Client) {
	t.Helper()
	device, err := fakedevice.New(fakedevice.Config{})
	if err != nil {
		t.Fatalf("fakedevice.New() error = %v", err)
	}
	server := httptest.NewServer(device)
	t.Cleanup(server.Close)
	transport, err := soap.NewClient(soap.Config{})
	if err != nil {
		t.Fatal(err)
	}
	client, err := events.NewClient(transport, server.URL+"/onvif/events_service")
	if err != nil {
		t.Fatal(err)
	}
	return device, client
}

func motion(state string) fakedevice.Event {
	return fakedevice.Event{
		Topic:  "VideoSource/MotionAlarm",
		Source: []fakedevice.EventItem{{Name: "Source", Value: "vsrc0"}},
		Data:   []fakedevice.EventItem{{Name: "State", Value: state}},
	}
}

func TestFakePullPointDeliversFilteredEvents(t *testing.T) {
	t.Parallel()
	device, client := eventsClient(t)
	ctx := context.Background()
	subscription, err := client.CreatePullPointSubscription(ctx, "tns1:VideoSource//.", time.Minute)
	if err != nil {
		t.Fatalf("CreatePullPointSubscription() error = %v", err)
	}
	device.PublishEvent(fakedevice.Event{Topic: "Device/Trigger/DigitalInput", Data: []fakedevice.EventItem{{Name: "LogicalState", Value: "true"}}})
	device.PublishEvent(motion("true"))
	device.PublishEvent(motion("false"))

	result, err := client.PullMessages(ctx, subscription, time.Second, 10)
	if err != nil {
		t.Fatalf("PullMessages() error = %v", err)
	}
	if len(result.Messages) != 2 || result.Messages[0].Topic != "VideoSource/MotionAlarm" ||
		result.Messages[0].Data[0].Value != "true" || result.Messages[1].Data[0].Value != "false" {
		t.Fatalf("messages = %+v", result.Messages)
	}

	// An empty queue waits for the timeout and returns no messages.
	started := time.Now()
	empty, err := client.PullMessages(ctx, subscription, time.Second, 10)
	if err != nil || len(empty.Messages) != 0 || time.Since(started) < 900*time.Millisecond {
		t.Fatalf("empty pull = %+v, %v after %v", empty, err, time.Since(started))
	}

	// A waiting pull returns as soon as an event arrives.
	go func() {
		time.Sleep(100 * time.Millisecond)
		device.PublishEvent(motion("true"))
	}()
	started = time.Now()
	woken, err := client.PullMessages(ctx, subscription, 5*time.Second, 10)
	if err != nil || len(woken.Messages) != 1 || time.Since(started) > 3*time.Second {
		t.Fatalf("woken pull = %+v, %v after %v", woken, err, time.Since(started))
	}

	if _, err := client.Renew(ctx, subscription, time.Minute); err != nil {
		t.Fatalf("Renew() error = %v", err)
	}
	if device.ActiveSubscriptions() != 1 {
		t.Fatalf("active subscriptions = %d", device.ActiveSubscriptions())
	}
	if err := client.Unsubscribe(ctx, subscription); err != nil {
		t.Fatalf("Unsubscribe() error = %v", err)
	}
	if device.ActiveSubscriptions() != 0 {
		t.Fatalf("active subscriptions after unsubscribe = %d", device.ActiveSubscriptions())
	}
	if _, err := client.PullMessages(ctx, subscription, time.Second, 10); !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("pull after unsubscribe error = %v", err)
	}
}

func TestFakePullPointWithoutFilterReceivesEverything(t *testing.T) {
	t.Parallel()
	device, client := eventsClient(t)
	subscription, err := client.CreatePullPointSubscription(context.Background(), "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	device.PublishEvent(fakedevice.Event{Topic: "Device/Trigger/DigitalInput"})
	device.PublishEvent(motion("true"))
	result, err := client.PullMessages(context.Background(), subscription, time.Second, 1)
	if err != nil || len(result.Messages) != 1 || result.Messages[0].Topic != "Device/Trigger/DigitalInput" {
		t.Fatalf("first pull = %+v, %v", result, err)
	}
}
