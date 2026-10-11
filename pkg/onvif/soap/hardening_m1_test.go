package soap

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// --- Requirement 1: fail-closed redaction and sanitized parser errors. ---

// TestRedactFailsClosedAtLexicalBoundaries verifies that a sensitive value is
// never retained when a quote, start tag, end tag, or terminated token is
// truncated at a lexical boundary.
func TestRedactFailsClosedAtLexicalBoundaries(t *testing.T) {
	t.Parallel()

	const secret = "boundary-secret-value"
	cases := []struct {
		name  string
		input string
	}{
		{"double-quoted attribute without closing quote", `<e password="` + secret},
		{"double-quoted attribute containing opposite quote", `<e password="prefix'` + secret},
		{"single-quoted attribute without closing quote", `<e nonce='` + secret},
		{"single-quoted attribute containing opposite quote", `<e nonce='prefix"` + secret},
		{"unquoted malformed attribute", `<e password=` + secret},
		{"missing equals after sensitive attribute", `<e password "` + secret + `">`},
		{"sensitive start tag never closes", `<ns:Password attr="` + secret},
		{"unicode sensitive start tag never closes", `<π:Password attr="` + secret},
		{"element text without end tag", `<Password>` + secret},
		{"qualified element text without end tag", `<wsse:PasswordDigest>` + secret},
		{"usernametoken without end tag", `<wsse:UsernameToken><wsse:Username>` + secret},
		{"authorization header without newline", "Authorization: Digest " + secret},
		{"proxy-authorization header without newline", "Proxy-Authorization: Basic " + secret},
		{"url userinfo terminated", "https://user:" + secret + "@host.invalid/x"},
		{"url query terminated", "https://host.invalid/x?token=" + secret},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := RedactText(tc.input)
			if strings.Contains(out, secret) {
				t.Fatalf("RedactText(%q) retained the secret: %q", tc.input, out)
			}
			if !utf8.ValidString(out) {
				t.Fatalf("RedactText(%q) produced invalid UTF-8: %q", tc.input, out)
			}
		})
	}
}

// TestRedactHandlesMalformedUnicode verifies output is always valid UTF-8 and
// never retains a secret even when the input carries incomplete runes.
func TestRedactHandlesMalformedUnicode(t *testing.T) {
	t.Parallel()

	const secret = "unicode-boundary-secret"
	inputs := [][]byte{
		append([]byte(`<Password>`+secret), 0xff, 0xfe),
		append([]byte("Authorization: Digest "+secret), 0xc3),
		{0xff, 0xfe, '<', 'P', 'a', 's', 's', 'w', 'o', 'r', 'd', '>'},
	}
	for i, raw := range inputs {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			out := RedactText(string(raw))
			if strings.Contains(out, secret) {
				t.Fatalf("RedactText retained secret: %q", out)
			}
			sanitized := sanitizeDiagnostic(string(raw))
			if !utf8.ValidString(sanitized) {
				t.Fatalf("sanitizeDiagnostic produced invalid UTF-8: %q", sanitized)
			}
			if strings.Contains(sanitized, secret) {
				t.Fatalf("sanitizeDiagnostic retained secret: %q", sanitized)
			}
		})
	}
}

// TestSanitizeDiagnosticIsBounded verifies diagnostics are clipped to a safe
// bound on a valid UTF-8 boundary.
func TestSanitizeDiagnosticIsBounded(t *testing.T) {
	t.Parallel()

	// A long run of multibyte runes stresses the clip-to-boundary logic.
	long := strings.Repeat("\u00e9", 4096)
	out := sanitizeDiagnostic(long)
	if len(out) > maxDiagnosticBytes {
		t.Fatalf("sanitizeDiagnostic length = %d, want <= %d", len(out), maxDiagnosticBytes)
	}
	if !utf8.ValidString(out) {
		t.Fatalf("sanitizeDiagnostic produced invalid UTF-8")
	}
}

