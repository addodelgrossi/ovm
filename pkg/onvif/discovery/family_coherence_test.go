package discovery

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// crossPairEnvelope builds a ProbeMatches datagram with explicitly chosen
// discovery and addressing namespaces for the header and the body, so a test
// can deliberately construct an incoherent cross-pairing. All prefixes are
// fixed; only the namespace URIs vary.
type crossPairEnvelope struct {
	// bodyDiscoveryNS is the discovery namespace of the ProbeMatches wrapper
	// and ProbeMatch child (the body fixes the family).
	bodyDiscoveryNS string
	// headerAddressingNS is the addressing namespace used for the header
	// Action, MessageID, and RelatesTo elements.
	headerAddressingNS string
	// bodyAddressingNS is the addressing namespace used for the body's
	// EndpointReference/Address. Empty uses headerAddressingNS.
	bodyAddressingNS string
}

func (c crossPairEnvelope) build() []byte {
	bodyAddr := c.bodyAddressingNS
	if bodyAddr == "" {
		bodyAddr = c.headerAddressingNS
	}
	var b strings.Builder
	b.WriteString(`<s:Envelope xmlns:s="` + nsSOAPEnvelope12 + `"`)
	b.WriteString(` xmlns:ah="` + c.headerAddressingNS + `"`)
	b.WriteString(` xmlns:ab="` + bodyAddr + `"`)
	b.WriteString(` xmlns:d="` + c.bodyDiscoveryNS + `">`)
	b.WriteString(`<s:Header>`)
	b.WriteString(`<ah:Action>` + discoveryAction(c.bodyDiscoveryNS, actionProbeMatches) + `</ah:Action>`)
	b.WriteString(`<ah:MessageID>urn:uuid:msg</ah:MessageID>`)
	b.WriteString(`<ah:RelatesTo>urn:uuid:probe</ah:RelatesTo>`)
	b.WriteString(`</s:Header>`)
	b.WriteString(`<s:Body><d:ProbeMatches><d:ProbeMatch>`)
	b.WriteString(`<ab:EndpointReference><ab:Address>urn:uuid:dev</ab:Address></ab:EndpointReference>`)
	b.WriteString(`<d:XAddrs>http://192.0.2.1/onvif/device_service</d:XAddrs>`)
	b.WriteString(`</d:ProbeMatch></d:ProbeMatches></s:Body></s:Envelope>`)
	return []byte(b.String())
}

// TestParseMessageAcceptsBothCoherentGenerations asserts finding 5: parser
// tolerance for EITHER coherent generation is retained. A coherent 2005/04 +
// 2004/08 message and a coherent 2009/01 + 2005/08 message both parse.
func TestParseMessageAcceptsBothCoherentGenerations(t *testing.T) {
	coherent2005 := crossPairEnvelope{
		bodyDiscoveryNS:    nsDiscovery2005,
		headerAddressingNS: nsAddressing2004,
	}.build()
	if _, err := parseMessage(coherent2005); err != nil {
		t.Fatalf("coherent 2005/04 message rejected: %v", err)
	}

	coherent2009 := crossPairEnvelope{
		bodyDiscoveryNS:    nsDiscovery2009,
		headerAddressingNS: nsAddressing2005,
	}.build()
	if _, err := parseMessage(coherent2009); err != nil {
		t.Fatalf("coherent 2009/01 message rejected: %v", err)
	}
}

// TestParseMessageRejectsCrossPairedHeaderAddressing asserts finding 5: a body
// discovery family paired with the OTHER generation's addressing namespace in
// the header (Action/MessageID/RelatesTo) is rejected. Both cross directions
// are covered.
func TestParseMessageRejectsCrossPairedHeaderAddressing(t *testing.T) {
	cases := []struct {
		name            string
		bodyDiscoveryNS string
		headerAddrNS    string
	}{
		{"2005 discovery with 2005/08 addressing", nsDiscovery2005, nsAddressing2005},
		{"2009 discovery with 2004/08 addressing", nsDiscovery2009, nsAddressing2004},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := crossPairEnvelope{
				bodyDiscoveryNS:    tc.bodyDiscoveryNS,
				headerAddressingNS: tc.headerAddrNS,
			}.build()
			if _, err := parseMessages(data); !errors.Is(err, ErrMalformedMessage) {
				t.Fatalf("err = %v, want ErrMalformedMessage for cross-paired header addressing", err)
			}
		})
	}
}

