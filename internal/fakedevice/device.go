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
	"sync/atomic"
	"time"
)

// RequestRecord is credential-free metadata for one fake-device request.
type RequestRecord struct {
	Version SOAPVersion
	Action  string
}

// Device is a race-safe, stateful fake ONVIF HTTP handler.
type Device struct {
	// current holds the active configuration. Mutating operations publish a
	// modified copy, so a reader always sees one consistent version.
	current atomic.Pointer[normalizedConfig]

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
		state:             cloneStringMap(normalized.InitialState),
		faults:            cloneFaultMap(normalized.Faults),
		unsupported:       cloneBoolMap(normalized.UnsupportedActions),
		seenUsernameNonce: make(map[string]time.Time),
		digestNC:          make(map[string]uint64),
	}
	device.current.Store(&normalized)
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

// cfg returns the active configuration. Callers must not modify it.
func (d *Device) cfg() *normalizedConfig { return d.current.Load() }

// ServeHTTP handles a bounded SOAP request, or the authenticated JPEG snapshot
// GET endpoint. The snapshot endpoint shares the HTTP authentication policy
// (Basic/Digest/none) but never requires a WS-Security UsernameToken, matching
// a real camera's plain HTTP GET snapshot URI.
func (d *Device) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet && d.isSnapshotPath(request.URL.Path) {
		d.serveSnapshot(response, request)
		return
	}
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
	if !d.cfg().supported[version] {
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
	body, tooLarge, err := readRequestBody(request.Body, d.cfg().MaxRequestBytes)
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
	if d.cfg().RequireUsernameToken && !d.validateUsernameToken(usernameTokenFromEnvelope(envelope)) {
		d.writeFault(response, version, FaultSpec{Kind: FaultNotAuthorized, Reason: "UsernameToken authentication failed"})
		return
	}

	// Enforce request-wrapper structural validity (immediate child QName and
	// cardinality) for the official Device and Media wrappers before dispatch,
	// so a wrapper with foreign, unknown, missing, duplicate, or forbidden
	// children is rejected as InvalidArgVal and never reaches a response
	// builder. Synthetic operations keep their own distinct parsing.
	if err := validateDeviceRequestWrapper(action, operation); err != nil {
		d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "Invalid request wrapper children"})
		return
	}
	mediaRequest, err := validateMediaRequestWrapper(action, operation)
	if err != nil {
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

	d.dispatch(response, request, version, action, operation, mediaRequest)
}

// dispatch produces a synthetic response for a validated, authorized action.
func (d *Device) dispatch(response http.ResponseWriter, request *http.Request, version SOAPVersion, action string, operation operationWire, requestData mediaRequest) {
	switch action {
	case ActionGetSystemDateAndTime:
		now := d.cfg().Clock().Add(d.cfg().ClockSkew)
		d.writeSuccess(response, version, buildSystemDateAndTime(*d.cfg(), now))
	case ActionGetDeviceInformation:
		d.writeSuccess(response, version, buildGetDeviceInformation(*d.cfg()))
	case ActionGetCapabilities:
		d.writeSuccess(response, version, buildGetCapabilities(*d.cfg()))
	case ActionGetServices:
		d.writeSuccess(response, version, buildGetServices(*d.cfg()))
	case ActionGetScopes:
		d.writeSuccess(response, version, buildGetScopes(*d.cfg()))
	case ActionGetHostname:
		d.writeSuccess(response, version, buildGetHostname(*d.cfg()))
	case ActionGetNetworkInterfaces:
		d.writeSuccess(response, version, buildGetNetworkInterfaces(*d.cfg()))
	case ActionGetState:
		stateRequest, parseErr := parseStateOperation(operation)
		if parseErr != nil {
			d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "Synthetic state key is required"})
			return
		}
		value, present := d.State(stateRequest.Key)
		d.writeSuccess(response, version, buildGetState(*d.cfg(), stateRequest.Key, value, present))
	case ActionSetState:
		stateRequest, parseErr := parseStateOperation(operation)
		if parseErr != nil {
			d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "Synthetic state key is required"})
			return
		}
		d.SetState(stateRequest.Key, stateRequest.Value)
		d.writeSuccess(response, version, buildSetState(*d.cfg()))
	default:
		d.dispatchMedia(response, request, version, action, requestData)
	}
}

