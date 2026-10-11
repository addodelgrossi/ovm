package app

import (
	"context"
	"errors"
	"net"

	"github.com/addodelgrossi/ovm/pkg/onvif/auth"
	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// shouldFallBackToMedia1 is the single, narrow predicate that decides whether a
// failed Media2 operation may fall back to the ver10 Media1 service. It returns
// true ONLY when the error is the namespace-resolved soap.ErrActionNotSupported
// sentinel AND the error does not also match any disqualifier. Every decision
// is made on typed, namespace-resolved sentinels and typed error values — never
// on fault reason text, which a hostile or buggy device controls.
//
// The rule exists because a fallback is a change of trust surface: it issues a
// second authenticated request to a different service. That is only justified
// when the device has positively told us, through the one unambiguous fault,
// that it does not implement the Media2 action. Any other failure (auth,
// cancellation, timeout, malformed response, size/complexity limit, protocol or
// policy error) must surface as that operation's own status and must NOT cause
// a second request.
func shouldFallBackToMedia1(err error) bool {
	if err == nil {
		return false
	}
	// A disqualifier anywhere in the chain vetoes the fallback, even when the
	// error is also joined with ActionNotSupported.
	if isFallbackDisqualifier(err) {
		return false
	}
	return errors.Is(err, soap.ErrActionNotSupported)
}

// isFallbackDisqualifier reports whether err matches any error class that must
// never permit a Media1 fallback, regardless of whether ActionNotSupported is
// also present. The checks are all typed: sentinels via errors.Is and concrete
// types via errors.As, plus the net.Error timeout interface.
func isFallbackDisqualifier(err error) bool {
	// Context lifecycle: an explicit cancellation or deadline must win and
	// never trigger another request.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Authentication / authorization and policy: never retry on another
	// service with the same rejected credentials or a refused policy.
	if errors.Is(err, soap.ErrNotAuthorized) ||
		errors.Is(err, auth.ErrAuthPolicy) ||
		errors.Is(err, ErrInsecureDigestBlocked) {
		return true
	}

	// SOAP-level protocol errors that are not an action-support signal.
	if errors.Is(err, soap.ErrInvalidArgVal) ||
		errors.Is(err, soap.ErrVersionMismatch) ||
		errors.Is(err, soap.ErrDTDNotAllowed) ||
		errors.Is(err, soap.ErrXMLComplexity) ||
		errors.Is(err, soap.ErrInvalidMediaType) ||
		errors.Is(err, soap.ErrEnvelopeVersionMismatch) ||
		errors.Is(err, soap.ErrInvalidXML) ||
		errors.Is(err, soap.ErrCrossOriginRedirect) ||
		errors.Is(err, soap.ErrUnsafeRedirect) ||
		errors.Is(err, soap.ErrInvalidEndpoint) ||
		errors.Is(err, soap.ErrInvalidAction) {
		return true
	}

	// Media2 wire-layer decode sentinels: a malformed, unexpected, missing,
	// invalid, or absent response is a protocol problem, not an action-support
	// signal.
	if errors.Is(err, media2.ErrMalformedXML) ||
		errors.Is(err, media2.ErrUnexpectedElement) ||
		errors.Is(err, media2.ErrMissingField) ||
		errors.Is(err, media2.ErrInvalidField) ||
		errors.Is(err, media2.ErrNilResponse) ||
		errors.Is(err, media2.ErrInvalidInput) {
		return true
	}

	// Typed SOAP error values carried through the chain.
	var httpErr *soap.HTTPError
	if errors.As(err, &httpErr) {
		return true
	}
	var tooLarge *soap.ResponseTooLargeError
	if errors.As(err, &tooLarge) {
		return true
	}
	var reqTooLarge *soap.RequestTooLargeError
	if errors.As(err, &reqTooLarge) {
		return true
	}
	var parseErr *soap.ParseError
	if errors.As(err, &parseErr) {
		return true
	}
	var transportErr *soap.TransportError
	if errors.As(err, &transportErr) {
		return true
	}

	// Network timeouts and transient network failures.
	var netErr net.Error
	return errors.As(err, &netErr)
}
