package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidateSnapshotProfileTokenPolicy(t *testing.T) {
	t.Parallel()

	valid := []string{
		"profile_main",
		`profile<&>"'`,
		" profile with spaces ",
		strings.Repeat("界", maxSnapshotProfileTokenCodePoints),
	}
	for _, token := range valid {
		if err := ValidateSnapshotProfileToken(token); err != nil {
			t.Errorf("valid token rejected: %v", err)
		}
	}

	invalid := []string{
		"",
		"   ",
		string([]byte{0xff}),
		"profile\x00main",
		"profile\u0085main",
		"profile\ufffdmain",
		strings.Repeat("界", maxSnapshotProfileTokenCodePoints+1),
	}
	for _, token := range invalid {
		err := ValidateSnapshotProfileToken(token)
		if err == nil || CategoryOf(err) != CategoryUsage {
			t.Errorf("invalid token category = %q (err=%v), want usage", CategoryOf(err), err)
		}
		if err != nil && err.Error() != "the explicit profile token is not valid" {
			t.Errorf("unsafe or unstable validation error: %q", err)
		}
	}
}

// TestSnapshotInvalidProfileRejectedBeforeAnyRequest proves the application
// boundary rejects caller token errors before endpoint preparation, clock
// negotiation, GetServices, or any other HTTP request.
func TestSnapshotInvalidProfileRejectedBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		atomic.AddInt32(&requests, 1)
	}))
	defer server.Close()

	credentials, err := NewCredentials("operator", "synthetic-password", false)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	invalid := []string{
		"   ",
		string([]byte{0xff}),
		"profile\x1fmain",
		"profile\u0085main",
		"profile\ufffdmain",
		"\u00a0 \t",
		strings.Repeat("界", maxSnapshotProfileTokenCodePoints+1),
	}
	for index, token := range invalid {
		_, snapshotErr := NewProductionSnapshot().Snapshot(context.Background(), SnapshotRequest{
			Host:         server.URL + "/onvif/device_service",
			Credentials:  credentials,
			ProfileToken: token,
			OutputPath:   filepath.Join(t.TempDir(), "snapshot.jpg"),
			Timeout:      time.Second,
		})
		if snapshotErr == nil || CategoryOf(snapshotErr) != CategoryUsage {
			t.Fatalf("case %d category = %q (err=%v), want usage", index, CategoryOf(snapshotErr), snapshotErr)
		}
	}
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Fatalf("invalid profile tokens caused %d network requests, want 0", got)
	}
}
