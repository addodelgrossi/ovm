package discovery

import (
	"bytes"
	"context"
	"errors"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// multiAddrInterfaceSource yields a single interface carrying two usable IPv4
// source addresses, so active discovery must bind each source independently.
func multiAddrInterfaceSource() fakeInterfaceSource {
	return fakeInterfaceSource{
		ifaces: []net.Interface{upIface(2, "en0", false)},
		addrs: map[string][]net.Addr{
			"en0": {ipNet("192.168.1.10"), ipNet("192.168.1.11")},
		},
	}
}

// TestDiscoverProbesEveryUsableSourceAddress asserts finding 1: every usable
// IPv4 source address on an interface is probed through its own exact-source
// socket, so a multi-homed interface is fully covered. The source-aware
// ActiveListen seam records which source each socket bound.
func TestDiscoverProbesEveryUsableSourceAddress(t *testing.T) {
	var mu sync.Mutex
	boundSources := map[string]int{}
	connections := map[string]*scriptedConn{}

	d := NewDiscoverer(Options{
		Timeout:         150 * time.Millisecond,
		InterfaceSource: multiAddrInterfaceSource(),
		Randomness:      seededRandomness(5),
		// No retransmission so the test is quick and deterministic.
		Retransmit: RetransmitPolicy{Retransmissions: 0},
		ActiveListen: func(_ Interface, source net.IP) (PacketConn, error) {
			conn := newScriptedConn()
			mu.Lock()
			boundSources[source.String()]++
			connections[source.String()] = conn
			mu.Unlock()
			return conn, nil
		},
	})

	if _, err := d.Discover(context.Background()); err != nil {
		t.Fatalf("Discover: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if boundSources["192.168.1.10"] != 1 || boundSources["192.168.1.11"] != 1 {
		t.Fatalf("bound sources = %v, want each of 192.168.1.10 and 192.168.1.11 bound exactly once", boundSources)
	}
	for source, conn := range connections {
		if got := len(conn.writtenDatagrams()); got != len(probeTypes) {
			t.Fatalf("source %s wrote %d probes, want %d", source, got, len(probeTypes))
		}
	}
}

// TestDiscoverRetransmitsReusingMessageIDAndBytes asserts finding 2: each
// retransmission of a logical probe reuses the SAME MessageID and the SAME
// bytes, distinct logical probes keep distinct MessageIDs, and the number of
// transmissions equals (1 + Retransmissions) per logical probe. Deterministic
// Delay and Wait seams drive timing with no real sleeping.
func TestDiscoverRetransmitsReusingMessageIDAndBytes(t *testing.T) {
	conn := newScriptedConn() // no replies; we only inspect writes

	// A Wait seam that returns immediately (full wait elapsed) while the context
	// is live, so all retransmissions are emitted deterministically.
	waits := 0
	wait := func(ctx context.Context, _ time.Duration) bool {
		if ctx.Err() != nil {
			return false
		}
		waits++
		return true
	}

	d := NewDiscoverer(Options{
		Timeout:         2 * time.Second,
		InterfaceSource: singleInterfaceSource(),
		Randomness:      seededRandomness(9),
		Retransmit: RetransmitPolicy{
			Retransmissions: 2,
			Delay:           func(int) time.Duration { return 0 },
			Wait:            wait,
		},
		ActiveListen: func(Interface, net.IP) (PacketConn, error) { return conn, nil },
	})

	// Cancel shortly so the read loop does not block the whole 2s timeout; the
	// writes have already been emitted synchronously (initial) and via the
	// immediate Wait seam (retransmissions) before any blocking read.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := d.Discover(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Discover error = %v, want context.DeadlineExceeded", err)
	}

	writes := conn.writtenDatagrams()
	// One interface, one source, len(probeTypes) logical probes, each sent
	// (1 + 2) times.
	wantPerProbe := 3
	wantTotal := len(probeTypes) * wantPerProbe
	if len(writes) != wantTotal {
		t.Fatalf("writes = %d, want %d (%d logical probes x %d sends)", len(writes), wantTotal, len(probeTypes), wantPerProbe)
	}

	// Group identical byte payloads: each logical probe must appear exactly
	// wantPerProbe times with byte-identical copies, and the distinct logical
	// probes must differ (distinct MessageIDs embedded in distinct bytes).
	counts := map[string]int{}
	for _, w := range writes {
		counts[string(w)]++
	}
	if len(counts) != len(probeTypes) {
		t.Fatalf("distinct probe payloads = %d, want %d (distinct logical probes keep distinct bytes/IDs)", len(counts), len(probeTypes))
	}
	for payload, c := range counts {
		if c != wantPerProbe {
			t.Fatalf("payload repeated %d times, want %d (retransmission must reuse identical bytes)", c, wantPerProbe)
		}
		if !bytes.Contains([]byte(payload), []byte("urn:uuid:")) {
			t.Fatalf("probe payload missing a MessageID urn:uuid")
		}
	}
}

// TestDiscoverRetransmitStopsOnContextCancellation asserts that retransmission
// stops cleanly when the context ends during a backoff: the Wait seam reports a
// short wait, and once the context is cancelled no further sends occur and
// Discover returns the caller's context error without leaking goroutines (the
// race run covers the leak check).
func TestDiscoverRetransmitStopsOnContextCancellation(t *testing.T) {
	conn := newScriptedConn()
	ctx, cancel := context.WithCancel(context.Background())

	wait := func(c context.Context, _ time.Duration) bool {
		// Cancel the round from inside the first backoff, then report the wait
		// was cut short so retransmission stops immediately.
		cancel()
		return false
	}

	d := NewDiscoverer(Options{
		Timeout:         2 * time.Second,
		InterfaceSource: singleInterfaceSource(),
		Randomness:      seededRandomness(3),
		Retransmit: RetransmitPolicy{
			Retransmissions: 5,
			Delay:           func(int) time.Duration { return time.Millisecond },
			Wait:            wait,
		},
		ActiveListen: func(Interface, net.IP) (PacketConn, error) { return conn, nil },
	})

	_, err := d.Discover(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}

	// Only the initial transmission (len(probeTypes) writes) should have gone
	// out; the backoff was cut short before any retransmission.
	if got := len(conn.writtenDatagrams()); got != len(probeTypes) {
		t.Fatalf("writes = %d, want %d (no retransmission after cancellation)", got, len(probeTypes))
	}
}

// TestDiscoverRetransmitWriteFailurePropagates asserts finding 3: when the
// initial sends succeed but a retransmission write fails, the background sender
// does not silently discard the failure. The write error is propagated as the
// source socket diagnostic, the source is torn down immediately, and Discover
// returns well before its own long timeout rather than idling until it elapses
// or waiting out unrelated backoff.
func TestDiscoverRetransmitWriteFailurePropagates(t *testing.T) {
	conn := newScriptedConn() // no replies
	// Let the initial transmission (one write per logical probe) succeed, then
	// fail the very first retransmission write.
	conn.writeErrAfter = len(probeTypes)
	conn.writeErrLate = errPlain{}

	// A Wait seam that returns immediately while live so the first
	// retransmission is attempted at once; the write then fails.
	wait := func(ctx context.Context, _ time.Duration) bool {
		return ctx.Err() == nil
	}

	d := NewDiscoverer(Options{
		// A deliberately long timeout: the test proves the round ends on the
		// write failure, not on the timeout.
		Timeout:         30 * time.Second,
		InterfaceSource: singleInterfaceSource(),
		Randomness:      seededRandomness(7),
		Retransmit: RetransmitPolicy{
			Retransmissions: 3,
			Delay:           func(int) time.Duration { return 0 },
			Wait:            wait,
		},
		ActiveListen: func(Interface, net.IP) (PacketConn, error) { return conn, nil },
	})

	start := time.Now()
	result, err := d.Discover(context.Background())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if result.Diagnostic.Category != DiagnosticSocketError {
		t.Fatalf("diagnostic = %q, want socket_error from the failed retransmit write", result.Diagnostic.Category)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Discover took %v, want a prompt return on the write failure (not the 30s timeout)", elapsed)
	}
	// The initial sends went out; the failing retransmit is not recorded as a
	// successful write.
	if got := len(conn.writtenDatagrams()); got != len(probeTypes) {
		t.Fatalf("successful writes = %d, want %d (initial sends only)", got, len(probeTypes))
	}
}

// TestDiscoverPreCancelledContextDoesNoWork asserts finding 1: a caller whose
// context is already cancelled before Discover is called performs ZERO work:
// no interface enumeration and no socket binds. The interface source and the
// ActiveListen factory are both instrumented and must never be touched, and
// Discover returns the caller's context error.
func TestDiscoverPreCancelledContextDoesNoWork(t *testing.T) {
	var enumerations, binds int32
	source := countingInterfaceSource{
		inner: singleInterfaceSource(),
		count: &enumerations,
	}

	d := NewDiscoverer(Options{
		Timeout:         time.Second,
		InterfaceSource: source,
		ActiveListen: func(Interface, net.IP) (PacketConn, error) {
			atomic.AddInt32(&binds, 1)
			return newScriptedConn(), nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel before the call

	_, err := d.Discover(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if n := atomic.LoadInt32(&enumerations); n != 0 {
		t.Fatalf("interface enumerations = %d, want 0 for a pre-cancelled caller", n)
	}
	if n := atomic.LoadInt32(&binds); n != 0 {
		t.Fatalf("socket binds = %d, want 0 for a pre-cancelled caller", n)
	}
}

// countingInterfaceSource counts how many times Interfaces is called so a test
// can assert that enumeration never happened.
type countingInterfaceSource struct {
	inner InterfaceSource
	count *int32
}

func (c countingInterfaceSource) Interfaces() ([]net.Interface, error) {
	atomic.AddInt32(c.count, 1)
	return c.inner.Interfaces()
}

func (c countingInterfaceSource) Addrs(iface net.Interface) ([]net.Addr, error) {
	return c.inner.Addrs(iface)
}

// TestRetransmitDelayDefaultJitterNotFixedSeed asserts finding 2: the
// production default jitter is seeded from a non-fixed source, so two
// independently constructed Discoverers do not produce an identical default
// jitter schedule (a fixed seed would make retransmission timing predictable
// and synchronise storms). Bounds are still respected: each delay lies in
// [base, 2*base).
func TestRetransmitDelayDefaultJitterNotFixedSeed(t *testing.T) {
	const samples = 16
	collect := func() []time.Duration {
		d := NewDiscoverer(Options{InterfaceSource: singleInterfaceSource()})
		out := make([]time.Duration, samples)
		for i := 0; i < samples; i++ {
			delay := d.retransmitDelay(i + 1)
			if delay < defaultRetransmitBackoff || delay >= 2*defaultRetransmitBackoff {
				t.Fatalf("delay %v out of [%v, %v)", delay, defaultRetransmitBackoff, 2*defaultRetransmitBackoff)
			}
			out[i] = delay
		}
		return out
	}
	a := collect()
	b := collect()
	identical := true
	for i := range a {
		if a[i] != b[i] {
			identical = false
			break
		}
	}
	if identical {
		t.Fatal("two Discoverers produced an identical default jitter schedule; the seed appears fixed/predictable")
	}
}

// TestRetransmitDelayInjectedRandDeterministic asserts finding 2: an injected
// *rand.Rand keeps the default jitter schedule fully deterministic, so tests
// retain reproducibility while production stays randomly seeded.
func TestRetransmitDelayInjectedRandDeterministic(t *testing.T) {
	newD := func() *Discoverer {
		return NewDiscoverer(Options{
			InterfaceSource: singleInterfaceSource(),
			// #nosec G404 -- a fixed pseudorandom source is the deterministic test seam.
			Retransmit: RetransmitPolicy{Rand: rand.New(rand.NewSource(42))},
		})
	}
	d1, d2 := newD(), newD()
	for i := 1; i <= 8; i++ {
		if got, want := d1.retransmitDelay(i), d2.retransmitDelay(i); got != want {
			t.Fatalf("attempt %d: injected-Rand delays diverged (%v vs %v); want deterministic", i, got, want)
		}
	}
}

// TestDiscoverCancellationDuringSetupClosesSocket asserts finding 1: when the
// round is cancelled while a source socket is being set up (here, cancelled
// from inside the ActiveListen factory just after the socket is created), the
// returned socket is closed and no probe is transmitted on it.
func TestDiscoverCancellationDuringSetupClosesSocket(t *testing.T) {
	conn := newScriptedConn()
	ctx, cancel := context.WithCancel(context.Background())

	d := NewDiscoverer(Options{
		Timeout:         5 * time.Second,
		InterfaceSource: singleInterfaceSource(),
		Randomness:      seededRandomness(4),
		Retransmit:      RetransmitPolicy{Retransmissions: 0},
		ActiveListen: func(Interface, net.IP) (PacketConn, error) {
			// The round is cancelled during setup, after the socket is created
			// but before probing begins.
			cancel()
			return conn, nil
		},
	})

	_, err := d.Discover(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if !conn.isClosed() {
		t.Fatal("socket bound during a cancelled setup was not closed")
	}
	if got := len(conn.writtenDatagrams()); got != 0 {
		t.Fatalf("writes = %d, want 0 (no transmission after setup cancellation)", got)
	}
}
