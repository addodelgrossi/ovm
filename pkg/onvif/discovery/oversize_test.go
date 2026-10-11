package discovery

import (
	"context"
	"testing"
	"time"
)

// oversizeDatagram returns a syntactically irrelevant datagram one byte larger
// than the accepted maximum. The reader must reject it on length before any
// parse attempt, so its contents do not matter.
func oversizeDatagram() []byte {
	return make([]byte, maxDatagramSize+1)
}

// TestDiscoverRejectsOversizedDatagram verifies the active reader drops a
// datagram larger than maxDatagramSize instead of parsing a max-sized prefix,
// then continues reading and still collects a following valid, correlated
// match.
func TestDiscoverRejectsOversizedDatagram(t *testing.T) {
	probeID := seededProbeID(5)
	conn := newScriptedConn(
		oversizeDatagram(),
		probeMatchXML{endpoint: "urn:uuid:dev-ok", xaddrs: "http://192.0.2.1/", relatesTo: probeID}.build(),
	)
	d := NewDiscoverer(Options{
		Timeout:         200 * time.Millisecond,
		InterfaceSource: singleInterfaceSource(),
		Randomness:      seededRandomness(5),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})

	result, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.Devices) != 1 || result.Devices[0].EndpointReference != "urn:uuid:dev-ok" {
		t.Fatalf("devices = %+v, want only urn:uuid:dev-ok", result.Devices)
	}
}

// TestListenerRejectsOversizedDatagram verifies the passive reader drops an
// oversized datagram and still delivers a following valid lifecycle event.
func TestListenerRejectsOversizedDatagram(t *testing.T) {
	conn := newScriptedConn(
		oversizeDatagram(),
		lifecycleXML{local: "Hello", endpoint: "urn:uuid:dev-ok"}.build(),
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

	event := mustEvent(t, events)
	if event.EndpointReference != "urn:uuid:dev-ok" {
		t.Fatalf("event = %+v, want dev-ok after oversized datagram dropped", event)
	}
}
