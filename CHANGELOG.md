# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Initial implementation plan and stack/architecture decision record.
- Project governance, security, and contribution documentation.
- MIT license.
- Cobra CLI foundation with deterministic human and JSON version output.
- Wails v2 desktop shell with Svelte, TypeScript, Tailwind CSS, and a custom app icon.
- Cross-platform GitHub Actions validation for macOS and Ubuntu, including lint,
  tests, race detection, frontend checks, and native desktop builds.
- Bounded SOAP 1.2/1.1 transport and authentication foundations with typed
  faults, structural XML limits, credential-redacted diagnostics, opaque
  UsernameTokens, origin-scoped HTTP Digest, and explicitly enabled Basic.
- Typed, UI-independent ONVIF ver10 Device operations for system time, device
  information, capabilities, services, scopes, hostname, and network
  interfaces, backed by strict WSDL/XSD validation and provenance-tracked
  success/prefix/fault/malformed synthetic coverage.
- Interface-aware WS-Discovery with separate correlated probes for the current
  `tds:Device` and legacy `dn:NetworkVideoTransmitter` types, deterministic
  EndpointReference aggregation, ONVIF-required 2005/04 outbound emission,
  coherent 2005/04 and 2009/01 inbound parser tolerance, manual endpoint
  canonicalization, diagnostic categories, and cancellable passive Hello/Bye
  multicast lifecycle handling through injectable sockets.
- Expanded race-safe fake ONVIF device inventory, authentication, fault,
  namespace-variation, and optional-field behavior for end-to-end tests.
- `internal/app` application boundary shared by the CLI and future Wails
  backend: context-aware discovery and device-inventory use cases behind narrow
  interfaces, credential-free JSON-tagged DTOs, four explicit capability states
  (`supported`, `unsupported`, `unknown`, `temporarily_unavailable`), bounded
  concurrency with deterministic ordering, a precise minimum-useful-inventory
  policy (a successful clock read alone does NOT qualify; a result is useful only
  when at least one non-clock operation is supported, or when every non-clock
  operation is definitively unsupported), promotion of a total failure to a typed
  dominant error by a fixed precedence (cancelled > protocol > network >
  authentication > internal) while an all-`unsupported` device remains a valid
  inventory, and typed user-safe errors with stable categories and
  `errors.Is`/`errors.As` support.
- Production adapters composing WS-Discovery, SOAP, origin-scoped HTTP Digest,
  WS-Security UsernameToken (with sampled clock offset), and the typed Device
  client, including unauthenticated, Digest + UsernameToken, and opt-in Basic
  authentication, all exercised against the in-process fake ONVIF device.
- `ovm discover` and `ovm info` commands with stable, versioned JSON envelopes
  (`schema_version: 1`, distinct `kind`, safe failure objects for runtime and
  validation errors), complete human inventory output by default, discovery
  diagnostics on stderr in both modes (concrete socket and permission-restricted
  rounds exit with the network code while an empty `no_response` round stays a
  success), single-line sanitization of camera- and network-controlled strings
  in human output, environment-variable-only credentials via `--password-env`,
  and a documented, tested exit-code mapping used by `cmd/ovm` that prints a safe
  error exactly once. Interrupt and SIGTERM are handled for clean cancellation.
- Manual, build-tagged real-camera smoke test using `OVM_TEST_HOST`,
  `OVM_TEST_USER`, and `OVM_TEST_PASS`, excluded from the default build and
  public CI.
- Separate `info` downgrade flags: `--allow-basic` permits Basic only over
  HTTPS, `--allow-insecure-basic` independently permits plaintext HTTP and
  requires `--allow-basic`, and `--allow-legacy-digest` permits RFC 2069.
  Each enabled downgrade requires credentials and emits one fixed,
  credential-free warning on stderr without polluting JSON stdout.

### Changed

- Inventory cancellation now has explicit-cancellation precedence: once the use
  case has started, an explicit `context.Canceled` of the parent context always
  wins and returns the partial inventory together with a `cancelled` error that
  maps to exit code 130, even when some operations succeeded or competing
  authentication/protocol/network failures were observed. Semaphore acquisition
  is context-aware so queued operations do not block a cancelled run. A deadline
  (`context.DeadlineExceeded`) remains a timeout handled by the network policy,
  not an explicit cancellation.
- Minimum useful inventory is now defined: `GetSystemDateAndTime` alone does not
  qualify. A result is useful only when at least one non-clock operation is
  supported, or when all six non-clock operations are definitively unsupported.
  Dominant-failure classification no longer gives authentication blanket
  priority: a single authentication rejection can no longer mask a competing
  transport or protocol fault. Authentication is the verdict only when every
  credential-protected (non-clock) operation was authentication-rejected, or
  when authentication is the sole unresolved failure category (an unsupported
  action may coexist without retyping it). Otherwise the dominant category
  follows a fixed precedence of cancelled > protocol > network > authentication >
  internal. In particular a mixed authentication + deadline + unsupported set is
  classified as network, not authentication, and per-operation states stay
  accurate. Explicit parent-context cancellation still wins unconditionally.
