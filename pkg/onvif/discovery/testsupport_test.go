package discovery

import (
	"bytes"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// fakeInterfaceSource is a deterministic InterfaceSource for tests. It never
// touches the real network stack.
type fakeInterfaceSource struct {
	ifaces []net.Interface
	addrs  map[string][]net.Addr
	err    error
}

func (f fakeInterfaceSource) Interfaces() ([]net.Interface, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.ifaces, nil
}

func (f fakeInterfaceSource) Addrs(iface net.Interface) ([]net.Addr, error) {
	return f.addrs[iface.Name], nil
}

// ipNet builds an *net.IPNet address for a dotted IPv4 string.
func ipNet(ip string) net.Addr {
	return &net.IPNet{IP: net.ParseIP(ip), Mask: net.CIDRMask(24, 32)}
}

// upIface builds an up, multicast-capable interface descriptor. Multicast
// capability is required of EVERY usable interface, including an opted-in
// loopback (there is no loopback multicast relaxation), so both the loopback
// and non-loopback forms set net.FlagMulticast here. Use pointToPointIface or
// nonMulticastIface to model interfaces the usability policy must exclude.
func upIface(index int, name string, loopback bool) net.Interface {
	flags := net.FlagUp | net.FlagMulticast
	if loopback {
		flags |= net.FlagLoopback
	}
	return net.Interface{Index: index, Name: name, Flags: flags}
}

// pointToPointIface builds an up, multicast-flagged, point-to-point interface
// (PPP/VPN/tunnel) which the usability policy must exclude by default.
func pointToPointIface(index int, name string) net.Interface {
	return net.Interface{
		Index: index,
		Name:  name,
		Flags: net.FlagUp | net.FlagMulticast | net.FlagPointToPoint,
	}
}

// nonMulticastIface builds an up, non-loopback interface that does NOT
// advertise multicast capability, which the usability policy must exclude.
func nonMulticastIface(index int, name string) net.Interface {
	return net.Interface{Index: index, Name: name, Flags: net.FlagUp}
}

// scriptedConn is a PacketConn test double. It records writes and replays a
// queue of inbound datagrams, then blocks until the read deadline or Close.
//
// When readErr is set, every ReadFrom returns that error immediately. That
// models a persistently broken socket, which the reader loop must not spin on:
// a reader using a bounded retry terminates after its failure budget rather
// than running hot forever.
type scriptedConn struct {
	mu sync.Mutex

	writes   [][]byte
	inbound  [][]byte
	writeErr error
	// writeErrAfter, when > 0, lets the first writeErrAfter writes succeed and
	// fails every write after that with writeErrLate. It models a socket whose
	// initial sends pass but whose later retransmission write fails, which the
	// background sender must propagate as the source socket diagnostic.
	writeErrAfter int
	writeErrLate  error
	readErr       error
	// readCount counts ReadFrom calls so a hot-loop test can assert that a
	// persistently failing reader stops after a bounded number of attempts.
	readCount int

	closed   chan struct{}
	closeOne sync.Once
	deadline time.Time
}

func newScriptedConn(inbound ...[]byte) *scriptedConn {
	return &scriptedConn{inbound: inbound, closed: make(chan struct{})}
}

func (c *scriptedConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	if c.writeErrAfter > 0 && len(c.writes) >= c.writeErrAfter {
		err := c.writeErrLate
		if err == nil {
			err = errPlain{}
		}
		return 0, err
	}
	c.writes = append(c.writes, append([]byte(nil), p...))
	return len(p), nil
}

func (c *scriptedConn) ReadFrom(p []byte) (int, net.Addr, error) {
	c.mu.Lock()
	c.readCount++
	if c.readErr != nil {
		err := c.readErr
		c.mu.Unlock()
		return 0, nil, err
	}
	if len(c.inbound) > 0 {
		datagram := c.inbound[0]
		c.inbound = c.inbound[1:]
		n := copy(p, datagram)
		addr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 10), Port: 3702}
		c.mu.Unlock()
		return n, addr, nil
	}
	deadline := c.deadline
	c.mu.Unlock()

	// No more scripted datagrams: block until the deadline or Close, then
	// return a timeout so the reader loop terminates like a real socket.
	var timer <-chan time.Time
	if !deadline.IsZero() {
		d := time.Until(deadline)
		if d < 0 {
			d = 0
		}
		t := time.NewTimer(d)
		defer t.Stop()
		timer = t.C
	}
	select {
	case <-c.closed:
		return 0, nil, net.ErrClosed
	case <-timer:
		return 0, nil, timeoutError{}
	}
}

func (c *scriptedConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	return nil
}

func (c *scriptedConn) Close() error {
	c.closeOne.Do(func() { close(c.closed) })
	return nil
}

func (c *scriptedConn) writtenDatagrams() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]byte(nil), c.writes...)
}

func (c *scriptedConn) reads() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.readCount
}

// isClosed reports whether Close has been called on the connection.
func (c *scriptedConn) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

// timeoutError is a net.Error-style timeout used by scriptedConn.
type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

// permissionError is a socket error that reports a permission restriction.
var errPermission = &net.OpError{Op: "listen", Net: "udp4", Err: errors.New("permission denied")}

// seededRandomness returns a deterministic io.Reader for probe MessageID
// generation so active-discovery correlation can be asserted against known
// probe IDs. It yields many distinct 16-byte blocks, allowing multi-address
// tests to generate two logical Probe IDs per source without exhausting the
// seam; the first block remains fill for seededProbeID compatibility.
func seededRandomness(fill byte) io.Reader {
	data := make([]byte, 0, 16*128)
	for i := 0; i < 128; i++ {
		data = append(data, bytes.Repeat([]byte{fill + byte(i)}, 16)...)
	}
	return bytes.NewReader(data)
}

// seededProbeID is the MessageID newMessageID produces from seededRandomness,
// after the RFC 4122 version (4) and variant bits are applied. Fixtures set
// their RelatesTo to this value to model a correlated reply.
func seededProbeID(fill byte) string {
	r := seededRandomness(fill)
	var raw [16]byte
	_, _ = io.ReadFull(r, raw[:])
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return "urn:uuid:" + formatUUID(raw)
}
