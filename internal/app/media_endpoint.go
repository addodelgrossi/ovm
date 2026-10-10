package app

import (
	"net/url"
	"strings"

	"github.com/addodelgrossi/ovm/pkg/onvif/discovery"
)

// canonicalizeOperationalDeviceEndpoint canonicalizes a manual Device service
// endpoint for ACTUAL requests while reusing, unchanged, the M1 discovery
// hardening in discovery.CanonicalizeEndpoint.
//
// The M1 canonicalizer deliberately rejects query strings and fragments
// because a canonical, deterministic endpoint identity must not depend on them.
// But a real device-service endpoint supplied by an operator may legitimately
// carry a query that the request requires (for example a tenant selector). This
// helper bridges that gap WITHOUT weakening M1:
//
//  1. The scheme, userinfo, fragment, control-character, path-traversal, and
//     host rules are enforced by handing a QUERY-FREE copy of the input to
//     discovery.CanonicalizeEndpoint. That function is called exactly as M1
//     uses it, so its behavior is unchanged and shared.
//  2. The original RawQuery and ForceQuery are then re-attached to the
//     canonical result to produce the operational endpoint used for requests.
//
// A fragment or userinfo anywhere in the input is rejected (the query-free copy
// still carries them, so CanonicalizeEndpoint rejects them). The returned URL's
// query is never placed into any DTO or error by callers; it exists only for
// request construction.
func canonicalizeOperationalDeviceEndpoint(input string) (*url.URL, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, usageError("the endpoint is not valid")
	}
	if containsControl(trimmed) {
		return nil, usageError("the endpoint is not valid")
	}

	// Split off the query so the M1 canonicalizer (which rejects queries) can
	// apply its full hardening to the query-free remainder. A fragment is left
	// attached on purpose so CanonicalizeEndpoint rejects it.
	queryFree := trimmed
	rawQuery := ""
	forceQuery := false
	if idx := strings.IndexByte(trimmed, '?'); idx >= 0 {
		queryFree = trimmed[:idx]
		rawQuery = trimmed[idx+1:]
		forceQuery = rawQuery == ""
	}

	endpoint, err := discovery.CanonicalizeEndpoint(queryFree)
	if err != nil {
		return nil, usageError("the endpoint is not valid")
	}

	operational := &url.URL{
		Scheme:     endpoint.Scheme,
		Host:       endpoint.Host,
		Path:       endpoint.Path,
		RawPath:    endpoint.RawPath,
		RawQuery:   rawQuery,
		ForceQuery: forceQuery,
	}

	// Defense in depth: the operational endpoint must itself pass the strict
	// same-boundary safety check (absolute http/https, no userinfo, no
	// fragment, host present). A query is permitted here, unlike M1.
	if _, perr := parseSafeOperationalURL(operational.String()); perr != nil {
		return nil, usageError("the endpoint is not valid")
	}
	return operational, nil
}
