package discovery

import (
	"errors"
	"net"
	"reflect"
	"testing"
)

func sampleSource() fakeInterfaceSource {
	return fakeInterfaceSource{
		ifaces: []net.Interface{
			upIface(1, "lo0", true),
			upIface(2, "en0", false),
			upIface(3, "en1", false),
			{Index: 4, Name: "down0", Flags: 0}, // not up
			upIface(5, "empty0", false),         // up but no IPv4
		},
		addrs: map[string][]net.Addr{
			"lo0":    {ipNet("127.0.0.1")},
			"en0":    {ipNet("192.168.1.10"), ipNet("fe80::1"), ipNet("169.254.1.1")},
			"en1":    {ipNet("10.0.0.5"), ipNet("10.0.0.5")}, // duplicate collapses
			"empty0": {ipNet("fe80::2")},                     // IPv6 only
		},
	}
}

func TestUsableInterfacesFiltering(t *testing.T) {
	tests := []struct {
		name            string
		includeLoopback bool
		wantNames       []string
	}{
		{name: "excludes loopback by default", includeLoopback: false, wantNames: []string{"en0", "en1"}},
		{name: "includes loopback when requested", includeLoopback: true, wantNames: []string{"lo0", "en0", "en1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := usableInterfaces(sampleSource(), interfaceFilter{includeLoopback: tc.includeLoopback})
			if err != nil {
				t.Fatalf("usableInterfaces: %v", err)
			}
			var names []string
			for _, iface := range got {
				names = append(names, iface.Name)
			}
			if !reflect.DeepEqual(names, tc.wantNames) {
				t.Fatalf("names = %v, want %v", names, tc.wantNames)
			}
		})
	}
}

// TestUsableInterfacesIncludesLinkLocalDropsIPv6AndDeduplicates asserts the
// usability policy: IPv4 link-local unicast (169.254/16) is INCLUDED because
// cameras commonly autoconfigure such an address, IPv6 is dropped, and
// duplicate IPv4 addresses collapse. en0 therefore keeps both its routable and
// its link-local IPv4 address, sorted deterministically.
func TestUsableInterfacesIncludesLinkLocalDropsIPv6AndDeduplicates(t *testing.T) {
	got, err := usableInterfaces(sampleSource(), interfaceFilter{})
	if err != nil {
		t.Fatalf("usableInterfaces: %v", err)
	}
	byName := map[string][]net.IP{}
	for _, iface := range got {
		byName[iface.Name] = iface.IPv4
	}
	// en0: link-local kept alongside the routable address; IPv6 dropped. Sorted
	// by string, "169.254.1.1" < "192.168.1.10".
	en0 := byName["en0"]
	if len(en0) != 2 || en0[0].String() != "169.254.1.1" || en0[1].String() != "192.168.1.10" {
		t.Fatalf("en0 IPv4 = %v, want [169.254.1.1 192.168.1.10] (link-local kept, IPv6 dropped)", en0)
	}
	if ips := byName["en1"]; len(ips) != 1 || ips[0].String() != "10.0.0.5" {
		t.Fatalf("en1 IPv4 = %v, want deduplicated 10.0.0.5", ips)
	}
}

// TestUsableIPv4RejectsUnspecifiedAndMulticast asserts that unspecified and
// multicast IPv4, and all IPv6, are rejected while link-local and routable
// unicast are accepted.
func TestUsableIPv4RejectsUnspecifiedAndMulticast(t *testing.T) {
	addrs := []net.Addr{
		ipNet("0.0.0.0"),      // unspecified: rejected
		ipNet("224.0.0.1"),    // multicast: rejected
		ipNet("fe80::1"),      // IPv6: rejected
		ipNet("169.254.5.5"),  // link-local unicast: accepted
		ipNet("192.168.0.20"), // routable unicast: accepted
	}
	got := usableIPv4(addrs, false)
	if len(got) != 2 {
		t.Fatalf("usableIPv4 = %v, want 2 (link-local + routable)", got)
	}
	if got[0].String() != "169.254.5.5" || got[1].String() != "192.168.0.20" {
		t.Fatalf("usableIPv4 = %v, want [169.254.5.5 192.168.0.20]", got)
	}
}

func TestSelectInterfaces(t *testing.T) {
	usable, err := usableInterfaces(sampleSource(), interfaceFilter{})
	if err != nil {
		t.Fatalf("usableInterfaces: %v", err)
	}

	all, err := selectInterfaces(usable, "")
	if err != nil {
		t.Fatalf("select all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("select all returned %d interfaces, want 2", len(all))
	}

	one, err := selectInterfaces(usable, "en1")
	if err != nil {
		t.Fatalf("select en1: %v", err)
	}
	if len(one) != 1 || one[0].Name != "en1" {
		t.Fatalf("select en1 = %v, want single en1", one)
	}

	if _, err := selectInterfaces(usable, "nope"); !errors.Is(err, ErrInterfaceNotUsable) {
		t.Fatalf("select unknown err = %v, want ErrInterfaceNotUsable", err)
	}
}

func TestResolveInterfacesDistinguishesAbsentFromUnusable(t *testing.T) {
	source := sampleSource()

	// "lo0" exists but is unusable without loopback inclusion.
	_, err := resolveInterfaces(source, interfaceFilter{}, "lo0")
	if !errors.Is(err, ErrInterfaceNotUsable) {
		t.Fatalf("lo0 err = %v, want ErrInterfaceNotUsable", err)
	}

	// "ghost" is absent entirely.
	_, err = resolveInterfaces(source, interfaceFilter{}, "ghost")
	if !errors.Is(err, ErrInterfaceNotFound) {
		t.Fatalf("ghost err = %v, want ErrInterfaceNotFound", err)
	}

	// With loopback allowed, lo0 resolves.
	got, err := resolveInterfaces(source, interfaceFilter{includeLoopback: true}, "lo0")
	if err != nil {
		t.Fatalf("lo0 with loopback: %v", err)
	}
	if len(got) != 1 || got[0].Name != "lo0" {
		t.Fatalf("lo0 with loopback = %v, want single lo0", got)
	}
}

func TestResolveInterfacesEmptyWhenNoneUsable(t *testing.T) {
	source := fakeInterfaceSource{
		ifaces: []net.Interface{upIface(1, "lo0", true)},
		addrs:  map[string][]net.Addr{"lo0": {ipNet("127.0.0.1")}},
	}
	_, err := resolveInterfaces(source, interfaceFilter{}, "")
	if !errors.Is(err, ErrNoUsableInterfaces) {
		t.Fatalf("err = %v, want ErrNoUsableInterfaces", err)
	}
}
