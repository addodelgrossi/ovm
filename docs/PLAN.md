# ovm v0.1.0 Implementation Plan

Status: **M1 — ONVIF Core and CLI in progress**
Last updated: 2026-10-08

## Product goal

`ovm` is an open-source desktop and command-line manager for ONVIF-compatible
cameras. It targets macOS first and Ubuntu Linux next. It helps a technician
discover cameras, inspect and configure ONVIF services, preview and measure RTSP
streams, run repeatable diagnostics, and export redacted evidence without
becoming an NVR.

The motivating workflow is a Mac connected by Ethernet to an isolated camera
network. The technician must quickly retrieve main/substream URLs, enforce
H.264 and day mode, configure NTP, replace supplied credentials, validate
stream stability, and produce a report.

## Locked decisions

- Module: `github.com/addodelgrossi/ovm`
- Bundle identifier: `io.github.addodelgrossi.ovm`
- License: MIT
- Platforms: macOS 13+ on amd64/arm64; Ubuntu 22.04+ on amd64/arm64
- CLI diagnostic target: Linux arm64 and Android arm64 where the selected
  dependencies support a static, headless build
- Backend: Go using the current stable toolchain
- Desktop: stable Wails v2 while Wails v3 remains prerelease
- Frontend: Svelte, TypeScript, and Tailwind CSS
- ONVIF: a small typed client based on official WSDL/XSD documents
- RTSP: gortsplib and mediacommon, without transcoding
- CLI: Cobra
- Desktop secrets: macOS Keychain and Linux Secret Service through go-keyring
- Communication and all repository content are English; maintainer chat may be
  Brazilian Portuguese
- No telemetry, cloud service, credential guessing, continuous recording, or
  ONVIF conformance claim
- PTZ is not on the v0.1.0 critical path

## Architecture

```text
Cobra CLI ---------+
                   +--> internal/app --> pkg/onvif --> SOAP / WS-Discovery
Wails backend -----+        |          --> pkg/stream --> RTSP
       |                    +----------> internal/store / secret provider
Svelte frontend <--+-------- Wails bindings and events
       ^
       +--------------------- token-protected loopback preview bridge

internal/fakedevice -------- integration tests and desktop demo mode
```

### Boundaries

- `pkg/onvif` and `pkg/stream` are reusable public libraries. They do not
  import Wails, Cobra, persistence, or application UI packages.
- `internal/app` owns use-case orchestration, cancellation, capability
  degradation, normalized DTOs, safe mutations, readback, and progress.
- CLI and desktop consume `internal/app`; they do not duplicate protocol code.
- `internal/store` persists only versioned, non-secret metadata. Passwords are
  held by a secret-provider interface.
- Long-running discovery, RTSP, event, and diagnostic operations have explicit
  lifecycle ownership, bounded concurrency, context cancellation, and leak
  tests.

## Protocol principles

### ONVIF and SOAP

- Handwrite only the supported typed subset and cite the official source WSDL.
- Prefer SOAP 1.2. Retry SOAP 1.1 only after protocol evidence such as HTTP 415
  or `VersionMismatch`.
- Parse known structures namespace-strictly, with controlled local-name
  tolerance for prefix divergence.
- Bound response bodies, reject DTDs, restrict URL schemes, and never forward
  credentials to another origin through redirects.
- Convert SOAP faults to typed Go errors while retaining bounded, redacted XML
  for diagnostics.
- Negotiate authentication in this order: unauthenticated system time,
  offset-adjusted WS-UsernameToken PasswordDigest, HTTP Digest, and Basic only
  after explicit opt-in.
- Generate whole-second RFC3339 `Created` values for interoperability.
- Prefer Media2 per operation and fall back to Media v1 only for an unsupported
  or protocol-incompatible operation—not for authentication or network errors.
- Represent support as `supported`, `unsupported`, `unknown`, or
  `temporarily unavailable`.
- Every mutation follows: read current value, read options, validate, set,
  poll/read back, and report exact/clamped/failed/disconnected.

### Vendor quirks

A quirk requires narrow manufacturer/model/firmware matching, provenance,
a regression fixture, visible diagnostics, and an opt-out. Compatibility is
reported separately as schema-tested, synthetic-fixture-tested,
contributed-fixture-tested, or hardware-verified.

### Streaming

