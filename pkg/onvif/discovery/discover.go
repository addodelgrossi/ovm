package discovery

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"math/rand"
	"net"
	"os"
	"sync"
	"time"
)

// defaultTimeout bounds a discovery round when the caller does not supply one.
const defaultTimeout = 4 * time.Second

// defaultRetransmissions is the number of EXTRA transmissions of each logical
// probe beyond the first, when the caller does not configure retransmission.
// WS-Discovery recommends retransmitting an unacknowledged multicast Probe a
// small bounded number of times because UDP is lossy; two retransmissions
// (three sends total per logical probe) is a conservative, link-friendly
// default.
const defaultRetransmissions = 2

// defaultRetransmitBackoff is the base inter-transmission delay used when the
// caller does not configure one. Each retransmission waits this base plus
// bounded, independently seeded jitter.
const defaultRetransmitBackoff = 300 * time.Millisecond

// Options configures a Discoverer. The zero value is usable and uses the
// system interface source, the system active listen factory, crypto/rand for
// message identifiers, and the default timeout and retransmission policy.
type Options struct {
	// InterfaceName selects a single interface by name. An empty name probes
	// every usable interface.
	InterfaceName string
	// IncludeLoopback admits loopback interfaces, which lets tests bind
	// multicast on the loopback device.
	IncludeLoopback bool
	// Timeout bounds a discovery round. Zero selects defaultTimeout.
	Timeout time.Duration

	// InterfaceSource overrides the source of host interfaces. Nil selects the
	// system source.
	InterfaceSource InterfaceSource
	// Listen overrides the packet-connection factory for the PASSIVE Listener
	// and is also used as a source-agnostic fallback for active discovery when
	// ActiveListen is nil. Nil selects the production factory (multicast-join
	// for the Listener; exact-source bind for active discovery).
	Listen ListenFactory
	// ActiveListen overrides the source-aware packet-connection factory for
	// ACTIVE discovery. Nil falls back to Listen (ignoring the source) when
	// Listen is set, otherwise to the production exact-source factory. The
	// fallback preserves existing source-agnostic test injections.
	ActiveListen ActiveListenFactory
	// Randomness overrides the source used for probe MessageIDs. Nil selects
	// crypto/rand.
	Randomness io.Reader

	// Retransmit configures bounded jittered probe retransmission. The zero
	// value selects the production default (defaultRetransmissions extra sends
	// with defaultRetransmitBackoff base delay and real time waits). Tests set
	// its fields to make retransmission deterministic.
	Retransmit RetransmitPolicy
}

// RetransmitPolicy governs bounded, jittered retransmission of each logical
// probe. Every transmission of one logical probe reuses the SAME MessageID and
// the SAME bytes, so a device that drops the first copy and answers a later one
// still correlates. Distinct logical probes (one per advertised type) keep
// distinct MessageIDs.
//
// The Delay and Wait seams are injected so tests drive retransmission timing
// deterministically with no real sleeping and no timers that could leak.
type RetransmitPolicy struct {
	// Retransmissions is the number of EXTRA sends of each logical probe beyond
	// the first. When the whole policy is left zero the production default
	// (defaultRetransmissions) applies; setting a Delay or Wait seam while
	// leaving this zero means "no retransmission".
	Retransmissions int
	// Delay returns the wait before the attempt-th retransmission (attempt is
	// 1-based). Nil selects a bounded base-plus-jitter schedule derived from the
	// Discoverer's jitter source. The returned duration is clamped to be
	// non-negative.
	Delay func(attempt int) time.Duration
	// Wait blocks for d or until ctx ends, reporting whether it waited the full
	// duration (true) or was cut short by cancellation (false). Nil selects a
	// real timer-based wait with guaranteed timer cleanup.
	Wait func(ctx context.Context, d time.Duration) bool
	// Rand seeds the default jitter schedule. Nil selects a package default. It
	// is only consulted when Delay is nil.
	Rand *rand.Rand
}

// Result reports the outcome of a discovery round.
type Result struct {
	// Devices holds the aggregated devices sorted by EndpointReference.
	Devices []Device
	// Interfaces lists the interfaces that were probed.
	Interfaces []Interface
	// Diagnostic explains an empty or failed round. It is DiagnosticNone when
	// at least one device was found.
	Diagnostic Diagnostic
}

// Discoverer performs interface-aware WS-Discovery probing.
type Discoverer struct {
	opts     Options
	randomMu sync.Mutex
	jitterMu sync.Mutex
	jitter   *rand.Rand
}

