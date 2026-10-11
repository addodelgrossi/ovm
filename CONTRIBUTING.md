# Contributing to ovm

Thank you for helping improve `ovm`.

## Before contributing

- Read [the implementation plan](docs/PLAN.md) and the relevant ADRs.
- Open an issue before a large architectural or protocol change.
- Never include camera passwords, authorization headers, private addresses that
  identify a real deployment, serial numbers, or unredacted SOAP captures.
- Do not copy code from GPL projects. Dependency licenses must be compatible
  with MIT distribution and recorded in `THIRD_PARTY_NOTICES`.

## Development workflow

1. Branch from the default branch.
2. Add a failing test or executable acceptance example where practical.
3. Make the smallest integrated change that passes it.
4. Run the repository validation commands.
5. Update documentation and fixtures when behavior changes.
6. Use a small Conventional Commit such as `feat(onvif): parse device info`.
7. Open a pull request; do not push directly to the protected default branch.

## Go style

- Run `gofmt` on all Go code.
- Pass `go vet`, unit tests, and the race detector.
- Accept `context.Context` for network and long-running operations.
- Keep `pkg/onvif` and `pkg/stream` independent of Wails, Cobra, and storage.
- Return typed errors and wrap them with useful context.
- Bound network input and close every owned resource.

## Frontend style

- Use TypeScript and accessible semantic HTML.
- Preserve keyboard navigation, visible focus, and AA contrast.
- Keep user-facing strings in the localization dictionaries.
- Run formatting, linting, type checks, and component tests.

## ONVIF fixtures and quirks

Synthetic fixtures are preferred. A contributed capture must be redacted and
include a provenance record. A quirk requires a fixture, narrow device matching,
documentation, diagnostics, and an opt-out.

## Developer Certificate of Origin

By contributing, you certify that you have the right to submit the contribution
under this project's MIT license.
