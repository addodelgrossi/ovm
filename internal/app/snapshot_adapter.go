package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/addodelgrossi/ovm/pkg/onvif/auth"
)

const (
	// defaultSnapshotMaxBytes caps a retrieved snapshot at 20 MiB unless the
	// caller configures a different positive limit.
	defaultSnapshotMaxBytes int64 = 20 << 20

	// snapshotContentType is the only content type a snapshot GET may return.
	snapshotContentType = "image/jpeg"

	// maxSnapshotRedirects caps the number of redirects the snapshot GET follows.
	maxSnapshotRedirects = 10

	// JPEG dimensions are checked from DecodeConfig before full decoding. The
	// per-axis and total-pixel limits bound decoder allocation independently of
	// the encoded-byte limit and still accommodate high-resolution cameras.
	maxSnapshotWidth  = 16_384
	maxSnapshotHeight = 16_384
	maxSnapshotPixels = 64 * 1024 * 1024
)

// Snapshot redirect-policy sentinels. These are fixed, secret-free typed errors
// returned by the GET CheckRedirect policy when a redirect is refused for a
// security-boundary reason (unsafe target, cross-origin, HTTPS->HTTP downgrade,
// or the redirect cap). net/http wraps a CheckRedirect error in a *url.Error
// (which embeds the request URL and query); classifySnapshotTransportError
// recognizes these sentinels THROUGH that wrapper with errors.Is and maps them
// to CategoryProtocol WITHOUT ever retaining the url.Error cause, so a security
// refusal is distinguishable from a dial/transport outage and no URL can leak.
// Each message is a stable, value-free English string.
var (
	// ErrSnapshotRedirectRefused is the base class for every redirect-policy
	// refusal. All specific sentinels wrap it so a caller can test the whole
	// family with a single errors.Is while the concrete sentinel still records
	// the precise reason.
	ErrSnapshotRedirectRefused = errors.New("the snapshot redirect was refused")
	// ErrSnapshotRedirectUnsafeTarget is a redirect to a target that is not a
	// usable, safe URL (missing, bad scheme, userinfo, fragment, control
	// characters, or no host).
	ErrSnapshotRedirectUnsafeTarget = fmt.Errorf("%w: the target is not safe", ErrSnapshotRedirectRefused)
	// ErrSnapshotRedirectCrossOrigin is a redirect that crosses to a different
	// origin than the camera endpoint.
	ErrSnapshotRedirectCrossOrigin = fmt.Errorf("%w: the target crosses origin", ErrSnapshotRedirectRefused)
	// ErrSnapshotRedirectDowngrade is a redirect that downgrades from HTTPS to
	// plaintext HTTP relative to the prior hop.
	ErrSnapshotRedirectDowngrade = fmt.Errorf("%w: the target downgrades transport security", ErrSnapshotRedirectRefused)
	// ErrSnapshotRedirectTooMany is the redirect-cap refusal.
	ErrSnapshotRedirectTooMany = fmt.Errorf("%w: too many redirects", ErrSnapshotRedirectRefused)
)

