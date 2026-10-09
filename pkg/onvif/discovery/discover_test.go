package discovery

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func singleInterfaceSource() fakeInterfaceSource {
	return fakeInterfaceSource{
		ifaces: []net.Interface{upIface(2, "en0", false)},
		addrs:  map[string][]net.Addr{"en0": {ipNet("192.168.1.10")}},
	}
}

func TestDiscoverAggregatesResponsesAndSendsProbe(t *testing.T) {
	// Correlate replies to the exact probe this round sends: seed the
	// randomness so the MessageID is deterministic and set each reply's
	// RelatesTo to it.
	probeID := seededProbeID(1)
	conn := newScriptedConn(
		probeMatchXML{endpoint: "urn:uuid:dev-a", xaddrs: "http://192.0.2.1/onvif/device_service", relatesTo: probeID}.build(),
		probeMatchXML{endpoint: "urn:uuid:dev-a", xaddrs: "http://192.0.2.2/onvif/device_service", relatesTo: probeID}.build(),
		probeMatchXML{endpoint: "urn:uuid:dev-b", xaddrs: "http://192.0.2.3/onvif/device_service", relatesTo: probeID}.build(),
	)
	d := NewDiscoverer(Options{
		Timeout:         200 * time.Millisecond,
		InterfaceSource: singleInterfaceSource(),
		Randomness:      seededRandomness(1),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})

	result, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.Devices) != 2 {
		t.Fatalf("devices = %d, want 2 (%+v)", len(result.Devices), result.Devices)
	}
	if result.Diagnostic.Category != DiagnosticNone {
		t.Fatalf("diagnostic = %q, want none", result.Diagnostic.Category)
	}
	// dev-a merged two XAddrs.
	devA := result.Devices[0]
	if devA.EndpointReference != "urn:uuid:dev-a" || len(devA.XAddrs) != 2 {
		t.Fatalf("dev-a = %+v, want merged XAddrs", devA)
	}

	// Separate probes advertise the current Device and legacy NVT types. They
	// must not be combined because type matching within one Probe is conjunctive.
	writes := conn.writtenDatagrams()
	if len(writes) != len(probeTypes) {
		t.Fatalf("probe writes = %d, want %d", len(writes), len(probeTypes))
	}
	for _, expectedType := range probeTypes {
		found := false
		for _, write := range writes {
			if strings.Contains(string(write), "<wsd:Types>"+expectedType+"</wsd:Types>") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("no separate probe advertised %q: %q", expectedType, writes)
		}
	}
	messageIDs := make(map[string]struct{}, len(writes))
	for _, write := range writes {
		text := string(write)
		start := strings.Index(text, "<wsa:MessageID>")
		end := strings.Index(text, "</wsa:MessageID>")
		if start < 0 || end <= start {
			t.Fatalf("probe has no MessageID: %s", text)
		}
		messageID := text[start+len("<wsa:MessageID>") : end]
		messageIDs[messageID] = struct{}{}
	}
	if len(messageIDs) != len(writes) {
		t.Fatalf("probe MessageIDs were not distinct: %v", messageIDs)
	}
}

// TestDiscoverIgnoresUncorrelatedReplies verifies that replies which do not
// correlate to the probe this round sent are ignored: a stale RelatesTo, a
// missing RelatesTo, a wrong (non-ProbeMatches) action, and an unsolicited
// Hello all fail correlation, while the one correctly correlated reply is
// aggregated.
func TestDiscoverIgnoresUncorrelatedReplies(t *testing.T) {
	probeID := seededProbeID(7)
	conn := newScriptedConn(
		// Stale: correlates to a different probe id.
		probeMatchXML{endpoint: "urn:uuid:stale", relatesTo: "urn:uuid:some-old-probe"}.build(),
		// Missing RelatesTo entirely (empty value never equals a real id).
		probeMatchXML{endpoint: "urn:uuid:no-relates", relatesTo: " "}.build(),
		// Right correlation but a wrong action (Hello action on ProbeMatches).
		probeMatchXML{endpoint: "urn:uuid:wrong-action", relatesTo: probeID,
			action: discoveryAction(nsDiscovery2009, actionHello)}.build(),
		// Unsolicited Hello during active discovery is not a probe answer.
		lifecycleXML{local: "Hello", endpoint: "urn:uuid:hello"}.build(),
		// The only correctly correlated reply.
		probeMatchXML{endpoint: "urn:uuid:good", relatesTo: probeID}.build(),
	)
	d := NewDiscoverer(Options{
		Timeout:         200 * time.Millisecond,
		InterfaceSource: singleInterfaceSource(),
		Randomness:      seededRandomness(7),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})

	result, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.Devices) != 1 || result.Devices[0].EndpointReference != "urn:uuid:good" {
		t.Fatalf("devices = %+v, want only urn:uuid:good", result.Devices)
	}
}

