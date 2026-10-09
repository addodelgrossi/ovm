package discovery

import (
	"errors"
	"testing"
)

// probeMatchEnvelope builds a 2005/04 ProbeMatches datagram with explicit,
// test-controlled header fragments so finding-3 behaviours (MessageID,
// AppSequence, RelationshipType, duplicate headers) can be exercised exactly.
// headerExtra is injected verbatim inside the SOAP header; appSeqAttrs, when
// non-empty, is injected verbatim as the attributes of a wsd:AppSequence
// element in the header.
func probeMatchEnvelope(headerExtra, appSeqAttrs string) []byte {
	var b []byte
	add := func(s string) { b = append(b, s...) }
	add(`<s:Envelope xmlns:s="` + nsSOAPEnvelope12 + `"`)
	add(` xmlns:a="` + nsAddressing2004 + `"`)
	add(` xmlns:d="` + nsDiscovery2005 + `">`)
	add(`<s:Header>`)
	add(headerExtra)
	if appSeqAttrs != "" {
		add(`<d:AppSequence ` + appSeqAttrs + `/>`)
	}
	add(`</s:Header>`)
	add(`<s:Body><d:ProbeMatches><d:ProbeMatch>`)
	add(`<a:EndpointReference><a:Address>urn:uuid:dev-1</a:Address></a:EndpointReference>`)
	add(`<d:XAddrs>http://192.0.2.1/onvif/device_service</d:XAddrs>`)
	add(`</d:ProbeMatch></d:ProbeMatches></s:Body></s:Envelope>`)
	return b
}

// TestParseMessageParsesMessageIDAndAppSequence asserts finding 3: the parser
// extracts the WS-Addressing MessageID and the WS-Discovery AppSequence
// (InstanceId, SequenceId, MessageNumber).
func TestParseMessageParsesMessageIDAndAppSequence(t *testing.T) {
	data := probeMatchEnvelope(
		`<a:MessageID>urn:uuid:abc-123</a:MessageID>`,
		`InstanceId="42" SequenceId="seq-7" MessageNumber="9"`,
	)
	msg, err := parseMessage(data)
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	if msg.MessageID != "urn:uuid:abc-123" {
		t.Fatalf("MessageID = %q, want urn:uuid:abc-123", msg.MessageID)
	}
	if !msg.HasAppSequence {
		t.Fatal("HasAppSequence = false, want true")
	}
	if msg.AppSequence.InstanceID != 42 || msg.AppSequence.SequenceID != "seq-7" || msg.AppSequence.MessageNumber != 9 {
		t.Fatalf("AppSequence = %+v, want {42 seq-7 9}", msg.AppSequence)
	}
}

// TestParseMessageRejectsMalformedAppSequence asserts that a present but
// malformed AppSequence (non-numeric InstanceId) is rejected rather than
// silently ignored.
func TestParseMessageRejectsMalformedAppSequence(t *testing.T) {
	data := probeMatchEnvelope(
		`<a:MessageID>urn:uuid:abc</a:MessageID>`,
		`InstanceId="not-a-number" MessageNumber="1"`,
	)
	if _, err := parseMessage(data); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("err = %v, want ErrMalformedMessage for malformed AppSequence", err)
	}
}

// TestParseMessageRejectsDuplicateAction asserts finding 3: a header carrying
// two Action elements is rejected whether or not they agree.
func TestParseMessageRejectsDuplicateAction(t *testing.T) {
	action := discoveryAction(nsDiscovery2005, actionProbeMatches)
	data := probeMatchEnvelope(
		`<a:Action>`+action+`</a:Action><a:Action>`+action+`</a:Action>`,
		"",
	)
	if _, err := parseMessage(data); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("err = %v, want ErrMalformedMessage for duplicate Action", err)
	}
}

// TestParseMessageRejectsDuplicateRelatesTo asserts that two RelatesTo elements
// (conflicting here) are rejected.
func TestParseMessageRejectsDuplicateRelatesTo(t *testing.T) {
	data := probeMatchEnvelope(
		`<a:RelatesTo>urn:uuid:one</a:RelatesTo><a:RelatesTo>urn:uuid:two</a:RelatesTo>`,
		"",
	)
	if _, err := parseMessage(data); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("err = %v, want ErrMalformedMessage for duplicate RelatesTo", err)
	}
}

