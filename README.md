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
and CLI) is **in progress**. The typed Device inventory client, interface-aware
WS-Discovery, `discover` and `info` commands, and shared `internal/app` core are
implemented and covered by synthetic-fixture tests against the in-process fake
ONVIF device and injected packet connections. No camera hardware has been
verified and the remote CI gate has not been declared passed for M1.

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
```

`info` accepts a host, `host:port`, or a full `http(s)://` URL and canonicalizes
it to the ONVIF device service path. Human output includes operation support,
identity, time, hostname, capability/service endpoints, scopes, and network
interfaces. JSON output always uses a `schema_version: 1` envelope; runtime and
validation failures retain that envelope with a safe `error` object and return
a nonzero exit code.

Active discovery emits the ONVIF-required WS-Discovery 2005/04 probe family;
2009/01 support is limited to coherent inbound parser tolerance. It requires
multicast-capable, non-point-to-point interfaces, probes every usable IPv4
source address with explicit egress and TTL 1, and retransmits each logical
Probe with bounded jitter while retaining its MessageID. The passive
`ListenStream` API reports setup/runtime errors, deduplicates MessageID per
interface, and suppresses stale Hello/Bye notifications using AppSequence.

Every URL-bearing field (discovery addresses, scopes, type namespaces, and
capability/service endpoints) is validated and redacted at the application
boundary: userinfo, query, and fragment are removed, and a value that is not a
valid URI — or, for a transport endpoint, not an absolute HTTP(S) URL with a
host — is omitted behind a safe marker so invalid values and credentials in
userinfo, query, or fragment components never reach output. URI path and opaque
identity content are preserved and are not claimed to be secret-free.

### Credentials

M1 supports **environment-variable credentials only**. Supply the username with
`--user` and name the environment variable that holds the password with
`--password-env`; there is no plaintext `-p`/`--password` flag, and the password
value is never printed to stdout, stderr, logs, or JSON. Secure terminal prompts
and system-keychain input are planned for a later milestone.

Populate the environment variable through a trusted secret injector or your
shell's non-echoing input; do not place a literal password in shell history.
Then run:

```sh
export OVM_CAMERA_PASSWORD
ovm info 192.0.2.10 -u admin --password-env OVM_CAMERA_PASSWORD --json
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
(for example an interrupt) always maps to 130 and returns whatever partial
inventory was collected, even if some operations had already succeeded.

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
