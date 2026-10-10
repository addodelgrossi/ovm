package app

import (
	"context"
	"errors"

	media1 "github.com/addodelgrossi/ovm/pkg/onvif/media"
	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// opResult pairs a capability state, provenance, and safe credential-free
// detail produced by classifying a media operation error. It never retains the
// raw lower-level error or any URI/query/token.
type opResult struct {
	State  CapabilityState
	Source MediaSourceService
	Detail string
}

// withOp projects the classified result onto a named operation status.
func (o opResult) withOp(op MediaOperationName) MediaOperationStatus {
	return MediaOperationStatus{Operation: op, State: o.State, Source: o.Source, Detail: o.Detail}
}

// withOpFellBack projects the classified result onto a named operation status
// and records whether the result was reached through a genuine Media1 fallback.
func (o opResult) withOpFellBack(op MediaOperationName, fellBack bool) MediaOperationStatus {
	status := o.withOp(op)
	status.FellBack = fellBack
	return status
}

// mediaDecodeError reports whether err is a Media1 or Media2 wire-layer decode
// sentinel (a malformed, unexpected, missing, invalid, or absent response).
// These are protocol-level ambiguities, not action-support or network signals.
func mediaDecodeError(err error) bool {
	return errors.Is(err, media1.ErrMalformedXML) ||
		errors.Is(err, media2.ErrMalformedXML) ||
		errors.Is(err, media1.ErrUnexpectedElement) ||
		errors.Is(err, media2.ErrUnexpectedElement) ||
		errors.Is(err, media1.ErrMissingField) ||
		errors.Is(err, media2.ErrMissingField) ||
		errors.Is(err, media1.ErrInvalidField) ||
		errors.Is(err, media2.ErrInvalidField) ||
		errors.Is(err, media1.ErrNilResponse) ||
		errors.Is(err, media2.ErrNilResponse) ||
		errors.Is(err, media1.ErrInvalidInput) ||
		errors.Is(err, media2.ErrInvalidInput)
}

// classifyMediaOp maps a media operation error to a normalized per-operation
// result with a safe, credential-free English detail. It never includes a URI,
// token, query, or response body, and it never retains the lower-level error as
// a cause: the mapping is to fixed sentinels and safe English only.
//
// The classification reuses the Device inventory classifiers (isAuthentication
// Error, isNetworkError, isProtocolError) so the two use cases report identical
// semantics, and extends them with the Media1/Media2 wire decode sentinels. The
// ordering mirrors classifyState: ActionNotSupported first, then context
// lifecycle, then authentication (so an auth-policy refusal wrapped in a
// *url.Error is an auth outcome rather than a transport failure), then media
// decode and SOAP/protocol errors, then real network conditions, then an
// ambiguous default.
func classifyMediaOp(err error) opResult {
	switch {
	case err == nil:
		return opResult{State: CapabilitySupported}
	case errors.Is(err, soap.ErrActionNotSupported):
		return opResult{State: CapabilityUnsupported, Detail: "the device reported the action is not supported"}
	case errors.Is(err, context.Canceled):
		return opResult{State: CapabilityTemporarilyUnavailable, Detail: "the operation was cancelled"}
	case errors.Is(err, context.DeadlineExceeded):
		return opResult{State: CapabilityTemporarilyUnavailable, Detail: "the operation timed out"}
	case isAuthenticationError(err):
		return opResult{State: CapabilityUnknown, Detail: "the device rejected the credentials"}
	case mediaDecodeError(err):
		return opResult{State: CapabilityUnknown, Detail: "the response could not be interpreted"}
	case isProtocolError(err):
		return opResult{State: CapabilityUnknown, Detail: "the response could not be interpreted"}
	case isNetworkError(err):
		return opResult{State: CapabilityTemporarilyUnavailable, Detail: "a transient network error prevented a result"}
	default:
		return opResult{State: CapabilityUnknown, Detail: "the response could not be interpreted"}
	}
}

// mapMediaError maps a fatal media error (one that aborts the whole use case,
// such as a failed GetServices, a failed root GetProfiles, or a cancelled
// context) to a typed application error with a safe, credential-free message.
// The underlying cause is sanitized to a stable context sentinel or nil: no
// raw, query-bearing lower error is ever retained, so a recursive unwrap of the
// returned error can never surface a secret.
//
// The category mapping mirrors the Device inventory categorizeFatal and extends
// it with the Media decode sentinels so a malformed Media response is a
// protocol failure rather than a generic internal one:
//   - authentication classes          -> CategoryAuthentication
//   - explicit cancellation           -> CategoryCancelled
//   - timeouts / concrete socket fail -> CategoryNetwork
//   - media decode / SOAP protocol /
//     non-auth HTTP status            -> CategoryProtocol
//   - anything else                   -> CategoryInternal
func mapMediaError(err error) error {
	switch {
	case err == nil:
		return nil
	case isAuthenticationError(err):
		return newError(CategoryAuthentication, "the device rejected the credentials", nil)
	case errors.Is(err, context.Canceled):
		return newError(CategoryCancelled, "the operation was cancelled", context.Canceled)
	case errors.Is(err, soap.ErrActionNotSupported):
		return newError(CategoryProtocol, "the device does not support the required operation", nil)
	case isNetworkError(err):
		// A context deadline is a timeout: preserve the stable sentinel so a
		// caller can still recognize the deadline, but never the raw cause.
		if errors.Is(err, context.DeadlineExceeded) {
			return newError(CategoryNetwork, "the operation timed out", context.DeadlineExceeded)
		}
		return newError(CategoryNetwork, "the device could not be reached", nil)
	case mediaDecodeError(err):
		return newError(CategoryProtocol, "the device response could not be interpreted", nil)
	case isProtocolError(err):
		return newError(CategoryProtocol, "the device response could not be interpreted", nil)
	default:
		return newError(CategoryInternal, "the media operation could not be completed", nil)
	}
}
