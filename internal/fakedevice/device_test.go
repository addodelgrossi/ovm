package fakedevice_test

import (
	"bytes"
	"context"
	"errors"
	"io"
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

func TestDeviceSOAPVersionNegotiationAndNamespaceVariation(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{
		SupportedSOAPVersions: []fakedevice.SOAPVersion{fakedevice.SOAP11},
		VersionRejection:      fakedevice.RejectWithVersionMismatch,
		EnvelopePrefix:        "cameraEnvelope",
		OperationPrefix:       "cameraDevice",
	})
	server := httptest.NewServer(device)
	defer server.Close()

	client := newSOAPClient(t, nil)
	response, err := client.Call(context.Background(), server.URL, soap.Request{
		Action:  fakedevice.ActionGetSystemDateAndTime,
		BodyXML: []byte(`<tds:GetSystemDateAndTime xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if response.Version != soap.Version11 {
		t.Fatalf("Version = %s, want SOAP 1.1 fallback", response.Version)
	}
	if !strings.Contains(string(response.RawXML), "GetSystemDateAndTimeResponse") {
		t.Errorf("RawXML = %s", response.RawXML)
	}
	records := device.Requests()
	if len(records) != 2 || records[0].Version != fakedevice.SOAP12 || records[1].Version != fakedevice.SOAP11 {
		t.Fatalf("request records = %+v, want SOAP 1.2 then 1.1", records)
	}
}

func TestDeviceUsernameTokenWithClockSkew(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	device := newDevice(t, fakedevice.Config{
		RequireUsernameToken: true,
		Username:             "operator",
		Password:             "camera-secret",
		ClockSkew:            3 * time.Minute,
		Clock:                func() time.Time { return base },
	})
	server := httptest.NewServer(device)
	defer server.Close()

	generator := auth.UsernameTokenGenerator{
		Random:    bytes.NewReader(bytes.Repeat([]byte{0x24}, 20)),
		Now:       func() time.Time { return base },
		NonceSize: 20,
	}
	token, err := generator.Generate("operator", "camera-secret", 3*time.Minute)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	header, err := token.HeaderXML()
	if err != nil {
		t.Fatalf("HeaderXML() error = %v", err)
	}

	client := newSOAPClient(t, nil)
	response, err := client.Call(context.Background(), server.URL, soap.Request{
		Action:    fakedevice.ActionGetSystemDateAndTime,
		HeaderXML: header,
		BodyXML:   []byte(`<tds:GetSystemDateAndTime xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if !strings.Contains(string(response.BodyXML), "<tt:Hour>12</tt:Hour>") || !strings.Contains(string(response.BodyXML), "<tt:Minute>3</tt:Minute>") {
		t.Errorf("BodyXML = %s, want three-minute-skewed device UTC time", response.BodyXML)
	}
	for _, record := range device.Requests() {
		if strings.Contains(record.Action, "camera-secret") {
			t.Fatalf("request record leaked credentials: %+v", record)
		}
	}
}

func TestDeviceRejectsInvalidUsernameTokenAsTypedFault(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{
		RequireUsernameToken: true,
		Username:             "operator",
		Password:             "camera-secret",
	})
	server := httptest.NewServer(device)
	defer server.Close()

	client := newSOAPClient(t, nil)
	_, err := client.Call(context.Background(), server.URL, soap.Request{
		Action:    fakedevice.ActionGetSystemDateAndTime,
		HeaderXML: []byte(`<wsse:Security xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"><wsse:UsernameToken><wsse:Username>operator</wsse:Username><wsse:Password>incorrect</wsse:Password></wsse:UsernameToken></wsse:Security>`),
		BodyXML:   []byte(`<tds:GetSystemDateAndTime xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`),
	})
	if !errors.Is(err, soap.ErrNotAuthorized) {
		t.Fatalf("Call() error = %v, want ErrNotAuthorized", err)
	}
}

func TestDeviceHTTPDigestAndExplicitBasic(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name       string
		mode       fakedevice.HTTPAuthMode
		algorithm  string
		allowBasic bool
	}{
		{name: "Digest SHA-256", mode: fakedevice.HTTPAuthDigest, algorithm: "SHA-256"},
		{name: "Digest MD5", mode: fakedevice.HTTPAuthDigest, algorithm: "MD5"},
		{name: "explicit Basic", mode: fakedevice.HTTPAuthBasic, allowBasic: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			device := newDevice(t, fakedevice.Config{
				HTTPAuth:        tt.mode,
				DigestAlgorithm: tt.algorithm,
				Username:        "operator",
				Password:        "camera-secret",
			})
			server := httptest.NewServer(device)
			defer server.Close()

			scope, err := url.Parse(server.URL)
			if err != nil {
				t.Fatalf("parse server URL: %v", err)
			}
			transport, err := auth.NewDigestTransport(auth.DigestTransportConfig{
				Scope:       scope,
				Credentials: auth.NewCredentials("operator", "camera-secret"),
				AllowBasic:  tt.allowBasic,
				// httptest serves plaintext HTTP, so the explicit-Basic case
				// must additionally permit Basic over HTTP; the Digest cases
				// leave both flags false.
				AllowBasicOverHTTP: tt.allowBasic,
			})
			if err != nil {
				t.Fatalf("NewDigestTransport() error = %v", err)
			}
			httpClient := &http.Client{Transport: transport}
			client := newSOAPClient(t, httpClient)
			response, err := client.Call(context.Background(), server.URL, soap.Request{
				Action:  fakedevice.ActionGetSystemDateAndTime,
				BodyXML: []byte(`<tds:GetSystemDateAndTime xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`),
			})
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if response.HTTPStatus != http.StatusOK {
				t.Errorf("HTTPStatus = %d, want 200", response.HTTPStatus)
			}
		})
	}
}

