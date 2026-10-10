package fakedevice

import (
	"crypto/md5" // #nosec G501 -- MD5 is intentionally supported for legacy HTTP Digest camera tests.
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func (d *Device) authorizeHTTP(response http.ResponseWriter, request *http.Request) bool {
	switch d.cfg().HTTPAuth {
	case HTTPAuthNone:
		return true
	case HTTPAuthBasic:
		username, password, ok := request.BasicAuth()
		if ok && secureEqual(username, d.cfg().Username) && secureEqual(password, d.cfg().Password) {
			return true
		}
		response.Header().Set("WWW-Authenticate", `Basic realm="`+quoteHeaderValue(d.cfg().DigestRealm)+`"`)
		response.WriteHeader(http.StatusUnauthorized)
		return false
	case HTTPAuthDigest:
		return d.authorizeDigest(response, request)
	default:
		response.WriteHeader(http.StatusInternalServerError)
		return false
	}
}

func (d *Device) authorizeDigest(response http.ResponseWriter, request *http.Request) bool {
	parameters, ok := parseDigestAuthorization(request.Header.Get("Authorization"))
	if !ok {
		d.writeDigestChallenge(response, false)
		return false
	}

	d.mu.Lock()
	if parameters["nonce"] != d.digestNonce {
		d.mu.Unlock()
		d.writeDigestChallenge(response, true)
		return false
	}
	if d.cfg().DigestStaleOnce && !d.digestStaleIssued {
		d.digestStaleIssued = true
		d.digestNonce = nextSyntheticNonce(d.digestNonce)
		// A stale challenge rotates the server nonce, so every per-pair
		// nonce-count high-water mark recorded under the old nonce is obsolete
		// and reset. The client must restart its nc sequence under the new
		// nonce.
		d.digestNC = make(map[string]uint64)
		d.mu.Unlock()
		d.writeDigestChallenge(response, true)
		return false
	}
	d.mu.Unlock()
	if !d.verifyDigestParameters(parameters, request) {
		d.writeDigestChallenge(response, false)
		return false
	}
	return true
}

func (d *Device) verifyDigestParameters(parameters map[string]string, request *http.Request) bool {
	if !secureEqual(parameters["username"], d.cfg().Username) ||
		parameters["realm"] != d.cfg().DigestRealm ||
		!strings.EqualFold(parameters["algorithm"], d.cfg().DigestAlgorithm) ||
		parameters["qop"] != "auth" ||
		parameters["uri"] != request.URL.RequestURI() ||
		parameters["cnonce"] == "" || parameters["response"] == "" {
		return false
	}
	if len(parameters["nc"]) != 8 {
		return false
	}
	nonceCount, err := strconv.ParseUint(parameters["nc"], 16, 32)
	if err != nil || nonceCount == 0 {
		return false
	}
	newHash := digestHash(d.cfg().DigestAlgorithm)
	hashText := func(value string) string {
		digest := newHash()
		_, _ = io.WriteString(digest, value)
		return hex.EncodeToString(digest.Sum(nil))
	}
	ha1 := hashText(d.cfg().Username + ":" + d.cfg().DigestRealm + ":" + d.cfg().Password)
	ha2 := hashText(request.Method + ":" + parameters["uri"])
	expected := hashText(strings.Join([]string{ha1, parameters["nonce"], parameters["nc"], parameters["cnonce"], "auth", ha2}, ":"))
	if !secureEqual(parameters["response"], expected) {
		return false
	}

	// Replay state tracks the highest accepted nonce-count per
	// username/server-nonce/cnonce triple. RFC 7616 requires the nc value to
	// strictly increase for a given (nonce, cnonce) pair; a value at or below
	// the recorded high-water mark is a replay or rollback and is rejected,
	// while a strictly larger value (including a forward jump) is accepted and
	// becomes the new mark. Distinct cnonces are independent keys. Capacity is
	// counted in pairs, not requests, and an update to an existing pair always
	// succeeds even at capacity.
	replayKey := parameters["username"] + "\x00" + parameters["nonce"] + "\x00" + parameters["cnonce"]
	d.mu.Lock()
	defer d.mu.Unlock()
	highest, known := d.digestNC[replayKey]
	if known {
		if nonceCount <= highest {
			return false
		}
		d.digestNC[replayKey] = nonceCount
		return true
	}
	if len(d.digestNC) >= d.cfg().MaxAuthStateEntries {
		return false
	}
	d.digestNC[replayKey] = nonceCount
	return true
}

func (d *Device) writeDigestChallenge(response http.ResponseWriter, stale bool) {
	d.mu.RLock()
	nonce := d.digestNonce
	d.mu.RUnlock()
	challenge := `Digest realm="` + quoteHeaderValue(d.cfg().DigestRealm) + `", nonce="` + quoteHeaderValue(nonce) + `", algorithm=` + d.cfg().DigestAlgorithm + `, qop="auth"`
	if stale {
		challenge += `, stale=true`
	}
	response.Header().Set("WWW-Authenticate", challenge)
	response.WriteHeader(http.StatusUnauthorized)
}

func parseDigestAuthorization(value string) (map[string]string, bool) {
	value = strings.TrimSpace(value)
	if len(value) < len("Digest ") || !strings.EqualFold(value[:len("Digest")], "Digest") {
		return nil, false
	}
	value = strings.TrimSpace(value[len("Digest"):])
	parameters := make(map[string]string)
	for value != "" {
		value = strings.TrimLeft(value, " \t,")
		equals := strings.IndexByte(value, '=')
		if equals <= 0 {
			return nil, false
		}
		key := strings.ToLower(strings.TrimSpace(value[:equals]))
		value = strings.TrimSpace(value[equals+1:])
		var parsed string
		if strings.HasPrefix(value, `"`) {
			var ok bool
			parsed, value, ok = consumeQuoted(value)
			if !ok {
				return nil, false
			}
		} else {
			comma := strings.IndexByte(value, ',')
			if comma < 0 {
				parsed, value = strings.TrimSpace(value), ""
			} else {
				parsed, value = strings.TrimSpace(value[:comma]), value[comma+1:]
			}
		}
		if key == "" || parsed == "" {
			return nil, false
		}
		parameters[key] = parsed
	}
	return parameters, true
}

func consumeQuoted(value string) (string, string, bool) {
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
			rest := strings.TrimSpace(value[index+1:])
			return output.String(), strings.TrimPrefix(rest, ","), true
		}
		output.WriteByte(character)
	}
	return "", "", false
}

func digestHash(algorithm string) func() hash.Hash {
	if algorithm == "MD5" {
		return md5.New
	}
	return sha256.New
}

func randomDigestNonce() (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", fmt.Errorf("generate fake-device Digest nonce: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func nextSyntheticNonce(previous string) string {
	digest := sha256.Sum256([]byte("stale:" + previous))
	return hex.EncodeToString(digest[:16])
}

func secureEqual(first, second string) bool {
	return subtle.ConstantTimeCompare([]byte(first), []byte(second)) == 1
}

func quoteHeaderValue(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}
