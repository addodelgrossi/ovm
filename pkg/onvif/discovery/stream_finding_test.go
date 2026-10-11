package discovery

import (
	"context"
	"testing"
	"time"
)

// byeEnvelope builds a 2009/01 Bye datagram mirroring helloEnvelope so ordering
// coverage can interleave Hello and Bye for one endpoint.
func byeEnvelope(endpoint, messageID, appSeqAttrs string) []byte {
	var b []byte
	add := func(s string) { b = append(b, s...) }
	add(`<s:Envelope xmlns:s="` + nsSOAPEnvelope12 + `"`)
	add(` xmlns:a="` + nsAddressing2005 + `"`)
	add(` xmlns:d="` + nsDiscovery2009 + `">`)
	add(`<s:Header>`)
	add(`<a:Action>` + discoveryAction(nsDiscovery2009, actionBye) + `</a:Action>`)
	if messageID != "" {
		add(`<a:MessageID>` + messageID + `</a:MessageID>`)
	}
	if appSeqAttrs != "" {
		add(`<d:AppSequence ` + appSeqAttrs + `/>`)
	}
	add(`</s:Header>`)
	add(`<s:Body><d:Bye>`)
	add(`<a:EndpointReference><a:Address>` + endpoint + `</a:Address></a:EndpointReference>`)
	add(`</d:Bye></s:Body></s:Envelope>`)
	return b
}

// collectEvents drains events until the channel closes or a short idle elapses,
// returning the ordered (type, endpoint) pairs observed.
func collectEvents(t *testing.T, events <-chan Event, idle time.Duration) []Event {
	t.Helper()
	var out []Event
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-time.After(idle):
			return out
		}
	}
}