// Snapshot resolves a snapshot URI for the requested profile (preferred Media2,
// else Media1 under the narrow fallback rule), then retrieves the JPEG over an
// authenticated, same-origin HTTP GET and writes it atomically to disk. The GET
// reuses the single session's authenticated transport (one Digest/Basic cache)
// — it only clones the *http.Client struct to install a GET redirect policy and
// never sends a WS-Security header. Only credential-free metadata is returned.
func (p *productionMedia) Snapshot(ctx context.Context, request SnapshotRequest) (SnapshotMetadata, error) {
	// Validate a non-empty caller token before output processing, endpoint
	// preparation, clock negotiation, GetServices, or any other network work.
	// Empty remains the documented "select deterministic default" sentinel.
	if request.ProfileToken != "" {
		if err := ValidateSnapshotProfileToken(request.ProfileToken); err != nil {
			return SnapshotMetadata{}, err
		}
	}

	// Normalize and validate the destination before any network request. The
	// final publication still enforces no-clobber atomically, so this early
	// check is only a fast, operator-friendly rejection rather than the safety
	// boundary.
	outputPath, err := normalizeSnapshotOutputPath(request.OutputPath)
	if err != nil {
		return SnapshotMetadata{}, err
	}
	if err := validateSnapshotOutputDestination(outputPath, request.Overwrite); err != nil {
		return SnapshotMetadata{}, err
	}

	// Resolve the byte limit and reject an overflowing maximum BEFORE any
	// network request. The read path uses maxBytes+1 to detect over-limit
	// bodies, so a maximum where maxBytes+1 would overflow (math.MaxInt64) is
	// rejected here rather than silently wrapping to a negative sentinel.
	maxBytes, err := resolveSnapshotMaxBytes(request.MaxBytes)
	if err != nil {
		return SnapshotMetadata{}, err
	}

	cameraURL, seam, timeout, err := p.prepare(request.Host, request.Timeout, request.Credentials)
	if err != nil {
		return SnapshotMetadata{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	seam.negotiate(ctx)

	clients, err := p.resolveServices(ctx, cameraURL, seam)
	if err != nil {
		return SnapshotMetadata{}, err
	}
	if !clients.media2Available && !clients.media1Available {
		return SnapshotMetadata{}, newError(CategoryProtocol, "the camera advertises no usable media service", nil)
	}

	resolver := &profileResolver{clients: clients, session: seam.session, cameraURL: cameraURL}
	profileToken := request.ProfileToken
	if profileToken == "" {
		profiles, _, _, profilesErr := resolver.fetchProfiles(ctx)
		if profilesErr != nil {
			return SnapshotMetadata{}, mapMediaError(profilesErr)
		}
		profileToken, err = deterministicPrimaryProfileToken(profiles)
		if err != nil {
			return SnapshotMetadata{}, err
		}
	}
	rawURI, source, uriErr := resolver.snapshotURI(ctx, profileToken)
	if uriErr != nil {
		return SnapshotMetadata{}, mapMediaError(uriErr)
	}

	// Validate the snapshot URI: absolute HTTP/HTTPS, no userinfo/fragment/
	// control chars, and same-origin with the camera endpoint. The error must
	// never include the URI.
	snapshotURL, err := validateSnapshotURI(cameraURL, rawURI)
	if err != nil {
		// The URI came from the camera, so an unsafe or malformed value is a
		// protocol failure rather than invalid caller input. Do not retain the
		// validation error or URI in the unwrap chain.
		return SnapshotMetadata{}, newError(CategoryProtocol, "the camera returned an invalid snapshot URI", nil)
	}

	// Reuse the session's single authenticated HTTP client. Clone ONLY the
	// *http.Client struct so the GET can carry its own redirect policy; the
	// Transport pointer (origin-scoped auth.DigestTransport and its challenge
	// cache) is shared, not rebuilt. A snapshot GET carries HTTP auth only and
	// never a WS-Security header.
	httpClient := snapshotHTTPClient(seam.session.httpClient, cameraURL)

	size, contentType, err := fetchAndWriteSnapshotWithOverwrite(ctx, httpClient, snapshotURL, outputPath, maxBytes, request.Overwrite)
	if err != nil {
		return SnapshotMetadata{}, err
	}

	return SnapshotMetadata{
		ProfileToken: profileToken,
		OutputPath:   outputPath,
		ByteSize:     size,
		ContentType:  contentType,
		Source:       source,
	}, nil
}

// normalizeSnapshotOutputPath requires a non-empty output path and normalizes
// it with filepath.Abs followed by filepath.Clean. The returned path is the
// exact value used for the temp file, the final rename, and the returned
// metadata. An empty or un-absolutizable path is a safe usage error and no
// network request is ever made. The error never includes the raw path.
func normalizeSnapshotOutputPath(raw string) (string, error) {
	if raw == "" {
		return "", usageError("an output path is required")
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", usageError("the output path is not usable")
	}
	return filepath.Clean(abs), nil
}

// validateSnapshotOutputDestination rejects unusable parent paths and, unless
// overwrite is explicitly enabled, existing destinations before any device is
// contacted. The later publication step independently enforces no-clobber to
// close the race between this check and the final write.
func validateSnapshotOutputDestination(outputPath string, overwrite bool) error {
	parent, err := os.Stat(filepath.Dir(outputPath))
	if err != nil || !parent.IsDir() {
		return usageError("the output directory is not usable")
	}
	info, err := os.Lstat(outputPath)
	switch {
	case err == nil && info.IsDir():
		return usageError("the output path must name a file")
	case err == nil && !overwrite:
		return usageError("the output file already exists")
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return usageError("the output path is not usable")
	default:
		return nil
	}
}

// deterministicPrimaryProfileToken selects the lexicographically smallest
// non-empty token. This is stable across device response order and service
// version and avoids treating a display name as an identity. An empty or
// unusable profile list is a malformed device response, not caller input.
func deterministicPrimaryProfileToken(profiles []normProfile) (string, error) {
	selected := ""
	for _, profile := range profiles {
		if profile.token != "" && (selected == "" || profile.token < selected) {
			selected = profile.token
		}
	}
	if selected == "" {
		return "", newError(CategoryProtocol, "the camera returned no selectable media profile", nil)
	}
	return selected, nil
}

// resolveSnapshotMaxBytes resolves the effective byte limit. A non-positive
// request uses the 20 MiB default. A positive request is honored. Any value for
// which maxBytes+1 would overflow a signed 64-bit integer (math.MaxInt64) is
// rejected as a usage error BEFORE any network request, so the limit+1 bounded
// read can never wrap to a negative sentinel.
func resolveSnapshotMaxBytes(requested int64) (int64, error) {
	if requested <= 0 {
		return defaultSnapshotMaxBytes, nil
	}
	// Reject any value where maxBytes+1 would overflow a signed 64-bit integer.
	if requested > math.MaxInt64-1 {
		return 0, usageError("the maximum snapshot size is too large")
	}
	return requested, nil
}

// validateSnapshotURI enforces: absolute HTTP/HTTPS, no userinfo, no fragment,
// no control characters, and same-origin with the camera endpoint. The returned
// URL preserves the query because the outgoing request requires it. Errors never
// include the URI value.
func validateSnapshotURI(cameraURL *url.URL, raw string) (*url.URL, error) {
	if containsControl(raw) {
		return nil, usageError("the snapshot URI is not valid")
	}
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() {
		return nil, usageError("the snapshot URI is not valid")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, usageError("the snapshot URI scheme is not supported")
	}
	if parsed.User != nil {
		return nil, usageError("the snapshot URI must not contain credentials")
	}
	if parsed.Fragment != "" || parsed.RawFragment != "" {
		return nil, usageError("the snapshot URI must not contain a fragment")
	}
	if parsed.Hostname() == "" {
		return nil, usageError("the snapshot URI is not valid")
	}
	if !sameOrigin(cameraURL, parsed) {
		return nil, usageError("the snapshot URI is not same-origin with the camera")
	}
	if urlHasDecodedControl(parsed) {
		return nil, usageError("the snapshot URI is not valid")
	}
	return parsed, nil
}

// snapshotHTTPClient derives the HTTP client used for the snapshot GET from
// the session's single authenticated client. It clones ONLY the *http.Client
// struct so the GET can carry its own same-origin/no-downgrade redirect policy;
// the Transport pointer (the origin-scoped auth.DigestTransport and its
// challenge cache) is shared, never rebuilt, so the GET reuses the exact same
// RoundTripper the SOAP calls used. The clone carries no WS-Security: WSSE is a
// SOAP header applied per request body, and a snapshot GET sends none.
func snapshotHTTPClient(base *http.Client, cameraURL *url.URL) *http.Client {
	return &http.Client{
		Transport:     base.Transport,
		CheckRedirect: snapshotRedirectPolicy(cameraURL),
		Jar:           base.Jar,
		Timeout:       base.Timeout,
	}
}

// snapshotRedirectPolicy returns an http.Client CheckRedirect that validates
// every redirect target BEFORE the client issues the request to it: the target
// must carry no userinfo, no fragment, no control characters, a supported
// http/https scheme, must not downgrade HTTPS->HTTP relative to any prior hop,
// and must remain exact same-origin with the camera endpoint. Returning an
// error aborts the request before the target is contacted; the error text never
// includes a URL. The redirect count is capped.
func snapshotRedirectPolicy(cameraURL *url.URL) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxSnapshotRedirects {
			return ErrSnapshotRedirectTooMany
		}
		target := req.URL
		if target == nil {
			return ErrSnapshotRedirectUnsafeTarget
		}
		// Reject controls whether they appear literally or percent-encoded: a
		// decoded control in the path or query is a response-splitting vector.
		if containsControl(target.String()) || urlHasDecodedControl(target) {
			return ErrSnapshotRedirectUnsafeTarget
		}
		scheme := strings.ToLower(target.Scheme)
		if scheme != "http" && scheme != "https" {
			return ErrSnapshotRedirectUnsafeTarget
		}
		if target.User != nil {
			return ErrSnapshotRedirectUnsafeTarget
		}
		if target.Fragment != "" || target.RawFragment != "" {
			return ErrSnapshotRedirectUnsafeTarget
		}
		if target.Hostname() == "" {
			return ErrSnapshotRedirectUnsafeTarget
		}
		// Refuse HTTP downgrade relative to the immediately prior hop.
		prev := via[len(via)-1].URL
		if strings.EqualFold(prev.Scheme, "https") && !strings.EqualFold(target.Scheme, "https") {
			return ErrSnapshotRedirectDowngrade
		}
		if !sameOrigin(cameraURL, target) {
			return ErrSnapshotRedirectCrossOrigin
		}
		return nil
	}
}

