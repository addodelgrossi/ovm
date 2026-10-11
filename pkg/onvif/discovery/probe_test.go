package discovery

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
)

func TestProbeMessageContent(t *testing.T) {
	msg := probeMessage("urn:uuid:test-id", onvifDeviceType)
	text := string(msg)

	// The probe must be well-formed XML.
	if err := xml.Unmarshal(msg, new(struct {
		XMLName xml.Name
	})); err != nil {
		t.Fatalf("probe is not well-formed XML: %v", err)
	}

	wantSubstrings := []string{
		`xmlns:soap="` + nsSOAPEnvelope12 + `"`,
		`xmlns:wsa="` + probeAddressingNamespace + `"`,
		`xmlns:wsd="` + probeDiscoveryNamespace + `"`,
		`xmlns:dn="` + onvifNetworkNamespace + `"`,
		`<wsa:MessageID>urn:uuid:test-id</wsa:MessageID>`,
		`<wsa:To>` + probeTo + `</wsa:To>`,
		`<wsa:Action>` + probeAction + `</wsa:Action>`,
		`<wsd:Probe>`,
		onvifDeviceType,
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(text, want) {
			t.Errorf("probe missing %q", want)
		}
	}

	// Exactly one ONVIF type must appear in each Types element; active
	// discovery sends a separate, independently correlated probe for the other.
	start := strings.Index(text, "<wsd:Types>")
	end := strings.Index(text, "</wsd:Types>")
	if start < 0 || end < 0 || end < start {
		t.Fatalf("probe has no Types element")
	}
	types := text[start+len("<wsd:Types>") : end]
	if types != onvifDeviceType {
		t.Fatalf("Types element = %q, want exactly %q", types, onvifDeviceType)
	}
}

// TestProbeEmitsCoherentSingleFamily asserts that every addressing/discovery
// value in the Probe belongs to one coherent family (2005/04) and that no value
// from the other family (2009/01 OASIS) leaks in. This is the regression guard
// for the previously mixed probe.
func TestProbeEmitsCoherentSingleFamily(t *testing.T) {
	text := string(probeMessage("urn:uuid:coherent", onvifDeviceType))

	// Exact 2005/04-family values.
	wantPresent := map[string]string{
		"discovery namespace":  nsDiscovery2005,
		"addressing namespace": nsAddressing2004,
		"discovery target":     discoveryTo2005,
		"probe action":         discoveryAction(nsDiscovery2005, actionProbe),
	}
	for label, value := range wantPresent {
		if !strings.Contains(text, value) {
			t.Errorf("probe missing %s %q", label, value)
		}
	}

	// None of the OTHER family's distinctive values may appear.
	wantAbsent := map[string]string{
		"2009 discovery namespace": nsDiscovery2009,
		"2005/08 addressing":       nsAddressing2005,
		"2009 discovery target":    discoveryTo2009,
	}
	for label, value := range wantAbsent {
		if strings.Contains(text, value) {
			t.Errorf("probe mixed in %s %q; family must be coherent", label, value)
		}
	}

	// Document the exact normative values this test pins.
	if probeTo != "urn:schemas-xmlsoap-org:ws:2005:04:discovery" {
		t.Fatalf("probe To = %q, want the 2005/04 ad-hoc target", probeTo)
	}
	if probeAction != "http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe" {
		t.Fatalf("probe Action = %q, want the 2005/04 Probe action", probeAction)
	}
	if probeAddressingNamespace != "http://schemas.xmlsoap.org/ws/2004/08/addressing" {
		t.Fatalf("probe addressing namespace = %q, want 2004/08", probeAddressingNamespace)
	}
}

func TestProbeMessageEscapesMessageID(t *testing.T) {
	msg := probeMessage(`a&b<c>`, onvifDeviceType)
	if bytes.Contains(msg, []byte(`a&b<c>`)) {
		t.Fatalf("message id was not escaped")
	}
	if !bytes.Contains(msg, []byte(`a&amp;b&lt;c&gt;`)) {
		t.Fatalf("message id escaping missing: %s", msg)
	}
}

func TestNewMessageIDDeterministicWithSeededRandomness(t *testing.T) {
	seed := bytes.Repeat([]byte{0xAB}, 16)
	id, err := newMessageID(bytes.NewReader(seed))
	if err != nil {
		t.Fatalf("newMessageID: %v", err)
	}
	// Version and variant bits must be set per RFC 4122.
	const want = "urn:uuid:abababab-abab-4bab-abab-abababababab"
	if id != want {
		t.Fatalf("id = %q, want %q", id, want)
	}
}

func TestNewMessageIDReportsRandomnessFailure(t *testing.T) {
	if _, err := newMessageID(bytes.NewReader(nil)); err == nil {
		t.Fatalf("expected error from exhausted randomness source")
	}
}