// TestParseErrorSanitizesCauseAndPreservesSentinels verifies the parser error
// surfaced through Error, Err, and Unwrap never exposes raw XML markers while
// errors.Is still matches the stable sentinels.
func TestParseErrorSanitizesCauseAndPreservesSentinels(t *testing.T) {
	t.Parallel()

	const marker = "camera-secret"

	t.Run("marker-bearing cause is redacted", func(t *testing.T) {
		t.Parallel()
		cause := errors.New(`XML syntax error on <Password>` + marker + `</Password>`)
		pe := newParseError(cause, http.StatusOK, nil)
		if strings.Contains(pe.Error(), marker) {
			t.Fatalf("ParseError.Error() leaked marker: %q", pe.Error())
		}
		if pe.Unwrap() != nil && strings.Contains(pe.Unwrap().Error(), marker) {
			t.Fatalf("ParseError.Unwrap() leaked marker: %q", pe.Unwrap())
		}
		if pe.Err != nil && strings.Contains(pe.Err.Error(), marker) {
			t.Fatalf("ParseError.Err leaked marker: %q", pe.Err)
		}
		if !utf8.ValidString(pe.Error()) {
			t.Fatalf("ParseError.Error() is not valid UTF-8")
		}
	})

	t.Run("DTD sentinel preserved", func(t *testing.T) {
		t.Parallel()
		pe := newParseError(ErrDTDNotAllowed, http.StatusOK, nil)
		if !errors.Is(pe, ErrDTDNotAllowed) {
			t.Fatalf("errors.Is(ParseError, ErrDTDNotAllowed) = false")
		}
	})

	t.Run("complexity sentinel preserved", func(t *testing.T) {
		t.Parallel()
		wrapped := errors.New("too deep: " + marker)
		cause := errors.Join(ErrXMLComplexity, wrapped)
		pe := newParseError(cause, http.StatusOK, nil)
		if !errors.Is(pe, ErrXMLComplexity) {
			t.Fatalf("errors.Is(ParseError, ErrXMLComplexity) = false")
		}
		if strings.Contains(pe.Error(), marker) {
			t.Fatalf("ParseError.Error() leaked marker through complexity cause: %q", pe.Error())
		}
	})
}

// TestTypedErrorsNeverExposeMarkers verifies HTTPError, ResponseTooLargeError,
// and ParseError RawXML carry only redacted content from an end-to-end call.
func TestTypedErrorsNeverExposeMarkers(t *testing.T) {
	t.Parallel()

	const marker = "must-not-leak-secret"

	t.Run("HTTPError RawXML redacted", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `<Password>`+marker+`</Password>`)
		}))
		defer server.Close()
		_, err := newTestClient(t, Config{}).Call(context.Background(), server.URL, Request{BodyXML: []byte(`<Get/>`)})
		var he *HTTPError
		if !errors.As(err, &he) {
			t.Fatalf("error = %v, want *HTTPError", err)
		}
		if strings.Contains(string(he.RawXML), marker) {
			t.Fatalf("HTTPError.RawXML leaked marker: %s", he.RawXML)
		}
	})

	t.Run("ParseError RawXML redacted", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", contentTypeSOAP12)
			// Malformed envelope: broken XML carrying a secret.
			_, _ = io.WriteString(w, `<env:Envelope xmlns:env="`+envelopeNamespace12+`"><env:Body><Password>`+marker)
		}))
		defer server.Close()
		_, err := newTestClient(t, Config{}).Call(context.Background(), server.URL, Request{BodyXML: []byte(`<Get/>`)})
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Fatalf("error = %v, want *ParseError", err)
		}
		if strings.Contains(string(pe.RawXML), marker) || strings.Contains(pe.Error(), marker) {
			t.Fatalf("ParseError leaked marker: raw=%s err=%s", pe.RawXML, pe.Error())
		}
	})
}

// --- Requirement 2: media type and envelope-version validation. ---

