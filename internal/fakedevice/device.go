package fakedevice

import (
	"crypto/sha1" // #nosec G505 -- WS-Security UsernameToken PasswordDigest requires SHA-1 by specification.
	"crypto/subtle"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RequestRecord is credential-free metadata for one fake-device request.
type RequestRecord struct {
	Version SOAPVersion
	Action  string
}

// Device is a race-safe, stateful fake ONVIF HTTP handler.
type Device struct {
	config normalizedConfig

	mu                sync.RWMutex
	state             map[string]string
	faults            map[string]FaultSpec
	unsupported       map[string]bool
	requests          []RequestRecord
	seenUsernameNonce map[string]time.Time
	digestNonce       string
	digestNC          map[string]uint64
	digestStaleIssued bool
}

// New creates a fake device. Configuration maps are copied.
func New(config Config) (*Device, error) {
	normalized, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	device := &Device{
		config:            normalized,
		state:             cloneStringMap(normalized.InitialState),
		faults:            cloneFaultMap(normalized.Faults),
		unsupported:       cloneBoolMap(normalized.UnsupportedActions),
		seenUsernameNonce: make(map[string]time.Time),
		digestNC:          make(map[string]uint64),
	}
	// A Digest nonce is only required when HTTP Digest authentication is
	// active. Generating one unconditionally would waste entropy and imply a
	// challenge the device never issues.
	if normalized.HTTPAuth == HTTPAuthDigest {
		nonce := normalized.DigestNonce
		if nonce == "" {
			nonce, err = randomDigestNonce()
			if err != nil {
				return nil, err
			}
		}
		device.digestNonce = nonce
	}
	return device, nil
}

// ServeHTTP handles a bounded SOAP request.
func (d *Device) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	version, action, err := requestProtocol(request)
	if err != nil {
		response.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	if !d.config.supported[version] {
		d.recordRequest(RequestRecord{Version: version, Action: safeAction(action)})
		d.rejectVersion(response, version)
		return
	}
	if !d.authorizeHTTP(response, request) {
		return
	}
	defer func() {
		_ = request.Body.Close()
	}()
	body, tooLarge, err := readRequestBody(request.Body, d.config.MaxRequestBytes)
	if err != nil {
		response.WriteHeader(http.StatusBadRequest)
		return
	}
	if tooLarge {
		response.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	envelope, err := parseRequestEnvelope(body, version)
	if err != nil {
		d.recordRequest(RequestRecord{Version: version, Action: safeAction(action)})
		d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "Malformed SOAP request"})
		return
	}
	operation := envelope.Body.Operations[0]
	bodyAction, actionErr := operationAction(operation)
	if actionErr != nil {
		// The body operation QName is unknown or in a foreign namespace. Reject
		// it rather than dispatching on a SOAP header action, which would allow
		// header-only dispatch of an unmodeled operation.
		d.recordRequest(RequestRecord{Version: version, Action: safeAction(action)})
		d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "Unknown or foreign operation"})
		return
	}
	if action != "" && action != bodyAction {
		d.recordRequest(RequestRecord{Version: version, Action: safeAction(action)})
		d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "SOAP action does not match the operation"})
		return
	}
	// The dispatched action is always the validated body operation action.
	action = bodyAction
	d.recordRequest(RequestRecord{Version: version, Action: safeAction(action)})
	if d.config.RequireUsernameToken && !d.validateUsernameToken(usernameTokenFromEnvelope(envelope)) {
		d.writeFault(response, version, FaultSpec{Kind: FaultNotAuthorized, Reason: "UsernameToken authentication failed"})
		return
	}

	// Enforce request-wrapper structural validity (immediate child QName and
	// cardinality) for the official Device wrappers before dispatch, so a
	// wrapper with foreign, unknown, missing, duplicate, or forbidden children
	// is rejected as InvalidArgVal and never reaches a response builder.
	// Synthetic operations keep their own distinct parsing and are untouched.
	if err := validateDeviceRequestWrapper(action, operation); err != nil {
		d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "Invalid request wrapper children"})
		return
	}

	d.mu.RLock()
	fault, hasFault := d.faults[action]
	unsupported := d.unsupported[action]
	d.mu.RUnlock()
	if hasFault {
		d.writeFault(response, version, fault)
		return
	}
	if unsupported {
		d.writeFault(response, version, FaultSpec{Kind: FaultActionNotSupported, Reason: "Operation is configured as unsupported"})
		return
	}

	d.dispatch(response, version, action, operation)
}

