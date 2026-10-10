//go:build darwin && !ios

package store

import (
	"errors"

	keyring "github.com/zalando/go-keyring"
)

type platformKeyringAPI struct{}

func (platformKeyringAPI) Set(service, account, payload string) error {
	return keyring.Set(service, account, payload)
}

func (platformKeyringAPI) Get(service, account string) (string, error) {
	return keyring.Get(service, account)
}

func (platformKeyringAPI) Delete(service, account string) error {
	return keyring.Delete(service, account)
}

func (platformKeyringAPI) IsNotFound(err error) bool {
	return errors.Is(err, keyring.ErrNotFound)
}

// NewPlatformSecretProvider returns the macOS Keychain-backed provider.
func NewPlatformSecretProvider() (SecretProvider, error) {
	return newKeyringSecretProvider(platformKeyringAPI{}), nil
}