- Unauthenticated clock negotiation now runs under a short child budget (the
  smaller of 2s and 20% of the remaining parent budget) with bounded attempts.
  A retry triggered by a SOAP `NotAuthorized` fault performs one shared, bounded
  clock resample (the smaller of 750ms and 10% of the remaining budget, single
  attempt) before one fresh-token retry. The sampled offset is race-protected,
  parent cancellation is honoured, and a failed resample falls back to the prior
  (or zero) offset without blocking the retry. The retry path is now covered by
  a deterministic test that drives an actual SOAP `NotAuthorized` failure
  through the seam and proves exactly one shared, bounded-deadline resample for
  the whole use case, exactly one retry per operation, and that parent
  cancellation arriving during the resample wins so no retry succeeds.
- Discovery no longer discards devices found before a mid-round cancellation.
  When the underlying discoverer returns an aggregated-so-far result together
  with `context.Canceled`, the application normalizes and returns the partial
  devices paired with a `cancelled` error, and the CLI renders those partial
  devices (human mode to stdout, JSON mode embedded in the versioned failure
  envelope alongside the error object) before exiting with code 130.
- Exit-code mapping is hardened: after the explicit usage-marker precedence, a
  wrapped or joined `context.Canceled` maps to 130 before the typed-category
  switch, so an interrupt cannot be masked by a competing, incorrectly
  categorized application error. A bare `context.DeadlineExceeded` is not
  promoted and remains a network/timeout condition.
- Root unknown-command errors no longer echo the raw first token, so a mistyped
  command or a credential-bearing argument cannot be reflected into an error
  string.
- Active discovery now requires multicast-capable, non-point-to-point
  interfaces and probes every usable IPv4 source address through an exact-source
  socket. Production explicitly selects multicast egress, sets TTL 1, and uses
  bounded jittered retransmission that reuses each logical Probe MessageID and
  byte sequence. The parser preserves and validates `RelatesTo`
  `RelationshipType`, rejects duplicate addressing headers and incoherent
  family pairings, and keeps 2009/01 support limited to coherent inbound parser
  tolerance. Passive `ListenStream` surfaces terminal errors, deduplicates
  MessageID per interface, and suppresses stale Hello/Bye with bounded
  `(InstanceId, SequenceId, MessageNumber)` state per endpoint/interface.

### Security

- Authentication downgrade policy is enforced by independent native hooks.
  `AllowBasic` permits challenge-driven Basic only on HTTPS; the separate
  `AllowBasicOverHTTP` gate is zero-value-off and can be enabled only through
  `--allow-insecure-basic` together with `--allow-basic`. `AllowRFC2069`
  independently gates legacy no-qop Digest. Policy refusal surfaces as
  `ErrAuthPolicy`. A second application-boundary RoundTripper blocks any
  generated Digest header without `qop` unless legacy mode is enabled. Digest
  nonce-count history is retained across stale A→B→A rotations in a bounded
  map, preventing a previously used nonce from restarting at `nc=1`.
- App-local URI boundary validation and redaction no longer relies solely on
  `soap.RedactURL`, and general URI validation now requires a well-formed
  *absolute* URI with a non-empty, syntactically valid scheme (not merely a bare
  `url.Parse`). Discovery EndpointReference, scopes, XAddrs, and expanded type
  namespace URIs, and inventory capabilities XAddrs, service XAddr and Namespace,
  Scope.Item, and the canonical endpoint are each validated and redacted at the
  boundary: only userinfo, query, and fragment are redacted (path segments are
  preserved verbatim and are NOT claimed to be credential-redacted). A relative
  reference, a value with an invalid or missing scheme, a value carrying control
  characters, or (for transport endpoints) anything that is not an absolute
  HTTP(S) URL with a host is replaced wholesale with a safe marker. Credentials
  in userinfo, query, and fragment components cannot cross into a DTO or output;
  URI paths and opaque identity content remain visible and are not claimed to be
  secret-free.
- The `pkg/onvif/device` decoder now enforces independent aggregate caps on
  total decoded character data, total attribute material, XML token work, and a
  weighted structural complexity score, in addition to the existing depth,
  element-count, and transport body bounds. Character data uses linear byte-slice
  accumulation, preventing fragmented XML text from causing quadratic copying
  below the caps.

### Notes

- M1 remains in progress. Evidence is synthetic-fixture only; no camera hardware
  has been verified and the remote CI gate for M1 has not been declared passed.
