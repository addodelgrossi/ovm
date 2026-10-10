package soap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// DefaultTimeout bounds a call when the caller has no earlier deadline.
	DefaultTimeout = 10 * time.Second
	// DefaultMaxResponseBytes is the default SOAP response body limit.
	DefaultMaxResponseBytes int64 = 2 << 20
	// DefaultMaxRequestBytes is the default serialized SOAP request limit. It is
	// positive and bounded so a request can never be sent unbounded.
	DefaultMaxRequestBytes int64 = 1 << 20
)

// maxInt64 is the largest representable int64, used as the overflow sentinel.
const maxInt64 = int64(^uint64(0) >> 1)

// TraceEvent is the credential-safe structured result of one HTTP attempt.
type TraceEvent struct {
	Method     string
	Action     string
	Version    Version
	Duration   time.Duration
	StatusCode int
	Outcome    string
	// Endpoint is the credential-free endpoint (RedactURL).
	Endpoint string
	// RequestXML and ResponseXML are the redacted, bounded envelopes. They
	// are populated only when Config.TraceBodies is set.
	RequestXML  []byte
	ResponseXML []byte
}

// MaxTraceBodyBytes bounds each traced envelope.
const MaxTraceBodyBytes = 64 << 10

// TraceFunc receives one event per SOAP version attempt.
type TraceFunc func(context.Context, TraceEvent)

// Config configures a SOAP client. The supplied HTTP client is cloned.
type Config struct {
	HTTPClient       *http.Client
	Timeout          time.Duration
	MaxResponseBytes int64
	// MaxRequestBytes bounds the serialized SOAP request size. Zero selects
	// DefaultMaxRequestBytes. The size is checked before any DTD scan, buffer
	// growth, replay, or HTTP transport, so an over-limit request sends nothing.
	MaxRequestBytes int64
	Trace           TraceFunc
	// TraceBodies adds redacted, bounded request and response envelopes to
	// each trace event, for a diagnostic SOAP console.
	TraceBodies bool
}

// Request contains a SOAP body fragment and optional header fragments.
type Request struct {
	Action      string
	HeaderXML   []byte
	BodyXML     []byte
	HTTPHeaders http.Header
}

// Response contains a parsed SOAP response. RawXML is bounded and redacted;
// BodyXML retains the operation payload for a typed service decoder.
type Response struct {
	Version    Version
	BodyXML    []byte
	RawXML     []byte
	HTTPStatus int
	Header     http.Header
}

// Client performs bounded SOAP HTTP calls.
type Client struct {
	httpClient       *http.Client
	timeout          time.Duration
	maxResponseBytes int64
	maxRequestBytes  int64
	trace            TraceFunc
	traceBodies      bool
}

// NewClient constructs a SOAP client with secure redirect handling.
func NewClient(config Config) (*Client, error) {
	timeout := config.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	if timeout < 0 {
		return nil, errors.New("SOAP timeout must be positive")
	}
	limit := config.MaxResponseBytes
	if limit == 0 {
		limit = DefaultMaxResponseBytes
	}
	if limit < 1 || limit == maxInt64 {
		return nil, errors.New("SOAP response limit must be positive and bounded")
	}
	requestLimit := config.MaxRequestBytes
	if requestLimit == 0 {
		requestLimit = DefaultMaxRequestBytes
	}
	if requestLimit < 1 || requestLimit == maxInt64 {
		return nil, errors.New("SOAP request limit must be positive and bounded")
	}

	var httpClient http.Client
	if config.HTTPClient != nil {
		httpClient = *config.HTTPClient
	} else {
		httpClient = http.Client{}
	}
	previousRedirectPolicy := httpClient.CheckRedirect
	httpClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if len(via) == 0 {
			return ErrUnsafeRedirect
		}
		initialScheme := via[0].URL.Scheme
		initialHost := via[0].URL.Hostname()
		initialPort := effectivePort(via[0].URL)
		if previousRedirectPolicy != nil {
			if err := previousRedirectPolicy(request, via); err != nil {
				return err
			}
		}
		if request.URL.User != nil ||
			!strings.EqualFold(initialScheme, request.URL.Scheme) ||
			!strings.EqualFold(initialHost, request.URL.Hostname()) ||
			initialPort != effectivePort(request.URL) {
			return ErrCrossOriginRedirect
		}
		if request.Method != http.MethodPost || request.Body == nil {
			return ErrUnsafeRedirect
		}
		return nil
	}

	return &Client{
		httpClient:       &httpClient,
		timeout:          timeout,
		maxResponseBytes: limit,
		maxRequestBytes:  requestLimit,
		trace:            config.Trace,
		traceBodies:      config.TraceBodies && config.Trace != nil,
	}, nil
}