// dispatch produces a synthetic response for a validated, authorized action.
func (d *Device) dispatch(response http.ResponseWriter, version SOAPVersion, action string, operation operationWire) {
	switch action {
	case ActionGetSystemDateAndTime:
		now := d.config.Clock().Add(d.config.ClockSkew)
		d.writeSuccess(response, version, buildSystemDateAndTime(d.config, now))
	case ActionGetDeviceInformation:
		d.writeSuccess(response, version, buildGetDeviceInformation(d.config))
	case ActionGetCapabilities:
		d.writeSuccess(response, version, buildGetCapabilities(d.config))
	case ActionGetServices:
		d.writeSuccess(response, version, buildGetServices(d.config))
	case ActionGetScopes:
		d.writeSuccess(response, version, buildGetScopes(d.config))
	case ActionGetHostname:
		d.writeSuccess(response, version, buildGetHostname(d.config))
	case ActionGetNetworkInterfaces:
		d.writeSuccess(response, version, buildGetNetworkInterfaces(d.config))
	case ActionGetState:
		stateRequest, parseErr := parseStateOperation(operation)
		if parseErr != nil {
			d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "Synthetic state key is required"})
			return
		}
		value, present := d.State(stateRequest.Key)
		d.writeSuccess(response, version, buildGetState(d.config, stateRequest.Key, value, present))
	case ActionSetState:
		stateRequest, parseErr := parseStateOperation(operation)
		if parseErr != nil {
			d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "Synthetic state key is required"})
			return
		}
		d.SetState(stateRequest.Key, stateRequest.Value)
		d.writeSuccess(response, version, buildSetState(d.config))
	default:
		// Unreachable: operationAction only returns modeled actions. Kept as a
		// defensive fault rather than a silent success.
		d.writeFault(response, version, FaultSpec{Kind: FaultActionNotSupported, Reason: "Operation is not implemented by the synthetic device"})
	}
}

// SetState updates a synthetic state value.
func (d *Device) SetState(key, value string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.state[key] = value
}

// State retrieves a synthetic state value.
func (d *Device) State(key string) (string, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	value, ok := d.state[key]
	return value, ok
}

// SetFault configures an action fault.
func (d *Device) SetFault(action string, fault FaultSpec) error {
	if action == "" {
		return errors.New("fake-device fault action is required")
	}
	if err := validateFault(fault); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.faults[action] = fault
	return nil
}

// ClearFault removes an action fault.
func (d *Device) ClearFault(action string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.faults, action)
}

// SetUnsupported changes whether an action returns ActionNotSupported.
func (d *Device) SetUnsupported(action string, unsupported bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if unsupported {
		d.unsupported[action] = true
	} else {
		delete(d.unsupported, action)
	}
}

// Requests returns a copy of credential-free request metadata.
func (d *Device) Requests() []RequestRecord {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return append([]RequestRecord(nil), d.requests...)
}

func (d *Device) validateUsernameToken(token *usernameTokenWire) bool {
	if token == nil || token.XMLName != (xmlName(wsseNamespace, "UsernameToken")) ||
		token.Username.XMLName != (xmlName(wsseNamespace, "Username")) ||
		token.Password.XMLName != (xmlName(wsseNamespace, "Password")) ||
		token.Nonce.XMLName != (xmlName(wsseNamespace, "Nonce")) ||
		token.Created.XMLName != (xmlName(wsuNamespace, "Created")) {
		return false
	}
	username := strings.TrimSpace(token.Username.Value)
	passwordDigest := strings.TrimSpace(token.Password.Value)
	nonceText := strings.TrimSpace(token.Nonce.Value)
	// Created participates byte-for-byte in PasswordDigest and must itself be a
	// valid RFC3339 lexical value, so unlike display-oriented fields it is not
	// whitespace-trimmed or re-serialized before parsing and hashing.
	createdText := token.Created.Value
	if !secureEqual(username, d.config.Username) || passwordDigest == "" || nonceText == "" || createdText == "" {
		return false
	}
	if token.Password.Type != passwordDigestURI || token.Nonce.EncodingType != base64BinaryURI {
		return false
	}
	rawNonce, err := base64.StdEncoding.DecodeString(nonceText)
	if err != nil || len(rawNonce) < 16 {
		return false
	}
	created, ok := parseUsernameTokenCreated(createdText, d.config.UsernameTokenStrictCreated)
	if !ok {
		return false
	}
	deviceNow := d.config.Clock().Add(d.config.ClockSkew).UTC()
	difference := deviceNow.Sub(created)
	if difference < 0 {
		difference = -difference
	}
	if difference > d.config.UsernameTokenWindow {
		return false
	}

	// PasswordDigest = Base64( SHA1( nonce + createdBytes + password ) ), using
	// the exact transmitted Created bytes so a semantically valid offset or
	// fractional-second timestamp verifies against the digest the client built.
	digestInput := make([]byte, 0, len(rawNonce)+len(createdText)+len(d.config.Password))
	digestInput = append(digestInput, rawNonce...)
	digestInput = append(digestInput, createdText...)
	digestInput = append(digestInput, d.config.Password...)
	digest := sha1.Sum(digestInput) // #nosec G401 -- mandated by the WS-Security PasswordDigest formula.
	expected := base64.StdEncoding.EncodeToString(digest[:])
	if subtle.ConstantTimeCompare([]byte(passwordDigest), []byte(expected)) != 1 {
		return false
	}

	nonceKey := base64.StdEncoding.EncodeToString(rawNonce)
	d.mu.Lock()
	defer d.mu.Unlock()
	cutoff := deviceNow.Add(-d.config.UsernameTokenWindow)
	for seenNonce, seenAt := range d.seenUsernameNonce {
		if seenAt.Before(cutoff) {
			delete(d.seenUsernameNonce, seenNonce)
		}
	}
	if _, replayed := d.seenUsernameNonce[nonceKey]; replayed || len(d.seenUsernameNonce) >= d.config.MaxAuthStateEntries {
		return false
	}
	d.seenUsernameNonce[nonceKey] = deviceNow
	return true
}

