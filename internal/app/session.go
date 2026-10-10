package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/addodelgrossi/ovm/pkg/onvif/auth"
	"github.com/addodelgrossi/ovm/pkg/onvif/device"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// This file hosts the single reusable authenticated ONVIF camera session that
// every top-level use case shares. The Device inventory use case, the Device
// GetServices call that Media discovery depends on, every Media1/Media2 SOAP
// call, and the snapshot HTTP GET all drive the SAME *session instance and the
// SAME origin-scoped RoundTripper. Auth negotiation, UsernameToken generation,
// the Digest/Basic transport policy, the clock-offset logic, the child budgets,
// and the single NotAuthorized retry are therefore implemented exactly once and
// never duplicated.
//
// The session owns one authenticated *http.Client (whose Transport is the
// origin-scoped auth.DigestTransport with its challenge cache) and the one
// *soap.Client built over it. The Media clients are constructed over that same
// *soap.Client; the snapshot GET clones only the *http.Client struct to install
// a GET redirect policy but shares the exact Transport pointer (and therefore
// the same Digest cache) and never sends WS-Security.
//
// The free functions below (deriveChildBudget, sampleClockOffset,
// freshUsernameTokenHeader, buildAuthenticatedHTTPClient, same-origin helpers)
// are the building blocks the session composes; they remain package-level so
// the construction code and the session method bodies share one implementation.

// Security invariant: a session validates that an operational service endpoint
// is same-origin with the camera endpoint BEFORE it ever attaches a
// WS-Security UsernameToken or forwards HTTP credentials. Redirect protection
// alone is insufficient because an advertised (GetServices) endpoint could
// itself point at a foreign origin on the very first request; same-origin
// validation closes that gap at the point the credential is applied.

// clockReader reads the device clock and returns the device UTC, a validity
// flag, and any transport error. It abstracts the typed Device client so both
// the inventory seam and the media flow can share the budget/sampling code.
type clockReader func(ctx context.Context) (utc time.Time, valid bool, err error)

// nowFunc supplies the current time; nil defers to time.Now.
type nowFunc func() time.Time

// currentTime returns now() or time.Now when now is nil.
func currentTime(now nowFunc) time.Time {
	if now != nil {
		return now()
	}
	return time.Now()
}

// session is the single stateful authenticated camera session shared by every
// top-level use case. It owns the one authenticated HTTP client (with its
// origin-scoped auth.DigestTransport and challenge cache), the one soap.Client
// built over it, the Credentials reference, the UsernameToken generator, the
// clock sampler and current-time hook, the clock read hook, the synchronized
// clock offset, and the single shared resample gate. It performs the initial
// clock negotiation, derives child budgets, generates fresh WSSE headers, and
// performs exactly one retry ONLY on soap.ErrNotAuthorized.
type session struct {
	// creds is the credential reference. A nil value is an unauthenticated
	// session: no WSSE header is attached and no retry is attempted.
	creds *Credentials

	// generator produces fresh WS-Security UsernameToken headers.
	generator auth.UsernameTokenGenerator

	// sampler bounds the clock-offset sampling attempts.
	sampler auth.ClockSampler

	// clock is the current-time hook used for budget math; nil defers to
	// time.Now.
	clock func() time.Time

	// readClock reads the device clock. Negotiation and the resample both read
	// through the Device service, which is always same-origin.
	readClock clockReader

	// httpClient is the ONE authenticated HTTP client. Its Transport is the
	// origin-scoped auth.DigestTransport (with challenge cache) when credentials
	// are present. Every SOAP call and the snapshot GET share this transport.
	httpClient *http.Client

	// soapClient is the ONE SOAP client built over httpClient. The Device client
	// and both Media clients are constructed over it.
	soapClient *soap.Client

	// offsetMu guards the sampled clock offset against concurrent operations:
	// many Device/Media operations may run in parallel and any one of them may
	// trigger the single shared authentication resample.
	offsetMu    sync.Mutex
	clockOffset time.Duration
	haveOffset  bool

	// resampleOnce ensures the authentication-triggered resample happens at most
	// once across all concurrent Device and Media operations for the whole use
	// case.
	resampleOnce sync.Once
}

// sessionConfig carries the inputs required to build a session's one
// authenticated transport chain.
type sessionConfig struct {
	// endpointURL is the canonical, already-validated device-service endpoint.
	// It defines the Digest scope and the SOAP/Device client address.
	endpointURL string

	// creds is the credential reference (nil for an unauthenticated session).
	creds *Credentials

	// timeout bounds each SOAP request.
	timeout time.Duration

	// baseHTTPClient is the base client to clone; nil uses a fresh client.
	baseHTTPClient *http.Client

	// sampler and clock are injectable for tests. A zero sampler defaults to the
	// negotiation attempt count; a nil clock defers to time.Now.
	sampler auth.ClockSampler
	clock   func() time.Time
	// trace, when set, receives redacted SOAP exchanges with bodies.
	trace soap.TraceFunc
}

