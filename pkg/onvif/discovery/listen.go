package discovery

import (
	"context"
	"errors"
	"os"
)

// EventType identifies the WS-Discovery lifecycle event a datagram announced.
type EventType string

const (
	// EventHello announces that a device joined the network.
	EventHello EventType = "hello"
	// EventBye announces that a device is leaving the network.
	EventBye EventType = "bye"
)

// maxConsecutiveReadErrors bounds how many consecutive non-timeout, non-closed
// read errors a single reader tolerates before it gives up on that connection.
// A persistently broken socket therefore terminates its reader instead of
// spinning in a hot loop; healthy readers are unaffected because any successful
// read or benign timeout resets the counter.
const maxConsecutiveReadErrors = 16

// Event is a typed WS-Discovery lifecycle notification. XAddrs, Scopes, and
// Types are bounded and deduplicated projections of the originating datagram.
type Event struct {
	// Type is EventHello or EventBye.
	Type EventType
	// EndpointReference identifies the device.
	EndpointReference string
	// XAddrs holds the advertised transport addresses (empty for most Bye).
	XAddrs []string
	// Scopes holds the advertised scopes.
	Scopes []string
	// Types holds the advertised types as stable expanded-name strings of the
	// form "{namespace}local".
	Types []string
	// Interface is the name of the interface the event arrived on.
	Interface string
}

// Listener passively observes WS-Discovery Hello and Bye announcements. Like
// Discoverer it binds through injectable seams so the lifecycle can be driven
// deterministically in tests with no multicast capability.
type Listener struct {
	source InterfaceSource
	listen ListenFactory
	filter interfaceFilter
	name   string
}

// NewListener returns a Listener configured by opts. Only the interface
// selection, loopback, and seam fields of Options are consulted; timeout and
// randomness do not apply to passive listening.
//
// When Options.Listen is nil the Listener uses the production multicast-join
// factory, which joins 239.255.255.250:3702 on each selected interface so that
// passive Hello and Bye announcements are actually received. Tests supply
// Options.Listen to inject a scripted connection and keep public CI free of any
// multicast capability.
func NewListener(opts Options) *Listener {
	source := opts.InterfaceSource
	if source == nil {
		source = SystemInterfaceSource{}
	}
	listen := opts.Listen
	if listen == nil {
		listen = multicastListenFactory
	}
	return &Listener{
		source: source,
		listen: listen,
		filter: interfaceFilter{includeLoopback: opts.IncludeLoopback},
		name:   opts.InterfaceName,
	}
}

// Listen observes Hello and Bye announcements until ctx is cancelled,
// delivering typed events on the returned channel. The channel is closed when
// listening stops. Every packet connection is closed on cancellation.
//
// Listen is the source-compatible API: it hides the errors channel. Callers
// that need to observe persistent terminal read errors or partial bind
// failures should use ListenStream instead. Internally Listen is a thin
// wrapper over ListenStream that drains and discards the errors channel.
//
// Partial-bind policy: when several interfaces are selected and some bind while
// others fail, the healthy bindings are kept and listening runs on them; the
// per-interface failures are not fatal. Listen returns an error only when NO
// interface could be bound. Once listening starts, cancellation is a clean,
// non-error shutdown.
func (l *Listener) Listen(ctx context.Context) (<-chan Event, error) {
	stream, err := l.ListenStream(ctx)
	if err != nil {
		return nil, err
	}
	// Drain errors so a reported error never blocks the stream when the caller
	// uses the events-only API.
	go func() {
		for range stream.Errors() {
		}
	}()
	return stream.Events(), nil
}

