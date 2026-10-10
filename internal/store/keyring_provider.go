package store

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"syscall"
)

const (
	// KeyringService is the stable native secret-store service identifier.
	KeyringService         = "io.github.addodelgrossi.ovm"
	keyringPayloadVersion  = 1
	maxKeyringPayloadBytes = 2500
)

type keyringAPI interface {
	Set(service, account, payload string) error
	Get(service, account string) (string, error)
	Delete(service, account string) error
	IsNotFound(error) bool
}

type keyringSecretProvider struct {
	api keyringAPI
}

type keyringPayloadV1 struct {
	SchemaVersion int               `json:"schema_version"`
	Username      string            `json:"username"`
	Password      string            `json:"password"`
	AuthPolicy    *diskAuthPolicyV1 `json:"auth_policy"`
}

func newKeyringSecretProvider(api keyringAPI) SecretProvider {
	return &keyringSecretProvider{api: api}
}

func (provider *keyringSecretProvider) Put(ctx context.Context, reference string, secret Secret) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if provider == nil || provider.api == nil || !validIdentifier(reference) || secret.validate() != nil {
		return ErrSecretInvalid
	}
	// This private DTO is the only authorized serialization boundary, and its bytes go directly to native secret storage.
	payload, err := json.Marshal(keyringPayloadV1{ //nolint:gosec // password serialization is required inside the OS keyring payload
		SchemaVersion: keyringPayloadVersion,
		Username:      secret.username,
		Password:      secret.password,
		AuthPolicy:    authPolicyToDisk(secret.policy),
	})
	if err != nil || len(payload) > maxKeyringPayloadBytes {
		return ErrSecretInvalid
	}
	if err := provider.api.Set(KeyringService, reference, string(payload)); err != nil {
		return classifyKeyringError(provider.api, err)
	}
	return nil
}

func (provider *keyringSecretProvider) Get(ctx context.Context, reference string) (Secret, error) {
	if err := contextError(ctx); err != nil {
		return Secret{}, err
	}
	if provider == nil || provider.api == nil || !validIdentifier(reference) {
		return Secret{}, ErrSecretInvalid
	}
	encoded, err := provider.api.Get(KeyringService, reference)
	if err != nil {
		return Secret{}, classifyKeyringError(provider.api, err)
	}
	if len(encoded) > maxKeyringPayloadBytes {
		return Secret{}, ErrSecretUnavailable
	}
	var header schemaHeader
	if err := json.Unmarshal([]byte(encoded), &header); err != nil || header.SchemaVersion == nil || *header.SchemaVersion != keyringPayloadVersion {
		return Secret{}, ErrSecretUnavailable
	}
	var payload keyringPayloadV1
	if err := decodeStrictJSON([]byte(encoded), &payload); err != nil {
		return Secret{}, ErrSecretUnavailable
	}
	policy, validPolicy := authPolicyFromDisk(payload.AuthPolicy)
	if !validPolicy {
		return Secret{}, ErrSecretUnavailable
	}
	secret, err := NewSecret(payload.Username, payload.Password, policy)
	if err != nil {
		return Secret{}, ErrSecretUnavailable
	}
	return secret, nil
}

func (provider *keyringSecretProvider) Delete(ctx context.Context, reference string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if provider == nil || provider.api == nil || !validIdentifier(reference) {
		return ErrSecretInvalid
	}
	if err := provider.api.Delete(KeyringService, reference); err != nil {
		return classifyKeyringError(provider.api, err)
	}
	return nil
}

func classifyKeyringError(api keyringAPI, err error) error {
	if api != nil && api.IsNotFound(err) {
		return ErrSecretNotFound
	}
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return ErrSecretDenied
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"access denied", "permission denied", "authorization denied", "interaction is not allowed", "user canceled", "user cancelled"} {
		if strings.Contains(message, marker) {
			return ErrSecretDenied
		}
	}
	return ErrSecretUnavailable
}