// TestParseMessageRejectsCrossPairedBodyEndpointReference asserts finding 5: an
// EndpointReference/Address drawn from the wrong addressing family (while the
// header is coherent) is rejected, because every addressing element in the
// subtree must use the paired namespace.
func TestParseMessageRejectsCrossPairedBodyEndpointReference(t *testing.T) {
	// Coherent 2009/01 header, but the body EndpointReference uses 2004/08.
	data := crossPairEnvelope{
		bodyDiscoveryNS:    nsDiscovery2009,
		headerAddressingNS: nsAddressing2005,
		bodyAddressingNS:   nsAddressing2004,
	}.build()
	if _, err := parseMessages(data); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("err = %v, want ErrMalformedMessage for cross-family EndpointReference", err)
	}
}

// TestParseMessageRejectsCrossFamilyProbeMatchWrapperAndChild asserts finding
// 5: a ProbeMatches wrapper or ProbeMatch child from the other discovery family
// relative to the message is rejected.
func TestParseMessageRejectsCrossFamilyProbeMatchWrapperAndChild(t *testing.T) {
	// ProbeMatches wrapper in 2005/04 but the ProbeMatch child in 2009/01.
	var b strings.Builder
	b.WriteString(`<s:Envelope xmlns:s="` + nsSOAPEnvelope12 + `"`)
	b.WriteString(` xmlns:a="` + nsAddressing2004 + `"`)
	b.WriteString(` xmlns:d="` + nsDiscovery2005 + `"`)
	b.WriteString(` xmlns:d2="` + nsDiscovery2009 + `">`)
	b.WriteString(`<s:Header></s:Header>`)
	b.WriteString(`<s:Body><d:ProbeMatches><d2:ProbeMatch>`)
	b.WriteString(`<a:EndpointReference><a:Address>urn:uuid:dev</a:Address></a:EndpointReference>`)
	b.WriteString(`</d2:ProbeMatch></d:ProbeMatches></s:Body></s:Envelope>`)
	// The 2009/01 child is not a recognised child of the 2005/04 wrapper, so no
	// match is found and the datagram is malformed.
	if _, err := parseMessages([]byte(b.String())); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("err = %v, want ErrMalformedMessage for cross-family ProbeMatch child", err)
	}
}

// TestParseMessageRejectsCrossFamilyAppSequence asserts finding 5/6: an
// AppSequence element from the other discovery family relative to the body is
// rejected rather than silently ignored.
func TestParseMessageRejectsCrossFamilyAppSequence(t *testing.T) {
	// Coherent 2009/01 body + 2005/08 header, but AppSequence in 2005/04.
	var b strings.Builder
	b.WriteString(`<s:Envelope xmlns:s="` + nsSOAPEnvelope12 + `"`)
	b.WriteString(` xmlns:a="` + nsAddressing2005 + `"`)
	b.WriteString(` xmlns:d="` + nsDiscovery2009 + `"`)
	b.WriteString(` xmlns:dold="` + nsDiscovery2005 + `">`)
	b.WriteString(`<s:Header>`)
	b.WriteString(`<dold:AppSequence InstanceId="1" MessageNumber="1"/>`)
	b.WriteString(`</s:Header>`)
	b.WriteString(`<s:Body><d:ProbeMatches><d:ProbeMatch>`)
	b.WriteString(`<a:EndpointReference><a:Address>urn:uuid:dev</a:Address></a:EndpointReference>`)
	b.WriteString(`</d:ProbeMatch></d:ProbeMatches></s:Body></s:Envelope>`)
	if _, err := parseMessages([]byte(b.String())); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("err = %v, want ErrMalformedMessage for cross-family AppSequence", err)
	}
}

