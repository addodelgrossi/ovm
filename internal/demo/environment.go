package demo

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync/atomic"

	"github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/internal/store"
	"github.com/addodelgrossi/ovm/internal/synthmedia"
)

// DemoCameraLabel is the clearly labeled display name seeded for the single
// demo device, so any surface rendering the catalog shows an unmistakably
// synthetic camera.
const DemoCameraLabel = "OVM Demo Camera"

// Synthetic credentials. They authenticate only the in-process fake device and
// are seeded into process memory alone — never written to disk or a keyring.
// They are not real and grant access to nothing outside this process.
const (
	demoUsername = "ovm-demo"
	// demoPassword is a synthetic, process-local value for the in-memory fake
	// device only. It is not a real credential and protects nothing outside
	// this process.
	demoPassword = "ovm-demo-synthetic-secret" // #nosec G101 -- synthetic in-memory demo credential, not a real secret.
)

// Capabilities is the credential-free descriptor of what the demo environment
// provides. The demo serves a project-owned synthetic H.264 test pattern over a
// loopback RTSP server; it never claims a physical camera.
type Capabilities struct {
	// RTSPServerAvailable reports the loopback synthetic RTSP server.
	RTSPServerAvailable bool `json:"rtsp_server_available"`
	// PlaybackAvailable reports in-app live preview of the synthetic stream.
	PlaybackAvailable bool `json:"playback_available"`
	// VLCAvailable is always false: the demo does not launch external players.
	VLCAvailable bool `json:"vlc_available"`
}

// Environment is the demo composition root. It owns the fake camera's lifecycle
// and exposes the production Manager plus credential-free URLs and a capability
// descriptor. It holds no secret material: the synthetic credential lives only
// in the in-memory secret provider, keyed by an opaque reference.
type Environment struct {
	manager  *app.Manager
	camera   *fakeCamera
	rtsp     *synthmedia.RTSPServer
	deviceID string
}

// Runtime is the small, boundary-safe view of a running demo environment.
// Everything in it is safe to render: the URLs are credential-free and the
// capability flags advertise the absence of streaming features.
type Runtime struct {
	// DeviceID is the stable identifier of the seeded demo device.
	DeviceID string `json:"device_id"`
	// Label is the demo device display name.
	Label string `json:"label"`
	// Origin is the real HTTP origin the fake camera is bound to.
	Origin string `json:"origin"`
	// DeviceServiceURL is the ONVIF Device service URL.
	DeviceServiceURL string `json:"device_service_url"`
	// Media1ServiceURL is the ONVIF Media ver10 service URL.
	Media1ServiceURL string `json:"media1_service_url"`
	// Media2ServiceURL is the ONVIF Media ver20 service URL.
	Media2ServiceURL string `json:"media2_service_url"`
	// SnapshotURL is the authenticated JPEG snapshot URL.
	SnapshotURL string `json:"snapshot_url"`
	// Capabilities advertises which features the demo provides.
	Capabilities Capabilities `json:"capabilities"`
}

// NewEnvironment builds a fully composed, running demo environment. It binds
// the loopback fake camera, wires the production Inventory and MediaProfiles
// adapters through the Manager, and seeds a single clearly labeled demo device
// with synthetic credentials in a private in-memory repository and secret
// provider. It never constructs a disk-backed repository or a platform secret
// provider and never touches disk or the keyring.
//
// On any failure the already-bound camera is closed so no listener or goroutine
// leaks.
func NewEnvironment(ctx context.Context) (*Environment, error) {
	// The synthetic RTSP server requires the same demo credentials, so live
	// preview exercises the production RTSP client end to end. Each request is
	// served in the codec the fake camera currently reports for the profile,
	// so an encoder change in the app changes the streamed codec too.
	var cameraRef atomic.Pointer[fakeCamera]
	rtsp, err := synthmedia.StartRTSPServer(synthmedia.RTSPServerConfig{
		Video: synthmedia.H264Config{Width: 640, Height: 352, FPS: 25, GOP: 50},
		Route: func(_, query string) string {
			return demoStreamCodec(cameraRef.Load(), query)
		},
		Username:         demoUsername,
		Password:         demoPassword,
		SDPParameterSets: true,
	})
	if err != nil {
		return nil, fmt.Errorf("start demo RTSP server: %w", err)
	}
	camera, err := newFakeCamera(demoUsername, demoPassword, rtsp.Port())
	if err != nil {
		rtsp.Close()
		return nil, err
	}
	cameraRef.Store(camera)

	// In-memory persistence only: a process-local secret provider and a private
	// in-memory repository. Neither NewDefaultRepository/NewRepository nor
	// NewPlatformSecretProvider is ever called here.
	secrets := store.NewMemorySecretProvider()
	repository := newMemoryRepository(secrets)

	manager, err := app.NewManager(app.ManagerDependencies{
		Repository:     repository,
		HostInterfaces: demoHostInterfaces{},
		Discovery:      demoDiscovery{endpoint: camera.urls.Device},
		Inventory:      app.NewProductionInventory(),
		Media:          app.NewProductionMediaProfiles(),
		LiveStreams:    app.NewProductionLiveStreams(),
		SnapshotFeeds:  app.NewProductionSnapshotFeed(),
		Probe:          app.NewProductionProbe(),
		Encoder:        app.NewProductionEncoder(),
	})
	if err != nil {
		_ = camera.Close()
		rtsp.Close()
		return nil, fmt.Errorf("compose demo manager: %w", err)
	}

	// The seeded device's canonical endpoint is the fake camera's same-origin
	// Device service URL, so the Manager drives the production adapters against
	// the real loopback origin.
	deviceID, err := seedDemoDevice(ctx, repository, camera.urls.Device)
	if err != nil {
		_ = manager.Close()
		_ = camera.Close()
		rtsp.Close()
		return nil, err
	}

	return &Environment{
		manager:  manager,
		camera:   camera,
		rtsp:     rtsp,
		deviceID: deviceID,
	}, nil
}