// parseUsernameTokenCreated parses a WS-Security UsernameToken Created
// timestamp. By default (permissive mode) RFC3339 timestamps accepted by the
// standard parser are allowed, including non-UTC numeric offsets and fractional
// seconds. This covers the interoperable lexical variations exercised by the
// fake. In strict mode only the exact canonical UTC whole-second form
// "YYYY-MM-DDTHH:MM:SSZ" is accepted, for interoperability tests that pin the
// transmitted lexical form.
func parseUsernameTokenCreated(createdText string, strict bool) (time.Time, bool) {
	if strict {
		created, err := time.Parse(time.RFC3339, createdText)
		if err != nil || created.Nanosecond() != 0 || createdText != created.UTC().Format(time.RFC3339) {
			return time.Time{}, false
		}
		return created, true
	}
	created, err := time.Parse(time.RFC3339, createdText)
	if err != nil {
		return time.Time{}, false
	}
	return created, true
}

func (d *Device) rejectVersion(response http.ResponseWriter, version SOAPVersion) {
	if d.config.VersionRejection == RejectWithVersionMismatch {
		d.writeFault(response, version, FaultSpec{Kind: FaultVersionMismatch, Reason: "SOAP version is not supported"})
		return
	}
	response.WriteHeader(http.StatusUnsupportedMediaType)
}

func (d *Device) writeSuccess(response http.ResponseWriter, version SOAPVersion, payload string) {
	setResponseContentType(response.Header(), version)
	response.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(response, buildEnvelope(d.config, version, payload))
}

func (d *Device) writeFault(response http.ResponseWriter, version SOAPVersion, fault FaultSpec) {
	setResponseContentType(response.Header(), version)
	response.WriteHeader(http.StatusInternalServerError)
	_, _ = io.WriteString(response, buildFault(d.config, version, fault))
}

func (d *Device) recordRequest(record RequestRecord) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.requests) >= d.config.MaxRequestHistory {
		copy(d.requests, d.requests[1:])
		d.requests[len(d.requests)-1] = record
		return
	}
	d.requests = append(d.requests, record)
}

func requestProtocol(request *http.Request) (SOAPVersion, string, error) {
	mediaType, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil {
		return "", "", err
	}
	switch strings.ToLower(mediaType) {
	case "application/soap+xml":
		return SOAP12, parameters["action"], nil
	case "text/xml":
		action := strings.TrimSpace(request.Header.Get("SOAPAction"))
		if unquoted, unquoteErr := strconv.Unquote(action); unquoteErr == nil {
			action = unquoted
		}
		return SOAP11, action, nil
	default:
		return "", "", errors.New("unsupported SOAP media type")
	}
}

func readRequestBody(body io.Reader, limit int64) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > limit {
		return data[:limit], true, nil
	}
	return data, false, nil
}

func setResponseContentType(headers http.Header, version SOAPVersion) {
	if version == SOAP11 {
		headers.Set("Content-Type", "text/xml; charset=utf-8")
	} else {
		headers.Set("Content-Type", "application/soap+xml; charset=utf-8")
	}
}

func safeAction(action string) string {
	const maxActionRunes = 512
	parsed, err := url.Parse(action)
	if err == nil {
		if parsed.User != nil {
			parsed.User = url.User("REDACTED")
		}
		if parsed.RawQuery != "" {
			parsed.RawQuery = "REDACTED"
		}
		action = parsed.String()
	}
	runes := []rune(action)
	if len(runes) > maxActionRunes {
		action = string(runes[:maxActionRunes])
	}
	return action
}

func xmlName(namespace, local string) xml.Name {
	return xml.Name{Space: namespace, Local: local}
}

func cloneStringMap(source map[string]string) map[string]string {
	copy := make(map[string]string, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func cloneBoolMap(source map[string]bool) map[string]bool {
	copy := make(map[string]bool, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func cloneFaultMap(source map[string]FaultSpec) map[string]FaultSpec {
	copy := make(map[string]FaultSpec, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}
