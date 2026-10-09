# ADR 0002: ONVIF modeling and specification provenance

- Status: Accepted
- Date: 2026-10-08
- Decision owners: ovm maintainers

## Context

ADR 0001 selected a small handwritten ONVIF client instead of generated
bindings or an existing client runtime. The protocol core must remain auditable,
tolerate namespace-prefix differences seen in embedded devices, and avoid
claiming support for operations that have not been modeled and tested.

ONVIF combines its own WSDL and XML Schema documents with SOAP and WS-*
standards. Those documents evolve independently, and examples or camera
captures can contain deployment identifiers and credentials. The project needs
a durable record of which normative sources inform each model and which test
evidence supports compatibility statements.

## Decision

### Supported model

`pkg/onvif` contains only types and operations needed by an implemented product
workflow. A service operation is added only with all of the following:

1. the normative WSDL action and request/response element names;
2. the required XML namespace URIs and optional-field semantics;
3. success, namespace-prefix variation, fault, malformed-input, and
   optional-field tests where those cases apply;
4. a synthetic or provenance-tracked fixture; and
5. a normalized application mapping outside `pkg/onvif` when UI or CLI output
   needs a stable product-level type.

Tasks 4–6 established the transport, authentication, and fake-device
foundations. Tasks 7–9 add the bounded Device inventory subset, interface-aware
WS-Discovery, normalized application use cases, and CLI adapters. Media,
Imaging, Events, mutation, and diagnostic-console models remain separate scoped
tasks.

### SOAP transport model

The transport represents SOAP 1.1 and SOAP 1.2 explicitly. Calls begin with
SOAP 1.2. A call retries once with SOAP 1.1 only after an actual HTTP 415 or a
fully parsed SOAP 1.2 `VersionMismatch` in the top-level fault code whose
response media type and envelope namespace both match the attempted version.
Authentication errors, timeouts, malformed XML, incompatible media types,
cross-version envelopes, misplaced fault QNames, and arbitrary HTTP failures do
not trigger a version downgrade.

Known SOAP structures are matched by namespace URI and local name. Prefixes are
never semantically significant. Local-name tolerance is limited to payloads
whose enclosing standardized namespace has already been established; it is not
a general namespace-discarding parser.

Serialized requests and wire responses are independently bounded. Request size
is computed with overflow-safe arithmetic before envelope allocation, DTD
scanning, replay, or transport; an over-limit request returns a typed error and
sends nothing. Responses are bounded before parsing, and parser
depth/element/attribute/namespace work plus retained decoded text are
independently capped. Document type declarations are rejected and owned bodies
are closed. Successful and fault SOAP responses must have the media type and
envelope namespace of the attempted version. Endpoints are restricted to HTTP
and HTTPS without URI userinfo. Cross-origin redirects are blocked so
authorization headers and WS-Security bodies cannot be replayed to a different
origin.

SOAP faults are represented by a typed fault with stable classifications for
`NotAuthorized`, `ActionNotSupported`, `InvalidArgVal`, and
`VersionMismatch`. Mandatory and singleton SOAP 1.1/1.2 fields are enforced;
SOAP 1.2 protocol classification uses the top code, and ONVIF application
classification uses the deliberate direct subcode rather than arbitrary
`Detail` descendants. Diagnostic response XML is redacted before truncation,
bounded on a valid UTF-8 boundary, and stored only in sanitized form. Parser
errors exposed to users are fixed categories. The operation payload remains
available to the next typed service decoder.

The fake device validates only the supported Device operations' immediate WSDL
request-wrapper structure: empty wrappers, `GetCapabilities` category children,
and the required `GetServices` boolean child. It also emits modeled capability
categories in schema sequence. This is not a claim of complete WSDL/XSD
validation or complete operation semantics; synthetic state operations retain
their separately documented contract.

The downstream typed decoder in `pkg/onvif/device` also enforces independent
aggregate decoded-text, attribute-data, and weighted-complexity caps in addition
to its depth/element and upstream SOAP wire limits. That device-seam hardening
was completed outside this remediation's edit scope.

### Normative source register

The following normative sources define the M1 protocol foundation. URLs are
recorded directly rather than vendoring specification text.

