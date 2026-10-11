# Device Quirks

No device quirks are enabled yet.

A quirk is accepted only when it has:

1. Narrow manufacturer, model, and firmware matching.
2. Redacted evidence with documented provenance.
3. A regression fixture and tests.
4. A diagnostic message explaining when it was applied.
5. An opt-out for troubleshooting.

Standards-compliant behavior remains the default. General protocol negotiation,
such as SOAP-version or Media2-operation fallback, is not a vendor quirk.