// fetchAndWriteSnapshot performs a no-clobber snapshot fetch. It is retained as
// the narrow default used by tests and internal callers that do not explicitly
// authorize replacement.
func fetchAndWriteSnapshot(ctx context.Context, client *http.Client, snapshotURL *url.URL, outputPath string, maxBytes int64) (int64, string, error) {
	return fetchAndWriteSnapshotWithOverwrite(ctx, client, snapshotURL, outputPath, maxBytes, false)
}

// fetchAndWriteSnapshotWithOverwrite performs the authenticated GET and writes
// the body to outputPath atomically. It enforces: a 2xx status (401/403 and
// policy refusals mapped to authentication), a parsed content type of exactly
// image/jpeg, an early Content-Length check against the limit, a limit+1
// bounded read, declared-length truncation detection, explicit body close, and
// context/deadline honoring. Publication is atomic and either no-clobber or an
// explicitly authorized replacement. Errors never retain the raw
// url.Error/http error, the URI, the body, or any secret in their unwrap chain.
func fetchAndWriteSnapshotWithOverwrite(ctx context.Context, client *http.Client, snapshotURL *url.URL, outputPath string, maxBytes int64, overwrite bool) (int64, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, snapshotURL.String(), nil)
	if err != nil {
		// A construction error here cannot carry a response/url cause; it is a
		// local build failure. Do not retain it to keep the unwrap chain safe.
		return 0, "", newError(CategoryInternal, "the snapshot request could not be constructed", nil)
	}
	req.Header.Set("Accept", snapshotContentType)

	resp, err := client.Do(req)
	if err != nil {
		return 0, "", classifySnapshotTransportError(err)
	}
	// Always close the body on every response path; a close error is handled
	// explicitly so a leak or an ignored failure never slips past lint.
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			_ = cerr // The body is drained/closed best-effort; nothing to add.
		}
	}()

	if status := classifySnapshotStatus(resp.StatusCode); status != nil {
		return 0, "", status
	}

	contentType, ctErr := parseJPEGContentType(resp.Header.Get("Content-Type"))
	if ctErr != nil {
		return 0, "", ctErr
	}

	// Early Content-Length rejection when the advertised size already exceeds
	// the limit, so an oversized body is never buffered or streamed.
	declaredLength := resp.ContentLength
	if declaredLength > maxBytes {
		return 0, "", newError(CategoryProtocol, "the snapshot exceeds the maximum allowed size", nil)
	}

	written, writeErr := writeSnapshotAtomically(ctx, resp.Body, outputPath, maxBytes, declaredLength, overwrite)
	if writeErr != nil {
		return 0, "", writeErr
	}
	return written, contentType, nil
}

