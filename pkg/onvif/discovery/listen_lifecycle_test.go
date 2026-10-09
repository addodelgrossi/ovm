package discovery

import (
	"context"
	"testing"
	"time"
)

// TestListenerAcceptsLifecycleWithMatchingAction verifies that a Hello whose
// WS-Addressing Action matches its body (and belongs to a recognised discovery
// family) is delivered.
func TestListenerAcceptsLifecycleWithMatchingAction(t *testing.T) {
	for _, ns := range []string{nsDiscovery2005, nsDiscovery2009} {
		conn := newScriptedConn(
			lifecycleXML{local: "Hello", endpoint: "urn:uuid:ok", wsdNS: ns,
				action: discoveryAction(ns, actionHello)}.build(),
		)
		l := NewListener(Options{
			InterfaceSource: singleInterfaceSource(),
			Listen:          func(Interface) (PacketConn, error) { return conn, nil },
		})
		ctx, cancel := context.WithCancel(context.Background())
		event := mustEvent(t, mustListen(t, l, ctx))
		cancel()
		if event.Type != EventHello || event.EndpointReference != "urn:uuid:ok" {
			t.Fatalf("ns=%s event = %+v, want Hello urn:uuid:ok", ns, event)
		}
	}
}

// TestListenerToleratesAbsentLifecycleAction verifies the documented vendor
// tolerance: a Hello that omits the Action header entirely is still delivered,
// because the body element already identifies the notification.
func TestListenerToleratesAbsentLifecycleAction(t *testing.T) {
	conn := newScriptedConn(
		lifecycleXML{local: "Hello", endpoint: "urn:uuid:noaction", action: omitAction}.build(),
	)
	l := NewListener(Options{
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	event := mustEvent(t, mustListen(t, l, ctx))
	if event.Type != EventHello || event.EndpointReference != "urn:uuid:noaction" {
		t.Fatalf("event = %+v, want tolerated Hello without action", event)
	}
}

// TestListenerRejectsContradictoryLifecycleAction verifies that a Hello body
// carrying a Bye action (contradictory) is rejected: the contradictory
// datagram is dropped and the following coherent Bye is the first event
// delivered.
func TestListenerRejectsContradictoryLifecycleAction(t *testing.T) {
	conn := newScriptedConn(
		// Hello body but Bye action: contradictory, must be dropped.
		lifecycleXML{local: "Hello", endpoint: "urn:uuid:contradiction",
			action: discoveryAction(nsDiscovery2009, actionBye)}.build(),
		// A coherent Bye that should be the first delivered event.
		lifecycleXML{local: "Bye", endpoint: "urn:uuid:coherent"}.build(),
	)
	l := NewListener(Options{
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	event := mustEvent(t, mustListen(t, l, ctx))
	if event.Type != EventBye || event.EndpointReference != "urn:uuid:coherent" {
		t.Fatalf("event = %+v, want the coherent Bye (contradictory Hello dropped)", event)
	}
}

// TestListenerRejectsUnrecognisedLifecycleAction verifies that a lifecycle
// datagram whose action is from a foreign namespace is rejected even though its
// body is a valid Hello.
func TestListenerRejectsUnrecognisedLifecycleAction(t *testing.T) {
	conn := newScriptedConn(
		lifecycleXML{local: "Hello", endpoint: "urn:uuid:foreign",
			action: "urn:evil:discovery/Hello"}.build(),
		lifecycleXML{local: "Hello", endpoint: "urn:uuid:good"}.build(),
	)
	l := NewListener(Options{
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	event := mustEvent(t, mustListen(t, l, ctx))
	if event.EndpointReference != "urn:uuid:good" {
		t.Fatalf("event = %+v, want urn:uuid:good (foreign-action Hello dropped)", event)
	}
}

// TestListenerReaderTerminatesOnPersistentReadError verifies the no-hot-loop
// guarantee: a connection that returns a persistent non-timeout, non-closed
// read error does not spin forever. The reader gives up after a bounded number
// of attempts, so the event channel closes on its own without cancellation and
// the read count stays bounded.
func TestListenerReaderTerminatesOnPersistentReadError(t *testing.T) {
	conn := newScriptedConn()
	conn.readErr = errPlain{} // every ReadFrom fails with a non-timeout error

	l := NewListener(Options{
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})

	// A context that is NOT cancelled: termination must come from the bounded
	// retry, not from cancellation.
	events, err := l.Listen(context.Background())
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("unexpected event from a persistently failing socket")
		}
		// Channel closed: the reader gave up as required.
	case <-time.After(2 * time.Second):
		t.Fatal("reader did not terminate on persistent read error (hot loop)")
	}

	if got := conn.reads(); got > maxConsecutiveReadErrors+1 {
		t.Fatalf("read attempts = %d, want bounded by %d", got, maxConsecutiveReadErrors+1)
	}
}

// mustListen starts listening and fails the test on error.
func mustListen(t *testing.T, l *Listener, ctx context.Context) <-chan Event {
	t.Helper()
	events, err := l.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	return events
}
