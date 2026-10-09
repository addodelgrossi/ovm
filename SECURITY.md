# Security Policy

## Supported versions

No production-ready version has been released. Security fixes are currently
made on the default development line.

## Reporting a vulnerability

Do not open a public issue for a vulnerability that could expose credentials,
execute code, bypass a destructive-action confirmation, or compromise devices.
Use GitHub's private vulnerability reporting feature for this repository. If it
is unavailable, contact the repository owner privately through their verified
GitHub profile.

Include a clear impact statement, affected version or commit, reproduction
steps, and suggested mitigation if known. Never include working credentials or
unredacted captures from a real camera.

The maintainers will acknowledge a report as soon as practical, keep the
reporter informed, and coordinate disclosure after a fix is available.

## Security guarantees

The project treats camera responses, discovery packets, stream metadata, and
imported fixtures as untrusted input. Passwords belong in native OS secret
stores and must not appear in logs, reports, telemetry, or URLs by default.
`ovm` does not include telemetry and does not intentionally send device data
outside the local network.
