package app

import (
	"bytes"
	"context"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/pkg/onvif/auth"
	"github.com/addodelgrossi/ovm/pkg/onvif/device"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// TestDeviceAndMediaShareOneResampleGateAndDistinctTokens proves the Stage 1
// unification: a single *session drives both a Device operation (through the
// typed authDeviceSeam.call delegate) and a Media operation (through
// session.invokeWithAuth directly). When both see a NotAuthorized fault on
// their first attempt, exactly ONE shared clock resample runs for the whole use
// case, and every logical attempt (Device first, Device retry, Media first,
// Media retry) carries a fresh, distinct WS-Security UsernameToken header.
func TestDeviceAndMediaShareOneResampleGateAndDistinctTokens(t *testing.T) {
	var resamples int32
	clockCall := func(ctx context.Context) (device.SystemDateAndTime, error) {
		atomic.AddInt32(&resamples, 1)
		return device.SystemDateAndTime{UTC: time.Now().UTC(), UTCValid: true}, nil
	}

	sess := newTestSession(&Credentials{username: "u", password: "p"},
		auth.ClockSampler{Attempts: 1, Now: time.Now}, clockCall)
	seam := &authDeviceSeam{session: sess}

	var mu sync.Mutex
	var headers [][]byte
	record := func(h []byte) {
		mu.Lock()
		defer mu.Unlock()
		cp := append([]byte(nil), h...)
		headers = append(headers, cp)
	}

	// A Device operation: fail NotAuthorized on the first attempt, succeed on
	// the retry. The seam injects the session's fresh WSSE header into opts.
	var deviceAttempts int32
	deviceErr := seam.call(context.Background(), func(ctx context.Context, opts []device.RequestOptions) error {
		if len(opts) == 0 || len(opts[0].HeaderXML) == 0 {
			t.Error("device attempt carried no WS-Security header")
		} else {
			record(opts[0].HeaderXML)
		}
		if atomic.AddInt32(&deviceAttempts, 1) == 1 {
			return soap.ErrNotAuthorized
		}
		return nil
	})
	if deviceErr != nil {
		t.Fatalf("device call = %v, want success after one retry", deviceErr)
	}

	// A Media operation driven through the SAME session: identical retry shape.
	var mediaAttempts int32
	mediaErr := sess.invokeWithAuth(context.Background(), func(ctx context.Context, header []byte) error {
		if len(header) == 0 {
			t.Error("media attempt carried no WS-Security header")
		} else {
			record(header)
		}
		if atomic.AddInt32(&mediaAttempts, 1) == 1 {
			return soap.ErrNotAuthorized
		}
		return nil
	})
	if mediaErr != nil {
		t.Fatalf("media call = %v, want success after one retry", mediaErr)
	}

	// Exactly one shared resample across BOTH the Device and Media retries.
	if got := atomic.LoadInt32(&resamples); got != 1 {
		t.Fatalf("shared resample ran %d times across Device+Media, want exactly 1", got)
	}

	// Four logical attempts total, each with a fresh distinct token.
	mu.Lock()
	defer mu.Unlock()
	if len(headers) != 4 {
		t.Fatalf("captured %d WSSE headers, want 4 (device x2 + media x2)", len(headers))
	}
	for i := 0; i < len(headers); i++ {
		for j := i + 1; j < len(headers); j++ {
			if bytes.Equal(headers[i], headers[j]) {
				t.Fatalf("attempts %d and %d reused an identical UsernameToken; each must be fresh", i, j)
			}
		}
	}
}

// TestSnapshotClientSharesSessionRoundTripperWithoutWSSE proves the snapshot GET
// reuses the session's exact authenticated RoundTripper (same Transport pointer
// and therefore the same Digest challenge cache), installs the GET redirect
// policy, and carries no WS-Security header. The snapshotHTTPClient helper has
// no header parameter, so it is structurally impossible for it to attach WSSE;
// this test additionally pins the shared-transport invariant.
func TestSnapshotClientSharesSessionRoundTripperWithoutWSSE(t *testing.T) {
	creds, err := NewCredentials("admin", "pw", false)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	sess, err := newSession(sessionConfig{
		endpointURL: "http://camera.example/onvif/device_service",
		creds:       creds,
		timeout:     5 * time.Second,
		sampler:     auth.ClockSampler{Attempts: negotiationAttempts},
		clock:       time.Now,
	})
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}

	// The session's authenticated transport must be the origin-scoped Digest
	// transport, not a bare default.
	if sess.httpClient.Transport == nil {
		t.Fatal("session HTTP client has no authenticated transport")
	}
	if _, ok := sess.httpClient.Transport.(*auth.DigestTransport); !ok {
		t.Fatalf("session transport = %T, want *auth.DigestTransport", sess.httpClient.Transport)
	}

	cameraURL, _ := url.Parse("http://camera.example/onvif/device_service")
	snapClient := snapshotHTTPClient(sess.httpClient, cameraURL)

	// Exact same RoundTripper pointer: the GET shares the Digest cache rather
	// than constructing a second authenticated transport.
	if snapClient.Transport != sess.httpClient.Transport {
		t.Fatal("snapshot client does not share the session's exact RoundTripper")
	}
	if snapClient.CheckRedirect == nil {
		t.Fatal("snapshot client must install a redirect policy for the GET")
	}
}
