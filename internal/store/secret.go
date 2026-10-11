package store

import (
	"context"
	"errors"
	"sync"
	"unicode/utf8"
)

// SecretErrorKind is a stable secret-provider failure classification.
type SecretErrorKind string

const (
	SecretNotFound    SecretErrorKind = "not_found"
	SecretUnavailable SecretErrorKind = "unavailable"
	SecretDenied      SecretErrorKind = "denied"
	SecretUnsupported SecretErrorKind = "unsupported"
	SecretInvalid     SecretErrorKind = "invalid"
)

// SecretError is safe to display. It never wraps backend diagnostics or secret material.
type SecretError struct {
	Kind    SecretErrorKind
	message string
}

func (e *SecretError) Error() string {
	if e == nil {
		return ""
	}
	return e.message
}

// Is compares provider errors by stable kind.
func (e *SecretError) Is(target error) bool {
	other, ok := target.(*SecretError)
	return ok && e != nil && e.Kind == other.Kind
}

var (
	ErrSecretNotFound    = &SecretError{Kind: SecretNotFound, message: "the secret was not found"}
	ErrSecretUnavailable = &SecretError{Kind: SecretUnavailable, message: "the secret provider is unavailable"}
	ErrSecretDenied      = &SecretError{Kind: SecretDenied, message: "access to the secret provider was denied"}
	ErrSecretUnsupported = &SecretError{Kind: SecretUnsupported, message: "secure secret storage is unsupported on this platform"}
	ErrSecretInvalid     = &SecretError{Kind: SecretInvalid, message: "the secret or reference is invalid"}
)

// SecretErrorKindOf returns the stable provider kind, or empty for another error type.
func SecretErrorKindOf(err error) SecretErrorKind {
	var secretErr *SecretError
	if errors.As(err, &secretErr) {
		return secretErr.Kind
	}
	return ""
}

// Secret is an opaque, deliberately non-serializable credential value. The
// keyring adapter serializes a private versioned payload rather than this type.
type Secret struct {
	username string
	password string
	policy   AuthPolicy
}

// NewSecret validates and constructs an ephemeral secret value. It does not
// validate credentials against a camera; that is the application layer's responsibility.
func NewSecret(username, password string, policy AuthPolicy) (Secret, error) {
	secret := Secret{username: username, password: password, policy: policy}
	if err := secret.validate(); err != nil {
		return Secret{}, err
	}
	return secret, nil
}

// Username returns the ephemeral username for application-layer credential construction.
func (secret Secret) Username() string { return secret.username }

// Password returns the ephemeral password for application-layer credential construction.
func (secret Secret) Password() string { return secret.password }

// Policy returns a copy of the three authentication policy decisions.
func (secret Secret) Policy() AuthPolicy { return secret.policy }

// String always redacts the secret.
func (Secret) String() string { return "store.Secret{REDACTED}" }

// GoString always redacts the secret.
func (Secret) GoString() string { return "store.Secret{REDACTED}" }

// MarshalJSON refuses generic serialization. Only private provider payloads are serializable.
func (Secret) MarshalJSON() ([]byte, error) {
	return nil, errors.New("store.Secret cannot be serialized")
}

func (secret Secret) validate() error {
	if secret.username == "" || len(secret.username) > 1024 || len(secret.password) > 1024 {
		return ErrSecretInvalid
	}
	if !utf8.ValidString(secret.username) || !utf8.ValidString(secret.password) {
		return ErrSecretInvalid
	}
	if err := validateAuthPolicy(secret.policy); err != nil {
		return ErrSecretInvalid
	}
	return nil
}

// SecretProvider stores opaque credential payloads by random account reference.
type SecretProvider interface {
	Put(ctx context.Context, reference string, secret Secret) error
	Get(ctx context.Context, reference string) (Secret, error)
	Delete(ctx context.Context, reference string) error
}

// MemorySecretProvider is a concurrency-safe, process-local provider for tests
// and explicit demo composition. It is never selected as a native fallback.
type MemorySecretProvider struct {
	mu      sync.RWMutex
	secrets map[string]Secret
}

// NewMemorySecretProvider creates an empty in-memory provider.
func NewMemorySecretProvider() *MemorySecretProvider {
	return &MemorySecretProvider{secrets: make(map[string]Secret)}
}

func (provider *MemorySecretProvider) Put(ctx context.Context, reference string, secret Secret) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if provider == nil || !validIdentifier(reference) || secret.validate() != nil {
		return ErrSecretInvalid
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.secrets[reference] = secret
	return nil
}

func (provider *MemorySecretProvider) Get(ctx context.Context, reference string) (Secret, error) {
	if err := contextError(ctx); err != nil {
		return Secret{}, err
	}
	if provider == nil || !validIdentifier(reference) {
		return Secret{}, ErrSecretInvalid
	}
	provider.mu.RLock()
	defer provider.mu.RUnlock()
	secret, exists := provider.secrets[reference]
	if !exists {
		return Secret{}, ErrSecretNotFound
	}
	return secret, nil
}

func (provider *MemorySecretProvider) Delete(ctx context.Context, reference string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if provider == nil || !validIdentifier(reference) {
		return ErrSecretInvalid
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if _, exists := provider.secrets[reference]; !exists {
		return ErrSecretNotFound
	}
	delete(provider.secrets, reference)
	return nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return ErrSecretInvalid
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
