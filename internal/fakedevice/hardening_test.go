package fakedevice_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
	"github.com/addodelgrossi/ovm/pkg/onvif/auth"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

func TestDeviceRejectsForeignUsernameTokenNamespaces(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	device := newDevice(t, fakedevice.Config{
		RequireUsernameToken: true,
		Username:             "operator",
		Password:             "secret",
		Clock:                func() time.Time { return base },
	})
	server := httptest.NewServer(device)
	defer server.Close()
	generator := auth.UsernameTokenGenerator{
		Random:    bytes.NewReader(bytes.Repeat([]byte{0x31}, 20)),
		Now:       func() time.Time { return base },
		NonceSize: 20,
	}
	token, err := generator.Generate("operator", "secret", 0)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	header, err := token.HeaderXML()
	if err != nil {
		t.Fatalf("HeaderXML() error = %v", err)
	}
	foreignHeader := strings.ReplaceAll(string(header), "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd", "urn:foreign-wsse")

	_, err = newSOAPClient(t, nil).Call(context.Background(), server.URL, soap.Request{
		Action:    fakedevice.ActionGetSystemDateAndTime,
		HeaderXML: []byte(foreignHeader),
		BodyXML:   []byte(`<tds:GetSystemDateAndTime xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`),
	})
	if !errors.Is(err, soap.ErrNotAuthorized) {
		t.Fatalf("Call() error = %v, want ErrNotAuthorized", err)
	}
}

func TestDeviceRejectsActionBodyMismatchAndWrongOperationNamespace(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{InitialState: map[string]string{"mode": "day"}})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)

	_, err := client.Call(context.Background(), server.URL, soap.Request{
		Action:  fakedevice.ActionSetState,
		BodyXML: []byte(`<ovm:GetState xmlns:ovm="urn:ovm:synthetic"><ovm:Key>mode</ovm:Key><ovm:Value>night</ovm:Value></ovm:GetState>`),
	})
	if !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("mismatched Call() error = %v, want ErrInvalidArgVal", err)
	}
	if value, _ := device.State("mode"); value != "day" {
		t.Fatalf("mismatched action mutated state to %q", value)
	}

	_, err = client.Call(context.Background(), server.URL, soap.Request{
		Action:  fakedevice.ActionGetSystemDateAndTime,
		BodyXML: []byte(`<evil:GetSystemDateAndTime xmlns:evil="urn:foreign"/>`),
	})
	if !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("wrong-namespace Call() error = %v, want ErrInvalidArgVal", err)
	}
}

func TestDeviceBoundsRequestHistory(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{MaxRequestHistory: 2})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)
	for range 3 {
		_, err := client.Call(context.Background(), server.URL, soap.Request{
			Action:  fakedevice.ActionGetSystemDateAndTime,
			BodyXML: []byte(`<tds:GetSystemDateAndTime xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`),
		})
		if err != nil {
			t.Fatalf("Call() error = %v", err)
		}
	}
	if records := device.Requests(); len(records) != 2 {
		t.Fatalf("request history length = %d, want 2", len(records))
	}
}

func TestDeviceSetFaultReportsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{})
	if err := device.SetFault("", fakedevice.FaultSpec{Kind: "invalid"}); err == nil {
		t.Fatal("SetFault() error = nil for invalid configuration")
	}
}

func TestDeviceDigestStaleChallengeAndUsernameTokenReplay(t *testing.T) {
	t.Parallel()

	t.Run("Digest stale challenge", func(t *testing.T) {
		device := newDevice(t, fakedevice.Config{
			HTTPAuth:        fakedevice.HTTPAuthDigest,
			DigestStaleOnce: true,
			Username:        "operator",
			Password:        "secret",
		})
		server := httptest.NewServer(device)
		defer server.Close()
		scope, err := url.Parse(server.URL)
		if err != nil {
			t.Fatalf("parse scope: %v", err)
		}
		transport, err := auth.NewDigestTransport(auth.DigestTransportConfig{
			Scope:       scope,
			Credentials: auth.NewCredentials("operator", "secret"),
		})
		if err != nil {
			t.Fatalf("NewDigestTransport() error = %v", err)
		}
		client := newSOAPClient(t, &http.Client{Transport: transport})
		_, err = client.Call(context.Background(), server.URL, soap.Request{
			Action:  fakedevice.ActionGetSystemDateAndTime,
			BodyXML: []byte(`<tds:GetSystemDateAndTime xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`),
		})
		if err != nil {
			t.Fatalf("Call() error = %v", err)
		}
	})

	t.Run("UsernameToken replay", func(t *testing.T) {
		base := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
		device := newDevice(t, fakedevice.Config{
			RequireUsernameToken: true,
			Username:             "operator",
			Password:             "secret",
			Clock:                func() time.Time { return base },
		})
		server := httptest.NewServer(device)
		defer server.Close()
		token, err := (auth.UsernameTokenGenerator{
			Random:    bytes.NewReader(bytes.Repeat([]byte{0x62}, 20)),
			Now:       func() time.Time { return base },
			NonceSize: 20,
		}).Generate("operator", "secret", 0)
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		header, err := token.HeaderXML()
		if err != nil {
			t.Fatalf("HeaderXML() error = %v", err)
		}
		request := soap.Request{
			Action:    fakedevice.ActionGetSystemDateAndTime,
			HeaderXML: header,
			BodyXML:   []byte(`<tds:GetSystemDateAndTime xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`),
		}
		client := newSOAPClient(t, nil)
		if _, err := client.Call(context.Background(), server.URL, request); err != nil {
			t.Fatalf("first Call() error = %v", err)
		}
		if _, err := client.Call(context.Background(), server.URL, request); !errors.Is(err, soap.ErrNotAuthorized) {
			t.Fatalf("replay Call() error = %v, want ErrNotAuthorized", err)
		}
	})
}
