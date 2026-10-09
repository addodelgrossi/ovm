package soap

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClientRejectsNamespaceSpoofedVersionMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{
			name: "foreign Fault element",
			body: `<foreign:Fault xmlns:foreign="urn:foreign"><foreign:Code><foreign:Value>foreign:VersionMismatch</foreign:Value></foreign:Code><foreign:Reason><foreign:Text>not SOAP</foreign:Text></foreign:Reason></foreign:Fault>`,
		},
		{
			name: "foreign fault code QName",
			body: `<env:Fault xmlns:foreign="urn:foreign"><env:Code><env:Value>foreign:VersionMismatch</env:Value></env:Code><env:Reason><env:Text xml:lang="en">not a SOAP code</env:Text></env:Reason></env:Fault>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests++
				w.Header().Set("Content-Type", contentTypeSOAP12)
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, soap12Response(tt.body))
			}))
			defer server.Close()

			client := newTestClient(t, Config{})
			_, err := client.Call(context.Background(), server.URL, Request{BodyXML: []byte(`<Get/>`)})
			if errors.Is(err, ErrVersionMismatch) {
				t.Fatalf("Call() accepted namespace-spoofed VersionMismatch: %v", err)
			}
			if requests != 1 {
				t.Fatalf("request count = %d, want no SOAP 1.1 fallback", requests)
			}
		})
	}
}

func TestResponseBodyXMLRetainsInheritedNamespaces(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentTypeSOAP12)
		_, _ = io.WriteString(w, `<?xml version="1.0"?><env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><env:Body><tds:Reply><tds:Value>ok</tds:Value></tds:Reply></env:Body></env:Envelope>`)
	}))
	defer server.Close()

	response, err := newTestClient(t, Config{}).Call(context.Background(), server.URL, Request{BodyXML: []byte(`<Get/>`)})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	var payload struct{ XMLName xml.Name }
	if err := xml.Unmarshal(response.BodyXML, &payload); err != nil {
		t.Fatalf("BodyXML is not standalone XML: %v; %s", err, response.BodyXML)
	}
	if payload.XMLName.Space != "http://www.onvif.org/ver10/device/wsdl" || payload.XMLName.Local != "Reply" {
		t.Fatalf("payload name = {%s}%s", payload.XMLName.Space, payload.XMLName.Local)
	}
}

func TestClientTimeoutCoversVersionFallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/soap+xml") {
			time.Sleep(70 * time.Millisecond)
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer server.Close()

	client := newTestClient(t, Config{Timeout: 100 * time.Millisecond})
	started := time.Now()
	_, err := client.Call(context.Background(), server.URL, Request{BodyXML: []byte(`<Get/>`)})
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Call() error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 140*time.Millisecond {
		t.Fatalf("Call() elapsed = %s, want one aggregate 100ms budget", elapsed)
	}
}

func TestClientTreatsHTTP415AsEvidenceRegardlessOfBody(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			_, _ = io.WriteString(w, strings.Repeat("x", 1024))
			return
		}
		w.Header().Set("Content-Type", contentTypeSOAP11)
		_, _ = io.WriteString(w, soap11Response(`<Result/>`))
	}))
	defer server.Close()

	client := newTestClient(t, Config{MaxResponseBytes: 128})
	response, err := client.Call(context.Background(), server.URL, Request{BodyXML: []byte(`<Get/>`)})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if response.Version != Version11 || requests != 2 {
		t.Fatalf("Version = %s, requests = %d; want SOAP 1.1 after 415", response.Version, requests)
	}
}

func TestRedirectPolicyRechecksCustomCallbackMutation(t *testing.T) {
	t.Parallel()

	received := make(chan struct{}, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received <- struct{}{}
		w.Header().Set("Content-Type", contentTypeSOAP12)
		_, _ = io.WriteString(w, soap12Response(`<Result/>`))
	}))
	defer target.Close()
	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatalf("parse target URL: %v", err)
	}

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/same-origin", http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Content-Type", contentTypeSOAP12)
		_, _ = io.WriteString(w, soap12Response(`<Result/>`))
	}))
	defer redirector.Close()

	httpClient := &http.Client{CheckRedirect: func(request *http.Request, _ []*http.Request) error {
		request.URL = targetURL
		return nil
	}}
	client := newTestClient(t, Config{HTTPClient: httpClient})
	_, err = client.Call(context.Background(), redirector.URL+"/start", Request{HeaderXML: []byte(`<Password>must-not-cross</Password>`), BodyXML: []byte(`<Get/>`)})
	if !errors.Is(err, ErrCrossOriginRedirect) {
		t.Fatalf("Call() error = %v, want ErrCrossOriginRedirect", err)
	}
	select {
	case <-received:
		t.Fatal("custom redirect callback bypassed cross-origin policy")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRedirectPolicyRejectsSOAPMethodRewrite(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/target", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", contentTypeSOAP12)
		_, _ = io.WriteString(w, soap12Response(`<Result/>`))
	}))
	defer server.Close()

	_, err := newTestClient(t, Config{}).Call(context.Background(), server.URL+"/start", Request{BodyXML: []byte(`<Get/>`)})
	if !errors.Is(err, ErrUnsafeRedirect) {
		t.Fatalf("Call() error = %v, want ErrUnsafeRedirect", err)
	}
}

func TestSOAP11EmptyActionUsesEmptySOAPActionHeader(t *testing.T) {
	t.Parallel()

	var present bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, present = r.Header["Soapaction"]
		w.Header().Set("Content-Type", contentTypeSOAP11)
		_, _ = io.WriteString(w, soap11Response(`<Result/>`))
	}))
	defer server.Close()

	client := newTestClient(t, Config{})
	if _, err := client.callVersion(context.Background(), server.URL, Request{BodyXML: []byte(`<Get/>`)}, Version11); err != nil {
		t.Fatalf("callVersion() error = %v", err)
	}
	if !present {
		t.Fatal("SOAP 1.1 request omitted SOAPAction header")
	}
}

func TestRedactionHandlesUnicodePrefixesAttributesAndQuerySecrets(t *testing.T) {
	t.Parallel()

	input := `<π:Password xmlns:π="urn:test" value="attribute-secret">element-secret</π:Password> https://example.invalid/action?access_token=query-secret`
	redacted := RedactText(input)
	for _, secret := range []string{"attribute-secret", "element-secret", "query-secret"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("RedactText() retained %q in %s", secret, redacted)
		}
	}
}

