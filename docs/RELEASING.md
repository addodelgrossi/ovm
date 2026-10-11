# Releasing

Releases are built only by GitHub Actions (`.github/workflows/release.yml`).
Nothing is uploaded from a workstation.

## What a release contains

| Package | Built by | Notes |
| --- | --- | --- |
| `ovm_<version>_{darwin,linux}_{amd64,arm64}.tar.gz`, `ovm_<version>_android_arm64.tar.gz` | GoReleaser | Static CLI (`CGO_ENABLED=0`) with LICENSE, README, CHANGELOG, and THIRD_PARTY_NOTICES |
| `ovm_<version>_checksums.txt` | GoReleaser | SHA-256 of the CLI archives |
| `*.spdx.json` | GoReleaser + Syft | One SPDX SBOM per CLI archive |
| `ovm_<version>_macos_universal.dmg` | `scripts/package-macos.sh` | Universal (arm64 + x86_64) app for macOS 13+ |
| `ovm_<version>_linux_{amd64,arm64}.deb` | `scripts/package-linux.sh` | `/usr/bin/ovm`, `/usr/bin/ovm-desktop`, desktop entry, icon; Ubuntu 22.04+ |
| `ovm_<version>_desktop_checksums.txt` | release job | SHA-256 of the DMG and Debian packages |

## Dry runs

Every pull request that touches the release machinery runs the whole workflow
as a dry run: the same packages are built with a `0.0.0-dryrun+<commit>`
version and uploaded as workflow artifacts, and nothing is published. Locally:

```sh
make release-snapshot   # CLI archives in dist/
make package-macos      # on macOS
make package-linux      # on Ubuntu with the Wails Linux dependencies and dpkg-dev
```

## Publishing a version

1. Complete the [physical acceptance](validation/README.md) with two cameras
   on the release candidate commit and commit the sanitized manifest as
   `docs/validation/vX.Y.Z.json`. Check it with
   `make validation-gate TAG=vX.Y.Z`.
2. Move the `CHANGELOG.md` entries from `[Unreleased]` to the version.
3. Tag the commit on `main`: `git tag -s vX.Y.Z && git push origin vX.Y.Z`.
4. The workflow refuses to publish unless the manifest gate passes, then
   builds every package and creates a **draft** release. Review the assets,
   add sanitized screenshots from demo mode, and publish the draft.

Only the final job has `contents: write`; every other job is read-only.

## macOS signing (optional)

Without credentials the app is ad-hoc signed: it runs, but Gatekeeper asks the
user to confirm the first launch (Control-click → Open). To ship a signed and
notarized DMG, add these repository secrets:

| Secret | Value |
| --- | --- |
| `APPLE_CERTIFICATE_P12` | Base64 of the Developer ID Application certificate (`.p12`) |
| `APPLE_CERTIFICATE_PASSWORD` | Password of the `.p12` |
| `APPLE_SIGNING_IDENTITY` | For example `Developer ID Application: Name (TEAMID)` |
| `APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_APP_PASSWORD` | Notarization account, team, and app-specific password |
