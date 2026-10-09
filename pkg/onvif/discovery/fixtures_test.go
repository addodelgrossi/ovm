package discovery

import "strings"

// probeMatchXML builds a synthetic WS-Discovery ProbeMatches datagram with
// configurable namespaces and prefixes so tests can exercise namespace and
// prefix variation deterministically without any network.
//
// By default it carries a coherent 2009/01 header: a ProbeMatches Action in the
// 2009 discovery namespace and a RelatesTo so active-discovery correlation can
// be asserted. Tests that correlate to a specific probe set relatesTo to that
// probe's MessageID.
type probeMatchXML struct {
	soapPrefix string
	soapNS     string
	wsaPrefix  string
	wsaNS      string
	wsdPrefix  string
	wsdNS      string

	endpoint string
	types    string
	scopes   string
	xaddrs   string
	// relatesTo sets the wsa:RelatesTo header value. When empty the default
	// correlation id is used. Set to the sent probe MessageID to correlate.
	relatesTo string
	// relToType sets the wsa:RelationshipType attribute on the RelatesTo
	// element. When empty no attribute is emitted (defaulting to Reply).
	relToType string
	// action overrides the wsa:Action header. When empty the ProbeMatches
	// action for the datagram's discovery namespace is used. Set to omitAction
	// to render no Action header at all.
	action string
	// typesXMLNS, when non-empty, is emitted verbatim as extra namespace
	// declarations on the Types element so QName-resolution tests can bind the
	// prefixes used inside types.
	typesXMLNS string
	// metadataVersion overrides the MetadataVersion element text. When empty a
	// default of "1" is used; set to "-" to omit the element entirely.
	metadataVersion string
	// omitEndpoint, when set, renders a ProbeMatch without any
	// EndpointReference. It is consumed by multiProbeMatchXML to model a
	// malformed individual match.
	omitEndpoint bool
}

// omitAction is a sentinel for probeMatchXML.action and lifecycleXML.action
// that renders no WS-Addressing Action header.
const omitAction = "\x00omit\x00"

// defaultRelatesTo is the correlation id used by fixtures that do not override
// relatesTo. Tests that assert correlation against a specific probe set
// relatesTo to that probe's MessageID instead.
const defaultRelatesTo = "urn:uuid:probe-default"

func (p probeMatchXML) withDefaults() probeMatchXML {
	if p.soapPrefix == "" {
		p.soapPrefix = "s"
	}
	if p.soapNS == "" {
		p.soapNS = nsSOAPEnvelope12
	}
	if p.wsaPrefix == "" {
		p.wsaPrefix = "a"
	}
	if p.wsdPrefix == "" {
		p.wsdPrefix = "d"
	}
	if p.wsdNS == "" {
		p.wsdNS = nsDiscovery2009
	}
	if p.wsaNS == "" {
		// Keep the datagram family-coherent: the addressing namespace is the
		// one paired with the chosen discovery family (2005/04 -> 2004/08,
		// 2009/01 -> 2005/08). Tests exercising cross-pairing set wsaNS
		// explicitly to the mismatched family.
		if paired := addressingForDiscovery(p.wsdNS); paired != "" {
			p.wsaNS = paired
		} else {
			p.wsaNS = nsAddressing2005
		}
	}
	if p.endpoint == "" {
		p.endpoint = "urn:uuid:device-1"
	}
	if p.relatesTo == "" {
		p.relatesTo = defaultRelatesTo
	}
	if p.action == "" {
		p.action = discoveryAction(p.wsdNS, actionProbeMatches)
	}
	if p.metadataVersion == "" {
		p.metadataVersion = "1"
	}
	return p
}

