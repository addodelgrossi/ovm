# ovm desktop

This directory contains the Wails v2 host and Svelte frontend for `ovm`.
Business and protocol behavior belongs in the root module's `internal/app`,
`pkg/onvif`, and `pkg/stream` packages rather than in Wails bindings.

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

See the root [README](../README.md) and [architecture](../docs/ARCHITECTURE.md).

The frontend `postinstall` script creates an ignored `node_modules/go.mod`
module boundary. This prevents the root `go test ./...` command from traversing
JavaScript dependencies that happen to contain unrelated Go source files;
frontend validation is performed with the npm scripts above.
