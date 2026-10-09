package discovery

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// maxDatagramSize bounds a single WS-Discovery datagram. UDP payloads larger
// than this are rejected to avoid unbounded buffering of untrusted input.
const maxDatagramSize = 64 * 1024

// maxScopes bounds the number of scope entries retained from a single message.
const maxScopes = 256

// maxXAddrs bounds the number of transport addresses retained from a single
// message.
const maxXAddrs = 64

// maxFieldLength bounds the length of individual scalar fields (addresses,
// scopes, endpoint references) retained from untrusted messages.
const maxFieldLength = 2048

// maxNamespaceDecls bounds the number of in-scope namespace declarations a
// single element node retains. The bound keeps a hostile datagram that spams
// xmlns declarations from forcing unbounded per-node allocation while still
// covering any realistic WS-Discovery envelope.
const maxNamespaceDecls = 64

// maxAttrs bounds the number of attributes a single element node retains from
// untrusted input, so a datagram cannot force unbounded per-node allocation by
// spamming attributes.
const maxAttrs = 64

// WS-Addressing RelatesTo RelationshipType values. The default relationship for
// a RelatesTo that omits the attribute is "Reply"; a reply to a Probe uses this
// default or names it explicitly.
const (
	relationshipReplyLocal = "Reply"
	// relationshipReply2005URI is the absolute WS-Addressing 2005/08 reply
	// relationship URI. In the 2005/08 family the Reply relationship is named
	// by this absolute IRI rather than by a QName local part.
	relationshipReply2005URI = nsAddressing2005 + "/reply"
)

// messageKind identifies the WS-Discovery message body a datagram carried.
type messageKind int

const (
	kindUnknown messageKind = iota
	kindProbeMatch
	kindHello
	kindBye
)

// appSequence holds the WS-Discovery AppSequence header fields. AppSequence
// lets a receiver suppress stale or duplicate Hello/Bye: InstanceId identifies
// a device boot generation, MessageNumber orders messages within it, and the
// optional SequenceId namespaces the message numbers.
type appSequence struct {
	InstanceID    uint64
	SequenceID    string
	MessageNumber uint64
}

// parsedMessage is the trusted projection of a single WS-Discovery datagram
// after bounding and validation.
type parsedMessage struct {
	Kind               messageKind
	DiscoveryNamespace string
	EndpointReference  string
	// Action is the namespace-resolved WS-Addressing Action URI carried in the
	// SOAP header, or "" when the datagram omitted it. Correlation and
	// lifecycle validation compare against this exact value.
	Action string
	// MessageID is the WS-Addressing MessageID of this datagram, or "" when
	// absent. It is retained so a passive listener can dedupe repeated
	// announcements of the same logical message.
	MessageID string
	// RelatesTo is the WS-Addressing RelatesTo value carried in the SOAP
	// header, or "" when absent. For an active ProbeMatches it must equal the
	// MessageID of the probe this datagram answers.
	RelatesTo string
	// RelatesToType is the resolved RelationshipType of the RelatesTo header. An
	// absent attribute defaults to "Reply". It is retained so callers can
	// require a Reply (default or explicit) relationship before correlating.
	RelatesToType string
	// ReplyRelationship reports whether RelatesTo carries the default or an
	// explicit "Reply" relationship. Correlation uses it so a non-Reply
	// relationship (for example a custom response type) is not mistaken for an
	// answer to our probe.
	ReplyRelationship bool
	// Types holds the advertised types as stable expanded-name strings of the
	// form "{namespace}local", resolved against the message's in-scope
	// namespace declarations so prefix variants deduplicate.
	Types  []string
	Scopes []string
	XAddrs []string
	// MetadataVersion is the raw textual MetadataVersion retained for public
	// output compatibility. Selection and comparison use MetadataVersionNum.
	MetadataVersion string
	// MetadataVersionNum is the parsed non-negative numeric MetadataVersion.
	// HasMetadataVersion reports whether a valid value was present; a malformed
	// or negative value is rejected and leaves this zero with the flag unset.
	MetadataVersionNum uint64
	HasMetadataVersion bool
	// AppSequence holds the parsed WS-Discovery AppSequence header fields.
	// HasAppSequence reports whether a well-formed AppSequence was present; a
	// malformed one is rejected and leaves this zero with the flag unset.
	AppSequence    appSequence
	HasAppSequence bool
}