// NewDiscoverer returns a Discoverer configured by opts. Nil seams fall back to
// their production implementations.
func NewDiscoverer(opts Options) *Discoverer {
	if opts.InterfaceSource == nil {
		opts.InterfaceSource = SystemInterfaceSource{}
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}

	// Resolve the active-discovery factory. Preference order:
	//   1. an explicit source-aware ActiveListen,
	//   2. an injected source-agnostic Listen (adapted, ignoring the source),
	//   3. the production exact-source factory.
	if opts.ActiveListen == nil {
		if opts.Listen != nil {
			listen := opts.Listen
			opts.ActiveListen = func(iface Interface, _ net.IP) (PacketConn, error) {
				return listen(iface)
			}
		} else {
			opts.ActiveListen = systemActiveListenFactory
		}
	}

	// Normalise the retransmission policy. An untouched zero policy selects the
	// production default; a policy that sets a seam but no count means "no
	// retransmission" and is honoured verbatim.
	if opts.Retransmit.Retransmissions == 0 && opts.Retransmit.Delay == nil && opts.Retransmit.Wait == nil {
		opts.Retransmit.Retransmissions = defaultRetransmissions
	}
	if opts.Retransmit.Retransmissions < 0 {
		opts.Retransmit.Retransmissions = 0
	}

	jitterSeed := opts.Retransmit.Rand
	if jitterSeed == nil {
		// #nosec G404 -- jitter avoids packet bursts; it is not a security token.
		jitterSeed = rand.New(rand.NewSource(secureJitterSeed()))
	}
	return &Discoverer{opts: opts, jitter: jitterSeed}
}

// secureJitterSeed returns a seed for the production retransmission jitter
// source. It draws from crypto/rand so the default jitter schedule is not
// predictable across processes (a fixed seed would let an observer anticipate
// retransmission timing and would synchronise storms across hosts that share
// the binary). When crypto/rand is unavailable it falls back to the current
// time in nanoseconds, which is still non-fixed, so seeding never fails. The
// seam remains fully deterministic whenever a caller injects Retransmit.Rand,
// Delay, or Wait.
func secureJitterSeed() int64 {
	var buf [8]byte
	if _, err := io.ReadFull(cryptorand.Reader, buf[:]); err == nil {
		return int64(binary.LittleEndian.Uint64(buf[:])) // #nosec G115 -- seed bit pattern is intentionally reinterpreted.
	}
	// Safe fallback: time-based, non-fixed. Still avoids a predictable constant.
	return time.Now().UnixNano()
}