// ListenStream is the error-surfacing passive listening API. It behaves like
// Listen but returns a Stream whose Errors channel reports persistent terminal
// read errors per interface and any partial bind failures observed at setup.
//
// Delivered events are deduplicated by WS-Addressing MessageID with a bounded
// cache and filtered for stale/equal Hello/Bye via the WS-Discovery
// AppSequence (InstanceId, SequenceId, MessageNumber) per endpoint and
// interface. A datagram without AppSequence is not suppressed on sequence
// grounds (vendor tolerance), and a datagram without a MessageID is never
// collapsed into a single identity; both are still subject to Action/body
// coherence validation.
//
// Both the Events and Errors channels close when listening stops. Cancellation
// is a clean, non-error shutdown that closes every connection; the channels are
// not closed until every reader has stopped, so when Events is closed every
// connection has already been released.
func (l *Listener) ListenStream(ctx context.Context) (*Stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ifaces, err := resolveInterfaces(l.source, l.filter, l.name)
	if err != nil {
		return nil, err
	}

	type bound struct {
		iface Interface
		conn  PacketConn
	}
	conns := make([]bound, 0, len(ifaces))
	var (
		lastErr     error
		partialErrs []StreamError
	)
	closeBound := func() {
		for _, open := range conns {
			_ = open.conn.Close()
		}
	}
	for _, iface := range ifaces {
		if err := ctx.Err(); err != nil {
			closeBound()
			return nil, err
		}
		conn, err := l.listen(iface)
		if err != nil {
			// Keep going: a single interface that cannot bind must not discard
			// the interfaces that did. Remember the last error so a round that
			// binds nothing can report a concrete cause, and record it as a
			// non-terminal partial-bind error for the stream.
			lastErr = err
			partialErrs = append(partialErrs, StreamError{Interface: iface.Name, Err: err, Terminal: false})
			continue
		}
		if err := ctx.Err(); err != nil {
			_ = conn.Close()
			closeBound()
			return nil, err
		}
		conns = append(conns, bound{iface: iface, conn: conn})
	}
	if len(conns) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, ErrNoUsableInterfaces
	}

	// rawEvents and rawErrors carry pre-suppression lifecycle events and
	// per-reader errors from every reader to the single aggregation goroutine,
	// which owns the suppressor and emits the public events and errors.
	type rawEvent struct {
		event Event
		msg   parsedMessage
	}
	rawEvents := make(chan rawEvent)
	// Size the error channels so one setup error (partial bind) plus one
	// runtime terminal error PER interface can be delivered without blocking.
	// This guarantees a caller that temporarily reads only Events cannot
	// deadlock stream termination: every reader can report its single terminal
	// error into a buffer and then exit, allowAllReadersDone to fire and the
	// public channels to close, so the terminal errors remain observable (the
	// caller can drain Errors after Events closes) and shutdown stays clean.
	errorBuffer := len(conns) + len(partialErrs)
	rawErrors := make(chan StreamError, errorBuffer)

	events := make(chan Event)
	errorsCh := make(chan StreamError, errorBuffer)

	readerDone := make(chan struct{}, len(conns))
	allReadersDone := make(chan struct{})

	for i := range conns {
		go func(b bound) {
			defer func() { readerDone <- struct{}{} }()
			readLifecycle(ctx, b.iface, b.conn, func(ev Event, msg parsedMessage) bool {
				select {
				case rawEvents <- rawEvent{event: ev, msg: msg}:
					return true
				case <-ctx.Done():
					return false
				}
			}, func(serr StreamError) {
				select {
				case rawErrors <- serr:
				case <-ctx.Done():
				}
			})
		}(conns[i])
	}

	// Close connections when the context ends so blocked reads unblock. Stop
	// this coordinator when every reader has already exited on its own.
	go func() {
		select {
		case <-ctx.Done():
			for _, open := range conns {
				_ = open.conn.Close()
			}
		case <-allReadersDone:
		}
	}()

	// Signal when every reader has stopped so the aggregator can finish.
	go func() {
		for range conns {
			<-readerDone
		}
		close(allReadersDone)
	}()

	// The aggregator is the single owner of the suppressor. It forwards
	// suppression-filtered events and reader errors until every reader has
	// stopped, then closes the public channels. Because it does not return
	// until allReadersDone (readers stop promptly on cancellation, which also
	// closes every connection), the public channels close only after every
	// connection has been released.
	go func() {
		defer close(events)
		defer close(errorsCh)
		suppress := newSuppressor()

		// emitError forwards a stream error without blocking shutdown: if the
		// caller is not draining errors and the context has ended, the error is
		// dropped rather than wedging the aggregator.
		emitError := func(serr StreamError) {
			select {
			case errorsCh <- serr:
			case <-ctx.Done():
			}
		}
		// emitEvent forwards a filtered event, dropping it only if the context
		// ends mid-delivery so shutdown cannot wedge.
		emitEvent := func(ev Event) {
			select {
			case events <- ev:
			case <-ctx.Done():
			}
		}

		for _, serr := range partialErrs {
			emitError(serr)
		}

		for {
			select {
			case raw := <-rawEvents:
				if suppress.admit(raw.msg, raw.event.Interface) {
					emitEvent(raw.event)
				}
			case serr := <-rawErrors:
				emitError(serr)
			case <-allReadersDone:
				// Every reader has stopped and all connections are released.
				// Drain anything already queued, then close the channels.
				for {
					select {
					case raw := <-rawEvents:
						if suppress.admit(raw.msg, raw.event.Interface) {
							emitEvent(raw.event)
						}
					case serr := <-rawErrors:
						emitError(serr)
					default:
						return
					}
				}
			}
		}
	}()

	return &Stream{events: events, errors: errorsCh}, nil
}

