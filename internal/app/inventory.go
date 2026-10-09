package app

import (
	"context"
	"errors"
	"net"
	"sort"
	"sync"

	"github.com/addodelgrossi/ovm/pkg/onvif/auth"
	"github.com/addodelgrossi/ovm/pkg/onvif/device"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// defaultInventoryConcurrency bounds parallel independent inventory calls when
// the caller does not request a specific limit.
const defaultInventoryConcurrency = 4

// nonClockOperations is the set of inventory operations that carry genuine
// device inventory value. GetSystemDateAndTime is deliberately excluded: the
// clock is reachable without credentials and alone does not constitute a
// useful inventory, so a result that only produced the clock is not treated as
// a success. See the "minimum useful inventory" policy documented on
// runInventory.
var nonClockOperations = map[OperationName]struct{}{
	OpDeviceInformation: {},
	OpCapabilities:      {},
	OpServices:          {},
	OpScopes:            {},
	OpHostname:          {},
	OpNetworkInterfaces: {},
}

// isNonClockOperation reports whether op is one of the six credential-relevant
// inventory operations (i.e. everything except the clock).
func isNonClockOperation(op OperationName) bool {
	_, ok := nonClockOperations[op]
	return ok
}

// deviceSeam is the narrow set of typed Device operations the inventory
// orchestration depends on. It is satisfied by the production adapter and by
// test doubles, keeping the orchestration free of transport and auth concerns.
//
// Each method returns the typed pkg/onvif/device result and an error whose
// cause remains errors.Is-compatible with soap.ErrNotAuthorized,
// soap.ErrActionNotSupported, context.Canceled, context.DeadlineExceeded, and
// the device decode sentinels, so classifyState can map it deterministically.
type deviceSeam interface {
	SystemDateAndTime(ctx context.Context) (device.SystemDateAndTime, error)
	DeviceInformation(ctx context.Context) (device.DeviceInformation, error)
	Capabilities(ctx context.Context) (device.Capabilities, error)
	Services(ctx context.Context) ([]device.Service, error)
	Scopes(ctx context.Context) ([]device.Scope, error)
	Hostname(ctx context.Context) (device.Hostname, error)
	NetworkInterfaces(ctx context.Context) ([]device.NetworkInterface, error)
}

// inventoryTask binds one operation name to a closure that performs the call
// and returns its capability status, the raw operation error (nil on success),
// and a deferred mutation that stores the successful DTO under the shared
// mutex. The raw error is used only to classify a dominant fatal failure when
// no operation succeeds; it is never rendered to users.
type inventoryTask struct {
	name OperationName
	run  func(ctx context.Context) (OperationStatus, func(), error)
}

// runInventory executes the seven independent Device operations against seam
// with deterministic ordering and bounded concurrency, mapping every outcome
// into the normalized Inventory. It supports useful partial results: a failed
// operation records a capability state and detail while successful operations
// still populate their DTO.
//
// Explicit-cancellation precedence. The supplied context bounds the whole use
// case. Semaphore acquisition is context-aware, so a cancellation observed
// while goroutines are queued stops further work promptly. Once the use case
// has started, an explicit cancellation (context.Canceled) of the parent
// context ALWAYS wins: the function returns whatever partial Inventory was
// assembled together with a CategoryCancelled error, even if some operations
// succeeded or competing authentication/protocol/network failures were also
// observed. This maps to exit code 130 at the CLI boundary. A deadline
// (context.DeadlineExceeded) is a timeout, not an explicit cancellation, and
// is left to the ordinary dominant-failure policy as a network condition.
//
// Minimum useful inventory. GetSystemDateAndTime is reachable without
// credentials and alone does not qualify as a useful inventory. A result is
// useful (nil error) only when at least one non-clock operation is supported,
// or when all six non-clock operations are definitively unsupported (a
// responsive device that answered every request). When the clock succeeds but
// every credential-protected operation was authentication-rejected, the
// partial clock data is returned together with a CategoryAuthentication error.
// Mixed unsupported/transient non-clock failures are never classified as
// authentication; per-operation states remain accurate and a defensible fatal
// policy selects the dominant category.
func runInventory(ctx context.Context, endpoint string, seam deviceSeam, maxConcurrency int) (Inventory, error) {
	if err := ctx.Err(); err != nil {
		return Inventory{}, classifyFatal(err)
	}
	if maxConcurrency <= 0 {
		maxConcurrency = defaultInventoryConcurrency
	}

	// The DTO endpoint is redacted at the boundary; the raw (canonical) URL is
	// still used by the caller for transport. A clean canonical endpoint passes
	// through unchanged.
	inv := Inventory{Endpoint: redactBoundaryTransportURI(endpoint)}
	var mu sync.Mutex

	// states collects per-operation results keyed by operation so the final
	// Operations slice can be assembled in the fixed deterministic order,
	// independent of goroutine completion order. errs retains the raw per-
	// operation error (if any) solely for dominant-failure classification.
	states := make(map[OperationName]OperationStatus, len(orderedOperations))
	errs := make(map[OperationName]error, len(orderedOperations))
	record := func(op OperationName, status OperationStatus, opErr error, apply func()) {
		mu.Lock()
		defer mu.Unlock()
		states[op] = status
		if opErr != nil {
			errs[op] = opErr
		}
		if apply != nil {
			apply()
		}
	}

	tasks := inventoryTasks(seam, &inv)

	semaphore := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	for _, task := range tasks {
		wg.Add(1)
		go func(t inventoryTask) {
			defer wg.Done()
			// Context-aware acquisition: if the use case is cancelled while
			// this goroutine is still queued for a slot, record a cancellation
			// state instead of blocking. This keeps cancellation responsive
			// under a saturated semaphore and keeps per-operation states
			// deterministic.
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				state, detail := classifyState(ctx.Err())
				record(t.name, OperationStatus{Operation: t.name, State: state, Detail: detail}, ctx.Err(), nil)
				return
			}
			defer func() { <-semaphore }()
			status, apply, opErr := t.run(ctx)
			record(t.name, status, opErr, apply)
		}(task)
	}
	wg.Wait()

	inv.Operations = make([]OperationStatus, 0, len(orderedOperations))
	anyNonClockSupported := false
	allNonClockUnsupported := true
	for _, op := range orderedOperations {
		status, ok := states[op]
		if !ok {
			continue
		}
		inv.Operations = append(inv.Operations, status)
		if !isNonClockOperation(op) {
			continue
		}
		if status.State == CapabilitySupported {
			anyNonClockSupported = true
		}
		if status.State != CapabilityUnsupported {
			allNonClockUnsupported = false
		}
	}

	// Explicit cancellation of the parent context wins unconditionally once the
	// use case has started, regardless of any successes or competing failures.
	// A deadline is intentionally excluded: it is a timeout handled by the
	// ordinary dominant-failure policy below.
	if errors.Is(ctx.Err(), context.Canceled) {
		return inv, newError(CategoryCancelled, "the operation was cancelled", ctx.Err())
	}

	// A useful inventory is one with at least one supported non-clock
	// operation, or one where every non-clock operation is definitively
	// unsupported (a responsive device answered every request). Clock-only
	// success is not useful on its own.
	if anyNonClockSupported || allNonClockUnsupported {
		return inv, nil
	}

	if fatal := dominantFailure(inv.Operations, errs); fatal != nil {
		return inv, fatal
	}
	return inv, nil
}

