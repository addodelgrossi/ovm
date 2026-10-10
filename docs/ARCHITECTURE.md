# Architecture

`ovm` uses one Go application core from both a Cobra CLI and a Wails desktop
backend. Protocol libraries are public and UI-independent.

## Layers

- `cmd/ovm`: CLI composition and process lifecycle.
- `pkg/onvif`: SOAP, authentication, discovery, typed services, and quirks.
- `pkg/stream`: RTSP sessions, media statistics, remuxing, reconnection, and recording.
- `internal/app`: application use cases and normalized presentation models.
- `internal/store`: versioned non-secret metadata and secret-provider adapters.
- `internal/fakedevice`: stateful Device/Media SOAP and authenticated JPEG test services.
- `internal/demo`: isolated in-memory desktop demo environment built on the fake device.
- `internal/synthmedia`: project-owned synthetic H.264/H.265 media and a loopback RTSP server.
- `internal/preview`: token-protected loopback bridge serving previews to the WebView.
- `desktop`: Wails host, generated bindings, and Svelte frontend.

Dependencies point inward through explicit interfaces. Neither public library
can import desktop, CLI, or persistence packages.

## Application boundary (`internal/app`)

`internal/app` is the single normalized boundary shared by the Cobra CLI and the
Wails backend. It owns use-case orchestration and never leaks transport,
XML, or UI types.

- **Use cases** are expressed as narrow interfaces — `DiscoveryUseCase`,
  `InventoryUseCase`, `MediaProfilesUseCase`, and `SnapshotUseCase` — so the CLI
  and tests inject deterministic seams with no multicast or hardware dependency.
  Production wiring lives in `NewProductionDiscovery`, `NewProductionInventory`,
  `NewProductionMediaProfiles`, and `NewProductionSnapshot`.
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
- **Authentication** is composed, not duplicated: each host-scoped use case
  owns one authenticated camera session and one origin-scoped HTTP
  Digest/Basic transport. Device, Media1, Media2, and snapshot GET requests in
  that use case share its challenge cache, sampled clock offset, fresh
  WS-Security UsernameTokens, and retry gate. The session performs an
  unauthenticated clock call first (which also lets the transport answer a
  Digest challenge), samples the device clock offset, and attaches a freshly
  generated UsernameToken PasswordDigest header for authenticated SOAP
  operations. Snapshot HTTP GETs reuse the transport but carry no WS-Security
  header. The initial clock negotiation runs under a short child budget (the
  smaller of 2s and 20% of the remaining parent budget) with bounded attempts. A
  SOAP `NotAuthorized` fault triggers exactly one retry with a new token,
  preceded by a single shared, bounded clock resample (the smaller of 750ms and
  10% of the remaining budget); the sampled offset is race-protected. Context
  lifecycle is checked before the first call, before header generation,
  resampling, and retry, and after each callback. Cancellation or deadline
  completion, including an error that also matches `NotAuthorized`, therefore
  prevents the retry callback. A failed resample falls back to the prior (or
  zero) offset only while the parent remains active. Network and protocol errors
  never trigger an authentication retry. Credentials are ephemeral,
  non-serializable, and never returned.
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

## M4 video boundaries

- `pkg/stream` is UI-independent. `VideoSource` yields H.264 or H.265 access
  units with presentation timestamps; `OpenRTSP` is the production source and
  `OpenReconnecting` wraps any opener, skipping to the next keyframe after a
  reconnect and shifting timestamps so the output stays monotonic.
  `PumpFMP4` drives an `FMP4Muxer` into a `FragmentSink` and republishes the
  initialization segment when in-band parameter sets change. `Meter` measures
  encoded payload, not transport overhead, using presentation timestamps.
- RTSP credentials travel only in the RTSP URL handed to gortsplib inside the
  backend. Library errors, which can embed URLs, are mapped to typed sentinels
  with fixed messages.
- `app.Manager` opens live streams and snapshot feeds by re-resolving the
  selected profile with stored credentials; the URI and credentials never
  reach the frontend. `ProbeUseCase` composes profile resolution and the RTSP
  client for both `ovm probe` and the desktop.
- `internal/preview` binds `127.0.0.1` on an ephemeral port, requires a
  256-bit bearer token compared in constant time, rejects foreign Host headers
  and Origins, answers CORS only for `wails://wails`, bounds every viewer's
  queue, and disconnects slow viewers without blocking the producer. A late
  viewer receives the initialization segment and the current group of
  pictures. Snapshot sessions fetch one validated JPEG per request, serialized
  per session.
- The frontend plays the byte stream through Media Source Extensions
  (`ManagedMediaSource` when present), chases the live edge, trims old buffer,
  and falls back to authenticated JPEG polling when MSE or the codec is
  unavailable. Clips are written by the backend to a `0600` temporary file and
  published with an atomic no-clobber link; they start at a keyframe and stop
  on a codec change or the 2 GiB limit.

