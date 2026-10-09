# Architecture

`ovm` uses one Go application core from both a Cobra CLI and a Wails desktop
backend. Protocol libraries are public and UI-independent.

## Layers

- `cmd/ovm`: CLI composition and process lifecycle.
- `pkg/onvif`: SOAP, authentication, discovery, typed services, and quirks.
- `pkg/stream`: RTSP sessions, media statistics, remuxing, and recording.
- `internal/app`: application use cases and normalized presentation models.
- `internal/store`: versioned non-secret metadata and secret-provider adapters.
- `internal/fakedevice`: stateful ONVIF/RTSP test and demo services.
- `desktop`: Wails host, generated bindings, and Svelte frontend.

Dependencies point inward through explicit interfaces. Neither public library
can import desktop, CLI, or persistence packages.

## Application boundary (`internal/app`)

`internal/app` is the single normalized boundary shared by the Cobra CLI and the
future Wails backend. It owns use-case orchestration and never leaks transport,
XML, or UI types.

- **Use cases** are expressed as narrow interfaces — `DiscoveryUseCase` and
  `InventoryUseCase` — so the CLI and tests inject deterministic seams with no
  multicast or hardware dependency. Production wiring lives in
  `NewProductionDiscovery` and `NewProductionInventory`.
- **DTOs** are explicit, credential-free, and JSON-tagged. Raw XML, SOAP
  transport types, and auth material never cross the boundary.
- **Capability states** are exactly `supported`, `unsupported`, `unknown`, and
  `temporarily_unavailable`. Each of the seven Device operations reports a
  state: `ActionNotSupported` maps to `unsupported`; a successful call to
  `supported`; a transient network/timeout to `temporarily_unavailable`; and an
  authentication, protocol, or decode ambiguity to `unknown`.
- **Partial results** are first class, but a *useful* inventory has a precise
  definition: `GetSystemDateAndTime` is reachable without credentials and alone
  does not qualify. A result is useful (no error) only when at least one
  non-clock operation is `supported`, or when all six non-clock operations are
  definitively `unsupported` (a responsive device that answered every request).
  When no operation is useful, the dominant failure is promoted to a typed error
  and returned alongside the state-only inventory. Authentication is not given
  blanket priority: it is the verdict only when every credential-protected
  (non-clock) operation was authentication-rejected, or when authentication is
  the sole unresolved failure category (an `unsupported` action may coexist
  without retyping it). Otherwise the dominant category follows a fixed
  precedence of `cancelled > protocol > network > authentication > internal`, so
  a single authentication rejection can never mask a competing transport or
  protocol fault — a mixed authentication + deadline + unsupported set resolves
  to `network`. Per-operation states stay accurate throughout. Independent inventory calls run with
  bounded, configurable, context-aware concurrency and are reassembled in a
  deterministic order.
- **Cancellation precedence** is explicit: once the use case has started, an
  explicit `context.Canceled` of the parent context always wins. It returns the
  partial inventory with a `cancelled` error that maps to exit 130, even if some
  operations succeeded or competing failures were observed. Semaphore
  acquisition is context-aware so queued operations do not block a cancelled
  run. A deadline (`context.DeadlineExceeded`) is a timeout handled by the
  network policy, not an explicit cancellation.
- **Errors** are typed with stable categories (`usage`, `network`,
  `authentication`, `protocol`, `cancelled`, `internal`) and support
  `errors.Is`/`errors.As`. Their user-facing text is always credential-free.
- **Authentication** is composed, not duplicated: the inventory adapter wraps
  the SOAP HTTP client in an origin-scoped HTTP Digest transport, performs an
  unauthenticated clock call first (which also lets the transport answer a
  Digest challenge), samples the device clock offset, and attaches a freshly
  generated WS-Security UsernameToken PasswordDigest header for authenticated
  operations. The initial clock negotiation runs under a short child budget (the
  smaller of 2s and 20% of the remaining parent budget) with bounded attempts. A
  SOAP `NotAuthorized` fault triggers exactly one retry with a new token,
  preceded by a single shared, bounded clock resample (the smaller of 750ms and
  10% of the remaining budget); the sampled offset is race-protected, parent
  cancellation is honoured, and a failed resample falls back to the prior (or
  zero) offset without blocking the retry. Network and protocol errors never
  trigger an authentication retry. Credentials are ephemeral, non-serializable,
  and never returned.
