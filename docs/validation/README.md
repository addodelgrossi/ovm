# Physical acceptance

A release is published only after two physical cameras pass the acceptance
checks below and a sanitized manifest for that release is committed as
`docs/validation/<tag>.json` (for example `docs/validation/v0.1.0.json`). The
release workflow runs `go run ./internal/tools/validationgate` on it and stops
before publishing when it is missing or incomplete.

Use two different camera models, preferably from different manufacturers.
Start from [`template.json`](template.json).

## Checks

Run each check with the release candidate build, on the isolated camera
network, and record `pass`, `fail`, or (optional checks only) `not_supported`.

| Check | Required | How |
| --- | --- | --- |
| `discovery` | yes | The camera appears after **Scan** (or `ovm discover`). |
| `authentication` | yes | **Authenticate** succeeds; `ovm info` returns the inventory. |
| `profiles` | yes | Streams lists main and sub profiles with resolution and codec. |
| `rtsp_copy` | yes | The copied RTSP URL plays in VLC or `ffplay`. |
| `live_preview` | yes | Live shows video with frame rate and bitrate. |
| `h264` | yes | Quick Setup or Settings switches every profile to H.264; read back is `exact` or `adjusted`. |
| `ntp` | yes | NTP servers are applied and read back. |
| `password_change` | yes | The password is changed, the new sign-in is verified, and ovm still connects after a restart. |
| `clock_check` | yes | Diagnostics → Check clock gives a verdict after NTP sync. |
| `stability` | yes | Diagnostics → Stability test with the release preset (100 × 20 s) passes every cycle. |
| `report` | yes | Diagnostics → Save report writes Markdown and JSON with no password, serial number, or MAC address you did not expect. |
| `ir_day` | optional | Day mode reads back `ON`. |
| `events` | optional | Events shows camera events (for example motion). |
| `recording` | optional | A clip records and plays. |
| `snapshot` | optional | `ovm snapshot` writes a JPEG. |

## Sanitizing

The manifest identifies the camera model and firmware, never the unit. The gate
rejects:

- fields named like credentials or unit identifiers (`password`, `token`,
  `serial`, `mac`, `username`, ...),
- URLs, MAC addresses, and IPv4 addresses outside the RFC 5737 documentation
  ranges (`192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`),
- unknown fields, a placeholder commit, fewer than two cameras, or a stability
  run shorter than 100 cycles of 20 s or with any failed cycle.

Screenshots for the release notes must come from demo mode or be redacted the
same way.

## Checking locally

```sh
go run ./internal/tools/validationgate docs/validation/v0.1.0.json v0.1.0
```
