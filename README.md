# ovm

`ovm` is an open-source desktop and command-line manager for ONVIF-compatible
cameras, built for macOS first and Linux next.

> [!IMPORTANT]
> The project is under active development and is not yet suitable for changing
> production camera settings. The v0.1.0 release is gated on validation with at
> least two physical cameras.

## Goals

- Discover cameras through interface-aware WS-Discovery or manual endpoints.
- Inspect device identity, services, media profiles, and URI fields with
  credential-bearing components redacted.
- Preview and measure RTSP streams without transcoding.
- Safely configure encoder, imaging, time/NTP, users, and network settings.
- Diagnose firmware behavior through redacted SOAP traces and repeatable tests.
- Export deterministic, credential-free Markdown and JSON reports.
- Share one Go application core between the Wails desktop app and Cobra CLI.

`ovm` is not an NVR and does not provide continuous recording, analytics, or
remote cloud access.

## Status

See [the implementation plan](docs/PLAN.md) and
[ADR 0001](docs/adr/0001-stack-and-architecture.md). Milestone M1 (ONVIF core
and CLI) is complete. Milestone M2 (Media and Snapshot) is **in progress**:
typed Media1/Media2 clients, normalized profile and snapshot use cases, the
`profiles` and `snapshot` commands, and the expanded in-process fake device are
implemented and pass local synthetic-fixture tests. M2 remains in progress until
remote CI passes. No physical camera has been verified.

## Commands

```sh
# Print build version information.
ovm version [--json]

# Discover ONVIF cameras on the local network. Diagnostics are written to
# stderr in both human and JSON modes.
ovm discover [--iface name] [--timeout 3s] [--json]

# Retrieve a device inventory from a single camera.
ovm info <host> [-u user] [--password-env NAME] \
    [--allow-basic [--allow-insecure-basic]] [--allow-legacy-digest] \
    [--timeout duration] [--json]

# List every normalized media profile and presentation-safe URI.
ovm profiles <host> [-u user] [--password-env NAME] \
    [--allow-basic [--allow-insecure-basic]] [--allow-legacy-digest] \
    [--timeout duration] [--json]

# Write a JPEG snapshot to FILE. The profile token is optional; -o/--output is not.
ovm snapshot <host> [--profile TOKEN] -o FILE [--force] \
    [-u user] [--password-env NAME] \
    [--allow-basic [--allow-insecure-basic]] [--allow-legacy-digest] \
    [--timeout duration] [--json]
```

The host-scoped commands accept a host, `host:port`, or full `http(s)://` Device
service URL. `profiles` reports every profile in deterministic token order,
including Media1/Media2 provenance, codec, resolution, frame rate, bitrate, GOP,
and presentation-safe RTSP TCP/UDP and snapshot URIs. Displayed URIs never
contain userinfo or fragment components. RTSP URIs keep the camera's query,
because cameras commonly select the channel or substream with it (for example
`?channel=1&subtype=0`); snapshot URIs are shown without a query.

`snapshot` writes JPEG bytes only to the named file and reports credential-free
metadata to stdout. When `--profile` is omitted, it selects the
lexicographically smallest non-empty profile token returned by the preferred
media service. An explicit token is validated before any endpoint or network
work: it must contain 1–64 Unicode code points, be valid UTF-8, and contain no
control characters; XML-special characters remain valid and are escaped by the
protocol serializer. The response must advertise `image/jpeg`, stay within the
20 MiB default encoded-size bound, declare dimensions no larger than 16384 per
axis or 67,108,864 total pixels, and decode completely with no trailing data
before publication. Publication uses a same-directory `0600` temporary file and
is atomic. Existing files are refused unless `--force` explicitly authorizes an
atomic replacement; errors and cancellation leave no partial output.

`info` human output includes operation support, identity, time, hostname,
capability/service endpoints, scopes, and network interfaces. Machine output
for `discover`, `info`, `profiles`, and `snapshot` uses a stable
`schema_version: 1` typed envelope; runtime and validation failures retain the
command's envelope with a safe categorized `error` object and return a nonzero
exit code.

Active discovery emits the ONVIF-required WS-Discovery 2005/04 probe family;
2009/01 support is limited to coherent inbound parser tolerance. It requires
multicast-capable, non-point-to-point interfaces, probes every usable IPv4
source address with explicit egress and TTL 1, and retransmits each logical
Probe with bounded jitter while retaining its MessageID. The passive
`ListenStream` API reports setup/runtime errors, deduplicates MessageID per
interface, and suppresses stale Hello/Bye notifications using AppSequence.

Every URL-bearing field is validated and redacted at the application boundary.
Device/discovery presentation fields remove userinfo, query, and fragment;
Media presentation URIs additionally enforce camera-origin/host and supported
scheme rules; RTSP stream URIs keep their query exactly as the camera returned
it, while snapshot URIs have it stripped. Operational Media service and
snapshot URLs retain required paths and queries only inside the authenticated
use case and never cross into DTOs, CLI output, errors, or fake-device request
history. URI paths and opaque identity content remain visible and are not
claimed to be secret-free.

