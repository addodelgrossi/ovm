// Package preview serves live fragmented-MP4 previews to the desktop WebView
// over a token-protected loopback HTTP bridge.
//
// The server binds only to 127.0.0.1 on an ephemeral port, requires a random
// bearer token compared in constant time, accepts only its own Host header
// (defeating DNS rebinding), answers CORS only for configured origins, and
// gives every viewer a bounded queue so a slow WebView can never stall the
// producer. A late viewer receives the initialization segment and the current
// group of pictures, so playback starts at a keyframe without waiting.
package preview

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// viewerQueueLength bounds buffered fragments per viewer; a viewer that
	// falls this far behind is disconnected and may reconnect at a keyframe.
	viewerQueueLength = 256
	// maxReplayBytes bounds the cached group of pictures replayed to a new
	// viewer. A larger GOP is not replayed; the viewer waits for a keyframe.
	maxReplayBytes = 16 << 20

	sessionPathPrefix = "/v1/sessions/"
)

// ErrClosed reports use of a closed server or session.
var ErrClosed = errors.New("preview: closed")

// Server is the loopback preview bridge. It is safe for concurrent use.
type Server struct {
	listener net.Listener
	http     *http.Server
	token    string
	host     string
	origins  map[string]struct{}

	mu       sync.Mutex
	sessions map[string]*Session
	closed   bool
}

// NewServer starts a bridge that answers CORS requests only from the given
// page origins, such as "wails://wails".
func NewServer(allowedOrigins []string) (*Server, error) {
	token, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	server := &Server{
		listener: listener,
		token:    token,
		host:     listener.Addr().String(),
		origins:  make(map[string]struct{}, len(allowedOrigins)),
		sessions: make(map[string]*Session),
	}
	for _, origin := range allowedOrigins {
		server.origins[origin] = struct{}{}
	}
	server.http = &http.Server{
		Handler:           http.HandlerFunc(server.serveHTTP),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	go func() { _ = server.http.Serve(listener) }()
	return server, nil
}

// BaseURL is the bridge origin, for example "http://127.0.0.1:49152".
func (s *Server) BaseURL() string { return "http://" + s.host }

// Token is the bearer token the WebView must present.
func (s *Server) Token() string { return s.token }

// NewSession registers a publishable stream with a random identifier.
func (s *Server) NewSession() (*Session, error) {
	id, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	session := &Session{id: id, viewers: make(map[*viewer]struct{}), done: make(chan struct{})}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	s.sessions[id] = session
	session.unregister = func() {
		s.mu.Lock()
		delete(s.sessions, id)
		s.mu.Unlock()
	}
	return session, nil
}

// Close stops accepting requests, ends every session, and waits for handlers
// to return or ctx to expire.
func (s *Server) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	sessions := make([]*Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.mu.Unlock()
	for _, session := range sessions {
		session.Close()
	}
	return s.http.Shutdown(ctx)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Host != s.host {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	origin := r.Header.Get("Origin")
	if origin != "" {
		if _, ok := s.origins[origin]; !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		header := w.Header()
		header.Set("Access-Control-Allow-Origin", origin)
		header.Set("Access-Control-Expose-Headers", "X-OVM-Codec")
		header.Set("Vary", "Origin")
	}
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization")
		w.Header().Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	presented, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(presented), []byte(s.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, ok := strings.CutPrefix(r.URL.Path, sessionPathPrefix)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	session := s.sessions[id]
	s.mu.Unlock()
	if session == nil {
		http.NotFound(w, r)
		return
	}
	session.serve(w, r)
}

// Session is one published preview stream. The producer calls SetInit before
// writing fragments and Close when the stream ends.
type Session struct {
	id         string
	unregister func()

	mu       sync.Mutex
	codec    string
	init     []byte
	replay   [][]byte
	replayed int
	viewers  map[*viewer]struct{}
	ready    bool
	closed   bool
	done     chan struct{}
}

type viewer struct {
	queue   chan []byte
	dropped chan struct{}
	// waitKey holds live fragments back until a keyframe when the viewer
	// joined without a replayable group of pictures.
	waitKey bool
}

// ID is the session identifier used in the preview path.
func (s *Session) ID() string { return s.id }

// Path is the request path for this session.
func (s *Session) Path() string { return sessionPathPrefix + s.id }

// SetInit publishes the codecs parameter and initialization segment. A new
// initialization (for example after a resolution change) disconnects current
// viewers so they restart their decoder.
func (s *Session) SetInit(codec string, init []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.dropViewersLocked()
	s.codec = codec
	s.init = append([]byte(nil), init...)
	s.replay = nil
	s.replayed = 0
	s.ready = true
}

// WriteFragment fans a media fragment out to viewers. key marks a fragment that
// starts with a random-access sample and therefore restarts the replay cache.
func (s *Session) WriteFragment(fragment []byte, key bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.ready {
		return
	}
	if key {
		s.replay = s.replay[:0]
		s.replayed = 0
	}
	if s.replay != nil || key {
		if s.replayed+len(fragment) <= maxReplayBytes {
			s.replay = append(s.replay, fragment)
			s.replayed += len(fragment)
		} else {
			s.replay = nil
			s.replayed = 0
		}
	}
	for v := range s.viewers {
		if v.waitKey {
			if !key {
				continue
			}
			v.waitKey = false
		}
		select {
		case v.queue <- fragment:
		default:
			s.dropViewerLocked(v)
		}
	}
}

// Close ends the session and disconnects every viewer. It is idempotent.
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.dropViewersLocked()
	close(s.done)
	s.mu.Unlock()
	if s.unregister != nil {
		s.unregister()
	}
}

// Viewers reports the connected viewer count.
func (s *Session) Viewers() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.viewers)
}