// fixedResponseTransport returns a canned response with an exactly-controlled
// header set, so a test can simulate a genuinely missing Content-Type without
// the net/http server's content sniffing.
type fixedResponseTransport struct {
	contentType string // empty => header omitted entirely
	body        string
	status      int
}

func (f fixedResponseTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	header := make(http.Header)
	if f.contentType != "" {
		header.Set("Content-Type", f.contentType)
	}
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Request:    r,
	}, nil
}

func TestClientRejectsInvalidResponseMediaType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		version     Version
		contentType string // empty => header omitted
	}{
		{"soap12 missing", Version12, ""},
		{"soap12 text/xml cross-version", Version12, "text/xml; charset=utf-8"},
		{"soap12 malformed", Version12, "application/soap+xml;;;"},
		{"soap12 unrelated", Version12, "application/octet-stream"},
		{"soap11 missing", Version11, ""},
		{"soap11 soap+xml cross-version", Version11, "application/soap+xml; charset=utf-8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := soap12Response(`<Result/>`)
			if tc.version == Version11 {
				body = soap11Response(`<Result/>`)
			}
			client := newTestClient(t, Config{HTTPClient: &http.Client{
				Transport: fixedResponseTransport{contentType: tc.contentType, body: body},
			}})
			_, err := client.callVersion(context.Background(), "http://camera.invalid/onvif", Request{BodyXML: []byte(`<Get/>`)}, tc.version)
			if !errors.Is(err, ErrInvalidMediaType) {
				t.Fatalf("callVersion() error = %v, want ErrInvalidMediaType", err)
			}
			if isVersionFallbackEvidence(err) {
				t.Fatalf("invalid media type must not be fallback evidence")
			}
		})
	}
}

func TestClientRejectsCrossVersionEnvelope(t *testing.T) {
	t.Parallel()

	// Attempt SOAP 1.2 but reply with a SOAP 1.1 envelope and a soap+xml type.
	client := newTestClient(t, Config{HTTPClient: &http.Client{
		Transport: fixedResponseTransport{contentType: contentTypeSOAP12, body: soap11Response(`<Result/>`)},
	}})
	_, err := client.callVersion(context.Background(), "http://camera.invalid/onvif", Request{BodyXML: []byte(`<Get/>`)}, Version12)
	if !errors.Is(err, ErrEnvelopeVersionMismatch) {
		t.Fatalf("callVersion() error = %v, want ErrEnvelopeVersionMismatch", err)
	}
	if isVersionFallbackEvidence(err) {
		t.Fatalf("cross-version envelope must not be fallback evidence")
	}
}

func TestFallbackOnlyFromDeliberateEvidence(t *testing.T) {
	t.Parallel()

	// A SOAP 1.1 VersionMismatch fault is NOT evidence for a 1.1 retry: fallback
	// only follows a 1.2 VersionMismatch or an HTTP 415.
	soap11VM := &Fault{Version: Version11, Kind: FaultVersionMismatch}
	if isVersionFallbackEvidence(soap11VM) {
		t.Fatalf("SOAP 1.1 VersionMismatch must not drive fallback")
	}
	soap12VM := &Fault{Version: Version12, Kind: FaultVersionMismatch}
	if !isVersionFallbackEvidence(soap12VM) {
		t.Fatalf("SOAP 1.2 VersionMismatch should drive fallback")
	}
	if !isVersionFallbackEvidence(&HTTPError{StatusCode: http.StatusUnsupportedMediaType}) {
		t.Fatalf("HTTP 415 should drive fallback")
	}
	if isVersionFallbackEvidence(&HTTPError{StatusCode: http.StatusInternalServerError}) {
		t.Fatalf("HTTP 500 must not drive fallback")
	}
}

// --- Requirement 3: fault field cardinality and position. ---

func wrapSOAP12Fault(inner string) string {
	return `<?xml version="1.0"?><env:Envelope xmlns:env="` + envelopeNamespace12 +
		`" xmlns:ter="` + onvifErrorNamespace + `"><env:Body><env:Fault>` + inner +
		`</env:Fault></env:Body></env:Envelope>`
}

