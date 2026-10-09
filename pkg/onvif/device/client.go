package device

import (
	"context"
	"fmt"

	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// Transport is the SOAP caller seam the Device client depends on. It is
// satisfied by *soap.Client. Defining it as an interface keeps the Device
// client decoupled from transport construction and lets internal/app and tests
// supply their own implementations (for example a recording or fault-injecting
// transport) without a network dependency.
type Transport interface {
	Call(ctx context.Context, endpoint string, request soap.Request) (*soap.Response, error)
}

// RequestOptions carries optional WS-Security or other SOAP header fragments to
// attach to a Device call. HeaderXML, when set, is passed through to the
// transport unchanged; the auth package produces compatible fragments.
type RequestOptions struct {
	// HeaderXML is an optional SOAP header fragment (for example a WS-Security
	// UsernameToken) applied to the request.
	HeaderXML []byte
}

// Client issues typed ONVIF Device Management operations over a SOAP
// transport. It is safe for concurrent use when its Transport is.
type Client struct {
	transport Transport
	endpoint  string
}

// NewClient constructs a Device client bound to a Device service endpoint. The
// endpoint is validated by the underlying transport on each call; an empty
// endpoint or nil transport is rejected eagerly.
func NewClient(transport Transport, endpoint string) (*Client, error) {
	if transport == nil {
		return nil, fmt.Errorf("device: transport is required")
	}
	if endpoint == "" {
		return nil, fmt.Errorf("device: endpoint is required")
	}
	return &Client{transport: transport, endpoint: endpoint}, nil
}

// Endpoint returns the Device service endpoint this client targets.
func (c *Client) Endpoint() string { return c.endpoint }

// call performs one Device operation and returns the decoded operation payload
// element. Transport errors (SOAP faults, cancellation, HTTP, parse) are
// returned unchanged so they stay errors.Is-compatible with the soap sentinels
// and context errors.
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

// emptyBody builds a parameterless Device request body for the given operation
// local name, bound to the Device namespace via a stable prefix. The prefix is
// not semantically significant on the wire.
func emptyBody(local string) []byte {
	return []byte(`<tds:` + local + ` xmlns:tds="` + Namespace + `"/>`)
}

// GetSystemDateAndTime retrieves and normalizes the device system date/time.
func (c *Client) GetSystemDateAndTime(ctx context.Context, opts ...RequestOptions) (SystemDateAndTime, error) {
	root, _, err := c.call(ctx, ActionGetSystemDateAndTime, emptyBody("GetSystemDateAndTime"), opts)
	if err != nil {
		return SystemDateAndTime{}, err
	}
	return decodeSystemDateAndTime(root)
}

// GetDeviceInformation retrieves manufacturer, model, and firmware identity.
func (c *Client) GetDeviceInformation(ctx context.Context, opts ...RequestOptions) (DeviceInformation, error) {
	root, _, err := c.call(ctx, ActionGetDeviceInformation, emptyBody("GetDeviceInformation"), opts)
	if err != nil {
		return DeviceInformation{}, err
	}
	return decodeDeviceInformation(root)
}

// GetCapabilities retrieves service endpoint capabilities. It requests the All
// category; unmodeled capability detail is preserved as bounded raw XML.
func (c *Client) GetCapabilities(ctx context.Context, opts ...RequestOptions) (Capabilities, error) {
	body := []byte(`<tds:GetCapabilities xmlns:tds="` + Namespace + `"><tds:Category>All</tds:Category></tds:GetCapabilities>`)
	root, raw, err := c.call(ctx, ActionGetCapabilities, body, opts)
	if err != nil {
		return Capabilities{}, err
	}
	return decodeCapabilities(root, raw)
}

// GetServices retrieves the service directory. IncludeCapability is false to
// keep responses small; per-service capability XML is preserved if present.
func (c *Client) GetServices(ctx context.Context, opts ...RequestOptions) ([]Service, error) {
	body := []byte(`<tds:GetServices xmlns:tds="` + Namespace + `"><tds:IncludeCapability>false</tds:IncludeCapability></tds:GetServices>`)
	root, raw, err := c.call(ctx, ActionGetServices, body, opts)
	if err != nil {
		return nil, err
	}
	return decodeServices(root, raw)
}

// GetScopes retrieves the device scope list.
func (c *Client) GetScopes(ctx context.Context, opts ...RequestOptions) ([]Scope, error) {
	root, _, err := c.call(ctx, ActionGetScopes, emptyBody("GetScopes"), opts)
	if err != nil {
		return nil, err
	}
	return decodeScopes(root)
}

// GetHostname retrieves the configured hostname.
func (c *Client) GetHostname(ctx context.Context, opts ...RequestOptions) (Hostname, error) {
	root, _, err := c.call(ctx, ActionGetHostname, emptyBody("GetHostname"), opts)
	if err != nil {
		return Hostname{}, err
	}
	return decodeHostname(root)
}

// GetNetworkInterfaces retrieves network interface configuration.
func (c *Client) GetNetworkInterfaces(ctx context.Context, opts ...RequestOptions) ([]NetworkInterface, error) {
	root, raw, err := c.call(ctx, ActionGetNetworkInterfaces, emptyBody("GetNetworkInterfaces"), opts)
	if err != nil {
		return nil, err
	}
	return decodeNetworkInterfaces(root, raw)
}
