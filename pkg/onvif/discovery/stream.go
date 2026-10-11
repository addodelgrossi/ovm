package discovery

import (
	"container/list"
	"fmt"
)

// maxMessageIDCache bounds the per-interface MessageID dedupe cache. It is
// large enough to absorb a realistic burst of distinct announcements while
// keeping each cache strictly bounded against a hostile flood of unique
// identifiers.
const maxMessageIDCache = 4096

// maxDedupeInterfaces bounds how many distinct interfaces keep an independent
// MessageID dedupe cache. Interfaces are discovered once at bind time and are
// few in practice; the bound is a defensive ceiling so a pathological source
// cannot force unbounded cache allocation.
const maxDedupeInterfaces = 256

// maxEndpointStates bounds how many distinct (endpoint, interface) AppSequence
// states are retained. The least-recently-updated state is evicted when the
// bound is exceeded so a flood of distinct endpoints cannot grow state without
// limit. Eviction is deterministic (strict LRU by update order).
const maxEndpointStates = 4096

// maxSequencesPerEndpoint bounds how many distinct SequenceId message-number
// positions one (endpoint, interface) state tracks within a single InstanceId.
// A new InstanceId resets this set; within an instance the least-recently-used
// SequenceId is evicted past the bound.
const maxSequencesPerEndpoint = 64

// StreamError is a non-fatal or terminal error surfaced by a passive listening
// stream. It names the interface it concerns (empty for a stream-wide error) so
// a caller can attribute a persistent read failure or a partial bind failure to
// a specific binding.
type StreamError struct {
	// Interface is the interface the error concerns, or "" when it is not
	// specific to one interface.
	Interface string
	// Err is the underlying error.
	Err error
	// Terminal reports whether the reader for this interface has stopped. A
	// terminal error means no further events will arrive on that interface; a
	// non-terminal error (for example a tolerated partial bind failure at
	// setup) is informational and listening continues on the other bindings.
	Terminal bool
}

func (e StreamError) Error() string {
	if e.Interface != "" {
		return fmt.Sprintf("discovery stream (%s): %v", e.Interface, e.Err)
	}
	return fmt.Sprintf("discovery stream: %v", e.Err)
}

func (e StreamError) Unwrap() error { return e.Err }

// Stream is the error-surfacing passive listening API. Unlike Listen, which
// yields only an events channel, a Stream also exposes an errors channel so a
// caller can observe persistent terminal read errors (and, when present,
// partial bind failures reported at setup). Both channels are closed when
// listening stops; a caller that only cares about events may ignore Errors but
// SHOULD drain it to avoid blocking the stream when errors are reported.
//
// Events are deduplicated by WS-Addressing MessageID per interface with a
// bounded cache, and stale or equal Hello/Bye are suppressed per endpoint and
// interface using the WS-Discovery AppSequence (InstanceId, SequenceId,
// MessageNumber). See NewListener and Listener.ListenStream.
type Stream struct {
	events <-chan Event
	errors <-chan StreamError
}

// Events returns the channel of deduplicated, suppression-filtered lifecycle
// events. It is closed when listening stops.
func (s *Stream) Events() <-chan Event { return s.events }

// Errors returns the channel of surfaced stream errors. It is closed when
// listening stops.
func (s *Stream) Errors() <-chan StreamError { return s.errors }

// messageIDCache is a bounded, insertion-ordered set used to dedupe repeated
// WS-Addressing MessageIDs. When full the oldest identifier is evicted.
type messageIDCache struct {
	limit int
	order *list.List
	index map[string]*list.Element
}

func newMessageIDCache(limit int) *messageIDCache {
	return &messageIDCache{limit: limit, order: list.New(), index: make(map[string]*list.Element)}
}