## M3 desktop boundaries

- `app.Manager` is the device facade consumed by the Wails host. It is composed
  from injected collaborators only (repository, host interfaces, discovery,
  inventory, and media profiles) and creates no disk or keyring resources
  itself. At most one scan and one network operation per device are active; a
  newer request or a Forget cancels the prior one, and the manager never holds
  its mutex across repository, provider, or network calls. Runtime status lives
  only in manager memory and is never persisted.
- `internal/store` persists versioned, strictly validated, non-secret metadata
  (`schema_version: 1`) with atomic `0600` writes under a `0700` directory, and
  keeps passwords behind `SecretProvider`. Production uses the macOS Keychain or
  Linux Secret Service through go-keyring; unsupported targets fail explicitly.
  The coordinator compensates across the metadata/secret boundary because the
  two stores cannot commit atomically (see
  [ADR 0003](adr/0003-authentication-and-secret-storage.md)).
- Host interfaces are enumerated by `pkg/onvif/discovery` with a closed set of
  usability reasons, so the UI can explain why an interface cannot be scanned
  without exposing raw operating-system errors.
- The desktop composition root (`desktop/composition.go`) wires production
  adapters, or with `--demo` an isolated `internal/demo` environment: an
  in-memory repository and secret provider plus a fake camera on IPv4 loopback.
  Demo mode never touches the real configuration file, keyring, or multicast
  networks, and reports RTSP playback and VLC as unavailable.
- Wails bindings return result/error envelopes with a stable `BindingError`
  (`category`, `code`, safe English `message`) and bound every call with the
  host's root context. Passwords are accepted only in a request and never
  returned or logged. `OpenInVLC` accepts identifiers only; the manager
  re-resolves a credential-free RTSP URI and the host launches an
  allowlisted VLC executable without a shell.
- The Svelte frontend calls the backend only through `lib/api.ts`. Copy actions
  use the native Wails clipboard first and the web clipboard as a fallback.
  Locale (English, pt-BR) and theme (system, light, dark) are persisted
  preferences, and pt-BR key parity is enforced by types and tests.

## M2 Media and snapshot boundaries

- The Media1 and Media2 wire packages expose only the supported read subset.
  The application prefers Media2 independently per operation and falls back to
  Media1 only for a namespace-resolved `ActionNotSupported`; authentication,
  cancellation, deadline, transport, HTTP, decode, malformed-response, and
  security failures never trigger a fallback request.
- Profile DTOs retain every profile, sort by token, and emit operation statuses
  in a fixed order. Each status carries capability state, Media1/Media2 source,
  and a `fell_back` marker. Encoder summaries carry configured codec,
  resolution, frame rate, bitrate, GOP, codec profile, and audio values.
- Advertised Media endpoints are validated against the camera origin before any
  credential or UsernameToken is applied. Duplicate or unsafe service
  advertisements fail closed. Operational endpoint and snapshot queries remain
  private to the session. Presentation snapshot URIs are query-free.
  Presentation RTSP URIs keep the camera's query, which commonly selects the
  channel or substream; they never carry userinfo or a fragment, and a query
  with an encoded control character is rejected.
- Snapshot selection uses an explicit token or the lexicographically smallest
  non-empty token returned by the preferred service. Explicit tokens are
  validated before endpoint preparation or network I/O: valid UTF-8, 1–64
  Unicode code points, no C0/C1 controls or replacement rune, and not
  whitespace-only. XML-special characters remain valid for wire escaping. The
  JPEG GET reuses the authenticated transport, rejects cross-origin/downgrade
  redirects, non-JPEG responses, truncation, and bodies over 20 MiB by default.
  Before publication, JPEG dimensions are read without a full allocation and
  limited to 16384 per axis and 67,108,864 total pixels; the bounded body must
  then decode completely with no trailing data. The use case returns metadata
  rather than bytes.
- Snapshot files stream to a same-directory `0600` temporary file, are synced,
  closed, and then published atomically. The default uses a no-clobber hard-link
  publication; explicit overwrite uses atomic rename. Failure and cancellation
  remove temporary files and preserve an existing destination.

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

The CLI maps typed error categories to a stable exit-code table (see the
README). `discover`, `info`, `profiles`, and `snapshot` emit typed
`schema_version: 1` envelopes for success and failure, including validation
failures detected after normal flag parsing stops by consulting command-scoped
raw-argument JSON intent. Human profile output is deterministic and
single-line-sanitized. Snapshot stdout contains metadata only; JPEG bytes are
written exclusively through the application file boundary. Process errors are
printed safely and once. A plain timeout is never reported as a permission
problem.

See [ADR 0001](adr/0001-stack-and-architecture.md) for the initial decision and
[PLAN.md](PLAN.md) for milestone details.