// writeSnapshotAtomically streams the response body into a 0600 temp file in
// the destination directory and publishes it only after a complete,
// within-limit, non-truncated read. Without overwrite it uses a same-directory
// hard link as the atomic no-clobber publication primitive; with overwrite it
// uses atomic rename. The final output is always 0600.
func writeSnapshotAtomically(ctx context.Context, body io.Reader, outputPath string, maxBytes, declaredLength int64, overwrite bool) (int64, error) {
	dir := filepath.Dir(outputPath)
	tmp, err := os.CreateTemp(dir, ".snapshot-*.jpg.tmp")
	if err != nil {
		return 0, newError(CategoryInternal, "the snapshot file could not be created", nil)
	}
	tmpName := tmp.Name()
	// Install the cleanup guard immediately so every subsequent error path
	// removes the temp file. closed guards against a double close.
	closed := false
	cleanup := func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return 0, newError(CategoryInternal, "the snapshot file permissions could not be set", nil)
	}

	// Read at most maxBytes+1 so an over-limit body (chunked, no Content-Length)
	// is detected by the extra byte without buffering the whole stream.
	limited := io.LimitReader(body, maxBytes+1)
	written, copyErr := io.Copy(tmp, limited)
	if copyErr != nil {
		cleanup()
		return 0, classifySnapshotReadError(copyErr)
	}
	if written > maxBytes {
		cleanup()
		return 0, newError(CategoryProtocol, "the snapshot exceeds the maximum allowed size", nil)
	}
	// Declared-length truncation: when the server advertised a Content-Length,
	// a short body (even one that ended cleanly) is a truncated transfer and
	// must never publish a file.
	if declaredLength >= 0 && written != declaredLength {
		cleanup()
		return 0, newError(CategoryProtocol, "the snapshot transfer was truncated", nil)
	}
	// Honor context cancellation that may have arrived during the copy without
	// surfacing as a read error (defensive).
	if err := ctx.Err(); err != nil {
		cleanup()
		return 0, mapContextError(err)
	}

	// MIME is only a transport assertion. Parse dimensions without allocating a
	// full image, enforce the decoder-resource policy, then decode the complete
	// file and reject trailing bytes before the temp file can be published.
	if err := validateSnapshotJPEG(ctx, tmp, written); err != nil {
		cleanup()
		return 0, err
	}

	if err := tmp.Sync(); err != nil {
		cleanup()
		return 0, newError(CategoryInternal, "the snapshot could not be flushed", nil)
	}
	if err := tmp.Close(); err != nil {
		closed = true
		_ = os.Remove(tmpName)
		return 0, newError(CategoryInternal, "the snapshot file could not be closed", nil)
	}
	closed = true
	// Verify/retain 0600 on the closed temp file so an inherited umask or a
	// prior permissive destination cannot widen the final mode.
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return 0, newError(CategoryInternal, "the snapshot file permissions could not be set", nil)
	}
	// Second context check immediately before the atomic rename: a cancel that
	// arrived after the copy must still prevent publishing the file.
	if err := ctx.Err(); err != nil {
		_ = os.Remove(tmpName)
		return 0, mapContextError(err)
	}
	if overwrite {
		if err := os.Rename(tmpName, outputPath); err != nil {
			_ = os.Remove(tmpName)
			return 0, newError(CategoryInternal, "the snapshot file could not be finalized", nil)
		}
		return written, nil
	}

	// Link publishes the complete temp inode under the destination name only if
	// that name is still absent. Unlike a preflight Stat, this is the atomic
	// no-clobber boundary and closes races with another writer. Once linked, the
	// destination is complete; removing the temporary name is best-effort and
	// cannot turn a successful publication into an error with a visible output.
	if err := os.Link(tmpName, outputPath); err != nil {
		_ = os.Remove(tmpName)
		if errors.Is(err, os.ErrExist) {
			return 0, usageError("the output file already exists")
		}
		return 0, newError(CategoryInternal, "the snapshot file could not be finalized", nil)
	}
	_ = os.Remove(tmpName)
	return written, nil
}

