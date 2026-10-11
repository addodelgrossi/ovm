package soap

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

const onvifErrorNamespace = "http://www.onvif.org/ver10/error"

const (
	maxEnvelopeDepth             = 96
	maxEnvelopeElements          = 50_000
	maxEnvelopeAttributes        = 100_000
	maxAttributesPerElement      = 128
	maxNamespacesInScope         = 64
	maxNamespaceCloneWork        = 64_000
	maxEnvelopeRetainedTextBytes = 2 << 20
)

type parsedEnvelope struct {
	Version Version
	BodyXML []byte
	Fault   *Fault
}

type xmlNode struct {
	Name               xml.Name
	Attributes         []xml.Attr
	Text               strings.Builder
	Children           []*xmlNode
	Namespaces         map[string]string
	DeclaredNamespaces map[string]bool
	startOffset        int64
	contentOffset      int64
	contentEndOffset   int64
	endOffset          int64
}

func buildEnvelope(version Version, headerXML, bodyXML []byte) ([]byte, error) {
	if version.EnvelopeNamespace() == "" {
		return nil, fmt.Errorf("unsupported SOAP version %q", version)
	}
	if containsDTD(headerXML) || containsDTD(bodyXML) {
		return nil, ErrDTDNotAllowed
	}
	var envelope bytes.Buffer
	envelope.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	envelope.WriteString(`<env:Envelope xmlns:env="`)
	envelope.WriteString(version.EnvelopeNamespace())
	envelope.WriteString(`">`)
	if len(bytes.TrimSpace(headerXML)) > 0 {
		envelope.WriteString(`<env:Header>`)
		envelope.Write(headerXML)
		envelope.WriteString(`</env:Header>`)
	}
	envelope.WriteString(`<env:Body>`)
	envelope.Write(bodyXML)
	envelope.WriteString(`</env:Body></env:Envelope>`)
	return envelope.Bytes(), nil
}

func parseEnvelope(data []byte) (parsedEnvelope, error) {
	root, err := parseXMLTree(data)
	if err != nil {
		return parsedEnvelope{}, err
	}
	if root.Name.Local != "Envelope" {
		return parsedEnvelope{}, fmt.Errorf("root element is %q, want Envelope", root.Name.Local)
	}
	version, ok := versionFromNamespace(root.Name.Space)
	if !ok {
		return parsedEnvelope{}, fmt.Errorf("unsupported SOAP envelope namespace %q", root.Name.Space)
	}
	body, err := requiredChild(root, root.Name.Space, "Body")
	if err != nil {
		return parsedEnvelope{}, err
	}
	if len(body.Children) == 0 {
		return parsedEnvelope{Version: version}, nil
	}
	if len(body.Children) != 1 {
		return parsedEnvelope{}, errors.New("SOAP Body must contain exactly one operation element")
	}
	operation := body.Children[0]
	operationXML, err := standaloneNodeXML(data, operation)
	if err != nil {
		return parsedEnvelope{}, fmt.Errorf("preserve SOAP operation XML: %w", err)
	}
	parsed := parsedEnvelope{Version: version, BodyXML: operationXML}
	if operation.Name.Local != "Fault" || operation.Name.Space != root.Name.Space {
		return parsed, nil
	}
	fault, err := parseFaultNode(version, operation, data)
	if err != nil {
		return parsedEnvelope{}, err
	}
	parsed.Fault = fault
	return parsed, nil
}