// Discover performs a single WS-Discovery round across the selected
// interfaces. It distinguishes two ways a round can end without devices:
//
//   - If the caller's context is cancelled (or already carried a deadline that
//     expired), Discover returns the caller context's error (context.Canceled
//     or context.DeadlineExceeded). Cancellation is never reported as the
//     no_response diagnostic.
//   - If only the Discoverer's own configured timeout elapses with no reply,
//     Discover returns a nil error and a Result whose Diagnostic is
//     DiagnosticNoResponse.
//
// Discover also returns an error when the round cannot start (for example no
// usable interface). A round that starts, finds nothing, and times out on its
// own clock reports a Diagnostic instead of an error.
//
// Each inbound ProbeMatches is correlated to a probe this process sent on the
// receiving source: a reply is aggregated only when it is a ProbeMatches with
// the exact recognised Action for its accepted discovery family, a default or
// explicit Reply RelationshipType, and a WS-Addressing RelatesTo equal to the
// exact MessageID of one of that source's probes. Stale, unrelated,
// action-less, wrong-action, and non-Reply datagrams are ignored.
//
// Multi-homed probing: every usable IPv4 source address on each selected
// interface is probed independently through its own socket, so a camera on any
// of a host's subnets is reachable.
func (d *Discoverer) Discover(ctx context.Context) (Result, error) {
	// A caller whose context is already cancelled performs zero work: no
	// interface enumeration and no socket binds. Return the caller's context
	// error before touching the host network stack.
	if cerr := ctx.Err(); cerr != nil {
		return Result{}, cerr
	}
	ifaces, err := resolveInterfaces(
		d.opts.InterfaceSource,
		interfaceFilter{includeLoopback: d.opts.IncludeLoopback},
		d.opts.InterfaceName,
	)
	if err != nil {
		return Result{}, err
	}

	// The round ends when EITHER the caller's context ends OR the Discoverer's
	// own timeout elapses. The two causes are tracked separately so the result
	// never mislabels a caller cancellation as the no_response diagnostic.
	callerCtx := ctx
	ownTimer := time.NewTimer(d.opts.Timeout)
	defer ownTimer.Stop()
	// Derive the round context from the caller's context so a caller
	// cancellation (including one that lands during per-source setup) is
	// observed by the workers immediately, without waiting for the monitor
	// goroutine to relay it. The monitor still cancels the round on the
	// Discoverer's own timeout. The caller-vs-timeout distinction for the
	// returned diagnostic is preserved by checking callerCtx.Err() explicitly
	// after the round, not by isolating the round context from the caller.
	roundCtx, roundCancel := context.WithCancel(callerCtx)
	defer roundCancel()

	// workersDone is closed once every per-source worker has returned. The
	// monitor watches it so that an immediate all-socket failure ends the round
	// at once instead of waiting the full own timeout.
	workersDone := make(chan struct{})

	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		select {
		case <-callerCtx.Done():
		case <-ownTimer.C:
		case <-workersDone:
			// Every worker finished early (for example all sockets failed to
			// bind); stop the round now rather than idling until the timeout.
		}
		roundCancel()
	}()

	aggregate := newAggregator()
	var (
		mu          sync.Mutex
		wg          sync.WaitGroup
		socketDiags []Diagnostic
	)
	collect := func(message parsedMessage) {
		mu.Lock()
		aggregate.add(message)
		mu.Unlock()
	}
	noteDiagnostic := func(diag Diagnostic) {
		mu.Lock()
		socketDiags = append(socketDiags, diag)
		mu.Unlock()
	}

	// Fan out one worker per (interface, source address). Each worker binds its
	// own exact-source socket so a multi-homed interface is fully covered.
	for _, iface := range ifaces {
		for _, source := range iface.IPv4 {
			wg.Add(1)
			go func(iface Interface, source net.IP) {
				defer wg.Done()
				if diag := d.probeSource(roundCtx, iface, source, collect); diag.Category != DiagnosticNone {
					noteDiagnostic(diag)
				}
			}(iface, source)
		}
	}
	go func() {
		wg.Wait()
		close(workersDone)
	}()
	wg.Wait()

	devices := aggregate.devicesSorted()
	result := Result{Devices: devices, Interfaces: ifaces}

	// Wait until the monitor has recorded the condition that ended the round,
	// then give the caller's context authoritative precedence.
	<-monitorDone
	if cerr := callerCtx.Err(); cerr != nil {
		return result, cerr
	}
	if len(devices) > 0 {
		result.Diagnostic = Diagnostic{Category: DiagnosticNone}
		return result, nil
	}

	result.Diagnostic = summariseDiagnostics(socketDiags)
	return result, nil
}

// summariseDiagnostics reduces per-source socket diagnostics to a single
// category. A permission restriction dominates a generic socket error, and
// both dominate the no_response default. An empty round with no socket error
// is strictly no_response and never claims a permission problem.
func summariseDiagnostics(diags []Diagnostic) Diagnostic {
	var socketError *Diagnostic
	for i := range diags {
		switch diags[i].Category {
		case DiagnosticPossiblePermissionRestriction:
			return diags[i]
		case DiagnosticSocketError:
			if socketError == nil {
				socketError = &diags[i]
			}
		}
	}
	if socketError != nil {
		return *socketError
	}
	return Diagnostic{
		Category: DiagnosticNoResponse,
		Detail:   "no device responded within the timeout",
	}
}

// probePlan is one logical probe: a distinct MessageID and the exact bytes to
// (re)transmit for it.
type probePlan struct {
	messageID string
	bytes     []byte
}

