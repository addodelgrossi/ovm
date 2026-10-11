package discovery

import (
	"net"
	"testing"
)

// TestUsableInterfacesExcludesPointToPointAndNonMulticast asserts the usability
// policy additions: an up interface that is point-to-point (PPP/VPN/tunnel) or
// that does not advertise multicast is excluded even though it carries a usable
// IPv4 address, while an ordinary multicast-capable interface is kept.
func TestUsableInterfacesExcludesPointToPointAndNonMulticast(t *testing.T) {
	source := fakeInterfaceSource{
		ifaces: []net.Interface{
			upIface(2, "en0", false),       // ordinary: kept
			pointToPointIface(3, "tun0"),   // VPN/tunnel: excluded
			nonMulticastIface(4, "weird0"), // no multicast flag: excluded
		},
		addrs: map[string][]net.Addr{
			"en0":    {ipNet("192.168.1.10")},
			"tun0":   {ipNet("10.8.0.2")},
			"weird0": {ipNet("172.16.0.9")},
		},
	}

	got, err := usableInterfaces(source, interfaceFilter{})
	if err != nil {
		t.Fatalf("usableInterfaces: %v", err)
	}
	if len(got) != 1 || got[0].Name != "en0" {
		var names []string
		for _, iface := range got {
			names = append(names, iface.Name)
		}
		t.Fatalf("usable = %v, want only [en0] (tun0 point-to-point, weird0 non-multicast excluded)", names)
	}
}

// TestUsableInterfacesNamedPointToPointStaysExcluded asserts that even an
// explicitly named point-to-point interface is reported unusable: the exclusion
// is a safety default that a name selection cannot override, because such a link
// has no shared segment for ad-hoc multicast discovery.
func TestUsableInterfacesNamedPointToPointStaysExcluded(t *testing.T) {
	source := fakeInterfaceSource{
		ifaces: []net.Interface{pointToPointIface(3, "tun0")},
		addrs:  map[string][]net.Addr{"tun0": {ipNet("10.8.0.2")}},
	}
	if _, err := resolveInterfaces(source, interfaceFilter{}, "tun0"); err == nil {
		t.Fatal("expected a named point-to-point interface to be unusable")
	}
}

// TestUsableInterfacesOptedLoopbackMustBeMulticast asserts finding 9: the
// net.FlagMulticast requirement is enforced on EVERY usable interface,
// including an explicitly opted-in loopback. There is no loopback multicast
// relaxation. An opted-in loopback that advertises multicast is kept; an
// opted-in loopback that does NOT advertise multicast is excluded.
func TestUsableInterfacesOptedLoopbackMustBeMulticast(t *testing.T) {
	// Multicast-capable opted-in loopback: kept.
	withMulticast := fakeInterfaceSource{
		ifaces: []net.Interface{upIface(1, "lo0", true)},
		addrs:  map[string][]net.Addr{"lo0": {ipNet("127.0.0.1")}},
	}
	got, err := usableInterfaces(withMulticast, interfaceFilter{includeLoopback: true})
	if err != nil {
		t.Fatalf("usableInterfaces: %v", err)
	}
	if len(got) != 1 || got[0].Name != "lo0" {
		t.Fatalf("usable = %+v, want opted-in multicast loopback kept", got)
	}

	// Opted-in loopback WITHOUT the multicast flag: excluded, no relaxation.
	noMulticast := fakeInterfaceSource{
		ifaces: []net.Interface{
			{Index: 1, Name: "lo0", Flags: net.FlagUp | net.FlagLoopback},
		},
		addrs: map[string][]net.Addr{"lo0": {ipNet("127.0.0.1")}},
	}
	got, err = usableInterfaces(noMulticast, interfaceFilter{includeLoopback: true})
	if err != nil {
		t.Fatalf("usableInterfaces: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("usable = %+v, want non-multicast loopback excluded even when opted in", got)
	}
}
