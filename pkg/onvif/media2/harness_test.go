package media2_test

import (
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// Namespace declaration fragments for the Media2 tests. tr2 binds the ver20
// Media2 WSDL namespace and tt binds the shared ONVIF schema namespace. The
// decoder matches on namespace URI only, so these prefixes are a convenience.
const (
	tr2NS = ` xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"`
	ttNS  = ` xmlns:tt="http://www.onvif.org/ver10/schema"`
)

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

func newRawClient(t *testing.T, contentType string, status int, body string) (*media2.Client, *capture, func()) {
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
	client, err := media2.NewClient(soapClient, server.URL)
	if err != nil {
		server.Close()
		t.Fatalf("media2.NewClient() error = %v", err)
	}
	return client, rec, server.Close
}

func newSOAP11RawClient(t *testing.T, body string) (*media2.Client, func()) {
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
	client, err := media2.NewClient(soapClient, server.URL)
	if err != nil {
		server.Close()
		t.Fatalf("media2.NewClient() error = %v", err)
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

func serveOK(t *testing.T, payload string) (*media2.Client, *capture, func()) {
	t.Helper()
	return newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK, soap12Envelope(payload))
}

func invoke(t *testing.T, payload string, call func(*media2.Client) error) (*capture, error) {
	t.Helper()
	client, rec, closeFn := serveOK(t, payload)
	defer closeFn()
	return rec, call(client)
}