// Call sends SOAP 1.2 first and retries once with SOAP 1.1 only after HTTP 415
// or a validated VersionMismatch fault.
func (c *Client) Call(ctx context.Context, endpoint string, request Request) (*Response, error) {
	callContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	response, err := c.callVersion(callContext, endpoint, request, Version12)
	if err == nil || !isVersionFallbackEvidence(err) {
		return response, err
	}
	return c.callVersion(callContext, endpoint, request, Version11)
}

func (c *Client) callVersion(ctx context.Context, endpoint string, request Request, version Version) (*Response, error) {
	started := time.Now()
	event := TraceEvent{
		Method:   http.MethodPost,
		Action:   RedactText(request.Action),
		Version:  version,
		Outcome:  "transport_error",
		Endpoint: RedactURL(endpoint),
	}
	defer func() {
		if c.trace == nil {
			return
		}
		event.Duration = time.Since(started)
		c.trace(ctx, event)
	}()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parsedEndpoint, err := validateEndpoint(endpoint)
	if err != nil {
		event.Outcome = "invalid_request"
		return nil, err
	}
	if strings.ContainsAny(request.Action, "\r\n") {
		event.Outcome = "invalid_request"
		return nil, ErrInvalidAction
	}

	// Enforce the request-size limit with overflow-safe arithmetic BEFORE the
	// DTD scan, buffer growth, replay, or transport. An over-limit request
	// yields a typed error and sends nothing.
	if size, ok := serializedEnvelopeSize(version, request.HeaderXML, request.BodyXML); !ok || size > c.maxRequestBytes {
		event.Outcome = "request_too_large"
		reported := size
		if !ok {
			reported = -1
		}
		return nil, &RequestTooLargeError{Version: version, Limit: c.maxRequestBytes, Size: reported}
	}

	envelope, err := buildEnvelope(version, request.HeaderXML, request.BodyXML)
	if err != nil {
		event.Outcome = "invalid_request"
		return nil, err
	}
	if c.traceBodies {
		event.RequestXML = traceBody(envelope)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, parsedEndpoint.String(), bytes.NewReader(envelope))
	if err != nil {
		event.Outcome = "invalid_request"
		return nil, fmt.Errorf("create SOAP request: %w", err)
	}
	if request.HTTPHeaders == nil {
		httpRequest.Header = make(http.Header)
	} else {
		httpRequest.Header = request.HTTPHeaders.Clone()
	}
	applyActionHeaders(httpRequest.Header, version, request.Action)

	httpResponse, err := c.httpClient.Do(httpRequest)
	if err != nil {
		if httpResponse != nil && httpResponse.Body != nil {
			_ = httpResponse.Body.Close()
			event.StatusCode = httpResponse.StatusCode
		}
		return nil, &TransportError{Version: version, Err: err}
	}
	event.StatusCode = httpResponse.StatusCode

	// HTTP 415 is version-fallback evidence regardless of body readability,
	// size, or content, so it is classified before reading the body.
	if httpResponse.StatusCode == http.StatusUnsupportedMediaType {
		_ = httpResponse.Body.Close()
		event.Outcome = "http_error"
		return nil, &HTTPError{StatusCode: httpResponse.StatusCode}
	}

	data, tooLarge, readErr := readBounded(httpResponse.Body, c.maxResponseBytes)
	closeErr := httpResponse.Body.Close()
	if readErr != nil {
		event.Outcome = "read_error"
		return nil, fmt.Errorf("read SOAP response: %w", readErr)
	}
	if closeErr != nil {
		event.Outcome = "read_error"
		return nil, fmt.Errorf("close SOAP response: %w", closeErr)
	}
	rawXML := boundedRedacted(data, c.maxResponseBytes)
	if c.traceBodies {
		event.ResponseXML = traceBody(data)
	}
	if tooLarge {
		event.Outcome = "response_too_large"
		return nil, &ResponseTooLargeError{Limit: c.maxResponseBytes, HTTPStatus: httpResponse.StatusCode, RawXML: rawXML}
	}

	parsed, parseErr := parseEnvelope(data)
	// A DTD is an XXE vector and is rejected regardless of media type.
	if errors.Is(parseErr, ErrDTDNotAllowed) {
		event.Outcome = "invalid_xml"
		return nil, newParseError(parseErr, httpResponse.StatusCode, rawXML)
	}

	// A non-success status without a parseable SOAP fault is an HTTP error. This
	// path does not require a SOAP media type because the body is not a SOAP
	// envelope the client trusts.
	isSuccess := httpResponse.StatusCode >= 200 && httpResponse.StatusCode < 300
	hasFault := parseErr == nil && parsed.Fault != nil
	if !isSuccess && !hasFault {
		event.Outcome = "http_error"
		return nil, &HTTPError{StatusCode: httpResponse.StatusCode, RawXML: rawXML}
	}

	// Both successful and fault SOAP responses must carry a media type
	// compatible with the attempted version. A missing, malformed, or
	// cross-version media type is rejected and never used as fallback evidence.
	if err := validateContentType(version, httpResponse.Header.Values("Content-Type")); err != nil {
		event.Outcome = "invalid_media_type"
		return nil, newParseError(err, httpResponse.StatusCode, rawXML)
	}

	// Reject a cross-version envelope: the resolved envelope namespace must
	// match the attempted version. Checked before fault/body handling so a
	// mismatched envelope cannot drive fallback or be returned as success.
	if parseErr == nil && parsed.Version != "" && parsed.Version != version {
		event.Outcome = "envelope_version_mismatch"
		return nil, newParseError(ErrEnvelopeVersionMismatch, httpResponse.StatusCode, rawXML)
	}

	if hasFault {
		parsed.Fault.RawXML = rawXML
		parsed.Fault.HTTPStatus = httpResponse.StatusCode
		event.Outcome = "soap_fault"
		return nil, parsed.Fault
	}

	if parseErr != nil {
		event.Outcome = "invalid_xml"
		return nil, newParseError(parseErr, httpResponse.StatusCode, rawXML)
	}

	event.Outcome = "success"
	return &Response{
		Version:    parsed.Version,
		BodyXML:    parsed.BodyXML,
		RawXML:     rawXML,
		HTTPStatus: httpResponse.StatusCode,
		Header:     httpResponse.Header.Clone(),
	}, nil
}

