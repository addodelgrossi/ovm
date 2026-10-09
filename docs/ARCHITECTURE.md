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

See [ADR 0001](adr/0001-stack-and-architecture.md) for the initial decision and
[PLAN.md](PLAN.md) for milestone details.
