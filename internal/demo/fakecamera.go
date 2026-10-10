package demo

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

// Loopback service paths. These are the standard ONVIF service entry points the
// fake device dispatches on, and the demo advertises them same-origin so the
// application's same-origin validation accepts every URL.
const (
	devicePath   = "/onvif/device_service"
	media1Path   = "/onvif/media_service"
	media2Path   = "/onvif/media2_service"
	snapshotPath = "/onvif/snapshot"
)

// HTTP server timeouts. They are intentionally short but ample for a loopback
// round trip, and they bound every phase of a connection so a misbehaving
// client cannot pin a goroutine or socket open.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 30 * time.Second
	shutdownTimeout   = 5 * time.Second
)

// serviceURLs holds the credential-free, same-origin URLs derived from the
// bound loopback address. Every field is a plain HTTP URL safe to render.
type serviceURLs struct {
	// Origin is the scheme://host[:port] base, for example
	// "http://127.0.0.1:54321".
	Origin string
	// Device is the ONVIF Device management service URL.
	Device string
	// Media1 is the ONVIF Media ver10 service URL.
	Media1 string
	// Media2 is the ONVIF Media ver20 service URL.
	Media2 string
	// Snapshot is the authenticated JPEG snapshot URL.
	Snapshot string
}

// fakeCamera owns a loopback IPv4 listener, a same-origin fake ONVIF device,
// and the http.Server that serves it. Its lifecycle is: bind -> serve on one
// tracked goroutine -> idempotent, bounded shutdown with a forced-close
// fallback. It leaks neither the listener nor the serve goroutine.
type fakeCamera struct {
	device   *fakedevice.Device
	listener net.Listener
	server   *http.Server
	urls     serviceURLs

	serveErrOnce sync.Once
	serveErr     error
	serveDone    chan struct{}

	closeOnce sync.Once
	closeErr  error
}

// newFakeCamera binds an IPv4 loopback listener on an ephemeral port, derives
// the same-origin service URLs from the bound address, configures the fake
// device to advertise exactly those URLs, and starts serving on a single
// tracked goroutine. On any error it leaves no listener open.
func newFakeCamera(username, password string, rtspPort int) (*fakeCamera, error) {
	// Bind explicitly to the IPv4 loopback with an ephemeral port. Using the
	// literal "127.0.0.1" (never "localhost" or "::1") guarantees an IPv4
	// loopback socket regardless of host name resolution or IPv6 preference.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("bind loopback listener: %w", err)
	}

	urls, err := deriveServiceURLs(listener.Addr())
	if err != nil {
		// Close the listener we just opened so a derivation failure cannot leak
		// the socket.
		_ = listener.Close()
		return nil, err
	}

	device, err := fakedevice.New(demoDeviceConfig(urls, username, password, rtspPort))
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("configure fake device: %w", err)
	}

	camera := &fakeCamera{
		device:    device,
		listener:  listener,
		urls:      urls,
		serveDone: make(chan struct{}),
	}
	camera.server = &http.Server{
		Handler:           device,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	go camera.serve()

	return camera, nil
}

// serve runs the single tracked serve goroutine. http.Server.Serve always
// returns a non-nil error; the orderly shutdown sentinel http.ErrServerClosed
// is expected and is not recorded as a failure. serveDone is closed exactly
// once when the goroutine exits so shutdown can join it without racing.
func (c *fakeCamera) serve() {
	defer close(c.serveDone)
	err := c.server.Serve(c.listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		c.serveErrOnce.Do(func() { c.serveErr = err })
	}
}

// deriveServiceURLs builds one real HTTP origin from the bound address and the
// four same-origin service URLs from it, using net/url for all composition so
// the host:port is always correctly bracketed and escaped.
func deriveServiceURLs(addr net.Addr) (serviceURLs, error) {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		return serviceURLs{}, fmt.Errorf("listener address is not TCP: %T", addr)
	}
	if !tcpAddr.IP.IsLoopback() || tcpAddr.IP.To4() == nil {
		return serviceURLs{}, fmt.Errorf("listener is not bound to an IPv4 loopback address")
	}

	origin := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(tcpAddr.IP.String(), fmt.Sprintf("%d", tcpAddr.Port)),
	}
	join := func(path string) string {
		ref := &url.URL{Path: path}
		return origin.ResolveReference(ref).String()
	}
	return serviceURLs{
		Origin:   origin.String(),
		Device:   join(devicePath),
		Media1:   join(media1Path),
		Media2:   join(media2Path),
		Snapshot: join(snapshotPath),
	}, nil
}