// inventoryTasks builds the per-operation task closures. The slice order is not
// significant for correctness because results are reassembled by orderedOperations,
// but it is kept in the canonical order for readability.
func inventoryTasks(seam deviceSeam, inv *Inventory) []inventoryTask {
	return []inventoryTask{
		{
			name: OpSystemDateAndTime,
			run: func(ctx context.Context) (OperationStatus, func(), error) {
				value, err := seam.SystemDateAndTime(ctx)
				if status, failed := failedStatus(OpSystemDateAndTime, err); failed {
					return status, nil, err
				}
				dto := normalizeSystemDateAndTime(value)
				return supportedStatus(OpSystemDateAndTime), func() { inv.SystemDateAndTime = &dto }, nil
			},
		},
		{
			name: OpDeviceInformation,
			run: func(ctx context.Context) (OperationStatus, func(), error) {
				value, err := seam.DeviceInformation(ctx)
				if status, failed := failedStatus(OpDeviceInformation, err); failed {
					return status, nil, err
				}
				dto := normalizeDeviceInformation(value)
				return supportedStatus(OpDeviceInformation), func() { inv.DeviceInformation = &dto }, nil
			},
		},
		{
			name: OpCapabilities,
			run: func(ctx context.Context) (OperationStatus, func(), error) {
				value, err := seam.Capabilities(ctx)
				if status, failed := failedStatus(OpCapabilities, err); failed {
					return status, nil, err
				}
				dto := normalizeCapabilities(value)
				return supportedStatus(OpCapabilities), func() { inv.Capabilities = &dto }, nil
			},
		},
		{
			name: OpServices,
			run: func(ctx context.Context) (OperationStatus, func(), error) {
				value, err := seam.Services(ctx)
				if status, failed := failedStatus(OpServices, err); failed {
					return status, nil, err
				}
				dto := normalizeServices(value)
				return supportedStatus(OpServices), func() { inv.Services = dto }, nil
			},
		},
		{
			name: OpScopes,
			run: func(ctx context.Context) (OperationStatus, func(), error) {
				value, err := seam.Scopes(ctx)
				if status, failed := failedStatus(OpScopes, err); failed {
					return status, nil, err
				}
				dto := normalizeScopes(value)
				return supportedStatus(OpScopes), func() { inv.Scopes = dto }, nil
			},
		},
		{
			name: OpHostname,
			run: func(ctx context.Context) (OperationStatus, func(), error) {
				value, err := seam.Hostname(ctx)
				if status, failed := failedStatus(OpHostname, err); failed {
					return status, nil, err
				}
				dto := normalizeHostname(value)
				return supportedStatus(OpHostname), func() { inv.Hostname = &dto }, nil
			},
		},
		{
			name: OpNetworkInterfaces,
			run: func(ctx context.Context) (OperationStatus, func(), error) {
				value, err := seam.NetworkInterfaces(ctx)
				if status, failed := failedStatus(OpNetworkInterfaces, err); failed {
					return status, nil, err
				}
				dto := normalizeNetworkInterfaces(value)
				return supportedStatus(OpNetworkInterfaces), func() { inv.NetworkInterfaces = dto }, nil
			},
		},
	}
}

