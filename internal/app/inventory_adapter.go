package app

import (
	"context"
	"net/http"
	"time"

	"github.com/addodelgrossi/ovm/pkg/onvif/auth"
	"github.com/addodelgrossi/ovm/pkg/onvif/device"
	"github.com/addodelgrossi/ovm/pkg/onvif/discovery"
)

// defaultInventoryTimeout bounds a whole inventory use case when the caller
// supplies no timeout.
const defaultInventoryTimeout = 15 * time.Second

// Negotiation budgets bound the unauthenticated clock negotiation and the
// authentication-triggered resample so neither can consume the whole use-case
// deadline. They are shared by every use case through the session primitives in
// session.go.
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
// manual endpoint, builds the single authenticated session, negotiates
// authentication, and drives the typed Device client. It never stores or
// returns credentials.
type productionInventory struct {
	// httpClient is the base HTTP client used for the SOAP transport. A nil
	// value uses a fresh default client per request.
	httpClient *http.Client
	// console, when set, receives redacted SOAP exchanges.
	console *SOAPConsole
}

// NewProductionInventory returns the production InventoryUseCase backed by the
// real SOAP, auth, and Device packages.
func NewProductionInventory() InventoryUseCase {
	return &productionInventory{}
}

// Inventory canonicalizes the endpoint, builds the single authenticated
// session, negotiates authentication, and collects the normalized inventory.
// The supplied context and the request timeout together bound the whole use
// case.
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

// newSeam builds the single authenticated session for one endpoint and wraps it
// in the typed Device adapter. Exactly one authenticated HTTP client (one
// Digest transport + cache) and one soap.Client are constructed here.
func (p *productionInventory) newSeam(endpointURL string, timeout time.Duration, creds *Credentials) (*authDeviceSeam, error) {
	sess, err := newSession(sessionConfig{
		endpointURL:    endpointURL,
		creds:          creds,
		timeout:        timeout,
		baseHTTPClient: p.httpClient,
		sampler:        auth.ClockSampler{Attempts: negotiationAttempts},
		clock:          time.Now,
		trace:          p.console.trace(),
	})
	if err != nil {
		return nil, err
	}
	deviceClient, err := device.NewClient(sess.soapClient, endpointURL)
	if err != nil {
		return nil, newError(CategoryInternal, "the device client could not be constructed", err)
	}
	sess.setClockReader(deviceReadClock(deviceClient, nil))
	return &authDeviceSeam{session: sess, client: deviceClient}, nil
}

// authDeviceSeam is a thin typed Device adapter over the shared authenticated
// session. It owns NO credentials, generator, sampler, clock offset, or
// resample state: all of that lives in the single *session, which it shares
// with the Media flow when both run in one top-level use case. The seam's only
// job is to bind each typed Device operation to session.invokeWithAuth so the
// shared negotiation, fresh-header generation, child budgets, and single
// NotAuthorized retry are applied uniformly.
//
// The method names negotiate/call/childBudget are retained as
// delegation wrappers so the existing seam-level tests keep their behavioral
// assertions while the state they exercise lives in the session.
type authDeviceSeam struct {
	session *session
	client  *device.Client
}

// negotiate delegates the initial unauthenticated clock negotiation to the
// shared session.
func (s *authDeviceSeam) negotiate(ctx context.Context) { s.session.negotiate(ctx) }

// childBudget delegates child-budget derivation to the shared session.
func (s *authDeviceSeam) childBudget(parent context.Context, absolute time.Duration, fraction int) (context.Context, context.CancelFunc) {
	return s.session.childBudget(parent, absolute, fraction)
}

// call runs one Device operation through the shared session: a fresh
// UsernameToken header, and exactly one retry (after the shared, bounded
// resample) on a SOAP NotAuthorized fault. The device opts receive the WSSE
// header the session generated for that attempt.
func (s *authDeviceSeam) call(ctx context.Context, invoke func(ctx context.Context, opts []device.RequestOptions) error) error {
	return s.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
		var opts []device.RequestOptions
		if header != nil {
			opts = []device.RequestOptions{{HeaderXML: header}}
		}
		return invoke(ctx, opts)
	})
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
