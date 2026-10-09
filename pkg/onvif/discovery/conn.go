package discovery

import (
	"fmt"
	"net"
	"time"

	"golang.org/x/net/ipv4"
)

// PacketConn is the subset of net.PacketConn the package depends on. Narrowing
// the surface keeps injected test doubles small and makes the required
// behaviour explicit.
type PacketConn interface {
	// WriteTo writes a datagram to addr.
	WriteTo(p []byte, addr net.Addr) (int, error)
	// ReadFrom reads a single datagram, returning the source address.
	ReadFrom(p []byte) (int, net.Addr, error)
	// SetReadDeadline bounds subsequent ReadFrom calls.
	SetReadDeadline(t time.Time) error
	// Close releases the connection.
	Close() error
}

// ListenFactory creates a PacketConn bound to a particular interface. The
// passive listening factory joins the WS-Discovery multicast group; tests
// inject a factory that returns a scripted connection so listening needs no
// multicast capability.
//
// The returned connection is owned by the caller and closed when the operation
// completes or its context is cancelled.
type ListenFactory func(iface Interface) (PacketConn, error)

// ActiveListenFactory creates a PacketConn for ACTIVE discovery bound to one
// exact unicast source address on an interface. Unlike ListenFactory it is
// source-aware: active probing binds every usable IPv4 source address on an
// interface independently so that, on a multi-homed interface, each source
// emits its own probe and receives the unicast ProbeMatches that devices send
// back to that exact source.
//
// The source is the exact local IPv4 address the socket binds to. Production
// also sets the multicast egress interface and a TTL of 1 on the returned
// connection so the probe is confined to the local link. Tests inject a
// factory that ignores the source and returns a scripted connection.
//
// The returned connection is owned by the caller and closed when the operation
// completes or its context is cancelled.
type ActiveListenFactory func(iface Interface, source net.IP) (PacketConn, error)

// systemActiveListenFactory binds a UDP IPv4 socket on the EXACT source address
// for ACTIVE discovery, then explicitly pins the multicast egress interface and
// sets the multicast hop limit (TTL) to 1 using golang.org/x/net/ipv4.
//
// Why exact-source binding: WS-Discovery active probing sends to the multicast
// group and reads the unicast ProbeMatches replies that devices send back to
// the probe's source address. Binding the exact source (rather than the
// wildcard) makes the egress address deterministic on multi-homed hosts and
// lets replies land on the right socket.
//
// Why explicit egress + TTL 1: the kernel's default multicast egress interface
// is not necessarily the one we intend, and the default multicast TTL is
// implementation-defined. Pinning the egress interface guarantees the probe
// leaves the chosen NIC, and TTL 1 confines ad-hoc discovery to the local link
// so a probe is never routed beyond it.
//
// Cleanup: any failure after the socket is opened closes it before returning so
// a half-configured descriptor never leaks. The caller owns the returned
// connection on success and closes it on completion or cancellation.
func systemActiveListenFactory(iface Interface, source net.IP) (PacketConn, error) {
	local := &net.UDPAddr{IP: source, Port: 0}
	conn, err := net.ListenUDP("udp4", local)
	if err != nil {
		return nil, err
	}

	netIface, err := net.InterfaceByIndex(iface.Index)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("resolve interface %q for multicast egress: %w", iface.Name, err)
	}

	pc := ipv4.NewPacketConn(conn)
	if err := configureActiveMulticast(pc, netIface); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("configure multicast on interface %q: %w", iface.Name, err)
	}
	return conn, nil
}

// multicastConfigurer is the exact x/net/ipv4 option surface active discovery
// needs. Keeping the configuration in a small helper makes the required egress
// interface and TTL=1 policy deterministic to unit-test without opening a real
// multicast socket.
type multicastConfigurer interface {
	SetMulticastInterface(*net.Interface) error
	SetMulticastTTL(int) error
}

func configureActiveMulticast(pc multicastConfigurer, iface *net.Interface) error {
	if pc == nil || iface == nil {
		return fmt.Errorf("multicast configuration requires an interface")
	}
	if err := pc.SetMulticastInterface(iface); err != nil {
		return fmt.Errorf("set multicast egress interface: %w", err)
	}
	if err := pc.SetMulticastTTL(1); err != nil {
		return fmt.Errorf("set multicast ttl: %w", err)
	}
	return nil
}

// multicastListenFactory joins the WS-Discovery IPv4 multicast group on the
// given interface so PASSIVE listening receives Hello and Bye announcements.
// It binds the multicast port and requests membership on the specific
// interface, which is required to receive the group on multi-homed hosts and
// is the production default for Listener.
//
// Group membership can require privilege on some platforms; when the join
// fails the error surfaces to NewListener's caller as a listen error rather
// than being silently downgraded to a unicast socket that could not observe
// the lifecycle.
func multicastListenFactory(iface Interface) (PacketConn, error) {
	group, err := multicastAddr()
	if err != nil {
		return nil, err
	}
	netIface, err := net.InterfaceByIndex(iface.Index)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenMulticastUDP("udp4", netIface, group)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// multicastAddr returns the resolved WS-Discovery multicast destination.
func multicastAddr() (*net.UDPAddr, error) {
	return net.ResolveUDPAddr("udp4", multicastGroup)
}
