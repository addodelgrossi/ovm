package app

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/auth"
)

// TestClassifySnapshotTransportErrorNeverRetainsURLError proves the transport
// error classifier never keeps a *url.Error (which embeds the request URL and
// query) anywhere in the returned error's unwrap chain. It only retains stable
// context sentinels or no cause at all.
func TestClassifySnapshotTransportErrorNeverRetainsURLError(t *testing.T) {
	t.Parallel()
	const leaked = "http://cam.local/snap?profile=secret-marker"
	for _, in := range []*url.Error{
		{Op: "Get", URL: leaked, Err: errors.New("dial tcp: connection refused")},
		{Op: "Get", URL: leaked, Err: context.Canceled},
		{Op: "Get", URL: leaked, Err: context.DeadlineExceeded},
		{Op: "Get", URL: leaked, Err: auth.ErrAuthPolicy},
		{Op: "Get", URL: leaked, Err: ErrInsecureDigestBlocked},
	} {
		got := classifySnapshotTransportError(in)
		for cur := got; cur != nil; cur = errors.Unwrap(cur) {
			var ue *url.Error
			if errors.As(cur, &ue) {
				t.Fatalf("classified error retained a *url.Error: %v", cur)
			}
			if strings.Contains(cur.Error(), "cam.local") || strings.Contains(cur.Error(), "secret-marker") {
				t.Fatalf("classified error leaked the URL/query: %v", cur)
			}
		}
	}

	// Policy refusals must classify as authentication.
	for _, in := range []*url.Error{
		{Op: "Get", URL: leaked, Err: auth.ErrAuthPolicy},
		{Op: "Get", URL: leaked, Err: ErrInsecureDigestBlocked},
	} {
		if CategoryOf(classifySnapshotTransportError(in)) != CategoryAuthentication {
			t.Fatalf("policy refusal not categorized as authentication: %v", in)
		}
	}
	// Cancellation keeps only its stable sentinel.
	cancelled := classifySnapshotTransportError(&url.Error{Op: "Get", URL: leaked, Err: context.Canceled})
	if CategoryOf(cancelled) != CategoryCancelled || !errors.Is(cancelled, context.Canceled) {
		t.Fatalf("cancellation classification wrong: %v", cancelled)
	}
	deadline := classifySnapshotTransportError(&url.Error{Op: "Get", URL: leaked, Err: context.DeadlineExceeded})
	if CategoryOf(deadline) != CategoryNetwork || !errors.Is(deadline, context.DeadlineExceeded) {
		t.Fatalf("deadline classification wrong: %v", deadline)
	}
}
