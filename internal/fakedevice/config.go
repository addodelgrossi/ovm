package fakedevice

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// SOAPVersion identifies a SOAP version accepted by the fake device.
type SOAPVersion string

const (
	// SOAP11 is SOAP 1.1.
	SOAP11 SOAPVersion = "1.1"
	// SOAP12 is SOAP 1.2.
	SOAP12 SOAPVersion = "1.2"
)

// VersionRejection controls how an unsupported SOAP version is rejected.
type VersionRejection string

const (
	// RejectWithHTTP415 returns Unsupported Media Type.
	RejectWithHTTP415 VersionRejection = "http_415"
	// RejectWithVersionMismatch returns a SOAP VersionMismatch fault.
	RejectWithVersionMismatch VersionRejection = "version_mismatch"
)

// HTTPAuthMode controls HTTP-level authentication.
type HTTPAuthMode string

const (
	// HTTPAuthNone disables HTTP authentication.
	HTTPAuthNone HTTPAuthMode = "none"
	// HTTPAuthDigest requires HTTP Digest qop=auth.
	HTTPAuthDigest HTTPAuthMode = "digest"
	// HTTPAuthBasic requires HTTP Basic.
	HTTPAuthBasic HTTPAuthMode = "basic"
)

// FaultKind identifies a configurable SOAP fault.
type FaultKind string

const (
	// FaultNotAuthorized is an ONVIF NotAuthorized fault.
	FaultNotAuthorized FaultKind = "NotAuthorized"
	// FaultActionNotSupported is an ONVIF ActionNotSupported fault.
	FaultActionNotSupported FaultKind = "ActionNotSupported"
	// FaultInvalidArgVal is an ONVIF InvalidArgVal fault.
	FaultInvalidArgVal FaultKind = "InvalidArgVal"
	// FaultVersionMismatch is a SOAP VersionMismatch fault.
	FaultVersionMismatch FaultKind = "VersionMismatch"
)

// deviceActionBase is the ONVIF Device Management WSDL namespace. Action URIs
// are formed by appending "/" and the operation name, matching the ONVIF SOAP
// binding and pkg/onvif/device.
const deviceActionBase = "http://www.onvif.org/ver10/device/wsdl"

const (
	// ActionGetSystemDateAndTime is the ONVIF Device Management system-time action.
	ActionGetSystemDateAndTime = deviceActionBase + "/GetSystemDateAndTime"
	// ActionGetDeviceInformation is the ONVIF GetDeviceInformation action.
	ActionGetDeviceInformation = deviceActionBase + "/GetDeviceInformation"
	// ActionGetCapabilities is the ONVIF GetCapabilities action.
	ActionGetCapabilities = deviceActionBase + "/GetCapabilities"
	// ActionGetServices is the ONVIF GetServices action.
	ActionGetServices = deviceActionBase + "/GetServices"
	// ActionGetScopes is the ONVIF GetScopes action.
	ActionGetScopes = deviceActionBase + "/GetScopes"
	// ActionGetHostname is the ONVIF GetHostname action.
	ActionGetHostname = deviceActionBase + "/GetHostname"
	// ActionGetNetworkInterfaces is the ONVIF GetNetworkInterfaces action.
	ActionGetNetworkInterfaces = deviceActionBase + "/GetNetworkInterfaces"

	// ActionGetState is a project-authored synthetic state-read action.
	ActionGetState = "urn:ovm:synthetic:GetState"
	// ActionSetState is a project-authored synthetic state-mutation action.
	ActionSetState = "urn:ovm:synthetic:SetState"
)

const (
	defaultMaxRequestBytes     int64 = 1 << 20
	defaultUsernameTokenWindow       = 5 * time.Minute
	defaultMaxRequestHistory         = 256
	defaultMaxAuthStateEntries       = 1024
	maxInt64                   int64 = int64(^uint64(0) >> 1)
)

var xmlPrefixPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

// reservedResponsePrefixes are the fixed prefixes the synthetic responses bind
// to ONVIF and project namespaces. Configurable envelope and operation prefixes
// must not collide with them, otherwise a single prefix would be bound to two
// different namespaces in one document. The media prefixes trt/tr2 are included
// because media responses bind them to the ver10/ver20 Media namespaces.
var reservedResponsePrefixes = []string{"tt", "ter", "ovm", "trt", "tr2"}

