package media_test

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// Namespace declaration fragments used throughout the Media tests. trt binds
// the ver10 Media WSDL namespace and tt binds the shared ONVIF schema
// namespace, matching the prefixes the fixtures and matrices use. The decoder
// is prefix-tolerant, so the specific prefixes are a test convenience only.
const (
	trtNS = ` xmlns:trt="http://www.onvif.org/ver10/media/wsdl"`
	ttNS  = ` xmlns:tt="http://www.onvif.org/ver10/schema"`
)

// capture records the single HTTP request a client makes so a test can assert
// the SOAP action and the request body QNames. For SOAP 1.2 the action travels
// as the "action" parameter of the application/soap+xml Content-Type; for SOAP
// 1.1 it is the quoted SOAPAction header. action normalizes both.
type capture struct {
	action      string
	contentType string
	body        string
}

func actionFromRequest(r *http.Request) string {
	contentType := r.Header.Get("Content-Type")
	if _, params, err := mime.ParseMediaType(contentType); err == nil {
		if action := params["action"]; action != "" {
			return action
		}
	}
	if soapAction := r.Header.Get("SOAPAction"); soapAction != "" {
		return strings.Trim(soapAction, `"`)
	}
	return ""
}

// newRawClient serves a fixed response body over the real SOAP transport and
// records the request into capture. The returned client therefore exercises the
// genuine soap.Client request builder and response parser, not a stub.
func newRawClient(t *testing.T, contentType string, status int, body string) (*media.Client, *capture, func()) {
	t.Helper()
	rec := &capture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		rec.body = string(raw)
		rec.action = actionFromRequest(r)
		rec.contentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	soapClient, err := soap.NewClient(soap.Config{})
	if err != nil {
		server.Close()
		t.Fatalf("soap.NewClient() error = %v", err)
	}
	client, err := media.NewClient(soapClient, server.URL)
	if err != nil {
		server.Close()
		t.Fatalf("media.NewClient() error = %v", err)
	}
	return client, rec, server.Close
}

// newSOAP11RawClient answers the initial SOAP 1.2 attempt with HTTP 415 and the
// SOAP 1.1 retry with the given text/xml SOAP 1.1 envelope. This is the only
// coherent way to exercise the version fallback: a text/xml body served to the
// initial 1.2 attempt is not valid fallback evidence.
func newSOAP11RawClient(t *testing.T, body string) (*media.Client, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/soap+xml") {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	soapClient, err := soap.NewClient(soap.Config{})
	if err != nil {
		server.Close()
		t.Fatalf("soap.NewClient() error = %v", err)
	}
	client, err := media.NewClient(soapClient, server.URL)
	if err != nil {
		server.Close()
		t.Fatalf("media.NewClient() error = %v", err)
	}
	return client, server.Close
}

func soap12Envelope(payload string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Body>` + payload + `</env:Body></env:Envelope>`
}

func soap11Envelope(payload string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body>` + payload + `</soap:Body></soap:Envelope>`
}

func soap12Fault(code, subcode, reason string) string {
	return `<env:Fault xmlns:env="http://www.w3.org/2003/05/soap-envelope" xmlns:ter="http://www.onvif.org/ver10/error">` +
		`<env:Code><env:Value>` + code + `</env:Value><env:Subcode><env:Value>` + subcode + `</env:Value></env:Subcode></env:Code>` +
		`<env:Reason><env:Text xml:lang="en">` + reason + `</env:Text></env:Reason></env:Fault>`
}

// serveOK is a convenience for the common "SOAP 1.2 success" transport setup.
func serveOK(t *testing.T, payload string) (*media.Client, *capture, func()) {
	t.Helper()
	return newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK, soap12Envelope(payload))
}

// invoke runs an operation against a success response and returns the recorded
// request plus the operation error, centralizing transport wiring.
func invoke(t *testing.T, payload string, call func(*media.Client) error) (*capture, error) {
	t.Helper()
	client, rec, closeFn := serveOK(t, payload)
	defer closeFn()
	return rec, call(client)
}

var _ = context.Background

const validMedia1StreamingTail = `<tt:Multicast xmlns:tt="http://www.onvif.org/ver10/schema"><tt:Address><tt:Type>IPv4</tt:Type><tt:IPv4Address>0.0.0.0</tt:IPv4Address></tt:Address><tt:Port>0</tt:Port><tt:TTL>1</tt:TTL><tt:AutoStart>false</tt:AutoStart></tt:Multicast><tt:SessionTimeout xmlns:tt="http://www.onvif.org/ver10/schema">PT60S</tt:SessionTimeout>`
