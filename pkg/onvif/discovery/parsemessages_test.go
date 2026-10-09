package discovery

import (
	"errors"
	"testing"
)

// TestParseMessagesReturnsEveryMatch verifies that parseMessages aggregates all
// ProbeMatch children of a single datagram, not only the first.
func TestParseMessagesReturnsEveryMatch(t *testing.T) {
	data := multiProbeMatchXML{
		matches: []probeMatchXML{
			{endpoint: "urn:uuid:dev-a", xaddrs: "http://192.0.2.1/"},
			{endpoint: "urn:uuid:dev-b", xaddrs: "http://192.0.2.2/"},
			{endpoint: "urn:uuid:dev-c", xaddrs: "http://192.0.2.3/"},
		},
	}.build()

	messages, err := parseMessages(data)
	if err != nil {
		t.Fatalf("parseMessages: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(messages))
	}
	got := map[string]bool{}
	for _, m := range messages {
		if m.Kind != kindProbeMatch {
			t.Fatalf("kind = %v, want ProbeMatch", m.Kind)
		}
		got[m.EndpointReference] = true
	}
	for _, want := range []string{"urn:uuid:dev-a", "urn:uuid:dev-b", "urn:uuid:dev-c"} {
		if !got[want] {
			t.Fatalf("missing match %q in %v", want, got)
		}
	}
}

// TestParseMessagesSkipsMalformedIndividualMatch verifies the documented
// policy: a single match lacking an EndpointReference is skipped while valid
// siblings in the same datagram are retained.
func TestParseMessagesSkipsMalformedIndividualMatch(t *testing.T) {
	data := multiProbeMatchXML{
		matches: []probeMatchXML{
			{endpoint: "urn:uuid:dev-a", xaddrs: "http://192.0.2.1/"},
			{omitEndpoint: true, xaddrs: "http://192.0.2.9/"}, // malformed: no EPR
			{endpoint: "urn:uuid:dev-c", xaddrs: "http://192.0.2.3/"},
		},
	}.build()

	messages, err := parseMessages(data)
	if err != nil {
		t.Fatalf("parseMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2 (malformed match skipped)", len(messages))
	}
	for _, m := range messages {
		if m.EndpointReference == "" {
			t.Fatalf("kept a match with no endpoint reference")
		}
	}
}

// TestParseMessagesRejectsDatagramWithNoValidMatch verifies that a datagram
// whose only matches are all malformed is rejected rather than reported as an
// empty success.
func TestParseMessagesRejectsDatagramWithNoValidMatch(t *testing.T) {
	data := multiProbeMatchXML{
		matches: []probeMatchXML{
			{omitEndpoint: true, xaddrs: "http://192.0.2.9/"},
			{omitEndpoint: true, xaddrs: "http://192.0.2.8/"},
		},
	}.build()

	if _, err := parseMessages(data); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("err = %v, want ErrMalformedMessage", err)
	}
}

// TestParseMessageReturnsFirstMatch verifies the single-message compatibility
// helper still returns a usable first match from a multi-match datagram.
func TestParseMessageReturnsFirstMatch(t *testing.T) {
	data := multiProbeMatchXML{
		matches: []probeMatchXML{
			{endpoint: "urn:uuid:dev-a", xaddrs: "http://192.0.2.1/"},
			{endpoint: "urn:uuid:dev-b", xaddrs: "http://192.0.2.2/"},
		},
	}.build()

	msg, err := parseMessage(data)
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	if msg.EndpointReference != "urn:uuid:dev-a" {
		t.Fatalf("endpoint = %q, want first match urn:uuid:dev-a", msg.EndpointReference)
	}
}

// TestParseMessagesBoundsDatagramSize verifies the plural entry point enforces
// the same size bound as the single-message helper.
func TestParseMessagesBoundsDatagramSize(t *testing.T) {
	oversized := make([]byte, maxDatagramSize+1)
	if _, err := parseMessages(oversized); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("err = %v, want ErrMessageTooLarge", err)
	}
}
