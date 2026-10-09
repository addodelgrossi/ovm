package soap

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestParseEnvelopeRejectsStructuralComplexityWithinByteLimit(t *testing.T) {
	tests := []struct {
		name string
		xml  func() string
	}{
		{
			name: "deep nesting",
			xml: func() string {
				depth := maxEnvelopeDepth + 1
				return `<e:Envelope xmlns:e="` + envelopeNamespace12 + `"><e:Body><Operation>` +
					strings.Repeat("<n>", depth) + strings.Repeat("</n>", depth) +
					`</Operation></e:Body></e:Envelope>`
			},
		},
		{
			name: "wide element count",
			xml: func() string {
				return `<e:Envelope xmlns:e="` + envelopeNamespace12 + `"><e:Body><Operation>` +
					strings.Repeat("<n/>", maxEnvelopeElements) +
					`</Operation></e:Body></e:Envelope>`
			},
		},
		{
			name: "namespaces in scope",
			xml: func() string {
				var b strings.Builder
				b.WriteString(`<e:Envelope xmlns:e="` + envelopeNamespace12 + `"`)
				for i := 0; i < maxNamespacesInScope; i++ {
					b.WriteString(` xmlns:p` + strconv.Itoa(i) + `="urn:synthetic:` + strconv.Itoa(i) + `"`)
				}
				b.WriteString(`><e:Body/></e:Envelope>`)
				return b.String()
			},
		},
		{
			name: "namespace clone work",
			xml: func() string {
				var b strings.Builder
				b.WriteString(`<e:Envelope xmlns:e="` + envelopeNamespace12 + `"`)
				for i := 0; i < 32; i++ {
					b.WriteString(` xmlns:r` + strconv.Itoa(i) + `="urn:root:` + strconv.Itoa(i) + `"`)
				}
				b.WriteString(`><e:Body><Operation>`)
				for i := 0; i < 2_000; i++ {
					b.WriteString(`<n xmlns:x="urn:child:` + strconv.Itoa(i) + `"/>`)
				}
				b.WriteString(`</Operation></e:Body></e:Envelope>`)
				return b.String()
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := []byte(test.xml())
			if len(data) >= int(DefaultMaxResponseBytes) {
				t.Fatalf("fixture is %d bytes, must remain below the transport limit", len(data))
			}
			_, err := parseEnvelope(data)
			if !errors.Is(err, ErrXMLComplexity) {
				t.Fatalf("parseEnvelope() error = %v, want ErrXMLComplexity", err)
			}
		})
	}
}

func TestParseEnvelopeSharesUnchangedNamespaceScopes(t *testing.T) {
	t.Parallel()

	data := []byte(`<e:Envelope xmlns:e="` + envelopeNamespace12 + `" xmlns:t="urn:test"><e:Body><t:Operation><t:A/><t:B/></t:Operation></e:Body></e:Envelope>`)
	root, err := parseXMLTree(data)
	if err != nil {
		t.Fatalf("parseXMLTree() error = %v", err)
	}
	body, err := requiredChild(root, envelopeNamespace12, "Body")
	if err != nil {
		t.Fatalf("Body error = %v", err)
	}
	if len(body.Children) != 1 || len(body.Children[0].Children) != 2 {
		t.Fatalf("unexpected tree shape")
	}
	if body.Children[0].Children[0].Namespaces["t"] != "urn:test" {
		t.Fatalf("inherited namespace was not retained")
	}
}
