package soap

// Version identifies a SOAP envelope version.
type Version string

const (
	// Version11 is SOAP 1.1.
	Version11 Version = "1.1"
	// Version12 is SOAP 1.2.
	Version12 Version = "1.2"
)

const (
	envelopeNamespace11 = "http://schemas.xmlsoap.org/soap/envelope/"
	envelopeNamespace12 = "http://www.w3.org/2003/05/soap-envelope"
)

// EnvelopeNamespace returns the normative envelope namespace for the version.
func (v Version) EnvelopeNamespace() string {
	switch v {
	case Version11:
		return envelopeNamespace11
	case Version12:
		return envelopeNamespace12
	default:
		return ""
	}
}

func versionFromNamespace(namespace string) (Version, bool) {
	switch namespace {
	case envelopeNamespace11:
		return Version11, true
	case envelopeNamespace12:
		return Version12, true
	default:
		return "", false
	}
}