// FaultSpec configures a SOAP fault for an action.
type FaultSpec struct {
	Kind   FaultKind
	Reason string
}

// Config controls fake-device protocol behavior. Nil maps and version slices
// receive safe defaults and are copied by New.
type Config struct {
	SupportedSOAPVersions []SOAPVersion
	VersionRejection      VersionRejection
	EnvelopePrefix        string
	OperationPrefix       string
	MaxRequestBytes       int64
	MaxRequestHistory     int
	MaxAuthStateEntries   int

	RequireUsernameToken bool
	Username             string
	Password             string
	UsernameTokenWindow  time.Duration
	// UsernameTokenStrictCreated selects how a WS-Security UsernameToken
	// Created timestamp is accepted. The zero value is permissive: RFC3339
	// timestamps accepted by the standard parser verify, including non-UTC
	// numeric offsets and fractional seconds. When true, strict mode accepts
	// only the exact canonical UTC whole-second form "YYYY-MM-DDTHH:MM:SSZ",
	// for interoperability tests that pin the transmitted lexical form.
	UsernameTokenStrictCreated bool
	ClockSkew                  time.Duration
	Clock                      func() time.Time

	HTTPAuth        HTTPAuthMode
	DigestAlgorithm string
	DigestRealm     string
	DigestNonce     string
	DigestStaleOnce bool

	// Inventory supplies synthetic device-management inventory data for the
	// typed Device operations. A zero value is filled with defaults.
	Inventory Inventory

	// MediaInventory supplies synthetic Media ver10 and ver20 data. A zero
	// value is filled with defaults.
	MediaInventory MediaInventory

	// Settings is the mutable device-management state (time, NTP, users,
	// gateway, DNS, reboot, and logs). A zero value receives defaults.
	Settings DeviceSettings

	// Imaging supplies synthetic Imaging ver20 settings. A zero value is
	// filled with one AUTO video source.
	Imaging ImagingInventory

	// JPEGSnapshot is the exact synthetic JPEG served by the authenticated GET
	// snapshot endpoint. When empty, a project-owned synthetic default is used.
	// It must be project-authored bytes, never a captured image.
	JPEGSnapshot []byte

	UnsupportedActions map[string]bool
	Faults             map[string]FaultSpec
	InitialState       map[string]string
}

type normalizedConfig struct {
	Config
	supported map[SOAPVersion]bool
}

