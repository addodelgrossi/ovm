package app

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/addodelgrossi/ovm/pkg/onvif/device"
)

// NetworkView is the IPv4 configuration of the camera's primary interface.
type NetworkView struct {
	InterfaceToken string   `json:"interface_token"`
	InterfaceName  string   `json:"interface_name"`
	HwAddress      string   `json:"hw_address"`
	DHCP           bool     `json:"dhcp"`
	Address        string   `json:"address"`
	PrefixLength   int      `json:"prefix_length"`
	Gateways       []string `json:"gateways"`
	DNSFromDHCP    bool     `json:"dns_from_dhcp"`
	DNSServers     []string `json:"dns_servers"`
}

// NetworkChange is the requested IPv4 configuration. With DHCP, Address,
// PrefixLength, and Gateway are ignored and DNS also comes from DHCP.
type NetworkChange struct {
	DHCP         bool     `json:"dhcp"`
	Address      string   `json:"address"`
	PrefixLength int      `json:"prefix_length"`
	Gateway      string   `json:"gateway"`
	DNSServers   []string `json:"dns_servers"`
}

// NetworkApplyResult is the result of a network change. NewEndpoint is the
// Device service URL to use from now on when the address changed; it is
// empty when the address is unchanged or unknown (DHCP).
type NetworkApplyResult struct {
	Before          NetworkView    `json:"before"`
	After           *NetworkView   `json:"after,omitempty"`
	Report          MutationReport `json:"report"`
	RebootRequested bool           `json:"reboot_requested"`
	NewEndpoint     string         `json:"new_endpoint,omitempty"`
}

// NetworkUseCase reads and changes IPv4 network settings.
type NetworkUseCase interface {
	ReadNetwork(ctx context.Context, request DeviceRequest) (NetworkView, error)
	ApplyNetwork(ctx context.Context, request DeviceRequest, change NetworkChange) (NetworkApplyResult, error)
}

// NewProductionNetwork returns the production NetworkUseCase.
func NewProductionNetwork() NetworkUseCase { return &productionMedia{} }

// networkReadback waits for a camera that may reboot or move to a new
// address.
var networkReadback = readbackPolicy{attempts: 15, delay: 3 * time.Second}

func (p *productionMedia) networkPolicy() readbackPolicy {
	if p.readbackPolicy != nil {
		return *p.readbackPolicy
	}
	return networkReadback
}

func (p *productionMedia) readNetwork(ctx context.Context, seam *authDeviceSeam) (NetworkView, error) {
	interfaces, err := seam.NetworkInterfaces(ctx)
	if err != nil {
		return NetworkView{}, err
	}
	index := slices.IndexFunc(interfaces, func(i device.NetworkInterface) bool { return i.Enabled && i.HasIPv4 })
	if index < 0 {
		return NetworkView{}, newError(CategoryProtocol, "the camera reports no enabled IPv4 interface", nil)
	}
	primary := interfaces[index]
	view := NetworkView{
		InterfaceToken: primary.Token, InterfaceName: primary.Name, HwAddress: primary.HwAddress,
		DHCP: primary.DHCP, Gateways: []string{}, DNSServers: []string{},
	}
	if len(primary.IPv4Addresses) > 0 {
		view.Address, view.PrefixLength = primary.IPv4Addresses[0].Address, primary.IPv4Addresses[0].PrefixLength
	}
	// Each call gets its own UsernameToken; a nonce must never be reused.
	err = seam.call(ctx, func(ctx context.Context, opts []device.RequestOptions) error {
		gateways, callErr := seam.client.GetNetworkDefaultGateway(ctx, opts...)
		view.Gateways = gateways
		return callErr
	})
	if err != nil {
		return NetworkView{}, err
	}
	var dns device.DNSInformation
	err = seam.call(ctx, func(ctx context.Context, opts []device.RequestOptions) error {
		var callErr error
		dns, callErr = seam.client.GetDNS(ctx, opts...)
		return callErr
	})
	if err != nil {
		return NetworkView{}, err
	}
	view.DNSFromDHCP, view.DNSServers = dns.FromDHCP, dns.Manual
	if dns.FromDHCP {
		view.DNSServers = dns.FromDHCPAddrs
	}
	if view.DNSServers == nil {
		view.DNSServers = []string{}
	}
	return view, nil
}

// ReadNetwork returns the primary interface configuration.
func (p *productionMedia) ReadNetwork(ctx context.Context, request DeviceRequest) (NetworkView, error) {
	seam, opCtx, cancel, err := p.openDevice(ctx, request)
	if err != nil {
		return NetworkView{}, err
	}
	defer cancel()
	view, err := p.readNetwork(opCtx, seam)
	if err != nil {
		return NetworkView{}, mapConfigError(err)
	}
	return view, nil
}

func validateNetworkChange(change NetworkChange) error {
	if len(change.DNSServers) > 3 {
		return usageError("at most three DNS servers are supported")
	}
	for _, server := range change.DNSServers {
		if address, err := netip.ParseAddr(server); err != nil || !usableHost(address) {
			return usageError("a DNS server address is not valid")
		}
	}
	if change.DHCP {
		return nil
	}
	address, err := netip.ParseAddr(change.Address)
	if err != nil || !usableHost(address) {
		return usageError("the IPv4 address is not a usable host address")
	}
	if change.PrefixLength < 8 || change.PrefixLength > 30 {
		return usageError("the prefix length must be between 8 and 30")
	}
	prefix := netip.PrefixFrom(address, change.PrefixLength).Masked()
	if address == prefix.Addr() {
		return usageError("the IPv4 address is the network address")
	}
	if change.Gateway != "" {
		gateway, err := netip.ParseAddr(change.Gateway)
		if err != nil || !usableHost(gateway) || !prefix.Contains(gateway) || gateway == address {
			return usageError("the gateway must be another host on the same subnet")
		}
	}
	return nil
}