func TestDeviceMutableStateFaultsAndUnsupportedOperations(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{InitialState: map[string]string{"mode": "day"}})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)

	setResponse, err := client.Call(context.Background(), server.URL, soap.Request{
		Action:  fakedevice.ActionSetState,
		BodyXML: []byte(`<ovm:SetState xmlns:ovm="urn:ovm:synthetic"><ovm:Key>mode</ovm:Key><ovm:Value>night</ovm:Value></ovm:SetState>`),
	})
	if err != nil {
		t.Fatalf("SetState Call() error = %v", err)
	}
	if !strings.Contains(string(setResponse.BodyXML), "SetStateResponse") {
		t.Errorf("set BodyXML = %s", setResponse.BodyXML)
	}
	if value, ok := device.State("mode"); !ok || value != "night" {
		t.Fatalf("State(mode) = (%q, %v), want (night, true)", value, ok)
	}

	getResponse, err := client.Call(context.Background(), server.URL, soap.Request{
		Action:  fakedevice.ActionGetState,
		BodyXML: []byte(`<ovm:GetState xmlns:ovm="urn:ovm:synthetic"><ovm:Key>mode</ovm:Key></ovm:GetState>`),
	})
	if err != nil {
		t.Fatalf("GetState Call() error = %v", err)
	}
	if !strings.Contains(string(getResponse.BodyXML), "night") {
		t.Errorf("get BodyXML = %s, want night", getResponse.BodyXML)
	}

	if err := device.SetFault(fakedevice.ActionGetState, fakedevice.FaultSpec{Kind: fakedevice.FaultInvalidArgVal, Reason: "synthetic invalid key"}); err != nil {
		t.Fatalf("SetFault() error = %v", err)
	}
	_, err = client.Call(context.Background(), server.URL, soap.Request{Action: fakedevice.ActionGetState, BodyXML: []byte(`<ovm:GetState xmlns:ovm="urn:ovm:synthetic"><ovm:Key>mode</ovm:Key></ovm:GetState>`)})
	if !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("fault Call() error = %v, want ErrInvalidArgVal", err)
	}
	device.ClearFault(fakedevice.ActionGetState)
	device.SetUnsupported(fakedevice.ActionGetState, true)
	_, err = client.Call(context.Background(), server.URL, soap.Request{Action: fakedevice.ActionGetState, BodyXML: []byte(`<ovm:GetState xmlns:ovm="urn:ovm:synthetic"><ovm:Key>mode</ovm:Key></ovm:GetState>`)})
	if !errors.Is(err, soap.ErrActionNotSupported) {
		t.Fatalf("unsupported Call() error = %v, want ErrActionNotSupported", err)
	}
}

func TestDeviceRequestBodyLimitAndConcurrentState(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{MaxRequestBytes: 128})
	server := httptest.NewServer(device)
	defer server.Close()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, strings.NewReader(strings.Repeat("x", 256)))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/soap+xml")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatalf("close response: %v", closeErr)
	}
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", response.StatusCode)
	}

	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func(value string) {
			device.SetState("key", value)
			_, _ = device.State("key")
			done <- struct{}{}
		}(string(rune('a' + i)))
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}

func newDevice(t *testing.T, config fakedevice.Config) *fakedevice.Device {
	t.Helper()
	device, err := fakedevice.New(config)
	if err != nil {
		t.Fatalf("fakedevice.New() error = %v", err)
	}
	return device
}

func newSOAPClient(t *testing.T, httpClient *http.Client) *soap.Client {
	t.Helper()
	client, err := soap.NewClient(soap.Config{HTTPClient: httpClient})
	if err != nil {
		t.Fatalf("soap.NewClient() error = %v", err)
	}
	return client
}