// newSession builds the single authenticated session for one top-level use
// case: exactly one authenticated HTTP client (one Digest transport + cache)
// and one soap.Client over it. readClock defaults to reading the device clock
// through the supplied Device client; callers that have not yet built a Device
// client may leave it nil and set it afterward via setClockReader.
func newSession(cfg sessionConfig) (*session, error) {
	httpClient, err := buildAuthenticatedHTTPClient(cfg.baseHTTPClient, cfg.endpointURL, cfg.creds)
	if err != nil {
		return nil, err
	}
	soapClient, err := soap.NewClient(soap.Config{HTTPClient: httpClient, Timeout: cfg.timeout, Trace: cfg.trace, TraceBodies: cfg.trace != nil})
	if err != nil {
		return nil, newError(CategoryInternal, "the SOAP client could not be constructed", err)
	}
	sampler := cfg.sampler
	if sampler.Attempts == 0 {
		sampler.Attempts = negotiationAttempts
	}
	clock := cfg.clock
	if clock == nil {
		clock = time.Now
	}
	return &session{
		creds:      cfg.creds,
		generator:  auth.UsernameTokenGenerator{},
		sampler:    sampler,
		clock:      clock,
		httpClient: httpClient,
		soapClient: soapClient,
	}, nil
}

// setClockReader installs the clock read hook. It is set once, immediately
// after the Device client is constructed over the session's soap.Client.
func (s *session) setClockReader(read clockReader) { s.readClock = read }

// childBudget derives a bounded child context via the shared primitive.
func (s *session) childBudget(parent context.Context, absolute time.Duration, fraction int) (context.Context, context.CancelFunc) {
	return deriveChildBudget(parent, s.clock, absolute, fraction)
}

// negotiate performs the first clock call without a WS-Security header under a
// short child budget so the HTTP transport may answer a Digest challenge and so
// a sampled clock offset becomes available for subsequent UsernameToken
// headers. Failures are non-fatal and leave the offset unpublished.
func (s *session) negotiate(ctx context.Context) {
	if s.creds == nil || ctx.Err() != nil {
		return
	}
	budgetCtx, cancel := s.childBudget(ctx, negotiationMaxBudget, negotiationBudgetFraction)
	defer cancel()
	sample, err := sampleClockOffset(budgetCtx, s.sampler, s.readClock)
	if err != nil || ctx.Err() != nil {
		return
	}
	s.offsetMu.Lock()
	s.clockOffset = sample.Offset
	s.haveOffset = true
	s.offsetMu.Unlock()
}

// resampleForRetry performs at most one shared clock resample before a
// fresh-token authentication retry. It is invoked only after a SOAP
// NotAuthorized fault, runs at most once for the whole use case (across all
// Device and Media operations), honours parent cancellation through a short
// child budget, and retains the previous offset on failure.
func (s *session) resampleForRetry(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	s.resampleOnce.Do(func() {
		// Recheck inside the once callback so a cancellation that races with Do
		// never reaches the clock reader or publishes an offset.
		if ctx.Err() != nil {
			return
		}
		budgetCtx, cancel := s.childBudget(ctx, resampleMaxBudget, resampleBudgetFraction)
		defer cancel()
		sampler := s.sampler
		sampler.Attempts = resampleAttempts
		sample, err := sampleClockOffset(budgetCtx, sampler, s.readClock)
		if err != nil || ctx.Err() != nil {
			return
		}
		s.offsetMu.Lock()
		s.clockOffset = sample.Offset
		s.haveOffset = true
		s.offsetMu.Unlock()
	})
}

// headerXML produces a fresh WS-Security UsernameToken header at the sampled
// offset, or nil for an unauthenticated session. A fresh token (new nonce and
// timestamp) is produced on every call so no token is ever reused across
// logical attempts.
func (s *session) headerXML() ([]byte, error) {
	if s.creds == nil {
		return nil, nil
	}
	s.offsetMu.Lock()
	offset := time.Duration(0)
	if s.haveOffset {
		offset = s.clockOffset
	}
	s.offsetMu.Unlock()
	return freshUsernameTokenHeader(s.generator, s.creds, offset)
}

