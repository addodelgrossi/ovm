package app

import (
	"errors"
	"net/http"
	"strings"
)

// ErrInsecureDigestBlocked is returned by the application-boundary Digest
// policy transport when it refuses to transmit a generated Digest Authorization
// header that carries no qop directive (legacy RFC 2069). It is classified as
// an authentication failure by the inventory classifier. The error text is
// credential-free and never includes the Authorization header value.
var ErrInsecureDigestBlocked = errors.New("insecure legacy no-qop Digest authorization blocked by policy")

// digestPolicyTransport is an application-boundary http.RoundTripper that wraps
// the auth.DigestTransport base and inspects every outgoing request's
// Authorization header. When the header is a Digest credential that lacks a qop
// directive (legacy RFC 2069), and legacy no-qop is not explicitly permitted,
// the request is refused before it reaches the network and ErrInsecureDigestBlocked
// is returned.
//
// Why a boundary wrapper, and its relationship to the native policy. The auth
// package DOES enforce a native no-qop (RFC 2069) policy: its public
// DigestTransportConfig.AllowRFC2069 flag is deny-by-default, so a legacy
// no-qop challenge is already refused inside auth unless the operator opts in.
// This boundary wrapper is therefore NOT a substitute for a missing native
// policy; it is strictly defense in depth. What auth does not expose is a
// post-generation inspection seam over an already-produced Authorization
// header. Rather than edit the auth package, the application re-asserts the
// same secure default at its own boundary: even if a lower layer were ever
// misconfigured to emit a no-qop Authorization, this wrapper blocks it from
// crossing to the network unless the operator explicitly opted in with the
// legacy flag. The wrapper integrates with auth only through its public
// DigestTransport, which it holds as the wrapped base RoundTripper, and the
// opt-in it honours mirrors the same credential policy that drives
// AllowRFC2069 in auth.
type digestPolicyTransport struct {
	// base is the auth.DigestTransport (as an http.RoundTripper) that generates
	// the Authorization header. It is always non-nil in production.
	base http.RoundTripper
	// allowLegacyNoQOP permits a no-qop Digest Authorization to be transmitted.
	// It mirrors the credential policy's legacy-Digest opt-in.
	allowLegacyNoQOP bool
}

// newDigestPolicyTransport wraps base with the application-boundary no-qop
// Digest policy. A nil base is rejected by returning an error so a miswired
// transport fails loudly rather than silently bypassing the policy.
func newDigestPolicyTransport(base http.RoundTripper, allowLegacyNoQOP bool) (*digestPolicyTransport, error) {
	if base == nil {
		return nil, errors.New("digest policy transport requires a base round tripper")
	}
	return &digestPolicyTransport{base: base, allowLegacyNoQOP: allowLegacyNoQOP}, nil
}

// RoundTrip inspects the outgoing Authorization header and refuses a legacy
// no-qop Digest credential unless policy permits it, then delegates to the base
// transport. The base transport is what generates the Digest header, so the
// inspection happens on the request the base is about to send; to observe the
// generated header the policy transport sits as the base's own base.
//
// The policy transport is installed as the innermost transport (the Digest
// transport's base), so when the Digest transport calls RoundTrip to actually
// send its authenticated request, this method sees the final Authorization
// header the Digest layer produced and can veto it before the network.
func (t *digestPolicyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	authorization := request.Header.Get("Authorization")
	if isNoQOPDigestAuthorization(authorization) && !t.allowLegacyNoQOP {
		// Refuse before touching the network. The error is credential-free.
		return nil, ErrInsecureDigestBlocked
	}
	return t.base.RoundTrip(request)
}

// isNoQOPDigestAuthorization reports whether the Authorization header value is a
// Digest credential that carries no qop directive. A Basic header, an empty
// header, or a Digest header that includes qop= returns false. Parsing is
// deliberately tolerant: it scans the comma/space separated directives for a
// qop token rather than fully parsing the credential, because any Digest header
// the auth layer generates either includes qop=auth or omits it entirely.
func isNoQOPDigestAuthorization(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	// The scheme is the first space-delimited token.
	scheme := trimmed
	if idx := strings.IndexByte(trimmed, ' '); idx >= 0 {
		scheme = trimmed[:idx]
	}
	if !strings.EqualFold(scheme, "Digest") {
		return false
	}
	return !digestParamsIncludeQOP(trimmed)
}

// digestParamsIncludeQOP reports whether a Digest Authorization header contains
// a qop directive. It looks for a "qop" token followed by "=" at a directive
// boundary so a substring such as a field value containing "qop" does not cause
// a false positive.
func digestParamsIncludeQOP(header string) bool {
	// Drop the leading "Digest" scheme token.
	rest := header
	if idx := strings.IndexByte(header, ' '); idx >= 0 {
		rest = header[idx+1:]
	}
	for _, directive := range strings.Split(rest, ",") {
		directive = strings.TrimSpace(directive)
		eq := strings.IndexByte(directive, '=')
		if eq < 0 {
			continue
		}
		name := strings.TrimSpace(directive[:eq])
		if strings.EqualFold(name, "qop") {
			return true
		}
	}
	return false
}
