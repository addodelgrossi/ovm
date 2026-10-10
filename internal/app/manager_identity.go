package app

import (
	"net/url"
	"strings"

	"github.com/addodelgrossi/ovm/internal/store"
)

// preparedIdentity is the pure, order-independent result of canonicalizing one
// discovered device off the hot path. Computing it never touches the
// repository, so the manager can compute it with bounded concurrency and then
// persist decisions deterministically.
type preparedIdentity struct {
	// endpoint is the canonical, query-free safe endpoint, or empty when none
	// of the discovered addresses was safe.
	endpoint string
	// epr is the normalized endpoint reference when valid and non-empty.
	epr string
	// hasEPR reports whether epr is a usable non-empty endpoint reference.
	hasEPR bool
}

// identityDecision is the outcome of reconciling one incoming device against
// the current catalog. Exactly one of upsert or conflict is meaningful.
type identityDecision struct {
	// upsert, when non-nil, is the fully-formed device metadata to persist. It
	// carries an existing ID when a stored record is being updated, or an empty
	// ID when a new record should be created (the repository then generates a
	// crypto-random ID).
	upsert *store.Device
	// isNew reports whether the upsert creates a new catalog record.
	isNew bool
	// conflict, when non-nil, is a deterministic per-item conflict that must be
	// reported without corrupting the catalog.
	conflict *ScanConflict
}

// prepareIdentity computes the canonical endpoint and normalized EPR of a
// discovered device. It is pure and safe to run concurrently.
func prepareIdentity(discovered DiscoveredDevice) preparedIdentity {
	endpoint := firstCanonicalEndpoint(discovered.Addresses)
	epr, hasEPR := normalizeEPR(discovered.EndpointReference)
	return preparedIdentity{endpoint: endpoint, epr: epr, hasEPR: hasEPR}
}

// normalizeEPR validates and normalizes an endpoint reference safely. It mirrors
// the store's EPR acceptance rules (absolute URL, no userinfo, no control
// characters) and returns the trimmed value plus whether it is a usable
// non-empty EPR. An EPR that fails validation is treated as absent so it never
// becomes a dedupe key or is persisted.
func normalizeEPR(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	if containsControl(trimmed) {
		return "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || !parsed.IsAbs() || parsed.User != nil {
		return "", false
	}
	return trimmed, true
}

// reconcileDiscovered decides how one discovered device maps onto the catalog
// given its precomputed identity.
//
// Identity rules, in order:
//  1. A device with no safe canonical endpoint is an "invalid" conflict.
//  2. If the device has a non-empty, valid EPR that matches a stored device's
//     EPR, that stored record is updated and KEEPS its ID even if the endpoint
//     changed — unless the new endpoint is already owned by a different stored
//     device, which is an "endpoint_owned" conflict (never collide two
//     records).
//  3. Otherwise, if the canonical endpoint exactly matches a stored device's
//     endpoint, that record is the dedupe target — unless adopting the
//     discovered EPR would overwrite a different non-empty EPR already on that
//     record, which is an "epr_mismatch" conflict (never merge two different
//     non-empty EPRs).
//  4. Otherwise it is a new device with an empty ID (repository generates one).
//
// The function never mutates the snapshot and never erases an existing EPR or
// credential reference: an update preserves the stored EPR when the discovered
// one is empty and always preserves the stored credential reference and policy.
func reconcileDiscovered(snapshot store.Snapshot, identity preparedIdentity) identityDecision {
	if identity.endpoint == "" {
		return identityDecision{conflict: &ScanConflict{
			EndpointReference: identity.epr,
			Reason:            ScanConflictInvalid,
			Detail:            "the discovered device has no usable endpoint",
		}}
	}

	// Rule 2: prefer an EPR match; it retains the stored ID across endpoint
	// changes.
	if identity.hasEPR {
		if match, ok := deviceByEPR(snapshot, identity.epr); ok {
			if owner, owned := deviceByEndpoint(snapshot, identity.endpoint); owned && owner.ID != match.ID {
				return identityDecision{conflict: &ScanConflict{
					EndpointReference: identity.epr,
					Endpoint:          identity.endpoint,
					Reason:            ScanConflictEndpointOwned,
					Detail:            "the discovered endpoint already belongs to another device",
				}}
			}
			updated := match
			updated.Endpoint = identity.endpoint
			updated.EPR = identity.epr
			return identityDecision{upsert: &updated}
		}
	}

	// Rule 3: fall back to an exact canonical endpoint match.
	if match, ok := deviceByEndpoint(snapshot, identity.endpoint); ok {
		storedEPR, storedHasEPR := normalizeEPR(match.EPR)
		if identity.hasEPR && storedHasEPR && storedEPR != identity.epr {
			return identityDecision{conflict: &ScanConflict{
				EndpointReference: identity.epr,
				Endpoint:          identity.endpoint,
				Reason:            ScanConflictEPRMismatch,
				Detail:            "the stored device for this endpoint has a different identity",
			}}
		}
		updated := match
		updated.Endpoint = identity.endpoint
		if identity.hasEPR {
			updated.EPR = identity.epr
		}
		return identityDecision{upsert: &updated}
	}

	// Rule 4: a brand-new device. The repository assigns the crypto-random ID.
	created := store.Device{
		Endpoint:        identity.endpoint,
		DiscoverySource: store.DiscoverySourceWSDiscovery,
	}
	if identity.hasEPR {
		created.EPR = identity.epr
	}
	return identityDecision{upsert: &created, isNew: true}
}

// firstCanonicalEndpoint returns the deterministic canonical endpoint derived
// from the discovered addresses: the lexicographically smallest canonical form
// among all addresses that canonicalize safely.
func firstCanonicalEndpoint(addresses []string) string {
	best := ""
	for _, address := range addresses {
		canonical := canonicalEndpointOf(address)
		if canonical == "" {
			continue
		}
		if best == "" || canonical < best {
			best = canonical
		}
	}
	return best
}

// deviceByEPR returns a stored device whose normalized EPR equals epr.
func deviceByEPR(snapshot store.Snapshot, epr string) (store.Device, bool) {
	for _, device := range snapshot.Devices {
		stored, ok := normalizeEPR(device.EPR)
		if ok && stored == epr {
			return device, true
		}
	}
	return store.Device{}, false
}

// deviceByEndpoint returns a stored device whose canonical endpoint equals the
// given canonical endpoint.
func deviceByEndpoint(snapshot store.Snapshot, endpoint string) (store.Device, bool) {
	for _, device := range snapshot.Devices {
		if device.Endpoint == endpoint {
			return device, true
		}
	}
	return store.Device{}, false
}
