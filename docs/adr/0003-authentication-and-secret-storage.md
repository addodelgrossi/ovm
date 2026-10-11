# ADR 0003: Authentication and secret storage

- Status: Accepted
- Date: 2026-10-08
- Decision owners: ovm maintainers

## Context

ONVIF devices commonly combine WS-Security UsernameToken, HTTP Digest, and in
some cases HTTP Basic. Device clocks can be inaccurate, Digest implementations
can be limited to legacy algorithms, and authentication retries risk leaking
credentials through logs, redirects, URLs, error values, or persistent
configuration.

ADR 0001 assigned protocol behavior to public `pkg/onvif` packages and future
native secret-provider adapters to `internal/store`. The boundary and downgrade
policy need to be explicit before either protocol authentication or persistent
secret storage is expanded.

## Decision

### Authentication order and per-device state

Authentication is negotiated per device and per origin in this order:

1. request system time without credentials when the device permits it;
2. use WS-Security UsernameToken `PasswordDigest` with an offset-adjusted
   timestamp;
3. answer an HTTP Digest challenge; and
4. answer HTTP Basic only when the caller's explicit policy enables it.

The protocol foundation provides a credential-free in-memory strategy record
for a future long-lived application session. It can store the selected mode,
measured clock offset and uncertainty, the Basic and plaintext-transport policy
flags, the legacy no-`qop` Digest compatibility flag, and update time under a
stable device key; it never stores a username, password, nonce, digest response,
or authorization header. The M1 CLI inventory adapter intentionally constructs
a fresh strategy for each invocation and does not yet wire this cache, because
there is no persistent device/session owner until a later desktop milestone.
Persisting strategy hints in the future does not make them a secret source and
does not permit persisting credentials alongside them.

Authentication failure, network failure, and SOAP-version incompatibility are
separate signals. None is used to guess credentials or silently downgrade to
Basic.

### UsernameToken PasswordDigest

Each generated token uses a fresh cryptographically random raw nonce. Its
timestamp is the local time plus the measured camera offset, converted to UTC,
truncated to a whole second, and formatted as RFC3339. The fake-device validator
accepts standard-parser RFC3339 forms with numeric offsets or fractional seconds
within the freshness window by default and hashes the exact element text; an
explicit strict interoperability-test mode accepts only canonical whole-second
UTC `...SSZ`. The digest is exactly:

```text
Base64(SHA1(rawNonce || UTF8(Created) || UTF8(password)))
```

The transmitted nonce is `Base64(rawNonce)`. Hashing its Base64 text instead of
the raw bytes is incorrect. The implementation retains the generated username,
Base64 digest, Base64 nonce, and timestamp only long enough to construct the
outbound header; it does not retain the plaintext password in the token value.
Deterministic tests inject time and entropy and include an independently
verified fixed vector. Runtime generation uses `crypto/rand`.

SHA-1 is used only for the standards-mandated PasswordDigest construction. It
is not selected for signatures, password storage, or general hashing.

### Camera clock offset

A clock observation records local time immediately before and after the camera
time request. ONVIF's serialized integer second is treated as the start of its
represented one-second interval, so the point estimate adds 500 milliseconds
before comparing it with the request midpoint. Several samples are taken and
the successful sample with the minimum round-trip time is retained. Reported
uncertainty is half that round-trip time plus 500 milliseconds for the remaining
whole-second quantization interval.

Every query receives the caller's `context.Context`. A shorter caller deadline
wins over transport defaults. Subsecond clock claims are not derived from this
coarse method.

### HTTP Digest

The HTTP authentication transport is bound to one configured HTTP(S) origin
and never authenticates a request outside it. Digest challenges are cached by
realm and canonical protection-space set rather than in a single origin-wide
slot. An RFC `domain` directive is parsed as URI prefixes; every member must be
a valid same-origin HTTP(S) URI or absolute-path reference. A present malformed
or cross-origin member rejects the challenge instead of widening its scope. If
`domain` is omitted, the RFC origin-wide default applies. The most specific
matching protection space is selected for a request URL; equal-specificity
ambiguity sends no preemptive credential and waits for a challenge. Digest and
Basic records for independent realms can coexist. Out-of-origin authorization
and proxy authorization headers are removed.

The default supported Digest subset is RFC 7616 `qop=auth` with nonce count and
a fresh cryptographic client nonce. SHA-256 is preferred when advertised. MD5
is supported for legacy camera interoperability; the corresponding `-sess`
variants are also accepted. `auth-int` and unknown algorithms fail explicitly
rather than changing semantics. Legacy RFC 2069 challenges without `qop` are
disabled by default and return a typed, non-secret policy error. They are used
only when the caller explicitly sets the separate `AllowRFC2069` compatibility
policy; a modern `qop=auth` challenge always outranks a no-`qop` alternative.

A request is retried only when its body is absent or can be recreated through
`GetBody`. Each rejected response body is drained only to a small bound and
closed before retry. A `stale=true` challenge replaces only its matching
realm/protection-space nonce. Retry count is bounded. Concurrent requests
receive distinct, monotonically increasing counts for each active
realm/protection-space/nonce tuple.

HTTP Basic remains challenge-driven and is unavailable unless `AllowBasic` is
set for that device. That flag alone permits Basic only over HTTPS. Sending
Basic over plaintext HTTP requires the distinct zero-value-off
`AllowBasicOverHTTP` policy; otherwise the transport returns a typed policy
error without sending credentials. A Basic credential is cached only for the
challenged path-derived protection space within the exact origin. These are
policy decisions by a higher layer, not automatic fallback inside a service
operation.