- RTSP-over-TCP is the default; UDP is optional.
- The stream pipeline depacketizes video, detects codec configuration and
  keyframes, measures statistics, and emits fMP4 initialization/media parts.
- A preview bridge binds to `127.0.0.1` on an ephemeral port and uses random
  session credentials, strict origins, bounded queues, and clean shutdown.
- The frontend verifies codec support with MSE. Unsupported codecs fall back to
  authenticated JPEG polling at an adaptive 1–5 fps.
- Clips begin at a keyframe and are atomically finalized as fragmented MP4.
- Measured bitrate means encoded media payload, not transport overhead.

### Time accuracy

ONVIF system time exposes integer seconds and is suitable only for coarse
clock offset and configuration. Subsecond checks use RTCP Sender Reports when
available and always report method and uncertainty. A result is
`inconclusive` when the source cannot substantiate the requested precision.

## Milestones

| Milestone | Status | Deliverable | Exit gate |
|---|---|---|---|
| M0 Foundation | Complete | Repository, docs, ADR 0001, license, minimal CLI/desktop, CI | Passed locally and in GitHub Actions on macOS/Ubuntu |
| M1 ONVIF Core + CLI | In progress | SOAP, auth, discovery, Device info, fake device, `discover`/`info` | Fake-device end-to-end workflow |
| M2 Media | Planned | Media/Media2 reads, URLs, snapshots, `profiles`/`snapshot` | Correct main/substream URLs |
| M3 Desktop Base | Planned | Wails UI, persistence, Keychain, sidebar, Overview, Streams, demo | Discover and copy RTSP end to end |
| M4 Video | Planned | RTSP, fMP4/MSE, fallback, stats, grid, record, probe | Native preview and measured diagnostics |
| M5 Configuration | Planned | Encoder, Imaging, Time/NTP, Users, Network, Maintenance | Every mutation read back |
| M6 Diagnostics | Planned | SOAP console, Events, clock/stability, reports, Quick Setup, palette | Complete redacted reports |
| M7 Distribution | Planned/blocked on hardware | macOS/Linux packages, CLI releases, two-camera gate, GitHub release | Physical acceptance and green release workflow |

## Detailed milestone scope

### M0 — Foundation

- Add repository/community/security documentation and MIT license.
- Pin a mutually compatible Go/Wails/Svelte/Tailwind toolchain after checking
  the current stable release feeds (Wails v2.16.0 at M0 implementation time).
- Create a minimal `ovm version` command with human and JSON output.
- Create a minimal desktop shell sharing build metadata.
- Add reproducible formatting, linting, testing, race, frontend, license, and
  build commands.
- Add GitHub Actions for macOS and Ubuntu.

### M1 — ONVIF Core and CLI

- SOAP 1.2/1.1 transport, typed faults, safe tracing, timeouts, and cancellation.
- WS-Security PasswordDigest, clock offset, HTTP Digest, and explicit Basic.
- Stateful fake ONVIF device and fixture provenance framework.
- Typed Device inventory operations.
- Interface-aware WS-Discovery, Hello/Bye, and manual endpoint entry.
- `ovm discover` and `ovm info` with stable JSON output.

### M2 — Media

- Media v1 and Media2 profile/source/configuration reads.
- Per-operation Media2 fallback and normalized profile DTOs.
- Safe RTSP/snapshot URI handling.
- Authenticated JPEG retrieval.
- `ovm profiles` and `ovm snapshot`.

### M3 — Desktop Base

- Atomic device/preferences storage and platform secret providers.
- Accessible design system, light/dark themes, English/pt-BR, and demo mode.
- Device sidebar, scanning, interface selection, manual add, search, and status.
- Overview and Streams tabs, copy actions, and optional VLC launch.
- Local Network privacy description and actionable diagnostics.

### M4 — Video

- Cancellable RTSP sessions with TCP/UDP, auth, reconnect, and leak tests.
- Codec/resolution/fps/bitrate/GOP/TTFF/loss/reconnect measurements.
- H.264/H.265 fMP4 packaging and secure loopback preview bridge.
- MSE Live view, snapshot fallback, fullscreen, and four-camera grid.
- Keyframe-aligned MP4 clip recording.
- Stream Probe in desktop and CLI.

### M5 — Configuration

