package device

import (
	"errors"
	"strings"
	"testing"
)

func TestDecodeOperationRejectsUnsafeAndMalformedXML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{"document type declaration", `<!DOCTYPE x><tds:GetHostnameResponse xmlns:tds="` + Namespace + `"/>`},
		{"truncated", `<tds:GetHostnameResponse xmlns:tds="` + Namespace + `">`},
		{"empty", ``},
		{"text only", `   `},
		{"deep nesting", strings.Repeat(`<a xmlns="urn:x">`, maxDecodeDepth+2)},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := decodeOperation([]byte(tt.input)); !errors.Is(err, ErrMalformedXML) {
				t.Fatalf("decodeOperation(%q) error = %v, want ErrMalformedXML", tt.name, err)
			}
		})
	}
}

func TestDecodeOperationNamespaceStrictPrefixTolerant(t *testing.T) {
	t.Parallel()

	// Two documents with different prefixes but identical namespaces must
	// decode to the same identity, and a matching local name in a foreign
	// namespace must be rejected by requireRoot.
	withTds := `<tds:GetHostnameResponse xmlns:tds="` + Namespace + `"/>`
	withOther := `<x:GetHostnameResponse xmlns:x="` + Namespace + `"/>`
	for _, input := range []string{withTds, withOther} {
		root, err := decodeOperation([]byte(input))
		if err != nil {
			t.Fatalf("decodeOperation(%q) error = %v", input, err)
		}
		if err := requireRoot(root, "GetHostnameResponse"); err != nil {
			t.Errorf("requireRoot error = %v for %q", err, input)
		}
	}

	foreign := `<tds:GetHostnameResponse xmlns:tds="urn:foreign"/>`
	root, err := decodeOperation([]byte(foreign))
	if err != nil {
		t.Fatalf("decodeOperation(foreign) error = %v", err)
	}
	if err := requireRoot(root, "GetHostnameResponse"); !errors.Is(err, ErrUnexpectedElement) {
		t.Errorf("requireRoot(foreign) error = %v, want ErrUnexpectedElement", err)
	}
}

func TestPreservedXMLIsBounded(t *testing.T) {
	t.Parallel()

	var builder strings.Builder
	builder.WriteString(`<tds:GetCapabilitiesResponse xmlns:tds="` + Namespace + `"><tds:Capabilities>`)
	// Produce more than the preservation bound of filler content.
	filler := strings.Repeat(`<tds:X>abcdefghij</tds:X>`, 2000)
	builder.WriteString(filler)
	builder.WriteString(`</tds:Capabilities></tds:GetCapabilitiesResponse>`)
	data := []byte(builder.String())

	root, err := decodeOperation(data)
	if err != nil {
		t.Fatalf("decodeOperation() error = %v", err)
	}
	caps := root.child(Namespace, "Capabilities")
	if caps == nil {
		t.Fatal("Capabilities child missing")
	}
	raw := preservedDiagnosticXML(caps, data)
	if len(raw) == 0 {
		t.Fatal("preservedXML returned nothing")
	}
	if len(raw) > maxPreservedExtensionBytes {
		t.Errorf("preservedXML length = %d, want <= %d", len(raw), maxPreservedExtensionBytes)
	}
}

func TestPreservedDiagnosticXMLRedactsBeforeBoundaryTruncation(t *testing.T) {
	t.Parallel()

	marker := strings.Join([]string{"boundary", "sensitive", "marker"}, "-")
	const openingPrefix = `<v:Password data="`
	preservedPrefix := `<tds:Capabilities><v:Padding>`
	preservedMiddle := `</v:Padding>` + openingPrefix
	paddingLength := maxPreservedExtensionBytes - 8 - len(preservedPrefix) - len(preservedMiddle) - len(marker)
	if paddingLength < 1 {
		t.Fatal("invalid boundary fixture calculation")
	}
	capabilities := preservedPrefix + strings.Repeat("x", paddingLength) + preservedMiddle + marker +
		`" padding="` + strings.Repeat("y", 512) + `">value</v:Password></tds:Capabilities>`
	data := []byte(`<tds:GetCapabilitiesResponse xmlns:tds="` + Namespace + `" xmlns:v="urn:synthetic">` +
		capabilities + `</tds:GetCapabilitiesResponse>`)
	root, err := decodeOperation(data)
	if err != nil {
		t.Fatalf("decodeOperation() error = %v", err)
	}
	caps := root.child(Namespace, "Capabilities")
	if caps == nil {
		t.Fatal("Capabilities child missing")
	}
	retained := preservedDiagnosticXML(caps, data)
	if len(retained) > maxPreservedExtensionBytes {
		t.Fatalf("retained length = %d, want <= %d", len(retained), maxPreservedExtensionBytes)
	}
	if strings.Contains(string(retained), marker) {
		t.Fatalf("boundary-truncated XML leaked sensitive marker: %q", retained)
	}
	if !strings.Contains(string(retained), "REDACTED") {
		t.Fatalf("boundary fixture did not retain a redaction marker")
	}
}
