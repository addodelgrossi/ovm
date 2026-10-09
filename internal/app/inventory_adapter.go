package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/addodelgrossi/ovm/pkg/onvif/auth"
	"github.com/addodelgrossi/ovm/pkg/onvif/device"
	"github.com/addodelgrossi/ovm/pkg/onvif/discovery"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// defaultInventoryTimeout bounds a whole inventory use case when the caller
// supplies no timeout.
const defaultInventoryTimeout = 15 * time.Second

// Negotiation budgets bound the unauthenticated clock negotiation and the
// authentication-triggered resample so neither can consume the whole use-case
// deadline.
const (
	// negotiationMaxBudget caps the initial unauthenticated clock negotiation
	// at an absolute 2s and no more than negotiationBudgetFraction of the
	// remaining parent budget, whichever is smaller.
	negotiationMaxBudget      = 2 * time.Second
	negotiationBudgetFraction = 20 // percent of remaining parent budget

	// negotiationAttempts bounds the clock sampling attempts during initial
	// negotiation so a slow or flapping device cannot stall negotiation.
	negotiationAttempts = 2

	// resampleMaxBudget caps the single authentication-triggered clock resample
	// at an absolute 750ms and no more than resampleBudgetFraction of the
	// remaining parent budget, whichever is smaller.
	resampleMaxBudget      = 750 * time.Millisecond
	resampleBudgetFraction = 10 // percent of remaining parent budget

	// resampleAttempts bounds the resample to a single attempt: it is a best
	// effort refresh before one fresh-token retry, not a retry loop.
	resampleAttempts = 1
)

// productionInventory is the production InventoryUseCase. It canonicalizes the
// manual endpoint, composes a SOAP client (optionally wrapped by an
// origin-scoped HTTP Digest transport behind an application-boundary no-qop
// Digest policy), negotiates authentication, and drives the typed Device
// client. It never stores or returns credentials.
type productionInventory struct {
	// httpClient is the base HTTP client used for the SOAP transport. A nil
	// value uses a fresh default client per request.
	httpClient *http.Client
}

// NewProductionInventory returns the production InventoryUseCase backed by the
// real SOAP, auth, and Device packages.
func NewProductionInventory() InventoryUseCase {
	return &productionInventory{}
}

