# ADR 0001: Stack and architecture

- Status: Accepted
- Date: 2026-10-08
- Decision owners: ovm maintainers

## Context

The classic ONVIF Device Manager is effectively a Windows-only tool. `ovm`
needs to provide a polished macOS-first interface and a reusable headless core,
then support Linux without duplicating protocol behavior. The application must
handle inconsistent camera firmware, local-network discovery, SOAP/WS-*
authentication, RTSP media, secure credential storage, and diagnostics.

The project must remain open source under a permissive license, avoid GPL code
reuse, and keep protocol packages usable without the desktop UI.

## Decision

### Repository identity and license

- Go module: `github.com/addodelgrossi/ovm`
- macOS bundle identifier: `io.github.addodelgrossi.ovm`
- License: MIT

MIT was selected for its short, permissive terms. Dependency licenses must be
reviewed for compatibility and listed in `THIRD_PARTY_NOTICES`.

### Language and toolchain

Use the current stable Go release and pin the exact toolchain in CI. At the time
of this decision, the official download feed reported Go 1.27.2. Revalidate
before the first dependency lock is committed.

Go provides a small deployment footprint, good concurrency primitives, useful
cross-compilation, and a strong standard library for HTTP/XML/UDP. Public
protocol libraries will use `context.Context` and avoid UI dependencies.

### Desktop shell

Use Wails v2 while Wails v3 remains prerelease. At the time of this decision:

- Wails v2.16.0 was the latest stable release.
- Wails v3.0.0-beta.28 was prerelease and therefore ineligible.

Use Svelte 5 with TypeScript and Tailwind CSS 4 for the frontend. The exact
compatible frontend versions are pinned after a minimal native build proves
the combination. Wails uses the platform WebView instead of bundling a browser,
which keeps artifacts smaller than an Electron application.

Supported desktop baselines are macOS 13+ and Ubuntu 22.04+, on amd64 and
arm64 where platform tooling permits.

### ONVIF and SOAP

Implement a small typed ONVIF client in `pkg/onvif`, based directly on official
ONVIF WSDL/XSD documents. Handwrite only the operations in product scope.
Generated all-service bindings and existing Go ONVIF clients are references,
not runtime dependencies.

This provides control over:

- SOAP 1.2 with evidence-driven SOAP 1.1 fallback;
- WS-Security UsernameToken and clock offset;
- HTTP Digest and explicit Basic fallback;
- namespace tolerance without discarding validation;
- typed SOAP faults and raw redacted diagnostics;
- manufacturer/model/firmware quirks;
- cancellation, timeouts, response limits, and redirect security.

### RTSP and media

Use `github.com/bluenviron/gortsplib/v5` and
`github.com/bluenviron/mediacommon/v2`. The research snapshot found versions
v5.6.7 and v2.9.6 respectively; exact versions are revalidated and pinned when
the stream package is introduced.

Remux supported video to fragmented MP4 without transcoding. H.264 is the
baseline codec. H.265 is attempted only after runtime WebView support detection;
otherwise the application falls back to JPEG snapshots.

A separate ADR will finalize the binary preview transport after a native
Wails/WebKit spike. The preferred design is a token-protected loopback bridge
bound only to `127.0.0.1`, because Wails v2 does not expose an internal
WebSocket transport and its dynamic asset handlers have modern Vite development
constraints.

### CLI and secrets

Use Cobra for a human-readable CLI with stable `--json` output. Use an internal
secret-provider interface with go-keyring adapters for macOS Keychain and Linux
Secret Service. Android receives a build-tagged unsupported provider and accepts
credentials through secure prompt or environment input.

At the time of this decision, the research snapshot found Cobra v1.10.2 and
go-keyring v0.2.8. Versions are pinned only when integrated.

### Package boundaries

```text
cmd/ovm/                 Cobra CLI
pkg/onvif/               reusable ONVIF/SOAP/discovery/services library
pkg/stream/              reusable RTSP/remux/statistics library
internal/app/            UI/CLI use cases and normalized DTOs
internal/store/          non-secret storage and secret-provider adapters
internal/fakedevice/     stateful test and demo device
desktop/                 Wails backend and Svelte frontend
testdata/fixtures/       synthetic and provenance-tracked SOAP fixtures
docs/                    plan, ADRs, compatibility, quirks, and guides
.github/workflows/       validation and release automation
```

`pkg/onvif` and `pkg/stream` cannot import Wails, Cobra, persistence, or desktop
packages. CLI and desktop call `internal/app` rather than duplicating business
logic.

## Alternatives considered

### Wails v3

Rejected for v0.1.0 because it is still beta. Reconsider after a stable release
and a documented migration benefit.

### Electron

Rejected because it bundles a large browser runtime and weakens the desired
small native distribution footprint.

### Fyne or Gio

Rejected because the product requires a refined modern interface, strong HTML
media APIs, and efficient iteration on responsive layouts.

### React

Acceptable, but not selected. Svelte has a smaller runtime and a simpler state
and component model for this application.

### Existing Go ONVIF client as the core

Rejected because available libraries do not provide the required combination
of auth negotiation, namespace tolerance, trace redaction, quirks, and typed
scope control. They may be inspected for interoperability ideas without copying
incompatible code.

### Generated bindings for all WSDLs

Rejected because they create a large, difficult-to-audit API surface and often
encode namespace assumptions that fail against real firmware.

### FFmpeg as a required runtime

Rejected because the live path should not transcode and the CLI should remain a
small Go deployment. `ffprobe` may be used as an optional test oracle without
being distributed.

### SQLite

Deferred. Versioned atomic JSON is sufficient for the small v0.1.0 device and
preference set. A database can be reconsidered if migrations or scale justify it.

## Consequences

### Positive

- One Go core serves desktop, CLI, fake-device tests, and embedded diagnostics.
- Protocol behavior is explicit, testable, and independent of UI concerns.
- The frontend can deliver a polished native-looking macOS/Linux experience.
- No mandatory transcoder or cloud service is introduced.
- Credentials use native OS secret stores on supported desktops.

### Negative

- A custom ONVIF subset requires careful fixture coverage and ongoing quirk
  maintenance.
- Wails/WebKit codec behavior varies by OS and installed Linux media plugins.
- A loopback preview bridge adds a local attack surface that requires strict
  session authentication and lifecycle tests.
- Android cannot share the desktop keyring implementation.
- Hardware interoperability cannot be proven in public CI.

## Follow-up decisions

The following require focused ADRs when implemented:

1. ONVIF modeling and official-spec provenance.
2. Authentication negotiation and secret storage.
3. Live preview bridge framing, origin validation, and backpressure.
4. Mutation/readback semantics for device configuration.
5. Subsecond clock estimation and uncertainty.
6. Release packaging and cross-compilation constraints.

## References

- ONVIF Network Interface Specifications: https://www.onvif.org/profiles/specifications/
- Go downloads: https://go.dev/dl/
- Wails v2 documentation: https://wails.io/docs/introduction/
- Wails releases: https://github.com/wailsapp/wails/releases
- gortsplib: https://github.com/bluenviron/gortsplib
- mediacommon: https://github.com/bluenviron/mediacommon
