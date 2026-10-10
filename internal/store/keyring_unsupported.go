//go:build (!darwin && !linux) || android || ios

package store

// NewPlatformSecretProvider refuses persistence on unsupported targets. It
// never falls back to memory or plaintext storage.
func NewPlatformSecretProvider() (SecretProvider, error) {
	return nil, ErrSecretUnsupported
}
