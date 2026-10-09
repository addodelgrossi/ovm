package soap

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

type failingRoundTripper struct {
	err error
}

func (f failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, f.err
}

func TestTransportErrorDoesNotRenderEndpointQuery(t *testing.T) {
	t.Parallel()

	cause := errors.New("synthetic dial failure")
	client, err := NewClient(Config{HTTPClient: &http.Client{Transport: failingRoundTripper{err: cause}}})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	const marker = "query-sensitive-value"
	_, err = client.Call(context.Background(), "http://camera.invalid/onvif/device_service?opaque="+marker, Request{
		Action:  "urn:synthetic:operation",
		BodyXML: []byte(`<Get/>`),
	})
	if err == nil {
		t.Fatal("Call() error = nil, want transport failure")
	}
	if strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), "camera.invalid") {
		t.Fatalf("transport error rendered endpoint data: %q", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want wrapped transport cause", err)
	}
	var transportErr *TransportError
	if !errors.As(err, &transportErr) || transportErr.Version != Version12 {
		t.Fatalf("error = %#v, want SOAP 1.2 TransportError", err)
	}
}