// exactJPEGReader prevents image/jpeg's private read-ahead buffer from hiding
// trailing bytes. Read returns one logical byte at a time, while buffered keeps
// the underlying file access efficient. ReadByte exposes the next unconsumed
// logical byte for the exact-consumption check after decoding.
type exactJPEGReader struct {
	buffered *bufio.Reader
}

func (reader *exactJPEGReader) Read(destination []byte) (int, error) {
	if len(destination) == 0 {
		return 0, nil
	}
	value, err := reader.buffered.ReadByte()
	if err != nil {
		return 0, err
	}
	destination[0] = value
	return 1, nil
}

func (reader *exactJPEGReader) ReadByte() (byte, error) {
	return reader.buffered.ReadByte()
}

// validateSnapshotJPEG verifies the staged body structurally before it can be
// published. DecodeConfig obtains dimensions without allocating the image;
// only an image within the hard per-axis and total-pixel policy is fully
// decoded. A caller-supplied image/jpeg MIME type is not sufficient evidence.
// The full decoder must consume exactly the staged bytes, so truncated images,
// malformed scan data, format spoofing, concatenated images, and trailing data
// are rejected with fixed, value-free protocol errors.
func validateSnapshotJPEG(ctx context.Context, file io.ReaderAt, size int64) error {
	if err := ctx.Err(); err != nil {
		return mapContextError(err)
	}
	if size <= 0 {
		return newError(CategoryProtocol, "the snapshot response is not a complete JPEG image", nil)
	}

	config, err := jpeg.DecodeConfig(io.NewSectionReader(file, 0, size))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return mapContextError(ctxErr)
		}
		return newError(CategoryProtocol, "the snapshot response is not a complete JPEG image", nil)
	}
	if err := validateSnapshotDimensions(config.Width, config.Height); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return mapContextError(err)
	}

	// The standard decoder reads ahead internally in 4 KiB blocks. Feed it
	// through an exactJPEGReader that exposes only one logical byte per Read
	// while buffering file I/O underneath. The decoder can no longer consume
	// bytes past EOI into an inaccessible buffer, so the post-decode ReadByte can
	// reliably detect trailing or concatenated data.
	reader := &exactJPEGReader{buffered: bufio.NewReader(io.NewSectionReader(file, 0, size))}
	decoded, err := jpeg.Decode(reader)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return mapContextError(ctxErr)
		}
		return newError(CategoryProtocol, "the snapshot response is not a complete JPEG image", nil)
	}
	if err := ctx.Err(); err != nil {
		return mapContextError(err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != config.Width || bounds.Dy() != config.Height {
		return newError(CategoryProtocol, "the snapshot response is not a complete JPEG image", nil)
	}

	if _, trailingErr := reader.ReadByte(); trailingErr == nil {
		return newError(CategoryProtocol, "the snapshot response contains trailing data", nil)
	} else if !errors.Is(trailingErr, io.EOF) {
		return newError(CategoryInternal, "the snapshot file could not be validated", nil)
	}
	return nil
}

// validateSnapshotDimensions applies overflow-safe decoder resource limits.
func validateSnapshotDimensions(width, height int) error {
	if width <= 0 || height <= 0 {
		return newError(CategoryProtocol, "the snapshot response has invalid JPEG dimensions", nil)
	}
	if width > maxSnapshotWidth || height > maxSnapshotHeight {
		return newError(CategoryProtocol, "the snapshot JPEG dimensions exceed the allowed limit", nil)
	}
	pixels := uint64(width) * uint64(height)
	if pixels > uint64(maxSnapshotPixels) {
		return newError(CategoryProtocol, "the snapshot JPEG dimensions exceed the allowed limit", nil)
	}
	return nil
}

// classifySnapshotTransportError maps a client.Do error to a safe typed
// application error. It inspects the error with errors.Is to recognize
// cancellation, deadline, and the authentication-policy sentinels (Basic
// disabled, plaintext Basic disallowed, legacy no-qop Digest blocked), but it
// NEVER retains the raw url.Error (which embeds the request URL and query) in
// the returned error's unwrap chain. Only a stable context sentinel or no cause
// is kept, so a recursive errors.Unwrap can never recover the URL, query, or
// response body.
func classifySnapshotTransportError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return newError(CategoryCancelled, "the snapshot request was cancelled", context.Canceled)
	case errors.Is(err, context.DeadlineExceeded):
		return newError(CategoryNetwork, "the snapshot request timed out", context.DeadlineExceeded)
	case errors.Is(err, ErrSnapshotRedirectRefused):
		// A redirect-policy refusal (unsafe target, cross-origin, HTTPS->HTTP
		// downgrade, or the redirect cap) is a protocol/security-boundary
		// decision, NOT a transport outage. net/http wraps the specific
		// sentinel in a *url.Error that embeds the request URL and query;
		// recognize it through that wrapper with errors.Is and retain the
		// SPECIFIC, value-free sentinel (which itself wraps the base class) so
		// a caller still learns the precise reason while a recursive unwrap can
		// never recover the URL.
		return newError(CategoryProtocol, "the snapshot redirect was refused", redirectRefusalSentinel(err))
	case errors.Is(err, auth.ErrAuthPolicy),
		errors.Is(err, ErrInsecureDigestBlocked):
		// A policy refusal (Basic disabled, plaintext Basic disallowed, or
		// legacy no-qop Digest blocked) is an authentication failure. Keep the
		// stable sentinel only; never retain the url.Error cause.
		return newError(CategoryAuthentication, "the device rejected the credentials", nil)
	default:
		return newError(CategoryNetwork, "the snapshot request failed", nil)
	}
}