// Inventory canonicalizes the endpoint, builds the transport chain, negotiates
// authentication, and collects the normalized inventory. The supplied context
// and the request timeout together bound the whole use case.
func (p *productionInventory) Inventory(ctx context.Context, request InventoryRequest) (Inventory, error) {
	endpoint, err := discovery.CanonicalizeEndpoint(request.Host)
	if err != nil {
		return Inventory{}, usageError("the endpoint is not valid")
	}
	endpointURL := endpoint.String()

	timeout := request.Timeout
	if timeout <= 0 {
		timeout = defaultInventoryTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	seam, err := p.newSeam(endpointURL, timeout, request.Credentials)
	if err != nil {
		return Inventory{}, err
	}

	// Negotiation: perform the clock call first without a WS-Security header so
	// the HTTP transport itself may answer a Digest challenge, and so a sampled
	// clock offset is available for subsequent UsernameToken headers. The
	// negotiation runs under a short child budget so it cannot consume the
	// whole use-case deadline.
	seam.negotiate(ctx)

	return runInventory(ctx, endpointURL, seam, request.MaxConcurrency)
}

// newSeam builds a deviceSeam over the composed transport chain for one
// endpoint. It validates the endpoint origin for the Digest scope and rejects
// credential/endpoint combinations that cannot be served safely.
//
// Transport chain (credentialed): the application-boundary digestPolicyTransport
// sits as the innermost transport so it inspects the final Authorization header
// the auth.DigestTransport generates and can veto a legacy no-qop credential
// before it reaches the network. Order, outermost to innermost:
//
//	soap.Client -> auth.DigestTransport -> digestPolicyTransport -> base
func (p *productionInventory) newSeam(endpointURL string, timeout time.Duration, creds *Credentials) (*authDeviceSeam, error) {
	parsed, err := url.Parse(endpointURL)
	if err != nil {
		return nil, usageError("the endpoint is not valid")
	}

	base := p.httpClient
	var httpClient *http.Client
	if base != nil {
		clone := *base
		httpClient = &clone
	} else {
		httpClient = &http.Client{}
	}

	// When credentials are supplied, wrap the HTTP transport in an
	// origin-scoped Digest transport so a Digest challenge is answered
	// automatically and credentials are never sent to a redirected origin. The
	// Digest transport's base is the application-boundary no-qop policy
	// transport, which blocks a legacy no-qop Digest Authorization unless the
	// legacy opt-in is set.
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
			Base:        policy,
			Scope:       scope,
			Credentials: auth.NewCredentials(creds.username, creds.password),
			AllowBasic:  creds.AllowBasic(),
			// Plaintext Basic is a distinct, higher-risk opt-in. Enabling
			// Basic for an HTTPS camera must never silently authorize reusable
			// credentials on an HTTP endpoint.
			AllowBasicOverHTTP: creds.AllowBasicOverHTTP(),
			// Legacy no-qop is also gated in auth so the challenge is only
			// selected when explicitly permitted; the boundary policy transport
			// is an independent second layer that blocks the generated header.
			AllowRFC2069: creds.AllowLegacyDigest(),
		})
		if derr != nil {
			// A scope/credential validation failure is a usage problem; the
			// error text from auth is credential-free but we substitute a
			// stable message anyway.
			return nil, usageError("the credentials or endpoint are not valid for authentication")
		}
		httpClient.Transport = digest
	}

	soapClient, err := soap.NewClient(soap.Config{HTTPClient: httpClient, Timeout: timeout})
	if err != nil {
		return nil, newError(CategoryInternal, "the SOAP client could not be constructed", err)
	}
	deviceClient, err := device.NewClient(soapClient, endpointURL)
	if err != nil {
		return nil, newError(CategoryInternal, "the device client could not be constructed", err)
	}

	return &authDeviceSeam{
		client:    deviceClient,
		creds:     creds,
		generator: auth.UsernameTokenGenerator{},
		sampler:   auth.ClockSampler{Attempts: negotiationAttempts},
		clock:     time.Now,
	}, nil
}

// authDeviceSeam adapts the typed Device client to the inventory deviceSeam,
// applying WS-Security UsernameToken headers for authenticated operations and
// retrying a SOAP NotAuthorized fault once with a freshly generated token at
// the sampled clock offset. It never retries authentication on network or
// protocol errors.
type authDeviceSeam struct {
	client    *device.Client
	creds     *Credentials
	generator auth.UsernameTokenGenerator
	sampler   auth.ClockSampler
	// clock supplies the current time for budget computation; overridable in
	// tests. A nil clock defers to time.Now.
	clock func() time.Time

	// offsetMu protects the sampled clock offset against concurrent inventory
	// operations: many operations run in parallel and any one of them may
	// trigger a single shared authentication resample.
	offsetMu    sync.Mutex
	clockOffset time.Duration
	haveOffset  bool

	// resampleOnce ensures the authentication-triggered resample happens at
	// most once across all concurrent operations for the whole use case.
	resampleOnce sync.Once

	// clockCall is the clock-sampling function used by both negotiate and
	// resampleForRetry. In production it is nil and both paths read the device
	// clock through s.client.GetSystemDateAndTime. Tests may inject a
	// deterministic double to drive the NotAuthorized resample path without a
	// network or a device, and to count how many times a shared resample runs.
	clockCall func(ctx context.Context) (device.SystemDateAndTime, error)
}

// sampleClock reads the device clock through the injected clockCall seam when
// present, falling back to the real typed Device client otherwise. Centralizing
// the read lets a test observe and control every clock sample deterministically.
func (s *authDeviceSeam) sampleClock(ctx context.Context) (device.SystemDateAndTime, error) {
	if s.clockCall != nil {
		return s.clockCall(ctx)
	}
	return s.client.GetSystemDateAndTime(ctx)
}

// now returns the seam's current time, defaulting to time.Now.
func (s *authDeviceSeam) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now()
}

