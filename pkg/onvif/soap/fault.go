package soap

// FaultKind classifies SOAP and ONVIF faults used for safe control flow.
type FaultKind string

const (
	// FaultUnknown is an unclassified SOAP fault.
	FaultUnknown FaultKind = "unknown"
	// FaultNotAuthorized is an ONVIF NotAuthorized fault.
	FaultNotAuthorized FaultKind = "not_authorized"
	// FaultActionNotSupported is an ONVIF ActionNotSupported fault.
	FaultActionNotSupported FaultKind = "action_not_supported"
	// FaultInvalidArgVal is an ONVIF InvalidArgVal fault.
	FaultInvalidArgVal FaultKind = "invalid_arg_val"
	// FaultVersionMismatch is a SOAP VersionMismatch fault.
	FaultVersionMismatch FaultKind = "version_mismatch"
)

// Fault is a parsed SOAP fault. Diagnostic XML is always bounded and redacted.
type Fault struct {
	Version    Version
	Kind       FaultKind
	Code       string
	Subcode    string
	Reason     string
	DetailXML  []byte
	RawXML     []byte
	HTTPStatus int
}

func (f *Fault) Error() string {
	message := "SOAP fault"
	if f.Kind != FaultUnknown {
		message += ": " + string(f.Kind)
	}
	if f.Reason != "" {
		message += ": " + RedactText(f.Reason)
	}
	return message
}

// Is maps classified faults to their stable sentinel errors.
func (f *Fault) Is(target error) bool {
	switch f.Kind {
	case FaultNotAuthorized:
		return target == ErrNotAuthorized
	case FaultActionNotSupported:
		return target == ErrActionNotSupported
	case FaultInvalidArgVal:
		return target == ErrInvalidArgVal
	case FaultVersionMismatch:
		return target == ErrVersionMismatch
	default:
		return false
	}
}

func (f *Fault) httpStatusCode() int { return f.HTTPStatus }

func classifySOAP12Fault(codeNamespace, codeLocal, subcodeNamespace, subcodeLocal string) FaultKind {
	if codeNamespace == envelopeNamespace12 && codeLocal == "VersionMismatch" {
		return FaultVersionMismatch
	}
	// ONVIF application codes are deliberate SOAP 1.2 Subcode values. A
	// matching QName in Code, Reason, Detail, or a deeper arbitrary descendant
	// does not control classification.
	if subcodeNamespace == onvifErrorNamespace {
		return classifyONVIFFaultLocal(subcodeLocal)
	}
	return FaultUnknown
}

func classifySOAP11Fault(codeNamespace, codeLocal string) FaultKind {
	if codeNamespace == envelopeNamespace11 && codeLocal == "VersionMismatch" {
		return FaultVersionMismatch
	}
	// SOAP 1.1 has no Subcode element. Preserve the deliberate compatibility
	// mapping for a direct ONVIF faultcode QName, but never inspect arbitrary
	// Detail descendants.
	if codeNamespace == onvifErrorNamespace {
		return classifyONVIFFaultLocal(codeLocal)
	}
	return FaultUnknown
}

func classifyONVIFFaultLocal(local string) FaultKind {
	switch local {
	case "NotAuthorized":
		return FaultNotAuthorized
	case "ActionNotSupported":
		return FaultActionNotSupported
	case "InvalidArgVal":
		return FaultInvalidArgVal
	default:
		return FaultUnknown
	}
}