// nsDecl is a single in-scope XML namespace declaration (prefix to URI). The
// empty prefix is the default namespace.
type nsDecl struct {
	Prefix string
	URI    string
}

// attr is a single retained XML attribute: its namespace URI, local name, and
// value. Attributes are retained boundedly so header fields such as the
// RelatesTo RelationshipType and the AppSequence counters can be read after the
// walk without re-parsing.
type attr struct {
	Space string
	Local string
	Value string
}

// node is a minimal namespace-resolved XML element used to walk WS-Discovery
// messages without binding to a fixed prefix or namespace revision. Each node
// retains the namespace declarations that were in scope at the element so that
// prefixed QName content (such as Types) can be resolved deterministically.
type node struct {
	Space    string
	Local    string
	Text     string
	Children []*node
	// Attrs holds the element's non-namespace attributes, bounded by maxAttrs.
	Attrs []attr
	// attrsTruncated reports that this element carried more non-namespace
	// attributes than maxAttrs, so Attrs is an incomplete view. A critical
	// attribute (a RelatesTo RelationshipType, an AppSequence counter) might
	// have been among those dropped; parsing treats such an element as
	// malformed rather than letting a critical attribute silently disappear.
	attrsTruncated bool
	// nsScope holds the namespace declarations in scope at this element,
	// inherited from ancestors and extended by this element's own xmlns
	// attributes. It is bounded by maxNamespaceDecls.
	nsScope []nsDecl
}

// resolvePrefix resolves an XML prefix against the node's in-scope namespace
// declarations. The empty prefix resolves to the default namespace. It returns
// the namespace URI and whether a binding was found. The reserved "xml" prefix
// always resolves to its fixed namespace.
func (n *node) resolvePrefix(prefix string) (string, bool) {
	if prefix == "xml" {
		return "http://www.w3.org/XML/1998/namespace", true
	}
	// Later declarations shadow earlier ones for the same prefix, so scan from
	// the end (closest scope wins).
	for i := len(n.nsScope) - 1; i >= 0; i-- {
		if n.nsScope[i].Prefix == prefix {
			return n.nsScope[i].URI, n.nsScope[i].URI != ""
		}
	}
	return "", false
}

// parseMessage validates and projects a WS-Discovery datagram into a single
// message. It is a compatibility helper for callers and tests that expect at
// most one message per datagram: for a ProbeMatches datagram carrying several
// matches it returns the first valid match. Discover must instead use
// parseMessages so that every match in a datagram is aggregated.
//
// The datagram is rejected when it exceeds the size bound, contains a DTD, is
// not well formed, or does not carry a recognised WS-Discovery body.
func parseMessage(data []byte) (parsedMessage, error) {
	messages, err := parseMessages(data)
	if err != nil {
		return parsedMessage{}, err
	}
	return messages[0], nil
}

