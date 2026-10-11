package discovery

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseMessageNamespaceAndPrefixVariations(t *testing.T) {
	tests := []struct {
		name    string
		fixture probeMatchXML
	}{
		{
			name:    "2009 discovery, 2005 addressing, default prefixes",
			fixture: probeMatchXML{xaddrs: "http://192.0.2.1/onvif/device_service"},
		},
		{
			name: "2005 discovery namespace",
			fixture: probeMatchXML{
				wsdNS:  nsDiscovery2005,
				xaddrs: "http://192.0.2.1/onvif/device_service",
			},
		},
		{
			name: "2004 addressing namespace",
			fixture: probeMatchXML{
				// Coherent 2005/04 family: 2005/04 discovery pairs with
				// 2004/08 addressing.
				wsdNS:  nsDiscovery2005,
				wsaNS:  nsAddressing2004,
				xaddrs: "http://192.0.2.1/onvif/device_service",
			},
		},
		{
			name: "unusual prefixes",
			fixture: probeMatchXML{
				soapPrefix: "soapenv",
				wsaPrefix:  "addr",
				wsdPrefix:  "disco",
				xaddrs:     "http://192.0.2.1/onvif/device_service",
			},
		},
		{
			name: "soap 1.1 envelope tolerated",
			fixture: probeMatchXML{
				soapNS: "http://schemas.xmlsoap.org/soap/envelope/",
				xaddrs: "http://192.0.2.1/onvif/device_service",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg, err := parseMessage(tc.fixture.build())
			if err != nil {
				t.Fatalf("parseMessage: %v", err)
			}
			if msg.Kind != kindProbeMatch {
				t.Fatalf("kind = %v, want ProbeMatch", msg.Kind)
			}
			if msg.EndpointReference != "urn:uuid:device-1" {
				t.Fatalf("endpoint = %q", msg.EndpointReference)
			}
			if len(msg.XAddrs) != 1 || msg.XAddrs[0] != "http://192.0.2.1/onvif/device_service" {
				t.Fatalf("xaddrs = %v", msg.XAddrs)
			}
		})
	}
}

func TestParseMessageSplitsSpaceDelimitedLists(t *testing.T) {
	fixture := probeMatchXML{
		xaddrs: "http://192.0.2.1/onvif/device_service  http://192.0.2.1/onvif/device_service http://[2001:db8::1]/onvif",
		scopes: "onvif://www.onvif.org/type/video_encoder onvif://www.onvif.org/name/Cam",
		types:  "dn:NetworkVideoTransmitter",
	}
	msg, err := parseMessage(fixture.build())
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	wantXAddrs := []string{
		"http://192.0.2.1/onvif/device_service",
		"http://[2001:db8::1]/onvif",
	}
	if !reflect.DeepEqual(msg.XAddrs, wantXAddrs) {
		t.Fatalf("xaddrs = %v, want deduplicated %v", msg.XAddrs, wantXAddrs)
	}
	if len(msg.Scopes) != 2 {
		t.Fatalf("scopes = %v, want 2", msg.Scopes)
	}
}

func TestParseMessageRejectsMalformedAndSpoofed(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "not xml", data: []byte("this is not xml")},
		{name: "truncated", data: []byte("<s:Envelope xmlns:s=\"" + nsSOAPEnvelope12 + "\"><s:Body>")},
		{name: "wrong root", data: []byte(`<root xmlns="urn:x"><child/></root>`)},
		{
			name: "foreign discovery namespace (spoofed)",
			data: probeMatchXML{wsdNS: "urn:evil:discovery"}.build(),
		},
		{
			name: "missing endpoint reference",
			data: []byte(`<s:Envelope xmlns:s="` + nsSOAPEnvelope12 + `" xmlns:d="` + nsDiscovery2009 + `">` +
				`<s:Body><d:ProbeMatches><d:ProbeMatch><d:XAddrs>http://x/</d:XAddrs></d:ProbeMatch></d:ProbeMatches></s:Body></s:Envelope>`),
		},
		{
			name: "dtd rejected",
			data: []byte(`<!DOCTYPE a><s:Envelope xmlns:s="` + nsSOAPEnvelope12 + `"><s:Body/></s:Envelope>`),
		},
		{
			name: "no discovery body",
			data: []byte(`<s:Envelope xmlns:s="` + nsSOAPEnvelope12 + `"><s:Body><other/></s:Body></s:Envelope>`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseMessage(tc.data); err == nil {
				t.Fatalf("expected rejection, got nil error")
			}
		})
	}
}

func TestParseMessageBoundsDatagramSize(t *testing.T) {
	oversized := make([]byte, maxDatagramSize+1)
	if _, err := parseMessage(oversized); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("err = %v, want ErrMessageTooLarge", err)
	}
}

func TestParseMessageBoundsFieldCountsAndLength(t *testing.T) {
	var scopes strings.Builder
	for i := 0; i < maxScopes+50; i++ {
		scopes.WriteString("onvif://scope/")
		scopes.WriteString(strings.Repeat("x", 3))
		scopes.WriteByte(' ')
		// make each unique
		scopes.WriteString(strings.Repeat("y", i%5))
		scopes.WriteByte(' ')
	}
	fixture := probeMatchXML{scopes: scopes.String(), xaddrs: "http://192.0.2.1/"}
	msg, err := parseMessage(fixture.build())
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	if len(msg.Scopes) > maxScopes {
		t.Fatalf("scopes len = %d, want <= %d", len(msg.Scopes), maxScopes)
	}
}

func TestParseMessageHelloAndBye(t *testing.T) {
	hello, err := parseMessage(lifecycleXML{local: "Hello", xaddrs: "http://192.0.2.5/onvif/device_service"}.build())
	if err != nil {
		t.Fatalf("hello: %v", err)
	}
	if hello.Kind != kindHello {
		t.Fatalf("hello kind = %v", hello.Kind)
	}
	bye, err := parseMessage(lifecycleXML{local: "Bye"}.build())
	if err != nil {
		t.Fatalf("bye: %v", err)
	}
	if bye.Kind != kindBye {
		t.Fatalf("bye kind = %v", bye.Kind)
	}
}
