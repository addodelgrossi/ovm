package soap

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testAction = "http://www.onvif.org/ver10/device/wsdl/GetDeviceInformation"

// contentTypeSOAP12 and contentTypeSOAP11 are the media types a conformant peer
// returns for each SOAP version. Fixtures set them explicitly because the
// client now validates the response media type against the attempted version.
const (
	contentTypeSOAP12 = "application/soap+xml; charset=utf-8"
	contentTypeSOAP11 = "text/xml; charset=utf-8"
)

func TestClientUsesSOAP12AndAction(t *testing.T) {
	t.Parallel()

	var contentType, soapAction, requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		soapAction = r.Header.Get("SOAPAction")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		requestBody = string(body)
		w.Header().Set("Content-Type", contentTypeSOAP12)
		_, _ = io.WriteString(w, soap12Response(`<tds:GetDeviceInformationResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`))
	}))
	defer server.Close()

	client := newTestClient(t, Config{})
	response, err := client.Call(context.Background(), server.URL, Request{
		Action:  testAction,
		BodyXML: []byte(`<tds:GetDeviceInformation xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if response.Version != Version12 {
		t.Fatalf("Version = %q, want %q", response.Version, Version12)
	}
	if !strings.HasPrefix(contentType, `application/soap+xml`) || !strings.Contains(contentType, `action="`+testAction+`"`) {
		t.Errorf("Content-Type = %q, want SOAP 1.2 action parameter", contentType)
	}
	if soapAction != "" {
		t.Errorf("SOAPAction = %q, want empty for SOAP 1.2", soapAction)
	}
	if !strings.Contains(requestBody, string(Version12.EnvelopeNamespace())) || !strings.Contains(requestBody, "GetDeviceInformation") {
		t.Errorf("request body does not contain SOAP 1.2 envelope and operation: %s", requestBody)
	}
}

func TestClientFallsBackToSOAP11OnlyWithProtocolEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		firstReply func(http.ResponseWriter)
	}{
		{
			name: "HTTP 415",
			firstReply: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusUnsupportedMediaType)
			},
		},
		{
			name: "VersionMismatch fault",
			firstReply: func(w http.ResponseWriter) {
				w.Header().Set("Content-Type", contentTypeSOAP12)
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, soap12Fault("env:VersionMismatch", "", "Unsupported SOAP version", ""))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			var contentTypes, soapActions []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				contentTypes = append(contentTypes, r.Header.Get("Content-Type"))
				soapActions = append(soapActions, r.Header.Get("SOAPAction"))
				attempt := len(contentTypes)
				mu.Unlock()
				if attempt == 1 {
					tt.firstReply(w)
					return
				}
				w.Header().Set("Content-Type", contentTypeSOAP11)
				_, _ = io.WriteString(w, soap11Response(`<ok:Result xmlns:ok="urn:ovm:test">ready</ok:Result>`))
			}))
			defer server.Close()

			client := newTestClient(t, Config{})
			response, err := client.Call(context.Background(), server.URL, Request{Action: testAction, BodyXML: []byte(`<Get/>`)})
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if response.Version != Version11 {
				t.Fatalf("Version = %q, want SOAP 1.1", response.Version)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(contentTypes) != 2 {
				t.Fatalf("request count = %d, want 2", len(contentTypes))
			}
			if !strings.HasPrefix(contentTypes[0], "application/soap+xml") || !strings.HasPrefix(contentTypes[1], "text/xml") {
				t.Errorf("Content-Types = %q, want SOAP 1.2 then 1.1", contentTypes)
			}
			if soapActions[1] != `"`+testAction+`"` {
				t.Errorf("SOAPAction = %q, want quoted action", soapActions[1])
			}
		})
	}
}

func TestClientDoesNotFallbackWithoutProtocolEvidence(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "synthetic failure")
	}))
	defer server.Close()

	client := newTestClient(t, Config{})
	_, err := client.Call(context.Background(), server.URL, Request{Action: testAction, BodyXML: []byte(`<Get/>`)})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("Call() error = %v, want HTTPError 500", err)
	}
	if requests != 1 {
		t.Fatalf("request count = %d, want 1", requests)
	}
}

func TestClientParsesTypedFaultsAndRedactsRawXML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		code    string
		subcode string
		want    error
	}{
		{name: "not authorized", code: "env:Sender", subcode: "ter:NotAuthorized", want: ErrNotAuthorized},
		{name: "action unsupported", code: "env:Sender", subcode: "ter:ActionNotSupported", want: ErrActionNotSupported},
		{name: "invalid argument", code: "env:Sender", subcode: "ter:InvalidArgVal", want: ErrInvalidArgVal},
		{name: "version mismatch", code: "env:VersionMismatch", want: ErrVersionMismatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", contentTypeSOAP12)
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, soap12Fault(tt.code, tt.subcode, "synthetic fault", `<Password>camera-secret</Password>`))
			}))
			defer server.Close()

			client := newTestClient(t, Config{})
			_, err := client.callVersion(context.Background(), server.URL, Request{Action: testAction, BodyXML: []byte(`<Get/>`)}, Version12)
			if !errors.Is(err, tt.want) {
				t.Fatalf("callVersion() error = %v, want errors.Is(%v)", err, tt.want)
			}
			var fault *Fault
			if !errors.As(err, &fault) {
				t.Fatalf("error type = %T, want *Fault", err)
			}
			if strings.Contains(string(fault.RawXML), "camera-secret") || !strings.Contains(string(fault.RawXML), RedactedValue) {
				t.Errorf("fault RawXML was not safely redacted: %s", fault.RawXML)
			}
		})
	}
}

func TestClientAcceptsNamespacePrefixVariation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentTypeSOAP12)
		_, _ = io.WriteString(w, `<?xml version="1.0"?><soapxyz:Envelope xmlns:soapxyz="http://www.w3.org/2003/05/soap-envelope"><soapxyz:Body><v:GetResponse xmlns:v="urn:ovm:test"><v:Value>42</v:Value></v:GetResponse></soapxyz:Body></soapxyz:Envelope>`)
	}))
	defer server.Close()

	client := newTestClient(t, Config{})
	response, err := client.Call(context.Background(), server.URL, Request{Action: "urn:ovm:test:Get", BodyXML: []byte(`<Get/>`)})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if !strings.Contains(string(response.BodyXML), "GetResponse") || !strings.Contains(string(response.BodyXML), "42") {
		t.Errorf("BodyXML = %s, want namespace-varied payload", response.BodyXML)
	}
}

func TestClientRejectsDTDAndBoundsResponses(t *testing.T) {
	t.Parallel()

	t.Run("DTD", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", contentTypeSOAP12)
			_, _ = io.WriteString(w, `<!DOCTYPE Envelope [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><Envelope xmlns="http://www.w3.org/2003/05/soap-envelope"><Body>&xxe;</Body></Envelope>`)
		}))
		defer server.Close()

		client := newTestClient(t, Config{})
		_, err := client.Call(context.Background(), server.URL, Request{BodyXML: []byte(`<Get/>`)})
		if !errors.Is(err, ErrDTDNotAllowed) {
			t.Fatalf("Call() error = %v, want ErrDTDNotAllowed", err)
		}
	})

	t.Run("oversize", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", contentTypeSOAP12)
			_, _ = io.WriteString(w, soap12Response(`<Result><Password>must-not-leak</Password>`+strings.Repeat("x", 1024)+`</Result>`))
		}))
		defer server.Close()

		client := newTestClient(t, Config{MaxResponseBytes: 256})
		_, err := client.Call(context.Background(), server.URL, Request{BodyXML: []byte(`<Get/>`)})
		var limitErr *ResponseTooLargeError
		if !errors.As(err, &limitErr) {
			t.Fatalf("Call() error = %v, want *ResponseTooLargeError", err)
		}
		if len(limitErr.RawXML) > 256 {
			t.Errorf("RawXML length = %d, want <= 256", len(limitErr.RawXML))
		}
		if strings.Contains(string(limitErr.RawXML), "must-not-leak") {
			t.Errorf("RawXML leaked a password: %s", limitErr.RawXML)
		}
	})
}

func TestClientHonorsContextAndDefaultTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
			w.Header().Set("Content-Type", contentTypeSOAP12)
			_, _ = io.WriteString(w, soap12Response(`<Result/>`))
		}
	}))
	defer server.Close()

	t.Run("caller cancellation", func(t *testing.T) {
		client := newTestClient(t, Config{Timeout: time.Second})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := client.Call(ctx, server.URL, Request{BodyXML: []byte(`<Get/>`)})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Call() error = %v, want context.Canceled", err)
		}
	})

	t.Run("default timeout", func(t *testing.T) {
		client := newTestClient(t, Config{Timeout: 20 * time.Millisecond})
		_, err := client.Call(context.Background(), server.URL, Request{BodyXML: []byte(`<Get/>`)})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Call() error = %v, want context.DeadlineExceeded", err)
		}
	})
}

func TestClientBlocksCrossOriginRedirects(t *testing.T) {
	t.Parallel()

	received := make(chan *http.Request, 1)
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		received <- r
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	client := newTestClient(t, Config{})
	_, err := client.Call(context.Background(), redirector.URL, Request{
		BodyXML: []byte(`<Get/>`),
		HTTPHeaders: http.Header{
			"Authorization":       []string{"Digest credential-equivalent-material"},
			"Proxy-Authorization": []string{"Basic credential-equivalent-material"},
		},
	})
	if !errors.Is(err, ErrCrossOriginRedirect) {
		t.Fatalf("Call() error = %v, want ErrCrossOriginRedirect", err)
	}
	select {
	case request := <-received:
		t.Fatalf("cross-origin target unexpectedly received request with headers %v", RedactHeaders(request.Header))
	case <-time.After(50 * time.Millisecond):
	}
}

func TestClientEmitsSafeStructuredTrace(t *testing.T) {
	t.Parallel()

	var events []TraceEvent
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentTypeSOAP12)
		_, _ = io.WriteString(w, soap12Response(`<Result/>`))
	}))
	defer server.Close()

	client := newTestClient(t, Config{Trace: func(_ context.Context, event TraceEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	}})
	//nolint:gosec // Synthetic credential-shaped URL verifies mandatory redaction.
	action := "https://operator:camera-secret@example.invalid/Get"
	_, err := client.Call(context.Background(), server.URL, Request{Action: action, BodyXML: []byte(`<Password>camera-secret</Password>`)})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 {
		t.Fatalf("trace event count = %d, want 1", len(events))
	}
	event := events[0]
	if event.Method != http.MethodPost || event.StatusCode != http.StatusOK || event.Version != Version12 || event.Duration < 0 {
		t.Errorf("unexpected trace event: %+v", event)
	}
	if strings.Contains(event.Action, "operator") || strings.Contains(event.Action, "camera-secret") || !strings.Contains(event.Action, RedactedValue) {
		t.Errorf("trace action was not redacted: %q", event.Action)
	}
}

func newTestClient(t *testing.T, config Config) *Client {
	t.Helper()
	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func soap12Response(body string) string {
	return `<?xml version="1.0"?><env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Body>` + body + `</env:Body></env:Envelope>`
}

func soap11Response(body string) string {
	return `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>` + body + `</s:Body></s:Envelope>`
}

func soap12Fault(code, subcode, reason, detail string) string {
	subcodeXML := ""
	if subcode != "" {
		subcodeXML = `<env:Subcode><env:Value>` + subcode + `</env:Value></env:Subcode>`
	}
	return `<?xml version="1.0"?><env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope" xmlns:ter="http://www.onvif.org/ver10/error"><env:Body><env:Fault><env:Code><env:Value>` + code + `</env:Value>` + subcodeXML + `</env:Code><env:Reason><env:Text xml:lang="en">` + reason + `</env:Text></env:Reason><env:Detail>` + detail + `</env:Detail></env:Fault></env:Body></env:Envelope>`
}
