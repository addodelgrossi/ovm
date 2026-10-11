package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// doGet performs a GET through the transport's client and closes the body.
func doGet(t *testing.T, client *http.Client, rawURL string) {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("NewRequest(%s): %v", rawURL, err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do(%s): %v", rawURL, err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
}

// TestDigestCacheLearnsTwoRealmsAndReusesEachPerPath verifies that two distinct
// realms, each guarding a distinct path, are learned independently and reused
// correctly on subsequent requests to their own path.
func TestDigestCacheLearnsTwoRealmsAndReusesEachPerPath(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	challenges := map[string]int{}
	authByRealm := map[string]int{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var realm, nonce, domain string
		switch {
		case strings.HasPrefix(r.URL.Path, "/onvif/"):
			realm, nonce, domain = "device", "nonce-device", "/onvif/"
		case strings.HasPrefix(r.URL.Path, "/media/"):
			realm, nonce, domain = "media", "nonce-media", "/media/"
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") == "" {
			mu.Lock()
			challenges[realm]++
			mu.Unlock()
			w.Header().Set("WWW-Authenticate", `Digest realm="`+realm+`", nonce="`+nonce+`", algorithm=SHA-256, qop="auth", domain="`+domain+`"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		parameters, err := parseSchemeParameters(r.Header.Get("Authorization"), "Digest")
		if err != nil {
			t.Errorf("parse auth: %v", err)
		}
		mu.Lock()
		authByRealm[parameters["realm"]]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
		Credentials: NewCredentials("operator", "secret"),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	client := &http.Client{Transport: transport}

	// Learn each realm, then re-request each path. Each path should challenge
	// only once and reuse its own realm thereafter.
	for _, path := range []string{"/onvif/a", "/media/a", "/onvif/b", "/media/b"} {
		doGet(t, client, server.URL+path)
	}

	mu.Lock()
	defer mu.Unlock()
	if challenges["device"] != 1 || challenges["media"] != 1 {
		t.Fatalf("challenge counts = %v, want each realm challenged exactly once", challenges)
	}
	if authByRealm["device"] < 2 || authByRealm["media"] < 2 {
		t.Fatalf("auth-by-realm = %v, want each realm reused", authByRealm)
	}
}

// TestDigestOverlappingProtectionSpacesSelectMostSpecific verifies that when a
// nested path is covered by two learned spaces, the most specific wins, and
// that a request matching neither sends no preemptive credential.
func TestDigestOverlappingProtectionSpacesSelectMostSpecific(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	usedRealm := map[string]string{} // path -> realm presented

	challengeFor := func(path string) (realm, header string) {
		if strings.HasPrefix(path, "/a/b/") {
			return "narrow", `Digest realm="narrow", nonce="n2", algorithm=SHA-256, qop="auth", domain="/a/b/"`
		}
		return "wide", `Digest realm="wide", nonce="n1", algorithm=SHA-256, qop="auth", domain="/a/"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/a/") {
			w.WriteHeader(http.StatusOK)
			return
		}
		wantRealm, header := challengeFor(r.URL.Path)
		if authorization := r.Header.Get("Authorization"); authorization != "" {
			parameters, _ := parseSchemeParameters(authorization, "Digest")
			// Only accept the realm that actually guards this path; otherwise
			// re-challenge so the client must learn the correct (narrow) record.
			if parameters["realm"] == wantRealm {
				mu.Lock()
				usedRealm[r.URL.Path] = parameters["realm"]
				mu.Unlock()
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		w.Header().Set("WWW-Authenticate", header)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
		Credentials: NewCredentials("operator", "secret"),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	client := &http.Client{Transport: transport}

	// Learn both spaces.
	doGet(t, client, server.URL+"/a/x")
	doGet(t, client, server.URL+"/a/b/y")

	// A nested request under /a/b/ must use the narrow realm (most specific).
	doGet(t, client, server.URL+"/a/b/z")

	mu.Lock()
	defer mu.Unlock()
	if usedRealm["/a/b/z"] != "narrow" {
		t.Fatalf("most-specific selection = %q, want narrow", usedRealm["/a/b/z"])
	}
}

// TestDigestAmbiguousSelectionSendsNoPreemptiveCredential verifies that when two
// learned spaces of equal specificity both cover a path, no preemptive
// credential is sent (the request goes out unauthenticated first).
func TestDigestAmbiguousSelectionSendsNoPreemptiveCredential(t *testing.T) {
	t.Parallel()

	scope := testURL(t, "https://cam.invalid")
	transport, err := NewDigestTransport(DigestTransportConfig{
		Scope:       scope,
		Credentials: NewCredentials("operator", "secret"),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	if err != nil {
		t.Fatalf("NewDigestTransport: %v", err)
	}

	// Two origin-wide records (equal specificity 0) for different realms.
	transport.cacheDigest(digestChallenge{realm: "a", nonce: "na", algorithm: "SHA-256", qop: "auth", space: originWideProtectionSpace()})
	transport.cacheDigest(digestChallenge{realm: "b", nonce: "nb", algorithm: "SHA-256", qop: "auth", space: originWideProtectionSpace()})

	if record := transport.selectRecord(testURL(t, "https://cam.invalid/anything")); record != nil {
		t.Fatalf("ambiguous selection returned %+v, want nil", record)
	}
}

// TestDigestStaleUpdateAffectsOnlyItsRealmSpace verifies that a stale rotation
// for one realm/space does not disturb another realm/space's cached nonce.
func TestDigestStaleUpdateAffectsOnlyItsRealmSpace(t *testing.T) {
	t.Parallel()

	scope := testURL(t, "https://cam.invalid")
	transport, err := NewDigestTransport(DigestTransportConfig{
		Scope:       scope,
		Credentials: NewCredentials("operator", "secret"),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	if err != nil {
		t.Fatalf("NewDigestTransport: %v", err)
	}

	spaceA := protectionSpaceSet{prefixes: []string{"/a/"}}
	spaceB := protectionSpaceSet{prefixes: []string{"/b/"}}
	transport.cacheDigest(digestChallenge{realm: "ra", nonce: "old-a", algorithm: "SHA-256", qop: "auth", space: spaceA})
	transport.cacheDigest(digestChallenge{realm: "rb", nonce: "nonce-b", algorithm: "SHA-256", qop: "auth", space: spaceB})

	// Stale rotation only for realm ra / space A.
	transport.cacheDigest(digestChallenge{realm: "ra", nonce: "new-a", algorithm: "SHA-256", qop: "auth", stale: true, space: spaceA})

	recA := transport.selectRecord(testURL(t, "https://cam.invalid/a/x"))
	recB := transport.selectRecord(testURL(t, "https://cam.invalid/b/x"))
	if recA == nil || recA.challenge.nonce != "new-a" {
		t.Fatalf("space A nonce = %+v, want rotated to new-a", recA)
	}
	if recB == nil || recB.challenge.nonce != "nonce-b" {
		t.Fatalf("space B nonce = %+v, want untouched nonce-b", recB)
	}
}

// TestDigestIndependentNonceCountersPerRealmSpace verifies independent
// monotonic nonce counters for independent realm/nonce/algorithm triples.
func TestDigestIndependentNonceCountersPerRealmSpace(t *testing.T) {
	t.Parallel()

	scope := testURL(t, "https://cam.invalid")
	transport, err := NewDigestTransport(DigestTransportConfig{
		Scope:       scope,
		Credentials: NewCredentials("operator", "secret"),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	if err != nil {
		t.Fatalf("NewDigestTransport: %v", err)
	}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, scope.String(), nil)

	a := &cacheRecord{kind: recordDigest, challenge: digestChallenge{realm: "ra", nonce: "na", algorithm: "SHA-256", qop: "auth"}, counter: &digestNonceCounter{}}
	b := &cacheRecord{kind: recordDigest, challenge: digestChallenge{realm: "rb", nonce: "nb", algorithm: "SHA-256", qop: "auth"}, counter: &digestNonceCounter{}}
	ncOf := func(record *cacheRecord) string {
		authorization, authErr := transport.digestAuthorization(record, request)
		if authErr != nil {
			t.Fatalf("digestAuthorization: %v", authErr)
		}
		parameters, _ := parseSchemeParameters(authorization, "Digest")
		return parameters["nc"]
	}
	// Interleave A and B; each must advance independently.
	got := []string{ncOf(a), ncOf(b), ncOf(a), ncOf(b), ncOf(a)}
	expect := []string{"00000001", "00000001", "00000002", "00000002", "00000003"}
	for i := range expect {
		if got[i] != expect[i] {
			t.Fatalf("interleaved nc = %v, want %v", got, expect)
		}
	}
}

// TestDigestNonceCounterConcurrency exercises concurrent qop=auth requests
// against one realm/nonce and asserts the counter never produces a duplicate.
func TestDigestNonceCounterConcurrency(t *testing.T) {
	t.Parallel()

	scope := testURL(t, "https://cam.invalid")
	transport, err := NewDigestTransport(DigestTransportConfig{
		Scope:       scope,
		Credentials: NewCredentials("operator", "secret"),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	if err != nil {
		t.Fatalf("NewDigestTransport: %v", err)
	}

	const workers = 32
	record := &cacheRecord{kind: recordDigest, challenge: digestChallenge{realm: "r", nonce: "n", algorithm: "SHA-256", qop: "auth"}, counter: &digestNonceCounter{}}
	var mu sync.Mutex
	seen := map[string]struct{}{}
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, scope.String(), nil)
			authorization, authErr := transport.digestAuthorization(record, request)
			if authErr != nil {
				t.Errorf("digestAuthorization: %v", authErr)
				return
			}
			parameters, _ := parseSchemeParameters(authorization, "Digest")
			mu.Lock()
			defer mu.Unlock()
			if _, dup := seen[parameters["nc"]]; dup {
				t.Errorf("duplicate nonce count %q under concurrency", parameters["nc"])
			}
			seen[parameters["nc"]] = struct{}{}
		}()
	}
	wg.Wait()
	if len(seen) != workers {
		t.Fatalf("distinct nonce counts = %d, want %d", len(seen), workers)
	}
}

// TestRFC2069DisabledByDefaultReturnsTypedPolicyError verifies that a no-qop
// Digest-only challenge is refused with ReasonRFC2069Disabled by default and no
// credentials leave the client.
func TestRFC2069DisabledByDefaultReturnsTypedPolicyError(t *testing.T) {
	t.Parallel()

	sawAuthorization := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			sawAuthorization = true
		}
		w.Header().Set("WWW-Authenticate", `Digest realm="legacy", nonce="legacy-nonce", algorithm=MD5`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
		Credentials: NewCredentials("operator", "secret"),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	response, err := (&http.Client{Transport: transport}).Do(mustGet(t, server.URL))
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	var policyErr *PolicyError
	if !errors.As(err, &policyErr) || policyErr.Reason != ReasonRFC2069Disabled {
		t.Fatalf("err = %v, want ReasonRFC2069Disabled", err)
	}
	if !errors.Is(err, ErrAuthPolicy) {
		t.Fatal("PolicyError should match ErrAuthPolicy sentinel")
	}
	if sawAuthorization {
		t.Fatal("refused RFC 2069 policy still sent credentials")
	}
	for _, secret := range []string{"operator", "secret", "legacy-nonce"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("policy error leaked %q: %v", secret, err)
		}
	}
}

// TestRFC2069OptInOmitsQOPNcCnonce verifies that legacy opt-in produces a valid
// RFC 2069 Authorization with no qop/nc/cnonce for a non-sess algorithm.
func TestRFC2069OptInOmitsQOPNcCnonce(t *testing.T) {
	t.Parallel()

	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="legacy", nonce="legacy-nonce", algorithm=MD5`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		got = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
		Credentials:  NewCredentials("operator", "secret"),
		AllowRFC2069: true,
		CNonce:       func() (string, error) { return "cnonce", nil },
	})
	doGet(t, &http.Client{Transport: transport}, server.URL)
	for _, forbidden := range []string{"qop", "nc=", "cnonce"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("legacy Authorization unexpectedly contained %q: %s", forbidden, got)
		}
	}
	if !strings.Contains(got, `response="`) {
		t.Fatalf("legacy Authorization missing response: %s", got)
	}
}

// TestRFC2069SessOptInKeepsCnonceOmitsQOP verifies a legacy -sess challenge
// still carries cnonce (required for HA1) but omits qop and nc.
func TestRFC2069SessOptInKeepsCnonceOmitsQOP(t *testing.T) {
	t.Parallel()

	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="legacy", nonce="legacy-nonce", algorithm=MD5-sess`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		got = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
		Credentials:  NewCredentials("operator", "secret"),
		AllowRFC2069: true,
		CNonce:       func() (string, error) { return "cnonce", nil },
	})
	doGet(t, &http.Client{Transport: transport}, server.URL)
	if !strings.Contains(got, "cnonce") {
		t.Fatalf("legacy -sess Authorization must carry cnonce: %s", got)
	}
	if strings.Contains(got, "qop") || strings.Contains(got, "nc=") {
		t.Fatalf("legacy -sess Authorization must omit qop/nc: %s", got)
	}
}

// TestDigestModernPreferredOverLegacyWhenBothOffered verifies qop=auth always
// outranks a legacy no-qop offer even when legacy is permitted.
func TestDigestModernPreferredOverLegacyWhenBothOffered(t *testing.T) {
	t.Parallel()

	var usedQOP string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Add("WWW-Authenticate", `Digest realm="camera", nonce="legacy", algorithm=MD5`)
			w.Header().Add("WWW-Authenticate", `Digest realm="camera", nonce="modern", algorithm=SHA-256, qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		parameters, _ := parseSchemeParameters(r.Header.Get("Authorization"), "Digest")
		usedQOP = parameters["qop"]
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
		Credentials:  NewCredentials("operator", "secret"),
		AllowRFC2069: true, // even allowed, modern must win
		CNonce:       func() (string, error) { return "cnonce", nil },
	})
	doGet(t, &http.Client{Transport: transport}, server.URL)
	if usedQOP != "auth" {
		t.Fatalf("negotiated qop = %q, want auth (modern preferred)", usedQOP)
	}
}

// TestDigestAuthIntOnlyRemainsUnsupportedNotPolicyBlocked verifies that an
// auth-int-only challenge yields ErrUnsupportedDigestChallenge, not a
// PolicyError.
func TestDigestAuthIntOnlyRemainsUnsupportedNotPolicyBlocked(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Digest realm="camera", nonce="n", algorithm=SHA-256, qop="auth-int"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
		Credentials:  NewCredentials("operator", "secret"),
		AllowRFC2069: true,
	})
	response, err := (&http.Client{Transport: transport}).Do(mustGet(t, server.URL))
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, ErrUnsupportedDigestChallenge) {
		t.Fatalf("err = %v, want ErrUnsupportedDigestChallenge", err)
	}
	var policyErr *PolicyError
	if errors.As(err, &policyErr) {
		t.Fatal("auth-int-only must not be a PolicyError")
	}
}

// TestBasicPolicyMatrixHTTPAndHTTPS exercises the full Basic policy matrix over
// plaintext HTTP and TLS, asserting typed refusals and no credential leakage.
func TestBasicPolicyMatrixHTTPAndHTTPS(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name               string
		tls                bool
		allowBasic         bool
		allowBasicOverHTTP bool
		wantReason         PolicyReason
		wantOK             bool
	}{
		{name: "http default refused", tls: false, allowBasic: false, wantReason: ReasonBasicDisabled},
		{name: "http allowbasic refused plaintext", tls: false, allowBasic: true, wantReason: ReasonBasicOverPlaintext},
		{name: "http allowbasic over http ok", tls: false, allowBasic: true, allowBasicOverHTTP: true, wantOK: true},
		{name: "https default refused", tls: true, allowBasic: false, wantReason: ReasonBasicDisabled},
		{name: "https allowbasic ok", tls: true, allowBasic: true, wantOK: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			sawAuthorization := false
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" {
					mu.Lock()
					sawAuthorization = true
					mu.Unlock()
					w.WriteHeader(http.StatusOK)
					return
				}
				w.Header().Set("WWW-Authenticate", `Basic realm="camera"`)
				w.WriteHeader(http.StatusUnauthorized)
			})

			var server *httptest.Server
			var client *http.Client
			if tt.tls {
				server = httptest.NewTLSServer(handler)
				client = server.Client()
			} else {
				server = httptest.NewServer(handler)
				client = &http.Client{}
			}
			defer server.Close()

			transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
				Base:               client.Transport,
				Credentials:        NewCredentials("operator", "secret"),
				AllowBasic:         tt.allowBasic,
				AllowBasicOverHTTP: tt.allowBasicOverHTTP,
			})
			client.Transport = transport

			response, err := client.Do(mustGet(t, server.URL))
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			if tt.wantOK {
				if err != nil {
					t.Fatalf("Do() error = %v, want success", err)
				}
				if response.StatusCode != http.StatusOK {
					t.Fatalf("status = %d, want 200", response.StatusCode)
				}
				return
			}
			var policyErr *PolicyError
			if !errors.As(err, &policyErr) || policyErr.Reason != tt.wantReason {
				t.Fatalf("err = %v, want PolicyError(%s)", err, tt.wantReason)
			}
			mu.Lock()
			defer mu.Unlock()
			if sawAuthorization {
				t.Fatal("refused Basic policy sent credentials")
			}
			for _, secret := range []string{"operator", "secret"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("policy error leaked %q", secret)
				}
			}
		})
	}
}