// supportedStatus builds a clean supported status with no detail.
func supportedStatus(op OperationName) OperationStatus {
	return OperationStatus{Operation: op, State: CapabilitySupported}
}

// failedStatus maps a non-nil operation error to a capability status, returning
// failed=true when err is non-nil. A nil error returns the zero status and
// failed=false so the caller proceeds to the success path.
func failedStatus(op OperationName, err error) (OperationStatus, bool) {
	if err == nil {
		return OperationStatus{}, false
	}
	state, detail := classifyState(err)
	return OperationStatus{Operation: op, State: state, Detail: detail}, true
}

// classifyState maps a Device operation error to a capability state and a
// safe, credential-free detail using the fixed policy:
//   - ActionNotSupported            -> unsupported
//   - transient network / timeout   -> temporarily_unavailable
//   - authentication / protocol /
//     decode ambiguity              -> unknown
//
// The detail text is derived only from stable classifications, never from raw
// error strings, so it cannot leak credential-bearing content a lower layer
// might (hypothetically) include.
func classifyState(err error) (CapabilityState, string) {
	switch {
	case errors.Is(err, soap.ErrActionNotSupported):
		return CapabilityUnsupported, "the device reported the operation is not supported"
	case errors.Is(err, context.Canceled):
		return CapabilityTemporarilyUnavailable, "the operation was cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return CapabilityTemporarilyUnavailable, "the operation timed out"
	case isAuthenticationError(err):
		return CapabilityUnknown, "authentication was rejected, so support is undetermined"
	case isProtocolError(err):
		return CapabilityUnknown, "the response could not be interpreted, so support is undetermined"
	case isNetworkError(err):
		return CapabilityTemporarilyUnavailable, "a transient network error prevented a result"
	default:
		return CapabilityUnknown, "the operation outcome was ambiguous"
	}
}

