package media

import (
	"context"
	"fmt"

	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// Transport is the SOAP caller seam the Media client depends on. It is
// satisfied by *soap.Client. Defining it as an interface keeps the Media client
// decoupled from transport construction and lets internal/app and tests supply
// their own implementations (for example a recording or fault-injecting
// transport) without a network dependency. It is intentionally the same narrow
// shape as device.Transport.
type Transport interface {
	Call(ctx context.Context, endpoint string, request soap.Request) (*soap.Response, error)
}

// RequestOptions carries optional WS-Security or other SOAP header fragments to
// attach to a Media call. HeaderXML, when set, is passed through to the
// transport unchanged; the auth package produces compatible fragments.
type RequestOptions struct {
	// HeaderXML is an optional SOAP header fragment (for example a WS-Security
	// UsernameToken) applied to the request.
	HeaderXML []byte
}

// Client issues typed ONVIF Media (ver10) operations over a SOAP transport. It
// is safe for concurrent use when its Transport is.
type Client struct {
	transport Transport
	endpoint  string
}

// NewClient constructs a Media client bound to a Media service endpoint. The
// endpoint is validated by the underlying transport on each call; an empty
// endpoint or nil transport is rejected eagerly.
func NewClient(transport Transport, endpoint string) (*Client, error) {
	if transport == nil {
		return nil, fmt.Errorf("media: transport is required")
	}
	if endpoint == "" {
		return nil, fmt.Errorf("media: endpoint is required")
	}
	return &Client{transport: transport, endpoint: endpoint}, nil
}

// Endpoint returns the Media service endpoint this client targets.
func (c *Client) Endpoint() string { return c.endpoint }

// call performs one Media operation and returns the decoded operation payload
// element plus its bounded raw XML. Transport errors (SOAP faults,
// cancellation, HTTP, parse) are returned unchanged so they stay
// errors.Is-compatible with the soap sentinels and context errors.
func (c *Client) call(ctx context.Context, action string, body []byte, opts []RequestOptions) (*element, []byte, error) {
	request := soap.Request{Action: action, BodyXML: body}
	for _, opt := range opts {
		if len(opt.HeaderXML) > 0 {
			request.HeaderXML = opt.HeaderXML
		}
	}
	response, err := c.transport.Call(ctx, c.endpoint, request)
	if err != nil {
		return nil, nil, err
	}
	if response == nil {
		return nil, nil, ErrNilResponse
	}
	root, decodeErr := decodeOperation(response.BodyXML)
	if decodeErr != nil {
		return nil, nil, decodeErr
	}
	return root, response.BodyXML, nil
}

// emptyBody builds a parameterless Media request body for the given operation
// local name, bound to the Media namespace via a stable prefix. The prefix is
// not semantically significant on the wire.
func emptyBody(local string) []byte {
	return []byte(`<trt:` + local + ` xmlns:trt="` + Namespace + `"/>`)
}

// GetProfiles retrieves all media profiles. The ver10 GetProfiles request takes
// no parameters.
func (c *Client) GetProfiles(ctx context.Context, opts ...RequestOptions) ([]Profile, error) {
	root, raw, err := c.call(ctx, ActionGetProfiles, emptyBody("GetProfiles"), opts)
	if err != nil {
		return nil, err
	}
	return decodeProfiles(root, raw)
}

// GetStreamUri retrieves a stream URI for the given profile and stream setup.
// setup selects RTP unicast/multicast and the transport protocol (for example
// RTP-Unicast over RTSP for RTP/TCP, or RTP-Unicast over UDP). profileToken is
// XML-escaped after validation.
func (c *Client) GetStreamUri(ctx context.Context, profileToken string, setup StreamSetup, opts ...RequestOptions) (MediaURI, error) {
	if err := validateStreamSetup(setup); err != nil {
		return MediaURI{}, err
	}
	if err := validateReferenceToken("ProfileToken", profileToken); err != nil {
		return MediaURI{}, err
	}
	body := []byte(`<trt:GetStreamUri xmlns:trt="` + Namespace + `" xmlns:tt="` + SchemaNamespace + `">` +
		`<trt:StreamSetup>` +
		`<tt:Stream>` + string(setup.Stream) + `</tt:Stream>` +
		`<tt:Transport><tt:Protocol>` + string(setup.Protocol) + `</tt:Protocol></tt:Transport>` +
		`</trt:StreamSetup>` +
		`<trt:ProfileToken>` + escapeToken(profileToken) + `</trt:ProfileToken>` +
		`</trt:GetStreamUri>`)
	root, _, err := c.call(ctx, ActionGetStreamURI, body, opts)
	if err != nil {
		return MediaURI{}, err
	}
	return decodeMediaURI(root, "GetStreamUriResponse")
}

// GetSnapshotUri retrieves a snapshot URI for the given profile. profileToken
// is XML-escaped after validation.
func (c *Client) GetSnapshotUri(ctx context.Context, profileToken string, opts ...RequestOptions) (MediaURI, error) {
	if err := validateReferenceToken("ProfileToken", profileToken); err != nil {
		return MediaURI{}, err
	}
	body := []byte(`<trt:GetSnapshotUri xmlns:trt="` + Namespace + `">` +
		`<trt:ProfileToken>` + escapeToken(profileToken) + `</trt:ProfileToken>` +
		`</trt:GetSnapshotUri>`)
	root, _, err := c.call(ctx, ActionGetSnapshotURI, body, opts)
	if err != nil {
		return MediaURI{}, err
	}
	return decodeMediaURI(root, "GetSnapshotUriResponse")
}

// GetVideoSources retrieves the device's physical video sources. This operation
// exists only in the ver10 Media service; the ver20 Media2 service has no
// GetVideoSources operation (see pkg/onvif/media2 doc.go).
func (c *Client) GetVideoSources(ctx context.Context, opts ...RequestOptions) ([]VideoSource, error) {
	root, raw, err := c.call(ctx, ActionGetVideoSources, emptyBody("GetVideoSources"), opts)
	if err != nil {
		return nil, err
	}
	return decodeVideoSources(root, raw)
}

// GetVideoEncoderConfigurations retrieves all video encoder configurations. The
// ver10 request takes no parameters.
func (c *Client) GetVideoEncoderConfigurations(ctx context.Context, opts ...RequestOptions) ([]VideoEncoderConfiguration, error) {
	root, raw, err := c.call(ctx, ActionGetVideoEncoderConfigurations, emptyBody("GetVideoEncoderConfigurations"), opts)
	if err != nil {
		return nil, err
	}
	return decodeVideoEncoderConfigurations(root, raw)
}

// GetVideoEncoderConfigurationOptions retrieves the configuration options. Both
// the configuration token and the profile token are optional per the ver10
// WSDL; an empty string omits the corresponding element. Supplied tokens are
// validated and XML-escaped.
func (c *Client) GetVideoEncoderConfigurationOptions(ctx context.Context, configurationToken, profileToken string, opts ...RequestOptions) (VideoEncoderConfigurationOptions, error) {
	body, err := buildOptionsBody("GetVideoEncoderConfigurationOptions", configurationToken, profileToken)
	if err != nil {
		return VideoEncoderConfigurationOptions{}, err
	}
	root, raw, err := c.call(ctx, ActionGetVideoEncoderConfigurationOptions, body, opts)
	if err != nil {
		return VideoEncoderConfigurationOptions{}, err
	}
	return decodeVideoEncoderConfigurationOptions(root, raw)
}

// GetAudioEncoderConfigurations retrieves all audio encoder configurations. The
// ver10 request takes no parameters.
func (c *Client) GetAudioEncoderConfigurations(ctx context.Context, opts ...RequestOptions) ([]AudioEncoderConfiguration, error) {
	root, raw, err := c.call(ctx, ActionGetAudioEncoderConfigurations, emptyBody("GetAudioEncoderConfigurations"), opts)
	if err != nil {
		return nil, err
	}
	return decodeAudioEncoderConfigurations(root, raw)
}

// buildOptionsBody serializes a ver10 options request with optional
// ConfigurationToken and ProfileToken children, in schema order. Empty tokens
// are omitted; non-empty tokens are validated and XML-escaped.
func buildOptionsBody(operation, configurationToken, profileToken string) ([]byte, error) {
	var inner string
	if configurationToken != "" {
		if err := validateReferenceToken("ConfigurationToken", configurationToken); err != nil {
			return nil, err
		}
		inner += `<trt:ConfigurationToken>` + escapeToken(configurationToken) + `</trt:ConfigurationToken>`
	}
	if profileToken != "" {
		if err := validateReferenceToken("ProfileToken", profileToken); err != nil {
			return nil, err
		}
		inner += `<trt:ProfileToken>` + escapeToken(profileToken) + `</trt:ProfileToken>`
	}
	return []byte(`<trt:` + operation + ` xmlns:trt="` + Namespace + `">` + inner + `</trt:` + operation + `>`), nil
}