### Redirect and diagnostic policy

SOAP redirects to a different scheme, host, or effective port are blocked,
even if the standard HTTP client might otherwise remove selected headers. This
also prevents a WS-Security token embedded in a replayable request body from
crossing origins. URI userinfo is rejected for protocol endpoints.

Structured SOAP traces contain only method, redacted action, SOAP version,
duration, HTTP status, and outcome. They never contain request bodies or
request headers. Diagnostic redaction removes, case-insensitively and even from
partial XML:

- `Username`, `Password`, `PasswordDigest`, and `Nonce` token values;
- URI userinfo; and
- `Authorization` and `Proxy-Authorization` header values.

Fault and malformed-response XML is redacted before diagnostic truncation and
bounded before being exposed. The lexical redactor fails closed on incomplete
sensitive tags, attributes, quotes, end tags, and UTF-8 boundaries. Parser
errors exposed to callers use fixed categories rather than attacker-controlled
parser prose. Authentication errors describe the refused policy or unsupported
capability without embedding challenge nonces or credentials.

### Secret-storage boundary

`pkg/onvif/auth` accepts ephemeral credential values from its caller. It does
not import keyring, persistence, Cobra, Wails, desktop, or application packages;
read environment variables; prompt a user; or serialize credentials.

The M3 persistence slice implements the provider boundary under
`internal/store`. Metadata uses deterministic JSON with `schema_version: 1` at
`os.UserConfigDir()/ovm/state.json`. The document contains only stable device
IDs, canonical query-free Device endpoints, optional EPR/display/discovery
metadata, opaque credential references, the three explicit authentication
policy flags, controlled UTC timestamps, and durable preferences. Runtime
status, operational URI query/userinfo, demo state, usernames, and passwords
are outside the schema. Loading is strict: malformed data, duplicate records,
unknown fields, invalid references or enums, broken timestamp invariants, and
unsupported or future versions are returned as typed errors and are never
silently replaced. Migrations go through an explicit version table, including
while v1 has no predecessor.

Writes create the `ovm` directory with mode `0700` and use a `0600`
same-directory temporary file. The implementation encodes, syncs, closes,
renames atomically, and syncs the parent directory. A repository mutex
serializes updates, deterministic device ordering is by stable ID, and every
read API returns a copy.

`SecretProvider` carries a redacted, non-serializable `Secret` containing the
username, password, and matching authentication policy. The process-local
memory implementation is available only for tests and explicit demo
composition; it is never an automatic fallback. Production macOS and Linux
adapters use `github.com/zalando/go-keyring` v0.2.8 with service
`io.github.addodelgrossi.ovm`. Each keyring value is its own strict,
separately-versioned JSON payload and is addressed by a cryptographically
random opaque account reference. Provider errors expose only stable
not-found, unavailable, denied, invalid, and unsupported categories. Android,
iOS, Windows, and other targets without an approved adapter return the typed
unsupported error and never persist plaintext.

JSON metadata and an OS secret store cannot share one atomic commit. The
coordinator therefore uses explicit compensation without holding the
repository lock over provider calls: a new secret is stored before its metadata
is published and is deleted if metadata publication fails; credential rotation
restores prior metadata when old-secret cleanup fails and no concurrent update
has intervened. Forget removes metadata first, treats an already-missing secret
as idempotent success, and restores metadata plus selection when secret deletion
is denied or unavailable. A concurrent metadata change prevents rollback and
returns a typed conflict/rollback failure rather than overwriting newer state.
Application code remains responsible for validating camera authentication
before storage. `app.Manager` performs that validation and owns the coordinator
for the desktop; the CLI continues to take credentials only from the
environment.

### Normative sources

- [OASIS Web Services Security UsernameToken Profile 1.1.1](https://docs.oasis-open.org/wss-m/wss/v1.1.1/os/wss-UsernameTokenProfile-v1.1.1-os.html)
- [RFC 7616: HTTP Digest Access Authentication](https://www.rfc-editor.org/rfc/rfc7616)
- [RFC 7617: The Basic HTTP Authentication Scheme](https://www.rfc-editor.org/rfc/rfc7617)
- [ONVIF Core Specification](https://www.onvif.org/specs/core/ONVIF-Core-Specification.pdf)

## Consequences

- Modern Digest and UsernameToken devices work without a third-party auth
  dependency; legacy MD5 remains isolated and documented.
- Authentication retries are bounded, replay-aware, context-aware, and scoped
  to one origin.
- Basic and legacy no-`qop` Digest cannot be selected accidentally; callers
  must expose explicit compatibility policy controls, and plaintext Basic has a
  separate insecure-transport opt-in.
- Clock offset has an honest uncertainty and cannot be presented as a
  subsecond synchronization measurement.
- Native secret storage is isolated behind a tested provider boundary;
  unsupported targets fail explicitly without a plaintext or in-memory fallback.

## Alternatives considered

### Store passwords in the device database

Rejected because database permissions and encryption-at-rest behavior vary and
do not replace platform secret stores.

### Put credentials in endpoint URLs

Rejected because URI userinfo leaks through logs, errors, process inspection,
and redirect handling.

### Enable Basic after any authentication failure

Rejected because it is an unsafe downgrade and can send reusable credentials
over plaintext HTTP without informed policy.

### Use the first camera clock observation

Rejected because network delay biases the estimate. Minimum-round-trip midpoint
sampling provides a better coarse offset and a reportable uncertainty.

### Add a general authentication dependency

Rejected for this bounded subset. The standard library provides the required
cryptography, HTTP hooks, parsing, and randomness with a smaller audit and
license surface.
