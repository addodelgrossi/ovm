package soap

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestTraceBodiesAreRedactedAndOptIn(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = io.WriteString(w, `<env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Body><x:R xmlns:x="urn:x"><x:Password>device-secret</x:Password><x:Name>cam</x:Name></x:R></env:Body></env:Envelope>`)
	}))
	defer server.Close()
	header := []byte(`<wsse:Security xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"><wsse:UsernameToken><wsse:Username>admin</wsse:Username><wsse:Password>digest-secret</wsse:Password><wsse:Nonce>nonce-secret</wsse:Nonce></wsse:UsernameToken></wsse:Security>`)

	for _, bodies := range []bool{false, true} {
		var mu sync.Mutex
		var events []TraceEvent
		client, err := NewClient(Config{TraceBodies: bodies, Trace: func(_ context.Context, event TraceEvent) {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
		}})
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		if _, err := client.Call(context.Background(), server.URL, Request{Action: "urn:x/Get", HeaderXML: header, BodyXML: []byte(`<x:Get xmlns:x="urn:x"/>`)}); err != nil {
			t.Fatalf("Call() error = %v", err)
		}
		event := events[0]
		if !bodies {
			if event.RequestXML != nil || event.ResponseXML != nil {
				t.Fatal("bodies were traced without TraceBodies")
			}
			continue
		}
		all := string(event.RequestXML) + string(event.ResponseXML)
		for _, secret := range []string{"admin", "digest-secret", "nonce-secret", "device-secret"} {
			if strings.Contains(all, secret) {
				t.Fatalf("trace leaked %q:\n%s", secret, all)
			}
		}
		if !strings.Contains(string(event.ResponseXML), "<x:Name>cam</x:Name>") || !strings.Contains(string(event.RequestXML), "urn:x") {
			t.Fatalf("trace lost non-secret content:\n%s", all)
		}
	}
}

func TestTraceBodyTruncatesOnARuneBoundary(t *testing.T) {
	t.Parallel()
	large := []byte(strings.Repeat("é", MaxTraceBodyBytes))
	body := traceBody(large)
	if len(body) > MaxTraceBodyBytes+len("…") || !strings.HasSuffix(string(body), "…") {
		t.Fatalf("traced %d bytes", len(body))
	}
}