// redirectRefusalSentinel returns the specific, value-free redirect-policy
// sentinel that err matches, falling back to the base class. It lets the mapped
// application error retain the precise reason (cross-origin, downgrade, unsafe
// target, too many) WITHOUT retaining the url.Error that carries the URL.
func redirectRefusalSentinel(err error) error {
	switch {
	case errors.Is(err, ErrSnapshotRedirectCrossOrigin):
		return ErrSnapshotRedirectCrossOrigin
	case errors.Is(err, ErrSnapshotRedirectDowngrade):
		return ErrSnapshotRedirectDowngrade
	case errors.Is(err, ErrSnapshotRedirectTooMany):
		return ErrSnapshotRedirectTooMany
	case errors.Is(err, ErrSnapshotRedirectUnsafeTarget):
		return ErrSnapshotRedirectUnsafeTarget
	default:
		return ErrSnapshotRedirectRefused
	}
}

// classifySnapshotStatus maps the HTTP response status to a safe typed error,
// or nil for a 2xx success. A 401/403 is an authentication failure; any other
// non-2xx is a network failure. The error retains no cause.
func classifySnapshotStatus(status int) error {
	if status >= 200 && status < 300 {
		return nil
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return newError(CategoryAuthentication, "the device rejected the credentials", nil)
	}
	return newError(CategoryNetwork, "the snapshot request returned a non-success status", nil)
}

