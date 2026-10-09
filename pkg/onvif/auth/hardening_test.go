package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestDigestTransportPrefersStrongestSupportedChallenge(t *testing.T) {
	t.Parallel()

	var algorithm string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization := r.Header.Get("Authorization")
		if authorization == "" {
			w.Header().Add("WWW-Authenticate", `Digest realm="camera", nonce="md5", algorithm=MD5, qop="auth"`)
			w.Header().Add("WWW-Authenticate", `Digest realm="camera", nonce="sha", algorithm=SHA-256, qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		parameters, err := parseSchemeParameters(authorization, "Digest")
		if err != nil {
			t.Errorf("parse Authorization: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		algorithm = parameters["algorithm"]
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
		Credentials: NewCredentials("operator", "secret"),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatalf("close response: %v", closeErr)
	}
	if algorithm != "SHA-256" {
		t.Fatalf("selected algorithm = %q, want SHA-256", algorithm)
	}
}

func TestDigestNonceCountsRemainMonotonicAcrossNonceGenerations(t *testing.T) {
	t.Parallel()

	scope, err := url.Parse("https://device.invalid")
	if err != nil {
		t.Fatalf("parse scope: %v", err)
	}
	transport, err := NewDigestTransport(DigestTransportConfig{
		Scope:       scope,
		Credentials: NewCredentials("operator", "secret"),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	if err != nil {
		t.Fatalf("NewDigestTransport() error = %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, scope.String(), nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	first := &cacheRecord{kind: recordDigest, challenge: digestChallenge{realm: "camera", nonce: "first", algorithm: "SHA-256", qop: "auth"}, counter: &digestNonceCounter{}}
	second := &cacheRecord{kind: recordDigest, challenge: digestChallenge{realm: "camera", nonce: "second", algorithm: "SHA-256", qop: "auth", stale: true}, counter: &digestNonceCounter{}}

	counts := make([]string, 0, 4)
	for _, record := range []*cacheRecord{first, second, first, second} {
		authorization, authErr := transport.digestAuthorization(record, request)
		if authErr != nil {
			t.Fatalf("digestAuthorization() error = %v", authErr)
		}
		parameters, parseErr := parseSchemeParameters(authorization, "Digest")
		if parseErr != nil {
			t.Fatalf("parse authorization: %v", parseErr)
		}
		counts = append(counts, parameters["nc"])
	}
	want := []string{"00000001", "00000001", "00000002", "00000002"}
	for index := range want {
		if counts[index] != want[index] {
			t.Fatalf("nonce counts = %v, want %v", counts, want)
		}
	}
}

func TestBasicCacheIsLimitedToChallengedProtectionSpace(t *testing.T) {
	t.Parallel()

	adminSawAuthorization := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/onvif/device_service":
			if r.Header.Get("Authorization") == "" {
				w.Header().Set("WWW-Authenticate", `Basic realm="camera"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/admin/status":
			adminSawAuthorization = r.Header.Get("Authorization") != ""
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()
	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
		Credentials:        NewCredentials("operator", "secret"),
		AllowBasic:         true,
		AllowBasicOverHTTP: true,
	})
	client := &http.Client{Transport: transport}
	for _, path := range []string{"/onvif/device_service", "/admin/status"} {
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatalf("NewRequest() error = %v", err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("Do(%s) error = %v", path, err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Fatalf("close response: %v", closeErr)
		}
	}
	if adminSawAuthorization {
		t.Fatal("cached Basic credentials escaped the challenged protection space")
	}
}

func TestClockSamplerCentersWholeSecondCameraTime(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	times := []time.Time{base.Add(999 * time.Millisecond), base.Add(999 * time.Millisecond)}
	index := 0
	sample, err := (ClockSampler{Attempts: 1, Now: func() time.Time {
		value := times[index]
		index++
		return value
	}}).Sample(context.Background(), func(context.Context) (time.Time, error) {
		return base.Add(3 * time.Second), nil
	})
	if err != nil {
		t.Fatalf("Sample() error = %v", err)
	}
	if sample.Offset != 2501*time.Millisecond {
		t.Fatalf("Offset = %s, want midpoint of camera's represented second", sample.Offset)
	}
	if sample.Uncertainty != 500*time.Millisecond {
		t.Fatalf("Uncertainty = %s, want 500ms at zero RTT", sample.Uncertainty)
	}
}

func TestZeroValueStrategyStoreCanBeUsed(t *testing.T) {
	t.Parallel()

	var store StrategyStore
	if err := store.Set("device", DeviceStrategy{Mode: ModeDigest}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if strategy, ok := store.Get("device"); !ok || strategy.Mode != ModeDigest {
		t.Fatalf("Get() = (%+v, %v)", strategy, ok)
	}
}

func TestNonReplayableRequestBodyReturnsTypedError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Digest realm="camera", nonce="nonce", algorithm=SHA-256, qop="auth"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{Credentials: NewCredentials("operator", "secret")})
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, io.NopCloser(strings.NewReader("body")))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	response, err := (&http.Client{Transport: transport}).Do(request)
	if response != nil && response.Body != nil {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close error response: %v", closeErr)
		}
	}
	if err == nil || !strings.Contains(err.Error(), ErrRequestBodyNotReplayable.Error()) {
		t.Fatalf("Do() error = %v, want ErrRequestBodyNotReplayable", err)
	}
}

func TestCredentialsCannotLeakThroughDefaultFormattingOrJSON(t *testing.T) {
	t.Parallel()

	credentials := NewCredentials("sensitive-user", "sensitive-password")
	//nolint:staticcheck // Intentionally verifies that private credential fields serialize to no values.
	serialized, err := json.Marshal(credentials)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	output := fmt.Sprintf("%v %#v %+v %s", credentials, credentials, credentials, serialized)
	for _, secret := range []string{"sensitive-user", "sensitive-password"} {
		if strings.Contains(output, secret) {
			t.Fatalf("credential representation leaked %q: %s", secret, output)
		}
	}
}