func TestSOAP12FaultCardinalityRejected(t *testing.T) {
	t.Parallel()

	validCodeReason := `<env:Code><env:Value>env:Sender</env:Value></env:Code><env:Reason><env:Text xml:lang="en">r</env:Text></env:Reason>`
	cases := []struct {
		name  string
		inner string
	}{
		{"duplicate Code", `<env:Code><env:Value>env:Sender</env:Value></env:Code><env:Code><env:Value>env:Sender</env:Value></env:Code><env:Reason><env:Text xml:lang="en">r</env:Text></env:Reason>`},
		{"missing Code Value", `<env:Code/><env:Reason><env:Text xml:lang="en">r</env:Text></env:Reason>`},
		{"duplicate Code Value", `<env:Code><env:Value>env:Sender</env:Value><env:Value>env:Receiver</env:Value></env:Code><env:Reason><env:Text xml:lang="en">r</env:Text></env:Reason>`},
		{"missing Reason", `<env:Code><env:Value>env:Sender</env:Value></env:Code>`},
		{"Reason without Text", `<env:Code><env:Value>env:Sender</env:Value></env:Code><env:Reason/>`},
		{"duplicate Subcode", `<env:Code><env:Value>env:Sender</env:Value><env:Subcode><env:Value>ter:NotAuthorized</env:Value></env:Subcode><env:Subcode><env:Value>ter:InvalidArgVal</env:Value></env:Subcode></env:Code><env:Reason><env:Text xml:lang="en">r</env:Text></env:Reason>`},
		{"Subcode without Value", `<env:Code><env:Value>env:Sender</env:Value><env:Subcode/></env:Code><env:Reason><env:Text xml:lang="en">r</env:Text></env:Reason>`},
		{"duplicate Node", validCodeReason + `<env:Node>a</env:Node><env:Node>b</env:Node>`},
		{"duplicate Role", validCodeReason + `<env:Role>a</env:Role><env:Role>b</env:Role>`},
		{"duplicate Detail", validCodeReason + `<env:Detail>a</env:Detail><env:Detail>b</env:Detail>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseEnvelope([]byte(wrapSOAP12Fault(tc.inner)))
			if err == nil {
				t.Fatalf("parseEnvelope accepted malformed fault %q", tc.name)
			}
		})
	}
}

func TestSOAP12FaultSingletonsAccepted(t *testing.T) {
	t.Parallel()

	inner := `<env:Code><env:Value>env:Sender</env:Value><env:Subcode><env:Value>ter:NotAuthorized</env:Value></env:Subcode></env:Code>` +
		`<env:Reason><env:Text xml:lang="en">one</env:Text><env:Text xml:lang="de">zwei</env:Text></env:Reason>` +
		`<env:Node>n</env:Node><env:Role>r</env:Role><env:Detail><ter:Err/></env:Detail>`
	parsed, err := parseEnvelope([]byte(wrapSOAP12Fault(inner)))
	if err != nil {
		t.Fatalf("parseEnvelope rejected a valid fault: %v", err)
	}
	if parsed.Fault == nil || parsed.Fault.Kind != FaultNotAuthorized {
		t.Fatalf("fault = %+v, want NotAuthorized from direct Subcode", parsed.Fault)
	}
}

func TestSOAP11FaultCardinalityRejected(t *testing.T) {
	t.Parallel()

	wrap := func(inner string) string {
		return `<?xml version="1.0"?><s:Envelope xmlns:s="` + envelopeNamespace11 +
			`" xmlns:ter="` + onvifErrorNamespace + `"><s:Body><s:Fault>` + inner +
			`</s:Fault></s:Body></s:Envelope>`
	}
	cases := []struct {
		name  string
		inner string
	}{
		{"duplicate faultcode", `<faultcode>s:Server</faultcode><faultcode>s:Client</faultcode><faultstring>x</faultstring>`},
		{"missing faultcode", `<faultstring>x</faultstring>`},
		{"duplicate faultstring", `<faultcode>s:Server</faultcode><faultstring>a</faultstring><faultstring>b</faultstring>`},
		{"duplicate faultactor", `<faultcode>s:Server</faultcode><faultstring>x</faultstring><faultactor>a</faultactor><faultactor>b</faultactor>`},
		{"duplicate detail", `<faultcode>s:Server</faultcode><faultstring>x</faultstring><detail>a</detail><detail>b</detail>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseEnvelope([]byte(wrap(tc.inner)))
			if err == nil {
				t.Fatalf("parseEnvelope accepted malformed SOAP 1.1 fault %q", tc.name)
			}
		})
	}
}

// TestSOAP11DetailDescendantNotClassified verifies an ONVIF code buried in the
// detail does not drive classification; only the direct faultcode QName does.
func TestSOAP11DetailDescendantNotClassified(t *testing.T) {
	t.Parallel()

	body := `<?xml version="1.0"?><s:Envelope xmlns:s="` + envelopeNamespace11 +
		`" xmlns:ter="` + onvifErrorNamespace + `"><s:Body><s:Fault>` +
		`<faultcode>s:Server</faultcode><faultstring>generic</faultstring>` +
		`<detail><ter:NotAuthorized/></detail></s:Fault></s:Body></s:Envelope>`
	parsed, err := parseEnvelope([]byte(body))
	if err != nil {
		t.Fatalf("parseEnvelope error = %v", err)
	}
	if parsed.Fault == nil {
		t.Fatalf("expected a fault")
	}
	if parsed.Fault.Kind == FaultNotAuthorized {
		t.Fatalf("descendant Detail element must not classify the fault")
	}
}

// --- Requirement 4: request-size enforcement. ---

func TestNewClientRejectsInvalidRequestLimit(t *testing.T) {
	t.Parallel()

	if _, err := NewClient(Config{MaxRequestBytes: -1}); err == nil {
		t.Fatalf("NewClient accepted a negative MaxRequestBytes")
	}
	c, err := NewClient(Config{})
	if err != nil {
		t.Fatalf("NewClient error = %v", err)
	}
	if c.maxRequestBytes != DefaultMaxRequestBytes {
		t.Fatalf("default MaxRequestBytes = %d, want %d", c.maxRequestBytes, DefaultMaxRequestBytes)
	}
	if DefaultMaxRequestBytes <= 0 {
		t.Fatalf("DefaultMaxRequestBytes must be positive")
	}
}

func TestRequestSizeLimitExactAndOver(t *testing.T) {
	t.Parallel()

	body := []byte(`<Get/>`)
	exact, ok := serializedEnvelopeSize(Version12, nil, body)
	if !ok {
		t.Fatalf("serializedEnvelopeSize overflow for a tiny body")
	}

	t.Run("exact limit succeeds", func(t *testing.T) {
		t.Parallel()
		var requests int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests++
			w.Header().Set("Content-Type", contentTypeSOAP12)
			_, _ = io.WriteString(w, soap12Response(`<Result/>`))
		}))
		defer server.Close()
		client := newTestClient(t, Config{MaxRequestBytes: exact})
		if _, err := client.callVersion(context.Background(), server.URL, Request{BodyXML: body}, Version12); err != nil {
			t.Fatalf("callVersion at exact limit error = %v", err)
		}
		if requests != 1 {
			t.Fatalf("requests = %d, want 1", requests)
		}
	})

	t.Run("over limit sends nothing", func(t *testing.T) {
		t.Parallel()
		var requests int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests++
			w.Header().Set("Content-Type", contentTypeSOAP12)
			_, _ = io.WriteString(w, soap12Response(`<Result/>`))
		}))
		defer server.Close()
		client := newTestClient(t, Config{MaxRequestBytes: exact - 1})
		_, err := client.callVersion(context.Background(), server.URL, Request{BodyXML: body}, Version12)
		var rte *RequestTooLargeError
		if !errors.As(err, &rte) {
			t.Fatalf("error = %v, want *RequestTooLargeError", err)
		}
		if rte.Version != Version12 || rte.Limit != exact-1 {
			t.Fatalf("RequestTooLargeError = %+v", rte)
		}
		if requests != 0 {
			t.Fatalf("requests = %d, want 0 (nothing sent)", requests)
		}
	})
}

