package discovery

import "strings"

// multiProbeMatchXML builds a single WS-Discovery ProbeMatches datagram that
// wraps several ProbeMatch children, so tests can exercise aggregation of every
// match in one datagram and the skip-one-keep-the-rest malformed-match policy.
//
// A match whose omitEndpoint flag is set is rendered without any
// EndpointReference, which is the malformed-individual case: it cannot be keyed
// and must be skipped while valid siblings survive.
//
// The datagram carries a single coherent 2009/01 header shared by every match:
// a ProbeMatches Action and a RelatesTo, so a correlated multi-match datagram
// aggregates all of its matches. relatesTo defaults to defaultRelatesTo; set it
// to the sent probe MessageID to exercise active-discovery correlation.
type multiProbeMatchXML struct {
	matches   []probeMatchXML
	relatesTo string
	// action overrides the shared wsa:Action; empty uses the 2009 ProbeMatches
	// action and omitAction renders none.
	action string
}

func (m multiProbeMatchXML) build() []byte {
	const (
		s = "s"
		a = "a"
		d = "d"
	)
	relatesTo := m.relatesTo
	if relatesTo == "" {
		relatesTo = defaultRelatesTo
	}
	action := m.action
	if action == "" {
		action = discoveryAction(nsDiscovery2009, actionProbeMatches)
	}
	var b strings.Builder
	b.WriteString(`<` + s + `:Envelope`)
	b.WriteString(` xmlns:` + s + `="` + nsSOAPEnvelope12 + `"`)
	b.WriteString(` xmlns:` + a + `="` + nsAddressing2005 + `"`)
	b.WriteString(` xmlns:` + d + `="` + nsDiscovery2009 + `">`)
	b.WriteString(`<` + s + `:Header>`)
	b.WriteString(`<` + a + `:MessageID>urn:uuid:msg-multi</` + a + `:MessageID>`)
	if action != omitAction {
		b.WriteString(`<` + a + `:Action>` + action + `</` + a + `:Action>`)
	}
	b.WriteString(`<` + a + `:RelatesTo>` + relatesTo + `</` + a + `:RelatesTo>`)
	b.WriteString(`</` + s + `:Header>`)
	b.WriteString(`<` + s + `:Body>`)
	b.WriteString(`<` + d + `:ProbeMatches>`)
	for _, match := range m.matches {
		b.WriteString(`<` + d + `:ProbeMatch>`)
		if !match.omitEndpoint {
			endpoint := match.endpoint
			if endpoint == "" {
				endpoint = "urn:uuid:device-1"
			}
			b.WriteString(`<` + a + `:EndpointReference>`)
			b.WriteString(`<` + a + `:Address>` + endpoint + `</` + a + `:Address>`)
			b.WriteString(`</` + a + `:EndpointReference>`)
		}
		if match.types != "" {
			b.WriteString(`<` + d + `:Types>` + match.types + `</` + d + `:Types>`)
		}
		if match.scopes != "" {
			b.WriteString(`<` + d + `:Scopes>` + match.scopes + `</` + d + `:Scopes>`)
		}
		if match.xaddrs != "" {
			b.WriteString(`<` + d + `:XAddrs>` + match.xaddrs + `</` + d + `:XAddrs>`)
		}
		mv := match.metadataVersion
		if mv == "" {
			mv = "1"
		}
		if mv != "-" {
			b.WriteString(`<` + d + `:MetadataVersion>` + mv + `</` + d + `:MetadataVersion>`)
		}
		b.WriteString(`</` + d + `:ProbeMatch>`)
	}
	b.WriteString(`</` + d + `:ProbeMatches>`)
	b.WriteString(`</` + s + `:Body>`)
	b.WriteString(`</` + s + `:Envelope>`)
	return []byte(b.String())
}
