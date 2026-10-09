# Compatibility

No physical camera has been verified yet. This document deliberately separates
test evidence from hardware claims.

## Evidence levels

- **Schema-tested:** implementation is based on the cited official WSDL/XSD.
- **Synthetic-fixture-tested:** tests exercise project-authored responses.
- **Contributed-fixture-tested:** a redacted, provenance-tracked response from a
  real device passes the contract suite.
- **Hardware-verified:** the documented workflow passed against the named model
  and firmware on physical hardware.

## Current matrix

| Manufacturer | Model | Firmware | Evidence | Notes |
|---|---|---|---|---|
| Generic ONVIF fake device | Synthetic | Project-owned | Planned | Built incrementally during M1–M6 |

A v0.1.0 release requires hardware verification with at least two cameras.
