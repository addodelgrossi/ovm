package discovery

import (
	"context"
	"errors"
	"net"
	"slices"
	"testing"
)

// addrErrorSource wraps a fakeInterfaceSource but fails Addrs for one named
// interface, modeling a flaky interface whose addresses cannot be read.
type addrErrorSource struct {
	fakeInterfaceSource
	failFor string
	addrErr error
}

func (s addrErrorSource) Addrs(iface net.Interface) ([]net.Addr, error) {
	if iface.Name == s.failFor {
		return nil, s.addrErr
	}
	return s.fakeInterfaceSource.Addrs(iface)
}

// reasonByName indexes an assessment by interface name for concise assertions.
func reasonByName(hosts []HostInterface) map[string]HostInterface {
	out := make(map[string]HostInterface, len(hosts))
	for _, h := range hosts {
		out[h.Name] = h
	}
	return out
}

// TestAssessHostInterfacesReportsEveryInterfaceWithStableReasons asserts that
// the assessment returns ALL interfaces (never filtered) with the exact reason
// code for each policy exclusion, and that a usable interface carries cloned,
// sorted dotted-decimal IPv4 strings.
func TestAssessHostInterfacesReportsEveryInterfaceWithStableReasons(t *testing.T) {
	source := fakeInterfaceSource{
		ifaces: []net.Interface{
			upIface(2, "en0", false),                            // usable
			pointToPointIface(3, "tun0"),                        // point_to_point
			nonMulticastIface(4, "weird0"),                      // multicast_unsupported
			{Index: 5, Name: "down0", Flags: net.FlagMulticast}, // down (no FlagUp)
			upIface(1, "lo0", true),                             // loopback_excluded by default
			upIface(6, "empty0", false),                         // no_usable_ipv4
		},
		addrs: map[string][]net.Addr{
			"en0":    {ipNet("192.168.1.20"), ipNet("192.168.1.10")},
			"tun0":   {ipNet("10.8.0.2")},
			"weird0": {ipNet("172.16.0.9")},
			"down0":  {ipNet("192.168.9.9")},
			"lo0":    {ipNet("127.0.0.1")},
			"empty0": {}, // no addresses -> no usable IPv4
		},
	}

	hosts, err := AssessHostInterfaces(context.Background(), source, interfaceFilter{})
	if err != nil {
		t.Fatalf("AssessHostInterfaces: %v", err)
	}
	if len(hosts) != 6 {
		t.Fatalf("got %d interfaces, want all 6 reported", len(hosts))
	}

	// Deterministic order: by index then name.
	wantOrder := []string{"lo0", "en0", "tun0", "weird0", "down0", "empty0"}
	for i, name := range wantOrder {
		if hosts[i].Name != name {
			var got []string
			for _, h := range hosts {
				got = append(got, h.Name)
			}
			t.Fatalf("order = %v, want %v", got, wantOrder)
		}
	}

	by := reasonByName(hosts)
	cases := map[string]struct {
		reason InterfaceReason
		usable bool
	}{
		"en0":    {ReasonUsable, true},
		"tun0":   {ReasonPointToPoint, false},
		"weird0": {ReasonMulticastUnsupported, false},
		"down0":  {ReasonDown, false},
		"lo0":    {ReasonLoopbackExcluded, false},
		"empty0": {ReasonNoUsableIPv4, false},
	}
	for name, want := range cases {
		got := by[name]
		if got.Reason != want.reason {
			t.Errorf("%s reason = %q, want %q", name, got.Reason, want.reason)
		}
		if got.Usable != want.usable {
			t.Errorf("%s usable = %v, want %v", name, got.Usable, want.usable)
		}
	}

	// Usable interface carries sorted, cloned IPv4 strings; others carry none.
	en0 := by["en0"]
	if want := []string{"192.168.1.10", "192.168.1.20"}; !slices.Equal(en0.IPv4, want) {
		t.Errorf("en0 IPv4 = %v, want sorted %v", en0.IPv4, want)
	}
	for _, name := range []string{"tun0", "weird0", "down0", "lo0", "empty0"} {
		if len(by[name].IPv4) != 0 {
			t.Errorf("%s IPv4 = %v, want empty for an unusable interface", name, by[name].IPv4)
		}
	}
}

// TestAssessHostInterfacesLinkLocalAllowed asserts link-local IPv4 remains a
// usable unicast source, matching the active-discovery policy.
func TestAssessHostInterfacesLinkLocalAllowed(t *testing.T) {
	source := fakeInterfaceSource{
		ifaces: []net.Interface{upIface(2, "en0", false)},
		addrs:  map[string][]net.Addr{"en0": {ipNet("169.254.5.6")}},
	}
	hosts, err := AssessHostInterfaces(context.Background(), source, interfaceFilter{})
	if err != nil {
		t.Fatalf("AssessHostInterfaces: %v", err)
	}
	if len(hosts) != 1 || !hosts[0].Usable {
		t.Fatalf("link-local interface = %+v, want usable", hosts)
	}
	if want := []string{"169.254.5.6"}; !slices.Equal(hosts[0].IPv4, want) {
		t.Errorf("IPv4 = %v, want %v", hosts[0].IPv4, want)
	}
}