// dispatchMedia produces a synthetic response for a validated, authorized Media
// ver10 or ver20 action. Stream and snapshot URIs are derived from the live
// request origin so they always point back at the serving host.
func (d *Device) dispatchMedia(response http.ResponseWriter, request *http.Request, version SOAPVersion, action string, requestData mediaRequest) {
	if !mediaReferencesExist(d.cfg().MediaInventory, action, requestData) {
		d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "Unknown media reference token"})
		return
	}
	origin := originFromRequest(request)
	snapshotPath := d.cfg().MediaInventory.SnapshotPath
	snapshotQuery := d.cfg().MediaInventory.SnapshotQuery
	streamPath := d.cfg().MediaInventory.StreamPath
	rtspPort := d.cfg().MediaInventory.RTSPPort
	switch action {
	case ActionMedia1GetProfiles:
		d.writeMediaSuccess(response, version, mediaVer10, buildMediaProfiles(*d.cfg(), mediaVer10, requestData))
	case ActionMedia2GetProfiles:
		d.writeMediaSuccess(response, version, mediaVer20, buildMediaProfiles(*d.cfg(), mediaVer20, requestData))
	case ActionMedia1GetVideoSources:
		d.writeMediaSuccess(response, version, mediaVer10, buildVideoSources(*d.cfg()))
	case ActionMedia1GetVideoEncoderConfigurations:
		d.writeMediaSuccess(response, version, mediaVer10, buildVideoEncoderConfigurations(*d.cfg(), mediaVer10, requestData))
	case ActionMedia2GetVideoEncoderConfigurations:
		d.writeMediaSuccess(response, version, mediaVer20, buildVideoEncoderConfigurations(*d.cfg(), mediaVer20, requestData))
	case ActionMedia1GetVideoEncoderConfigurationOptions:
		d.writeMediaSuccess(response, version, mediaVer10, buildVideoEncoderOptions(*d.cfg(), mediaVer10))
	case ActionMedia2GetVideoEncoderConfigurationOptions:
		d.writeMediaSuccess(response, version, mediaVer20, buildVideoEncoderOptions(*d.cfg(), mediaVer20))
	case ActionMedia1GetAudioEncoderConfigurations:
		d.writeMediaSuccess(response, version, mediaVer10, buildAudioEncoderConfigurations(*d.cfg(), mediaVer10, requestData))
	case ActionMedia2GetAudioEncoderConfigurations:
		d.writeMediaSuccess(response, version, mediaVer20, buildAudioEncoderConfigurations(*d.cfg(), mediaVer20, requestData))
	case ActionMedia1GetStreamURI:
		d.writeMediaSuccess(response, version, mediaVer10, buildStreamURIResponse(mediaVer10, origin, streamPath, rtspPort, requestData))
	case ActionMedia2GetStreamURI:
		d.writeMediaSuccess(response, version, mediaVer20, buildStreamURIResponse(mediaVer20, origin, streamPath, rtspPort, requestData))
	case ActionMedia1GetSnapshotURI:
		d.writeMediaSuccess(response, version, mediaVer10, buildSnapshotURIResponse(mediaVer10, origin, snapshotPath, snapshotQuery, requestData))
	case ActionMedia2GetSnapshotURI:
		d.writeMediaSuccess(response, version, mediaVer20, buildSnapshotURIResponse(mediaVer20, origin, snapshotPath, snapshotQuery, requestData))
	default:
		d.writeFault(response, version, FaultSpec{Kind: FaultActionNotSupported, Reason: "Operation is not implemented by the synthetic device"})
	}
}

func mediaReferencesExist(inventory MediaInventory, action string, request mediaRequest) bool {
	media := mediaVersionForAction(action)
	if request.profileToken != "" {
		if _, ok := findMediaProfile(inventory, media, request.profileToken); !ok {
			return false
		}
	}
	if request.configurationToken == "" {
		return true
	}
	switch action {
	case ActionMedia1GetVideoEncoderConfigurationOptions:
		_, ok := findMedia1VideoEncoder(inventory, request.configurationToken)
		return ok
	case ActionMedia2GetVideoEncoderConfigurations,
		ActionMedia2GetVideoEncoderConfigurationOptions:
		_, ok := findMedia2VideoEncoder(inventory, request.configurationToken)
		return ok
	case ActionMedia2GetAudioEncoderConfigurations:
		_, ok := findMedia2AudioEncoder(inventory, request.configurationToken)
		return ok
	default:
		return false
	}
}

// isSnapshotPath reports whether path targets the synthetic JPEG endpoint. The
// comparison is on the exact configured path so an unrelated GET falls through
// to the method-not-allowed branch.
func (d *Device) isSnapshotPath(path string) bool {
	return path == d.cfg().MediaInventory.SnapshotPath
}