// childBudget derives a bounded child context from parent that is the smaller
// of an absolute cap and a fraction of the parent's remaining budget. When the
// parent has no deadline, the absolute cap is used. The returned cancel func
// must always be called.
func (s *authDeviceSeam) childBudget(parent context.Context, absolute time.Duration, fraction int) (context.Context, context.CancelFunc) {
	budget := absolute
	if deadline, ok := parent.Deadline(); ok {
		remaining := deadline.Sub(s.now())
		if remaining <= 0 {
			// Parent already past its deadline: return an immediately expired
			// child so the sampler observes cancellation.
			return context.WithDeadline(parent, s.now())
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

// negotiate performs the first clock call without a WS-Security header under a
// short child budget to let the HTTP transport answer a Digest challenge and to
// sample the device clock offset for subsequent UsernameToken headers. Failures
// are non-fatal: the inventory still runs and reports per-operation states. The
// child budget is the smaller of negotiationMaxBudget and
// negotiationBudgetFraction of the remaining parent budget, and sampling is
// bounded to negotiationAttempts.
func (s *authDeviceSeam) negotiate(ctx context.Context) {
	if s.creds == nil {
		return
	}
	budgetCtx, cancel := s.childBudget(ctx, negotiationMaxBudget, negotiationBudgetFraction)
	defer cancel()

	sample, err := s.sampler.Sample(budgetCtx, func(sampleCtx context.Context) (time.Time, error) {
		value, callErr := s.sampleClock(sampleCtx)
		if callErr != nil {
			return time.Time{}, callErr
		}
		if !value.UTCValid {
			return time.Time{}, errors.New("device did not report a valid UTC time")
		}
		return value.UTC, nil
	})
	if err != nil {
		return
	}
	s.offsetMu.Lock()
	s.clockOffset = sample.Offset
	s.haveOffset = true
	s.offsetMu.Unlock()
}

// resampleForRetry performs at most one shared clock resample before a
// fresh-token authentication retry. It is invoked only after a SOAP
// NotAuthorized fault, and runs at most once for the whole use case regardless
// of how many concurrent operations observed the fault. It honours parent
// cancellation through a short child budget (the smaller of resampleMaxBudget
// and resampleBudgetFraction of the remaining parent budget) and bounds itself
// to resampleAttempts.
//
// Fallback: if the resample fails (timeout, cancellation, or an unreadable
// clock), the previously sampled offset — or a zero offset when none was ever
// sampled — is retained and the fresh-token retry proceeds with that value. The
// retry still generates a brand-new nonce and timestamp, so a failed resample
// degrades the offset accuracy but never blocks the retry.
func (s *authDeviceSeam) resampleForRetry(ctx context.Context) {
	s.resampleOnce.Do(func() {
		budgetCtx, cancel := s.childBudget(ctx, resampleMaxBudget, resampleBudgetFraction)
		defer cancel()

		sampler := s.sampler
		sampler.Attempts = resampleAttempts
		sample, err := sampler.Sample(budgetCtx, func(sampleCtx context.Context) (time.Time, error) {
			value, callErr := s.sampleClock(sampleCtx)
			if callErr != nil {
				return time.Time{}, callErr
			}
			if !value.UTCValid {
				return time.Time{}, errors.New("device did not report a valid UTC time")
			}
			return value.UTC, nil
		})
		if err != nil {
			// Fallback: retain the existing offset (or zero) and let the retry
			// proceed. The offset state is left untouched.
			return
		}
		s.offsetMu.Lock()
		s.clockOffset = sample.Offset
		s.haveOffset = true
		s.offsetMu.Unlock()
	})
}

// requestOptions builds the per-call RequestOptions. For authenticated
// sessions it attaches a fresh UsernameToken header computed at the sampled
// clock offset; for unauthenticated sessions it returns no options. The offset
// read is guarded so a concurrent resample cannot race the read.
func (s *authDeviceSeam) requestOptions() ([]device.RequestOptions, error) {
	if s.creds == nil {
		return nil, nil
	}
	s.offsetMu.Lock()
	offset := time.Duration(0)
	if s.haveOffset {
		offset = s.clockOffset
	}
	s.offsetMu.Unlock()
	token, err := s.generator.Generate(s.creds.username, s.creds.password, offset)
	if err != nil {
		return nil, err
	}
	header, err := token.HeaderXML()
	if err != nil {
		return nil, err
	}
	return []device.RequestOptions{{HeaderXML: header}}, nil
}

// call runs one Device operation with a fresh UsernameToken header, retrying a
// single time with a new token when the device returns a SOAP NotAuthorized
// fault. Before the retry it performs the shared, bounded clock resample.
// Network, protocol, and decode errors are returned unchanged and never trigger
// an authentication retry.
func (s *authDeviceSeam) call(ctx context.Context, invoke func(ctx context.Context, opts []device.RequestOptions) error) error {
	opts, err := s.requestOptions()
	if err != nil {
		return err
	}
	callErr := invoke(ctx, opts)
	if callErr == nil || s.creds == nil {
		return callErr
	}
	if !errors.Is(callErr, soap.ErrNotAuthorized) {
		return callErr
	}
	// A genuine NotAuthorized fault triggered the retry path: resample the clock
	// once (shared across all operations) in case the first token was rejected
	// for a skew reason, then retry exactly once with a freshly generated token.
	s.resampleForRetry(ctx)
	retryOpts, retryErr := s.requestOptions()
	if retryErr != nil {
		return callErr
	}
	return invoke(ctx, retryOpts)
}

func (s *authDeviceSeam) SystemDateAndTime(ctx context.Context) (device.SystemDateAndTime, error) {
	var out device.SystemDateAndTime
	err := s.call(ctx, func(ctx context.Context, opts []device.RequestOptions) error {
		value, callErr := s.client.GetSystemDateAndTime(ctx, opts...)
		if callErr != nil {
			return callErr
		}
		out = value
		return nil
	})
	return out, err
}

func (s *authDeviceSeam) DeviceInformation(ctx context.Context) (device.DeviceInformation, error) {
	var out device.DeviceInformation
	err := s.call(ctx, func(ctx context.Context, opts []device.RequestOptions) error {
		value, callErr := s.client.GetDeviceInformation(ctx, opts...)
		if callErr != nil {
			return callErr
		}
		out = value
		return nil
	})
	return out, err
}

func (s *authDeviceSeam) Capabilities(ctx context.Context) (device.Capabilities, error) {
	var out device.Capabilities
	err := s.call(ctx, func(ctx context.Context, opts []device.RequestOptions) error {
		value, callErr := s.client.GetCapabilities(ctx, opts...)
		if callErr != nil {
			return callErr
		}
		out = value
		return nil
	})
	return out, err
}

func (s *authDeviceSeam) Services(ctx context.Context) ([]device.Service, error) {
	var out []device.Service
	err := s.call(ctx, func(ctx context.Context, opts []device.RequestOptions) error {
		value, callErr := s.client.GetServices(ctx, opts...)
		if callErr != nil {
			return callErr
		}
		out = value
		return nil
	})
	return out, err
}

func (s *authDeviceSeam) Scopes(ctx context.Context) ([]device.Scope, error) {
	var out []device.Scope
	err := s.call(ctx, func(ctx context.Context, opts []device.RequestOptions) error {
		value, callErr := s.client.GetScopes(ctx, opts...)
		if callErr != nil {
			return callErr
		}
		out = value
		return nil
	})
	return out, err
}

func (s *authDeviceSeam) Hostname(ctx context.Context) (device.Hostname, error) {
	var out device.Hostname
	err := s.call(ctx, func(ctx context.Context, opts []device.RequestOptions) error {
		value, callErr := s.client.GetHostname(ctx, opts...)
		if callErr != nil {
			return callErr
		}
		out = value
		return nil
	})
	return out, err
}

func (s *authDeviceSeam) NetworkInterfaces(ctx context.Context) ([]device.NetworkInterface, error) {
	var out []device.NetworkInterface
	err := s.call(ctx, func(ctx context.Context, opts []device.RequestOptions) error {
		value, callErr := s.client.GetNetworkInterfaces(ctx, opts...)
		if callErr != nil {
			return callErr
		}
		out = value
		return nil
	})
	return out, err
}