// parseMessages validates and projects a WS-Discovery datagram into one or
// more messages. A ProbeMatches datagram may carry multiple ProbeMatch
// children; every valid match is returned, in document order. The resolved
// WS-Addressing Action, MessageID, RelatesTo, and AppSequence from the SOAP
// header are copied onto every message so the caller can correlate and
// validate without re-parsing.
//
// Header coherence: a datagram is rejected when it carries a duplicate Action
// or a duplicate RelatesTo (whether the duplicates agree or conflict), because
// a well-formed WS-Addressing header has at most one of each and a repeated
// value is a sign of a spoofed or malformed message.
//
// Family coherence is STRICT: a message speaks exactly one generation
// throughout. The body's discovery namespace fixes the one WS-Addressing
// namespace that every addressing element (Action, RelatesTo, MessageID,
// EndpointReference/Address, and any nested addressing element) must use, and
// the one discovery namespace that every discovery element (the ProbeMatches
// wrapper, each ProbeMatch child, AppSequence, and every nested discovery
// element) must use:
//
//   - WS-Discovery 2005/04 pairs ONLY with WS-Addressing 2004/08;
//   - WS-Discovery 2009/01 pairs ONLY with WS-Addressing 2005/08.
//
// Any cross-pairing - a 2005/04 body with a 2005/08 addressing header, a
// 2009/01 body with a 2004/08 Action, an EndpointReference from the wrong
// addressing family, an AppSequence or ProbeMatch child from the wrong
// discovery family, or a nested element from either other family - makes the
// datagram malformed and is rejected. There is no tolerance for cross-pairing.
//
// Malformed-match policy: within a ProbeMatches envelope an individual match
// that lacks an EndpointReference is skipped while the remaining valid matches
// are kept. The datagram is rejected only when no valid match remains.
func parseMessages(data []byte) ([]parsedMessage, error) {
	if len(data) > maxDatagramSize {
		return nil, ErrMessageTooLarge
	}
	root, err := parseTree(data)
	if err != nil {
		return nil, err
	}
	if !isSOAPEnvelopeNamespace(root.Space) || root.Local != "Envelope" {
		return nil, ErrMalformedMessage
	}
	header := findChild(root, isSOAPEnvelopeNamespace, "Header")
	body := findChild(root, isSOAPEnvelopeNamespace, "Body")
	if body == nil {
		return nil, ErrMalformedMessage
	}

	bodyNodes, kind := discoveryBodies(body)
	if kind == kindUnknown || len(bodyNodes) == 0 {
		return nil, ErrMalformedMessage
	}

	// The discovery family is fixed by the body. It determines the single
	// coherent addressing namespace the whole message must use. All body nodes
	// share one family because discoveryBodies draws them from one wrapper.
	discoveryNS := bodyNodes[0].Space
	wantAddressing := addressingForDiscovery(discoveryNS)
	if wantAddressing == "" {
		return nil, ErrMalformedMessage
	}

	// Validate the complete SOAP body, not only the selected ProbeMatch/Hello/Bye
	// node. This catches a mismatched ProbeMatches wrapper, a second body element
	// from another discovery generation, and wrong-family addressing nested at
	// any depth.
	if !familyCoherent(body, discoveryNS, wantAddressing) {
		return nil, ErrMalformedMessage
	}

	// The header's addressing elements must all use the paired addressing
	// namespace, and any WS-Discovery element in the header (AppSequence) must
	// use the body's discovery namespace. A header element from the other
	// generation is a cross-pairing and is rejected.
	hdr, err := parseHeader(header, discoveryNS, wantAddressing)
	if err != nil {
		return nil, err
	}

	messages := make([]parsedMessage, 0, len(bodyNodes))
	for _, bodyNode := range bodyNodes {
		// Reject a body whose own namespace drifted from the fixed family or
		// that nests a discovery/addressing element from any other family.
		if bodyNode.Space != discoveryNS {
			return nil, ErrMalformedMessage
		}
		if !familyCoherent(bodyNode, discoveryNS, wantAddressing) {
			return nil, ErrMalformedMessage
		}
		message, ok := projectMessage(bodyNode, kind)
		if !ok {
			// Skip an individual match that cannot be keyed; keep the rest.
			continue
		}
		message.Action = hdr.action
		message.MessageID = hdr.messageID
		message.RelatesTo = hdr.relatesTo
		message.RelatesToType = hdr.relatesToType
		message.ReplyRelationship = hdr.replyRelationship
		message.AppSequence = hdr.appSequence
		message.HasAppSequence = hdr.hasAppSequence
		messages = append(messages, message)
	}
	if len(messages) == 0 {
		return nil, ErrMalformedMessage
	}
	return messages, nil
}

// parsedHeader is the validated projection of the SOAP/WS-Addressing header.
type parsedHeader struct {
	action            string
	messageID         string
	relatesTo         string
	relatesToType     string
	replyRelationship bool
	appSequence       appSequence
	hasAppSequence    bool
}