// seedDemoDevice inserts the synthetic secret keyed by an opaque reference and
// a clearly labeled demo device referencing it, both in memory only. It returns
// the stable device ID assigned by the repository.
//
// The secret and its reference exist only in the in-memory provider; the device
// metadata carries the opaque reference, never a username or password.
func seedDemoDevice(ctx context.Context, repository *memoryRepository, endpoint string) (string, error) {
	reference, err := repository.NewSecretReference()
	if err != nil {
		return "", fmt.Errorf("allocate demo secret reference: %w", err)
	}

	// Secure default policy: no Basic, no plaintext Basic, no legacy Digest.
	// The fake device authenticates with a modern WS-Security UsernameToken,
	// which the production media/inventory session negotiates without any
	// downgrade. The secret is a redacting value type and is confined to this
	// function: it is handed by value to the in-memory provider and never
	// retained in demo state.
	secret, err := store.NewSecret(demoUsername, demoPassword, store.AuthPolicy{})
	if err != nil {
		return "", fmt.Errorf("construct demo secret: %w", err)
	}
	if err := repository.PutSecret(ctx, reference, secret); err != nil {
		return "", fmt.Errorf("seed demo secret: %w", err)
	}

	stored, err := repository.Upsert(store.Device{
		Endpoint:            endpoint,
		DisplayName:         DemoCameraLabel,
		DiscoverySource:     store.DiscoverySourceManual,
		CredentialReference: reference,
		AuthPolicy:          store.AuthPolicy{},
	})
	if err != nil {
		// Best-effort cleanup of the orphaned secret so a failed seed leaves no
		// dangling reference even in memory.
		_ = repository.DeleteSecret(ctx, reference)
		return "", fmt.Errorf("seed demo device: %w", err)
	}
	return stored.ID, nil
}

// Manager returns the composed production Manager driving the demo environment.
func (e *Environment) Manager() *app.Manager {
	return e.manager
}

// DeviceID returns the stable identifier of the seeded demo device.
func (e *Environment) DeviceID() string {
	return e.deviceID
}

// Runtime returns the credential-free runtime view: the demo device identity,
// the real same-origin URLs, and the capability descriptor.
func (e *Environment) Runtime() Runtime {
	urls := e.camera.urls
	return Runtime{
		DeviceID:         e.deviceID,
		Label:            DemoCameraLabel,
		Origin:           urls.Origin,
		DeviceServiceURL: urls.Device,
		Media1ServiceURL: urls.Media1,
		Media2ServiceURL: urls.Media2,
		SnapshotURL:      urls.Snapshot,
		Capabilities:     e.Capabilities(),
	}
}

// Capabilities returns the credential-free capability descriptor. The demo
// serves a synthetic RTSP stream for live preview; VLC stays unavailable.
func (e *Environment) Capabilities() Capabilities {
	return Capabilities{
		RTSPServerAvailable: true,
		PlaybackAvailable:   true,
		VLCAvailable:        false,
	}
}

// Close shuts the environment down: it closes the Manager (cancelling in-flight
// operations) and then performs the bounded, idempotent camera shutdown. It is
// safe to call more than once and reports the first non-nil error from either
// the Manager or the camera.
func (e *Environment) Close(ctx context.Context) error {
	managerErr := e.manager.Close()
	cameraErr := e.camera.Shutdown(ctx)
	e.rtsp.Close()
	return errors.Join(managerErr, cameraErr)
}

// demoStreamCodec serves H.265 when the fake camera's current encoder for the
// requested profile is H.265 and H.264 otherwise.
func demoStreamCodec(camera *fakeCamera, query string) string {
	values, err := url.ParseQuery(query)
	if err != nil || camera == nil {
		return synthmedia.CodecH264
	}
	if encoding, ok := camera.device.ProfileEncoding(values.Get("profile")); ok && encoding == "H265" {
		return synthmedia.CodecH265
	}
	return synthmedia.CodecH264
}