// TestListenerHelloByeOrderingPreserved asserts finding 7: a Hello followed by a
// Bye for the same endpoint, with increasing AppSequence MessageNumbers, is
// delivered in order and both are observable.
func TestListenerHelloByeOrderingPreserved(t *testing.T) {
	conn := newScriptedConn(
		helloEnvelope("urn:uuid:cam", "urn:uuid:h1", `InstanceId="1" MessageNumber="1"`),
		byeEnvelope("urn:uuid:cam", "urn:uuid:b1", `InstanceId="1" MessageNumber="2"`),
	)
	l := NewListener(Options{
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := mustListen(t, l, ctx)

	got := collectEvents(t, events, 500*time.Millisecond)
	if len(got) != 2 {
		t.Fatalf("events = %d, want 2 (Hello then Bye)", len(got))
	}
	if got[0].Type != EventHello || got[1].Type != EventBye {
		t.Fatalf("ordering = %v/%v, want Hello then Bye", got[0].Type, got[1].Type)
	}
}

// TestListenerSameMessageIDAcrossInterfacesEmitsOncePerInterface asserts finding
// 7: the same WS-Addressing MessageID observed independently on two interfaces
// is NOT collapsed across interfaces; each interface emits it once (two events
// total), each attributed to its own interface. A within-interface repeat is
// still deduped.
func TestListenerSameMessageIDAcrossInterfacesEmitsOncePerInterface(t *testing.T) {
	connEn0 := newScriptedConn(
		helloEnvelope("urn:uuid:cam", "urn:uuid:shared", ""),
		helloEnvelope("urn:uuid:cam", "urn:uuid:shared", ""), // same iface repeat: deduped
	)
	connEn1 := newScriptedConn(
		helloEnvelope("urn:uuid:cam", "urn:uuid:shared", ""), // same id, other iface: emitted
	)
	l := NewListener(Options{
		InterfaceSource: twoInterfaceSource(),
		Listen: func(iface Interface) (PacketConn, error) {
			if iface.Name == "en0" {
				return connEn0, nil
			}
			return connEn1, nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := mustListen(t, l, ctx)

	got := collectEvents(t, events, 600*time.Millisecond)
	if len(got) != 2 {
		t.Fatalf("events = %d, want 2 (same MessageID emitted once per interface)", len(got))
	}
	ifaces := map[string]int{}
	for _, ev := range got {
		ifaces[ev.Interface]++
	}
	if ifaces["en0"] != 1 || ifaces["en1"] != 1 {
		t.Fatalf("per-interface counts = %v, want one on each of en0 and en1", ifaces)
	}
}

// TestListenerLowerInstanceWithNewSequenceSuppressed asserts finding 7: once a
// higher InstanceId has been accepted, a message from a LOWER instance is stale
// and suppressed EVEN when it introduces a brand-new SequenceId. InstanceId
// ordering dominates across SequenceId.
func TestListenerLowerInstanceWithNewSequenceSuppressed(t *testing.T) {
	conn := newScriptedConn(
		// instance 5, seq A, number 1: accepted.
		helloEnvelope("urn:uuid:cam", "urn:uuid:a1", `InstanceId="5" SequenceId="A" MessageNumber="1"`),
		// instance 3 (lower), seq B (new): stale by instance, suppressed.
		helloEnvelope("urn:uuid:cam", "urn:uuid:b1", `InstanceId="3" SequenceId="B" MessageNumber="1"`),
	)
	l := NewListener(Options{
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := mustListen(t, l, ctx)

	got := collectEvents(t, events, 500*time.Millisecond)
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1 (lower instance with new SequenceId suppressed)", len(got))
	}
}

// TestListenerHigherInstanceResetsSequenceState asserts finding 7: a strictly
// greater InstanceId resets per-SequenceId message-number state, so a
// SequenceId whose number had advanced under the old instance is accepted again
// at a low number under the new instance.
func TestListenerHigherInstanceResetsSequenceState(t *testing.T) {
	conn := newScriptedConn(
		// instance 1, seq A, number 9: accepted.
		helloEnvelope("urn:uuid:cam", "urn:uuid:a9", `InstanceId="1" SequenceId="A" MessageNumber="9"`),
		// instance 2, seq A, number 1: new boot resets state, accepted despite
		// the lower number under the same SequenceId.
		helloEnvelope("urn:uuid:cam", "urn:uuid:a1", `InstanceId="2" SequenceId="A" MessageNumber="1"`),
	)
	l := NewListener(Options{
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := mustListen(t, l, ctx)

	got := collectEvents(t, events, 500*time.Millisecond)
	if len(got) != 2 {
		t.Fatalf("events = %d, want 2 (higher instance resets sequence state)", len(got))
	}
}

// TestListenerIndependentSequenceIdsAtSameInstance asserts finding 7: at the
// same InstanceId, distinct SequenceIds are independent and each requires its
// own increasing MessageNumber.
func TestListenerIndependentSequenceIdsAtSameInstance(t *testing.T) {
	conn := newScriptedConn(
		helloEnvelope("urn:uuid:cam", "urn:uuid:a1", `InstanceId="1" SequenceId="A" MessageNumber="1"`),
		helloEnvelope("urn:uuid:cam", "urn:uuid:b1", `InstanceId="1" SequenceId="B" MessageNumber="1"`),  // other seq, independent: accepted
		helloEnvelope("urn:uuid:cam", "urn:uuid:a1b", `InstanceId="1" SequenceId="A" MessageNumber="1"`), // seq A repeat number: suppressed
		helloEnvelope("urn:uuid:cam", "urn:uuid:a2", `InstanceId="1" SequenceId="A" MessageNumber="2"`),  // seq A advance: accepted
	)
	l := NewListener(Options{
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := mustListen(t, l, ctx)

	got := collectEvents(t, events, 500*time.Millisecond)
	if len(got) != 3 {
		t.Fatalf("events = %d, want 3 (A#1, B#1, A#2 accepted; A#1 repeat suppressed)", len(got))
	}
}

// TestListenStreamClosesWithoutErrorDrain asserts finding 8: a caller that
// reads ONLY Events (never draining Errors) still sees stream termination.
// A terminal read error is buffered rather than wedging the aggregator, so the
// Events channel closes cleanly and the buffered error remains observable
// afterwards.
func TestListenStreamClosesWithoutErrorDrain(t *testing.T) {
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

	// Read ONLY events; never touch Errors until the events channel closes.
	select {
	case _, ok := <-stream.Events():
		if ok {
			t.Fatal("unexpected event from a persistently failing socket")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("events channel did not close; terminal error wedged the stream")
	}

	// The terminal error is still observable from the buffer after Events
	// closed, proving it was not dropped to achieve a clean shutdown.
	select {
	case serr, ok := <-stream.Errors():
		if !ok {
			t.Fatal("errors channel closed without surfacing the terminal error")
		}
		if !serr.Terminal {
			t.Fatalf("surfaced error should be terminal: %+v", serr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("terminal error was not observable after Events closed")
	}
}
