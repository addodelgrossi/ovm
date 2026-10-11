# ovm desktop

This directory contains the Wails v2 host and Svelte frontend for `ovm`.
Business and protocol behavior belongs in the root module's `internal/app`,
`pkg/onvif`, and `pkg/stream` packages rather than in Wails bindings.

## Composition and modes

The desktop executable is the production composition root. Normal startup opens
the versioned JSON metadata repository, constructs the platform secret-provider
adapter, and injects the production host-interface, discovery, inventory, and
media-profile use cases into `app.Manager`. Constructing the provider and
bootstrapping metadata do not read the keyring; secure storage is accessed only
when an operation needs credentials.

Run the isolated demo with:

```sh
go run . --demo
```

Demo mode uses only an in-memory repository, an in-memory secret provider, an
in-process camera bound to IPv4 loopback, and an authenticated loopback RTSP
server that streams a synthetic test pattern (H.265 for the main profile, H.264
for the others). Its interface list and discovery are synthetic and never probe
production interfaces or multicast networks. VLC launch is disabled in demo
mode; live preview, recording, and probing work against the synthetic stream.

Live preview starts a backend session that plays the profile's RTSP stream,
remuxes it to fragmented MP4, and serves it to the WebView through the
token-protected loopback bridge in `internal/preview`. Clips are saved to
`~/Movies/ovm` on macOS and `~/Videos/ovm` elsewhere.

The Diagnostics tab (⌘5) saves device reports as Markdown and JSON to
`~/Documents/ovm`, runs the clock check and the stability test, follows camera
events, and shows the SOAP console. Event feeds and stability tests run in the
background, are polled by identifier, and are cancelled and drained at
shutdown. Quick Setup (password, H.264, IR day mode, NTP) remembers its
progress per device, and ⌘/Ctrl+K opens the command palette. In demo mode the
camera publishes synthetic motion and digital-input events.

The Wails facade owns a root context and bounds every backend operation. Its
exported device methods return result/error envelopes with a stable
`BindingError` (`category`, `code`, and safe English `message`). Authentication
passwords are accepted only in the request, are never included in a response or
log, and are not retained by the host. `OpenInVLC` accepts only a device ID,
profile token, and transport; the Manager re-resolves a
credential-free RTSP URI and the host starts only an allowlisted VLC executable
without a shell.

## Development

```sh
cd frontend
npm ci
npm run check
npm test
npm run build

cd ..
go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0 dev
```

A pinned production build regenerates `frontend/wailsjs` and creates the native
application:

```sh
make build-desktop
```

Do not edit files under `frontend/wailsjs` by hand. The frontend `postinstall`
script creates an ignored `node_modules/go.mod` module boundary so root Go tests
do not traverse JavaScript dependencies.

For the complete backend and desktop gate, run:

```sh
make test
make test-race
make vet
make lint
make frontend-check
make frontend-test
make build-desktop
```

See the root [README](../README.md) and [architecture](../docs/ARCHITECTURE.md).