// isAuthenticationError reports whether err is an authentication/authorization
// rejection: an ONVIF NotAuthorized SOAP fault, an unsupported Digest challenge,
// an auth-layer policy refusal (for example Basic disabled or legacy no-qop
// Digest disabled), the app-boundary insecure-Digest block, or a bare HTTP
// 401/403 response that carried no SOAP fault.
//
// The auth policy refusal is checked before the network classifier even though
// it surfaces wrapped in a *url.Error (which satisfies net.Error): a deliberate
// policy decision not to transmit a weak credential is an authentication
// outcome, not a transport failure, and classifyState/categorizeFatal consult
// this function before isNetworkError.
func isAuthenticationError(err error) bool {
	if errors.Is(err, soap.ErrNotAuthorized) || errors.Is(err, auth.ErrUnsupportedDigestChallenge) {
		return true
	}
	if errors.Is(err, auth.ErrAuthPolicy) {
		return true
	}
	if errors.Is(err, ErrInsecureDigestBlocked) {
		return true
	}
	var httpErr *soap.HTTPError
	if errors.As(err, &httpErr) && (httpErr.StatusCode == 401 || httpErr.StatusCode == 403) {
		return true
	}
	return false
}

// isProtocolError reports whether err is a SOAP/device protocol or decode
// problem that leaves support undetermined. A bare HTTP 401/403 is handled as
// authentication before this check and is therefore excluded here.
func isProtocolError(err error) bool {
	if errors.Is(err, device.ErrMalformedXML) ||
		errors.Is(err, device.ErrUnexpectedElement) ||
		errors.Is(err, device.ErrMissingField) ||
		errors.Is(err, device.ErrInvalidField) ||
		errors.Is(err, device.ErrNilResponse) {
		return true
	}
	if errors.Is(err, soap.ErrVersionMismatch) ||
		errors.Is(err, soap.ErrDTDNotAllowed) ||
		errors.Is(err, soap.ErrInvalidArgVal) ||
		errors.Is(err, auth.ErrRequestBodyNotReplayable) {
		return true
	}
	var parseErr *soap.ParseError
	if errors.As(err, &parseErr) {
		return true
	}
	var tooLarge *soap.ResponseTooLargeError
	if errors.As(err, &tooLarge) {
		return true
	}
	var httpErr *soap.HTTPError
	if errors.As(err, &httpErr) {
		// A non-success HTTP status with no SOAP fault (other than the 401/403
		// handled as authentication) is a protocol-level ambiguity rather than
		// a transient network condition.
		return true
	}
	return false
}

// isNetworkError reports whether err is a concrete network or timeout
// condition. The device/soap packages return the underlying transport error
// wrapped, so a context deadline, a net.Error, a connect/DNS failure, or a
// connection refused surfaces here.
//
// Callers consult isAuthenticationError and isProtocolError first, so an
// auth-policy refusal wrapped in a *url.Error is already classified as
// authentication before it reaches this check.
func isNetworkError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	// net.OpError wraps syscall-level failures such as connection refused and
	// DNS resolution errors; errors.As on net.Error covers *net.OpError and
	// *net.DNSError because both implement net.Error, but a bare
	// *net.AddrError or a *os.SyscallError wrapped under *net.OpError is also
	// caught by the explicit *net.OpError check below.
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}

// classifyFatal maps a pre-flight context error to a typed fatal application
// error used only when no operation could run.
func classifyFatal(err error) *Error {
	switch {
	case errors.Is(err, context.Canceled):
		return newError(CategoryCancelled, "the operation was cancelled", err)
	case errors.Is(err, context.DeadlineExceeded):
		return newError(CategoryNetwork, "the operation timed out before it could start", err)
	default:
		return newError(CategoryInternal, "the operation could not start", err)
	}
}