// seen reports whether id was already present and records it otherwise. An
// empty id is never cached (so a vendor that omits MessageID is not collapsed
// into a single identity) and always reports false.
func (c *messageIDCache) seen(id string) bool {
	if id == "" {
		return false
	}
	if _, ok := c.index[id]; ok {
		return true
	}
	elem := c.order.PushBack(id)
	c.index[id] = elem
	if c.order.Len() > c.limit {
		oldest := c.order.Front()
		if oldest != nil {
			c.order.Remove(oldest)
			delete(c.index, oldest.Value.(string))
		}
	}
	return false
}

// endpointKey identifies a lifecycle source for AppSequence suppression: the
// endpoint reference together with the interface it was observed on. Keying by
// interface too keeps a device heard on two interfaces from suppressing itself
// across them, and keeps MessageID dedupe independent per interface.
type endpointKey struct {
	endpoint string
	iface    string
}

// endpointSequenceState records the AppSequence position for one
// (endpoint, interface). InstanceId ordering dominates across SequenceId: a
// lower instance is stale even under a new SequenceId, and a higher instance
// resets all per-SequenceId message-number tracking. Within a single instance
// each SequenceId tracks its own highest accepted MessageNumber independently.
type endpointSequenceState struct {
	instanceID uint64
	// numbers maps SequenceId to the highest accepted MessageNumber within the
	// current instanceID. It is reset whenever a strictly greater instanceID is
	// accepted. seqOrder records insertion order for deterministic LRU eviction
	// past maxSequencesPerEndpoint.
	numbers  map[string]uint64
	seqOrder *list.List
	seqElem  map[string]*list.Element
}

func newEndpointSequenceState(instanceID uint64) *endpointSequenceState {
	return &endpointSequenceState{
		instanceID: instanceID,
		numbers:    make(map[string]uint64),
		seqOrder:   list.New(),
		seqElem:    make(map[string]*list.Element),
	}
}

// recordSequence stores the highest accepted MessageNumber for sequenceID and
// bounds the number of tracked SequenceIds with deterministic LRU eviction.
func (st *endpointSequenceState) recordSequence(sequenceID string, number uint64) {
	if elem, ok := st.seqElem[sequenceID]; ok {
		st.seqOrder.MoveToBack(elem)
		st.numbers[sequenceID] = number
		return
	}
	elem := st.seqOrder.PushBack(sequenceID)
	st.seqElem[sequenceID] = elem
	st.numbers[sequenceID] = number
	if st.seqOrder.Len() > maxSequencesPerEndpoint {
		oldest := st.seqOrder.Front()
		if oldest != nil {
			st.seqOrder.Remove(oldest)
			evicted := oldest.Value.(string)
			delete(st.seqElem, evicted)
			delete(st.numbers, evicted)
		}
	}
}

// suppressor applies WS-Discovery AppSequence staleness suppression and
// per-interface MessageID deduplication for one stream. It is not safe for
// concurrent use; the single aggregation goroutine owns it.
type suppressor struct {
	// ids holds one MessageID dedupe cache per interface so the same MessageID
	// observed independently on two interfaces emits once on each.
	ids      map[string]*messageIDCache
	idsOrder *list.List
	idsElem  map[string]*list.Element

	// seqs holds one AppSequence state per (endpoint, interface), bounded with
	// deterministic LRU eviction.
	seqs      map[endpointKey]*endpointSequenceState
	seqsOrder *list.List
	seqsElem  map[endpointKey]*list.Element
}

func newSuppressor() *suppressor {
	return &suppressor{
		ids:       make(map[string]*messageIDCache),
		idsOrder:  list.New(),
		idsElem:   make(map[string]*list.Element),
		seqs:      make(map[endpointKey]*endpointSequenceState),
		seqsOrder: list.New(),
		seqsElem:  make(map[endpointKey]*list.Element),
	}
}

