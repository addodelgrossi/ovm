# ovm

`ovm` is an open-source desktop and command-line manager for ONVIF-compatible
cameras, built for macOS first and Linux next.

> [!IMPORTANT]
> The project is under active development and is not yet suitable for changing
> production camera settings. The v0.1.0 release is gated on validation with at
> least two physical cameras.

## Goals

- Discover cameras through interface-aware WS-Discovery or manual endpoints.
- Inspect device identity, services, media profiles, and credential-free URLs.
- Preview and measure RTSP streams without transcoding.
- Safely configure encoder, imaging, time/NTP, users, and network settings.
- Diagnose firmware behavior through redacted SOAP traces and repeatable tests.
- Export deterministic, credential-free Markdown and JSON reports.
- Share one Go application core between the Wails desktop app and Cobra CLI.

`ovm` is not an NVR and does not provide continuous recording, analytics, or
remote cloud access.

## Status

See [the implementation plan](docs/PLAN.md) and
[ADR 0001](docs/adr/0001-stack-and-architecture.md). Milestone M0 is currently
in progress.

## Requirements

- Go 1.27.2
- Node.js 24 and npm 11 for frontend development
- Wails v2.14.0 and its platform prerequisites

Platform-specific desktop dependencies are documented as they are introduced.

## Development

```sh
go test ./...
go run ./cmd/ovm version
```

The full validation commands will be added during M0.

## Security

Passwords must never be written to project configuration, logs, reports, or
credential-bearing URLs by default. See [SECURITY.md](SECURITY.md) for reporting
security issues.

## Trademark

ONVIF is a trademark of ONVIF, Inc. This independent project is a manager for
ONVIF-compatible devices. It is not affiliated with, endorsed by, or certified
by ONVIF, Inc., and it does not use the ONVIF logo.

## License

MIT. See [LICENSE](LICENSE).