// TestParseMessageRejectsCriticalAttributeBeyondCap asserts finding 6: when a
// RelatesTo element carries more attributes than the retained-attribute cap, a
// RelationshipType placed beyond the cap cannot be trusted absent; rather than
// silently defaulting to Reply, the datagram is rejected as malformed.
func TestParseMessageRejectsCriticalAttributeBeyondCap(t *testing.T) {
	var attrs strings.Builder
	// Emit more than maxAttrs filler attributes before any RelationshipType, so
	// the retained view is truncated and a critical attribute could be lost.
	for i := 0; i < maxAttrs+8; i++ {
		attrs.WriteString(` x` + strconv.Itoa(i) + `="v"`)
	}
	header := `<a:RelatesTo` + attrs.String() + `>urn:uuid:probe-1</a:RelatesTo>`
	data := probeMatchEnvelope(header, "")
	if _, err := parseMessage(data); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("err = %v, want ErrMalformedMessage when RelatesTo attributes exceed the cap", err)
	}
}

// TestParseMessageRejectsAppSequenceAttributesBeyondCap asserts finding 6: an
// AppSequence element whose attributes exceed the cap is malformed, because a
// critical ordering counter could have been dropped.
func TestParseMessageRejectsAppSequenceAttributesBeyondCap(t *testing.T) {
	var attrs strings.Builder
	attrs.WriteString(`InstanceId="1" MessageNumber="1"`)
	for i := 0; i < maxAttrs+8; i++ {
		attrs.WriteString(` y` + strconv.Itoa(i) + `="v"`)
	}
	data := probeMatchEnvelope(`<a:MessageID>urn:uuid:x</a:MessageID>`, attrs.String())
	if _, err := parseMessage(data); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("err = %v, want ErrMalformedMessage when AppSequence attributes exceed the cap", err)
	}
}

func TestParseMessageRejectsMismatchedWrapperEvenWhenChildDrivesOtherFamily(t *testing.T) {
	data := []byte(`<s:Envelope xmlns:s="` + nsSOAPEnvelope12 + `" xmlns:a="` + nsAddressing2005 + `" xmlns:dold="` + nsDiscovery2005 + `" xmlns:d="` + nsDiscovery2009 + `"><s:Header><a:Action>` + discoveryAction(nsDiscovery2009, actionProbeMatches) + `</a:Action><a:RelatesTo>urn:uuid:probe</a:RelatesTo></s:Header><s:Body><dold:ProbeMatches><d:ProbeMatch><a:EndpointReference><a:Address>urn:uuid:device</a:Address></a:EndpointReference></d:ProbeMatch></dold:ProbeMatches></s:Body></s:Envelope>`)
	if _, err := parseMessage(data); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("mismatched wrapper/child error = %v, want ErrMalformedMessage", err)
	}
}

func TestParseMessageRejectsNestedCrossFamilyHeader(t *testing.T) {
	data := []byte(`<s:Envelope xmlns:s="` + nsSOAPEnvelope12 + `" xmlns:a="` + nsAddressing2004 + `" xmlns:anew="` + nsAddressing2005 + `" xmlns:d="` + nsDiscovery2005 + `"><s:Header><a:Action>` + discoveryAction(nsDiscovery2005, actionProbeMatches) + `</a:Action><a:RelatesTo>urn:uuid:probe</a:RelatesTo><a:From><anew:EndpointReference><anew:Address>urn:uuid:sender</anew:Address></anew:EndpointReference></a:From></s:Header><s:Body><d:ProbeMatches><d:ProbeMatch><a:EndpointReference><a:Address>urn:uuid:device</a:Address></a:EndpointReference></d:ProbeMatch></d:ProbeMatches></s:Body></s:Envelope>`)
	if _, err := parseMessage(data); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("nested cross-family header error = %v, want ErrMalformedMessage", err)
	}
}