// TestIncoherentBasicPolicyRejectedAtConstruction verifies AllowBasicOverHTTP
// without AllowBasic is a construction-time typed PolicyError.
func TestIncoherentBasicPolicyRejectedAtConstruction(t *testing.T) {
	t.Parallel()

	_, err := NewDigestTransport(DigestTransportConfig{
		Scope:              testURL(t, "https://cam.invalid"),
		Credentials:        NewCredentials("operator", "secret"),
		AllowBasicOverHTTP: true,
	})
	var policyErr *PolicyError
	if !errors.As(err, &policyErr) || policyErr.Reason != ReasonIncoherentBasicPolicy {
		t.Fatalf("err = %v, want ReasonIncoherentBasicPolicy", err)
	}
}

// TestDigestAndBasicRecordsCoexist verifies that a Digest record and a Basic
// record for different paths are both retained and selected per path.
func TestDigestAndBasicRecordsCoexist(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	scheme := map[string]string{} // path -> auth scheme presented
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authorization := r.Header.Get("Authorization"); authorization != "" {
			mu.Lock()
			scheme[r.URL.Path] = strings.ToLower(firstToken(authorization))
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/digest/"):
			w.Header().Set("WWW-Authenticate", `Digest realm="d", nonce="n", algorithm=SHA-256, qop="auth", domain="/digest/"`)
		case strings.HasPrefix(r.URL.Path, "/basic/"):
			w.Header().Set("WWW-Authenticate", `Basic realm="b"`)
		default:
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
		Credentials:        NewCredentials("operator", "secret"),
		AllowBasic:         true,
		AllowBasicOverHTTP: true,
		CNonce:             func() (string, error) { return "cnonce", nil },
	})
	client := &http.Client{Transport: transport}
	for _, path := range []string{"/digest/a", "/basic/a", "/digest/b", "/basic/b"} {
		doGet(t, client, server.URL+path)
	}

	mu.Lock()
	defer mu.Unlock()
	if scheme["/digest/b"] != "digest" {
		t.Fatalf("/digest/b scheme = %q, want digest", scheme["/digest/b"])
	}
	if scheme["/basic/b"] != "basic" {
		t.Fatalf("/basic/b scheme = %q, want basic", scheme["/basic/b"])
	}
}