// readLifecycle reads one connection, forwarding Hello and Bye events through
// deliver until the context ends, the connection closes, or the socket produces
// too many consecutive non-recoverable read errors. A persistent terminal read
// error is reported through reportErr before the reader returns. Datagrams are
// read into a buffer one byte larger than the accepted maximum so an oversized
// or truncated-at-max datagram is detected and rejected rather than silently
// parsed from a max-sized prefix.
//
// deliver returns false when the context ended mid-delivery, which stops the
// reader. The reader closes its own connection on return so a reader that gives
// up on a broken socket releases the descriptor promptly.
func readLifecycle(
	ctx context.Context,
	iface Interface,
	conn PacketConn,
	deliver func(Event, parsedMessage) bool,
	reportErr func(StreamError),
) {
	defer func() { _ = conn.Close() }()
	buf := make([]byte, maxDatagramSize+1)
	consecutiveErrors := 0
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, os.ErrClosed) {
				return
			}
			if isTimeout(err) {
				// A benign read timeout is the socket working normally; reset
				// the failure budget and keep listening.
				consecutiveErrors = 0
				continue
			}
			// A persistent non-timeout error (for example a hard socket fault)
			// must not spin. Tolerate a bounded number of consecutive failures,
			// then report a terminal error and terminate this reader.
			consecutiveErrors++
			if consecutiveErrors >= maxConsecutiveReadErrors {
				reportErr(StreamError{Interface: iface.Name, Err: err, Terminal: true})
				return
			}
			continue
		}
		consecutiveErrors = 0
		if n > maxDatagramSize {
			// Oversized datagram: reject rather than parse a prefix.
			continue
		}
		datagram := make([]byte, n)
		copy(datagram, buf[:n])
		messages, perr := parseMessages(datagram)
		if perr != nil {
			continue
		}
		if !forwardLifecycleEvents(messages, iface.Name, deliver) {
			return
		}
	}
}

// forwardLifecycleEvents projects each message into a lifecycle event and
// delivers it through deliver. It returns false when deliver reports the
// context ended, so the caller stops reading.
func forwardLifecycleEvents(messages []parsedMessage, ifaceName string, deliver func(Event, parsedMessage) bool) bool {
	for _, message := range messages {
		event, ok := lifecycleEvent(message, ifaceName)
		if !ok {
			continue
		}
		if !deliver(event, message) {
			return false
		}
	}
	return true
}

// lifecycleEvent projects a Hello or Bye message into a typed Event.
//
// Action validation: WS-Discovery Hello and Bye carry a WS-Addressing Action
// identifying the notification. When an Action is present it must be the
// recognised Hello or Bye action for an accepted discovery family and must
// match the body element, so a datagram whose action contradicts its body (for
// example a Bye action on a Hello body) is rejected. An absent Action is
// tolerated for vendor leniency: some cameras omit the header, and the body
// element already identifies the notification unambiguously; this tolerance is
// deliberate and is the only case in which a lifecycle event is accepted
// without a validated action. A message that carries conflicting discovery or
// addressing families is already rejected by the parser, so it never reaches
// here.
func lifecycleEvent(message parsedMessage, ifaceName string) (Event, bool) {
	var (
		eventType EventType
		local     string
	)
	switch message.Kind {
	case kindHello:
		eventType, local = EventHello, actionHello
	case kindBye:
		eventType, local = EventBye, actionBye
	default:
		return Event{}, false
	}
	if message.Action != "" && message.Action != discoveryAction(message.DiscoveryNamespace, local) {
		// A present but contradictory, cross-family, or unrecognised action is rejected.
		return Event{}, false
	}
	return Event{
		Type:              eventType,
		EndpointReference: message.EndpointReference,
		XAddrs:            sortedCopy(message.XAddrs),
		Scopes:            sortedCopy(message.Scopes),
		Types:             sortedCopy(message.Types),
		Interface:         ifaceName,
	}, true
}