// dominantFailure promotes the dominant failure among the per-operation errors
// to a typed, user-safe *Error, used only when no operation succeeded and the
// result is not a valid all-unsupported inventory.
//
// Policy (authentication is deliberately NOT given blanket priority, so a
// single auth rejection cannot mask a competing transport or protocol fault):
//
//   - Authentication is chosen only in two cases:
//     1. Every credential-protected (non-clock) operation was
//     authentication-rejected. A responsive device that refuses all six
//     protected calls on credentials is an authentication verdict.
//     2. Authentication is the only unresolved failure category present. An
//     unsupported action contributes no category, so it may coexist with
//     an auth-only failure set without retyping the verdict.
//   - Otherwise, when any protocol, network, or cancellation failure competes
//     with authentication, the dominant category follows a fixed precedence
//     that prefers the most authoritative, actionable cause:
//     cancelled > protocol > network > auth > internal. A competing transport
//     or protocol fault therefore wins over a partial authentication rejection.
//
// Explicit parent-context cancellation is handled unconditionally by the caller
// before this function is reached, so a user interrupt is never reclassified
// here.
//
// The returned message is derived solely from the stable category, never from
// a raw lower-layer error string, so it cannot leak credential-bearing content.
func dominantFailure(operations []OperationStatus, errs map[OperationName]error) *Error {
	if len(operations) == 0 {
		return nil
	}

	// Count the typed categories and whether every operation was unsupported.
	var (
		haveAuth      bool
		authCause     error
		haveProtocol  bool
		protocolCause error
		haveNetwork   bool
		networkCause  error
		haveCancelled bool
		cancelCause   error
		haveInternal  bool
		internalCause error
	)

	// Track the non-clock credential-protected operations specifically to
	// decide the "clock-only success with everything else auth-rejected" case.
	nonClockSeen := 0
	nonClockAuthRejected := 0

	for _, status := range operations {
		if isNonClockOperation(status.Operation) {
			nonClockSeen++
		}
		err := errs[status.Operation]
		if err == nil {
			// A non-supported state with no retained error (for example an
			// unsupported action is modeled by state, not by a retained raw
			// error) contributes nothing to the fatal classification.
			continue
		}
		category := categorizeFatal(err)
		if isNonClockOperation(status.Operation) && category == CategoryAuthentication {
			nonClockAuthRejected++
		}
		switch category {
		case CategoryAuthentication:
			haveAuth = true
			authCause = err
		case CategoryProtocol:
			haveProtocol = true
			protocolCause = err
		case CategoryNetwork:
			haveNetwork = true
			networkCause = err
		case CategoryCancelled:
			haveCancelled = true
			cancelCause = err
		default:
			haveInternal = true
			internalCause = err
		}
	}

	// Case 1: every credential-protected operation was authentication-rejected.
	// A responsive device refusing all six protected calls on credentials is an
	// authentication verdict regardless of any clock-only partial success.
	if nonClockSeen > 0 && nonClockAuthRejected == nonClockSeen {
		return newError(CategoryAuthentication, "authentication was rejected by the device", authCause)
	}

	// Case 2: authentication is the only unresolved failure category. Unsupported
	// actions contribute no category and so do not count as competition here.
	if haveAuth && !haveProtocol && !haveNetwork && !haveCancelled && !haveInternal {
		return newError(CategoryAuthentication, "authentication was rejected by the device", authCause)
	}

	// Any competing protocol, network, or cancellation failure outranks a
	// partial authentication rejection. Precedence is fixed and favours the most
	// authoritative, actionable cause: cancelled > protocol > network > auth >
	// internal.
	switch {
	case haveCancelled:
		return newError(CategoryCancelled, "the operation was cancelled", cancelCause)
	case haveProtocol:
		return newError(CategoryProtocol, "the device response could not be interpreted", protocolCause)
	case haveNetwork:
		return newError(CategoryNetwork, "the device could not be reached", networkCause)
	case haveAuth:
		return newError(CategoryAuthentication, "authentication was rejected by the device", authCause)
	case haveInternal:
		return newError(CategoryInternal, "the inventory could not be completed", internalCause)
	default:
		return nil
	}
}

// categorizeFatal maps a single raw operation error to the stable error
// category used for dominant-failure promotion. ActionNotSupported never
// contributes a fatal category because it is a valid, responsive outcome.
func categorizeFatal(err error) ErrorCategory {
	switch {
	case errors.Is(err, soap.ErrActionNotSupported):
		return ""
	case isAuthenticationError(err):
		return CategoryAuthentication
	case errors.Is(err, context.Canceled):
		return CategoryCancelled
	case isNetworkError(err):
		// Timeouts and concrete socket failures are network conditions. A
		// context deadline is included here because it is a timeout, not a
		// protocol ambiguity.
		return CategoryNetwork
	case isProtocolError(err):
		return CategoryProtocol
	default:
		return CategoryInternal
	}
}