func parseXMLTree(data []byte) (*xmlNode, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	var (
		root               *xmlNode
		stack              []*xmlNode
		elementCount       int
		attributeCount     int
		retainedTextBytes  int
		namespaceCloneWork int
	)
	for {
		before := decoder.InputOffset()
		token, err := decoder.Token()
		after := decoder.InputOffset()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			elementCount++
			if elementCount > maxEnvelopeElements {
				return nil, fmt.Errorf("%w: too many elements", ErrXMLComplexity)
			}
			if len(stack) >= maxEnvelopeDepth {
				return nil, fmt.Errorf("%w: nesting is too deep", ErrXMLComplexity)
			}
			if len(typed.Attr) > maxAttributesPerElement {
				return nil, fmt.Errorf("%w: too many attributes on one element", ErrXMLComplexity)
			}
			attributeCount += len(typed.Attr)
			if attributeCount > maxEnvelopeAttributes {
				return nil, fmt.Errorf("%w: too many attributes", ErrXMLComplexity)
			}

			var parentNamespaces map[string]string
			if len(stack) == 0 {
				parentNamespaces = map[string]string{"xml": "http://www.w3.org/XML/1998/namespace"}
			} else {
				parentNamespaces = stack[len(stack)-1].Namespaces
			}
			declared := make(map[string]bool)
			type declaration struct{ prefix, namespace string }
			declarations := make([]declaration, 0)
			for _, attribute := range typed.Attr {
				prefix, namespace, isDeclaration := namespaceDeclaration(attribute)
				if isDeclaration {
					declarations = append(declarations, declaration{prefix: prefix, namespace: namespace})
					declared[prefix] = true
				}
			}

			// Namespace scopes are immutable after a node is created. Children
			// without declarations share the parent's map; only a declaration
			// requires a bounded clone. This avoids cloning a large inherited map
			// for every element in a wide document.
			namespaces := parentNamespaces
			if len(declarations) > 0 {
				namespaceCloneWork += len(parentNamespaces) + len(declarations)
				if namespaceCloneWork > maxNamespaceCloneWork {
					return nil, fmt.Errorf("%w: namespace processing is too complex", ErrXMLComplexity)
				}
				namespaces = cloneNamespaces(parentNamespaces)
				for _, declaration := range declarations {
					namespaces[declaration.prefix] = declaration.namespace
				}
			}
			if len(namespaces) > maxNamespacesInScope {
				return nil, fmt.Errorf("%w: too many namespaces in scope", ErrXMLComplexity)
			}
			if len(declared) == 0 {
				declared = nil
			}

			node := &xmlNode{
				Name:               typed.Name,
				Attributes:         append([]xml.Attr(nil), typed.Attr...),
				Namespaces:         namespaces,
				DeclaredNamespaces: declared,
				startOffset:        before,
				contentOffset:      after,
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, errors.New("SOAP XML contains multiple root elements")
				}
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, errors.New("SOAP XML contains an unmatched end element")
			}
			node := stack[len(stack)-1]
			node.contentEndOffset = before
			node.endOffset = after
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(typed)) != "" {
					return nil, errors.New("SOAP XML contains text outside the root element")
				}
				continue
			}
			retainedTextBytes += len(typed)
			if retainedTextBytes > maxEnvelopeRetainedTextBytes {
				return nil, fmt.Errorf("%w: too much retained text", ErrXMLComplexity)
			}
			_, _ = stack[len(stack)-1].Text.Write(typed)
		case xml.Directive:
			return nil, ErrDTDNotAllowed
		}
	}
	if root == nil {
		return nil, errors.New("SOAP XML has no root element")
	}
	if len(stack) != 0 {
		return nil, errors.New("SOAP XML contains an unclosed element")
	}
	return root, nil
}

func parseFaultNode(version Version, node *xmlNode, data []byte) (*Fault, error) {
	fault := &Fault{Version: version}
	switch version {
	case Version12:
		if err := parseSOAP12Fault(fault, node, data); err != nil {
			return nil, err
		}
	case Version11:
		if err := parseSOAP11Fault(fault, node, data); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported SOAP fault version %q", version)
	}
	fault.DetailXML = RedactXML(fault.DetailXML)
	return fault, nil
}