// parseHeader validates and projects the SOAP header against the fixed family.
// It rejects a header that repeats Action, RelatesTo, or MessageID (duplicates,
// agreeing or conflicting, are not well formed), rejects an addressing element
// that is NOT in the paired addressing namespace wantAddressing (a
// cross-pairing), resolves the RelatesTo RelationshipType (defaulting to
// Reply), and parses the WS-Discovery AppSequence in the body's discovery
// namespace discoveryNS. A nil header (no SOAP Header element) is valid and
// yields the zero projection.
func parseHeader(header *node, discoveryNS, wantAddressing string) (parsedHeader, error) {
	var hdr parsedHeader
	if header == nil {
		return hdr, nil
	}
	// Reject wrong-family discovery or addressing elements anywhere in the
	// header subtree, including nested EndpointReference containers. Checking
	// the whole subtree avoids treating an incoherent nested header as an
	// irrelevant extension.
	if !familyCoherent(header, discoveryNS, wantAddressing) {
		return parsedHeader{}, ErrMalformedMessage
	}

	// Action, RelatesTo, and MessageID must each appear at most once, counted
	// strictly within the paired addressing namespace.
	actionNode, actionDup := singleAddressingChild(header, wantAddressing, "Action")
	if actionDup {
		return parsedHeader{}, ErrMalformedMessage
	}
	relatesNode, relatesDup := singleAddressingChild(header, wantAddressing, "RelatesTo")
	if relatesDup {
		return parsedHeader{}, ErrMalformedMessage
	}
	messageNode, messageDup := singleAddressingChild(header, wantAddressing, "MessageID")
	if messageDup {
		return parsedHeader{}, ErrMalformedMessage
	}

	if actionNode != nil {
		hdr.action = truncateField(strings.TrimSpace(actionNode.Text))
	}
	if messageNode != nil {
		hdr.messageID = truncateField(strings.TrimSpace(messageNode.Text))
	}
	if relatesNode != nil {
		hdr.relatesTo = truncateField(strings.TrimSpace(relatesNode.Text))
		relType, reply, relErr := relationshipType(relatesNode, wantAddressing)
		if relErr != nil {
			return parsedHeader{}, relErr
		}
		hdr.relatesToType, hdr.replyRelationship = relType, reply
	} else {
		// No RelatesTo: there is no relationship to classify. ReplyRelationship
		// stays false; callers that require correlation also require RelatesTo.
		hdr.relatesToType = ""
	}

	seq, hasSeq, err := parseAppSequence(header, discoveryNS)
	if err != nil {
		return parsedHeader{}, err
	}
	hdr.appSequence = seq
	hdr.hasAppSequence = hasSeq
	return hdr, nil
}

// otherAddressingNamespace returns the WS-Addressing namespace that is NOT the
// coherent one, or "" when the input is not a recognised addressing namespace.
func otherAddressingNamespace(wantAddressing string) string {
	switch wantAddressing {
	case nsAddressing2004:
		return nsAddressing2005
	case nsAddressing2005:
		return nsAddressing2004
	default:
		return ""
	}
}

// singleAddressingChild returns the single header child with the given local
// name in the paired addressing namespace wantAddressing and reports whether
// more than one was present. A duplicate (two or more) signals a malformed
// header. Elements in the other addressing namespace are rejected earlier in
// parseHeader, so counting is confined to the coherent family here.
func singleAddressingChild(header *node, wantAddressing, local string) (*node, bool) {
	var found *node
	count := 0
	for _, child := range header.Children {
		if child.Local == local && child.Space == wantAddressing {
			count++
			if found == nil {
				found = child
			}
		}
	}
	return found, count > 1
}

// relationshipType validates and preserves a RelatesTo RelationshipType for
// the paired addressing family. An ABSENT attribute defaults to Reply. An
// explicitly present value must be structurally valid for its generation:
//
//   - WS-Addressing 2004/08 uses a lexical QName (with an unqualified local
//     token tolerated for deployed devices). A prefixed QName must have a
//     declared prefix. The conventional absolute 2004 reply URI is also
//     tolerated for interoperability.
//   - WS-Addressing 2005/08 uses an absolute URI.
//
// Valid custom relationship values are preserved but are not classified as
// Reply. Empty values, malformed or undeclared QNames, cross-family addressing
// QNames, invalid URIs, duplicate RelationshipType attributes, and an
// attribute set truncated by the parser cap make the datagram malformed.
func relationshipType(relatesNode *node, wantAddressing string) (string, bool, error) {
	raw, present, err := criticalRelationshipAttribute(relatesNode, wantAddressing)
	if err != nil {
		return "", false, err
	}
	if !present {
		return relationshipReplyLocal, true, nil
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false, ErrMalformedMessage
	}

	switch wantAddressing {
	case nsAddressing2005:
		parsed, parseErr := url.Parse(trimmed)
		if parseErr != nil || !parsed.IsAbs() || parsed.Scheme == "" {
			return "", false, ErrMalformedMessage
		}
		return truncateField(trimmed), trimmed == relationshipReply2005URI, nil
	case nsAddressing2004:
		if trimmed == nsAddressing2004+"/reply" {
			return truncateField(trimmed), true, nil
		}
		prefix, local, ok := parseLexicalQName(trimmed)
		if !ok {
			return "", false, ErrMalformedMessage
		}
		if prefix == "" {
			return truncateField(trimmed), local == relationshipReplyLocal, nil
		}
		uri, declared := relatesNode.resolvePrefix(prefix)
		if !declared {
			return "", false, ErrMalformedMessage
		}
		if isAddressingNamespace(uri) && uri != wantAddressing {
			return "", false, ErrMalformedMessage
		}
		return truncateField(trimmed), uri == wantAddressing && local == relationshipReplyLocal, nil
	default:
		return "", false, ErrMalformedMessage
	}
}