func normalizeConfig(config Config) (normalizedConfig, error) {
	if len(config.SupportedSOAPVersions) == 0 {
		config.SupportedSOAPVersions = []SOAPVersion{SOAP12, SOAP11}
	}
	supported := make(map[SOAPVersion]bool, len(config.SupportedSOAPVersions))
	for _, version := range config.SupportedSOAPVersions {
		if version != SOAP11 && version != SOAP12 {
			return normalizedConfig{}, fmt.Errorf("unsupported fake-device SOAP version %q", version)
		}
		supported[version] = true
	}
	if config.VersionRejection == "" {
		config.VersionRejection = RejectWithHTTP415
	}
	if config.VersionRejection != RejectWithHTTP415 && config.VersionRejection != RejectWithVersionMismatch {
		return normalizedConfig{}, errors.New("invalid fake-device SOAP version rejection policy")
	}
	if config.EnvelopePrefix == "" {
		config.EnvelopePrefix = "env"
	}
	if config.OperationPrefix == "" {
		config.OperationPrefix = "tds"
	}
	if err := validatePrefix(config.EnvelopePrefix); err != nil {
		return normalizedConfig{}, fmt.Errorf("envelope prefix: %w", err)
	}
	if err := validatePrefix(config.OperationPrefix); err != nil {
		return normalizedConfig{}, fmt.Errorf("operation prefix: %w", err)
	}
	if config.EnvelopePrefix == config.OperationPrefix {
		return normalizedConfig{}, errors.New("fake-device XML prefixes must be distinct")
	}
	// The envelope and operation prefixes must not collide with the fixed
	// tt/ter/ovm/trt/tr2 prefixes bound by every synthetic response, or one
	// prefix would be declared for two namespaces in a single document.
	for _, prefix := range reservedResponsePrefixes {
		if config.EnvelopePrefix == prefix || config.OperationPrefix == prefix {
			return normalizedConfig{}, fmt.Errorf("fake-device prefix %q collides with a reserved response prefix", prefix)
		}
	}
	if config.MaxRequestBytes == 0 {
		config.MaxRequestBytes = defaultMaxRequestBytes
	}
	if config.MaxRequestBytes < 1 || config.MaxRequestBytes == maxInt64 {
		return normalizedConfig{}, errors.New("fake-device request limit must be positive and bounded")
	}
	if config.MaxRequestHistory == 0 {
		config.MaxRequestHistory = defaultMaxRequestHistory
	}
	if config.MaxRequestHistory < 1 {
		return normalizedConfig{}, errors.New("fake-device request history limit must be positive")
	}
	if config.MaxAuthStateEntries == 0 {
		config.MaxAuthStateEntries = defaultMaxAuthStateEntries
	}
	if config.MaxAuthStateEntries < 1 {
		return normalizedConfig{}, errors.New("fake-device authentication state limit must be positive")
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.UsernameTokenWindow == 0 {
		config.UsernameTokenWindow = defaultUsernameTokenWindow
	}
	if config.UsernameTokenWindow < 0 {
		return normalizedConfig{}, errors.New("UsernameToken window cannot be negative")
	}
	if config.HTTPAuth == "" {
		config.HTTPAuth = HTTPAuthNone
	}
	if config.HTTPAuth != HTTPAuthNone && config.HTTPAuth != HTTPAuthDigest && config.HTTPAuth != HTTPAuthBasic {
		return normalizedConfig{}, errors.New("invalid fake-device HTTP authentication mode")
	}
	if (config.RequireUsernameToken || config.HTTPAuth != HTTPAuthNone) && config.Username == "" {
		return normalizedConfig{}, errors.New("fake-device authenticated mode requires a username")
	}
	if config.DigestAlgorithm == "" {
		config.DigestAlgorithm = "SHA-256"
	}
	config.DigestAlgorithm = strings.ToUpper(config.DigestAlgorithm)
	if config.DigestAlgorithm != "MD5" && config.DigestAlgorithm != "SHA-256" {
		return normalizedConfig{}, errors.New("fake-device Digest algorithm must be MD5 or SHA-256")
	}
	if config.DigestRealm == "" {
		config.DigestRealm = "ovm-synthetic-device"
	}
	// A Digest nonce is only meaningful when HTTP Digest is the active mode. A
	// configured nonce under a non-Digest mode is a configuration error so the
	// field cannot be set with no effect.
	if config.HTTPAuth != HTTPAuthDigest && config.DigestNonce != "" {
		return normalizedConfig{}, errors.New("fake-device Digest nonce requires HTTP Digest authentication")
	}
	config.Inventory = config.Inventory.withDefaults()
	if err := validateInventoryXSIntFields(config.Inventory); err != nil {
		return normalizedConfig{}, err
	}
	config.Imaging = config.Imaging.withDefaults()
	config.Settings = config.Settings.withDefaults(config.Username, config.Password)
	config.MediaInventory = config.MediaInventory.withDefaults()
	if err := validateMediaInventory(config.MediaInventory); err != nil {
		return normalizedConfig{}, err
	}
	// The snapshot JPEG is copied so the device owns its bytes independently of
	// the caller's slice and cannot race with later caller mutation. An empty
	// configuration receives the project-owned synthetic default.
	if len(config.JPEGSnapshot) == 0 {
		config.JPEGSnapshot = append([]byte(nil), syntheticJPEG...)
	} else {
		config.JPEGSnapshot = append([]byte(nil), config.JPEGSnapshot...)
	}
	if err := validateSyntheticJPEG(config.JPEGSnapshot); err != nil {
		return normalizedConfig{}, err
	}
	for action, fault := range config.Faults {
		if action == "" {
			return normalizedConfig{}, errors.New("fake-device fault action cannot be empty")
		}
		if err := validateFault(fault); err != nil {
			return normalizedConfig{}, err
		}
	}
	return normalizedConfig{Config: config, supported: supported}, nil
}

func validatePrefix(prefix string) error {
	if !xmlPrefixPattern.MatchString(prefix) || strings.EqualFold(prefix, "xml") || strings.EqualFold(prefix, "xmlns") {
		return errors.New("invalid XML namespace prefix")
	}
	return nil
}

func validateFault(fault FaultSpec) error {
	switch fault.Kind {
	case FaultNotAuthorized, FaultActionNotSupported, FaultInvalidArgVal, FaultVersionMismatch:
		return nil
	default:
		return errors.New("invalid fake-device fault kind")
	}
}