// --- normalization helpers -------------------------------------------------

func normalizeSystemDateAndTime(value device.SystemDateAndTime) SystemDateAndTimeDTO {
	dto := SystemDateAndTimeDTO{
		DateTimeType:    value.DateTimeType,
		DaylightSavings: value.DaylightSavings,
		TimeZone:        value.TimeZone,
	}
	if value.UTCValid {
		dto.UTC = value.UTC.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return dto
}

func normalizeDeviceInformation(value device.DeviceInformation) DeviceInformationDTO {
	return DeviceInformationDTO{
		Manufacturer:    value.Manufacturer,
		Model:           value.Model,
		FirmwareVersion: value.FirmwareVersion,
		SerialNumber:    value.SerialNumber,
		HardwareID:      value.HardwareID,
	}
}

func normalizeCapabilities(value device.Capabilities) CapabilitiesDTO {
	return CapabilitiesDTO{
		DeviceXAddr:    redactBoundaryTransportURI(value.DeviceXAddr),
		MediaXAddr:     redactBoundaryTransportURI(value.MediaXAddr),
		EventsXAddr:    redactBoundaryTransportURI(value.EventsXAddr),
		ImagingXAddr:   redactBoundaryTransportURI(value.ImagingXAddr),
		PTZXAddr:       redactBoundaryTransportURI(value.PTZXAddr),
		AnalyticsXAddr: redactBoundaryTransportURI(value.AnalyticsXAddr),
	}
}

func normalizeServices(values []device.Service) []ServiceDTO {
	out := make([]ServiceDTO, 0, len(values))
	for _, s := range values {
		out = append(out, ServiceDTO{
			Namespace:    redactBoundaryURI(s.Namespace),
			XAddr:        redactBoundaryTransportURI(s.XAddr),
			VersionMajor: s.Version.Major,
			VersionMinor: s.Version.Minor,
			HasVersion:   s.HasVersion,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].XAddr < out[j].XAddr
	})
	return out
}

func normalizeScopes(values []device.Scope) []ScopeDTO {
	out := make([]ScopeDTO, 0, len(values))
	for _, s := range values {
		out = append(out, ScopeDTO{Definition: s.Definition, Item: redactBoundaryURI(s.Item)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Item != out[j].Item {
			return out[i].Item < out[j].Item
		}
		return out[i].Definition < out[j].Definition
	})
	return out
}

func normalizeHostname(value device.Hostname) HostnameDTO {
	return HostnameDTO{FromDHCP: value.FromDHCP, Name: value.Name}
}

func normalizeNetworkInterfaces(values []device.NetworkInterface) []NetworkInterfaceDTO {
	out := make([]NetworkInterfaceDTO, 0, len(values))
	for _, ni := range values {
		addresses := make([]IPAddressDTO, 0, len(ni.IPv4Addresses))
		for _, a := range ni.IPv4Addresses {
			addresses = append(addresses, IPAddressDTO{
				Address:      a.Address,
				PrefixLength: a.PrefixLength,
				FromDHCP:     a.FromDHCP,
			})
		}
		sort.SliceStable(addresses, func(i, j int) bool {
			if addresses[i].Address != addresses[j].Address {
				return addresses[i].Address < addresses[j].Address
			}
			return addresses[i].PrefixLength < addresses[j].PrefixLength
		})
		out = append(out, NetworkInterfaceDTO{
			Token:         ni.Token,
			Enabled:       ni.Enabled,
			HasEnabled:    ni.HasEnabled,
			Name:          ni.Name,
			HwAddress:     ni.HwAddress,
			MTU:           ni.MTU,
			HasMTU:        ni.HasMTU,
			IPv4Enabled:   ni.IPv4Enabled,
			HasIPv4:       ni.HasIPv4,
			DHCP:          ni.DHCP,
			HasDHCP:       ni.HasDHCP,
			IPv4Addresses: addresses,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Token < out[j].Token })
	return out
}
