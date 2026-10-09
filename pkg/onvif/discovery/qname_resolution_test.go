package discovery

import (
	"reflect"
	"testing"
)

// TestParseMessageResolvesTypeQNamesToExpandedNames verifies that inbound Types
// QNames are resolved against the message's in-scope namespace declarations and
// normalised to stable "{uri}local" expanded-name strings, independent of the
// prefix a peer happened to use.
func TestParseMessageResolvesTypeQNamesToExpandedNames(t *testing.T) {
	fixture := probeMatchXML{
		types: "n:NetworkVideoTransmitter t:Device",
		typesXMLNS: `xmlns:n="` + onvifNetworkNamespace + `" ` +
			`xmlns:t="` + onvifDeviceNamespace + `"`,
	}
	msg, err := parseMessage(fixture.build())
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	want := []string{
		qname{Space: onvifNetworkNamespace, Local: "NetworkVideoTransmitter"}.expandedName(),
		qname{Space: onvifDeviceNamespace, Local: "Device"}.expandedName(),
	}
	if !reflect.DeepEqual(msg.Types, want) {
		t.Fatalf("types = %v, want expanded names %v", msg.Types, want)
	}
}

// TestTypeQNamePrefixVariantsDedupe verifies that two ProbeMatches advertising
// the SAME namespace+local under DIFFERENT prefixes aggregate to a single
// resolved identity rather than two prefix-dependent strings.
func TestTypeQNamePrefixVariantsDedupe(t *testing.T) {
	a := probeMatchXML{
		endpoint:   "urn:uuid:same",
		types:      "aa:NetworkVideoTransmitter",
		typesXMLNS: `xmlns:aa="` + onvifNetworkNamespace + `"`,
	}
	b := probeMatchXML{
		endpoint:   "urn:uuid:same",
		types:      "bb:NetworkVideoTransmitter",
		typesXMLNS: `xmlns:bb="` + onvifNetworkNamespace + `"`,
	}
	agg := newAggregator()
	for _, f := range []probeMatchXML{a, b} {
		msg, err := parseMessage(f.build())
		if err != nil {
			t.Fatalf("parseMessage: %v", err)
		}
		agg.add(msg)
	}
	devices := agg.devicesSorted()
	if len(devices) != 1 {
		t.Fatalf("devices = %d, want 1", len(devices))
	}
	want := []string{qname{Space: onvifNetworkNamespace, Local: "NetworkVideoTransmitter"}.expandedName()}
	if !reflect.DeepEqual(devices[0].Types, want) {
		t.Fatalf("types = %v, want single deduped identity %v", devices[0].Types, want)
	}
}

// TestTypeQNameUndeclaredPrefixSkipped verifies that a Types token whose prefix
// is not declared in scope is skipped (it cannot be assigned a stable
// identity) rather than retained as ambiguous lexical text.
func TestTypeQNameUndeclaredPrefixSkipped(t *testing.T) {
	fixture := probeMatchXML{
		types:      "ghost:Thing n:NetworkVideoTransmitter",
		typesXMLNS: `xmlns:n="` + onvifNetworkNamespace + `"`,
	}
	msg, err := parseMessage(fixture.build())
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	want := []string{qname{Space: onvifNetworkNamespace, Local: "NetworkVideoTransmitter"}.expandedName()}
	if !reflect.DeepEqual(msg.Types, want) {
		t.Fatalf("types = %v, want only the resolvable token %v", msg.Types, want)
	}
}

// TestParseMetadataVersionNumericPolicy verifies the metadata-version policy:
// a valid non-negative integer is parsed numerically and flagged present; an
// empty, signed, or non-numeric value is rejected (not present) so it cannot
// win selection.
func TestParseMetadataVersionNumericPolicy(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantNum  uint64
		wantHave bool
	}{
		{name: "zero", raw: "0", wantNum: 0, wantHave: true},
		{name: "ten", raw: "10", wantNum: 10, wantHave: true},
		{name: "large", raw: "4294967295", wantNum: 4294967295, wantHave: true},
		{name: "empty", raw: "", wantHave: false},
		{name: "whitespace", raw: "   ", wantHave: false},
		{name: "negative", raw: "-1", wantHave: false},
		{name: "non numeric", raw: "v2", wantHave: false},
		{name: "float", raw: "1.5", wantHave: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			num, have := parseMetadataVersion(tc.raw)
			if have != tc.wantHave {
				t.Fatalf("have = %v, want %v", have, tc.wantHave)
			}
			if have && num != tc.wantNum {
				t.Fatalf("num = %d, want %d", num, tc.wantNum)
			}
		})
	}
}

// TestParseMessageMetadataVersionNumeric verifies the parsed message carries a
// numeric metadata version while retaining the textual form, and that a
// malformed version is not flagged present.
func TestParseMessageMetadataVersionNumeric(t *testing.T) {
	ok, err := parseMessage(probeMatchXML{metadataVersion: "10"}.build())
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	if !ok.HasMetadataVersion || ok.MetadataVersionNum != 10 || ok.MetadataVersion != "10" {
		t.Fatalf("metadata = %+v, want numeric 10 with textual \"10\"", ok)
	}

	bad, err := parseMessage(probeMatchXML{metadataVersion: "nope"}.build())
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	if bad.HasMetadataVersion {
		t.Fatalf("malformed metadata version was flagged present: %+v", bad)
	}
}

// TestParseMessageExposesAddressingActionAndRelatesTo verifies that the
// WS-Addressing Action and RelatesTo headers are parsed namespace-resolved and
// copied onto the message for correlation.
func TestParseMessageExposesAddressingActionAndRelatesTo(t *testing.T) {
	fixture := probeMatchXML{
		relatesTo: "urn:uuid:probe-123",
		action:    discoveryAction(nsDiscovery2009, actionProbeMatches),
	}
	msg, err := parseMessage(fixture.build())
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	if msg.RelatesTo != "urn:uuid:probe-123" {
		t.Fatalf("RelatesTo = %q, want urn:uuid:probe-123", msg.RelatesTo)
	}
	if msg.Action != discoveryAction(nsDiscovery2009, actionProbeMatches) {
		t.Fatalf("Action = %q, want the 2009 ProbeMatches action", msg.Action)
	}
}

// TestParserRetainsNamespaceDeclarationsBounded verifies that per-node
// namespace scope is bounded: a datagram that declares far more than the bound
// still parses, and resolution against the most-recent declarations still
// works, without unbounded growth.
func TestParserRetainsNamespaceDeclarationsBounded(t *testing.T) {
	// Build a Types element that declares the network namespace on prefix "n"
	// and resolves correctly even though the datagram is otherwise ordinary.
	fixture := probeMatchXML{
		types:      "n:NetworkVideoTransmitter",
		typesXMLNS: `xmlns:n="` + onvifNetworkNamespace + `"`,
	}
	root, err := parseTree(fixture.build())
	if err != nil {
		t.Fatalf("parseTree: %v", err)
	}
	// Walk to the Types node and confirm its scope is bounded.
	var typesNode *node
	var walk func(*node)
	walk = func(n *node) {
		if n.Local == "Types" {
			typesNode = n
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	if typesNode == nil {
		t.Fatal("Types node not found")
	}
	if len(typesNode.nsScope) > maxNamespaceDecls {
		t.Fatalf("nsScope = %d declarations, want <= %d (bounded)", len(typesNode.nsScope), maxNamespaceDecls)
	}
	if uri, ok := typesNode.resolvePrefix("n"); !ok || uri != onvifNetworkNamespace {
		t.Fatalf("resolvePrefix(n) = %q,%v, want network namespace", uri, ok)
	}
}