// cacheFor returns the per-interface MessageID cache, creating it on first use
// and bounding the number of tracked interfaces with deterministic LRU
// eviction so a pathological source cannot force unbounded allocation.
func (s *suppressor) cacheFor(ifaceName string) *messageIDCache {
	if cache, ok := s.ids[ifaceName]; ok {
		s.idsOrder.MoveToBack(s.idsElem[ifaceName])
		return cache
	}
	cache := newMessageIDCache(maxMessageIDCache)
	s.ids[ifaceName] = cache
	s.idsElem[ifaceName] = s.idsOrder.PushBack(ifaceName)
	if s.idsOrder.Len() > maxDedupeInterfaces {
		oldest := s.idsOrder.Front()
		if oldest != nil {
			s.idsOrder.Remove(oldest)
			evicted := oldest.Value.(string)
			delete(s.ids, evicted)
			delete(s.idsElem, evicted)
		}
	}
	return cache
}

// admit reports whether a lifecycle message should be delivered. It suppresses
// a message whose MessageID was already seen ON THE SAME INTERFACE (a
// retransmitted announcement) and a Hello/Bye that is stale or equal relative
// to the last accepted AppSequence for its endpoint and interface.
//
// Per-interface MessageID dedupe: the same MessageID observed independently on
// two interfaces is NOT collapsed; each interface emits it once. This matches
// multi-homed reception where a device multicasts to several segments and the
// host receives the datagram on each binding.
//
// AppSequence policy (per endpoint+interface), with InstanceId DOMINANT across
// SequenceId:
//   - a strictly greater InstanceId is a newer device boot: accepted, and ALL
//     per-SequenceId message-number state for that endpoint+interface is reset;
//   - a lower InstanceId is from an older boot: suppressed as stale, EVEN when
//     it carries a SequenceId never seen before;
//   - at the SAME InstanceId each SequenceId is independent: a strictly greater
//     MessageNumber within that SequenceId is accepted, an equal or lower one
//     is suppressed as stale/duplicate, and a SequenceId first seen within the
//     current instance is accepted.
//
// Missing AppSequence: a message without AppSequence cannot be ordered, so it
// is NOT suppressed on sequence grounds (vendor tolerance); MessageID dedupe
// still applies per interface when a MessageID is present.
func (s *suppressor) admit(message parsedMessage, ifaceName string) bool {
	if s.cacheFor(ifaceName).seen(message.MessageID) {
		return false
	}
	if !message.HasAppSequence {
		return true
	}
	key := endpointKey{endpoint: message.EndpointReference, iface: ifaceName}
	seq := message.AppSequence

	st, ok := s.seqs[key]
	if !ok {
		st = newEndpointSequenceState(seq.InstanceID)
		st.recordSequence(seq.SequenceID, seq.MessageNumber)
		s.putState(key, st)
		return true
	}
	// Touch for LRU on every observation so active endpoints are retained.
	s.seqsOrder.MoveToBack(s.seqsElem[key])

	switch {
	case seq.InstanceID > st.instanceID:
		// Newer boot generation: reset all per-SequenceId state.
		fresh := newEndpointSequenceState(seq.InstanceID)
		fresh.recordSequence(seq.SequenceID, seq.MessageNumber)
		s.seqs[key] = fresh
		return true
	case seq.InstanceID < st.instanceID:
		// Older boot generation is stale even under a brand-new SequenceId.
		return false
	default:
		// Same instance: each SequenceId is independent.
		prev, seen := st.numbers[seq.SequenceID]
		if !seen {
			st.recordSequence(seq.SequenceID, seq.MessageNumber)
			return true
		}
		if seq.MessageNumber > prev {
			st.recordSequence(seq.SequenceID, seq.MessageNumber)
			return true
		}
		return false
	}
}

// putState inserts a new endpoint state and bounds the number of tracked
// (endpoint, interface) states with deterministic LRU eviction.
func (s *suppressor) putState(key endpointKey, st *endpointSequenceState) {
	s.seqs[key] = st
	s.seqsElem[key] = s.seqsOrder.PushBack(key)
	if s.seqsOrder.Len() > maxEndpointStates {
		oldest := s.seqsOrder.Front()
		if oldest != nil {
			s.seqsOrder.Remove(oldest)
			evicted := oldest.Value.(endpointKey)
			delete(s.seqs, evicted)
			delete(s.seqsElem, evicted)
		}
	}
}