- **Authentication downgrades** are opt-in and secure-by-default, enforced
  natively through the auth package's public hooks. `--allow-basic` maps only
  to `AllowBasic` and therefore permits Basic on HTTPS endpoints. The separate
  `--allow-insecure-basic` flag maps to `AllowBasicOverHTTP`, requires
  `--allow-basic`, and is the only path that permits reusable Basic credentials
  over plaintext HTTP. Legacy no-qop Digest is used only with
  `--allow-legacy-digest`, which maps to `AllowRFC2069`; a policy refusal
  surfaces as the typed `ErrAuthPolicy` sentinel. The application-boundary
  RoundTripper that blocks generated Digest headers without `qop` remains a
  defense-in-depth layer, not a substitute for the native policy. Each enabled
  downgrade emits its own fixed, credential-free stderr warning.
- **URL boundary redaction** does not rely solely on `soap.RedactURL`, and
  general URI validation requires a well-formed *absolute* URI with a non-empty,
  syntactically valid scheme (not a bare `url.Parse`). Every URI-bearing field
  crossing into a DTO — discovery EndpointReference, scopes, XAddrs, and expanded
  type namespace URIs, and inventory capabilities XAddrs, service
  XAddr/Namespace, Scope.Item, and the canonical endpoint — is validated and
  redacted at the boundary: only userinfo, query, and fragment are redacted,
  while path segments are preserved verbatim and are NOT claimed to be
  credential-redacted. A relative reference, an invalid or missing scheme, a
  control-character-bearing value, or (for a transport endpoint) anything that is
  not an absolute HTTP(S) URL with a host is replaced wholesale with a safe
  marker. This guarantees redaction of credentials in userinfo, query, and
  fragment components; URI paths and opaque identity content are intentionally
  preserved and are not claimed to be secret-free.

## M1 protocol boundaries

- Typed Device operations validate required WSDL/XSD fields and cardinalities,
  tolerate namespace-prefix variation, preserve only bounded credential-redacted
  extension XML, and keep raw protocol types out of application DTOs.
- The Device decoder enforces independent aggregate caps on total decoded
  character data, total attribute material, XML token work, and a weighted
  structural-complexity score, in addition to the existing depth, element-count,
  and transport body bounds. Character data accumulates linearly in byte slices,
  so fragmented text cannot trigger quadratic copying below those caps.
- Active discovery emits only the ONVIF-required WS-Discovery 2005/04 baseline
  and never mixes generations outbound. Inbound parsing additionally tolerates
  coherent 2009/01 messages (parser tolerance, not a 2009/01 emission claim),
  while rejecting cross-paired discovery/addressing families, duplicate
  `Action`/`RelatesTo`, and non-Reply probe relationships. Discovery requires
  up, multicast-capable, non-point-to-point interfaces; it binds and probes
  every usable IPv4 source address, explicitly selects multicast egress, fixes
  TTL at 1, and performs bounded jittered retransmission with the same logical
  Probe MessageID. Separate probes cover `tds:Device` and legacy
  `dn:NetworkVideoTransmitter`, because combining them would create a
  conjunctive type filter. Passive listeners join once per selected interface;
  `ListenStream` surfaces setup/runtime errors, deduplicates MessageID per
  interface, and suppresses stale Hello/Bye using bounded AppSequence state.
  Cancellation closes all sockets and streams.
- Discovery cancellation preserves partial output: when a round is cancelled
  mid-flight the underlying discoverer returns the devices aggregated so far
  together with `context.Canceled`, and the application normalizes and returns
  those partial devices with a `cancelled` error. The CLI renders them (human
  to stdout, JSON embedded in the versioned failure envelope beside the error
  object) before exiting 130, so an interrupted discovery never silently drops
  what it already found.
- SOAP responses have both byte limits and structural depth/element/attribute/
  namespace-work limits before a tree is allocated.

The CLI maps the typed error categories to a stable exit-code table (see the
README), emits the same versioned JSON envelope for success and failure, and
prints a safe error exactly once. A plain timeout is never reported as a
permission problem.

See [ADR 0001](adr/0001-stack-and-architecture.md) for the initial decision and
[PLAN.md](PLAN.md) for milestone details.
