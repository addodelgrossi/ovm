package device

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

// Decoding bounds. The transport already bounds the response body before it
// reaches a decoder, but the decoder enforces its own structural limits so a
// deeply nested or extremely wide operation payload cannot exhaust memory or
// stack while building the element tree.
const (
	maxDecodeDepth    = 64
	maxDecodeElements = 50000
	// maxPreservedExtensionBytes bounds the raw XML retained for an unknown or
	// optional extension element so preservation stays useful but cannot grow
	// without limit.
	maxPreservedExtensionBytes = 16 << 10

	// Aggregate decode caps. These are independent of maxDecodeDepth,
	// maxDecodeElements, and the transport body limit, and bound the *decoded*
	// volume a payload may expand into while the element tree is built. A
	// payload can stay within the transport byte limit yet still decode into a
	// disproportionate amount of retained text, attribute data, or node
	// complexity (for example through many short elements each carrying
	// attributes, or large accumulated character data). Enforcing these caps
	// at decode time keeps a single response from amplifying into excessive
	// retained memory even when every per-dimension structural bound is
	// individually satisfied.
	//
	//   - maxDecodedTextBytes bounds the sum of all CharData retained across
	//     the whole tree.
	//   - maxDecodedAttrBytes bounds the sum of all attribute names and values
	//     retained across the whole tree.
	//   - maxDecodeComplexity bounds a weighted node/attribute complexity
	//     score so a payload cannot combine many cheap-per-dimension nodes into
	//     an expensive whole.
	//   - maxDecodeTokens bounds parser work from fragmented text, comments,
	//     processing instructions, and other tokens that need not add retained
	//     bytes or elements.
	maxDecodedTextBytes = 1 << 20 // 1 MiB of accumulated character data
	maxDecodedAttrBytes = 1 << 20 // 1 MiB of accumulated attribute material
	maxDecodeComplexity = 1 << 21 // weighted structural complexity ceiling
	// maxDecodeTokens bounds parser work that does not necessarily increase
	// retained bytes or element count, such as many tiny CharData fragments
	// separated by comments or processing instructions.
	maxDecodeTokens = 200000

	// attributeComplexityWeight weights each retained attribute in the
	// aggregate complexity score so attribute-heavy payloads are bounded even
	// when their element count stays modest.
	attributeComplexityWeight = 8
)

// element is a parsed XML element with namespace-resolved identity. Prefixes
// are never retained for comparison: callers match on {namespace}local only,
// which gives intentional prefix tolerance without accepting foreign
// namespaces.
type element struct {
	name        xml.Name
	attrs       []xml.Attr
	text        []byte
	children    []*element
	startOffset int64
	endOffset   int64
}

// decodeOperation parses a standalone operation payload fragment (as provided
// by soap.Response.BodyXML) into an element tree. It rejects document type
// declarations and malformed XML, and never returns raw XML in its error.
func decodeOperation(data []byte) (*element, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true

	var (
		root     *element
		stack    []*element
		elements int

		// Aggregate accumulators enforced independently of the per-dimension
		// depth/element bounds. They bound the decoded text, attribute, and
		// complexity volume the whole payload may expand into.
		textBytes  int
		attrBytes  int
		complexity int
		tokens     int
	)
	for {
		before := decoder.InputOffset()
		token, err := decoder.Token()
		after := decoder.InputOffset()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrMalformedXML, sanitizeDecodeError(err))
		}
		tokens++
		if tokens > maxDecodeTokens {
			return nil, fmt.Errorf("%w: too many XML tokens", ErrMalformedXML)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			elements++
			if elements > maxDecodeElements {
				return nil, fmt.Errorf("%w: too many elements", ErrMalformedXML)
			}
			if len(stack) >= maxDecodeDepth {
				return nil, fmt.Errorf("%w: nesting too deep", ErrMalformedXML)
			}
			// Aggregate complexity: each element contributes a unit and each
			// attribute a weighted amount, with attribute material also counted
			// against the attribute-byte cap.
			complexity++
			for _, attr := range typed.Attr {
				attrBytes += len(attr.Name.Space) + len(attr.Name.Local) + len(attr.Value)
				complexity += attributeComplexityWeight
			}
			if attrBytes > maxDecodedAttrBytes {
				return nil, fmt.Errorf("%w: attribute volume too large", ErrMalformedXML)
			}
			if complexity > maxDecodeComplexity {
				return nil, fmt.Errorf("%w: structural complexity too large", ErrMalformedXML)
			}
			node := &element{
				name:        typed.Name,
				attrs:       append([]xml.Attr(nil), typed.Attr...),
				startOffset: before,
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, fmt.Errorf("%w: multiple root elements", ErrMalformedXML)
				}
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("%w: unmatched end element", ErrMalformedXML)
			}
			node := stack[len(stack)-1]
			node.endOffset = after
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				textBytes += len(typed)
				if textBytes > maxDecodedTextBytes {
					return nil, fmt.Errorf("%w: character data too large", ErrMalformedXML)
				}
				// Append to a byte slice rather than repeatedly concatenating an
				// immutable string. Geometric slice growth keeps fragmented
				// CharData accumulation linear in the retained text volume.
				stack[len(stack)-1].text = append(stack[len(stack)-1].text, typed...)
			}
		case xml.Directive:
			return nil, fmt.Errorf("%w: document type declarations are not allowed", ErrMalformedXML)
		}
	}
	if root == nil {
		return nil, fmt.Errorf("%w: empty operation payload", ErrMalformedXML)
	}
	if len(stack) != 0 {
		return nil, fmt.Errorf("%w: unclosed element", ErrMalformedXML)
	}
	return root, nil
}

