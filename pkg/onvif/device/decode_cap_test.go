package device

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// TestDecodeAggregateTextCapRejectsLargeCharData proves that a payload within
// the element and depth bounds is still rejected when its accumulated character
// data exceeds the aggregate text cap. The transport body limit and the
// element/depth bounds do not catch this on their own.
func TestDecodeAggregateTextCapRejectsLargeCharData(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("<Root>")
	// A single element whose text content exceeds the aggregate text cap.
	chunk := strings.Repeat("a", 64<<10) // 64 KiB per chunk
	chunks := (maxDecodedTextBytes / len(chunk)) + 2
	for i := 0; i < chunks; i++ {
		sb.WriteString(chunk)
	}
	sb.WriteString("</Root>")

	_, err := decodeOperation([]byte(sb.String()))
	if !errors.Is(err, ErrMalformedXML) {
		t.Fatalf("error = %v, want ErrMalformedXML for oversized character data", err)
	}
	if err != nil && len(err.Error()) > 100 {
		t.Fatalf("decode error appears to leak payload content: %q", err.Error())
	}
}

// TestDecodeAggregateAttrCapRejectsManyAttributes proves that attribute volume
// is bounded independently: many elements each carrying modest attributes stay
// under the element bound but their summed attribute bytes exceed the cap.
func TestDecodeAggregateAttrCapRejectsManyAttributes(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("<Root>")
	// Each child carries one attribute of ~1 KiB. Keep the child count under
	// maxDecodeElements while pushing attribute bytes over the cap.
	attrValue := strings.Repeat("v", 1<<10)
	children := (maxDecodedAttrBytes / len(attrValue)) + 2
	if children >= maxDecodeElements {
		t.Fatalf("test setup exceeds element bound (%d >= %d); adjust chunk size", children, maxDecodeElements)
	}
	for i := 0; i < children; i++ {
		sb.WriteString(`<C a="`)
		sb.WriteString(attrValue)
		sb.WriteString(`"/>`)
	}
	sb.WriteString("</Root>")

	_, err := decodeOperation([]byte(sb.String()))
	if !errors.Is(err, ErrMalformedXML) {
		t.Fatalf("error = %v, want ErrMalformedXML for oversized attribute volume", err)
	}
}

// TestDecodeComplexityCapRejectsManyAttributedElements proves that the weighted
// complexity score bounds a payload that combines many cheap-per-dimension
// attributed elements into an expensive whole, even when neither the element
// count nor the attribute-byte total alone would trip. The elements carry many
// tiny attributes so complexity (1 + attributeComplexityWeight per attribute)
// crosses its cap while attribute bytes and the element count stay bounded.
func TestDecodeComplexityCapRejectsManyAttributedElements(t *testing.T) {
	t.Parallel()
	const attrsPerChild = 12
	perChild := 1 + attrsPerChild*attributeComplexityWeight
	children := (maxDecodeComplexity / perChild) + 2
	if children >= maxDecodeElements {
		t.Fatalf("complexity cap unreachable within the element bound (%d children, bound %d); lower maxDecodeComplexity or raise attrsPerChild", children, maxDecodeElements)
	}

	var sb strings.Builder
	sb.WriteString("<Root>")
	for i := 0; i < children; i++ {
		sb.WriteString("<C")
		for a := 0; a < attrsPerChild; a++ {
			sb.WriteString(" a")
			sb.WriteString(strconv.Itoa(a))
			sb.WriteString(`="1"`)
		}
		sb.WriteString("/>")
	}
	sb.WriteString("</Root>")

	_, err := decodeOperation([]byte(sb.String()))
	if !errors.Is(err, ErrMalformedXML) {
		t.Fatalf("error = %v, want ErrMalformedXML for excessive structural complexity", err)
	}
}

// TestDecodeAggregateCapsAcceptModestPayload proves the caps do not reject a
// normal, modestly sized operation payload: the aggregate guards must not
// regress ordinary decoding.
func TestDecodeAggregateCapsAcceptModestPayload(t *testing.T) {
	t.Parallel()
	payload := `<GetHostnameResponse xmlns="` + Namespace + `">` +
		`<HostnameInformation><FromDHCP>false</FromDHCP><Name>camera-01</Name></HostnameInformation>` +
		`</GetHostnameResponse>`
	root, err := decodeOperation([]byte(payload))
	if err != nil {
		t.Fatalf("modest payload rejected: %v", err)
	}
	if root == nil || root.name.Local != "GetHostnameResponse" {
		t.Fatalf("unexpected root: %+v", root)
	}
}

// TestDecodeTokenCapRejectsFragmentedCharData proves that a peer cannot keep
// retained text below maxDecodedTextBytes while forcing unbounded parser work
// through tiny CharData fragments separated by comments. The byte-slice text
// accumulator is linear, and the independent token cap terminates the parse.
func TestDecodeTokenCapRejectsFragmentedCharData(t *testing.T) {
	t.Parallel()
	var payload strings.Builder
	payload.WriteString("<Root>")
	fragments := maxDecodeTokens/2 + 8
	for i := 0; i < fragments; i++ {
		payload.WriteString("x<!--separator-->")
	}
	payload.WriteString("</Root>")
	if fragments >= maxDecodedTextBytes {
		t.Fatalf("test setup text bytes %d must stay below text cap %d", fragments, maxDecodedTextBytes)
	}

	_, err := decodeOperation([]byte(payload.String()))
	if !errors.Is(err, ErrMalformedXML) {
		t.Fatalf("error = %v, want ErrMalformedXML for excessive fragmented tokens", err)
	}
	if err != nil && !strings.Contains(err.Error(), "too many XML tokens") {
		t.Fatalf("error = %q, want token-limit classification", err)
	}
}

func TestDecodeFragmentedCharDataBelowCapRemainsCorrect(t *testing.T) {
	t.Parallel()
	var payload strings.Builder
	payload.WriteString("<Root>")
	const fragments = 2048
	for i := 0; i < fragments; i++ {
		payload.WriteString("x<!--separator-->")
	}
	payload.WriteString("</Root>")

	root, err := decodeOperation([]byte(payload.String()))
	if err != nil {
		t.Fatalf("fragmented payload below caps rejected: %v", err)
	}
	if got := root.trimmedText(); len(got) != fragments {
		t.Fatalf("decoded text length = %d, want %d", len(got), fragments)
	}
}