func TestRequestSizeLimitCheckedPerVersion(t *testing.T) {
	t.Parallel()

	// The SOAP 1.1 envelope is longer than the SOAP 1.2 one for the same body
	// because of the longer namespace. Choose a limit that allows 1.2 but not
	// 1.1, and force a fallback via HTTP 415: the fallback attempt must be
	// rejected on size and send nothing.
	body := []byte(`<Get/>`)
	size12, _ := serializedEnvelopeSize(Version12, nil, body)
	size11, _ := serializedEnvelopeSize(Version11, nil, body)
	if size11 <= size12 {
		t.Skipf("SOAP 1.1 envelope not larger (%d vs %d)", size11, size12)
	}

	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusUnsupportedMediaType)
	}))
	defer server.Close()

	client := newTestClient(t, Config{MaxRequestBytes: size12})
	_, err := client.Call(context.Background(), server.URL, Request{BodyXML: body})
	var rte *RequestTooLargeError
	if !errors.As(err, &rte) || rte.Version != Version11 {
		t.Fatalf("error = %v, want SOAP 1.1 RequestTooLargeError", err)
	}
	// Exactly one request: the 1.2 attempt (415), then the 1.1 attempt rejected
	// before transport.
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (1.1 fallback rejected before transport)", attempts)
	}
}

