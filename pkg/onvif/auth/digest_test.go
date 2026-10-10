package auth

import (
	"context"
	"crypto/md5" // #nosec G501 -- MD5 is required for legacy RFC 7616 interoperability tests.
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestDigestTransportSHA256QOPAuthAndNonceCount(t *testing.T) {
	t.Parallel()

	const (
		username = "operator"
		password = "camera-secret"
		realm    = "synthetic-camera"
		nonce    = "server-nonce"
		cnonce   = "fixed-cnonce"
	)
	var mu sync.Mutex
	var authorizations []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization := r.Header.Get("Authorization")
		if authorization == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="`+realm+`", nonce="`+nonce+`", algorithm=SHA-256, qop="auth", opaque="synthetic"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mu.Lock()
		authorizations = append(authorizations, authorization)
		mu.Unlock()
		parameters, err := parseSchemeParameters(authorization, "Digest")
		if err != nil {
			t.Errorf("parse Authorization: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		wantResponse := testDigestResponse(t, "SHA-256", username, password, realm, nonce, parameters["nc"], cnonce, r.Method, r.URL.RequestURI())
		if parameters["response"] != wantResponse || parameters["cnonce"] != cnonce || parameters["qop"] != "auth" {
			t.Errorf("unexpected digest parameters: %#v; want response %s", parameters, wantResponse)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()

	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
		Credentials: NewCredentials(username, password),
		CNonce:      func() (string, error) { return cnonce, nil },
	})
	client := &http.Client{Transport: transport}
	for i := 0; i < 2; i++ {
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/onvif?profile=main", nil)
		if err != nil {
			t.Fatalf("NewRequest() error = %v", err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("Do() error = %v", err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		if err := response.Body.Close(); err != nil {
			t.Fatalf("close response: %v", err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", response.StatusCode)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(authorizations) != 2 {
		t.Fatalf("authenticated request count = %d, want 2", len(authorizations))
	}
	if !strings.Contains(authorizations[0], "nc=00000001") || !strings.Contains(authorizations[1], "nc=00000002") {
		t.Errorf("nonce counts did not advance: %q", authorizations)
	}
}

func TestDigestTransportHandlesStaleChallenge(t *testing.T) {
	t.Parallel()

	const username, password = "operator", "camera-secret"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		authorization := r.Header.Get("Authorization")
		switch requests {
		case 1:
			w.Header().Set("WWW-Authenticate", `Digest realm="camera", nonce="old", algorithm=MD5, qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
		case 2:
			if !strings.Contains(authorization, `nonce="old"`) {
				t.Errorf("second request Authorization = %q, want old nonce", authorization)
			}
			w.Header().Set("WWW-Authenticate", `Digest realm="camera", nonce="new", algorithm=MD5, qop="auth", stale=true`)
			w.WriteHeader(http.StatusUnauthorized)
		case 3:
			if !strings.Contains(authorization, `nonce="new"`) || !strings.Contains(authorization, "nc=00000001") {
				t.Errorf("third request Authorization = %q, want reset new nonce", authorization)
			}
			_, _ = io.WriteString(w, "ok")
		default:
			t.Errorf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
		Credentials: NewCredentials(username, password),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	response, err := (&http.Client{Transport: transport}).Get(server.URL) //nolint:noctx // The test's bounded local server responds synchronously.
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close response: %v", closeErr)
		}
	}()
	if response.StatusCode != http.StatusOK || requests != 3 {
		t.Fatalf("status = %d, requests = %d; want 200 and 3", response.StatusCode, requests)
	}
}

// TestDigestTransportBasicRequiresExplicitPolicy verifies the secure-default
// Basic policy over plaintext HTTP: refusal is a typed PolicyError that sends
// no credentials, and plaintext Basic needs both AllowBasic and
// AllowBasicOverHTTP.
func TestDigestTransportBasicRequiresExplicitPolicy(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name               string
		allowBasic         bool
		allowBasicOverHTTP bool
		wantReason         PolicyReason // non-empty means a typed PolicyError is expected
		wantStatus         int
		wantCalls          int
	}{
		{name: "disabled", allowBasic: false, wantReason: ReasonBasicDisabled, wantCalls: 1},
		{name: "enabled_plaintext_refused", allowBasic: true, wantReason: ReasonBasicOverPlaintext, wantCalls: 1},
		{name: "enabled_plaintext_allowed", allowBasic: true, allowBasicOverHTTP: true, wantStatus: http.StatusOK, wantCalls: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			calls := 0
			sawAuthorization := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				calls++
				if r.Header.Get("Authorization") != "" {
					sawAuthorization = true
				}
				mu.Unlock()
				if got := r.Header.Get("Authorization"); got != "Basic "+base64.StdEncoding.EncodeToString([]byte("operator:camera-secret")) {
					w.Header().Set("WWW-Authenticate", `Basic realm="synthetic-camera"`)
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
				Credentials:        NewCredentials("operator", "camera-secret"),
				AllowBasic:         tt.allowBasic,
				AllowBasicOverHTTP: tt.allowBasicOverHTTP,
			})
			request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			response, err := (&http.Client{Transport: transport}).Do(request)
			if tt.wantReason != "" {
				if response != nil && response.Body != nil {
					_ = response.Body.Close()
				}
				var policyErr *PolicyError
				if !errors.As(err, &policyErr) {
					t.Fatalf("Do() error = %v, want *PolicyError", err)
				}
				if policyErr.Reason != tt.wantReason {
					t.Fatalf("PolicyError.Reason = %q, want %q", policyErr.Reason, tt.wantReason)
				}
				mu.Lock()
				defer mu.Unlock()
				if sawAuthorization {
					t.Fatal("refused Basic policy still sent credentials")
				}
				if calls != tt.wantCalls {
					t.Fatalf("calls = %d, want %d", calls, tt.wantCalls)
				}
				return
			}
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			defer func() {
				if closeErr := response.Body.Close(); closeErr != nil {
					t.Errorf("close response: %v", closeErr)
				}
			}()
			mu.Lock()
			defer mu.Unlock()
			if response.StatusCode != tt.wantStatus || calls != tt.wantCalls {
				t.Errorf("status = %d, calls = %d; want %d, %d", response.StatusCode, calls, tt.wantStatus, tt.wantCalls)
			}
		})
	}
}

func TestDigestTransportReplaysPOSTBodyAndNeverAuthenticatesOutsideScope(t *testing.T) {
	t.Parallel()

	const payload = "synthetic request body"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if string(body) != payload {
			t.Errorf("request %d body = %q, want %q", calls, body, payload)
		}
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="camera", nonce="nonce", algorithm=SHA-256, qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
		Credentials: NewCredentials("operator", "camera-secret"),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	client := &http.Client{Transport: transport}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, strings.NewReader(payload))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response: %v", err)
	}
	if calls != 2 {
		t.Fatalf("request count = %d, want 2", calls)
	}

	outsideSawAuthorization := false
	outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outsideSawAuthorization = r.Header.Get("Authorization") != ""
		w.Header().Set("WWW-Authenticate", `Digest realm="other", nonce="other", qop="auth"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer outside.Close()
	outsideRequest, err := http.NewRequestWithContext(context.Background(), http.MethodGet, outside.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	outsideResponse, err := client.Do(outsideRequest)
	if err != nil {
		t.Fatalf("outside Do() error = %v", err)
	}
	defer func() {
		if closeErr := outsideResponse.Body.Close(); closeErr != nil {
			t.Errorf("close outside response: %v", closeErr)
		}
	}()
	if outsideSawAuthorization {
		t.Fatal("DigestTransport forwarded credentials outside its configured origin")
	}
}

func TestDigestTransportErrorsDoNotLeakCredentials(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Digest realm="camera", nonce="sensitive-nonce", algorithm=unsupported, qop="auth"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
		Credentials: NewCredentials("sensitive-user", "sensitive-password"),
	})
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	response, err := (&http.Client{Transport: transport}).Do(request)
	if response != nil && response.Body != nil {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close error response: %v", closeErr)
		}
	}
	if err == nil {
		t.Fatal("Do() error = nil, want unsupported challenge error")
	}
	for _, secret := range []string{"sensitive-user", "sensitive-password", "sensitive-nonce"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("Do() error leaked %q: %v", secret, err)
		}
	}
}

func newTestDigestTransport(t *testing.T, rawScope string, config DigestTransportConfig) *DigestTransport {
	t.Helper()
	scope, err := url.Parse(rawScope)
	if err != nil {
		t.Fatalf("parse scope: %v", err)
	}
	config.Scope = scope
	if config.Base == nil {
		// httptest.Server.Close closes idle connections on the shared
		// http.DefaultTransport, which breaks parallel tests mid-request.
		base := &http.Transport{}
		t.Cleanup(base.CloseIdleConnections)
		config.Base = base
	}
	transport, err := NewDigestTransport(config)
	if err != nil {
		t.Fatalf("NewDigestTransport() error = %v", err)
	}
	return transport
}

func testDigestResponse(t *testing.T, algorithm, username, password, realm, nonce, nc, cnonce, method, uri string) string {
	t.Helper()
	var newHash func() hash.Hash
	switch algorithm {
	case "MD5":
		newHash = md5.New
	case "SHA-256":
		newHash = sha256.New
	default:
		t.Fatalf("unsupported test hash %s", algorithm)
	}
	hashText := func(input string) string {
		digest := newHash()
		_, _ = io.WriteString(digest, input)
		return hex.EncodeToString(digest.Sum(nil))
	}
	ha1 := hashText(fmt.Sprintf("%s:%s:%s", username, realm, password))
	ha2 := hashText(method + ":" + uri)
	return hashText(strings.Join([]string{ha1, nonce, nc, cnonce, "auth", ha2}, ":"))
}