// parseSOAP12Fault enforces the SOAP 1.2 fault structure: exactly one Code with
// exactly one Value, exactly one Reason carrying one-or-more Text, at most one
// Subcode at each supported level with exactly one Value, and at most one Node,
// Role, and Detail. VersionMismatch is classified only from the top Code; ONVIF
// application kinds only from the deliberate direct Subcode position.
func parseSOAP12Fault(fault *Fault, node *xmlNode, data []byte) error {
	ns := Version12.EnvelopeNamespace()

	code, err := exactlyOneChild(node, ns, "Code")
	if err != nil {
		return fmt.Errorf("parse SOAP 1.2 fault code: %w", err)
	}
	value, err := exactlyOneChild(code, ns, "Value")
	if err != nil {
		return fmt.Errorf("parse SOAP 1.2 fault code value: %w", err)
	}
	fault.Code = RedactText(strings.TrimSpace(value.Text.String()))
	codeNamespace, codeLocal, codeResolved := resolveQName(value.Text.String(), value.Namespaces)
	if !codeResolved {
		return errors.New("SOAP 1.2 fault code Value is not a resolvable QName")
	}

	// At most one Subcode directly under Code. Only this direct Subcode is a
	// classification position, but every nested Subcode is recursively checked
	// for singleton Value/Subcode cardinality and a resolvable QName.
	var subcodeNamespace, subcodeLocal string
	if subcode, err := atMostOneChild(code, ns, "Subcode"); err != nil {
		return fmt.Errorf("parse SOAP 1.2 fault subcode: %w", err)
	} else if subcode != nil {
		var display string
		subcodeNamespace, subcodeLocal, display, err = parseSOAP12Subcode(subcode, ns)
		if err != nil {
			return fmt.Errorf("parse SOAP 1.2 fault subcode: %w", err)
		}
		fault.Subcode = RedactText(display)
	}

	// Exactly one Reason with one-or-more Text.
	reason, err := exactlyOneChild(node, ns, "Reason")
	if err != nil {
		return fmt.Errorf("parse SOAP 1.2 fault reason: %w", err)
	}
	texts := childrenNamed(reason, ns, "Text")
	if len(texts) == 0 {
		return errors.New("SOAP 1.2 fault Reason must contain at least one Text")
	}
	languages := make(map[string]bool, len(texts))
	for _, text := range texts {
		language := strings.ToLower(strings.TrimSpace(attributeValue(text, "http://www.w3.org/XML/1998/namespace", "lang")))
		if language == "" {
			return errors.New("SOAP 1.2 fault Reason Text requires xml:lang")
		}
		if languages[language] {
			return errors.New("SOAP 1.2 fault Reason contains duplicate xml:lang")
		}
		languages[language] = true
	}
	fault.Reason = RedactText(strings.TrimSpace(texts[0].Text.String()))

	// At most one Node, Role, and Detail.
	if _, err := atMostOneChild(node, ns, "Node"); err != nil {
		return fmt.Errorf("parse SOAP 1.2 fault node: %w", err)
	}
	if _, err := atMostOneChild(node, ns, "Role"); err != nil {
		return fmt.Errorf("parse SOAP 1.2 fault role: %w", err)
	}
	detail, err := atMostOneChild(node, ns, "Detail")
	if err != nil {
		return fmt.Errorf("parse SOAP 1.2 fault detail: %w", err)
	}
	if detail != nil {
		fault.DetailXML = nodeInnerXML(data, detail)
	}

	fault.Kind = classifySOAP12Fault(codeNamespace, codeLocal, subcodeNamespace, subcodeLocal)
	return nil
}

func parseSOAP12Subcode(subcode *xmlNode, namespace string) (string, string, string, error) {
	value, err := exactlyOneChild(subcode, namespace, "Value")
	if err != nil {
		return "", "", "", fmt.Errorf("fault Subcode Value: %w", err)
	}
	display := strings.TrimSpace(value.Text.String())
	resolvedNamespace, resolvedLocal, resolved := resolveQName(display, value.Namespaces)
	if !resolved {
		return "", "", "", errors.New("SOAP 1.2 fault Subcode Value is not a resolvable QName")
	}
	nested, err := atMostOneChild(subcode, namespace, "Subcode")
	if err != nil {
		return "", "", "", fmt.Errorf("nested fault Subcode: %w", err)
	}
	if nested != nil {
		if _, _, _, err := parseSOAP12Subcode(nested, namespace); err != nil {
			return "", "", "", err
		}
	}
	return resolvedNamespace, resolvedLocal, display, nil
}