func TestSerializedEnvelopeSizeOverflowSafe(t *testing.T) {
	t.Parallel()

	size, ok := serializedEnvelopeSize(Version12, nil, []byte{})
	if !ok || size <= 0 {
		t.Fatalf("serializedEnvelopeSize empty body = (%d,%v)", size, ok)
	}
	withHeader, ok := serializedEnvelopeSize(Version12, []byte(`<h/>`), []byte(`<b/>`))
	if !ok || withHeader <= size {
		t.Fatalf("header fragment did not increase size: %d vs %d", withHeader, size)
	}
}

// --- Requirement 5: parseXMLTree retained-text and attribute boundary tests. ---

func TestRetainedTextBoundary(t *testing.T) {
	t.Parallel()

	build := func(textLen int) []byte {
		return []byte(`<e:Envelope xmlns:e="` + envelopeNamespace12 + `"><e:Body><Op>` +
			strings.Repeat("x", textLen) + `</Op></e:Body></e:Envelope>`)
	}

	// Exactly at the cap: accepted.
	if _, err := parseXMLTree(build(maxEnvelopeRetainedTextBytes)); err != nil {
		t.Fatalf("retained text at cap rejected: %v", err)
	}
	// One byte over: rejected with the complexity sentinel.
	_, err := parseXMLTree(build(maxEnvelopeRetainedTextBytes + 1))
	if !errors.Is(err, ErrXMLComplexity) {
		t.Fatalf("retained text over cap error = %v, want ErrXMLComplexity", err)
	}
}

func TestAttributesPerElementBoundary(t *testing.T) {
	t.Parallel()

	build := func(count int) []byte {
		var b strings.Builder
		b.WriteString(`<e:Envelope xmlns:e="` + envelopeNamespace12 + `"><e:Body><Op`)
		for i := 0; i < count; i++ {
			b.WriteString(` a` + strconv.Itoa(i) + `="v"`)
		}
		b.WriteString(`/></e:Body></e:Envelope>`)
		return []byte(b.String())
	}

	// Exactly at the per-element cap: accepted.
	if _, err := parseXMLTree(build(maxAttributesPerElement)); err != nil {
		t.Fatalf("attributes at cap rejected: %v", err)
	}
	// One attribute over: rejected.
	_, err := parseXMLTree(build(maxAttributesPerElement + 1))
	if !errors.Is(err, ErrXMLComplexity) {
		t.Fatalf("attributes over cap error = %v, want ErrXMLComplexity", err)
	}
}

