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
- Separate host-command downgrade flags: `--allow-basic` permits Basic only
  over HTTPS, `--allow-insecure-basic` independently permits plaintext HTTP and
  requires `--allow-basic`, and `--allow-legacy-digest` permits RFC 2069. Each
  enabled downgrade requires credentials and emits one fixed, credential-free
  warning on stderr without polluting JSON stdout. The same flags, policies,
  warning order, timeout behavior, and environment-only password input are used
  by `info`, `profiles`, and `snapshot`.
- Typed Media1 and Media2 read clients and one shared authenticated application
  session for Device, Media1, Media2, and snapshot GET operations. Normalized
  profile DTOs retain every profile in token order, report Media1/Media2
  provenance and narrow fallback per operation, and expose codec, resolution,
  frame rate, bitrate, GOP, audio, and presentation-safe RTSP/snapshot URIs.
- `ovm profiles <host>` with deterministic complete human output and a stable
  `schema_version: 1`, `kind: profiles` JSON envelope. Media2 is preferred per
  operation; only namespace-resolved `ActionNotSupported` can fall back to
  Media1.
- `ovm snapshot <host> [--profile TOKEN] -o FILE` with deterministic default
  selection, pre-network validation of explicit 1–64-code-point profile tokens,
  credential-free metadata output, a stable `kind: snapshot` JSON envelope, a
  20 MiB default encoded-size limit, bounded JPEG dimensions and total pixels,
  complete structural JPEG decoding before publication, and explicit `--force`
  overwrite opt-in. XML-special token characters are preserved for safe wire
  escaping, and JPEG bytes are never written to stdout.
- Expanded fake-device Media1/Media2 fixtures, profile/configuration association
  validation, authenticated synthetic JPEG serving, fault controls, and
  credential-free request history. Command, golden, application, and real
  subprocess tests cover full Media2, Media1-only, partial fallback, auth
  policy, secret/query non-echo, cancellation, JSON stability, and output
  atomicity.
- Versioned desktop metadata store (`internal/store`) with strict
  `schema_version: 1` loading, atomic `0600` writes under a `0700` directory,
  parent-directory sync, typed errors, and failure-injection tests for every
  write stage.
- Platform secret providers backed by the macOS Keychain and Linux Secret
  Service through `github.com/zalando/go-keyring` v0.2.8, addressed by random
  opaque references, plus an explicit compensating coordinator for the
  metadata/secret boundary. Unsupported targets return a typed error and never
  persist plaintext.
- Host-interface enumeration in `pkg/onvif/discovery` with a closed set of
  usability reasons, exposed through `internal/app` for interface selection.
- `app.Manager`, the desktop device facade: scan reconciliation, manual add,
  credential proof through inventory before storage, selection, preferences,
  forget with secret cleanup, profile loading, and stream URI re-resolution,
  with one active scan, one active operation per device, and memory-only
  runtime status.
- Wails desktop composition with typed result/error binding envelopes, an
  isolated `--demo` mode (in-memory store and secrets, loopback fake camera),
  and allowlisted, shell-free VLC launch from identifiers only.
- Desktop UI: device sidebar with scan/cancel, interface selection, search,
  status, manual add, authentication and forget dialogs; Overview and Streams
  tabs with credential-free URLs and copy actions; system/light/dark themes;
  English and pt-BR with enforced key parity; and Local Network diagnostics.

- `pkg/stream`: RTSP play sessions for H.264 and H.265 over TCP or UDP with
  typed, credential-free errors; fragmented MP4 muxing with decode-timestamp
  extraction; a remux pump that republishes initialization on parameter-set
  changes; frame-rate, encoded-bitrate, GOP, time-to-first-frame, loss, and
  reconnect measurements; keyframe-aligned automatic reconnection with
  continuous timestamps; and atomic, size-bounded clip recording.
- `internal/preview`: a loopback-only preview bridge with a random bearer
  token, Host and Origin checks, bounded per-viewer queues, keyframe replay for
  late viewers, and authenticated JPEG snapshot sessions.
- Desktop live preview through Media Source Extensions with live statistics,
  automatic JPEG snapshot fallback (1–5 fps) when the WebView cannot decode the
  codec, clip recording to `~/Movies/ovm` (macOS) or `~/Videos/ovm`, a
  four-position live grid with full screen, and a 30-second stream probe.
- `ovm probe <host>` measures a profile's stream for `--duration` and emits a
  stable `kind: probe` JSON envelope, keeping partial measurements on failure.
- `internal/synthmedia`: a lossless, dependency-free synthetic H.264 encoder, a
  project-owned H.265 fixture, real-time pacing, and an authenticated loopback
  RTSP server (TCP and optional UDP) used by tests and the demo, whose main
  profile now streams H.265 and other profiles H.264.

### Changed

- Presentation RTSP URIs (CLI `profiles`, desktop Streams, copy actions, and VLC
  launch) now keep the camera's query exactly as returned, because cameras such
  as `?channel=1&subtype=0` endpoints select the channel or substream with it.
  Userinfo and fragments are still rejected, an encoded control character in
  the query fails closed, and snapshot URIs remain query-free.
- Snapshot retrieval now selects the lexicographically smallest non-empty
  profile token in the same authenticated session when no token is supplied.
  Destination validation happens before network I/O; publication is atomic and
  no-clobber by default, with replacement allowed only by an explicit overwrite
  request (`--force` in the CLI).
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
  attempt) before one fresh-token retry. The sampled offset is race-protected;
  context lifecycle is checked before callback/header/resample/retry boundaries,
  and cancellation or deadline completion prevents the retry callback even when
  the same error also matches `NotAuthorized`. A failed resample falls back to
  the prior (or zero) offset only while the parent remains active. Deterministic
  callback and transport-count tests prove one shared resample during active
  concurrent retries and zero retry callbacks after lifecycle completion.
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

- CLI flag-parse failures now discard parser diagnostics that can embed malformed
  argument values and return fixed command-scoped usage text instead. Raw
  command-aware `--json` intent still emits the correct typed failure envelope
  without reflecting credential- or URI-shaped values to stderr.
- Camera-originated media failures, including absent advertised Media services
  and unsafe snapshot URIs, are categorized as protocol failures rather than
  caller usage errors; their fixed messages retain no device-supplied value.
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
- Media operational endpoints are validated same-origin before credentials or
  WS-Security are applied; duplicate or unsafe service advertisements fail
  closed. Presentation URIs reject userinfo, fragments, controls, and
  cross-origin hosts, then strip queries. Snapshot redirects are checked before
  target contact, operational queries never cross into DTOs/errors, and the
  authenticated JPEG writer uses bounded reads, `0600` temporary files, atomic
  no-clobber publication, and explicit overwrite authorization.
- The `pkg/onvif/device` decoder now enforces independent aggregate caps on
  total decoded character data, total attribute material, XML token work, and a
  weighted structural complexity score, in addition to the existing depth,
  element-count, and transport body bounds. Character data uses linear byte-slice
  accumulation, preventing fragmented XML text from causing quadratic copying
  below the caps.

### Notes

- M0 through M4 are complete, validated locally, in remote CI, and (for M3 and
  M4) in the native macOS app in demo mode. No physical camera has been
  verified, and no hardware or conformance claim is made.