// parseSOAP11Fault enforces the SOAP 1.1 fault structure: exactly one faultcode
// and faultstring, and at most one faultactor and detail. Classification uses
// the direct faultcode QName only; there is no descendant Detail scanning.
func parseSOAP11Fault(fault *Fault, node *xmlNode, data []byte) error {
	code, err := exactlyOneSOAP11FaultChild(node, "faultcode")
	if err != nil {
		return err
	}
	fault.Code = RedactText(strings.TrimSpace(code.Text.String()))
	codeNamespace, codeLocal, codeResolved := resolveQName(code.Text.String(), code.Namespaces)
	if !codeResolved {
		return errors.New("SOAP 1.1 faultcode is not a resolvable QName")
	}

	reason, err := exactlyOneSOAP11FaultChild(node, "faultstring")
	if err != nil {
		return err
	}
	fault.Reason = RedactText(strings.TrimSpace(reason.Text.String()))

	if _, err := atMostOneSOAP11FaultChild(node, "faultactor"); err != nil {
		return err
	}
	detail, err := atMostOneSOAP11FaultChild(node, "detail")
	if err != nil {
		return err
	}
	if detail != nil {
		fault.DetailXML = nodeInnerXML(data, detail)
	}

	// SOAP 1.1 classifies only from the direct faultcode QName.
	fault.Kind = classifySOAP11Fault(codeNamespace, codeLocal)
	return nil
}

func attributeValue(node *xmlNode, namespace, local string) string {
	for _, attribute := range node.Attributes {
		if attribute.Name.Space == namespace && attribute.Name.Local == local {
			return attribute.Value
		}
	}
	return ""
}

func childrenNamed(parent *xmlNode, namespace, local string) []*xmlNode {
	var found []*xmlNode
	for _, child := range parent.Children {
		if child.Name.Local == local && child.Name.Space == namespace {
			found = append(found, child)
		}
	}
	return found
}

func requiredChild(parent *xmlNode, namespace, local string) (*xmlNode, error) {
	var found *xmlNode
	for _, child := range parent.Children {
		if child.Name.Local != local || child.Name.Space != namespace {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("duplicate {%s}%s element", namespace, local)
		}
		found = child
	}
	if found == nil {
		return nil, fmt.Errorf("missing {%s}%s element", namespace, local)
	}
	return found, nil
}

// exactlyOneChild requires precisely one matching child.
func exactlyOneChild(parent *xmlNode, namespace, local string) (*xmlNode, error) {
	return requiredChild(parent, namespace, local)
}

// atMostOneChild permits zero or one matching child and rejects duplicates.
func atMostOneChild(parent *xmlNode, namespace, local string) (*xmlNode, error) {
	var found *xmlNode
	for _, child := range parent.Children {
		if child.Name.Local != local || child.Name.Space != namespace {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("duplicate {%s}%s element", namespace, local)
		}
		found = child
	}
	return found, nil
}

func soap11FaultChildren(parent *xmlNode, local string) []*xmlNode {
	var found []*xmlNode
	for _, child := range parent.Children {
		if child.Name.Local == local && (child.Name.Space == "" || child.Name.Space == envelopeNamespace11) {
			found = append(found, child)
		}
	}
	return found
}

func exactlyOneSOAP11FaultChild(parent *xmlNode, local string) (*xmlNode, error) {
	children := soap11FaultChildren(parent, local)
	switch len(children) {
	case 0:
		return nil, fmt.Errorf("missing SOAP 1.1 %s element", local)
	case 1:
		return children[0], nil
	default:
		return nil, fmt.Errorf("duplicate SOAP 1.1 %s element", local)
	}
}

