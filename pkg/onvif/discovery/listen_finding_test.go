package discovery

import (
	"context"
	"testing"
	"time"
)

// helloEnvelope builds a 2009/01 Hello datagram with explicit MessageID and
// optional AppSequence attributes so finding-4 dedupe and suppression can be
// exercised exactly. appSeqAttrs, when non-empty, is injected as the attributes
// of a d:AppSequence header element.
func helloEnvelope(endpoint, messageID, appSeqAttrs string) []byte {
	var b []byte
	add := func(s string) { b = append(b, s...) }
	add(`<s:Envelope xmlns:s="` + nsSOAPEnvelope12 + `"`)
	add(` xmlns:a="` + nsAddressing2005 + `"`)
	add(` xmlns:d="` + nsDiscovery2009 + `">`)
	add(`<s:Header>`)
	add(`<a:Action>` + discoveryAction(nsDiscovery2009, actionHello) + `</a:Action>`)
	if messageID != "" {
		add(`<a:MessageID>` + messageID + `</a:MessageID>`)
	}
	if appSeqAttrs != "" {
		add(`<d:AppSequence ` + appSeqAttrs + `/>`)
	}
	add(`</s:Header>`)
	add(`<s:Body><d:Hello>`)
	add(`<a:EndpointReference><a:Address>` + endpoint + `</a:Address></a:EndpointReference>`)
	add(`</d:Hello></s:Body></s:Envelope>`)
	return b
}

// drainOne reads the next event or fails after a short deadline.
func drainOne(t *testing.T, events <-chan Event) (Event, bool) {
	t.Helper()
	select {
	case ev, ok := <-events:
		return ev, ok
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an event or channel close")
		return Event{}, false
	}
}

// TestListenerDedupesRepeatedMessageID asserts finding 4: two datagrams sharing
// the same WS-Addressing MessageID are collapsed to a single delivered event.
func TestListenerDedupesRepeatedMessageID(t *testing.T) {
	conn := newScriptedConn(
		helloEnvelope("urn:uuid:dev-1", "urn:uuid:same", ""),
		helloEnvelope("urn:uuid:dev-1", "urn:uuid:same", ""), // duplicate MessageID
		helloEnvelope("urn:uuid:dev-2", "urn:uuid:other", ""),
	)
	l := NewListener(Options{
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := mustListen(t, l, ctx)

	first, _ := drainOne(t, events)
	if first.EndpointReference != "urn:uuid:dev-1" {
		t.Fatalf("first = %+v, want dev-1", first)
	}
	// The duplicate MessageID is suppressed, so the next delivered event is dev-2.
	second, _ := drainOne(t, events)
	if second.EndpointReference != "urn:uuid:dev-2" {
		t.Fatalf("second = %+v, want dev-2 (duplicate MessageID suppressed)", second)
	}
}

// TestListenerSuppressesStaleAppSequence asserts finding 4: a Hello with an
// equal-or-lower AppSequence MessageNumber for the same endpoint, interface and
// instance is suppressed as stale, while a strictly greater MessageNumber and a
// newer InstanceId are both accepted.
func TestListenerSuppressesStaleAppSequence(t *testing.T) {
	conn := newScriptedConn(
		// instance 1, number 5: accepted (first for this source).
		helloEnvelope("urn:uuid:cam", "urn:uuid:m1", `InstanceId="1" MessageNumber="5"`),
		// instance 1, number 5 again: stale/equal, suppressed.
		helloEnvelope("urn:uuid:cam", "urn:uuid:m2", `InstanceId="1" MessageNumber="5"`),
		// instance 1, number 4: stale (lower), suppressed.
		helloEnvelope("urn:uuid:cam", "urn:uuid:m3", `InstanceId="1" MessageNumber="4"`),
		// instance 1, number 6: newer, accepted.
		helloEnvelope("urn:uuid:cam", "urn:uuid:m4", `InstanceId="1" MessageNumber="6"`),
		// instance 2, number 1: new boot, accepted even though number regressed.
		helloEnvelope("urn:uuid:cam", "urn:uuid:m5", `InstanceId="2" MessageNumber="1"`),
	)
	l := NewListener(Options{
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := mustListen(t, l, ctx)

	// Expected delivered MessageIDs are m1 (first), m4 (newer number), m5 (new
	// instance). m2 and m3 are suppressed. We assert by counting accepted
	// events: exactly three should arrive before the stream idles.
	accepted := 0
	done := time.After(500 * time.Millisecond)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				goto check
			}
			if ev.EndpointReference != "urn:uuid:cam" {
				t.Fatalf("unexpected endpoint %q", ev.EndpointReference)
			}
			accepted++
		case <-done:
			goto check
		}
	}
check:
	if accepted != 3 {
		t.Fatalf("accepted = %d, want 3 (m1,m4,m5 accepted; m2,m3 suppressed)", accepted)
	}
}

// TestListenStreamSurfacesPartialBindError asserts finding 4: when one
// interface fails to bind while another succeeds, ListenStream keeps the
// healthy binding AND surfaces the partial bind failure as a non-terminal
// stream error naming the failed interface.
func TestListenStreamSurfacesPartialBindError(t *testing.T) {
	healthy := newScriptedConn(lifecycleXML{local: "Hello", endpoint: "urn:uuid:live"}.build())
	var calls int
	l := NewListener(Options{
		InterfaceSource: twoInterfaceSource(),
		Listen: func(Interface) (PacketConn, error) {
			calls++
			if calls == 1 {
				return nil, errPlain{}
			}
			return healthy, nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := l.ListenStream(ctx)
	if err != nil {
		t.Fatalf("ListenStream: %v", err)
	}

	select {
	case serr := <-stream.Errors():
		if serr.Terminal {
			t.Fatalf("partial bind error should be non-terminal: %+v", serr)
		}
		if serr.Interface == "" {
			t.Fatalf("partial bind error should name the failed interface: %+v", serr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected a partial bind error on the stream")
	}

	// The healthy binding still delivers events.
	ev, ok := drainOne(t, stream.Events())
	if !ok || ev.EndpointReference != "urn:uuid:live" {
		t.Fatalf("event = %+v ok=%v, want the Hello from the healthy interface", ev, ok)
	}
}

// TestListenStreamSurfacesTerminalReadError asserts finding 4: a persistent
// non-timeout read error surfaces as a terminal stream error for the interface
// and the stream then closes cleanly without a hot loop.
func TestListenStreamSurfacesTerminalReadError(t *testing.T) {
	conn := newScriptedConn()
	conn.readErr = errPlain{}
	l := NewListener(Options{
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})
	stream, err := l.ListenStream(context.Background())
	if err != nil {
		t.Fatalf("ListenStream: %v", err)
	}

	select {
	case serr := <-stream.Errors():
		if !serr.Terminal {
			t.Fatalf("persistent read error should be terminal: %+v", serr)
		}
		if serr.Interface != "en0" {
			t.Fatalf("terminal error interface = %q, want en0", serr.Interface)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected a terminal read error on the stream")
	}

	// Events channel closes without delivering anything.
	select {
	case _, ok := <-stream.Events():
		if ok {
			t.Fatal("unexpected event from a persistently failing socket")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("events channel did not close after terminal error")
	}
}