### Credentials

The host-scoped CLI commands support **environment-variable credentials only**.
Supply the username with `--user` and name the environment variable that holds
the password with `--password-env`; there is no plaintext `-p`/`--password`
flag, and the password value is never printed to stdout, stderr, logs, or JSON.
Secure terminal prompts and system-keychain input are planned for a later
milestone.

Populate the environment variable through a trusted secret injector or your
shell's non-echoing input; do not place a literal password in shell history.
Then run:

```sh
export OVM_CAMERA_PASSWORD
ovm info 192.0.2.10 -u admin --password-env OVM_CAMERA_PASSWORD --json
ovm profiles 192.0.2.10 -u admin --password-env OVM_CAMERA_PASSWORD --json
ovm snapshot 192.0.2.10 -u admin --password-env OVM_CAMERA_PASSWORD \
    -o snapshot.jpg --json
unset OVM_CAMERA_PASSWORD
```

### Authentication downgrades

Authentication is secure by default. Strong WS-Security UsernameToken over
origin-scoped HTTP Digest with `qop=auth` is used without any flag. Weaker
mechanisms require explicit, independent policy flags, credentials, and fixed
warnings on stderr that never include a secret, user, or endpoint:

- `--allow-basic` permits HTTP Basic only for HTTPS endpoints and scopes it to
  the camera origin. It does not permit plaintext transport.
- `--allow-insecure-basic` additionally permits Basic over plaintext HTTP and
  is rejected unless `--allow-basic` is also present. It emits a separate
  warning that reusable credentials are exposed to the network path.
- `--allow-legacy-digest` permits legacy no-qop (RFC 2069) Digest. Without this
  flag, the application boundary blocks any generated Digest `Authorization`
  header that lacks a `qop` directive before it reaches the network and reports
  the attempt as an authentication failure.

No downgrade is used implicitly. Basic, plaintext Basic, and legacy no-qop
Digest all remain deny-by-default.

### Exit codes

The CLI uses a stable exit-code mapping:

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Generic or internal failure |
| 2 | Usage or input error |
| 3 | Network, discovery, or timeout failure |
| 4 | Authentication failure |
| 5 | Protocol or malformed response |
| 130 | Cancelled (for example an interrupt signal) |

A timeout-only discovery round with no replies is a successful empty result and
writes a `no_response` diagnostic to stderr; it never claims permission denial.
Concrete socket or permission-like errors use exit 3. An explicit cancellation
(for example an interrupt) always maps to 130. Discovery, inventory, and profile
commands preserve any useful partial result already assembled; snapshot
cancellation removes its temporary file and never emits JPEG bytes to stdout.

## Requirements

- Go 1.27.2
- Node.js 24 and npm 11 for frontend development
- Wails v2.16.0 and its platform prerequisites

Platform-specific desktop dependencies are documented as they are introduced.

## Development

```sh
make format
make lint
make vet
make test
make frontend-check

go run ./cmd/ovm version
go run ./cmd/ovm discover --timeout 3s

# Hermetic M2 CLI smoke coverage against the complete in-process fake device.
go test -run 'TestProfilesCLIEndToEnd|TestSnapshotCLIEndToEnd' ./internal/cli -v
go test -run TestProcessM2ProfilesAndSnapshotAgainstFullFake ./cmd/ovm -v
```

The optional manual real-camera smoke test is excluded from the default build
and from public CI by a build tag. It never runs with shared credentials and
does not produce a hardware-verified compatibility claim:

Populate all three variables through trusted, non-echoing input first, then:

```sh
export OVM_TEST_HOST OVM_TEST_USER OVM_TEST_PASS
go test -tags manual -run TestManualRealCameraInventory ./internal/app -v
unset OVM_TEST_HOST OVM_TEST_USER OVM_TEST_PASS
```

## Security

Passwords must never be written to project configuration, logs, reports, or
credential-bearing URLs by default. The CLI reads a password only from the
environment variable named by `--password-env`, keeps it ephemeral and
non-serializable, and never echoes it. Error messages carry stable, credential-
free text, and URL-bearing fields are redacted at the application boundary.
Authentication downgrades (Basic, legacy no-qop Digest) are opt-in and each
emits one explicit stderr warning. See [SECURITY.md](SECURITY.md) for reporting
security issues.

## Trademark

ONVIF is a trademark of ONVIF, Inc. This independent project is a manager for
ONVIF-compatible devices. It is not affiliated with, endorsed by, or certified
by ONVIF, Inc., and it does not use the ONVIF logo.

## License

MIT. See [LICENSE](LICENSE).