// TestMalformedBoundaryDiagnosticsAcrossTypedErrors drives incomplete sensitive
// attributes and malformed UTF-8 through all three public response error types.
func TestMalformedBoundaryDiagnosticsAcrossTypedErrors(t *testing.T) {
	t.Parallel()

	const marker = "typed-boundary-secret"

	t.Run("HTTPError", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write(append([]byte(`<π:Node password="`+marker), 0xc3))
		}))
		defer server.Close()
		_, err := newTestClient(t, Config{}).Call(context.Background(), server.URL, Request{BodyXML: []byte(`<Get/>`)})
		var target *HTTPError
		if !errors.As(err, &target) {
			t.Fatalf("error = %v, want *HTTPError", err)
		}
		assertSafeDiagnostic(t, marker, target.Error(), target.RawXML)
	})

	t.Run("ResponseTooLargeError", func(t *testing.T) {
		t.Parallel()
		prefix := []byte(`<π:Node password="` + marker)
		limit := int64(len(prefix) + 1) // one byte of a multibyte rune is retained
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", contentTypeSOAP12)
			_, _ = w.Write(append(prefix, append([]byte{0xc3, 0xa9}, []byte(strings.Repeat("x", 64))...)...))
		}))
		defer server.Close()
		_, err := newTestClient(t, Config{MaxResponseBytes: limit}).Call(context.Background(), server.URL, Request{BodyXML: []byte(`<Get/>`)})
		var target *ResponseTooLargeError
		if !errors.As(err, &target) {
			t.Fatalf("error = %v, want *ResponseTooLargeError", err)
		}
		assertSafeDiagnostic(t, marker, target.Error(), target.RawXML)
	})

	t.Run("ParseError hides arbitrary parser token", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", contentTypeSOAP12)
			_, _ = io.WriteString(w, `<env:Envelope xmlns:env="`+envelopeNamespace12+`"><env:Body><`+marker+`></different></env:Body></env:Envelope>`)
		}))
		defer server.Close()
		_, err := newTestClient(t, Config{}).Call(context.Background(), server.URL, Request{BodyXML: []byte(`<Get/>`)})
		var target *ParseError
		if !errors.As(err, &target) {
			t.Fatalf("error = %v, want *ParseError", err)
		}
		if !errors.Is(target, ErrInvalidXML) {
			t.Fatalf("ParseError = %v, want ErrInvalidXML", target)
		}
		for cause := error(target); cause != nil; cause = errors.Unwrap(cause) {
			if strings.Contains(cause.Error(), marker) {
				t.Fatalf("parser error graph leaked marker: %v", cause)
			}
		}
	})
}

func assertSafeDiagnostic(t *testing.T, marker, message string, raw []byte) {
	t.Helper()
	if strings.Contains(message, marker) || strings.Contains(string(raw), marker) {
		t.Fatalf("typed diagnostic leaked marker: message=%q raw=%q", message, raw)
	}
	if !utf8.ValidString(message) || !utf8.Valid(raw) {
		t.Fatalf("typed diagnostic is not valid UTF-8: message=%q raw=%q", message, raw)
	}
}

