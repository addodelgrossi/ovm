package fakedevice

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"
)

const (
	envelopeNamespace11 = "http://schemas.xmlsoap.org/soap/envelope/"
	envelopeNamespace12 = "http://www.w3.org/2003/05/soap-envelope"
	deviceNamespace     = "http://www.onvif.org/ver10/device/wsdl"
	typeNamespace       = "http://www.onvif.org/ver10/schema"
	errorNamespace      = "http://www.onvif.org/ver10/error"
	syntheticNamespace  = "urn:ovm:synthetic"
	wsseNamespace       = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"
	wsuNamespace        = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"
	//nolint:gosec // This is a public WS-Security type URI, not a credential.
	passwordDigestURI = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest"
	base64BinaryURI   = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary"
)

// errUnknownOperation reports an operation element whose namespace-qualified
// name is not one the synthetic device models. It is handled by the dispatcher
// as an InvalidArgVal fault so an unknown or foreign QName can never be
// dispatched on the strength of a SOAP header action alone.
var errUnknownOperation = errors.New("unknown operation QName")

type requestEnvelope struct {
	XMLName xml.Name
	Header  *headerWire `xml:"Header"`
	Body    requestBody `xml:"Body"`
}

type headerWire struct {
	XMLName  xml.Name
	Security *securityWire `xml:"Security"`
}

type securityWire struct {
	XMLName xml.Name
	Token   *usernameTokenWire `xml:"UsernameToken"`
}

type usernameTokenWire struct {
	XMLName  xml.Name
	Username textElement       `xml:"Username"`
	Password credentialElement `xml:"Password"`
	Nonce    credentialElement `xml:"Nonce"`
	Created  textElement       `xml:"Created"`
}

type credentialElement struct {
	XMLName      xml.Name
	Type         string `xml:"Type,attr"`
	EncodingType string `xml:"EncodingType,attr"`
	Value        string `xml:",chardata"`
}

type textElement struct {
	XMLName  xml.Name
	Attrs    []xml.Attr    `xml:",any,attr"`
	Value    string        `xml:",chardata"`
	Elements []textElement `xml:",any"`
}

type requestBody struct {
	XMLName    xml.Name
	Operations []operationWire `xml:",any"`
}

type operationWire struct {
	XMLName  xml.Name
	Value    string        `xml:",chardata"`
	Elements []textElement `xml:",any"`
}

type stateOperation struct {
	Key   string
	Value string
}

// deviceOperations maps the modeled Device operation local names to their
// action URIs. Every entry is validated against the Device namespace in
// operationAction so a foreign namespace with a matching local name is
// rejected.
var deviceOperations = map[string]string{
	"GetSystemDateAndTime": ActionGetSystemDateAndTime,
	"GetDeviceInformation": ActionGetDeviceInformation,
	"GetCapabilities":      ActionGetCapabilities,
	"GetServices":          ActionGetServices,
	"GetScopes":            ActionGetScopes,
	"GetHostname":          ActionGetHostname,
	"GetNetworkInterfaces": ActionGetNetworkInterfaces,
}

// syntheticOperations maps project-authored synthetic operation local names to
// their action URIs, validated against the synthetic namespace.
var syntheticOperations = map[string]string{
	"GetState": ActionGetState,
	"SetState": ActionSetState,
}

func parseRequestEnvelope(data []byte, expected SOAPVersion) (requestEnvelope, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return requestEnvelope{}, err
		}
		if _, ok := token.(xml.Directive); ok {
			return requestEnvelope{}, errors.New("document type declarations are not allowed")
		}
	}
	var envelope requestEnvelope
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return requestEnvelope{}, err
	}
	if envelope.XMLName.Local != "Envelope" || envelope.XMLName.Space != envelopeNamespace(expected) {
		return requestEnvelope{}, errors.New("invalid SOAP Envelope namespace")
	}
	if envelope.Header != nil && (envelope.Header.XMLName.Local != "Header" || envelope.Header.XMLName.Space != envelope.XMLName.Space) {
		return requestEnvelope{}, errors.New("invalid SOAP Header namespace")
	}
	if envelope.Body.XMLName.Local != "Body" || envelope.Body.XMLName.Space != envelope.XMLName.Space {
		return requestEnvelope{}, errors.New("invalid SOAP Body namespace")
	}
	if len(envelope.Body.Operations) != 1 {
		return requestEnvelope{}, errors.New("SOAP Body must contain exactly one operation")
	}
	return envelope, nil
}

// operationAction resolves the action URI for a request body operation. It is
// namespace-strict: a modeled local name in the wrong namespace, or any
// unmodeled QName, is rejected with errUnknownOperation. This guarantees the
// dispatcher always derives the action from the validated body operation and
// never from a SOAP header action alone.
func operationAction(operation operationWire) (string, error) {
	local := operation.XMLName.Local
	space := operation.XMLName.Space
	if action, ok := deviceOperations[local]; ok {
		if space != deviceNamespace {
			return "", errUnknownOperation
		}
		return action, nil
	}
	if action, ok := syntheticOperations[local]; ok {
		if space != syntheticNamespace {
			return "", errUnknownOperation
		}
		return action, nil
	}
	return "", errUnknownOperation
}

func parseStateOperation(operation operationWire) (stateOperation, error) {
	key, keyPresent, err := operationElement(operation, syntheticNamespace, "Key")
	if err != nil {
		return stateOperation{}, err
	}
	key = strings.TrimSpace(key)
	if !keyPresent || key == "" {
		return stateOperation{}, errors.New("synthetic state key is required")
	}
	value, _, err := operationElement(operation, syntheticNamespace, "Value")
	if err != nil {
		return stateOperation{}, err
	}
	return stateOperation{Key: key, Value: value}, nil
}