// probeSource binds one exact-source socket on an interface, emits the logical
// probes (with bounded jittered retransmission reusing each probe's MessageID
// and bytes), and reads replies until the round context ends. It returns a
// non-None diagnostic only for a concrete socket failure; a clean stop (round
// context done, read timeout, or closed connection) returns DiagnosticNone.
//
// The initial transmission is synchronous so an immediate socket failure is
// reported before reading. Retransmissions run in a background goroutine that
// is joined before return, so reading and retransmitting proceed concurrently
// and a device whose first datagram is lost can still be heard. The goroutine
// and all timers are torn down on return, so no goroutine or timer leaks.
func (d *Discoverer) probeSource(ctx context.Context, iface Interface, source net.IP, collect func(parsedMessage)) Diagnostic {
	// Honour cancellation before binding: a round that was cancelled during
	// setup must not open a socket on this source at all.
	if ctx.Err() != nil {
		return Diagnostic{Category: DiagnosticNone}
	}
	conn, err := d.opts.ActiveListen(iface, source)
	if err != nil {
		return classifySocketError(err)
	}
	defer func() { _ = conn.Close() }()

	// If the round was cancelled while this socket was being bound, close it
	// immediately and perform no transmission or reading. The deferred Close
	// above guarantees the returned socket is released even on this early path.
	if ctx.Err() != nil {
		return Diagnostic{Category: DiagnosticNone}
	}

	// srcCtx scopes this source's lifetime. Cancelling it stops both the reader
	// and the background retransmitter at once. A retransmission write failure
	// cancels srcCtx so the reader is not left waiting out an unrelated backoff
	// before the socket error is reported.
	srcCtx, srcCancel := context.WithCancel(ctx)
	defer srcCancel()

	// Close the connection when this source's context ends so a blocked
	// ReadFrom unblocks even if the implementation does not honour read
	// deadlines. Watching srcCtx (not the parent) means a retransmit failure,
	// which cancels srcCtx, also closes the socket immediately.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-srcCtx.Done():
			_ = conn.Close()
		case <-stop:
		}
	}()

	addr, err := multicastAddr()
	if err != nil {
		return Diagnostic{Category: DiagnosticSocketError, Detail: "could not resolve the multicast group"}
	}

	// Build the logical probe plan: one logical probe per advertised type, each
	// with its own distinct MessageID and pre-rendered bytes. Retransmission
	// reuses these exact bytes so a device correlates a late reply.
	plans, diag := d.buildProbePlans()
	if diag.Category != DiagnosticNone {
		return diag
	}
	sentIDs := make(map[string]struct{}, len(plans))
	for _, plan := range plans {
		sentIDs[plan.messageID] = struct{}{}
	}

	// Initial transmission (attempt 0), synchronous.
	if diag := sendPlans(srcCtx, conn, addr, plans); diag.Category != DiagnosticNone {
		return diag
	}

	// Retransmit in the background so reading proceeds concurrently. A write
	// failure that is NOT caused by cancellation is a concrete socket fault for
	// THIS source: it is captured, srcCtx is cancelled so the reader stops
	// without waiting out the next backoff, and the socket is closed. The
	// captured diagnostic is read back after the reader returns and takes
	// precedence over the reader's clean stop. The goroutine and its timer are
	// always torn down, so nothing leaks.
	retransmitDone := make(chan struct{})
	retransmitDiag := make(chan Diagnostic, 1)
	go func() {
		defer close(retransmitDone)
		for attempt := 1; attempt <= d.opts.Retransmit.Retransmissions; attempt++ {
			if !d.wait(srcCtx, d.retransmitDelay(attempt)) {
				return // context ended during backoff
			}
			if diag := sendPlans(srcCtx, conn, addr, plans); diag.Category != DiagnosticNone {
				// Propagate the concrete write failure as this source's
				// diagnostic and tear the source down immediately.
				retransmitDiag <- diag
				srcCancel()
				return
			}
			if srcCtx.Err() != nil {
				return
			}
		}
	}()
	// readDiag records the reason the read loop stopped so the sender can be
	// joined before the function returns while still reporting the right
	// outcome. Its zero value is DiagnosticNone.
	var readDiag Diagnostic

	// Read into a buffer one byte larger than the accepted maximum so an
	// oversized datagram is detected and rejected rather than silently parsed
	// from a max-sized prefix.
	buf := make([]byte, maxDatagramSize+1)
readLoop:
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if srcCtx.Err() != nil || isTimeout(err) || errors.Is(err, os.ErrClosed) {
				readDiag = Diagnostic{Category: DiagnosticNone}
				break readLoop
			}
			readDiag = classifySocketError(err)
			break readLoop
		}
		if n <= maxDatagramSize {
			datagram := make([]byte, n)
			copy(datagram, buf[:n])
			if messages, perr := parseMessages(datagram); perr == nil {
				for _, message := range messages {
					if _, sent := sentIDs[message.RelatesTo]; sent && correlatedProbeMatch(message, message.RelatesTo) {
						collect(message)
					}
				}
			}
		}
		// An oversized datagram (n > maxDatagramSize) is dropped silently and
		// reading continues.
		if srcCtx.Err() != nil {
			readDiag = Diagnostic{Category: DiagnosticNone}
			break readLoop
		}
	}

	// Stop and join the background sender before deciding the outcome. A
	// retransmit write failure dominates the reader's clean stop so a lost
	// socket is reported rather than masked as no_response.
	srcCancel()
	<-retransmitDone
	select {
	case diag := <-retransmitDiag:
		return diag
	default:
		return readDiag
	}
}

