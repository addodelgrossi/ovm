package discovery

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// multicastGroup is the IPv4 WS-Discovery multicast address and port.
const multicastGroup = "239.255.255.250:3702"

// The probe is emitted as a fully coherent WS-Discovery 2005/04 message.
//
// Family choice: the ONVIF Remote Discovery WSDL
// (https://www.onvif.org/ver10/network/wsdl/remotediscovery.wsdl) is based on
// the WS-Discovery April 2005 draft, and the broadest set of deployed cameras
// speaks that family. We therefore emit a single, internally consistent
// 2005/04 Probe and never mix generations. Concretely, every outbound value is
// drawn from the 2005/04 family:
//
//   - discovery namespace: http://schemas.xmlsoap.org/ws/2005/04/discovery
//   - WS-Addressing namespace: http://schemas.xmlsoap.org/ws/2004/08/addressing
//     (the 2004/08 addressing generation that pairs with the 2005/04 draft)
//   - wsa:Action: http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe
//   - wsa:To: urn:schemas-xmlsoap-org:ws:2005:04:discovery
//
// Inbound parsing continues to accept BOTH the 2005/04 and 2009/01 families so
// that a camera which only replies in the OASIS 2009/01 generation is still
// understood; only emission is pinned to one coherent family.
const (
	// probeDiscoveryNamespace is the discovery namespace the Probe declares.
	probeDiscoveryNamespace = nsDiscovery2005
	// probeAddressingNamespace is the WS-Addressing namespace the Probe
	// declares, matching the discovery family.
	probeAddressingNamespace = nsAddressing2004
	// probeTo is the ad-hoc discovery target for the declared family.
	probeTo = discoveryTo2005
)

// probeAction is the wsa:Action URI for the emitted Probe. It is exported at
// package scope so tests can assert the exact value against the declared
// family.
var probeAction = discoveryAction(probeDiscoveryNamespace, actionProbe)

// probeTypes are emitted as separate Probe datagrams. WS-Discovery type
// matching is conjunctive within one Types element, so combining these values
// would miss a target that advertises only the current Device type or only the
// legacy NetworkVideoTransmitter type.
var probeTypes = []string{onvifDeviceType, onvifNVTType}

// probeMessage renders a WS-Discovery Probe envelope for exactly one ONVIF
// type. Active discovery sends one independently correlated message for each
// entry in probeTypes.
//
// Normative sources:
//   - WS-Discovery (2005/04 draft) defines the Probe message, the ad-hoc
//     discovery target urn, and the Action URI.
//   - The ONVIF Remote Discovery WSDL and the ONVIF Core Specification define
//     the discovery profile and the NetworkVideoTransmitter type under the
//     network WSDL namespace.
//   - The ONVIF device management WSDL (http://www.onvif.org/ver10/device/wsdl)
//     defines the Device type.
//
// The message is built as a byte buffer rather than via encoding/xml so that
// the exact namespace bindings, element ordering, and QName prefixes are
// deterministic and auditable. Both advertised type prefixes ("dn" and "tds")
// are declared on the envelope so no prefix is ever emitted undeclared.
func probeMessage(messageID, probeType string) []byte {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	buf.WriteString(`<soap:Envelope`)
	buf.WriteString(` xmlns:soap="` + nsSOAPEnvelope12 + `"`)
	buf.WriteString(` xmlns:wsa="` + probeAddressingNamespace + `"`)
	buf.WriteString(` xmlns:wsd="` + probeDiscoveryNamespace + `"`)
	buf.WriteString(` xmlns:` + onvifNVTPrefix + `="` + onvifNetworkNamespace + `"`)
	buf.WriteString(` xmlns:` + onvifDevicePrefix + `="` + onvifDeviceNamespace + `">`)
	buf.WriteString(`<soap:Header>`)
	buf.WriteString(`<wsa:MessageID>` + xmlEscape(messageID) + `</wsa:MessageID>`)
	buf.WriteString(`<wsa:To>` + probeTo + `</wsa:To>`)
	buf.WriteString(`<wsa:Action>` + probeAction + `</wsa:Action>`)
	buf.WriteString(`</soap:Header>`)
	buf.WriteString(`<soap:Body>`)
	buf.WriteString(`<wsd:Probe>`)
	buf.WriteString(`<wsd:Types>` + probeType + `</wsd:Types>`)
	buf.WriteString(`<wsd:Scopes/>`)
	buf.WriteString(`</wsd:Probe>`)
	buf.WriteString(`</soap:Body>`)
	buf.WriteString(`</soap:Envelope>`)
	return buf.Bytes()
}

// newMessageID returns a fresh WS-Discovery MessageID as a urn:uuid. It reads
// from the supplied randomness source so tests can make identifiers
// deterministic; production callers pass crypto/rand.Reader.
func newMessageID(randomness io.Reader) (string, error) {
	if randomness == nil {
		randomness = rand.Reader
	}
	var raw [16]byte
	if _, err := io.ReadFull(randomness, raw[:]); err != nil {
		return "", fmt.Errorf("generate probe message id: %w", err)
	}
	// Set the RFC 4122 version (4) and variant bits.
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return "urn:uuid:" + formatUUID(raw), nil
}

// formatUUID renders 16 bytes as the canonical 8-4-4-4-12 hexadecimal form.
func formatUUID(raw [16]byte) string {
	hexed := hex.EncodeToString(raw[:])
	return hexed[0:8] + "-" + hexed[8:12] + "-" + hexed[12:16] + "-" + hexed[16:20] + "-" + hexed[20:32]
}

// xmlEscape escapes the small set of characters that must not appear literally
// inside element text for the hand-built probe envelope.
func xmlEscape(value string) string {
	var buf bytes.Buffer
	for _, r := range value {
		switch r {
		case '&':
			buf.WriteString("&amp;")
		case '<':
			buf.WriteString("&lt;")
		case '>':
			buf.WriteString("&gt;")
		case '"':
			buf.WriteString("&quot;")
		case '\'':
			buf.WriteString("&apos;")
		default:
			buf.WriteRune(r)
		}
	}
	return buf.String()
}
