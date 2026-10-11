package auth

import "testing"

func FuzzParseAuthenticateChallengesDoesNotPanic(f *testing.F) {
	f.Add(`Digest realm="camera", nonce="nonce", algorithm=SHA-256, qop="auth"`)
	f.Add(`Basic realm="camera"`)
	f.Add(`Digest realm="escaped\\\"realm", nonce="n", stale=true`)
	f.Add("")
	f.Fuzz(func(t *testing.T, value string) {
		_ = parseAuthenticateChallenges([]string{value})
	})
}
