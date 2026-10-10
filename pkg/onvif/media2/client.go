package media2

import (
	"context"
	"fmt"

	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// Transport is the SOAP caller seam the Media2 client depends on. It is
// satisfied by *soap.Client. Defining it as an interface keeps the Media2
// client decoupled from transport construction and lets internal/app and tests
// supply their own implementations without a network dependency. It is
// intentionally the same narrow shape as device.Transport and media.Transport.
type Transport interface {
	Call(ctx context.Context, endpoint string, request soap.Request) (*soap.Response, error)
}

// RequestOptions carries optional WS-Security or other SOAP header fragments to
// attach to a Media2 call. HeaderXML, when set, is passed through to the
// transport unchanged; the auth package produces compatible fragments.
type RequestOptions struct {
	// HeaderXML is an optional SOAP header fragment (for example a WS-Security
	// UsernameToken) applied to the request.
	HeaderXML []byte
}

// Client issues typed ONVIF Media2 (ver20) operations over a SOAP transport. It
// is safe for concurrent use when its Transport is.
type Client struct {
	transport Transport
	endpoint  string
}

// NewClient constructs a Media2 client bound to a Media2 service endpoint. The
// endpoint is validated by the underlying transport on each call; an empty
// endpoint or nil transport is rejected eagerly.
func NewClient(transport Transport, endpoint string) (*Client, error) {
	if transport == nil {
		return nil, fmt.Errorf("media2: transport is required")
	}
	if endpoint == "" {
		return nil, fmt.Errorf("media2: endpoint is required")
	}
	return &Client{transport: transport, endpoint: endpoint}, nil
}

// Endpoint returns the Media2 service endpoint this client targets.
func (c *Client) Endpoint() string { return c.endpoint }

// call performs one Media2 operation and returns the decoded operation payload
// element plus its bounded raw XML. Transport errors are returned unchanged so
// they stay errors.Is-compatible with the soap sentinels and context errors.
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

// GetProfiles retrieves media profiles. The ver20 request is filterable: an
// empty token requests all profiles. An empty types slice omits tr2:Type, so
// the returned profiles contain no configuration information; pass
// ConfigurationAll to request every associated configuration, or pass the
// specific configuration types needed by the caller. A supplied token is
// validated and XML-escaped; supplied types are validated against the
// tr2:ConfigurationEnumeration.
func (c *Client) GetProfiles(ctx context.Context, token string, types []ConfigurationType, opts ...RequestOptions) ([]Profile, error) {
	if err := validateConfigurationTypes(types); err != nil {
		return nil, err
	}
	var inner string
	if token != "" {
		if err := validateReferenceToken("Token", token); err != nil {
			return nil, err
		}
		inner += `<tr2:Token>` + escapeToken(token) + `</tr2:Token>`
	}
	for _, configurationType := range types {
		if err := validateConfigurationType(configurationType); err != nil {
			return nil, err
		}
		inner += `<tr2:Type>` + string(configurationType) + `</tr2:Type>`
	}
	body := []byte(`<tr2:GetProfiles xmlns:tr2="` + Namespace + `">` + inner + `</tr2:GetProfiles>`)
	root, raw, err := c.call(ctx, ActionGetProfiles, body, opts)
	if err != nil {
		return nil, err
	}
	return decodeProfiles(root, raw)
}

// GetStreamUri retrieves a stream URI for the given profile using the Media2
// transport-protocol string (for example RtspUnicast for RTP-over-TCP via
// RTSP, or RtspOverHttp). protocol is validated against tr2:TransportProtocol
// and profileToken is XML-escaped after validation. The ver20 response is a
// bare Uri string, not a tt:MediaUri.
func (c *Client) GetStreamUri(ctx context.Context, protocol StreamProtocol, profileToken string, opts ...RequestOptions) (string, error) {
	if err := validateStreamProtocol(protocol); err != nil {
		return "", err
	}
	if err := validateReferenceToken("ProfileToken", profileToken); err != nil {
		return "", err
	}
	body := []byte(`<tr2:GetStreamUri xmlns:tr2="` + Namespace + `">` +
		`<tr2:Protocol>` + string(protocol) + `</tr2:Protocol>` +
		`<tr2:ProfileToken>` + escapeToken(profileToken) + `</tr2:ProfileToken>` +
		`</tr2:GetStreamUri>`)
	root, _, err := c.call(ctx, ActionGetStreamURI, body, opts)
	if err != nil {
		return "", err
	}
	return decodeURIResponse(root, "GetStreamUriResponse")
}

// GetSnapshotUri retrieves a snapshot URI for the given profile. profileToken is
// XML-escaped after validation. The ver20 response is a bare Uri string.
func (c *Client) GetSnapshotUri(ctx context.Context, profileToken string, opts ...RequestOptions) (string, error) {
	if err := validateReferenceToken("ProfileToken", profileToken); err != nil {
		return "", err
	}
	body := []byte(`<tr2:GetSnapshotUri xmlns:tr2="` + Namespace + `">` +
		`<tr2:ProfileToken>` + escapeToken(profileToken) + `</tr2:ProfileToken>` +
		`</tr2:GetSnapshotUri>`)
	root, _, err := c.call(ctx, ActionGetSnapshotURI, body, opts)
	if err != nil {
		return "", err
	}
	return decodeURIResponse(root, "GetSnapshotUriResponse")
}

// GetVideoEncoderConfigurations retrieves video encoder configurations. Both
// the configuration token and profile token are optional (tr2:GetConfiguration);
// an empty string omits the corresponding element. Supplied tokens are
// validated and XML-escaped.
func (c *Client) GetVideoEncoderConfigurations(ctx context.Context, configurationToken, profileToken string, opts ...RequestOptions) ([]VideoEncoderConfiguration, error) {
	body, err := buildGetConfigurationBody("GetVideoEncoderConfigurations", configurationToken, profileToken)
	if err != nil {
		return nil, err
	}
	root, raw, err := c.call(ctx, ActionGetVideoEncoderConfigurations, body, opts)
	if err != nil {
		return nil, err
	}
	return decodeVideoEncoderConfigurations(root, raw)
}

// GetVideoEncoderConfigurationOptions retrieves video encoder configuration
// options. Both tokens are optional (tr2:GetConfiguration) and are validated
// and XML-escaped when supplied.
func (c *Client) GetVideoEncoderConfigurationOptions(ctx context.Context, configurationToken, profileToken string, opts ...RequestOptions) (VideoEncoderConfigurationOptions, error) {
	body, err := buildGetConfigurationBody("GetVideoEncoderConfigurationOptions", configurationToken, profileToken)
	if err != nil {
		return VideoEncoderConfigurationOptions{}, err
	}
	root, raw, err := c.call(ctx, ActionGetVideoEncoderConfigurationOptions, body, opts)
	if err != nil {
		return VideoEncoderConfigurationOptions{}, err
	}
	return decodeVideoEncoderConfigurationOptions(root, raw)
}

// GetAudioEncoderConfigurations retrieves audio encoder configurations. Both
// tokens are optional (tr2:GetConfiguration) and are validated and XML-escaped
// when supplied.
func (c *Client) GetAudioEncoderConfigurations(ctx context.Context, configurationToken, profileToken string, opts ...RequestOptions) ([]AudioEncoderConfiguration, error) {
	body, err := buildGetConfigurationBody("GetAudioEncoderConfigurations", configurationToken, profileToken)
	if err != nil {
		return nil, err
	}
	root, raw, err := c.call(ctx, ActionGetAudioEncoderConfigurations, body, opts)
	if err != nil {
		return nil, err
	}
	return decodeAudioEncoderConfigurations(root, raw)
}

// buildGetConfigurationBody serializes a ver20 tr2:GetConfiguration request with
// optional ConfigurationToken and ProfileToken children, in schema order. Empty
// tokens are omitted; non-empty tokens are validated and XML-escaped.
func buildGetConfigurationBody(operation, configurationToken, profileToken string) ([]byte, error) {
	var inner string
	if configurationToken != "" {
		if err := validateReferenceToken("ConfigurationToken", configurationToken); err != nil {
			return nil, err
		}
		inner += `<tr2:ConfigurationToken>` + escapeToken(configurationToken) + `</tr2:ConfigurationToken>`
	}
	if profileToken != "" {
		if err := validateReferenceToken("ProfileToken", profileToken); err != nil {
			return nil, err
		}
		inner += `<tr2:ProfileToken>` + escapeToken(profileToken) + `</tr2:ProfileToken>`
	}
	return []byte(`<tr2:` + operation + ` xmlns:tr2="` + Namespace + `">` + inner + `</tr2:` + operation + `>`), nil
}
