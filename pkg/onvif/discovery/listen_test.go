package discovery

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestListenerDeliversHelloAndByeEvents(t *testing.T) {
	conn := newScriptedConn(
		lifecycleXML{local: "Hello", endpoint: "urn:uuid:dev-1", xaddrs: "http://192.0.2.1/onvif/device_service"}.build(),
		lifecycleXML{local: "Bye", endpoint: "urn:uuid:dev-1"}.build(),
	)
	l := NewListener(Options{
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := l.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	hello := mustEvent(t, events)
	if hello.Type != EventHello || hello.EndpointReference != "urn:uuid:dev-1" {
		t.Fatalf("hello = %+v", hello)
	}
	if hello.Interface != "en0" {
		t.Fatalf("hello interface = %q, want en0", hello.Interface)
	}
	if len(hello.XAddrs) != 1 {
		t.Fatalf("hello xaddrs = %v", hello.XAddrs)
	}

	bye := mustEvent(t, events)
	if bye.Type != EventBye || bye.EndpointReference != "urn:uuid:dev-1" {
		t.Fatalf("bye = %+v", bye)
	}
}

func TestListenerStopsAndClosesConnectionsOnCancellation(t *testing.T) {
	conn := newScriptedConn() // blocks waiting for datagrams
	l := NewListener(Options{
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})

	ctx, cancel := context.WithCancel(context.Background())
	events, err := l.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	cancel()

	// The channel must close promptly once listening stops.
	select {
	case _, ok := <-events:
		if ok {
			// Drain any late event then expect close.
			select {
			case _, ok := <-events:
				if ok {
					t.Fatal("channel delivered after cancellation and did not close")
				}
			case <-time.After(time.Second):
				t.Fatal("channel did not close after cancellation")
			}
		}
	case <-time.After(time.Second):
		t.Fatal("channel did not close after cancellation")
	}

	select {
	case <-conn.closed:
	default:
		t.Fatal("connection was not closed on cancellation")
	}
}

func TestListenerIgnoresMalformedDatagrams(t *testing.T) {
	conn := newScriptedConn(
		[]byte("garbage"),
		lifecycleXML{local: "Hello", endpoint: "urn:uuid:dev-7"}.build(),
	)
	l := NewListener(Options{
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := l.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	// The malformed datagram is skipped; the first delivered event is the Hello.
	event := mustEvent(t, events)
	if event.EndpointReference != "urn:uuid:dev-7" {
		t.Fatalf("event = %+v, want dev-7", event)
	}
}

func TestListenerPropagatesListenFactoryError(t *testing.T) {
	l := NewListener(Options{
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return nil, &net.OpError{Op: "listen", Err: errPlain{}} },
	})
	if _, err := l.Listen(context.Background()); err == nil {
		t.Fatal("expected error from listen factory")
	}
}

func mustEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("event channel closed before delivering an event")
		}
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}
