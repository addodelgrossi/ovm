# ovm frontend

This is the Svelte 5, TypeScript, Tailwind CSS, and Vite frontend embedded by
the Wails desktop host.

## Commands

```sh
npm ci
npm run format:check
npm run lint
npm run check
npm test
npm run build
```

All Wails calls must go through generated bindings. Protocol and business logic
belong in the Go application core, not in frontend components. User-facing text
must be externalized when localization is introduced in M3.