// TestDiscoverAcceptsCorrelatedReplyFromEitherFamily verifies that a reply is
// accepted whether its ProbeMatches action is in the 2005/04 or 2009/01
// discovery family, as long as it correlates to the sent probe.
func TestDiscoverAcceptsCorrelatedReplyFromEitherFamily(t *testing.T) {
	probeID := seededProbeID(3)
	conn := newScriptedConn(
		probeMatchXML{endpoint: "urn:uuid:family-2009", relatesTo: probeID,
			wsdNS: nsDiscovery2009, wsaNS: nsAddressing2005}.build(),
		probeMatchXML{endpoint: "urn:uuid:family-2005", relatesTo: probeID,
			wsdNS: nsDiscovery2005, wsaNS: nsAddressing2004}.build(),
	)
	d := NewDiscoverer(Options{
		Timeout:         200 * time.Millisecond,
		InterfaceSource: singleInterfaceSource(),
		Randomness:      seededRandomness(3),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})

	result, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.Devices) != 2 {
		t.Fatalf("devices = %d, want 2 across both families (%+v)", len(result.Devices), result.Devices)
	}
}

// TestDiscoverAggregatesAllMatchesInOneDatagram verifies that every ProbeMatch
// child of a single correlated ProbeMatches datagram is collected, not only the
// first.
func TestDiscoverAggregatesAllMatchesInOneDatagram(t *testing.T) {
	probeID := seededProbeID(2)
	datagram := multiProbeMatchXML{
		relatesTo: probeID,
		matches: []probeMatchXML{
			{endpoint: "urn:uuid:dev-a", xaddrs: "http://192.0.2.1/onvif/device_service"},
			{endpoint: "urn:uuid:dev-b", xaddrs: "http://192.0.2.2/onvif/device_service"},
			{endpoint: "urn:uuid:dev-c", xaddrs: "http://192.0.2.3/onvif/device_service"},
		},
	}.build()
	conn := newScriptedConn(datagram)
	d := NewDiscoverer(Options{
		Timeout:         200 * time.Millisecond,
		InterfaceSource: singleInterfaceSource(),
		Randomness:      seededRandomness(2),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})

	result, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.Devices) != 3 {
		t.Fatalf("devices = %d, want 3 from one datagram (%+v)", len(result.Devices), result.Devices)
	}
}

func TestDiscoverTimeoutOnlyReportsNoResponse(t *testing.T) {
	conn := newScriptedConn() // no inbound datagrams
	d := NewDiscoverer(Options{
		Timeout:         50 * time.Millisecond,
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})

	result, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.Devices) != 0 {
		t.Fatalf("devices = %d, want 0", len(result.Devices))
	}
	if result.Diagnostic.Category != DiagnosticNoResponse {
		t.Fatalf("diagnostic = %q, want no_response (never permission)", result.Diagnostic.Category)
	}
}