func usableHost(address netip.Addr) bool {
	return address.Is4() && !address.IsUnspecified() && !address.IsLoopback() && !address.IsMulticast() &&
		!address.IsLinkLocalUnicast() && address != netip.AddrFrom4([4]byte{255, 255, 255, 255})
}

// ApplyNetwork changes DNS, gateway, and the interface address, reboots when
// the camera asks for it, and reads the result back at the new address.
func (p *productionMedia) ApplyNetwork(ctx context.Context, request DeviceRequest, change NetworkChange) (NetworkApplyResult, error) {
	if err := validateNetworkChange(change); err != nil {
		return NetworkApplyResult{}, err
	}
	seam, opCtx, cancel, err := p.openDevice(ctx, request)
	if err != nil {
		return NetworkApplyResult{}, err
	}
	defer cancel()
	before, err := p.readNetwork(opCtx, seam)
	if err != nil {
		return NetworkApplyResult{}, mapConfigError(err)
	}
	result := NetworkApplyResult{Before: before}

	// DNS and gateway change first: once the address moves, the old one may
	// stop answering.
	err = seam.call(opCtx, func(ctx context.Context, opts []device.RequestOptions) error {
		return seam.client.SetDNS(ctx, change.DHCP && len(change.DNSServers) == 0, change.DNSServers, opts...)
	})
	if err == nil && !change.DHCP && change.Gateway != "" {
		err = seam.call(opCtx, func(ctx context.Context, opts []device.RequestOptions) error {
			return seam.client.SetNetworkDefaultGateway(ctx, []string{change.Gateway}, opts...)
		})
	}
	if err != nil {
		return result, mapSetError(err, "the camera rejected the DNS or gateway settings")
	}
	var rebootNeeded bool
	err = seam.call(opCtx, func(ctx context.Context, opts []device.RequestOptions) error {
		var callErr error
		rebootNeeded, callErr = seam.client.SetNetworkInterfaces(ctx, before.InterfaceToken, device.IPv4Change{
			DHCP: change.DHCP, Address: change.Address, PrefixLength: change.PrefixLength,
		}, opts...)
		return callErr
	})
	if err != nil {
		// A camera that already moved can drop the connection before replying.
		if !isNetworkError(err) {
			return result, mapSetError(err, "the camera rejected the network settings")
		}
	}
	if rebootNeeded {
		result.RebootRequested = true
		_ = seam.call(opCtx, func(ctx context.Context, opts []device.RequestOptions) error {
			_, rebootErr := seam.client.SystemReboot(ctx, opts...)
			return rebootErr
		})
	}

	target := request
	if !change.DHCP && change.Address != before.Address {
		endpoint, err := movedEndpoint(request.Host, change.Address)
		if err != nil {
			return result, err
		}
		target.Host, result.NewEndpoint = endpoint, endpoint
	}
	requested := networkFields(NetworkView{
		DHCP: change.DHCP, Address: change.Address, PrefixLength: change.PrefixLength,
		Gateways: []string{change.Gateway}, DNSServers: change.DNSServers,
	}, change)
	var after NetworkView
	afterFields, ok := readBack(ctx, p.networkPolicy(), requested, func(ctx context.Context) (fieldValues, error) {
		readSeam, readCtx, readCancel, err := p.openDevice(ctx, target)
		if err != nil {
			return fieldValues{}, err
		}
		defer readCancel()
		current, err := p.readNetwork(readCtx, readSeam)
		if err != nil {
			return fieldValues{}, err
		}
		after = current
		return networkFields(current, change), nil
	})
	if !ok {
		result.Report = MutationReport{Outcome: OutcomeDisconnected, Changes: []FieldChange{}}
		return result, nil
	}
	result.After = &after
	result.Report = compareMutation(networkFields(before, change), requested, afterFields)
	return result, nil
}

func networkFields(view NetworkView, change NetworkChange) fieldValues {
	fields := newFieldValues()
	fields.set("dhcp", strconv.FormatBool(view.DHCP))
	if !change.DHCP {
		fields.set("address", view.Address+"/"+strconv.Itoa(view.PrefixLength))
		if change.Gateway != "" {
			fields.set("gateway", strings.Join(view.Gateways, ", "))
		}
	}
	if len(change.DNSServers) > 0 {
		fields.set("dns_servers", strings.Join(view.DNSServers, ", "))
	}
	return fields
}

// movedEndpoint rewrites the Device service URL to a new IPv4 address,
// keeping scheme, port, and path.
func movedEndpoint(host, address string) (string, error) {
	endpoint, err := canonicalizeOperationalDeviceEndpoint(host)
	if err != nil {
		return "", usageError("the endpoint is not valid")
	}
	if port := endpoint.Port(); port != "" {
		endpoint.Host = net.JoinHostPort(address, port)
	} else {
		endpoint.Host = address
	}
	endpoint.RawQuery = ""
	return endpoint.String(), nil
}
