// Package soap provides a bounded, context-aware SOAP transport for the
// supported ONVIF protocol subset.
//
// The package is deliberately independent of service models, credential
// storage, command-line adapters, and user interfaces. Calls start with SOAP
// 1.2 and retry SOAP 1.1 only when the peer supplies protocol evidence.
package soap