func TestDiscoverSocketErrorDiagnostic(t *testing.T) {
	d := NewDiscoverer(Options{
		Timeout:         50 * time.Millisecond,
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

func TestDiscoverPermissionErrorDiagnostic(t *testing.T) {
	d := NewDiscoverer(Options{
		Timeout:         50 * time.Millisecond,
		InterfaceSource: singleInterfaceSource(),
		Listen: func(Interface) (PacketConn, error) {
			return nil, errPermission
		},
	})
	result, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if result.Diagnostic.Category != DiagnosticPossiblePermissionRestriction {
		t.Fatalf("diagnostic = %q, want possible_permission_restriction", result.Diagnostic.Category)
	}
}

// TestDiscoverSocketFailureStopsEarly verifies the monitor's early-stop: when
// the only socket fails to bind immediately, the round ends well before the
// configured timeout rather than idling until it elapses.
func TestDiscoverSocketFailureStopsEarly(t *testing.T) {
	d := NewDiscoverer(Options{
		Timeout:         5 * time.Second, // long; early-stop must win
		InterfaceSource: singleInterfaceSource(),
		Listen: func(Interface) (PacketConn, error) {
			return nil, &net.OpError{Op: "listen", Err: errPlain{}}
		},
	})
	start := time.Now()
	result, err := d.Discover(context.Background())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if result.Diagnostic.Category != DiagnosticSocketError {
		t.Fatalf("diagnostic = %q, want socket_error", result.Diagnostic.Category)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("all-socket failure waited the full timeout (%v)", elapsed)
	}
}

// TestDiscoverReturnsCallerContextErrorOnCancellation verifies that a caller
// cancellation is surfaced as the caller context's error and is never
// mislabelled as the no_response diagnostic.
func TestDiscoverReturnsCallerContextErrorOnCancellation(t *testing.T) {
	conn := newScriptedConn() // would otherwise block until deadline
	d := NewDiscoverer(Options{
		Timeout:         5 * time.Second, // long; cancellation must win
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	result, err := d.Discover(ctx)
	elapsed := time.Since(start)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("cancellation ignored, took %v", elapsed)
	}
	// Cancellation must not be reported as a no_response diagnostic.
	if result.Diagnostic.Category == DiagnosticNoResponse {
		t.Fatalf("cancellation was mislabelled as no_response")
	}

	// The connection must have been closed on cancellation.
	select {
	case <-conn.closed:
	default:
		t.Fatal("connection was not closed on cancellation")
	}
}

// TestDiscoverReturnsDeadlineExceededWhenCallerDeadlineExpires verifies that a
// deadline carried by the caller's own context is surfaced as
// context.DeadlineExceeded rather than the no_response diagnostic.
func TestDiscoverReturnsDeadlineExceededWhenCallerDeadlineExpires(t *testing.T) {
	conn := newScriptedConn()
	d := NewDiscoverer(Options{
		Timeout:         5 * time.Second, // longer than the caller deadline
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	result, err := d.Discover(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if result.Diagnostic.Category == DiagnosticNoResponse {
		t.Fatalf("caller deadline was mislabelled as no_response")
	}
}

// TestDiscoverOrdinaryTimeoutReturnsNoResponseWithoutError verifies that the
// Discoverer's own configured timeout, with a caller context that is never
// cancelled, returns a nil error and the no_response diagnostic — the opposite
// of the caller-cancellation case above.
func TestDiscoverOrdinaryTimeoutReturnsNoResponseWithoutError(t *testing.T) {
	conn := newScriptedConn()
	d := NewDiscoverer(Options{
		Timeout:         40 * time.Millisecond, // the only clock that fires
		InterfaceSource: singleInterfaceSource(),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})

	result, err := d.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if result.Diagnostic.Category != DiagnosticNoResponse {
		t.Fatalf("diagnostic = %q, want no_response on ordinary timeout", result.Diagnostic.Category)
	}
}

func TestDiscoverPropagatesInterfaceErrors(t *testing.T) {
	d := NewDiscoverer(Options{
		InterfaceSource: fakeInterfaceSource{
			ifaces: []net.Interface{upIface(1, "lo0", true)},
			addrs:  map[string][]net.Addr{"lo0": {ipNet("127.0.0.1")}},
		},
	})
	if _, err := d.Discover(context.Background()); err == nil {
		t.Fatal("expected ErrNoUsableInterfaces")
	}
}

// errPlain is a generic (non-permission) error.
type errPlain struct{}

func (errPlain) Error() string { return "connection reset by peer" }
