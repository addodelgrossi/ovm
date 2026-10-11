package auth

import (
	"crypto/md5" // #nosec G501 -- MD5 is required for legacy RFC 7616 camera interoperability.
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

const (
	maxChallengeBodyDrain int64 = 32 << 10
	maxDigestNonceStates        = 128
	// maxCachedRecords bounds the realm+protection-space cache so a hostile or
	// buggy server cannot force unbounded growth across many challenges.
	maxCachedRecords = 32
)

var (
	// ErrUnsupportedDigestChallenge identifies a Digest challenge this transport
	// cannot answer without weakening or changing its authentication semantics.
	ErrUnsupportedDigestChallenge = errors.New("unsupported HTTP Digest challenge")
	// ErrRequestBodyNotReplayable identifies an authenticated retry whose body
	// cannot be recreated safely.
	ErrRequestBodyNotReplayable = errors.New("HTTP request body cannot be replayed for authentication")
	errDigestNonceStateLimit    = errors.New("HTTP Digest nonce state limit reached")
)

// Credentials contains ephemeral credentials in a non-serializable value.
// String and GoString always redact its contents.
type Credentials struct {
	username string
	password string
}

// NewCredentials constructs ephemeral HTTP authentication credentials.
func NewCredentials(username, password string) Credentials {
	return Credentials{username: username, password: password}
}

// String returns a credential-safe representation.
func (Credentials) String() string { return "HTTP credentials (redacted)" }

// GoString returns a credential-safe Go-syntax representation.
func (Credentials) GoString() string { return "auth.Credentials{REDACTED}" }

// DigestTransportConfig configures an origin-scoped HTTP authentication
// RoundTripper. Basic is unavailable unless AllowBasic is true. CNonce may be
// called by concurrent requests; DigestTransport serializes those calls.
type DigestTransportConfig struct {
	Base        http.RoundTripper
	Scope       *url.URL
	Credentials Credentials
	// AllowBasic permits HTTP Basic. Over plaintext HTTP it is still refused
	// unless AllowBasicOverHTTP is also set. HTTPS Basic requires only this.
	AllowBasic bool
	// AllowBasicOverHTTP additionally permits HTTP Basic over plaintext HTTP.
	// It is meaningless without AllowBasic and that combination is rejected by
	// NewDigestTransport as incoherent.
	AllowBasicOverHTTP bool
	// AllowRFC2069 permits legacy no-qop (RFC 2069) Digest. Disabled by
	// default; qop=auth challenges always outrank legacy when this is enabled.
	AllowRFC2069 bool
	CNonce       func() (string, error)
}

// DigestTransport negotiates RFC 7616 qop=auth with MD5 or SHA-256 and handles
// stale challenges. It may also answer Basic challenges under explicit policy,
// and legacy RFC 2069 Digest only when explicitly permitted.
type DigestTransport struct {
	base               http.RoundTripper
	scope              origin
	credentials        Credentials
	allowBasic         bool
	allowBasicOverHTTP bool
	allowRFC2069       bool
	cnonce             func() (string, error)

	cnonceMu sync.Mutex
	mu       sync.Mutex
	// records holds learned Digest and Basic protection-space records keyed by
	// realm + canonical protection space. Insertion order in recordOrder bounds
	// the cache. Digest and Basic records coexist.
	records       map[string]*cacheRecord
	recordOrder   []string
	counterStates map[string]*digestNonceCounter
}

type origin struct {
	scheme string
	host   string
	port   string
}

// cacheRecordKind distinguishes a learned Digest challenge from a Basic record.
type cacheRecordKind int

const (
	recordDigest cacheRecordKind = iota
	recordBasic
)

// cacheRecord is one learned protection space for a realm, holding either a
// Digest challenge or a Basic marker.
type cacheRecord struct {
	kind      cacheRecordKind
	realm     string
	space     protectionSpaceSet
	challenge digestChallenge // valid when kind == recordDigest
	counter   *digestNonceCounter
}

// digestNonceCounter is shared by all live record generations for the same
// realm/protection-space/server nonce. DigestTransport.mu guards count. Keeping
// it behind a pointer makes selection/replacement/eviction safe for in-flight
// requests and prevents a counter reset when only the algorithm changes.
type digestNonceCounter struct {
	count   uint32
	blocked bool
}

type digestChallenge struct {
	realm         string
	nonce         string
	algorithm     string
	qop           string // "auth" or "" for legacy no-qop (RFC 2069)
	opaque        string
	stale         bool
	domainPresent bool
	space         protectionSpaceSet
}

// sameChallenge reports whether two challenges identify the same protected
// credential derivation. Space, qop, and opaque are part of the identity so a
// newly scoped or materially changed challenge is learned rather than mistaken
// for a repeated credential rejection.
func sameChallenge(a, b digestChallenge) bool {
	return a.realm == b.realm &&
		a.nonce == b.nonce &&
		a.algorithm == b.algorithm &&
		a.qop == b.qop &&
		a.opaque == b.opaque &&
		a.space.key() == b.space.key()
}

type authenticateChallenge struct {
	scheme     string
	parameters map[string]string
	err        error
}

// NewDigestTransport constructs an authentication transport bound to one
// HTTP(S) origin so cached credentials cannot be sent to redirected hosts.
func NewDigestTransport(config DigestTransportConfig) (*DigestTransport, error) {
	if config.Scope == nil || config.Scope.Host == "" {
		return nil, errors.New("HTTP authentication scope is required")
	}
	if config.Scope.Scheme != "http" && config.Scope.Scheme != "https" {
		return nil, errors.New("HTTP authentication scope must use HTTP or HTTPS")
	}
	if config.Scope.User != nil {
		return nil, errors.New("HTTP authentication scope must not contain userinfo")
	}
	if config.Credentials.username == "" {
		return nil, errors.New("HTTP authentication username is required")
	}
	// AllowBasicOverHTTP without AllowBasic can never permit any Basic exchange
	// and signals a misconfiguration; reject it rather than silently ignore it.
	if config.AllowBasicOverHTTP && !config.AllowBasic {
		return nil, newPolicyError(ReasonIncoherentBasicPolicy)
	}
	base := config.Base
	if base == nil {
		base = http.DefaultTransport
	}
	cnonce := config.CNonce
	if cnonce == nil {
		cnonce = randomCNonce
	}
	return &DigestTransport{
		base:               base,
		scope:              originFromURL(config.Scope),
		credentials:        config.Credentials,
		allowBasic:         config.AllowBasic,
		allowBasicOverHTTP: config.AllowBasicOverHTTP,
		allowRFC2069:       config.AllowRFC2069,
		cnonce:             cnonce,
		records:            make(map[string]*cacheRecord),
		counterStates:      make(map[string]*digestNonceCounter),
	}, nil
}

// RoundTrip implements http.RoundTripper.
func (t *DigestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil {
		return nil, errors.New("HTTP authentication request is required")
	}
	if !t.scope.matches(request.URL) {
		outbound, err := cloneRequest(request, false)
		if err != nil {
			return nil, err
		}
		outbound.Header.Del("Authorization")
		outbound.Header.Del("Proxy-Authorization")
		return t.base.RoundTrip(outbound)
	}

	selected := t.selectRecord(request.URL)
	var (
		previousScheme string
		answeredDigest digestChallenge
		haveAnswered   bool
	)
	for attempt := 0; attempt < 3; attempt++ {
		outbound, err := cloneRequest(request, attempt > 0)
		if err != nil {
			return nil, err
		}
		switch {
		case selected != nil && selected.kind == recordDigest:
			authorization, authErr := t.digestAuthorization(selected, outbound)
			if authErr != nil {
				if outbound.Body != nil {
					_ = outbound.Body.Close()
				}
				return nil, authErr
			}
			outbound.Header.Set("Authorization", authorization)
			previousScheme = "digest"
			answeredDigest = selected.challenge
			haveAnswered = true
		case selected != nil && selected.kind == recordBasic:
			outbound.SetBasicAuth(t.credentials.username, t.credentials.password)
			previousScheme = "basic"
		default:
			previousScheme = ""
		}

		response, roundTripErr := t.base.RoundTrip(outbound)
		if roundTripErr != nil {
			return nil, roundTripErr
		}
		if response.StatusCode != http.StatusUnauthorized {
			return response, nil
		}

		challenges := parseAuthenticateChallenges(response.Header.Values("WWW-Authenticate"))
		digest, digestErr := t.selectDigestChallenge(challenges, request.URL)
		if digestErr != nil {
			if closeErr := discardAndClose(response.Body); closeErr != nil {
				return nil, closeErr
			}
			return nil, digestErr
		}
		if digest != nil {
			// A stale challenge commonly omits domain. Inherit the protection
			// space of the challenge just answered so the rotation replaces only
			// that realm/space instead of creating an origin-wide sibling that can
			// later select the stale nonce.
			if digest.stale && previousScheme == "digest" && haveAnswered &&
				digest.realm == answeredDigest.realm && !digest.domainPresent {
				digest.space = answeredDigest.space
			}
			// A repeat of the challenge we just answered (same protected
			// not stale) is a genuine credential rejection: stop retrying.
			if previousScheme == "digest" && haveAnswered && sameChallenge(*digest, answeredDigest) && !digest.stale {
				return response, nil
			}
			if attempt == 2 {
				return response, nil
			}
			if err := discardAndClose(response.Body); err != nil {
				return nil, err
			}
			record := t.cacheDigest(*digest)
			selected = record
			continue
		}

		basic, basicErr := t.evaluateBasicChallenge(challenges, request.URL)
		if basicErr != nil {
			if closeErr := discardAndClose(response.Body); closeErr != nil {
				return nil, closeErr
			}
			return nil, basicErr
		}
		if basic != nil && previousScheme != "basic" && attempt < 2 {
			if err := discardAndClose(response.Body); err != nil {
				return nil, err
			}
			record := t.cacheBasic(*basic)
			selected = record
			continue
		}
		return response, nil
	}
	return nil, errors.New("HTTP authentication retry limit reached")
}

func cloneRequest(request *http.Request, replay bool) (*http.Request, error) {
	cloned := request.Clone(request.Context())
	cloned.Header = request.Header.Clone()
	if !replay || request.Body == nil {
		cloned.Body = request.Body
		return cloned, nil
	}
	if request.GetBody == nil {
		return nil, ErrRequestBodyNotReplayable
	}
	body, err := request.GetBody()
	if err != nil {
		return nil, fmt.Errorf("replay HTTP authentication request body: %w", err)
	}
	cloned.Body = body
	return cloned, nil
}

// selectRecord picks the most specific cached record whose protection space
// covers the request target. If two records tie on specificity the selection is
// ambiguous and no preemptive credential is sent (returns nil).
func (t *DigestTransport) selectRecord(requestURL *url.URL) *cacheRecord {
	target := requestTarget(requestURL)
	t.mu.Lock()
	defer t.mu.Unlock()
	var (
		best      *cacheRecord
		bestScore = -1
		tie       bool
	)
	for _, key := range t.recordOrder {
		record := t.records[key]
		if record == nil || !record.space.contains(target) {
			continue
		}
		score := record.space.specificity(target)
		switch {
		case score > bestScore:
			best, bestScore, tie = record, score, false
		case score == bestScore:
			tie = true
		}
	}
	if best == nil || tie {
		return nil
	}
	return best
}

// cacheDigest inserts or refreshes a Digest record keyed by realm + protection
// space. A stale rotation only replaces the record for its own realm+space.
func (t *DigestTransport) cacheDigest(challenge digestChallenge) *cacheRecord {
	recordCacheKey := recordKey(recordDigest, challenge.realm, challenge.space)
	t.mu.Lock()
	defer t.mu.Unlock()

	// Historical nonce counters remain in the bounded counterStates map even
	// after a stale rotation. If a server later reissues an earlier nonce for
	// the same realm and protection space, its nonce count must continue rather
	// than recreate a previously used authorization tuple.
	counterKey := digestCounterStateKey(challenge)
	counter := t.counterStates[counterKey]
	if counter == nil {
		if len(t.counterStates) >= maxDigestNonceStates {
			// Fail closed instead of evicting a counter and reusing nc=1 for an
			// already-seen server nonce.
			counter = &digestNonceCounter{blocked: true}
		} else {
			counter = &digestNonceCounter{}
			t.counterStates[counterKey] = counter
		}
	}
	record := &cacheRecord{
		kind:      recordDigest,
		realm:     challenge.realm,
		space:     challenge.space,
		challenge: challenge,
		counter:   counter,
	}
	t.putRecordLocked(recordCacheKey, record)
	return record
}

func digestCounterStateKey(challenge digestChallenge) string {
	return challenge.realm + "\x00" + challenge.space.key() + "\x00" + challenge.nonce
}

// cacheBasic inserts or refreshes a Basic record keyed by realm + protection
// space, leaving any coexisting Digest records intact.
func (t *DigestTransport) cacheBasic(record cacheRecord) *cacheRecord {
	stored := &record
	t.mu.Lock()
	defer t.mu.Unlock()
	t.putRecordLocked(recordKey(recordBasic, record.realm, record.space), stored)
	return stored
}

// putRecordLocked stores a record under key, bounding the cache by evicting the
// oldest distinct record when at capacity. In-flight requests retain their
// record and shared counter safely after replacement or eviction. Callers must
// hold t.mu.
func (t *DigestTransport) putRecordLocked(key string, record *cacheRecord) {
	if _, exists := t.records[key]; !exists {
		if len(t.recordOrder) >= maxCachedRecords {
			oldest := t.recordOrder[0]
			delete(t.records, oldest)
			t.recordOrder = t.recordOrder[1:]
		}
		t.recordOrder = append(t.recordOrder, key)
	}
	t.records[key] = record
}

func recordKey(kind cacheRecordKind, realm string, space protectionSpaceSet) string {
	prefix := "D"
	if kind == recordBasic {
		prefix = "B"
	}
	return prefix + "\x00" + realm + "\x00" + space.key()
}

// requestPath returns the request's absolute path, defaulting to "/".
func requestPath(value *url.URL) string {
	path := value.EscapedPath()
	if path == "" {
		return "/"
	}
	return path
}

func (t *DigestTransport) digestAuthorization(record *cacheRecord, request *http.Request) (string, error) {
	challenge := record.challenge
	// A -sess algorithm always needs a client nonce; qop=auth also needs one.
	_, session, err := digestHash(challenge.algorithm)
	if err != nil {
		return "", err
	}
	needsCNonce := challenge.qop == "auth" || session

	var cnonce string
	if needsCNonce {
		t.cnonceMu.Lock()
		cnonce, err = t.cnonce()
		t.cnonceMu.Unlock()
		if err != nil {
			return "", fmt.Errorf("generate HTTP Digest client nonce: %w", err)
		}
		if cnonce == "" {
			return "", errors.New("HTTP Digest client nonce is empty")
		}
	}

	var nonceCount uint32
	if challenge.qop == "auth" {
		nonceCount, err = t.nextNonceCount(record)
		if err != nil {
			return "", err
		}
	}

	newHash, _, err := digestHash(challenge.algorithm)
	if err != nil {
		return "", err
	}
	hashText := func(value string) string {
		digest := newHash()
		_, _ = io.WriteString(digest, value)
		return hex.EncodeToString(digest.Sum(nil))
	}
	ha1 := hashText(t.credentials.username + ":" + challenge.realm + ":" + t.credentials.password)
	if session {
		ha1 = hashText(ha1 + ":" + challenge.nonce + ":" + cnonce)
	}
	requestURI := request.URL.RequestURI()
	if requestURI == "" {
		requestURI = "/"
	}
	ha2 := hashText(request.Method + ":" + requestURI)
	nc := fmt.Sprintf("%08x", nonceCount)
	var response string
	if challenge.qop == "auth" {
		response = hashText(strings.Join([]string{ha1, challenge.nonce, nc, cnonce, challenge.qop, ha2}, ":"))
	} else {
		// Legacy RFC 2069: response = H(HA1:nonce:HA2), no qop/nc/cnonce.
		response = hashText(strings.Join([]string{ha1, challenge.nonce, ha2}, ":"))
	}

	parameters := []string{
		`username="` + quoteAuthValue(t.credentials.username) + `"`,
		`realm="` + quoteAuthValue(challenge.realm) + `"`,
		`nonce="` + quoteAuthValue(challenge.nonce) + `"`,
		`uri="` + quoteAuthValue(requestURI) + `"`,
		`response="` + response + `"`,
		`algorithm=` + challenge.algorithm,
	}
	if challenge.opaque != "" {
		parameters = append(parameters, `opaque="`+quoteAuthValue(challenge.opaque)+`"`)
	}
	switch {
	case challenge.qop == "auth":
		parameters = append(parameters, `qop=auth`, `nc=`+nc, `cnonce="`+quoteAuthValue(cnonce)+`"`)
	case session:
		// A legacy -sess challenge still requires cnonce for HA1 derivation,
		// but carries no qop or nc.
		parameters = append(parameters, `cnonce="`+quoteAuthValue(cnonce)+`"`)
	}
	return "Digest " + strings.Join(parameters, ", "), nil
}

// nextNonceCount advances the counter owned by a live Digest record. The
// counter object survives algorithm changes, record replacement, and eviction
// for every in-flight holder, so an active nonce cannot roll back or duplicate.
func (t *DigestTransport) nextNonceCount(record *cacheRecord) (uint32, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if record.counter == nil {
		record.counter = &digestNonceCounter{}
	}
	if record.counter.blocked {
		return 0, errDigestNonceStateLimit
	}
	if record.counter.count == ^uint32(0) {
		return 0, errors.New("HTTP Digest nonce count exhausted")
	}
	record.counter.count++
	return record.counter.count, nil
}

// selectDigestChallenge chooses the strongest acceptable Digest challenge for
// the request. qop=auth challenges always outrank legacy no-qop (RFC 2069)
// challenges. Legacy challenges are only eligible when AllowRFC2069 is set; if
// the only Digest offers are legacy and policy forbids them, a typed
// PolicyError is returned. auth-int-only challenges remain unsupported (not
// policy-blocked).
func (t *DigestTransport) selectDigestChallenge(challenges []authenticateChallenge, requestURL *url.URL) (*digestChallenge, error) {
	var (
		best            *digestChallenge
		bestRank        int
		sawDigest       bool
		sawLegacyNoQop  bool
		sawUsableLegacy bool
	)
	for _, challenge := range challenges {
		if !strings.EqualFold(challenge.scheme, "Digest") {
			continue
		}
		sawDigest = true
		if challenge.err != nil {
			continue
		}
		parameters := challenge.parameters
		realm, nonce := parameters["realm"], parameters["nonce"]
		if realm == "" || nonce == "" {
			continue
		}
		algorithm := strings.ToUpper(parameters["algorithm"])
		if algorithm == "" {
			algorithm = "MD5"
		}
		if _, _, err := digestHash(algorithm); err != nil {
			continue
		}
		qop, qopUsable := negotiateQOP(parameters["qop"])
		if !qopUsable {
			// qop was advertised but offered no "auth" (e.g. auth-int only):
			// unsupported, not policy-blocked. Skip it.
			continue
		}
		if qop == "" {
			// Legacy no-qop (RFC 2069).
			sawLegacyNoQop = true
			if !t.allowRFC2069 {
				continue
			}
			sawUsableLegacy = true
		}
		space := originWideProtectionSpace()
		if rawDomain, present := parameters["domain"]; present {
			var domainErr error
			space, domainErr = parseDomainDirective(rawDomain, t.scope)
			if domainErr != nil {
				// A present but invalid domain is an unsupported challenge. Never
				// treat it as omitted or widen it to the whole origin.
				continue
			}
		}
		candidate := &digestChallenge{
			realm:         realm,
			nonce:         nonce,
			algorithm:     algorithm,
			qop:           qop,
			opaque:        parameters["opaque"],
			stale:         strings.EqualFold(parameters["stale"], "true"),
			domainPresent: parameters["domain"] != "",
			space:         space,
		}
		rank := digestAlgorithmRank(algorithm)
		if qop == "auth" {
			// qop=auth always outranks any legacy challenge.
			rank += 1000
		}
		if best == nil || rank > bestRank {
			best, bestRank = candidate, rank
		}
	}
	if best != nil {
		return best, nil
	}
	// Only legacy no-qop offers were present and all were refused by policy.
	if sawLegacyNoQop && !sawUsableLegacy && !t.allowRFC2069 {
		return nil, newPolicyError(ReasonRFC2069Disabled)
	}
	if sawDigest {
		return nil, ErrUnsupportedDigestChallenge
	}
	return nil, nil
}

// negotiateQOP interprets an advertised qop list. It returns ("auth", true)
// when auth is offered, ("", true) when the directive is absent (legacy
// RFC 2069), and ("", false) when qop is present but offers no usable value
// (e.g. auth-int only), signalling an unsupported challenge.
func negotiateQOP(advertised string) (string, bool) {
	advertised = strings.TrimSpace(advertised)
	if advertised == "" {
		return "", true
	}
	for _, candidate := range strings.Split(advertised, ",") {
		if strings.EqualFold(strings.TrimSpace(candidate), "auth") {
			return "auth", true
		}
	}
	return "", false
}

func digestAlgorithmRank(algorithm string) int {
	switch strings.ToUpper(algorithm) {
	case "SHA-256-SESS":
		return 40
	case "SHA-256":
		return 30
	case "MD5-SESS":
		return 20
	case "MD5":
		return 10
	default:
		return 0
	}
}

// evaluateBasicChallenge inspects the challenges for a Basic offer and applies
// transport policy. It returns a Basic cache record to use, nil when no Basic
// challenge is present, or a typed PolicyError when a Basic challenge exists
// but policy forbids answering it. No credentials are sent on refusal.
func (t *DigestTransport) evaluateBasicChallenge(challenges []authenticateChallenge, requestURL *url.URL) (*cacheRecord, error) {
	var (
		found bool
		realm string
	)
	for _, challenge := range challenges {
		if !strings.EqualFold(challenge.scheme, "Basic") {
			continue
		}
		if challenge.err != nil {
			return nil, errors.New("malformed HTTP Basic challenge")
		}
		found = true
		realm = challenge.parameters["realm"]
		break
	}
	if !found {
		return nil, nil
	}
	if !t.allowBasic {
		return nil, newPolicyError(ReasonBasicDisabled)
	}
	// Basic over plaintext HTTP is refused unless explicitly permitted. Inspect
	// the actual request URL; the exact-origin guard guarantees it still matches
	// the configured scope.
	if !strings.EqualFold(requestURL.Scheme, "https") && !t.allowBasicOverHTTP {
		return nil, newPolicyError(ReasonBasicOverPlaintext)
	}
	return &cacheRecord{
		kind:  recordBasic,
		realm: realm,
		// Basic protection space is the challenged path's directory, scoped to
		// the exact origin. It is never origin-wide.
		space: protectionSpaceSet{prefixes: []string{basicProtectionPrefix(requestPath(requestURL))}},
	}, nil
}

func digestHash(algorithm string) (func() hash.Hash, bool, error) {
	switch strings.ToUpper(algorithm) {
	case "MD5":
		return md5.New, false, nil
	case "MD5-SESS":
		return md5.New, true, nil
	case "SHA-256":
		return sha256.New, false, nil
	case "SHA-256-SESS":
		return sha256.New, true, nil
	default:
		return nil, false, ErrUnsupportedDigestChallenge
	}
}

func parseAuthenticateChallenges(values []string) []authenticateChallenge {
	var challenges []authenticateChallenge
	for _, value := range values {
		for _, segment := range splitAuthenticateHeader(value) {
			scheme := firstToken(segment)
			if !strings.EqualFold(scheme, "Digest") && !strings.EqualFold(scheme, "Basic") {
				continue
			}
			parameters, err := parseSchemeParameters(segment, scheme)
			challenges = append(challenges, authenticateChallenge{scheme: scheme, parameters: parameters, err: err})
		}
	}
	return challenges
}

func splitAuthenticateHeader(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var (
		segments []string
		start    int
		quoted   bool
		escaped  bool
	)
	for index, character := range value {
		if escaped {
			escaped = false
			continue
		}
		if character == '\\' && quoted {
			escaped = true
			continue
		}
		if character == '"' {
			quoted = !quoted
			continue
		}
		if character != ',' || quoted {
			continue
		}
		remainder := strings.TrimSpace(value[index+1:])
		token := firstToken(remainder)
		if !strings.EqualFold(token, "Digest") && !strings.EqualFold(token, "Basic") {
			continue
		}
		segments = append(segments, strings.TrimSpace(value[start:index]))
		start = index + 1
	}
	segments = append(segments, strings.TrimSpace(value[start:]))
	return segments
}

func firstToken(value string) string {
	value = strings.TrimSpace(value)
	for index, character := range value {
		if character == ' ' || character == '\t' {
			return value[:index]
		}
	}
	return value
}

func parseSchemeParameters(value, scheme string) (map[string]string, error) {
	value = strings.TrimSpace(value)
	if len(value) < len(scheme) || !strings.EqualFold(value[:len(scheme)], scheme) {
		return nil, errors.New("authentication scheme does not match")
	}
	rest := strings.TrimSpace(value[len(scheme):])
	parameters := make(map[string]string)
	for rest != "" {
		rest = strings.TrimLeft(rest, " \t,")
		if rest == "" {
			break
		}
		equals := strings.IndexByte(rest, '=')
		if equals <= 0 {
			return nil, errors.New("malformed authentication parameter")
		}
		key := strings.ToLower(strings.TrimSpace(rest[:equals]))
		if key == "" {
			return nil, errors.New("malformed authentication parameter name")
		}
		if _, duplicate := parameters[key]; duplicate {
			return nil, errors.New("duplicate authentication parameter")
		}
		rest = strings.TrimSpace(rest[equals+1:])
		var parsed string
		if strings.HasPrefix(rest, `"`) {
			value, tail, err := parseQuotedAuthValue(rest)
			if err != nil {
				return nil, err
			}
			parsed, rest = value, tail
		} else {
			comma := strings.IndexByte(rest, ',')
			if comma < 0 {
				parsed, rest = strings.TrimSpace(rest), ""
			} else {
				parsed, rest = strings.TrimSpace(rest[:comma]), rest[comma+1:]
			}
		}
		if parsed == "" {
			return nil, errors.New("empty authentication parameter")
		}
		parameters[key] = parsed
	}
	return parameters, nil
}

func parseQuotedAuthValue(value string) (string, string, error) {
	var output strings.Builder
	escaped := false
	for index := 1; index < len(value); index++ {
		character := value[index]
		if escaped {
			output.WriteByte(character)
			escaped = false
			continue
		}
		if character == '\\' {
			escaped = true
			continue
		}
		if character == '"' {
			tail := strings.TrimSpace(value[index+1:])
			if tail != "" && !strings.HasPrefix(tail, ",") {
				return "", "", errors.New("malformed quoted authentication parameter")
			}
			return output.String(), strings.TrimPrefix(tail, ","), nil
		}
		output.WriteByte(character)
	}
	return "", "", errors.New("unterminated quoted authentication parameter")
}

func quoteAuthValue(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}

func randomCNonce() (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", fmt.Errorf("read random client nonce: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func discardAndClose(body io.ReadCloser) error {
	if body == nil {
		return nil
	}
	_, copyErr := io.Copy(io.Discard, io.LimitReader(body, maxChallengeBodyDrain))
	closeErr := body.Close()
	if copyErr != nil {
		return fmt.Errorf("discard authentication challenge body: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close authentication challenge body: %w", closeErr)
	}
	return nil
}

func originFromURL(value *url.URL) origin {
	port := value.Port()
	if port == "" {
		if value.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return origin{scheme: strings.ToLower(value.Scheme), host: strings.ToLower(value.Hostname()), port: port}
}

func (o origin) matches(value *url.URL) bool {
	return o == originFromURL(value)
}