// sanitizeDecodeError strips any payload content the XML decoder may embed in
// its error so decode failures never leak raw, unredacted response bytes. The
// classification uses errors.As so wrapped decoder errors are still recognized
// without matching on their rendered message.
func sanitizeDecodeError(err error) string {
	var (
		syntaxErr    *xml.SyntaxError
		tagPathErr   *xml.TagPathError
		unmarshalErr *xml.UnmarshalError
	)
	switch {
	case errors.As(err, &syntaxErr):
		return "XML syntax error"
	case errors.As(err, &tagPathErr):
		return "XML tag path error"
	case errors.As(err, &unmarshalErr):
		return "XML unmarshal error"
	default:
		return "XML decode error"
	}
}

// outerXML returns a view of the element's complete outer XML from the bounded
// operation payload. Callers must redact it before retaining or exposing a
// copy; truncating before redaction can cut through a sensitive opening tag and
// defeat lexical secret detection.
func (e *element) outerXML(data []byte) []byte {
	if e.startOffset < 0 || e.endOffset <= e.startOffset || e.endOffset > int64(len(data)) {
		return nil
	}
	return data[e.startOffset:e.endOffset]
}

// requireRoot checks that the operation root matches the expected Device
// namespace and local name, giving a safe typed error otherwise.
func requireRoot(root *element, local string) error {
	if root.name.Space != Namespace || root.name.Local != local {
		return fmt.Errorf("%w: expected {%s}%s, got {%s}%s",
			ErrUnexpectedElement, Namespace, local, root.name.Space, root.name.Local)
	}
	return nil
}

// child returns the first child matching {namespace}local, or nil.
func (e *element) child(namespace, local string) *element {
	for _, c := range e.children {
		if c.name.Space == namespace && c.name.Local == local {
			return c
		}
	}
	return nil
}

// childrenNamed returns every direct child matching {namespace}local in
// document order. The returned slice is independent of the element's own child
// slice so callers may retain or reorder it freely.
func (e *element) childrenNamed(namespace, local string) []*element {
	var out []*element
	for _, c := range e.children {
		if c.name.Space == namespace && c.name.Local == local {
			out = append(out, c)
		}
	}
	return out
}

// hasDuplicateChild reports whether more than one direct child matches
// {namespace}local. It is used to reject ambiguous payloads where a field that
// must appear at most once is repeated.
func (e *element) hasDuplicateChild(namespace, local string) bool {
	seen := 0
	for _, c := range e.children {
		if c.name.Space == namespace && c.name.Local == local {
			seen++
			if seen > 1 {
				return true
			}
		}
	}
	return false
}

// unqualifiedAttr returns the value of an attribute identified by its local
// name with no namespace qualifier, matching how ONVIF device responses carry
// identity attributes such as token. The boolean reports whether the attribute
// was present.
func (e *element) unqualifiedAttr(local string) (string, bool) {
	for _, attr := range e.attrs {
		if attr.Name.Space == "" && attr.Name.Local == local {
			return attr.Value, true
		}
	}
	return "", false
}

// trimmedText returns the element's accumulated character data with leading and
// trailing whitespace removed.
func (e *element) trimmedText() string {
	return string(bytes.TrimSpace(e.text))
}
