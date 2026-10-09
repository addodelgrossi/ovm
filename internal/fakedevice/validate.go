package fakedevice

import (
	"errors"
	"strings"
)

// errInvalidRequestWrapper reports that a modeled official Device request
// wrapper carried immediate element children that do not match the operation's
// WSDL request message: a wrong-namespace, unknown, missing, duplicated, or
// otherwise forbidden child. The dispatcher maps it to an InvalidArgVal fault
// before dispatch so a structurally invalid wrapper can never reach a builder.
//
// This is request-wrapper structural validation (immediate child QName and
// cardinality, plus the few lexical checks the WSDL request types imply). It is
// not full ONVIF schema conformance: it does not validate response shapes,
// deep element trees, or types beyond the request wrappers listed below.
var errInvalidRequestWrapper = errors.New("invalid request wrapper children")

// validateDeviceRequestWrapper enforces the immediate child QName and
// cardinality of the official Device request wrappers this synthetic device
// supports, based on the ONVIF Device Management WSDL (devicemgmt.wsdl) request
// messages. Synthetic operations (GetState/SetState) are intentionally excluded
// and keep their own distinct parsing in parseStateOperation.
//
// WSDL-derived rules enforced here:
//   - GetSystemDateAndTime, GetDeviceInformation, GetScopes, GetHostname,
//     GetNetworkInterfaces: no element children.
//   - GetCapabilities: zero-or-more tds:Category children and no other element
//     child. Each tds:Category value must be a member of the CapabilityCategory
//     enumeration (All, Analytics, Device, Events, Imaging, Media, PTZ).
//   - GetServices: exactly one tds:IncludeCapability child whose text is a
//     canonical xs:boolean lexical value.
//
// It is called only for a QName already resolved to a Device action by
// operationAction, so a foreign-namespace wrapper never reaches here.
func validateDeviceRequestWrapper(action string, operation operationWire) error {
	switch action {
	case ActionGetSystemDateAndTime,
		ActionGetDeviceInformation,
		ActionGetScopes,
		ActionGetHostname,
		ActionGetNetworkInterfaces:
		return requireNoElementChildren(operation)
	case ActionGetCapabilities:
		return validateGetCapabilities(operation)
	case ActionGetServices:
		return validateGetServices(operation)
	default:
		// Not an official Device wrapper with modeled request content (e.g. a
		// synthetic action). Nothing to validate here.
		return nil
	}
}

// requireNoElementChildren rejects immediate element children and non-whitespace
// character data, matching an element-only request wrapper with an empty WSDL
// sequence.
func requireNoElementChildren(operation operationWire) error {
	if len(operation.Elements) != 0 || strings.TrimSpace(operation.Value) != "" {
		return errInvalidRequestWrapper
	}
	return nil
}

// validateGetCapabilities permits zero-or-more tds:Category children in the
// Device namespace and rejects every other element child. Category values are
// validated against the CapabilityCategory enumeration.
func validateGetCapabilities(operation operationWire) error {
	if strings.TrimSpace(operation.Value) != "" {
		return errInvalidRequestWrapper
	}
	for _, child := range operation.Elements {
		if child.XMLName.Local != "Category" || child.XMLName.Space != deviceNamespace || !simpleElementContent(child) {
			return errInvalidRequestWrapper
		}
		if !isCapabilityCategory(child.Value) {
			return errInvalidRequestWrapper
		}
	}
	return nil
}

// validateGetServices requires exactly one tds:IncludeCapability child in the
// Device namespace whose text is a canonical xs:boolean lexical value.
func validateGetServices(operation operationWire) error {
	if len(operation.Elements) != 1 || strings.TrimSpace(operation.Value) != "" {
		return errInvalidRequestWrapper
	}
	child := operation.Elements[0]
	if child.XMLName.Local != "IncludeCapability" || child.XMLName.Space != deviceNamespace || !simpleElementContent(child) {
		return errInvalidRequestWrapper
	}
	if !isXSDBoolean(strings.TrimSpace(child.Value)) {
		return errInvalidRequestWrapper
	}
	return nil
}

// simpleElementContent accepts only character data plus namespace declaration
// attributes. The modeled WSDL children are simple typed values, so nested
// elements and ordinary attributes are structurally invalid.
func simpleElementContent(element textElement) bool {
	if len(element.Elements) != 0 {
		return false
	}
	for _, attribute := range element.Attrs {
		if attribute.Name.Space == "xmlns" || (attribute.Name.Space == "" && attribute.Name.Local == "xmlns") {
			continue
		}
		return false
	}
	return true
}

// capabilityCategories is the tds:CapabilityCategory enumeration from the ONVIF
// Device Management WSDL. "All" is included because the WSDL permits it, even
// though this synthetic device renders the same payload regardless.
var capabilityCategories = map[string]struct{}{
	"All":       {},
	"Analytics": {},
	"Device":    {},
	"Events":    {},
	"Imaging":   {},
	"Media":     {},
	"PTZ":       {},
}

func isCapabilityCategory(value string) bool {
	_, ok := capabilityCategories[value]
	return ok
}

// isXSDBoolean reports whether value is a canonical xs:boolean lexical form.
// xs:boolean also permits "1" and "0"; both are accepted here because they are
// valid lexical representations a conforming client may transmit.
func isXSDBoolean(value string) bool {
	switch value {
	case "true", "false", "1", "0":
		return true
	default:
		return false
	}
}