// TestAssessHostInterfacesLoopbackOptInNeverRelaxesOtherChecks asserts that an
// explicit loopback opt-in admits a multicast-capable loopback but still
// excludes a non-multicast loopback: the opt-in relaxes nothing else.
func TestAssessHostInterfacesLoopbackOptInNeverRelaxesOtherChecks(t *testing.T) {
	source := fakeInterfaceSource{
		ifaces: []net.Interface{
			upIface(1, "lo0", true), // multicast loopback -> usable when opted in
			{Index: 7, Name: "lo1", Flags: net.FlagUp | net.FlagLoopback}, // non-multicast loopback
		},
		addrs: map[string][]net.Addr{
			"lo0": {ipNet("127.0.0.1")},
			"lo1": {ipNet("127.0.0.2")},
		},
	}
	hosts, err := AssessHostInterfaces(context.Background(), source, interfaceFilter{includeLoopback: true})
	if err != nil {
		t.Fatalf("AssessHostInterfaces: %v", err)
	}
	by := reasonByName(hosts)
	if h := by["lo0"]; !h.Usable || h.Reason != ReasonUsable {
		t.Errorf("lo0 = %+v, want usable multicast loopback when opted in", h)
	}
	if h := by["lo1"]; h.Usable || h.Reason != ReasonMulticastUnsupported {
		t.Errorf("lo1 = %+v, want multicast_unsupported even when loopback opted in", h)
	}
}

// TestAssessHostInterfacesAddressReadFoldsToSafeReason asserts that a
// per-interface Addrs failure never aborts the enumeration and never leaks the
// operating-system error: it folds into ReasonAddressUnavailable while other
// interfaces are still reported.
func TestAssessHostInterfacesAddressReadFoldsToSafeReason(t *testing.T) {
	osErr := errors.New("ioctl SIOCGIFADDR: raw os failure that must not leak")
	source := addrErrorSource{
		fakeInterfaceSource: fakeInterfaceSource{
			ifaces: []net.Interface{
				upIface(2, "en0", false),
				upIface(3, "flaky0", false),
			},
			addrs: map[string][]net.Addr{"en0": {ipNet("192.168.1.10")}},
		},
		failFor: "flaky0",
		addrErr: osErr,
	}
	hosts, err := AssessHostInterfaces(context.Background(), source, interfaceFilter{})
	if err != nil {
		t.Fatalf("AssessHostInterfaces returned error, want folded reason: %v", err)
	}
	by := reasonByName(hosts)
	if h := by["flaky0"]; h.Usable || h.Reason != ReasonAddressUnavailable {
		t.Errorf("flaky0 = %+v, want address_unavailable", h)
	}
	// The raw OS error string must never appear in any DTO field.
	for _, h := range hosts {
		if string(h.Reason) == osErr.Error() {
			t.Fatalf("raw OS error leaked into reason: %q", h.Reason)
		}
	}
	if h := by["en0"]; !h.Usable {
		t.Errorf("en0 = %+v, want still usable despite a sibling's failure", h)
	}
}

// TestAssessHostInterfacesPreservesActiveDiscoveryAddressReadError asserts the
// paired invariant: the ACTIVE discovery path (projectInterface via
// usableInterfaces) still treats an address-read failure as a hard error, so
// existing Discoverer and Listener behavior is unchanged.
func TestAssessHostInterfacesPreservesActiveDiscoveryAddressReadError(t *testing.T) {
	osErr := errors.New("raw os failure")
	source := addrErrorSource{
		fakeInterfaceSource: fakeInterfaceSource{
			ifaces: []net.Interface{upIface(2, "en0", false)},
		},
		failFor: "en0",
		addrErr: osErr,
	}
	if _, err := usableInterfaces(source, interfaceFilter{}); err == nil {
		t.Fatal("usableInterfaces must still surface an address-read error for active discovery")
	}
}

// TestAssessHostInterfacesEnumerateErrorAndCancellation asserts that a
// whole-host enumeration failure is wrapped and returned, and that a cancelled
// context short-circuits before any work.
func TestAssessHostInterfacesEnumerateErrorAndCancellation(t *testing.T) {
	enumErr := errors.New("cannot list interfaces")
	source := fakeInterfaceSource{err: enumErr}
	if _, err := AssessHostInterfaces(context.Background(), source, interfaceFilter{}); !errors.Is(err, enumErr) {
		t.Fatalf("enumerate error = %v, want wrapped %v", err, enumErr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	good := fakeInterfaceSource{
		ifaces: []net.Interface{upIface(2, "en0", false)},
		addrs:  map[string][]net.Addr{"en0": {ipNet("192.168.1.10")}},
	}
	if _, err := AssessHostInterfaces(ctx, good, interfaceFilter{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context = %v, want context.Canceled", err)
	}
}