| Area | Normative source | Used for |
|---|---|---|
| ONVIF core | [ONVIF Core Specification](https://www.onvif.org/specs/core/ONVIF-Core-Specification.pdf) | SOAP binding, faults, security profile, and service conventions |
| Device service | [ONVIF Device Management WSDL](https://www.onvif.org/ver10/device/wsdl/devicemgmt.wsdl) | Device action and element provenance, beginning with system time |
| Shared types | [ONVIF schema](https://www.onvif.org/ver10/schema/onvif.xsd) | Shared date/time, capability, scope, service-version, and network types |
| Remote discovery | [ONVIF Remote Discovery WSDL](https://www.onvif.org/ver10/network/wsdl/remotediscovery.wsdl) | ONVIF discovery service and `NetworkVideoTransmitter` provenance |
| WS-Discovery | [WS-Discovery 2005/04](https://specs.xmlsoap.org/ws/2005/04/discovery/ws-discovery.pdf) and [OASIS WS-Discovery 1.1](https://docs.oasis-open.org/ws-dd/discovery/1.1/os/wsdd-discovery-1.1-spec-os.html) | Probe/ProbeMatches, correlation, multicast target, Hello/Bye, and accepted namespace families |
| WS-Addressing | [WS-Addressing 2004 submission](https://www.w3.org/Submission/2004/SUBM-ws-addressing-20040810/) and [W3C WS-Addressing 1.0 Core](https://www.w3.org/TR/ws-addr-core/) | EndpointReference, Action, MessageID, and RelatesTo handling |
| SOAP 1.2 | [W3C SOAP Version 1.2 Part 1](https://www.w3.org/TR/soap12-part1/) and [Part 2](https://www.w3.org/TR/soap12-part2/) | Envelope namespace, faults, HTTP content type, and action parameter |
| SOAP 1.1 | [W3C SOAP 1.1 Note](https://www.w3.org/TR/2000/NOTE-SOAP-20000508/) | Legacy envelope, faults, `text/xml`, and `SOAPAction` behavior |
| XML namespaces | [W3C Namespaces in XML 1.0](https://www.w3.org/TR/xml-names/) | Namespace URI and prefix treatment |

A future operation-specific change must extend this register when it relies on
a different service WSDL, schema revision, or WS-* standard.

### Fixture provenance and evidence

Project-authored fixtures live under `testdata/fixtures`. The machine-readable
`manifest.json` records, for every XML fixture:

- repository-relative path and SHA-256 digest;
- whether it is synthetic;
- how it was produced;
- the normative source used to shape it;
- the license under which it is contributed; and
- redaction actions, including an explicit not-applicable explanation.

Every XML fixture must be listed. Tests verify hashes and reject selected
obvious authorization-header, example-password, and private-address strings;
review remains responsible for broader identifying data. Synthetic fixtures use
invented values and do not imply physical camera compatibility.

A contributed device capture requires permission to redistribute, aggressive
redaction, a provenance entry, and review under `CONTRIBUTING.md`. Raw captures,
credentials, serial numbers, and identifying deployment addresses are never
committed.

Compatibility evidence remains one of: schema-tested,
synthetic-fixture-tested, contributed-fixture-tested, or hardware-verified. A
higher level is never inferred from a lower one. Vendor quirks additionally
require narrow device matching, a regression fixture, diagnostics, and an
opt-out as defined by the implementation plan.

## Consequences

- Protocol additions are smaller and require explicit evidence, but each new
  operation needs deliberate modeling work.
- Prefix variation is interoperable without accepting arbitrary namespaces.
- Tests and diagnostics can preserve useful protocol evidence without retaining
  credential material.
- Generated all-service bindings remain unsuitable as the public model because
  they obscure supported scope and enlarge the audit surface.
- The current fixture set proves only synthetic protocol behavior; hardware
  acceptance remains blocked until physical-device testing is recorded.

## Alternatives considered

### Generate all ONVIF service bindings

Rejected because the resulting public surface is much larger than product
scope, difficult to audit, and commonly encodes assumptions that embedded
firmware violates.

### Parse only by local name

Rejected because unrelated elements could be accepted as standardized SOAP or
ONVIF structures. Controlled local-name tolerance is used only after validating
the enclosing namespace.

### Commit copies of every specification

Rejected because it complicates provenance and update tracking. Stable source
URLs and operation-level citations are sufficient; fixtures remain
project-authored and licensed with the repository.

### Treat fake-device success as camera compatibility

Rejected because a synthetic implementation cannot establish vendor firmware
behavior. Evidence levels remain explicit.
