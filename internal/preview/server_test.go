package preview

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const testOrigin = "wails://wails"

func newTestServer(t *testing.T) *Server {
	t.Helper()
	server, err := NewServer([]string{testOrigin})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Close(ctx)
	})
	return server
}

func request(t *testing.T, server *Server, method, path string, mutate func(*http.Request)) *http.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, method, server.BaseURL()+path, nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+server.Token())
	req.Header.Set("Origin", testOrigin)
	if mutate != nil {
		mutate(req)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func TestServerBindsLoopbackWithRandomCredentials(t *testing.T) {
	t.Parallel()
	first := newTestServer(t)
	second := newTestServer(t)
	if !strings.HasPrefix(first.BaseURL(), "http://127.0.0.1:") {
		t.Fatalf("BaseURL() = %q, want IPv4 loopback", first.BaseURL())
	}
	if len(first.Token()) != 64 || first.Token() == second.Token() {
		t.Fatal("tokens must be 256-bit and unique per server")
	}
	a, _ := first.NewSession()
	b, _ := first.NewSession()
	if len(a.ID()) != 32 || a.ID() == b.ID() {
		t.Fatal("session IDs must be 128-bit and unique")
	}
}

func TestServerRejectsUnauthorizedRequests(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	session, err := server.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	session.SetInit("avc1.42c01f", []byte("init"))
	cases := map[string]struct {
		method string
		path   string
		mutate func(*http.Request)
		want   int
	}{
		"missing token":  {http.MethodGet, session.Path(), func(r *http.Request) { r.Header.Del("Authorization") }, http.StatusUnauthorized},
		"wrong token":    {http.MethodGet, session.Path(), func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") }, http.StatusUnauthorized},
		"foreign origin": {http.MethodGet, session.Path(), func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, http.StatusForbidden},
		"rebound host":   {http.MethodGet, session.Path(), func(r *http.Request) { r.Host = "attacker.example" }, http.StatusForbidden},
		"wrong method":   {http.MethodPost, session.Path(), nil, http.StatusMethodNotAllowed},
		"unknown":        {http.MethodGet, sessionPathPrefix + strings.Repeat("0", 32), nil, http.StatusNotFound},
		"outside prefix": {http.MethodGet, "/other", nil, http.StatusNotFound},
	}
	for name, tc := range cases {
		response := request(t, server, tc.method, tc.path, tc.mutate) //nolint:bodyclose // request closes the body in t.Cleanup
		if response.StatusCode != tc.want {
			t.Errorf("%s: status = %d, want %d", name, response.StatusCode, tc.want)
		}
	}
}

func TestServerAnswersPreflightOnlyForAllowedOrigin(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	response := request(t, server, http.MethodOptions, "/v1/sessions/x", func(r *http.Request) { r.Header.Del("Authorization") }) //nolint:bodyclose // request closes the body in t.Cleanup
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status = %d", response.StatusCode)
	}
	if got := response.Header.Get("Access-Control-Allow-Origin"); got != testOrigin {
		t.Fatalf("Allow-Origin = %q", got)
	}
	if got := response.Header.Get("Access-Control-Allow-Headers"); got != "Authorization" {
		t.Fatalf("Allow-Headers = %q", got)
	}
}

func readExactly(t *testing.T, body io.Reader, size int) []byte {
	t.Helper()
	buffer := make([]byte, size)
	if _, err := io.ReadFull(body, buffer); err != nil {
		t.Fatalf("ReadFull() error = %v", err)
	}
	return buffer
}

func TestLateViewerReceivesInitAndCurrentGroupOfPictures(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	session, err := server.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	session.SetInit("avc1.42c01f", []byte("INIT"))
	session.WriteFragment([]byte("k1"), true)
	session.WriteFragment([]byte("p1"), false)
	session.WriteFragment([]byte("K2"), true)
	session.WriteFragment([]byte("P2"), false)

	response := request(t, server, http.MethodGet, session.Path(), nil) //nolint:bodyclose // request closes the body in t.Cleanup
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if got := response.Header.Get("X-OVM-Codec"); got != "avc1.42c01f" {
		t.Fatalf("codec header = %q", got)
	}
	if got := response.Header.Get("Access-Control-Expose-Headers"); got != "X-OVM-Codec" {
		t.Fatalf("expose headers = %q", got)
	}
	if got := readExactly(t, response.Body, 8); !bytes.Equal(got, []byte("INITK2P2")) {
		t.Fatalf("replay = %q, want INITK2P2", got)
	}
	waitForViewers(t, session, 1)
	session.WriteFragment([]byte("p3"), false)
	if got := readExactly(t, response.Body, 2); !bytes.Equal(got, []byte("p3")) {
		t.Fatalf("live fragment = %q", got)
	}
	session.Close()
	if rest, _ := io.ReadAll(response.Body); len(rest) != 0 {
		t.Fatalf("unexpected bytes after close: %q", rest)
	}
}

func TestViewerWithoutReplayWaitsForKeyframe(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	session, err := server.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	session.SetInit("avc1.42c01f", []byte("INIT"))
	response := request(t, server, http.MethodGet, session.Path(), nil) //nolint:bodyclose // request closes the body in t.Cleanup
	if got := readExactly(t, response.Body, 4); string(got) != "INIT" {
		t.Fatalf("init = %q", got)
	}
	waitForViewers(t, session, 1)
	session.WriteFragment([]byte("p0"), false)
	session.WriteFragment([]byte("K1"), true)
	if got := readExactly(t, response.Body, 2); string(got) != "K1" {
		t.Fatalf("first fragment = %q, want keyframe K1", got)
	}
}

func TestSlowViewerIsDisconnectedWithoutBlockingProducer(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	session, err := server.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	session.SetInit("avc1.42c01f", []byte("INIT"))
	response := request(t, server, http.MethodGet, session.Path(), nil) //nolint:bodyclose // request closes the body in t.Cleanup
	readExactly(t, response.Body, 4)
	waitForViewers(t, session, 1)

	done := make(chan struct{})
	go func() {
		defer close(done)
		session.WriteFragment(bytes.Repeat([]byte{1}, 1024), true)
		for i := 0; i < viewerQueueLength*4; i++ {
			session.WriteFragment(bytes.Repeat([]byte{2}, 64<<10), false)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("producer blocked on a slow viewer")
	}
	waitForViewers(t, session, 0)
}

func TestNewInitDisconnectsViewersAndServerCloseEndsSessions(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	session, err := server.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	session.SetInit("avc1.42c01f", []byte("INIT"))
	response := request(t, server, http.MethodGet, session.Path(), nil) //nolint:bodyclose // request closes the body in t.Cleanup
	readExactly(t, response.Body, 4)
	waitForViewers(t, session, 1)
	session.SetInit("avc1.640028", []byte("NEW"))
	if rest, _ := io.ReadAll(response.Body); len(rest) != 0 {
		t.Fatalf("unexpected bytes after reinit: %q", rest)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := server.NewSession(); !errors.Is(err, ErrClosed) {
		t.Fatalf("NewSession() after close error = %v, want ErrClosed", err)
	}
}

func waitForViewers(t *testing.T, session *Session, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for session.Viewers() != want {
		if time.Now().After(deadline) {
			t.Fatalf("viewers = %d, want %d", session.Viewers(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSnapshotSessionServesFreshImagesWithTheSameProtections(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	var calls byte
	snapshot, err := server.NewSnapshotSession(func(context.Context) ([]byte, error) {
		calls++
		if calls == 3 {
			return nil, errors.New("camera unavailable")
		}
		return []byte{0xFF, 0xD8, calls, 0xFF, 0xD9}, nil
	})
	if err != nil {
		t.Fatalf("NewSnapshotSession() error = %v", err)
	}
	for want := 1; want <= 2; want++ {
		response := request(t, server, http.MethodGet, snapshot.Path(), nil) //nolint:bodyclose // request closes the body in t.Cleanup
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "image/jpeg" || body[2] != byte(want) {
			t.Fatalf("fetch %d: status=%d type=%q body=% x", want, response.StatusCode, response.Header.Get("Content-Type"), body)
		}
	}
	if response := request(t, server, http.MethodGet, snapshot.Path(), nil); response.StatusCode != http.StatusBadGateway { //nolint:bodyclose // request closes the body in t.Cleanup
		t.Fatalf("failed fetch status = %d, want 502", response.StatusCode)
	}
	unauthorized := request(t, server, http.MethodGet, snapshot.Path(), func(r *http.Request) { r.Header.Del("Authorization") }) //nolint:bodyclose // request closes the body in t.Cleanup
	if unauthorized.StatusCode != http.StatusUnauthorized || calls != 3 {
		t.Fatalf("unauthorized status = %d, calls = %d", unauthorized.StatusCode, calls)
	}
	snapshot.Close()
	if response := request(t, server, http.MethodGet, snapshot.Path(), nil); response.StatusCode != http.StatusNotFound { //nolint:bodyclose // request closes the body in t.Cleanup
		t.Fatalf("closed session status = %d, want 404", response.StatusCode)
	}
}