func (s *Session) dropViewersLocked() {
	for v := range s.viewers {
		s.dropViewerLocked(v)
	}
}

func (s *Session) dropViewerLocked(v *viewer) {
	delete(s.viewers, v)
	close(v.dropped)
}

func (s *Session) attach() (*viewer, string, []byte, [][]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.ready {
		return nil, "", nil, nil, false
	}
	v := &viewer{
		queue:   make(chan []byte, viewerQueueLength),
		dropped: make(chan struct{}),
		waitKey: len(s.replay) == 0,
	}
	s.viewers[v] = struct{}{}
	return v, s.codec, s.init, append([][]byte(nil), s.replay...), true
}

func (s *Session) detach(v *viewer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.viewers[v]; ok {
		s.dropViewerLocked(v)
	}
}

func (s *Session) waitReady(ctx context.Context) bool {
	for {
		s.mu.Lock()
		ready, closed := s.ready, s.closed
		s.mu.Unlock()
		if ready {
			return true
		}
		if closed {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-s.done:
			return false
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (s *Session) serve(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !s.waitReady(ctx) {
		http.Error(w, "stream unavailable", http.StatusServiceUnavailable)
		return
	}
	v, codec, init, replay, ok := s.attach()
	if !ok {
		http.Error(w, "stream unavailable", http.StatusServiceUnavailable)
		return
	}
	defer s.detach(v)

	flusher, _ := w.(http.Flusher)
	header := w.Header()
	header.Set("Content-Type", "video/mp4")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-OVM-Codec", codec)
	w.WriteHeader(http.StatusOK)
	write := func(data []byte) bool {
		if _, err := w.Write(data); err != nil {
			return false
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	}
	if !write(init) {
		return
	}
	for _, fragment := range replay {
		if !write(fragment) {
			return
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-v.dropped:
			// Drain fragments queued before the drop, then end the response.
			for {
				select {
				case fragment := <-v.queue:
					if !write(fragment) {
						return
					}
				default:
					return
				}
			}
		case fragment := <-v.queue:
			if !write(fragment) {
				return
			}
		}
	}
}

func randomHex(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}