- Validated Media/Media2 encoder mutations with readback.
- Imaging settings/options including explicit IR Cut Day/Night/Auto semantics.
- Manual time and NTP configuration with honest uncertainty.
- User administration and supplied-default-credential warnings.
- Guarded network changes with two confirmations and reconnection.
- Reboot, soft factory reset, and bounded system-log retrieval.

### M6 — Diagnostics and automation

- Bounded, redacted SOAP Console.
- PullPoint subscription, renewal, event feed, and filtering.
- Hybrid Clock Sync Check and repeated Stability Test.
- Deterministic, credential-free JSON and Markdown Device Reports.
- Resumable Quick Setup and keyboard command palette.

### M7 — Distribution and release

- Universal macOS app/DMG and Ubuntu 22.04 package.
- GoReleaser CLI archives for supported Darwin/Linux/Android targets.
- Checksums, SBOM, third-party notices, and sanitized screenshots.
- GitHub Actions-only tagged release with least-privilege permissions.
- Mandatory two-camera physical acceptance and checked-in sanitized validation
  manifest before a release job can publish v0.1.0.

## Test policy

- Use red-green-refactor for protocol and application behavior.
- Every supported SOAP operation receives success, namespace-variation,
  fault/unsupported, and malformed/optional-field coverage.
- Synthetic fixtures are the default. Contributed captures are redacted and
  include provenance.
- Discovery tests inject packet connections instead of requiring multicast in
  public CI.
- RTSP tests use a local gortsplib server and project-owned synthetic media.
- Frontend tests cover components, accessibility, keyboard behavior, and
  mocked Wails bindings. Native WebKit smoke tests remain mandatory.
- Public CI never requires real-camera credentials.
- Optional real-device tests use `OVM_TEST_HOST`, `OVM_TEST_USER`, and
  `OVM_TEST_PASS`.
- Race and goroutine-leak checks cover long-running subsystems.

## Release acceptance

A physical release candidate must demonstrate, on a Mac connected to two
cameras on an isolated LAN:

1. Discovery on the selected wired interface or successful manual addition.
2. Authentication and Keychain-backed reconnection with model/firmware shown.
3. Main, substream, and snapshot URLs retrieved for both cameras.
4. Two simultaneous previews with measured fps, bitrate, and GOP.
5. H.264, supported encoder values, IR Cut Day, and NTP applied and read back.
6. Supplied credential changed without guessing any other password.
7. 30-second Stream Probe and 100 × 20-second Stability Test per camera.
8. Credential-free Markdown and JSON reports exported.
9. Discovery, information, profiles, and diagnostics repeated through CLI JSON.

The `< 50 ms` clock criterion passes only with defensible subsecond evidence
and uncertainty. An inconclusive result does not pass the release gate.

## Current blockers

- No physical cameras are currently available. This does not block M0–M6 or
  release-workflow dry runs, but it blocks the validation manifest, release tag,
  and v0.1.0 publication.
- Signing/notarization credentials are not assumed. Unsigned artifacts and
  complete instructions can be produced; signed/notarized publication requires
  separately supplied secrets.

## Risk register

| Risk | Mitigation |
|---|---|
| Firmware divergence | Evidence-backed quirks, fixtures, per-operation fallback |
| Wails/WebKit media behavior | Native fMP4 bridge spike before full Live UI |
| H.265 unsupported | Runtime detection and JPEG fallback |
| Local Network privacy | Correct plist, diagnostics, manual add, macOS 15 validation |
| False subsecond precision | RTCP-based method, uncertainty, inconclusive state |
| Network mutation disconnect | Double confirmation, endpoint rescan, recovery guidance |
| Silent `Set*` failure | Mandatory delayed readback |
| Credential leakage | OS keyring, redirect controls, redaction/property tests |
| Android incompatibility | CLI-only build tags and binary inspection |
| Scope growth | Strict milestone gates and no orphan abstractions |

## Definition of done

Every task must end with integrated behavior, targeted tests, formatting/lint
and affected builds passing, cancellation and redaction preserved, and relevant
documentation updated. No dead code, unexplained TODO, unpinned dependency, or
unsupported compatibility claim is accepted.

At each milestone close:

1. Update this document with completed work, next work, and current risks.
2. Update `CHANGELOG.md`.
3. Run the complete milestone validation command.
4. Commit with a small Conventional Commit message.