func operationElement(operation operationWire, namespace, local string) (string, bool, error) {
	var (
		value string
		found bool
	)
	for _, element := range operation.Elements {
		if element.XMLName.Local != local {
			continue
		}
		if element.XMLName.Space != namespace || found {
			return "", false, errors.New("synthetic state element has an invalid namespace or duplicate")
		}
		value, found = element.Value, true
	}
	return value, found, nil
}

func usernameTokenFromEnvelope(envelope requestEnvelope) *usernameTokenWire {
	if envelope.Header == nil || envelope.Header.Security == nil {
		return nil
	}
	security := envelope.Header.Security
	if security.XMLName.Local != "Security" || security.XMLName.Space != wsseNamespace {
		return nil
	}
	return security.Token
}

func envelopeNamespace(version SOAPVersion) string {
	if version == SOAP11 {
		return envelopeNamespace11
	}
	return envelopeNamespace12
}

func buildEnvelope(config normalizedConfig, version SOAPVersion, payload string) string {
	envelopePrefix := config.EnvelopePrefix
	return `<?xml version="1.0" encoding="UTF-8"?><` + envelopePrefix + `:Envelope xmlns:` + envelopePrefix + `="` + envelopeNamespace(version) + `" xmlns:` + config.OperationPrefix + `="` + deviceNamespace + `" xmlns:tt="` + typeNamespace + `" xmlns:ter="` + errorNamespace + `" xmlns:ovm="` + syntheticNamespace + `"><` + envelopePrefix + `:Body>` + payload + `</` + envelopePrefix + `:Body></` + envelopePrefix + `:Envelope>`
}

func buildFault(config normalizedConfig, version SOAPVersion, fault FaultSpec) string {
	envelopePrefix := config.EnvelopePrefix
	reason := escapeXML(fault.Reason)
	if reason == "" {
		reason = "Synthetic ONVIF fault"
	}
	if version == SOAP11 {
		code := "ter:" + string(fault.Kind)
		if fault.Kind == FaultVersionMismatch {
			code = envelopePrefix + ":VersionMismatch"
		}
		return buildEnvelope(config, version, `<`+envelopePrefix+`:Fault><faultcode>`+code+`</faultcode><faultstring>`+reason+`</faultstring><detail/></`+envelopePrefix+`:Fault>`)
	}
	if fault.Kind == FaultVersionMismatch {
		return buildEnvelope(config, version, `<`+envelopePrefix+`:Fault><`+envelopePrefix+`:Code><`+envelopePrefix+`:Value>`+envelopePrefix+`:VersionMismatch</`+envelopePrefix+`:Value></`+envelopePrefix+`:Code><`+envelopePrefix+`:Reason><`+envelopePrefix+`:Text xml:lang="en">`+reason+`</`+envelopePrefix+`:Text></`+envelopePrefix+`:Reason></`+envelopePrefix+`:Fault>`)
	}
	return buildEnvelope(config, version, `<`+envelopePrefix+`:Fault><`+envelopePrefix+`:Code><`+envelopePrefix+`:Value>`+envelopePrefix+`:Sender</`+envelopePrefix+`:Value><`+envelopePrefix+`:Subcode><`+envelopePrefix+`:Value>ter:`+string(fault.Kind)+`</`+envelopePrefix+`:Value></`+envelopePrefix+`:Subcode></`+envelopePrefix+`:Code><`+envelopePrefix+`:Reason><`+envelopePrefix+`:Text xml:lang="en">`+reason+`</`+envelopePrefix+`:Text></`+envelopePrefix+`:Reason></`+envelopePrefix+`:Fault>`)
}

func buildSystemDateAndTime(config normalizedConfig, value time.Time) string {
	prefix := config.OperationPrefix
	value = value.UTC()
	return `<` + prefix + `:GetSystemDateAndTimeResponse><` + prefix + `:SystemDateAndTime><tt:DateTimeType>Manual</tt:DateTimeType><tt:DaylightSavings>false</tt:DaylightSavings><tt:UTCDateTime><tt:Time><tt:Hour>` + strconv.Itoa(value.Hour()) + `</tt:Hour><tt:Minute>` + strconv.Itoa(value.Minute()) + `</tt:Minute><tt:Second>` + strconv.Itoa(value.Second()) + `</tt:Second></tt:Time><tt:Date><tt:Year>` + strconv.Itoa(value.Year()) + `</tt:Year><tt:Month>` + strconv.Itoa(int(value.Month())) + `</tt:Month><tt:Day>` + strconv.Itoa(value.Day()) + `</tt:Day></tt:Date></tt:UTCDateTime></` + prefix + `:SystemDateAndTime></` + prefix + `:GetSystemDateAndTimeResponse>`
}

func buildGetState(_ normalizedConfig, key, value string, present bool) string {
	return `<ovm:GetStateResponse><ovm:Key>` + escapeXML(key) + `</ovm:Key><ovm:Value>` + escapeXML(value) + `</ovm:Value><ovm:Present>` + strconv.FormatBool(present) + `</ovm:Present></ovm:GetStateResponse>`
}

func buildSetState(_ normalizedConfig) string {
	return `<ovm:SetStateResponse/>`
}

func escapeXML(value string) string {
	var output bytes.Buffer
	_ = xml.EscapeText(&output, []byte(value))
	return output.String()
}
