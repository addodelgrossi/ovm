package main

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	core "github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/internal/preview"
	"github.com/addodelgrossi/ovm/pkg/stream"
)

// maxConcurrentPreviews matches the four-camera grid.
const maxConcurrentPreviews = 4

// wailsPageOrigin is the origin of the embedded frontend on macOS and Linux.
const wailsPageOrigin = "wails://wails"

// PreviewSourceFactory opens a live H.264 source for a stream selection. The
// returned release function, when non-nil, is called once the preview ends.
type PreviewSourceFactory func(ctx context.Context, manager Manager, selection core.StreamSelection) (stream.VideoSource, func(), error)

var (
	errPreviewLimit    = errors.New("preview limit reached")
	errPreviewNotFound = errors.New("preview session not found")
)

type previewController struct {
	factory PreviewSourceFactory
	origins []string
	// clipDir locates the clip folder; tests replace it.
	clipDir func() (string, error)

	mu        sync.Mutex
	server    *preview.Server
	active    map[string]*activePreview
	snapshots map[string]*preview.SnapshotSession
	closed    bool
	wg        sync.WaitGroup
}

type activePreview struct {
	cancel    context.CancelFunc
	meter     *stream.Meter
	sink      *previewSink
	selection core.StreamSelection
}

func newPreviewController(factory PreviewSourceFactory, origins []string) *previewController {
	return &previewController{
		factory:   factory,
		origins:   origins,
		clipDir:   clipDirectory,
		active:    make(map[string]*activePreview),
		snapshots: make(map[string]*preview.SnapshotSession),
	}
}

func (c *previewController) available() bool { return c != nil && c.factory != nil }

// start opens a source with the call context, then remuxes it in the
// background under root until stopped, closed, or the source ends.
func (c *previewController) start(callCtx, root context.Context, manager Manager, selection core.StreamSelection) (*StartPreviewResult, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, preview.ErrClosed
	}
	if len(c.active)+len(c.snapshots) >= maxConcurrentPreviews {
		c.mu.Unlock()
		return nil, errPreviewLimit
	}
	server, err := c.serverLocked()
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}

	// Time to first frame includes connecting to the camera.
	meter := stream.NewMeter(nil)
	source, release, err := c.factory(callCtx, manager, selection)
	if err != nil {
		return nil, err
	}
	session, err := server.NewSession()
	if err != nil {
		if release != nil {
			release()
		}
		return nil, err
	}

	metered := stream.NewMeteredSource(source, meter)
	ctx, cancel := context.WithCancel(root)
	c.mu.Lock()
	if c.closed || len(c.active)+len(c.snapshots) >= maxConcurrentPreviews {
		c.mu.Unlock()
		cancel()
		session.Close()
		if release != nil {
			release()
		}
		if c.closed {
			return nil, preview.ErrClosed
		}
		return nil, errPreviewLimit
	}
	sink := &previewSink{bridge: session}
	c.active[session.ID()] = &activePreview{cancel: cancel, meter: meter, sink: sink, selection: selection}
	c.wg.Add(1)
	c.mu.Unlock()

	go func() {
		defer c.wg.Done()
		_ = stream.PumpFMP4(ctx, metered, sink)
		// A recording still running when the stream ends is published.
		_, _, _, _ = sink.finishRecording()
		session.Close()
		if release != nil {
			release()
		}
		cancel()
		c.mu.Lock()
		delete(c.active, session.ID())
		c.mu.Unlock()
	}()
	return &StartPreviewResult{
		SessionID: session.ID(),
		URL:       server.BaseURL() + session.Path(),
		Token:     server.Token(),
	}, nil
}

// serverLocked starts the bridge on first use. c.mu must be held.
func (c *previewController) serverLocked() (*preview.Server, error) {
	if c.server == nil {
		server, err := preview.NewServer(c.origins)
		if err != nil {
			return nil, err
		}
		c.server = server
	}
	return c.server, nil
}

// startSnapshot opens the JPEG fallback for a selection. It shares the
// preview limit with live sessions.
func (c *previewController) startSnapshot(callCtx context.Context, manager Manager, selection core.StreamSelection) (*StartPreviewResult, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, preview.ErrClosed
	}
	if len(c.active)+len(c.snapshots) >= maxConcurrentPreviews {
		c.mu.Unlock()
		return nil, errPreviewLimit
	}
	server, err := c.serverLocked()
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	feed, err := manager.OpenSnapshotFeed(callCtx, selection)
	if err != nil {
		return nil, err
	}
	session, err := server.NewSnapshotSession(feed.Fetch)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.closed || len(c.active)+len(c.snapshots) >= maxConcurrentPreviews {
		closed := c.closed
		c.mu.Unlock()
		session.Close()
		if closed {
			return nil, preview.ErrClosed
		}
		return nil, errPreviewLimit
	}
	c.snapshots[session.ID()] = session
	c.mu.Unlock()
	return &StartPreviewResult{
		SessionID: session.ID(),
		URL:       server.BaseURL() + session.Path(),
		Token:     server.Token(),
	}, nil
}

// startRecording begins a keyframe-aligned clip of an active live preview.
func (c *previewController) startRecording(sessionID string, now time.Time) (string, error) {
	c.mu.Lock()
	active := c.active[sessionID]
	c.mu.Unlock()
	if active == nil {
		return "", errPreviewNotFound
	}
	directory, err := c.clipDir()
	if err != nil {
		return "", stream.ErrClipWrite
	}
	path := filepath.Join(directory, clipFileName(active.selection.ProfileToken, now))
	if err := active.sink.startRecording(path); err != nil {
		return "", err
	}
	return path, nil
}

// stopRecording publishes the clip of an active live preview.
func (c *previewController) stopRecording(sessionID string) (*RecordingResult, error) {
	c.mu.Lock()
	active := c.active[sessionID]
	c.mu.Unlock()
	if active == nil {
		return nil, errPreviewNotFound
	}
	path, size, interrupted, err := active.sink.finishRecording()
	if err != nil {
		return nil, err
	}
	return &RecordingResult{Path: path, Bytes: size, Interrupted: interrupted}, nil
}

// stats returns the measurements of an active preview.
func (c *previewController) stats(sessionID string) (stream.Stats, bool) {
	c.mu.Lock()
	active := c.active[sessionID]
	c.mu.Unlock()
	if active == nil {
		return stream.Stats{}, false
	}
	return active.meter.Snapshot(), true
}

func (c *previewController) stop(sessionID string) bool {
	c.mu.Lock()
	active := c.active[sessionID]
	snapshot := c.snapshots[sessionID]
	delete(c.snapshots, sessionID)
	c.mu.Unlock()
	if snapshot != nil {
		snapshot.Close()
		return true
	}
	if active == nil {
		return false
	}
	active.cancel()
	return true
}

// close stops every preview, waits for the pumps, then shuts the bridge down.
func (c *previewController) close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	for _, active := range c.active {
		active.cancel()
	}
	for id, snapshot := range c.snapshots {
		snapshot.Close()
		delete(c.snapshots, id)
	}
	server := c.server
	c.mu.Unlock()

	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if server != nil {
		return server.Close(ctx)
	}
	return nil
}
