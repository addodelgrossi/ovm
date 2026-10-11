package fakedevice_test

import (
	"context"
	"crypto/md5" // #nosec G501 -- test reproduces the HTTP Digest MD5 formula the device supports.
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
)

// md5Hex returns the lowercase hex MD5 of value, matching the device's digest
// hashing when DigestAlgorithm is MD5.
func md5Hex(value string) string {
	sum := md5.Sum([]byte(value)) // #nosec G401 -- HTTP Digest MD5 formula.
	return hex.EncodeToString(sum[:])
}

// digestAuthHeader builds a qop=auth, MD5 HTTP Digest Authorization header value
// for a GET-style POST request (the SOAP transport uses POST, but these helpers
// drive the handler directly and set method POST below).
func digestAuthHeader(username, realm, nonce, uri, cnonce, nc, password string) string {
	ha1 := md5Hex(username + ":" + realm + ":" + password)
	ha2 := md5Hex(http.MethodPost + ":" + uri)
	response := md5Hex(strings.Join([]string{ha1, nonce, nc, cnonce, "auth", ha2}, ":"))
	return `Digest username="` + username + `", realm="` + realm + `", nonce="` + nonce +
		`", uri="` + uri + `", qop=auth, nc=` + nc + `, cnonce="` + cnonce +
		`", response="` + response + `", algorithm=MD5`
}

// rawDigestStatus issues a POST with the given Authorization header and returns
// the HTTP status code. It targets the handler path in uri so the digest "uri"
// parameter matches request.URL.RequestURI().
func rawDigestStatus(t *testing.T, serverURL, uri, authHeader string) int {
	t.Helper()
	status, _ := rawDigestStatusWithNonce(t, serverURL, uri, authHeader)
	return status
}

// rawDigestStatusWithNonce issues a POST and returns both the HTTP status and,
// when the response carries a WWW-Authenticate challenge, the server nonce
// parsed from it (empty when absent).
func rawDigestStatusWithNonce(t *testing.T, serverURL, uri, authHeader string) (int, string) {
	t.Helper()
	body := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope">` +
		`<env:Body><tds:GetSystemDateAndTime xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/></env:Body>` +
		`</env:Envelope>`
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, serverURL+uri, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/soap+xml")
	request.Header.Set("Authorization", authHeader)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode, parseChallengeNonce(response.Header.Get("WWW-Authenticate"))
}

// parseChallengeNonce extracts the nonce value from a Digest WWW-Authenticate
// challenge header. It returns an empty string when no nonce is present.
func parseChallengeNonce(header string) string {
	const marker = `nonce="`
	start := strings.Index(header, marker)
	if start < 0 {
		return ""
	}
	start += len(marker)
	end := strings.IndexByte(header[start:], '"')
	if end < 0 {
		return ""
	}
	return header[start : start+end]
}