func applyActionHeaders(headers http.Header, version Version, action string) {
	switch version {
	case Version12:
		parameters := map[string]string{"charset": "utf-8"}
		if action != "" {
			parameters["action"] = action
		}
		headers.Set("Content-Type", mime.FormatMediaType("application/soap+xml", parameters))
		headers.Del("SOAPAction")
	case Version11:
		headers.Set("Content-Type", "text/xml; charset=utf-8")
		headers.Set("SOAPAction", strconv.Quote(action))
	}
	headers.Set("Accept", "application/soap+xml, text/xml")
}

// validateContentType requires exactly one parseable media-type field value
// compatible with the attempted SOAP version. SOAP 1.2 requires
// application/soap+xml; SOAP 1.1 requires text/xml. Missing, duplicated,
// malformed, or cross-version values are rejected with ErrInvalidMediaType.
func validateContentType(version Version, values []string) error {
	if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		return fmt.Errorf("%w: missing Content-Type", ErrInvalidMediaType)
	}
	if len(values) != 1 {
		return fmt.Errorf("%w: multiple Content-Type values", ErrInvalidMediaType)
	}
	mediaType, _, err := mime.ParseMediaType(values[0])
	if err != nil {
		return fmt.Errorf("%w: malformed Content-Type", ErrInvalidMediaType)
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	switch version {
	case Version12:
		if mediaType != "application/soap+xml" {
			return fmt.Errorf("%w: SOAP 1.2 requires application/soap+xml", ErrInvalidMediaType)
		}
	case Version11:
		if mediaType != "text/xml" {
			return fmt.Errorf("%w: SOAP 1.1 requires text/xml", ErrInvalidMediaType)
		}
	default:
		return fmt.Errorf("%w: unsupported SOAP version", ErrInvalidMediaType)
	}
	return nil
}

