package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestCredentialsAreOpaqueForValuesPointersAndJSON(t *testing.T) {
	t.Parallel()

	identityMarker := "synthetic-user-marker"
	sensitiveMarker := strings.Join([]string{"synthetic", "passphrase", "marker"}, "-")
	credentials, err := NewCredentials(identityMarker, sensitiveMarker, true)
	if err != nil {
		t.Fatalf("NewCredentials() error = %v", err)
	}

	formatted := []string{
		fmt.Sprintf("%v", credentials),
		fmt.Sprintf("%+v", credentials),
		fmt.Sprintf("%#v", credentials),
		fmt.Sprintf("%v", *credentials),
		fmt.Sprintf("%+v", *credentials),
		fmt.Sprintf("%#v", *credentials),
	}
	for _, output := range formatted {
		if strings.Contains(output, identityMarker) || strings.Contains(output, sensitiveMarker) {
			t.Fatalf("formatted credentials leaked material: %q", output)
		}
		if !strings.Contains(output, "REDACTED") {
			t.Fatalf("formatted credentials = %q, want redaction marker", output)
		}
	}
	if _, err := json.Marshal(credentials); err == nil {
		t.Fatal("json.Marshal(pointer) error = nil, want serialization refusal")
	}
	if _, err := json.Marshal(*credentials); err == nil {
		t.Fatal("json.Marshal(value) error = nil, want serialization refusal")
	}
}

func TestCredentialsKeepBasicTransportPoliciesIndependent(t *testing.T) {
	t.Parallel()

	httpsOnly, err := NewCredentials("operator", "value", true)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	if !httpsOnly.AllowBasic() || httpsOnly.AllowBasicOverHTTP() {
		t.Fatalf("HTTPS-only Basic policy collapsed: basic=%t plaintext=%t", httpsOnly.AllowBasic(), httpsOnly.AllowBasicOverHTTP())
	}

	plaintext, err := NewCredentialsWithPolicy("operator", "value", Policy{
		AllowBasic:         true,
		AllowBasicOverHTTP: true,
	})
	if err != nil {
		t.Fatalf("NewCredentialsWithPolicy: %v", err)
	}
	if !plaintext.AllowBasic() || !plaintext.AllowBasicOverHTTP() {
		t.Fatal("explicit plaintext Basic policy was not preserved")
	}
}

func TestCredentialsRejectIncoherentPlaintextBasicPolicy(t *testing.T) {
	t.Parallel()

	_, err := NewCredentialsWithPolicy("operator", "value", Policy{AllowBasicOverHTTP: true})
	if err == nil {
		t.Fatal("expected plaintext Basic without Basic to be rejected")
	}
}