// classifySnapshotReadError maps a body read/copy error to a safe typed error.
// Cancellation and deadline are recognized through errors.Is; any other read
// failure is a network error. The raw read error is never retained as a cause.
func classifySnapshotReadError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return newError(CategoryCancelled, "the snapshot read was cancelled", context.Canceled)
	case errors.Is(err, context.DeadlineExceeded):
		return newError(CategoryNetwork, "the snapshot read timed out", context.DeadlineExceeded)
	default:
		return newError(CategoryNetwork, "the snapshot could not be read", nil)
	}
}

// mapContextError maps a context error observed defensively to a safe typed
// error, retaining only the stable context sentinel as the cause.
func mapContextError(err error) error {
	if errors.Is(err, context.Canceled) {
		return newError(CategoryCancelled, "the operation was cancelled", context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return newError(CategoryNetwork, "the operation timed out", context.DeadlineExceeded)
	}
	return newError(CategoryInternal, "the operation could not be completed", nil)
}

// parseJPEGContentType parses the Content-Type header and requires the media
// type to be exactly image/jpeg (parameters allowed). The error never includes
// the header value.
func parseJPEGContentType(header string) (string, error) {
	if header == "" {
		return "", newError(CategoryProtocol, "the snapshot response has no content type", nil)
	}
	mediaType, _, err := mime.ParseMediaType(header)
	if err != nil {
		return "", newError(CategoryProtocol, "the snapshot response content type is malformed", nil)
	}
	if !strings.EqualFold(mediaType, snapshotContentType) {
		return "", newError(CategoryProtocol, "the snapshot response is not a JPEG image", nil)
	}
	return snapshotContentType, nil
}