// criticalRelationshipAttribute returns the one RelationshipType attribute
// applicable to this RelatesTo element. The schema normally leaves this
// attribute unqualified; accepting the paired addressing namespace is harmless
// interoperability tolerance. A duplicate or wrong-addressing-family spelling
// is ambiguous and rejected.
func criticalRelationshipAttribute(n *node, wantAddressing string) (string, bool, error) {
	if n.attrsTruncated {
		return "", false, ErrMalformedMessage
	}
	var (
		value string
		count int
	)
	for _, a := range n.Attrs {
		if a.Local != "RelationshipType" {
			continue
		}
		if isAddressingNamespace(a.Space) && a.Space != wantAddressing {
			return "", false, ErrMalformedMessage
		}
		if a.Space != "" && a.Space != wantAddressing {
			continue
		}
		count++
		value = a.Value
	}
	if count > 1 {
		return "", false, ErrMalformedMessage
	}
	return value, count == 1, nil
}

// parseLexicalQName validates a conservative XML QName spelling and returns
// its prefix and local part. XML tokenization has already guaranteed valid
// UTF-8; the checks here reject empty components, multiple colons, and
// characters that cannot occur in an NCName.
func parseLexicalQName(raw string) (prefix, local string, ok bool) {
	if strings.Count(raw, ":") > 1 {
		return "", "", false
	}
	prefix, local = splitQName(raw)
	if local == "" || !validNCName(local) {
		return "", "", false
	}
	if prefix != "" && !validNCName(prefix) {
		return "", "", false
	}
	return prefix, local, true
}

func validNCName(value string) bool {
	first := true
	for _, r := range value {
		if first {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			first = false
			continue
		}
		if r != '_' && r != '-' && r != '.' && !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r) {
			return false
		}
	}
	return !first
}

// parseAppSequence locates and parses the WS-Discovery AppSequence header in
// the body's discovery namespace discoveryNS. It returns ok=false when no
// AppSequence is present. A present AppSequence with a malformed or missing
// InstanceId/MessageNumber is rejected (returns an error) so a hostile peer
// cannot smuggle an unparsable sequence past suppression. A duplicate
// AppSequence (two or more) is also rejected.
func parseAppSequence(header *node, discoveryNS string) (appSequence, bool, error) {
	var seqNode *node
	count := 0
	for _, child := range header.Children {
		if child.Local == "AppSequence" && child.Space == discoveryNS {
			count++
			if seqNode == nil {
				seqNode = child
			}
		}
	}
	if count > 1 {
		return appSequence{}, false, ErrMalformedMessage
	}
	if seqNode == nil {
		return appSequence{}, false, nil
	}
	if seqNode.attrsTruncated {
		// An AppSequence counter might have been dropped by the attribute cap.
		// A critical ordering attribute cannot be trusted absent, so the
		// datagram is malformed rather than silently losing suppression state.
		return appSequence{}, false, ErrMalformedMessage
	}
	instanceRaw, hasInstance, err := criticalSequenceAttribute(seqNode, discoveryNS, "InstanceId")
	if err != nil {
		return appSequence{}, false, err
	}
	numberRaw, hasNumber, err := criticalSequenceAttribute(seqNode, discoveryNS, "MessageNumber")
	if err != nil {
		return appSequence{}, false, err
	}
	if !hasInstance || !hasNumber {
		return appSequence{}, false, ErrMalformedMessage
	}
	instance, err := strconv.ParseUint(strings.TrimSpace(instanceRaw), 10, 64)
	if err != nil {
		return appSequence{}, false, ErrMalformedMessage
	}
	number, err := strconv.ParseUint(strings.TrimSpace(numberRaw), 10, 64)
	if err != nil {
		return appSequence{}, false, ErrMalformedMessage
	}
	sequenceID, hasSequenceID, err := criticalSequenceAttribute(seqNode, discoveryNS, "SequenceId")
	if err != nil {
		return appSequence{}, false, err
	}
	if hasSequenceID {
		sequenceID = truncateField(strings.TrimSpace(sequenceID))
	}
	return appSequence{InstanceID: instance, SequenceID: sequenceID, MessageNumber: number}, true, nil
}

