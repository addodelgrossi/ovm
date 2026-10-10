package main

import (
	"context"
	"errors"
	"sync"

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
type PreviewSourceFactory func(ctx context.Context, manager Manager, selection core.StreamSelection) (stream.H264Source, func(), error)

var errPreviewLimit = errors.New("preview limit reached")

type previewController struct {
	factory PreviewSourceFactory
	origins []string

	mu     sync.Mutex
	server *preview.Server
	active map[string]*activePreview
	closed bool
	wg     sync.WaitGroup
}

type activePreview struct {
	cancel context.CancelFunc
}

func newPreviewController(factory PreviewSourceFactory, origins []string) *previewController {
	return &previewController{factory: factory, origins: origins, active: make(map[string]*activePreview)}
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
	if len(c.active) >= maxConcurrentPreviews {
		c.mu.Unlock()
		return nil, errPreviewLimit
	}
	if c.server == nil {
		server, err := preview.NewServer(c.origins)
		if err != nil {
			c.mu.Unlock()
			return nil, err
		}
		c.server = server
	}
	server := c.server
	c.mu.Unlock()

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

	ctx, cancel := context.WithCancel(root)
	c.mu.Lock()
	if c.closed || len(c.active) >= maxConcurrentPreviews {
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
	c.active[session.ID()] = &activePreview{cancel: cancel}
	c.wg.Add(1)
	c.mu.Unlock()

	go func() {
		defer c.wg.Done()
		_ = stream.PumpH264FMP4(ctx, source, session)
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

func (c *previewController) stop(sessionID string) bool {
	c.mu.Lock()
	active := c.active[sessionID]
	c.mu.Unlock()
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