// build renders the datagram as bytes.
func (p probeMatchXML) build() []byte {
	p = p.withDefaults()
	var b strings.Builder
	s, a, d := p.soapPrefix, p.wsaPrefix, p.wsdPrefix
	b.WriteString(`<` + s + `:Envelope`)
	b.WriteString(` xmlns:` + s + `="` + p.soapNS + `"`)
	b.WriteString(` xmlns:` + a + `="` + p.wsaNS + `"`)
	b.WriteString(` xmlns:` + d + `="` + p.wsdNS + `">`)
	b.WriteString(`<` + s + `:Header>`)
	b.WriteString(`<` + a + `:MessageID>urn:uuid:msg-1</` + a + `:MessageID>`)
	if p.action != omitAction {
		b.WriteString(`<` + a + `:Action>` + p.action + `</` + a + `:Action>`)
	}
	if p.relToType != "" {
		b.WriteString(`<` + a + `:RelatesTo RelationshipType="` + p.relToType + `">` + p.relatesTo + `</` + a + `:RelatesTo>`)
	} else {
		b.WriteString(`<` + a + `:RelatesTo>` + p.relatesTo + `</` + a + `:RelatesTo>`)
	}
	b.WriteString(`</` + s + `:Header>`)
	b.WriteString(`<` + s + `:Body>`)
	b.WriteString(`<` + d + `:ProbeMatches>`)
	b.WriteString(`<` + d + `:ProbeMatch>`)
	if !p.omitEndpoint {
		b.WriteString(`<` + a + `:EndpointReference>`)
		b.WriteString(`<` + a + `:Address>` + p.endpoint + `</` + a + `:Address>`)
		b.WriteString(`</` + a + `:EndpointReference>`)
	}
	if p.types != "" {
		typesOpen := `<` + d + `:Types`
		if p.typesXMLNS != "" {
			typesOpen += ` ` + p.typesXMLNS
		}
		typesOpen += `>`
		b.WriteString(typesOpen + p.types + `</` + d + `:Types>`)
	}
	if p.scopes != "" {
		b.WriteString(`<` + d + `:Scopes>` + p.scopes + `</` + d + `:Scopes>`)
	}
	if p.xaddrs != "" {
		b.WriteString(`<` + d + `:XAddrs>` + p.xaddrs + `</` + d + `:XAddrs>`)
	}
	if p.metadataVersion != "-" {
		b.WriteString(`<` + d + `:MetadataVersion>` + p.metadataVersion + `</` + d + `:MetadataVersion>`)
	}
	b.WriteString(`</` + d + `:ProbeMatch>`)
	b.WriteString(`</` + d + `:ProbeMatches>`)
	b.WriteString(`</` + s + `:Body>`)
	b.WriteString(`</` + s + `:Envelope>`)
	return []byte(b.String())
}

// lifecycleXML builds a Hello or Bye datagram.
type lifecycleXML struct {
	local    string // "Hello" or "Bye"
	wsdNS    string
	wsaNS    string
	endpoint string
	xaddrs   string
	scopes   string
	types    string
	// action overrides the wsa:Action header. When empty the lifecycle action
	// for local in the datagram's discovery namespace is used; set to
	// omitAction to render no Action header.
	action string
}

func (l lifecycleXML) build() []byte {
	wsdNS := l.wsdNS
	if wsdNS == "" {
		wsdNS = nsDiscovery2009
	}
	wsaNS := l.wsaNS
	if wsaNS == "" {
		if paired := addressingForDiscovery(wsdNS); paired != "" {
			wsaNS = paired
		} else {
			wsaNS = nsAddressing2005
		}
	}
	endpoint := l.endpoint
	if endpoint == "" {
		endpoint = "urn:uuid:device-1"
	}
	action := l.action
	if action == "" {
		action = discoveryAction(wsdNS, l.local)
	}
	var b strings.Builder
	b.WriteString(`<s:Envelope xmlns:s="` + nsSOAPEnvelope12 + `"`)
	b.WriteString(` xmlns:a="` + wsaNS + `" xmlns:d="` + wsdNS + `">`)
	b.WriteString(`<s:Header>`)
	if action != omitAction {
		b.WriteString(`<a:Action>` + action + `</a:Action>`)
	}
	b.WriteString(`</s:Header>`)
	b.WriteString(`<s:Body>`)
	b.WriteString(`<d:` + l.local + `>`)
	b.WriteString(`<a:EndpointReference><a:Address>` + endpoint + `</a:Address></a:EndpointReference>`)
	if l.types != "" {
		b.WriteString(`<d:Types>` + l.types + `</d:Types>`)
	}
	if l.xaddrs != "" {
		b.WriteString(`<d:XAddrs>` + l.xaddrs + `</d:XAddrs>`)
	}
	if l.scopes != "" {
		b.WriteString(`<d:Scopes>` + l.scopes + `</d:Scopes>`)
	}
	b.WriteString(`</d:` + l.local + `>`)
	b.WriteString(`</s:Body>`)
	b.WriteString(`</s:Envelope>`)
	return []byte(b.String())
}
