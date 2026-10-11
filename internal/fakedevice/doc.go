// Package fakedevice provides a configurable, stateful ONVIF HTTP handler for
// integration tests and future local demo mode. It models the supported Device,
// Media1, and Media2 SOAP operations plus an authenticated synthetic JPEG
// endpoint, fault/unsupported controls, and bounded request history.
//
// Request history contains only credential-free action metadata. The package is
// not an RTSP server and is not imported by production ONVIF packages.
package fakedevice