// TestDigestDomainScopingDoesNotLeakOutsideSpace verifies a domain-scoped
// Digest record is never presented to a sibling path outside its space.
func TestDigestDomainScopingDoesNotLeakOutsideSpace(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	outsideSawAuthorization := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/onvif/"):
			if r.Header.Get("Authorization") == "" {
				w.Header().Set("WWW-Authenticate", `Digest realm="d", nonce="n", algorithm=SHA-256, qop="auth", domain="/onvif/"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/other/"):
			mu.Lock()
			outsideSawAuthorization = r.Header.Get("Authorization") != ""
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
		Credentials: NewCredentials("operator", "secret"),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	client := &http.Client{Transport: transport}
	doGet(t, client, server.URL+"/onvif/x") // learn scoped record
	doGet(t, client, server.URL+"/other/y") // must not reuse it

	mu.Lock()
	defer mu.Unlock()
	if outsideSawAuthorization {
		t.Fatal("domain-scoped Digest credentials leaked outside their protection space")
	}
}

func testURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return parsed
}

func mustGet(t *testing.T, rawURL string) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("NewRequest(%s): %v", rawURL, err)
	}
	return request
}

// TestDigestRejectsUnsafeDomainWithoutSendingCredentials verifies that a
// present but invalid domain directive is not treated like an omitted domain
// and widened to the entire origin.
func TestDigestRejectsUnsafeDomainWithoutSendingCredentials(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := 0
	sawAuthorization := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		sawAuthorization = sawAuthorization || r.Header.Get("Authorization") != ""
		mu.Unlock()
		w.Header().Set("WWW-Authenticate", `Digest realm="camera", nonce="nonce", algorithm=SHA-256, qop="auth", domain="https://foreign.invalid/onvif/"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
		Credentials: NewCredentials("operator", "secret"),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	response, err := (&http.Client{Transport: transport}).Do(mustGet(t, server.URL+"/onvif/device_service"))
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, ErrUnsupportedDigestChallenge) {
		t.Fatalf("Do() error = %v, want ErrUnsupportedDigestChallenge", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 1 || sawAuthorization {
		t.Fatalf("requests=%d authorization=%v, want one credential-free request", requests, sawAuthorization)
	}
}

// TestDigestDomainExactPrefixDoesNotWidenToParent verifies that a file-like
// domain URI remains the advertised URI prefix rather than becoming its parent
// directory.
func TestDigestDomainExactPrefixDoesNotWidenToParent(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	outsideAuthorized := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/onvif/device_service":
			if r.Header.Get("Authorization") == "" {
				w.Header().Set("WWW-Authenticate", `Digest realm="device", nonce="nonce", algorithm=SHA-256, qop="auth", domain="/onvif/device_service"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/onvif/other":
			mu.Lock()
			outsideAuthorized = r.Header.Get("Authorization") != ""
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
		Credentials: NewCredentials("operator", "secret"),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	client := &http.Client{Transport: transport}
	doGet(t, client, server.URL+"/onvif/device_service")
	doGet(t, client, server.URL+"/onvif/other")

	mu.Lock()
	defer mu.Unlock()
	if outsideAuthorized {
		t.Fatal("exact Digest domain URI widened to its parent directory")
	}
}

// TestDigestNonceCountersAreIndependentPerProtectionSpace covers two records
// that intentionally share realm, nonce, and algorithm. Their protection-space
// identity must still give each an independent monotonic sequence.
func TestDigestNonceCountersAreIndependentPerProtectionSpace(t *testing.T) {
	t.Parallel()

	scope := testURL(t, "https://cam.invalid")
	transport, err := NewDigestTransport(DigestTransportConfig{
		Scope:       scope,
		Credentials: NewCredentials("operator", "secret"),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	if err != nil {
		t.Fatalf("NewDigestTransport: %v", err)
	}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, scope.String(), nil)
	aChallenge := digestChallenge{realm: "shared", nonce: "shared", algorithm: "SHA-256", qop: "auth", space: protectionSpaceSet{prefixes: []string{"/a/"}}}
	bChallenge := digestChallenge{realm: "shared", nonce: "shared", algorithm: "SHA-256", qop: "auth", space: protectionSpaceSet{prefixes: []string{"/b/"}}}
	a := &cacheRecord{kind: recordDigest, challenge: aChallenge, counter: &digestNonceCounter{}}
	b := &cacheRecord{kind: recordDigest, challenge: bChallenge, counter: &digestNonceCounter{}}

	nc := func(record *cacheRecord) string {
		header, authErr := transport.digestAuthorization(record, request)
		if authErr != nil {
			t.Fatalf("digestAuthorization: %v", authErr)
		}
		parameters, parseErr := parseSchemeParameters(header, "Digest")
		if parseErr != nil {
			t.Fatalf("parse Authorization: %v", parseErr)
		}
		return parameters["nc"]
	}
	got := []string{nc(a), nc(b), nc(a), nc(b)}
	want := []string{"00000001", "00000001", "00000002", "00000002"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("nonce counts = %v, want %v", got, want)
		}
	}
	if sameChallenge(aChallenge, bChallenge) {
		t.Fatal("challenges in different protection spaces must not be identical")
	}
}

func TestDuplicateAuthenticationDirectiveIsRejected(t *testing.T) {
	t.Parallel()

	if _, err := parseSchemeParameters(`Digest realm="a", realm="b", nonce="n"`, "Digest"); err == nil {
		t.Fatal("duplicate Digest directive was accepted")
	}
}

// TestDigestStaleWithoutDomainRetainsProtectionSpace verifies a stale nonce
// rotation that omits domain replaces the challenge just answered instead of
// leaving a stale scoped record beside a new origin-wide record.
func TestDigestStaleWithoutDomainRetainsProtectionSpace(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var nonces []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization := r.Header.Get("Authorization")
		if authorization == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="camera", nonce="old", algorithm=SHA-256, qop="auth", domain="/a/"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		parameters, err := parseSchemeParameters(authorization, "Digest")
		if err != nil {
			t.Errorf("parse Authorization: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		nonces = append(nonces, parameters["nonce"])
		mu.Unlock()
		if parameters["nonce"] == "old" {
			w.Header().Set("WWW-Authenticate", `Digest realm="camera", nonce="new", algorithm=SHA-256, qop="auth", stale=true`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := newTestDigestTransport(t, server.URL, DigestTransportConfig{
		Credentials: NewCredentials("operator", "secret"),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	client := &http.Client{Transport: transport}
	doGet(t, client, server.URL+"/a/one")
	doGet(t, client, server.URL+"/a/two")

	mu.Lock()
	defer mu.Unlock()
	want := []string{"old", "new", "new"}
	if len(nonces) != len(want) {
		t.Fatalf("authenticated nonces = %v, want %v", nonces, want)
	}
	for i := range want {
		if nonces[i] != want[i] {
			t.Fatalf("authenticated nonces = %v, want %v", nonces, want)
		}
	}
}

// TestDigestNonceCounterSurvivesAlgorithmChange verifies nc is monotonic for a
// server nonce even when the selected algorithm changes within the same realm
// and protection space.
func TestDigestNonceCounterSurvivesAlgorithmChange(t *testing.T) {
	t.Parallel()

	transport, err := NewDigestTransport(DigestTransportConfig{
		Scope:       testURL(t, "https://cam.invalid"),
		Credentials: NewCredentials("operator", "secret"),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	if err != nil {
		t.Fatalf("NewDigestTransport: %v", err)
	}
	space := protectionSpaceSet{prefixes: []string{"/a/"}}
	first := transport.cacheDigest(digestChallenge{realm: "camera", nonce: "shared", algorithm: "SHA-256", qop: "auth", space: space})
	request := mustGet(t, "https://cam.invalid/a/resource")
	firstHeader, err := transport.digestAuthorization(first, request)
	if err != nil {
		t.Fatalf("first authorization: %v", err)
	}
	second := transport.cacheDigest(digestChallenge{realm: "camera", nonce: "shared", algorithm: "MD5", qop: "auth", space: space})
	secondHeader, err := transport.digestAuthorization(second, request)
	if err != nil {
		t.Fatalf("second authorization: %v", err)
	}
	firstParameters, _ := parseSchemeParameters(firstHeader, "Digest")
	secondParameters, _ := parseSchemeParameters(secondHeader, "Digest")
	if firstParameters["nc"] != "00000001" || secondParameters["nc"] != "00000002" {
		t.Fatalf("nonce counts across algorithm change = %q, %q; want 1, 2", firstParameters["nc"], secondParameters["nc"])
	}
}

// TestDigestCounterSurvivesCacheEviction verifies an already-selected record
// and a later re-learned record retain the nonce's monotonic counter after
// unrelated realms evict the challenge from the bounded lookup cache.
func TestDigestCounterSurvivesCacheEviction(t *testing.T) {
	t.Parallel()

	transport, err := NewDigestTransport(DigestTransportConfig{
		Scope:       testURL(t, "https://cam.invalid"),
		Credentials: NewCredentials("operator", "secret"),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	if err != nil {
		t.Fatalf("NewDigestTransport: %v", err)
	}
	active := transport.cacheDigest(digestChallenge{
		realm: "active", nonce: "active-nonce", algorithm: "SHA-256", qop: "auth",
		space: protectionSpaceSet{prefixes: []string{"/active/"}},
	})
	request := mustGet(t, "https://cam.invalid/active/resource")
	first, err := transport.digestAuthorization(active, request)
	if err != nil {
		t.Fatalf("first authorization: %v", err)
	}
	for index := 0; index < maxCachedRecords; index++ {
		transport.cacheDigest(digestChallenge{
			realm: "realm-" + strconv.Itoa(index), nonce: "nonce-" + strconv.Itoa(index), algorithm: "SHA-256", qop: "auth",
			space: protectionSpaceSet{prefixes: []string{"/space-" + strconv.Itoa(index) + "/"}},
		})
	}
	second, err := transport.digestAuthorization(active, request)
	if err != nil {
		t.Fatalf("post-eviction authorization: %v", err)
	}
	relearned := transport.cacheDigest(active.challenge)
	third, err := transport.digestAuthorization(relearned, request)
	if err != nil {
		t.Fatalf("re-learned authorization: %v", err)
	}
	firstParameters, _ := parseSchemeParameters(first, "Digest")
	secondParameters, _ := parseSchemeParameters(second, "Digest")
	thirdParameters, _ := parseSchemeParameters(third, "Digest")
	if firstParameters["nc"] != "00000001" || secondParameters["nc"] != "00000002" || thirdParameters["nc"] != "00000003" {
		t.Fatalf("nonce counts across eviction = %q, %q, %q; want 1, 2, 3", firstParameters["nc"], secondParameters["nc"], thirdParameters["nc"])
	}
}

func TestDigestNonceStateLimitFailsClosed(t *testing.T) {
	t.Parallel()

	transport, err := NewDigestTransport(DigestTransportConfig{
		Scope:       testURL(t, "https://cam.invalid"),
		Credentials: NewCredentials("operator", "secret"),
		CNonce:      func() (string, error) { return "cnonce", nil },
	})
	if err != nil {
		t.Fatalf("NewDigestTransport: %v", err)
	}
	for index := 0; index < maxDigestNonceStates; index++ {
		transport.cacheDigest(digestChallenge{
			realm: "realm-" + strconv.Itoa(index), nonce: "nonce-" + strconv.Itoa(index), algorithm: "SHA-256", qop: "auth",
			space: protectionSpaceSet{prefixes: []string{"/state-" + strconv.Itoa(index) + "/"}},
		})
	}
	blocked := transport.cacheDigest(digestChallenge{
		realm: "overflow", nonce: "overflow", algorithm: "SHA-256", qop: "auth",
		space: protectionSpaceSet{prefixes: []string{"/overflow/"}},
	})
	if _, err := transport.digestAuthorization(blocked, mustGet(t, "https://cam.invalid/overflow/resource")); !errors.Is(err, errDigestNonceStateLimit) {
		t.Fatalf("digestAuthorization error = %v, want nonce-state limit", err)
	}
}

func TestDigestStaleNonceRotationPreservesHistoricalCounter(t *testing.T) {
	t.Parallel()

	transport, err := NewDigestTransport(DigestTransportConfig{
		Scope:       testURL(t, "https://cam.invalid"),
		Credentials: NewCredentials("operator", "secret"),
	})
	if err != nil {
		t.Fatalf("NewDigestTransport: %v", err)
	}
	space := protectionSpaceSet{prefixes: []string{"/onvif/"}}
	challenge := func(nonce string) digestChallenge {
		return digestChallenge{
			realm:     "camera",
			nonce:     nonce,
			algorithm: "SHA-256",
			qop:       "auth",
			stale:     true,
			space:     space,
		}
	}

	firstA := transport.cacheDigest(challenge("nonce-a"))
	if got, countErr := transport.nextNonceCount(firstA); countErr != nil || got != 1 {
		t.Fatalf("first nonce A count = %d, err = %v; want 1", got, countErr)
	}
	firstB := transport.cacheDigest(challenge("nonce-b"))
	if got, countErr := transport.nextNonceCount(firstB); countErr != nil || got != 1 {
		t.Fatalf("first nonce B count = %d, err = %v; want 1", got, countErr)
	}
	secondA := transport.cacheDigest(challenge("nonce-a"))
	if got, countErr := transport.nextNonceCount(secondA); countErr != nil || got != 2 {
		t.Fatalf("relearned nonce A count = %d, err = %v; want 2", got, countErr)
	}
}