// criticalSequenceAttribute returns one unqualified (or same-family qualified)
// AppSequence attribute. Duplicate or wrong-discovery-family spellings are
// malformed because ordering must never depend on which conflicting value was
// encountered first.
func criticalSequenceAttribute(n *node, discoveryNS, local string) (string, bool, error) {
	var (
		value string
		count int
	)
	for _, a := range n.Attrs {
		if a.Local != local {
			continue
		}
		if isDiscoveryNamespace(a.Space) && a.Space != discoveryNS {
			return "", false, ErrMalformedMessage
		}
		if a.Space != "" && a.Space != discoveryNS {
			continue
		}
		count++
		value = a.Value
	}
	if count > 1 {
		return "", false, ErrMalformedMessage
	}
	return value, count == 1, nil
}

// familyCoherent reports whether a discovery body subtree uses only the fixed
// family throughout: every discovery-namespaced element is discoveryNS and
// every addressing-namespaced element is wantAddressing. An element from the
// other discovery family or the other addressing family anywhere in the subtree
// makes the message incoherent. The check walks the whole subtree so a nested
// cross-family element cannot hide beneath a coherent parent.
func familyCoherent(bodyNode *node, discoveryNS, wantAddressing string) bool {
	otherDisc := otherDiscoveryNamespace(discoveryNS)
	otherAddr := otherAddressingNamespace(wantAddressing)
	var walk func(n *node) bool
	walk = func(n *node) bool {
		if otherDisc != "" && n.Space == otherDisc {
			return false
		}
		if otherAddr != "" && n.Space == otherAddr {
			return false
		}
		for _, child := range n.Children {
			if !walk(child) {
				return false
			}
		}
		return true
	}
	return walk(bodyNode)
}

// otherDiscoveryNamespace returns the opposite recognised discovery namespace,
// or "" when the input is not a recognised discovery namespace.
func otherDiscoveryNamespace(namespace string) string {
	switch namespace {
	case nsDiscovery2005:
		return nsDiscovery2009
	case nsDiscovery2009:
		return nsDiscovery2005
	default:
		return ""
	}
}

// projectMessage builds a bounded parsedMessage from one discovery body
// element. It reports ok=false when the element carries no EndpointReference,
// since such a message cannot be keyed deterministically.
func projectMessage(bodyNode *node, kind messageKind) (parsedMessage, bool) {
	message := parsedMessage{Kind: kind, DiscoveryNamespace: bodyNode.Space}
	message.EndpointReference = endpointReference(bodyNode)
	message.Types = resolveTypeQNames(bodyNode, maxScopes)
	message.Scopes = boundedFields(fieldTokens(bodyNode, "Scopes"), maxScopes)
	message.XAddrs = boundedFields(fieldTokens(bodyNode, "XAddrs"), maxXAddrs)
	metadataText := truncateField(firstChildText(bodyNode, "MetadataVersion"))
	if num, ok := parseMetadataVersion(metadataText); ok {
		message.MetadataVersion = strconv.FormatUint(num, 10)
		message.MetadataVersionNum = num
		message.HasMetadataVersion = true
	}
	if message.EndpointReference == "" {
		return parsedMessage{}, false
	}
	return message, true
}

// parseMetadataVersion parses a WS-Discovery MetadataVersion. WS-Discovery
// defines the version as an unsigned integer (xs:unsignedInt); malformed,
// signed, or non-numeric values are rejected by returning ok=false so a hostile
// or buggy peer cannot win selection with a non-numeric sentinel. An empty
// value is treated as absent (ok=false) rather than as zero.
func parseMetadataVersion(raw string) (uint64, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}
	num, err := strconv.ParseUint(trimmed, 10, 32)
	if err != nil {
		return 0, false
	}
	return num, true
}

