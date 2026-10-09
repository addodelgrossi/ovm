package discovery

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// twoInterfaceSource yields two usable interfaces so partial-bind handling can
// be exercised across more than one connection.
func twoInterfaceSource() fakeInterfaceSource {
	return fakeInterfaceSource{
		ifaces: []net.Interface{
			upIface(2, "en0", false),
			upIface(3, "en1", false),
		},
		addrs: map[string][]net.Addr{
			"en0": {ipNet("192.168.1.10")},
			"en1": {ipNet("192.168.1.11")},
		},
	}
}

// TestListenerKeepsHealthyBindingWhenOneInterfaceFails verifies the partial-bind
// policy: when the listen factory fails for one interface but succeeds for
// another, the healthy binding is kept and listening runs on it, rather than
// discarding every binding and aborting. A Hello delivered on the healthy
// connection is observed, proving the listener is live.
func TestListenerKeepsHealthyBindingWhenOneInterfaceFails(t *testing.T) {
	healthy := newScriptedConn(lifecycleXML{local: "Hello", endpoint: "urn:uuid:live"}.build())
	var calls int
	l := NewListener(Options{
		InterfaceSource: twoInterfaceSource(),
		Listen: func(Interface) (PacketConn, error) {
			calls++
			if calls == 1 {
				// First interface fails to bind.
				return nil, &net.OpError{Op: "listen", Err: errPlain{}}
			}
			// Second interface binds successfully.
			return healthy, nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := l.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen returned error despite one healthy binding: %v", err)
	}
	event := mustEvent(t, events)
	if event.EndpointReference != "urn:uuid:live" {
		t.Fatalf("event = %+v, want the Hello from the healthy interface", event)
	}
}

// TestListenerErrorsOnlyWhenNoInterfaceBinds verifies that Listen returns an
// error only when NO interface could be bound, surfacing the concrete bind
// failure rather than a generic one.
func TestListenerErrorsOnlyWhenNoInterfaceBinds(t *testing.T) {
	bindErr := &net.OpError{Op: "listen", Err: errPlain{}}
	l := NewListener(Options{
		InterfaceSource: twoInterfaceSource(),
		Listen: func(Interface) (PacketConn, error) {
			return nil, bindErr
		},
	})
	if _, err := l.Listen(context.Background()); err == nil {
		t.Fatal("expected an error when no interface binds")
	}
}

// TestDiscoverSetupFailureStartsNoReaderLeak verifies that an immediate listen
// failure for the only interface yields a socket diagnostic without starting a
// reader loop (the probe goroutine returns promptly). Combined with the race
// detector run, this guards the setup-failure path against goroutine leaks.
func TestDiscoverSetupFailureStartsNoReaderLeak(t *testing.T) {
	d := NewDiscoverer(Options{
		Timeout:         200 * time.Millisecond,
		InterfaceSource: singleInterfaceSource(),
		Listen: func(Interface) (PacketConn, error) {
			return nil, &net.OpError{Op: "listen", Err: errPlain{}}
		},
	})
	result, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if result.Diagnostic.Category != DiagnosticSocketError {
		t.Fatalf("diagnostic = %q, want socket_error", result.Diagnostic.Category)
	}
}

func TestListenStreamCancellationDuringSetupClosesAllBindings(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	first := newScriptedConn()
	second := newScriptedConn()
	calls := 0
	listener := NewListener(Options{
		InterfaceSource: twoInterfaceSource(),
		Listen: func(Interface) (PacketConn, error) {
			calls++
			if calls == 1 {
				return first, nil
			}
			cancel()
			return second, nil
		},
	})

	stream, err := listener.ListenStream(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ListenStream error = %v, want context.Canceled", err)
	}
	if stream != nil {
		t.Fatal("ListenStream returned a stream after cancellation during setup")
	}
	for name, conn := range map[string]*scriptedConn{"first": first, "second": second} {
		select {
		case <-conn.closed:
		case <-time.After(time.Second):
			t.Fatalf("%s setup connection was not closed", name)
		}
	}
}
