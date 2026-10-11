package discovery

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParseMessageRequiresBodyEndpointReference(t *testing.T) {
	t.Parallel()

	data := []byte(`<s:Envelope xmlns:s="` + nsSOAPEnvelope12 + `" xmlns:a="` + nsAddressing2005 + `" xmlns:d="` + nsDiscovery2009 + `"><s:Header><a:From><a:EndpointReference><a:Address>urn:uuid:header-only</a:Address></a:EndpointReference></a:From><a:Action>` + discoveryAction(nsDiscovery2009, actionProbeMatches) + `</a:Action><a:RelatesTo>urn:uuid:probe</a:RelatesTo></s:Header><s:Body><d:ProbeMatches><d:ProbeMatch><d:XAddrs>http://device.invalid/onvif/device_service</d:XAddrs></d:ProbeMatch></d:ProbeMatches></s:Body></s:Envelope>`)
	if _, err := parseMessage(data); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("parseMessage() error = %v, want ErrMalformedMessage", err)
	}
}

func TestDiscoverCallerCancellationWinsAfterPartialReply(t *testing.T) {
	t.Parallel()

	probeID := seededProbeID(9)
	conn := newScriptedConn(probeMatchXML{
		endpoint:  "urn:uuid:partial-before-cancel",
		relatesTo: probeID,
	}.build())
	discoverer := NewDiscoverer(Options{
		Timeout:         5 * time.Second,
		InterfaceSource: singleInterfaceSource(),
		Randomness:      seededRandomness(9),
		Listen:          func(Interface) (PacketConn, error) { return conn, nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	result, err := discoverer.Discover(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Discover() error = %v, want context.Canceled", err)
	}
	if len(result.Devices) != 1 || result.Devices[0].EndpointReference != "urn:uuid:partial-before-cancel" {
		t.Fatalf("partial result = %+v, want the correlated reply retained", result.Devices)
	}
}

func TestListenerPreCancelledContextDoesNotBind(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	binds := 0
	listener := NewListener(Options{
		InterfaceSource: singleInterfaceSource(),
		Listen: func(Interface) (PacketConn, error) {
			binds++
			return newScriptedConn(), nil
		},
	})
	if _, err := listener.Listen(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Listen() error = %v, want context.Canceled", err)
	}
	if binds != 0 {
		t.Fatalf("listen factory calls = %d, want 0 for pre-cancelled context", binds)
	}
}

func TestDiscoveryActionsMustMatchBodyFamily(t *testing.T) {
	t.Parallel()

	probe := parsedMessage{
		Kind:               kindProbeMatch,
		DiscoveryNamespace: nsDiscovery2009,
		Action:             discoveryAction(nsDiscovery2005, actionProbeMatches),
		RelatesTo:          "urn:uuid:probe",
	}
	if correlatedProbeMatch(probe, "urn:uuid:probe") {
		t.Fatal("cross-family ProbeMatches action was accepted")
	}
	hello := parsedMessage{
		Kind:               kindHello,
		DiscoveryNamespace: nsDiscovery2009,
		EndpointReference:  "urn:uuid:device",
		Action:             discoveryAction(nsDiscovery2005, actionHello),
	}
	if _, ok := lifecycleEvent(hello, "en0"); ok {
		t.Fatal("cross-family Hello action was accepted")
	}
}

func TestMetadataVersionUsesUnsignedIntCanonicalForm(t *testing.T) {
	t.Parallel()

	message, err := parseMessage(probeMatchXML{metadataVersion: "00010"}.build())
	if err != nil {
		t.Fatalf("parseMessage() error = %v", err)
	}
	if !message.HasMetadataVersion || message.MetadataVersionNum != 10 || message.MetadataVersion != "10" {
		t.Fatalf("metadata = %q/%d (present=%t), want canonical 10", message.MetadataVersion, message.MetadataVersionNum, message.HasMetadataVersion)
	}
	message, err = parseMessage(probeMatchXML{metadataVersion: "4294967296"}.build())
	if err != nil {
		t.Fatalf("parseMessage(overflow) error = %v", err)
	}
	if message.HasMetadataVersion || message.MetadataVersion != "" {
		t.Fatalf("overflow metadata was retained: %+v", message)
	}
}