func atMostOneSOAP11FaultChild(parent *xmlNode, local string) (*xmlNode, error) {
	children := soap11FaultChildren(parent, local)
	switch len(children) {
	case 0:
		return nil, nil
	case 1:
		return children[0], nil
	default:
		return nil, fmt.Errorf("duplicate SOAP 1.1 %s element", local)
	}
}

func resolveQName(value string, namespaces map[string]string) (string, string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return "", "", false
	}
	prefix, local := "", value
	if separator := strings.IndexByte(value, ':'); separator >= 0 {
		if strings.Contains(value[separator+1:], ":") {
			return "", "", false
		}
		prefix, local = value[:separator], value[separator+1:]
	}
	if local == "" {
		return "", "", false
	}
	namespace, ok := namespaces[prefix]
	return namespace, local, ok
}

func standaloneNodeXML(data []byte, node *xmlNode) ([]byte, error) {
	if node.startOffset < 0 || node.endOffset > int64(len(data)) || node.startOffset >= node.endOffset {
		return nil, errors.New("invalid XML node offsets")
	}
	raw := append([]byte(nil), data[node.startOffset:node.endOffset]...)
	missing := make([]string, 0, len(node.Namespaces))
	for prefix := range node.Namespaces {
		if prefix != "xml" && !node.DeclaredNamespaces[prefix] {
			missing = append(missing, prefix)
		}
	}
	if len(missing) == 0 {
		return raw, nil
	}
	sort.Strings(missing)
	insertion, err := startTagInsertion(raw)
	if err != nil {
		return nil, err
	}
	var declarations bytes.Buffer
	for _, prefix := range missing {
		declarations.WriteString(` xmlns`)
		if prefix != "" {
			declarations.WriteByte(':')
			declarations.WriteString(prefix)
		}
		declarations.WriteString(`="`)
		if err := xml.EscapeText(&declarations, []byte(node.Namespaces[prefix])); err != nil {
			return nil, err
		}
		declarations.WriteByte('"')
	}
	standalone := make([]byte, 0, len(raw)+declarations.Len())
	standalone = append(standalone, raw[:insertion]...)
	standalone = append(standalone, declarations.Bytes()...)
	standalone = append(standalone, raw[insertion:]...)
	return standalone, nil
}

func startTagInsertion(data []byte) (int, error) {
	quoted := byte(0)
	for index, character := range data {
		if quoted != 0 {
			if character == quoted {
				quoted = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			quoted = character
			continue
		}
		if character == '>' {
			if index > 0 && data[index-1] == '/' {
				return index - 1, nil
			}
			return index, nil
		}
	}
	return 0, errors.New("operation start tag is incomplete")
}

func nodeInnerXML(data []byte, node *xmlNode) []byte {
	if node.contentOffset < 0 || node.contentEndOffset > int64(len(data)) || node.contentOffset > node.contentEndOffset {
		return nil
	}
	return append([]byte(nil), data[node.contentOffset:node.contentEndOffset]...)
}

func namespaceDeclaration(attribute xml.Attr) (string, string, bool) {
	if attribute.Name.Space == "xmlns" {
		return attribute.Name.Local, attribute.Value, true
	}
	if attribute.Name.Space == "" && attribute.Name.Local == "xmlns" {
		return "", attribute.Value, true
	}
	return "", "", false
}

func cloneNamespaces(source map[string]string) map[string]string {
	copy := make(map[string]string, len(source))
	for prefix, namespace := range source {
		copy[prefix] = namespace
	}
	return copy
}

func containsDTD(data []byte) bool {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return false
		}
		if err != nil {
			// Fragments can contain multiple roots or inherited prefixes. A lexical
			// check remains conservative only after tokenization cannot continue.
			return strings.Contains(strings.ToLower(string(data)), "<!doctype")
		}
		if _, ok := token.(xml.Directive); ok {
			return true
		}
	}
}