func TestClientIsSafeForConcurrentCalls(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentTypeSOAP12)
		_, _ = io.WriteString(w, soap12Response(`<Result/>`))
	}))
	defer server.Close()
	client := newTestClient(t, Config{})

	var workers sync.WaitGroup
	for range 20 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if _, err := client.Call(context.Background(), server.URL, Request{BodyXML: []byte(`<Get/>`)}); err != nil {
				t.Errorf("Call() error = %v", err)
			}
		}()
	}
	workers.Wait()
}

func TestSOAP11FaultsRetainTypedClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
		want error
	}{
		{name: "NotAuthorized", code: "ter:NotAuthorized", want: ErrNotAuthorized},
		{name: "ActionNotSupported", code: "ter:ActionNotSupported", want: ErrActionNotSupported},
		{name: "InvalidArgVal", code: "ter:InvalidArgVal", want: ErrInvalidArgVal},
		{name: "VersionMismatch", code: "s:VersionMismatch", want: ErrVersionMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", contentTypeSOAP11)
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, soap11Response(`<s:Fault xmlns:ter="http://www.onvif.org/ver10/error"><faultcode>`+tt.code+`</faultcode><faultstring>Synthetic fault</faultstring><detail/></s:Fault>`))
			}))
			defer server.Close()
			client := newTestClient(t, Config{})
			_, err := client.callVersion(context.Background(), server.URL, Request{BodyXML: []byte(`<Get/>`)}, Version11)
			if !errors.Is(err, tt.want) {
				t.Fatalf("callVersion() error = %v, want %v", err, tt.want)
			}
		})
	}
}
