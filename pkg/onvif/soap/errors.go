package soap

import (
	"errors"
	"fmt"
)

var (
	// ErrNotAuthorized identifies an ONVIF NotAuthorized SOAP fault.
	ErrNotAuthorized = errors.New("ONVIF request is not authorized")
	// ErrActionNotSupported identifies an ONVIF ActionNotSupported SOAP fault.
	ErrActionNotSupported = errors.New("ONVIF action is not supported")
	// ErrInvalidArgVal identifies an ONVIF InvalidArgVal SOAP fault.
	ErrInvalidArgVal = errors.New("ONVIF argument value is invalid")
	// ErrVersionMismatch identifies a SOAP VersionMismatch fault.
	ErrVersionMismatch = errors.New("SOAP version mismatch")
	// ErrDTDNotAllowed identifies XML containing a document type declaration.
	ErrDTDNotAllowed = errors.New("SOAP XML document type declarations are not allowed")
	// ErrCrossOriginRedirect identifies a redirect to a different origin.
	ErrCrossOriginRedirect = errors.New("cross-origin SOAP redirect is not allowed")
	// ErrUnsafeRedirect identifies a redirect that changes SOAP request semantics.
	ErrUnsafeRedirect = errors.New("SOAP redirect changed the request method or body")
	// ErrInvalidEndpoint identifies an unsafe or malformed SOAP endpoint.
	ErrInvalidEndpoint = errors.New("invalid SOAP endpoint")
	// ErrInvalidAction identifies a SOAP action that cannot be represented safely.
	ErrInvalidAction = errors.New("invalid SOAP action")
	// ErrXMLComplexity identifies a response whose XML structure exceeds safe
	// parser depth, element, attribute, namespace, or text-work limits.
	ErrXMLComplexity = errors.New("SOAP XML exceeds structural limits")
	// ErrInvalidMediaType identifies a response whose Content-Type is missing,
	// malformed, or incompatible with the attempted SOAP version.
	ErrInvalidMediaType = errors.New("SOAP response media type is invalid")
	// ErrEnvelopeVersionMismatch identifies a response whose SOAP envelope
	// namespace does not match the attempted SOAP version.
	ErrEnvelopeVersionMismatch = errors.New("SOAP response envelope version does not match the request")
	// ErrInvalidXML identifies malformed SOAP XML without exposing parser text
	// derived from attacker-controlled markup.
	ErrInvalidXML = errors.New("SOAP response XML is invalid")
)

type httpStatusError interface {
	httpStatusCode() int
}

// TransportError reports a SOAP request transport failure without rendering the
// endpoint URL. The underlying cause remains available to errors.Is/errors.As,
// but Error deliberately omits it because a valid endpoint may contain a
// sensitive query value supplied by a device.
type TransportError struct {
	Version Version
	Err     error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("send SOAP %s request failed", e.Version)
}

// Unwrap exposes the transport cause without including it in user-facing text.
func (e *TransportError) Unwrap() error { return e.Err }

// HTTPError reports a non-success HTTP response that did not contain a SOAP
// fault. RawXML is bounded and redacted.
type HTTPError struct {
	StatusCode int
	RawXML     []byte
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("SOAP HTTP response has status %d", e.StatusCode)
}

func (e *HTTPError) httpStatusCode() int { return e.StatusCode }

// ResponseTooLargeError reports a response larger than the configured limit.
// RawXML contains at most the bounded, redacted prefix.
type ResponseTooLargeError struct {
	Limit      int64
	HTTPStatus int
	RawXML     []byte
}

func (e *ResponseTooLargeError) Error() string {
	return fmt.Sprintf("SOAP response exceeds %d-byte limit", e.Limit)
}

func (e *ResponseTooLargeError) httpStatusCode() int { return e.HTTPStatus }

// RequestTooLargeError reports that the serialized SOAP request would exceed the
// configured request-size limit. It is produced before any DTD scan, buffer
// growth, replay, or HTTP transport, so no request is sent.
type RequestTooLargeError struct {
	// Version is the SOAP version whose serialized request exceeded the limit.
	Version Version
	// Limit is the configured maximum serialized request size in bytes.
	Limit int64
	// Size is the serialized request size in bytes, or -1 when the size could
	// not be represented without overflow.
	Size int64
}

func (e *RequestTooLargeError) Error() string {
	if e.Size < 0 {
		return fmt.Sprintf("SOAP %s request size overflows the %d-byte limit", e.Version, e.Limit)
	}
	return fmt.Sprintf("SOAP %s request is %d bytes, exceeding the %d-byte limit", e.Version, e.Size, e.Limit)
}

// ParseError reports invalid SOAP XML. RawXML is bounded and redacted, and the
// user-facing message is sanitized so a malformed document can never surface a
// credential-bearing fragment through Error or the wrapped cause.
type ParseError struct {
	// Err is the sanitized parser cause. It preserves errors.Is for the stable
	// sentinels (ErrDTDNotAllowed, ErrXMLComplexity, ErrInvalidMediaType,
	// ErrEnvelopeVersionMismatch) while never exposing raw XML markers from the
	// offending document.
	Err        error
	HTTPStatus int
	RawXML     []byte
}

func (e *ParseError) Error() string {
	if e.Err == nil {
		return "parse SOAP response"
	}
	return "parse SOAP response: " + e.Err.Error()
}

// Unwrap exposes the sanitized parser error for errors.Is and errors.As.
func (e *ParseError) Unwrap() error { return e.Err }

func (e *ParseError) httpStatusCode() int { return e.HTTPStatus }

// newParseError builds a ParseError whose cause is a fixed, sanitized
// category. It preserves errors.Is for stable sentinels and never retains raw
// parser prose derived from the offending XML.
func newParseError(cause error, httpStatus int, rawXML []byte) *ParseError {
	return &ParseError{Err: sanitizeParserError(cause), HTTPStatus: httpStatus, RawXML: rawXML}
}

func sanitizeParserError(cause error) error {
	if cause == nil {
		return nil
	}
	switch {
	case errors.Is(cause, ErrDTDNotAllowed):
		return ErrDTDNotAllowed
	case errors.Is(cause, ErrXMLComplexity):
		return ErrXMLComplexity
	case errors.Is(cause, ErrInvalidMediaType):
		return ErrInvalidMediaType
	case errors.Is(cause, ErrEnvelopeVersionMismatch):
		return ErrEnvelopeVersionMismatch
	case errors.Is(cause, ErrInvalidXML):
		return ErrInvalidXML
	default:
		return ErrInvalidXML
	}
}