// discoveryBodies locates the WS-Discovery body elements and classifies them.
// For ProbeMatches it returns every ProbeMatch child; for a bare ProbeMatch,
// Hello, or Bye it returns the single element.
func discoveryBodies(body *node) ([]*node, messageKind) {
	for _, child := range body.Children {
		if !isDiscoveryNamespace(child.Space) {
			continue
		}
		switch child.Local {
		case "ProbeMatches":
			var matches []*node
			for _, match := range child.Children {
				if isDiscoveryNamespace(match.Space) && match.Local == "ProbeMatch" {
					matches = append(matches, match)
				}
			}
			if len(matches) > 0 {
				return matches, kindProbeMatch
			}
		case "ProbeMatch":
			return []*node{child}, kindProbeMatch
		case "Hello":
			return []*node{child}, kindHello
		case "Bye":
			return []*node{child}, kindBye
		}
	}
	return nil, kindUnknown
}

// endpointReference extracts the wsa:EndpointReference/wsa:Address text from a
// WS-Discovery body or an addressing container.
func endpointReference(parent *node) string {
	ref := findChild(parent, isAddressingNamespace, "EndpointReference")
	if ref == nil {
		return ""
	}
	addr := findChild(ref, isAddressingNamespace, "Address")
	if addr == nil {
		return ""
	}
	return truncateField(strings.TrimSpace(addr.Text))
}

// fieldTokens returns the whitespace-separated tokens of a WS-Discovery child
// element (Scopes, XAddrs are space-delimited lists).
func fieldTokens(parent *node, local string) []string {
	child := findChild(parent, isDiscoveryNamespace, local)
	if child == nil {
		return nil
	}
	return strings.Fields(child.Text)
}

// resolveTypeQNames reads the WS-Discovery Types element (a space-delimited
// list of QNames) and resolves each QName against the Types element's in-scope
// namespace declarations, returning stable expanded-name strings of the form
// "{namespace}local". A token whose prefix is undeclared is skipped rather than
// retained under an ambiguous identity, so different prefixes for the same
// namespace deduplicate to a single identity while unresolvable tokens never
// pollute the aggregate.
func resolveTypeQNames(parent *node, limit int) []string {
	child := findChild(parent, isDiscoveryNamespace, "Types")
	if child == nil {
		return nil
	}
	tokens := strings.Fields(child.Text)
	if len(tokens) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		prefix, local := splitQName(token)
		uri, ok := child.resolvePrefix(prefix)
		if !ok {
			// Undeclared prefix: cannot assign a stable identity, so skip.
			continue
		}
		expanded := truncateField(qname{Space: uri, Local: local}.expandedName())
		if _, dup := seen[expanded]; dup {
			continue
		}
		seen[expanded] = struct{}{}
		out = append(out, expanded)
		if len(out) >= limit {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// splitQName splits a lexical QName into its prefix and local part. A QName
// with no colon has the empty (default-namespace) prefix.
func splitQName(token string) (prefix, local string) {
	if idx := strings.IndexByte(token, ':'); idx >= 0 {
		return token[:idx], token[idx+1:]
	}
	return "", token
}

// firstChildText returns the trimmed text of the named WS-Discovery child.
func firstChildText(parent *node, local string) string {
	child := findChild(parent, isDiscoveryNamespace, local)
	if child == nil {
		return ""
	}
	return strings.TrimSpace(child.Text)
}

// boundedFields deduplicates, bounds the length of, and caps the count of a
// slice of untrusted tokens, preserving first-seen order.
func boundedFields(tokens []string, limit int) []string {
	if len(tokens) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = truncateField(strings.TrimSpace(token))
		if token == "" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// truncateField bounds a single scalar field drawn from untrusted input.
func truncateField(value string) string {
	if len(value) > maxFieldLength {
		return value[:maxFieldLength]
	}
	return value
}

// findChild returns the first direct child whose namespace satisfies match and
// whose local name equals local.
func findChild(parent *node, match func(string) bool, local string) *node {
	for _, child := range parent.Children {
		if child.Local == local && match(child.Space) {
			return child
		}
	}
	return nil
}

// parseTree decodes a datagram into a namespace-resolved tree. Strict decoding
// rejects malformed XML; directives (DTDs) are rejected outright. Each element
// node inherits the in-scope namespace declarations of its ancestors and
// extends them with its own xmlns attributes, bounded by maxNamespaceDecls, so
// prefixed QName content can be resolved after the walk. Non-namespace
// attributes are retained boundedly so header counters and relationship types
// can be read after the walk.
func parseTree(data []byte) (*node, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	var (
		root  *node
		stack []*node
		depth int
	)
	const maxDepth = 64
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, ErrMalformedMessage
		}
		switch typed := token.(type) {
		case xml.StartElement:
			depth++
			if depth > maxDepth {
				return nil, ErrMalformedMessage
			}
			element := &node{Space: typed.Name.Space, Local: typed.Name.Local}
			var inherited []nsDecl
			if len(stack) > 0 {
				inherited = stack[len(stack)-1].nsScope
			}
			element.nsScope = extendNamespaceScope(inherited, typed.Attr)
			element.Attrs, element.attrsTruncated = retainAttrs(typed.Attr)
			if len(stack) == 0 {
				if root != nil {
					return nil, ErrMalformedMessage
				}
				root = element
			} else {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, element)
			}
			stack = append(stack, element)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, ErrMalformedMessage
			}
			stack = stack[:len(stack)-1]
			depth--
		case xml.CharData:
			if len(stack) > 0 {
				current := stack[len(stack)-1]
				if len(current.Text) < maxFieldLength {
					current.Text += string(typed)
				}
			}
		case xml.Directive:
			return nil, ErrMalformedMessage
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, ErrMalformedMessage
	}
	return root, nil
}

