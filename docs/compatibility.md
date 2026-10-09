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
| Generic ONVIF fake device | Synthetic | Project-owned | Synthetic-fixture-tested | All seven Device inventory operations, plus unauthenticated, HTTP Digest + WS-Security UsernameToken, and opt-in HTTP Basic authentication, pass end to end through `internal/app` and the CLI against `internal/fakedevice`. |

The synthetic-fixture evidence covers the `discover` and `info` application and
CLI paths only. It is **not** a hardware or conformance claim. The optional
manual real-camera smoke test (build tag `manual`, environment variables
`OVM_TEST_HOST`/`OVM_TEST_USER`/`OVM_TEST_PASS`) runs only on an operator's own
isolated network and does **not** upgrade any row to hardware-verified.

A v0.1.0 release requires hardware verification with at least two cameras.

## WS-Discovery version posture

Outbound probes are emitted as a fully coherent WS-Discovery **2005/04**
baseline message (discovery namespace `.../ws/2005/04/discovery`, the 2004/08
addressing generation that pairs with the 2005/04 draft, and the matching
`Probe` action). The project does not emit 2009/01 outbound values and never
mixes generations in a single datagram.

**2009/01 is parser tolerance only.** The discovery parser accepts both the
2005/04 and 2009/01 discovery namespaces (and the common WS-Addressing
namespaces) on inbound Hello/Bye/ProbeMatch datagrams, so a device that answers
in the newer generation is still understood. This is an inbound compatibility
affordance, not an outbound capability claim. The discovery worker's own package
comments remain the source of truth for the exact namespaces; this note only
summarises them and does not change discovery behaviour.

Multicast join, retransmission, and the passive Hello/Bye lifecycle are
implemented in the discovery package through injectable sockets and are
exercised in that package's tests; the current hardening work did not change
discovery source, so no new multicast/retransmit/lifecycle behaviour is claimed
here beyond what the discovery package already documents.
