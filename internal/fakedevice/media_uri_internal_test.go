package fakedevice

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestMediaURIBuilderHandlesIPv6AndEscapesTokens(t *testing.T) {
	t.Parallel()
	origin := requestOrigin{httpScheme: "https", host: "[2001:db8::20]:8443"}
	//nolint:gosec // This is a synthetic ONVIF reference token, not a credential.
	token := "main & sub"

	stream, err := url.Parse(origin.streamURI("/live/main", token, "RtspsUnicast"))
	if err != nil {
		t.Fatalf("parse stream URI: %v", err)
	}
	if stream.Scheme != "rtsps" || stream.Host != "[2001:db8::20]:554" || stream.Path != "/live/main" {
		t.Fatalf("stream URI = %s", stream.String())
	}
	if stream.Query().Get("profile") != token || stream.Query().Get("transport") != "RtspsUnicast" {
		t.Fatalf("stream query = %q", stream.RawQuery)
	}

	snapshot, err := url.Parse(origin.snapshotURI("/snapshot.jpg", token, ""))
	if err != nil {
		t.Fatalf("parse snapshot URI: %v", err)
	}
	if snapshot.Scheme != "https" || snapshot.Host != "[2001:db8::20]:8443" || snapshot.Query().Get("profile") != token {
		t.Fatalf("snapshot URI = %s", snapshot.String())
	}
}

func TestMediaURIBuilderDoesNotReflectUnsafeHostAuthority(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest("POST", "http://device.invalid/onvif/media", nil)
	request.Host = "viewer:host-secret@attacker.invalid"
	origin := originFromRequest(request)
	uri := origin.snapshotURI("/snapshot.jpg", "main", "")
	if strings.Contains(uri, "host-secret") || strings.Contains(uri, "attacker.invalid") {
		t.Fatalf("unsafe Host authority was reflected: %s", uri)
	}
	if !strings.Contains(uri, "device.invalid") {
		t.Fatalf("unsafe Host authority did not fall back safely: %s", uri)
	}
}
