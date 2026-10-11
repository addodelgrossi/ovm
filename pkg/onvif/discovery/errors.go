package discovery

import "errors"

var (
	// ErrNoUsableInterfaces reports that no interface satisfied the selection
	// criteria (up, non-loopback IPv4 by default).
	ErrNoUsableInterfaces = errors.New("discovery: no usable network interfaces")
	// ErrInterfaceNotFound reports that a named interface was requested but is
	// not present in the interface source.
	ErrInterfaceNotFound = errors.New("discovery: named interface not found")
	// ErrInterfaceNotUsable reports that a named interface exists but does not
	// satisfy the usability criteria.
	ErrInterfaceNotUsable = errors.New("discovery: named interface is not usable")

	// ErrMalformedMessage reports a WS-Discovery datagram that is not valid or
	// is structured in a way that cannot be trusted.
	ErrMalformedMessage = errors.New("discovery: malformed WS-Discovery message")
	// ErrMessageTooLarge reports a datagram larger than the configured bound.
	ErrMessageTooLarge = errors.New("discovery: WS-Discovery datagram exceeds size limit")

	// ErrInvalidEndpoint reports an unsafe or malformed manual endpoint.
	ErrInvalidEndpoint = errors.New("discovery: invalid endpoint")
)