func TestSOAP12FaultClassificationUsesOnlyDeliberatePositions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		code string
		sub  string
		want FaultKind
	}{
		{name: "top VersionMismatch", code: "env:VersionMismatch", want: FaultVersionMismatch},
		{name: "direct ONVIF subcode", code: "env:Sender", sub: "ter:NotAuthorized", want: FaultNotAuthorized},
		{name: "ONVIF QName in top Code", code: "ter:NotAuthorized", want: FaultUnknown},
		{name: "VersionMismatch in Subcode", code: "env:Sender", sub: "env:VersionMismatch", want: FaultUnknown},
		{name: "wrong-case VersionMismatch", code: "env:versionmismatch", want: FaultUnknown},
		{name: "wrong-case ONVIF subcode", code: "env:Sender", sub: "ter:notauthorized", want: FaultUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			subcode := ""
			if tc.sub != "" {
				subcode = `<env:Subcode><env:Value>` + tc.sub + `</env:Value></env:Subcode>`
			}
			body := wrapSOAP12Fault(`<env:Code><env:Value>` + tc.code + `</env:Value>` + subcode + `</env:Code><env:Reason><env:Text xml:lang="en">reason</env:Text></env:Reason>`)
			parsed, err := parseEnvelope([]byte(body))
			if err != nil {
				t.Fatalf("parseEnvelope: %v", err)
			}
			if parsed.Fault == nil || parsed.Fault.Kind != tc.want {
				t.Fatalf("fault = %+v, want kind %s", parsed.Fault, tc.want)
			}
		})
	}
}

func TestSOAP12NestedSubcodeCardinalityIsValidated(t *testing.T) {
	t.Parallel()

	cases := []string{
		`<env:Subcode/>`,
		`<env:Subcode><env:Value>ter:InvalidArgVal</env:Value><env:Value>ter:NotAuthorized</env:Value></env:Subcode>`,
		`<env:Subcode><env:Value>ter:InvalidArgVal</env:Value><env:Subcode/><env:Subcode><env:Value>ter:NotAuthorized</env:Value></env:Subcode></env:Subcode>`,
	}
	for index, nested := range cases {
		inner := `<env:Code><env:Value>env:Sender</env:Value><env:Subcode><env:Value>ter:InvalidArgVal</env:Value>` + nested + `</env:Subcode></env:Code><env:Reason><env:Text xml:lang="en">reason</env:Text></env:Reason>`
		if _, err := parseEnvelope([]byte(wrapSOAP12Fault(inner))); err == nil {
			t.Fatalf("case %d accepted malformed nested Subcode", index)
		}
	}
}

func TestSOAP12ReasonTextRequiresLanguage(t *testing.T) {
	t.Parallel()

	inner := `<env:Code><env:Value>env:Sender</env:Value></env:Code><env:Reason><env:Text>reason</env:Text></env:Reason>`
	if _, err := parseEnvelope([]byte(wrapSOAP12Fault(inner))); err == nil {
		t.Fatal("SOAP 1.2 Reason/Text without xml:lang was accepted")
	}
}

func TestRedactSensitiveElementRejectsFalseClosers(t *testing.T) {
	t.Parallel()

	const secret = "secret-after-decoy"
	inputs := []string{
		`<Password><![CDATA[</Password>]]>` + secret + `</Password>`,
		`<Password><!-- </Password> -->` + secret + `</Password>`,
		`<Password>first</evil:Password>` + secret,
		`<Password>first< /Password>` + secret,
		`<Password><Password>inner</Password>` + secret + `</Password>`,
		`<wsse:Password>first</other:Password>` + secret,
	}
	for index, input := range inputs {
		redacted := RedactText(input)
		if strings.Contains(redacted, secret) {
			t.Fatalf("case %d retained data after a false closer: %q", index, redacted)
		}
	}
}

func TestResponseContentTypeMustBeSingleton(t *testing.T) {
	t.Parallel()

	err := validateContentType(Version12, []string{contentTypeSOAP12, contentTypeSOAP11})
	if !errors.Is(err, ErrInvalidMediaType) {
		t.Fatalf("validateContentType error = %v, want ErrInvalidMediaType", err)
	}
	if isVersionFallbackEvidence(newParseError(err, http.StatusInternalServerError, nil)) {
		t.Fatal("duplicate Content-Type values must not drive SOAP fallback")
	}
}
