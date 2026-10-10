# Fake ONVIF device

`internal/fakedevice` is the project-owned, in-process integration harness. It
provides deterministic synthetic evidence without requiring multicast, camera
hardware, external services, or real credentials. Passing these tests is not an
ONVIF conformance or hardware-compatibility claim.

## Modeled surface

The handler implements the supported subset of:

- ONVIF Device operations used by inventory and service discovery;
- Media1 profiles, video sources, encoder configurations/options, stream URI,
  and snapshot URI operations;
- Media2 profiles, encoder configurations/options, stream URI, and snapshot URI
  operations; and
- an HTTP JPEG endpoint used by snapshot retrieval.

It is not an RTSP server. Stream URIs are synthetic presentation values used to
verify selection, validation, provenance, and redaction.

## Configuration and safety

`fakedevice.Config` controls SOAP versions, HTTP Digest/Basic authentication,
WS-Security UsernameToken requirements, clock skew, inventory values, Media1
and Media2 profile/configuration associations, synthetic JPEG bytes, supported
faults, and unsupported actions. A zero-valued media inventory receives bounded
main/substream defaults. All supplied collections and JPEG bytes are copied and
validated when the handler is created.

The JPEG endpoint shares the configured HTTP authentication policy but does not
require a SOAP UsernameToken. Optional synthetic URI queries let tests prove an
operational query reaches the GET while remaining absent from DTOs, errors, CLI
output, and request history.

Request history is bounded and credential-free. It records SOAP version and a
safe action identifier, or a fixed snapshot marker. It never stores request
bodies, Authorization values, usernames, passwords, profile queries, or raw
snapshot paths.

## Evidence covered

Application, command-tree, golden-output, and real-process tests exercise:

- full Media2 and direct Media1 profile reads;
- narrow per-operation Media1 fallback only after `ActionNotSupported`;
- distinct Media1/Media2 association tokens and partial capability results;
- unauthenticated, Digest plus UsernameToken, Basic opt-in, and plaintext-Basic
  double opt-in policies;
- deterministic profile ordering and `schema_version: 1` JSON envelopes;
- safe RTSP/snapshot presentation URIs and operational-query non-disclosure;
- explicit and deterministic-default snapshot profile selection, including
  pre-network rejection of malformed explicit tokens;
- encoded-size-bounded, dimension-bounded, structurally decoded JPEG retrieval,
  cancellation, `0600` output, temporary-file cleanup, atomic no-clobber
  publication, and explicit forced replacement; and
- process interruption with exit code 130.

Run the focused smoke coverage with:

```sh
go test -run 'TestProfilesCLIEndToEnd|TestSnapshotCLIEndToEnd' ./internal/cli -v
go test -run 'TestProcessM2ProfilesAndSnapshotAgainstFullFake|TestProcessProfilesInterruptAgainstFullFake' ./cmd/ovm -v
```

The complete local gate remains `make ci`.
