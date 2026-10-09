package discovery

import (
	"encoding/xml"
	"strings"
	"testing"
)

// TestProbeTypeQNamesResolveToNormativeNamespaces asserts the exact resolved
// qualified names a Probe advertises — namespace URI and local name — rather
// than mere substring presence of a prefixed string. NetworkVideoTransmitter
// must bind to the network WSDL namespace and Device to the device WSDL
// namespace.
func TestProbeTypeQNamesResolveToNormativeNamespaces(t *testing.T) {
	got := probeTypeQNames()
	want := []qname{
		{Space: onvifNetworkNamespace, Local: "NetworkVideoTransmitter"},
		{Space: onvifDeviceNamespace, Local: "Device"},
	}
	if len(got) != len(want) {
		t.Fatalf("qnames = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("qname[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	// The two namespaces must be distinct: Device is deliberately not under the
	// network WSDL namespace.
	if onvifNetworkNamespace == onvifDeviceNamespace {
		t.Fatalf("network and device namespaces must differ")
	}
}

// TestProbeTypesResolveAgainstDeclaredPrefixes decodes the actual Probe
// envelope and resolves each token in the Types element against the prefixes
// declared on the envelope, asserting the resolved {namespace, local} pairs.
// This fails if a type ever used an undeclared prefix or the wrong namespace.
func TestProbeTypesResolveAgainstDeclaredPrefixes(t *testing.T) {
	lexical := []string{onvifNVTType, onvifDeviceType}
	want := probeTypeQNames()
	for i, probeType := range lexical {
		msg := probeMessage("urn:uuid:qname-test", probeType)
		prefixNS, typesText := decodeProbeTypes(t, msg)
		tokens := strings.Fields(typesText)
		if len(tokens) != 1 {
			t.Fatalf("Types tokens = %v, want exactly one", tokens)
		}
		prefix, local, hasPrefix := strings.Cut(tokens[0], ":")
		if !hasPrefix {
			t.Fatalf("type %q has no prefix", tokens[0])
		}
		namespace, ok := prefixNS[prefix]
		if !ok {
			t.Fatalf("type %q uses undeclared prefix %q", tokens[0], prefix)
		}
		if got := (qname{Space: namespace, Local: local}); got != want[i] {
			t.Fatalf("resolved type = %+v, want %+v", got, want[i])
		}
	}
}

// decodeProbeTypes walks the probe envelope, collecting the namespace
// declarations in scope on the root element and the raw text of the Types
// element. It intentionally reads prefix declarations from attributes so the
// test resolves tokens exactly as a conformant parser would.
func decodeProbeTypes(t *testing.T, msg []byte) (map[string]string, string) {
	t.Helper()
	decoder := xml.NewDecoder(strings.NewReader(string(msg)))
	prefixNS := map[string]string{}
	var (
		inTypes   bool
		typesText string
	)
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch el := tok.(type) {
		case xml.StartElement:
			for _, attr := range el.Attr {
				if attr.Name.Space == "xmlns" {
					prefixNS[attr.Name.Local] = attr.Value
				}
			}
			if el.Name.Local == "Types" {
				inTypes = true
			}
		case xml.CharData:
			if inTypes {
				typesText += string(el)
			}
		case xml.EndElement:
			if el.Name.Local == "Types" {
				inTypes = false
			}
		}
	}
	if typesText == "" {
		t.Fatalf("probe has no Types text")
	}
	return prefixNS, typesText
}