// buildProbePlans renders one logical probe per advertised type, each with a
// fresh distinct MessageID. The randomness source is guarded because all
// sources probe concurrently and a caller-supplied io.Reader is not necessarily
// concurrency-safe.
func (d *Discoverer) buildProbePlans() ([]probePlan, Diagnostic) {
	plans := make([]probePlan, 0, len(probeTypes))
	seen := make(map[string]struct{}, len(probeTypes))
	for _, probeType := range probeTypes {
		d.randomMu.Lock()
		messageID, idErr := newMessageID(d.opts.Randomness)
		d.randomMu.Unlock()
		if idErr != nil {
			return nil, Diagnostic{Category: DiagnosticSocketError, Detail: "could not generate a probe identifier"}
		}
		if _, duplicate := seen[messageID]; duplicate {
			return nil, Diagnostic{Category: DiagnosticSocketError, Detail: "probe identifiers were not unique"}
		}
		seen[messageID] = struct{}{}
		plans = append(plans, probePlan{messageID: messageID, bytes: probeMessage(messageID, probeType)})
	}
	return plans, Diagnostic{Category: DiagnosticNone}
}

// sendPlans writes every logical probe's identical bytes once. A write error
// that is not caused by cancellation is reported as a socket diagnostic; a
// write that fails because the round ended is a clean stop.
func sendPlans(ctx context.Context, conn PacketConn, addr net.Addr, plans []probePlan) Diagnostic {
	for _, plan := range plans {
		if _, writeErr := conn.WriteTo(plan.bytes, addr); writeErr != nil {
			if ctx.Err() != nil {
				return Diagnostic{Category: DiagnosticNone}
			}
			return classifySocketError(writeErr)
		}
	}
	return Diagnostic{Category: DiagnosticNone}
}

// retransmitDelay returns the backoff before the attempt-th retransmission,
// using the injected Delay seam when present or a deterministic
// base-plus-jitter schedule otherwise. The result is clamped non-negative.
func (d *Discoverer) retransmitDelay(attempt int) time.Duration {
	if d.opts.Retransmit.Delay != nil {
		delay := d.opts.Retransmit.Delay(attempt)
		if delay < 0 {
			return 0
		}
		return delay
	}
	d.jitterMu.Lock()
	// Jitter in [0, base) added to the base so the schedule is bounded and
	// link-friendly while avoiding synchronised retransmission storms.
	jitter := time.Duration(d.jitter.Int63n(int64(defaultRetransmitBackoff)))
	d.jitterMu.Unlock()
	return defaultRetransmitBackoff + jitter
}

// wait blocks for delay or until ctx ends, using the injected Wait seam when
// present or a real timer otherwise. It reports whether the full duration
// elapsed (true) or cancellation cut it short (false). The real timer is always
// stopped so no timer leaks.
func (d *Discoverer) wait(ctx context.Context, delay time.Duration) bool {
	if d.opts.Retransmit.Wait != nil {
		return d.opts.Retransmit.Wait(ctx, delay)
	}
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// correlatedProbeMatch reports whether a parsed message is a ProbeMatches reply
// to a probe this process sent with sentID. ALL of the following must hold:
//
//   - the message is a ProbeMatch;
//   - its RelatesTo is exactly equal to sentID;
//   - its RelatesTo carries the Reply relationship, either the default (no
//     RelationshipType attribute) or an explicit generation-appropriate Reply;
//   - its Action is the exact recognised ProbeMatches Action for the message's
//     discovery family.
//
// Requiring both Action and the Reply relationship rejects stale replies from a
// previous round, unrelated or action-less datagrams, wrong-family actions, and
// messages that relate to our probe under a custom non-Reply relationship.
func correlatedProbeMatch(message parsedMessage, sentID string) bool {
	if message.Kind != kindProbeMatch || message.RelatesTo != sentID || !message.ReplyRelationship {
		return false
	}
	return message.Action == discoveryAction(message.DiscoveryNamespace, actionProbeMatches)
}

// isTimeout reports whether err is a timeout from the net package.
func isTimeout(err error) bool {
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}
