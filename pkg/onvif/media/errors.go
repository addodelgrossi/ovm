package media

import "errors"

// Decode and validation sentinels. These classify failures that occur after a
// successful SOAP transport exchange, when a well-formed HTTP/SOAP response
// could not be interpreted as the expected typed Media result, or when caller
// input could not be represented as a valid request. SOAP faults and
// transport/cancellation errors are reported by pkg/onvif/soap and remain
// errors.Is-compatible through the client (see client.go), so callers can test
// for soap.ErrNotAuthorized, soap.ErrActionNotSupported, soap.ErrInvalidArgVal,
// context.Canceled, and context.DeadlineExceeded on the error returned by any
// operation.
//
// None of these errors carries raw, unredacted response XML, nor any request
// token, URI, or credential value. Messages describe the structural problem by
// field name only.
var (
	// ErrMalformedXML reports that the operation payload was not well-formed
	// XML, contained a document type declaration, or exceeded structural
	// bounds.
	ErrMalformedXML = errors.New("media: malformed response XML")
	// ErrUnexpectedElement reports that the response root or a required element
	// did not match the expected ONVIF Media namespace and local name.
	ErrUnexpectedElement = errors.New("media: unexpected response element")
	// ErrMissingField reports that a required field was absent from an
	// otherwise well-formed response.
	ErrMissingField = errors.New("media: required response field is missing")
	// ErrInvalidField reports that a present field held a value that could not
	// be validated or converted to its typed form.
	ErrInvalidField = errors.New("media: response field is invalid")
	// ErrNilResponse reports that the transport returned no response and no
	// error, which must never happen for a successful call.
	ErrNilResponse = errors.New("media: transport returned no response")
	// ErrInvalidInput reports that a caller-supplied argument (a token, enum,
	// or cardinality) could not be represented as a valid, safe request. Its
	// message never echoes the rejected value.
	ErrInvalidInput = errors.New("media: invalid request input")
)
