package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestSecretIsOpaqueNonSerializableAndPoliciesRemainDistinct(t *testing.T) {
	t.Parallel()
	username := "synthetic-user-marker"
	password := "synthetic-password-marker"
	policy := AuthPolicy{AllowBasic: true, AllowBasicOverHTTP: true, AllowLegacyDigest: true}
	secret, err := NewSecret(username, password, policy)
	if err != nil {
		t.Fatalf("NewSecret() error = %v", err)
	}
	for _, output := range []string{
		fmt.Sprintf("%v", secret),
		fmt.Sprintf("%+v", secret),
		fmt.Sprintf("%#v", secret),
		fmt.Sprintf("%v", &secret),
		fmt.Sprintf("%+v", &secret),
		fmt.Sprintf("%#v", &secret),
	} {
		if strings.Contains(output, username) || strings.Contains(output, password) || !strings.Contains(output, "REDACTED") {
			t.Fatalf("formatted secret is unsafe: %q", output)
		}
	}
	if _, err := json.Marshal(secret); err == nil {
		t.Fatal("json.Marshal(value) error = nil")
	}
	if _, err := json.Marshal(&secret); err == nil {
		t.Fatal("json.Marshal(pointer) error = nil")
	}
	if secret.Username() != username || secret.Password() != password || secret.Policy() != policy {
		t.Fatal("secret accessors did not preserve the value")
	}
	if _, err := NewSecret("operator", "value", AuthPolicy{AllowBasicOverHTTP: true}); !errors.Is(err, ErrSecretInvalid) {
		t.Fatalf("incoherent policy error = %v, want ErrSecretInvalid", err)
	}
}

