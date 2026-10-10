package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"time"
)

// SnapshotFeedRequest opens a repeatable snapshot source for one profile.
type SnapshotFeedRequest struct {
	// Host is the camera endpoint, the same-origin anchor.
	Host string
	// Credentials authenticate the session for the feed's lifetime.
	Credentials *Credentials
	// ProfileToken selects the profile whose snapshot URI is polled.
	ProfileToken string
	// Timeout bounds opening the feed; zero uses the default.
	Timeout time.Duration
	// MaxBytes bounds each image; zero uses the snapshot default.
	MaxBytes int64
}

// SnapshotFeed fetches fresh, fully validated JPEG images in memory.
type SnapshotFeed interface {
	Fetch(ctx context.Context) ([]byte, error)
}

// SnapshotFeedUseCase resolves a profile's snapshot URI once and returns a
// feed that reuses the authenticated session for every fetch. It backs the
// live-preview JPEG fallback.
type SnapshotFeedUseCase interface {
	OpenSnapshotFeed(ctx context.Context, request SnapshotFeedRequest) (SnapshotFeed, error)
}

// NewProductionSnapshotFeed returns the production SnapshotFeedUseCase.
func NewProductionSnapshotFeed() SnapshotFeedUseCase { return &productionMedia{} }

// OpenSnapshotFeed validates the token, negotiates the authenticated session,
// and resolves the same-origin snapshot URI without fetching an image.
func (p *productionMedia) OpenSnapshotFeed(ctx context.Context, request SnapshotFeedRequest) (SnapshotFeed, error) {
	if err := ValidateSnapshotProfileToken(request.ProfileToken); err != nil {
		return nil, err
	}
	maxBytes, err := resolveSnapshotMaxBytes(request.MaxBytes)
	if err != nil {
		return nil, err
	}
	cameraURL, seam, timeout, err := p.prepare(request.Host, request.Timeout, request.Credentials)
	if err != nil {
		return nil, err
	}
	openCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	seam.negotiate(openCtx)

	clients, err := p.resolveServices(openCtx, cameraURL, seam)
	if err != nil {
		return nil, err
	}
	if !clients.media2Available && !clients.media1Available {
		return nil, newError(CategoryProtocol, "the camera advertises no usable media service", nil)
	}
	resolver := &profileResolver{clients: clients, session: seam.session, cameraURL: cameraURL}
	rawURI, _, uriErr := resolver.snapshotURI(openCtx, request.ProfileToken)
	if uriErr != nil {
		return nil, mapMediaError(uriErr)
	}
	snapshotURL, err := validateSnapshotURI(cameraURL, rawURI)
	if err != nil {
		return nil, newError(CategoryProtocol, "the camera returned an invalid snapshot URI", nil)
	}
	return &snapshotFeed{
		client:   snapshotHTTPClient(seam.session.httpClient, cameraURL),
		url:      snapshotURL,
		maxBytes: maxBytes,
	}, nil
}

type snapshotFeed struct {
	client   *http.Client
	url      *url.URL
	maxBytes int64
}

// Fetch performs one authenticated GET with the same status, content-type,
// size, truncation, and complete-decode checks as the file snapshot, but
// returns the bytes instead of publishing a file.
func (f *snapshotFeed) Fetch(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url.String(), nil)
	if err != nil {
		return nil, newError(CategoryInternal, "the snapshot request could not be constructed", nil)
	}
	req.Header.Set("Accept", snapshotContentType)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, classifySnapshotTransportError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if status := classifySnapshotStatus(resp.StatusCode); status != nil {
		return nil, status
	}
	if _, ctErr := parseJPEGContentType(resp.Header.Get("Content-Type")); ctErr != nil {
		return nil, ctErr
	}
	if resp.ContentLength > f.maxBytes {
		return nil, newError(CategoryProtocol, "the snapshot exceeds the maximum allowed size", nil)
	}
	var buffer bytes.Buffer
	read, copyErr := io.Copy(&buffer, io.LimitReader(resp.Body, f.maxBytes+1))
	if copyErr != nil {
		return nil, classifySnapshotReadError(copyErr)
	}
	if read > f.maxBytes {
		return nil, newError(CategoryProtocol, "the snapshot exceeds the maximum allowed size", nil)
	}
	if resp.ContentLength >= 0 && read != resp.ContentLength {
		return nil, newError(CategoryProtocol, "the snapshot transfer was truncated", nil)
	}
	image := buffer.Bytes()
	if err := validateSnapshotJPEG(ctx, bytes.NewReader(image), read); err != nil {
		return nil, err
	}
	return image, nil
}