// Shutdown performs a bounded, graceful shutdown and then forces a close if the
// graceful phase does not complete in time. It is idempotent: repeated calls
// return the first outcome. It never leaves the listener open or the serve
// goroutine running on return.
func (c *fakeCamera) Shutdown(ctx context.Context) error {
	c.closeOnce.Do(func() {
		shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()

		err := c.server.Shutdown(shutdownCtx)
		if err != nil {
			// Graceful shutdown timed out or failed; force every connection and
			// the listener closed. Close is safe to call after Shutdown.
			if closeErr := c.server.Close(); closeErr != nil &&
				!errors.Is(closeErr, http.ErrServerClosed) {
				err = errors.Join(err, closeErr)
			} else {
				// The forced close succeeded; the orderly outcome is a clean
				// shutdown even though the graceful phase was exceeded.
				err = nil
			}
		}

		// Join the serve goroutine so no goroutine outlives Shutdown.
		<-c.serveDone

		// Surface a genuine serve error (never the orderly-close sentinel).
		if err == nil {
			err = c.serveErr
		}
		c.closeErr = err
	})
	return c.closeErr
}

// Close is the context-free convenience form of Shutdown used by callers that
// do not supply a deadline. It is idempotent through the same guard.
func (c *fakeCamera) Close() error {
	return c.Shutdown(context.Background())
}

// demoDeviceConfig assembles the fake-device configuration that advertises the
// same-origin Device/Media1/Media2 service and capability URLs and requires a
// modern WS-Security UsernameToken authenticated with the synthetic
// credentials, and advertises stream URIs on the synthetic RTSP server's port.
// UsernameToken works over plaintext loopback HTTP without any
// Basic/legacy-Digest downgrade, so the demo keeps the secure default policy.
func demoDeviceConfig(urls serviceURLs, username, password string, rtspPort int) fakedevice.Config {
	return fakedevice.Config{
		RequireUsernameToken: true,
		Username:             username,
		Password:             password,
		// Stream URIs point at the demo's loopback synthetic RTSP server, and
		// the encoder accepts both codecs so encoder changes can be tried.
		MediaInventory: fakedevice.MediaInventory{
			RTSPPort: rtspPort,
			Media2EncoderOptions: []fakedevice.Media2EncoderOptionData{
				{
					Encoding: "H264", Profiles: []string{"Baseline", "Main", "High"},
					FrameRates:  []float64{30, 25, 20, 15, 10, 5},
					Resolutions: []fakedevice.ResolutionData{{Width: 1920, Height: 1080}, {Width: 1280, Height: 720}, {Width: 640, Height: 360}},
					QualityMin:  0, QualityMax: 10, BitrateMin: 256, BitrateMax: 8192, GovLengthMin: 1, GovLengthMax: 120,
				},
				{
					Encoding: "H265", Profiles: []string{"Main"},
					FrameRates:  []float64{30, 25, 20, 15, 10, 5},
					Resolutions: []fakedevice.ResolutionData{{Width: 1920, Height: 1080}, {Width: 1280, Height: 720}},
					QualityMin:  0, QualityMax: 10, BitrateMin: 256, BitrateMax: 8192, GovLengthMin: 1, GovLengthMax: 120,
				},
			},
		},
		Inventory: fakedevice.Inventory{
			Services: []fakedevice.ServiceData{
				{
					Namespace:    "http://www.onvif.org/ver10/device/wsdl",
					XAddr:        urls.Device,
					VersionMajor: 25,
					VersionMinor: 6,
				},
				{
					Namespace:    "http://www.onvif.org/ver10/media/wsdl",
					XAddr:        urls.Media1,
					VersionMajor: 25,
					VersionMinor: 6,
				},
				{
					Namespace:    "http://www.onvif.org/ver20/media/wsdl",
					XAddr:        urls.Media2,
					VersionMajor: 25,
					VersionMinor: 6,
				},
			},
			Capabilities: fakedevice.CapabilitiesData{
				DeviceXAddr: urls.Device,
				MediaXAddr:  urls.Media1,
			},
		},
	}
}