// TestParseMessagePreservesRelationshipTypeAndDefaultsReply asserts finding 3:
// the RelatesTo RelationshipType is preserved, and an absent attribute defaults
// to Reply (ReplyRelationship true).
func TestParseMessagePreservesRelationshipTypeAndDefaultsReply(t *testing.T) {
	// Default (no RelationshipType attribute): classified as Reply.
	def := probeMatchEnvelope(`<a:RelatesTo>urn:uuid:probe-1</a:RelatesTo>`, "")
	msg, err := parseMessage(def)
	if err != nil {
		t.Fatalf("parseMessage default: %v", err)
	}
	if msg.RelatesToType != "Reply" || !msg.ReplyRelationship {
		t.Fatalf("default RelatesTo type = %q reply=%v, want Reply/true", msg.RelatesToType, msg.ReplyRelationship)
	}

	// Explicit non-Reply RelationshipType is preserved and NOT classified Reply.
	custom := probeMatchEnvelope(
		`<a:RelatesTo RelationshipType="CustomResponse">urn:uuid:probe-1</a:RelatesTo>`,
		"",
	)
	msg, err = parseMessage(custom)
	if err != nil {
		t.Fatalf("parseMessage custom: %v", err)
	}
	if msg.RelatesToType != "CustomResponse" || msg.ReplyRelationship {
		t.Fatalf("custom RelatesTo type = %q reply=%v, want CustomResponse/false", msg.RelatesToType, msg.ReplyRelationship)
	}
}

// TestCorrelatedProbeMatchRequiresActionAndReply asserts that active
// correlation requires both the exact ProbeMatches Action and a Reply
// relationship. An action-less message is not accepted even when RelatesTo and
// RelationshipType otherwise look valid.
func TestCorrelatedProbeMatchRequiresActionAndReply(t *testing.T) {
	const probeID = "urn:uuid:probe-xyz"

	actionless := parsedMessage{
		Kind:               kindProbeMatch,
		DiscoveryNamespace: nsDiscovery2005,
		RelatesTo:          probeID,
		ReplyRelationship:  true,
	}
	if correlatedProbeMatch(actionless, probeID) {
		t.Fatal("action-less ProbeMatch must not correlate")
	}

	reply := actionless
	reply.Action = discoveryAction(nsDiscovery2005, actionProbeMatches)
	if !correlatedProbeMatch(reply, probeID) {
		t.Fatal("ProbeMatches with the exact action and Reply relationship should correlate")
	}

	nonReply := reply
	nonReply.ReplyRelationship = false
	if correlatedProbeMatch(nonReply, probeID) {
		t.Fatal("non-Reply ProbeMatch must not correlate")
	}
}

// TestParseMessageRejectsNestedCrossFamilyDiscovery asserts finding 3: a
// discovery body that nests an element from the OTHER discovery family is
// incoherent and rejected.
func TestParseMessageRejectsNestedCrossFamilyDiscovery(t *testing.T) {
	// 2005/04 ProbeMatch body nesting a 2009/01 Types element.
	var b []byte
	add := func(s string) { b = append(b, s...) }
	add(`<s:Envelope xmlns:s="` + nsSOAPEnvelope12 + `"`)
	add(` xmlns:a="` + nsAddressing2004 + `"`)
	add(` xmlns:d="` + nsDiscovery2005 + `"`)
	add(` xmlns:d2="` + nsDiscovery2009 + `">`)
	add(`<s:Header></s:Header>`)
	add(`<s:Body><d:ProbeMatches><d:ProbeMatch>`)
	add(`<a:EndpointReference><a:Address>urn:uuid:x</a:Address></a:EndpointReference>`)
	add(`<d2:Types>dn:NetworkVideoTransmitter</d2:Types>`) // cross-family nested child
	add(`</d:ProbeMatch></d:ProbeMatches></s:Body></s:Envelope>`)
	if _, err := parseMessages(b); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("err = %v, want ErrMalformedMessage for nested cross-family element", err)
	}
}

// TestParseMessageRejectsMixedAddressingFamilies asserts finding 3: a header
// that mixes two WS-Addressing generations (Action in 2004/08 and RelatesTo in
// 2005/08) is incoherent and rejected.
func TestParseMessageRejectsMixedAddressingFamilies(t *testing.T) {
	var b []byte
	add := func(s string) { b = append(b, s...) }
	add(`<s:Envelope xmlns:s="` + nsSOAPEnvelope12 + `"`)
	add(` xmlns:a="` + nsAddressing2004 + `"`)
	add(` xmlns:a2="` + nsAddressing2005 + `"`)
	add(` xmlns:d="` + nsDiscovery2005 + `">`)
	add(`<s:Header>`)
	add(`<a:Action>` + discoveryAction(nsDiscovery2005, actionProbeMatches) + `</a:Action>`)
	add(`<a2:RelatesTo>urn:uuid:probe-1</a2:RelatesTo>`) // other addressing family
	add(`</s:Header>`)
	add(`<s:Body><d:ProbeMatches><d:ProbeMatch>`)
	add(`<a:EndpointReference><a:Address>urn:uuid:x</a:Address></a:EndpointReference>`)
	add(`</d:ProbeMatch></d:ProbeMatches></s:Body></s:Envelope>`)
	if _, err := parseMessages(b); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("err = %v, want ErrMalformedMessage for mixed addressing families", err)
	}
}