// serializedEnvelopeSize computes the exact serialized envelope size using
// overflow-safe addition. The second return is false when the size would
// overflow int64, in which case the request must be rejected.
func serializedEnvelopeSize(version Version, headerXML, bodyXML []byte) (int64, bool) {
	namespace := version.EnvelopeNamespace()
	if namespace == "" {
		// Unsupported versions are sized as zero fixed overhead; buildEnvelope
		// will reject them. Returning ok keeps the size check from masking the
		// clearer unsupported-version error.
		return 0, true
	}
	// Fixed literals emitted by buildEnvelope.
	const (
		prolog     = `<?xml version="1.0" encoding="UTF-8"?>`
		envOpen    = `<env:Envelope xmlns:env="`
		envClose   = `">`
		headerOpen = `<env:Header>`
		headerEnd  = `</env:Header>`
		bodyOpen   = `<env:Body>`
		bodyEnd    = `</env:Body></env:Envelope>`
	)
	total := int64(0)
	add := func(n int) bool {
		if n < 0 {
			return false
		}
		if total > maxInt64-int64(n) {
			return false
		}
		total += int64(n)
		return true
	}
	ok := add(len(prolog)) &&
		add(len(envOpen)) &&
		add(len(namespace)) &&
		add(len(envClose)) &&
		add(len(bodyOpen)) &&
		add(len(bodyXML)) &&
		add(len(bodyEnd))
	if !ok {
		return 0, false
	}
	// A non-whitespace header fragment contributes the header element.
	if len(bytes.TrimSpace(headerXML)) > 0 {
		if !add(len(headerOpen)) || !add(len(headerXML)) || !add(len(headerEnd)) {
			return 0, false
		}
	}
	return total, true
}

func readBounded(body io.Reader, limit int64) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > limit {
		return data[:limit], true, nil
	}
	return data, false, nil
}

func boundedRedacted(data []byte, limit int64) []byte {
	// Redact BEFORE clipping so a secret spanning the limit boundary cannot
	// survive by being split. clipValidUTF8 then enforces both the byte bound and
	// a complete UTF-8 boundary for every typed diagnostic.
	return clipValidUTF8(RedactXML(data), limit)
}

func validateEndpoint(endpoint string) (*url.URL, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed URL", ErrInvalidEndpoint)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%w: endpoint scheme must be HTTP or HTTPS", ErrInvalidEndpoint)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("%w: endpoint host is required", ErrInvalidEndpoint)
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("%w: endpoint userinfo is not allowed", ErrInvalidEndpoint)
	}
	if parsed.Fragment != "" {
		return nil, fmt.Errorf("%w: endpoint fragment is not allowed", ErrInvalidEndpoint)
	}
	return parsed, nil
}

func effectivePort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	if strings.EqualFold(value.Scheme, "http") {
		return "80"
	}
	return ""
}

// isVersionFallbackEvidence reports whether an error justifies a SOAP 1.1 retry.
// Only an actual HTTP 415 or a fully validated SOAP 1.2 VersionMismatch fault
// qualifies; a wrong media type, cross-version envelope, or misplaced fault does
// not.
func isVersionFallbackEvidence(err error) bool {
	// A media-type or envelope-version rejection must never drive fallback even
	// though it may wrap a status code.
	if errors.Is(err, ErrInvalidMediaType) || errors.Is(err, ErrEnvelopeVersionMismatch) {
		return false
	}
	if errors.Is(err, ErrVersionMismatch) {
		// Only a SOAP 1.2 VersionMismatch fault is evidence.
		var fault *Fault
		if errors.As(err, &fault) {
			return fault.Version == Version12
		}
		return false
	}
	var statusError httpStatusError
	return errors.As(err, &statusError) && statusError.httpStatusCode() == http.StatusUnsupportedMediaType
}

// traceBody returns a redacted copy of an envelope, truncated to
// MaxTraceBodyBytes on a UTF-8 boundary. Redaction runs on the full envelope
// before truncation so a secret can never be split past the redactor.
func traceBody(data []byte) []byte {
	redacted := RedactXML(data)
	if len(redacted) <= MaxTraceBodyBytes {
		return redacted
	}
	cut := MaxTraceBodyBytes
	for cut > 0 && !utf8.RuneStart(redacted[cut]) {
		cut--
	}
	return append(redacted[:cut:cut], []byte("…")...)
}