func TestMemorySecretProviderContract(t *testing.T) {
	t.Parallel()
	provider := NewMemorySecretProvider()
	secret, err := NewSecret("operator", "value", AuthPolicy{AllowLegacyDigest: true})
	if err != nil {
		t.Fatalf("NewSecret() error = %v", err)
	}
	ctx := context.Background()
	if _, err := provider.Get(ctx, "secret-one"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("initial Get() error = %v", err)
	}
	if err := provider.Put(ctx, "secret-one", secret); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	got, err := provider.Get(ctx, "secret-one")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Username() != secret.Username() || got.Password() != secret.Password() || got.Policy() != secret.Policy() {
		t.Fatal("Get() did not round-trip secret")
	}
	if err := provider.Delete(ctx, "secret-one"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := provider.Delete(ctx, "secret-one"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("second Delete() error = %v, want ErrSecretNotFound", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := provider.Put(cancelled, "secret-two", secret); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Put() error = %v", err)
	}
}

func TestKeyringProviderUsesVersionedPayloadAndStableService(t *testing.T) {
	t.Parallel()
	backend := newFakeKeyringAPI()
	provider := newKeyringSecretProvider(backend)
	policy := AuthPolicy{AllowBasic: true, AllowLegacyDigest: true}
	secret, err := NewSecret("operator", "synthetic-password-marker", policy)
	if err != nil {
		t.Fatalf("NewSecret() error = %v", err)
	}
	if err := provider.Put(context.Background(), "secret-reference", secret); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	backend.mu.Lock()
	if backend.lastService != KeyringService || backend.lastAccount != "secret-reference" {
		t.Fatalf("backend key = %q / %q", backend.lastService, backend.lastAccount)
	}
	encoded := backend.values["secret-reference"]
	backend.mu.Unlock()
	var payload keyringPayloadV1
	if err := json.Unmarshal([]byte(encoded), &payload); err != nil {
		t.Fatalf("payload decode error = %v", err)
	}
	decodedPolicy, validPolicy := authPolicyFromDisk(payload.AuthPolicy)
	if payload.SchemaVersion != keyringPayloadVersion || payload.Username != "operator" || payload.Password != "synthetic-password-marker" || !validPolicy || decodedPolicy != policy {
		t.Fatalf("payload = %#v", payload)
	}
	got, err := provider.Get(context.Background(), "secret-reference")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Username() != secret.Username() || got.Password() != secret.Password() || got.Policy() != policy {
		t.Fatal("keyring payload did not round-trip")
	}
	if err := provider.Delete(context.Background(), "secret-reference"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	backend.mu.Lock()
	_, remains := backend.values["secret-reference"]
	backend.mu.Unlock()
	if remains {
		t.Fatal("Delete() retained backend payload")
	}
}

func TestKeyringProviderMapsErrorsWithoutBackendMaterial(t *testing.T) {
	t.Parallel()
	secret, err := NewSecret("operator", "value", AuthPolicy{})
	if err != nil {
		t.Fatalf("NewSecret() error = %v", err)
	}
	tests := []struct {
		name       string
		backendErr error
		configure  func(*fakeKeyringAPI, error)
		operation  func(SecretProvider, Secret) error
		want       error
	}{
		{
			name:       "not found",
			backendErr: errFakeKeyringNotFound,
			configure:  func(backend *fakeKeyringAPI, backendErr error) { backend.getErr = backendErr },
			operation: func(provider SecretProvider, _ Secret) error {
				_, operationErr := provider.Get(context.Background(), "secret-one")
				return operationErr
			},
			want: ErrSecretNotFound,
		},
		{
			name:       "denied",
			backendErr: errors.New("permission denied: synthetic-backend-marker"),
			configure:  func(backend *fakeKeyringAPI, backendErr error) { backend.setErr = backendErr },
			operation: func(provider SecretProvider, value Secret) error {
				return provider.Put(context.Background(), "secret-one", value)
			},
			want: ErrSecretDenied,
		},
		{
			name:       "unavailable",
			backendErr: errors.New("backend exploded: synthetic-backend-marker"),
			configure:  func(backend *fakeKeyringAPI, backendErr error) { backend.deleteErr = backendErr },
			operation: func(provider SecretProvider, _ Secret) error {
				return provider.Delete(context.Background(), "secret-one")
			},
			want: ErrSecretUnavailable,
		},
		{
			name:       "os permission",
			backendErr: os.ErrPermission,
			configure:  func(backend *fakeKeyringAPI, backendErr error) { backend.deleteErr = backendErr },
			operation: func(provider SecretProvider, _ Secret) error {
				return provider.Delete(context.Background(), "secret-one")
			},
			want: ErrSecretDenied,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			backend := newFakeKeyringAPI()
			test.configure(backend, test.backendErr)
			err := test.operation(newKeyringSecretProvider(backend), secret)
			if !errors.Is(err, test.want) {
				t.Fatalf("operation error = %v, want %v", err, test.want)
			}
			if strings.Contains(err.Error(), "synthetic-backend-marker") {
				t.Fatalf("provider error leaked backend material: %q", err)
			}
		})
	}
}

func TestKeyringProviderRejectsMalformedAndFuturePayloads(t *testing.T) {
	t.Parallel()
	tests := []string{
		`not-json`,
		`{"schema_version":2,"username":"operator","password":"value","auth_policy":{"allow_basic":false,"allow_basic_over_http":false,"allow_legacy_digest":false}}`,
		`{"schema_version":1,"username":"operator","password":"value","unexpected":true,"auth_policy":{"allow_basic":false,"allow_basic_over_http":false,"allow_legacy_digest":false}}`,
		`{"schema_version":1,"username":"operator","username":"other","password":"value","auth_policy":{"allow_basic":false,"allow_basic_over_http":false,"allow_legacy_digest":false}}`,
		`{"SCHEMA_VERSION":1,"username":"operator","password":"value","auth_policy":{"allow_basic":false,"allow_basic_over_http":false,"allow_legacy_digest":false}}`,
		`{"schema_version":2,"SCHEMA_VERSION":1,"username":"operator","password":"value","auth_policy":{"allow_basic":false,"allow_basic_over_http":false,"allow_legacy_digest":false}}`,
		`{"schema_version":1,"username":"operator","password":"value","auth_policy":{"allow_basic":false,"allow_basic_over_http":false}}`,
	}
	for index, encoded := range tests {
		backend := newFakeKeyringAPI()
		backend.values["secret-one"] = encoded
		_, err := newKeyringSecretProvider(backend).Get(context.Background(), "secret-one")
		if !errors.Is(err, ErrSecretUnavailable) {
			t.Fatalf("payload %d error = %v, want ErrSecretUnavailable", index, err)
		}
	}
}

func TestGeneratedReferencesAreOpaqueAndUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{})
	for range 128 {
		reference, err := NewSecretReference()
		if err != nil {
			t.Fatalf("NewSecretReference() error = %v", err)
		}
		if !strings.HasPrefix(reference, "secret_") || !validIdentifier(reference) {
			t.Fatalf("reference = %q", reference)
		}
		if _, exists := seen[reference]; exists {
			t.Fatalf("duplicate reference = %q", reference)
		}
		seen[reference] = struct{}{}
	}
}

var errFakeKeyringNotFound = errors.New("fake keyring not found")

type fakeKeyringAPI struct {
	mu          sync.Mutex
	values      map[string]string
	setErr      error
	getErr      error
	deleteErr   error
	lastService string
	lastAccount string
}

func newFakeKeyringAPI() *fakeKeyringAPI {
	return &fakeKeyringAPI{values: make(map[string]string)}
}

func (backend *fakeKeyringAPI) Set(service, account, payload string) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.lastService = service
	backend.lastAccount = account
	if backend.setErr != nil {
		return backend.setErr
	}
	backend.values[account] = payload
	return nil
}

func (backend *fakeKeyringAPI) Get(service, account string) (string, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.lastService = service
	backend.lastAccount = account
	if backend.getErr != nil {
		return "", backend.getErr
	}
	value, exists := backend.values[account]
	if !exists {
		return "", errFakeKeyringNotFound
	}
	return value, nil
}

func (backend *fakeKeyringAPI) Delete(service, account string) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.lastService = service
	backend.lastAccount = account
	if backend.deleteErr != nil {
		return backend.deleteErr
	}
	if _, exists := backend.values[account]; !exists {
		return errFakeKeyringNotFound
	}
	delete(backend.values, account)
	return nil
}

func (*fakeKeyringAPI) IsNotFound(err error) bool {
	return errors.Is(err, errFakeKeyringNotFound)
}