// TestCorrelatedProbeMatchRejectsNonReplyEvenWithAction asserts finding 4: the
// Reply relationship is required for active correlation REGARDLESS of whether a
// recognised ProbeMatches Action is present. A datagram carrying the correct
// ProbeMatches Action and a matching RelatesTo but a non-Reply relationship is
// not an answer to our probe and must be rejected.
func TestCorrelatedProbeMatchRejectsNonReplyEvenWithAction(t *testing.T) {
	const probeID = "urn:uuid:probe-xyz"

	withActionReply := parsedMessage{
		Kind:               kindProbeMatch,
		DiscoveryNamespace: nsDiscovery2005,
		Action:             discoveryAction(nsDiscovery2005, actionProbeMatches),
		RelatesTo:          probeID,
		ReplyRelationship:  true,
	}
	if !correlatedProbeMatch(withActionReply, probeID) {
		t.Fatal("ProbeMatches with correct action and Reply relationship should correlate")
	}

	withActionNonReply := parsedMessage{
		Kind:               kindProbeMatch,
		DiscoveryNamespace: nsDiscovery2005,
		Action:             discoveryAction(nsDiscovery2005, actionProbeMatches),
		RelatesTo:          probeID,
		ReplyRelationship:  false, // non-Reply relationship despite correct action
	}
	if correlatedProbeMatch(withActionNonReply, probeID) {
		t.Fatal("ProbeMatches with a non-Reply relationship must not correlate even with the correct action")
	}
}

// TestParseMessageRejectsDuplicateAppSequence asserts finding 6: a header that
// carries two AppSequence elements is malformed and rejected.
func TestParseMessageRejectsDuplicateAppSequence(t *testing.T) {
	data := probeMatchEnvelope(
		`<a:MessageID>urn:uuid:dup</a:MessageID>`+
			`<d:AppSequence InstanceId="1" MessageNumber="1"/>`+
			`<d:AppSequence InstanceId="1" MessageNumber="2"/>`,
		"",
	)
	if _, err := parseMessage(data); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("err = %v, want ErrMalformedMessage for duplicate AppSequence", err)
	}
}

// TestParseMessageRelationshipType2005AbsoluteReplyURI asserts finding 6: in
// the 2009/01 + 2005/08 family the explicit Reply relationship is the absolute
// reply URI, and any other absolute value is a preserved custom relationship
// that is NOT classified as Reply.
func TestParseMessageRelationshipType2005AbsoluteReplyURI(t *testing.T) {
	explicit := probeMatchXML{
		relatesTo: "urn:uuid:probe-1",
		relToType: nsAddressing2005 + "/reply",
	}.build()
	msg, err := parseMessage(explicit)
	if err != nil {
		t.Fatalf("parseMessage explicit 2005 reply: %v", err)
	}
	if !msg.ReplyRelationship || msg.RelatesToType != nsAddressing2005+"/reply" {
		t.Fatalf("2005 reply URI: type=%q reply=%v, want absolute reply URI classified Reply", msg.RelatesToType, msg.ReplyRelationship)
	}

	custom := probeMatchXML{
		relatesTo: "urn:uuid:probe-1",
		relToType: "http://example.com/custom",
	}.build()
	msg, err = parseMessage(custom)
	if err != nil {
		t.Fatalf("parseMessage custom 2005: %v", err)
	}
	if msg.ReplyRelationship || msg.RelatesToType != "http://example.com/custom" {
		t.Fatalf("custom 2005: type=%q reply=%v, want preserved custom not classified Reply", msg.RelatesToType, msg.ReplyRelationship)
	}
}

// TestParseMessageRelationshipType2004QName asserts that in the 2005/04 +
// 2004/08 family an explicit Reply is an unqualified "Reply" token or a QName
// resolving to the addressing namespace with local part "Reply". A QName with
// an undeclared prefix is malformed rather than being treated as custom.
func TestParseMessageRelationshipType2004QName(t *testing.T) {
	// Unqualified local token.
	local := probeMatchXML{
		wsdNS:     nsDiscovery2005,
		relatesTo: "urn:uuid:probe-1",
		relToType: "Reply",
	}.build()
	msg, err := parseMessage(local)
	if err != nil {
		t.Fatalf("parseMessage 2004 local Reply: %v", err)
	}
	if !msg.ReplyRelationship {
		t.Fatalf("2004 local Reply not classified as Reply: %q", msg.RelatesToType)
	}

	undeclared := probeMatchXML{
		wsdNS:     nsDiscovery2005,
		relatesTo: "urn:uuid:probe-1",
		relToType: "ghost:Reply",
	}.build()
	if _, err := parseMessage(undeclared); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("undeclared RelationshipType QName error = %v, want ErrMalformedMessage", err)
	}
}

func TestParseMessageRejectsMalformedRelationshipType(t *testing.T) {
	cases := []probeMatchXML{
		{wsdNS: nsDiscovery2005, relatesTo: "urn:uuid:p", relToType: "bad::QName"},
		{relatesTo: "urn:uuid:p", relToType: "relative/relation"},
	}
	for _, fixture := range cases {
		if _, err := parseMessage(fixture.build()); !errors.Is(err, ErrMalformedMessage) {
			t.Fatalf("RelationshipType %q error = %v, want ErrMalformedMessage", fixture.relToType, err)
		}
	}
}
