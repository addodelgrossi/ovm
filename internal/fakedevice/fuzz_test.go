package fakedevice

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func FuzzDeviceMalformedSOAPDoesNotPanic(f *testing.F) {
	f.Add([]byte(`<Envelope xmlns="http://www.w3.org/2003/05/soap-envelope"><Body><Get/></Body></Envelope>`), "application/soap+xml")
	f.Add([]byte(`<!DOCTYPE x><Envelope/>`), "text/xml")
	f.Add([]byte(`<broken`), "application/soap+xml")
	f.Fuzz(func(t *testing.T, body []byte, contentType string) {
		device, err := New(Config{MaxRequestBytes: 4096})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		request := httptest.NewRequest(http.MethodPost, "http://device.invalid/onvif", bytes.NewReader(body))
		request.Header.Set("Content-Type", contentType)
		response := httptest.NewRecorder()
		device.ServeHTTP(response, request)
		if response.Code < 100 || response.Code > 599 {
			t.Fatalf("invalid HTTP status %d", response.Code)
		}
	})
}