// serveSnapshot serves the synthetic JPEG under the HTTP authentication policy.
// It records a credential-free request entry, enforces Basic/Digest/none exactly
// as the SOAP endpoint does, and never requires a WS-Security UsernameToken: a
// snapshot URI is a plain authenticated GET.
func (d *Device) serveSnapshot(response http.ResponseWriter, request *http.Request) {
	// Record only a fixed marker. Neither the configured path, query, profile
	// token, nor Authorization header can enter request history.
	d.recordRequest(RequestRecord{Action: snapshotRecordAction})
	if !d.authorizeHTTP(response, request) {
		return
	}
	response.Header().Set("Content-Type", "image/jpeg")
	response.Header().Set("Content-Length", strconv.Itoa(len(d.cfg().JPEGSnapshot)))
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(d.cfg().JPEGSnapshot)
}

// snapshotRecordAction labels snapshot GETs in the credential-free request
// history. It is a synthetic marker, not a protocol action URI.
const snapshotRecordAction = "urn:ovm:synthetic:SnapshotGet:"

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
	if !secureEqual(username, d.cfg().Username) || passwordDigest == "" || nonceText == "" || createdText == "" {
		return false
	}
	if token.Password.Type != passwordDigestURI || token.Nonce.EncodingType != base64BinaryURI {
		return false
	}
	rawNonce, err := base64.StdEncoding.DecodeString(nonceText)
	if err != nil || len(rawNonce) < 16 {
		return false
	}
	created, ok := parseUsernameTokenCreated(createdText, d.cfg().UsernameTokenStrictCreated)
	if !ok {
		return false
	}
	deviceNow := d.cfg().Clock().Add(d.cfg().ClockSkew).UTC()
	difference := deviceNow.Sub(created)
	if difference < 0 {
		difference = -difference
	}
	if difference > d.cfg().UsernameTokenWindow {
		return false
	}

	// PasswordDigest = Base64( SHA1( nonce + createdBytes + password ) ), using
	// the exact transmitted Created bytes so a semantically valid offset or
	// fractional-second timestamp verifies against the digest the client built.
	digestInput := make([]byte, 0, len(rawNonce)+len(createdText)+len(d.cfg().Password))
	digestInput = append(digestInput, rawNonce...)
	digestInput = append(digestInput, createdText...)
	digestInput = append(digestInput, d.cfg().Password...)
	digest := sha1.Sum(digestInput) // #nosec G401 -- mandated by the WS-Security PasswordDigest formula.
	expected := base64.StdEncoding.EncodeToString(digest[:])
	if subtle.ConstantTimeCompare([]byte(passwordDigest), []byte(expected)) != 1 {
		return false
	}

	nonceKey := base64.StdEncoding.EncodeToString(rawNonce)
	d.mu.Lock()
	defer d.mu.Unlock()
	cutoff := deviceNow.Add(-d.cfg().UsernameTokenWindow)
	for seenNonce, seenAt := range d.seenUsernameNonce {
		if seenAt.Before(cutoff) {
			delete(d.seenUsernameNonce, seenNonce)
		}
	}
	if _, replayed := d.seenUsernameNonce[nonceKey]; replayed || len(d.seenUsernameNonce) >= d.cfg().MaxAuthStateEntries {
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
	if d.cfg().VersionRejection == RejectWithVersionMismatch {
		d.writeFault(response, version, FaultSpec{Kind: FaultVersionMismatch, Reason: "SOAP version is not supported"})
		return
	}
	response.WriteHeader(http.StatusUnsupportedMediaType)
}

func (d *Device) writeSuccess(response http.ResponseWriter, version SOAPVersion, payload string) {
	setResponseContentType(response.Header(), version)
	response.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(response, buildEnvelope(*d.cfg(), version, payload))
}

// writeMediaSuccess writes a media payload wrapped in an envelope that binds the
// media operation prefix to the correct ver10/ver20 namespace.
func (d *Device) writeMediaSuccess(response http.ResponseWriter, version SOAPVersion, media mediaVersion, payload string) {
	setResponseContentType(response.Header(), version)
	response.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(response, buildMediaEnvelope(*d.cfg(), version, media, payload))
}

func (d *Device) writeFault(response http.ResponseWriter, version SOAPVersion, fault FaultSpec) {
	setResponseContentType(response.Header(), version)
	response.WriteHeader(http.StatusInternalServerError)
	_, _ = io.WriteString(response, buildFault(*d.cfg(), version, fault))
}

func (d *Device) recordRequest(record RequestRecord) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.requests) >= d.cfg().MaxRequestHistory {
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
