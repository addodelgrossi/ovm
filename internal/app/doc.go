// Package app is the normalized application boundary shared by the Cobra CLI
// and the future Wails desktop backend.
//
// It owns use-case orchestration for discovery and device inventory. It
// consumes the public pkg/onvif libraries (discovery, soap, auth, device) and
// normalizes their results into explicit, credential-free, JSON-tagged DTOs. It
// never exposes raw XML, transport types, or UI types, and it never returns or
// serializes credentials.
//
// The package depends on narrow interfaces (DiscoveryUseCase and
// InventoryUseCase) so the CLI and tests can inject deterministic seams without
// a network or multicast dependency. Production wiring is provided by the
// adapters in this package (NewProductionDiscovery and NewProductionInventory).
//
// Capability states are exactly supported, unsupported, unknown, and
// temporarily_unavailable. Errors are typed with stable categories and support
// errors.Is/errors.As unwrapping. No error text produced here contains
// plaintext passwords, WS-Security UsernameToken material, HTTP Authorization
// values, or credential-bearing URLs.
package app