// retainAttrs projects an element's non-namespace attributes into a bounded
// slice. xmlns declarations are excluded (they are handled by the namespace
// scope); the result is capped at maxAttrs and each value is length-bounded so
// a hostile datagram cannot force unbounded allocation.
func retainAttrs(attrs []xml.Attr) ([]attr, bool) {
	var out []attr
	truncated := false
	for i, a := range attrs {
		if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
			continue
		}
		if len(out) >= maxAttrs {
			// At least one non-namespace attribute remains beyond the cap: the
			// retained view is incomplete. Scan the tail only to confirm a
			// non-namespace attribute was actually dropped (trailing xmlns
			// declarations do not count as lost critical data).
			for _, rest := range attrs[i:] {
				if rest.Name.Space == "xmlns" || (rest.Name.Space == "" && rest.Name.Local == "xmlns") {
					continue
				}
				truncated = true
				break
			}
			break
		}
		out = append(out, attr{
			Space: a.Name.Space,
			Local: a.Name.Local,
			Value: truncateField(a.Value),
		})
	}
	return out, truncated
}

// extendNamespaceScope returns the namespace scope for an element: the parent's
// scope plus any xmlns declarations carried on the element's attributes. The
// result is bounded by maxNamespaceDecls so a datagram cannot force unbounded
// per-node growth; declarations beyond the bound are dropped. The parent slice
// is never mutated: a new slice is allocated only when the element declares at
// least one namespace, so unchanged scopes are shared cheaply.
func extendNamespaceScope(parent []nsDecl, attrs []xml.Attr) []nsDecl {
	var decls []nsDecl
	for _, attr := range attrs {
		switch {
		case attr.Name.Space == "xmlns":
			// xmlns:prefix="uri"
			decls = append(decls, nsDecl{Prefix: attr.Name.Local, URI: attr.Value})
		case attr.Name.Space == "" && attr.Name.Local == "xmlns":
			// Default namespace: xmlns="uri".
			decls = append(decls, nsDecl{Prefix: "", URI: attr.Value})
		}
	}
	if len(decls) == 0 {
		return parent
	}
	scope := make([]nsDecl, 0, len(parent)+len(decls))
	scope = append(scope, parent...)
	scope = append(scope, decls...)
	if len(scope) > maxNamespaceDecls {
		// Keep the most recent declarations, which are the closest in scope.
		scope = scope[len(scope)-maxNamespaceDecls:]
	}
	return scope
}

// sortedCopy returns a sorted, deduplicated copy of the input for deterministic
// aggregation output.
func sortedCopy(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
