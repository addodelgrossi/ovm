// Package discovery implements interface-aware ONVIF WS-Discovery and the
// manual endpoint canonicalization used by commands such as "ovm info <host>".
//
// The package is deliberately independent of service models, credential
// storage, command-line adapters, and user interfaces. Network access is
// expressed through injectable seams (an interface source and a packet
// connection factory) so that probing, response aggregation, and the passive
// Hello/Bye lifecycle can be exercised deterministically in public CI without
// any multicast capability.
//
// Active discovery (Discoverer) binds one exact-source UDP socket for every
// usable IPv4 address on each selected interface, pins the multicast egress
// interface and a hop limit (TTL) of 1 so a Probe stays on the local link,
// sends the Probe to the multicast group with bounded jittered retransmission
// (each retransmission reusing the same MessageID and bytes), and reads the
// unicast ProbeMatches replies. Only interfaces that are up, multicast-capable,
// and not point-to-point are probed. Passive listening (Listener) instead joins
// the WS-Discovery IPv4 multicast group 239.255.255.250:3702 on each selected
// interface, which is the only way to observe the multicast Hello and Bye
// announcements devices emit; ListenStream additionally surfaces per-interface
// read and partial-bind errors and suppresses duplicate (MessageID) and stale
// (AppSequence) announcements.
//
// Normative sources:
//   - WS-Discovery: outbound active probes use the ONVIF-required 2005/04
//     family. Inbound parsing additionally tolerates coherent OASIS 2009/01
//     ProbeMatches and lifecycle messages; this is parser tolerance, not a
//     claim that this package emits 2009/01 probes.
//   - WS-Addressing: each accepted discovery family is paired strictly with
//     its coherent addressing generation (2005/04 discovery with 2004/08
//     addressing; 2009/01 discovery with 2005/08 addressing).
//   - ONVIF Remote Discovery WSDL
//     (https://www.onvif.org/ver10/network/wsdl/remotediscovery.wsdl) and the
//     ONVIF Core Specification define the discovery profile and bind the
//     NetworkVideoTransmitter type to the ONVIF network WSDL namespace.
//   - ONVIF device management WSDL (http://www.onvif.org/ver10/device/wsdl)
//     defines the Device type. Because "Device" is not a network-WSDL type, the
//     Probe binds it to the device WSDL namespace (prefix "tds") while
//     NetworkVideoTransmitter stays bound to the network WSDL namespace (prefix
//     "dn"); see namespace.go for the explicit rationale.
//
// WS-Discovery responses are untrusted. Datagrams and diagnostic data are
// bounded, oversized datagrams are rejected rather than parsed from a truncated
// prefix, malformed or spoofed structures are rejected, and parsing accepts
// namespace-prefix variations plus coherent 2005/04 and 2009/01 inbound
// families. Duplicate addressing headers, cross-family combinations, stale
// lifecycle sequences, and uncorrelated replies are rejected.
package discovery