// invokeWithAuth runs one operation with a fresh WSSE header and performs
// exactly one NotAuthorized retry after the shared resample. Context lifecycle
// is checked before header generation, before the first callback, after a
// NotAuthorized result, before resampling, before retry header generation, and
// before/after the retry callback. Every other error class
// (ActionNotSupported, InvalidArgVal, timeout/network, DTD, oversized,
// malformed, protocol, security) is returned unchanged with no retry. An error
// that also matches cancellation or deadline is always treated as lifecycle
// completion and can never enter the retry callback. A retry-token generation
// failure returns the original NotAuthorized while the parent remains active.
func (s *session) invokeWithAuth(ctx context.Context, run func(ctx context.Context, header []byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	header, err := s.headerXML()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		return err
	}
	// Header generation can take observable time (for example while obtaining
	// randomness), so check once more immediately before entering the callback.
	if err := ctx.Err(); err != nil {
		return err
	}

	callErr := run(ctx, header)
	if lifecycleErr := contextLifecycleError(ctx, callErr); lifecycleErr != nil {
		return lifecycleErr
	}
	if callErr == nil || s.creds == nil {
		return callErr
	}
	if !errors.Is(callErr, soap.ErrNotAuthorized) {
		return callErr
	}

	// NotAuthorized is retry evidence only while the parent is still active.
	if err := ctx.Err(); err != nil {
		return err
	}
	s.resampleForRetry(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}

	retryHeader, retryErr := s.headerXML()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if retryErr != nil {
		return callErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	result := run(ctx, retryHeader)
	if lifecycleErr := contextLifecycleError(ctx, result); lifecycleErr != nil {
		return lifecycleErr
	}
	return result
}

// contextLifecycleError gives an ended parent or a callback error that matches
// cancellation/deadline precedence over authentication retry evidence. Stable
// context sentinels are returned so callers can classify without retaining a
// potentially unsafe joined or wrapped callback error.
func contextLifecycleError(ctx context.Context, callbackErr error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if errors.Is(callbackErr, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(callbackErr, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return nil
}

// deriveChildBudget derives a bounded child context from parent that is the
// smaller of an absolute cap and a fraction of the parent's remaining budget.
// When the parent has no deadline, the absolute cap is used. The returned
// cancel func must always be called.
func deriveChildBudget(parent context.Context, now nowFunc, absolute time.Duration, fraction int) (context.Context, context.CancelFunc) {
	budget := absolute
	if deadline, ok := parent.Deadline(); ok {
		remaining := deadline.Sub(currentTime(now))
		if remaining <= 0 {
			return context.WithDeadline(parent, currentTime(now))
		}
		fractional := remaining * time.Duration(fraction) / 100
		if fractional < budget {
			budget = fractional
		}
	}
	if budget <= 0 {
		budget = absolute
	}
	return context.WithTimeout(parent, budget)
}

// sampleClockOffset runs the sampler against read under budgetCtx and returns
// the sampled offset, treating an invalid UTC flag as an error so a bad sample
// is never trusted.
func sampleClockOffset(ctx context.Context, sampler auth.ClockSampler, read clockReader) (auth.ClockSample, error) {
	return sampler.Sample(ctx, func(sampleCtx context.Context) (time.Time, error) {
		utc, valid, err := read(sampleCtx)
		if err != nil {
			return time.Time{}, err
		}
		if !valid {
			return time.Time{}, errors.New("device did not report a valid UTC time")
		}
		return utc, nil
	})
}

// freshUsernameTokenHeader builds a fresh WS-Security UsernameToken header
// fragment at the supplied clock offset. A fresh token (new nonce and
// timestamp) is produced on every call so no token is ever reused across
// logical attempts.
func freshUsernameTokenHeader(generator auth.UsernameTokenGenerator, creds *Credentials, offset time.Duration) ([]byte, error) {
	token, err := generator.Generate(creds.username, creds.password, offset)
	if err != nil {
		return nil, err
	}
	return token.HeaderXML()
}

// buildAuthenticatedHTTPClient composes the HTTP client used by a SOAP
// transport for one same-origin service endpoint. When credentials are present
// it wraps the transport with the application-boundary no-qop Digest policy and
// an origin-scoped auth.DigestTransport, exactly as the inventory adapter does.
// Order, outermost to innermost:
//
//	soap.Client -> auth.DigestTransport -> digestPolicyTransport -> base
//
// endpointURL must already be validated as a safe, absolute HTTP(S) endpoint.
func buildAuthenticatedHTTPClient(base *http.Client, endpointURL string, creds *Credentials) (*http.Client, error) {
	parsed, err := url.Parse(endpointURL)
	if err != nil {
		return nil, usageError("the endpoint is not valid")
	}

	var httpClient *http.Client
	if base != nil {
		clone := *base
		httpClient = &clone
	} else {
		httpClient = &http.Client{}
	}

	if creds != nil {
		underlying := httpClient.Transport
		if underlying == nil {
			underlying = http.DefaultTransport
		}
		policy, perr := newDigestPolicyTransport(underlying, creds.AllowLegacyDigest())
		if perr != nil {
			return nil, newError(CategoryInternal, "the authentication policy transport could not be constructed", perr)
		}
		scope := &url.URL{Scheme: parsed.Scheme, Host: parsed.Host}
		digest, derr := auth.NewDigestTransport(auth.DigestTransportConfig{
			Base:               policy,
			Scope:              scope,
			Credentials:        auth.NewCredentials(creds.username, creds.password),
			AllowBasic:         creds.AllowBasic(),
			AllowBasicOverHTTP: creds.AllowBasicOverHTTP(),
			AllowRFC2069:       creds.AllowLegacyDigest(),
		})
		if derr != nil {
			return nil, usageError("the credentials or endpoint are not valid for authentication")
		}
		httpClient.Transport = digest
	}

	return httpClient, nil
}

// sameOrigin reports whether candidate shares scheme, hostname, and effective
// port with reference. Both must be absolute HTTP(S) URLs with a host. The
// comparison is case-insensitive on scheme and host, and normalizes the default
// port for each scheme so http://h and http://h:80 are the same origin.
func sameOrigin(reference, candidate *url.URL) bool {
	if reference == nil || candidate == nil {
		return false
	}
	rs := strings.ToLower(reference.Scheme)
	cs := strings.ToLower(candidate.Scheme)
	if rs != cs {
		return false
	}
	if rs != "http" && rs != "https" {
		return false
	}
	if reference.Hostname() == "" || candidate.Hostname() == "" {
		return false
	}
	if !strings.EqualFold(reference.Hostname(), candidate.Hostname()) {
		return false
	}
	return effectiveOriginPort(reference) == effectiveOriginPort(candidate)
}

// effectiveOriginPort returns the explicit port or the scheme default.
func effectiveOriginPort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	default:
		return "80"
	}
}

// validateSameOriginEndpoint parses an advertised operational endpoint and
// confirms it is a safe absolute HTTP(S) URL (no userinfo, no fragment, no
// control characters, supported scheme) that is same-origin with the camera
// endpoint. It returns the parsed endpoint for request construction. The raw
// query is preserved on the returned URL because the actual request may require
// it; callers must never leak it into DTOs or errors.
func validateSameOriginEndpoint(cameraEndpoint *url.URL, rawEndpoint string) (*url.URL, error) {
	parsed, err := parseSafeOperationalURL(rawEndpoint)
	if err != nil {
		return nil, err
	}
	if !sameOrigin(cameraEndpoint, parsed) {
		return nil, usageError("the service endpoint is not same-origin with the camera")
	}
	return parsed, nil
}

// parseSafeOperationalURL parses an operational URL and rejects userinfo,
// control characters, fragments, missing host, and unsupported schemes. The raw
// query is preserved on the returned URL for request construction; it must
// never be surfaced in a DTO or error.
func parseSafeOperationalURL(raw string) (*url.URL, error) {
	if containsControl(raw) {
		return nil, usageError("the service endpoint is not valid")
	}
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() {
		return nil, usageError("the service endpoint is not valid")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, usageError("the service endpoint scheme is not supported")
	}
	if parsed.User != nil {
		return nil, usageError("the service endpoint must not contain credentials")
	}
	if parsed.Fragment != "" || parsed.RawFragment != "" {
		return nil, usageError("the service endpoint must not contain a fragment")
	}
	if parsed.Hostname() == "" {
		return nil, usageError("the service endpoint is not valid")
	}
	if urlHasDecodedControl(parsed) {
		return nil, usageError("the service endpoint is not valid")
	}
	return parsed, nil
}

// deviceReadClock adapts a typed Device client's GetSystemDateAndTime (or an
// injected test seam) to the shared clockReader shape.
func deviceReadClock(client *device.Client, clockCall func(ctx context.Context) (device.SystemDateAndTime, error)) clockReader {
	return func(ctx context.Context) (time.Time, bool, error) {
		var (
			value device.SystemDateAndTime
			err   error
		)
		if clockCall != nil {
			value, err = clockCall(ctx)
		} else {
			value, err = client.GetSystemDateAndTime(ctx)
		}
		if err != nil {
			return time.Time{}, false, err
		}
		return value.UTC, value.UTCValid, nil
	}
}
