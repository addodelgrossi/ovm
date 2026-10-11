package app

import (
	"context"
	"testing"

	media1 "github.com/addodelgrossi/ovm/pkg/onvif/media"
	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// stubMedia2 is a recording media2Client whose per-operation error is
// injectable. Call counts prove exactly which operations ran.
type stubMedia2 struct {
	profilesErr error
	streamErr   error
	snapshotErr error
	videoErr    error
	audioErr    error

	profilesCalls int
	streamCalls   int
	snapshotCalls int
	videoCalls    int
	audioCalls    int

	streamProtocols  []media2.StreamProtocol
	profileTypeCalls [][]media2.ConfigurationType
}

func (s *stubMedia2) GetProfiles(_ context.Context, _ string, types []media2.ConfigurationType, _ ...media2.RequestOptions) ([]media2.Profile, error) {
	s.profilesCalls++
	s.profileTypeCalls = append(s.profileTypeCalls, append([]media2.ConfigurationType(nil), types...))
	if s.profilesErr != nil {
		return nil, s.profilesErr
	}
	return []media2.Profile{{Token: "p0", Name: "main", Associations: media2.ProfileAssociations{
		VideoSourceConfigurationToken: "vsrccfg0", VideoSourceToken: "vsrc0",
		VideoEncoderConfigurationToken: "venc0", AudioEncoderConfigurationToken: "aenc0",
	}}}, nil
}

func (s *stubMedia2) GetStreamUri(_ context.Context, protocol media2.StreamProtocol, _ string, _ ...media2.RequestOptions) (string, error) {
	s.streamCalls++
	s.streamProtocols = append(s.streamProtocols, protocol)
	if s.streamErr != nil {
		return "", s.streamErr
	}
	return "rtsp://cam.local:554/stream", nil
}

func (s *stubMedia2) GetSnapshotUri(_ context.Context, _ string, _ ...media2.RequestOptions) (string, error) {
	s.snapshotCalls++
	if s.snapshotErr != nil {
		return "", s.snapshotErr
	}
	return "http://cam.local/snap", nil
}

func (s *stubMedia2) GetVideoEncoderConfigurations(_ context.Context, _, _ string, _ ...media2.RequestOptions) ([]media2.VideoEncoderConfiguration, error) {
	s.videoCalls++
	if s.videoErr != nil {
		return nil, s.videoErr
	}
	return []media2.VideoEncoderConfiguration{{Token: "venc0", Encoding: "H265", Resolution: media2.VideoResolution{Width: 1920, Height: 1080}}}, nil
}

func (s *stubMedia2) GetAudioEncoderConfigurations(_ context.Context, _, _ string, _ ...media2.RequestOptions) ([]media2.AudioEncoderConfiguration, error) {
	s.audioCalls++
	if s.audioErr != nil {
		return nil, s.audioErr
	}
	return []media2.AudioEncoderConfiguration{{Token: "aenc0", Encoding: "PCMU", Bitrate: 64, SampleRate: 8}}, nil
}

// stubMedia1 is a recording media1Client. Each read records a call so a test
// can prove a Media1 operation ran exactly once (a genuine fallback) or never.
type stubMedia1 struct {
	profilesCalls int
	streamCalls   int
	snapshotCalls int
	videoSrcCalls int
	videoEncCalls int
	audioEncCalls int

	streamSetups []media1.StreamSetup

	profiles      []media1.Profile
	videoSources  []media1.VideoSource
	videoEncoders []media1.VideoEncoderConfiguration
	audioEncoders []media1.AudioEncoderConfiguration
}

func (s *stubMedia1) GetProfiles(_ context.Context, _ ...media1.RequestOptions) ([]media1.Profile, error) {
	s.profilesCalls++
	if s.profiles != nil {
		return s.profiles, nil
	}
	return []media1.Profile{{Token: "p0", Name: "main", Associations: media1.ProfileAssociations{
		VideoSourceConfigurationToken: "vsrccfg0", VideoSourceToken: "vsrc0",
		VideoEncoderConfigurationToken: "venc0", AudioEncoderConfigurationToken: "aenc0",
	}}}, nil
}

func (s *stubMedia1) GetStreamUri(_ context.Context, _ string, setup media1.StreamSetup, _ ...media1.RequestOptions) (media1.MediaURI, error) {
	s.streamCalls++
	s.streamSetups = append(s.streamSetups, setup)
	return media1.MediaURI{URI: "rtsp://cam.local:554/stream"}, nil
}

func (s *stubMedia1) GetSnapshotUri(_ context.Context, _ string, _ ...media1.RequestOptions) (media1.MediaURI, error) {
	s.snapshotCalls++
	return media1.MediaURI{URI: "http://cam.local/snap"}, nil
}

func (s *stubMedia1) GetVideoSources(_ context.Context, _ ...media1.RequestOptions) ([]media1.VideoSource, error) {
	s.videoSrcCalls++
	if s.videoSources != nil {
		return s.videoSources, nil
	}
	return []media1.VideoSource{{Token: "vsrc0"}}, nil
}

func (s *stubMedia1) GetVideoEncoderConfigurations(_ context.Context, _ ...media1.RequestOptions) ([]media1.VideoEncoderConfiguration, error) {
	s.videoEncCalls++
	if s.videoEncoders != nil {
		return s.videoEncoders, nil
	}
	return []media1.VideoEncoderConfiguration{{Token: "venc0", Encoding: media1.VideoEncodingH264, Resolution: media1.VideoResolution{Width: 1920, Height: 1080}}}, nil
}

func (s *stubMedia1) GetAudioEncoderConfigurations(_ context.Context, _ ...media1.RequestOptions) ([]media1.AudioEncoderConfiguration, error) {
	s.audioEncCalls++
	if s.audioEncoders != nil {
		return s.audioEncoders, nil
	}
	return []media1.AudioEncoderConfiguration{{Token: "aenc0", Encoding: media1.AudioEncodingG711, Bitrate: 64, SampleRate: 8}}, nil
}

// newUnitResolver builds a resolver over an unauthenticated session (nil
// credentials, so invokeWithAuth runs each op exactly once with no WSSE and no
// retry) and the injected stub clients. No network, SOAP server, or fake HTTP
// is involved.
func newUnitResolver(t *testing.T, m2 *stubMedia2, m1 *stubMedia1) *profileResolver {
	t.Helper()
	sess, err := newSession(sessionConfig{endpointURL: "http://cam.local/onvif/device_service", timeout: 0})
	if err != nil {
		t.Fatalf("newSession() error = %v", err)
	}
	clients := mediaClients{}
	if m2 != nil {
		clients.media2 = m2
		clients.media2Available = true
	}
	if m1 != nil {
		clients.media1 = m1
		clients.media1Available = true
	}
	return &profileResolver{
		clients:   clients,
		session:   sess,
		cameraURL: mustURL(t, "http://cam.local/onvif/device_service"),
	}
}

// newUnitResolverIface is newUnitResolver for callers that already hold the
// narrow client interfaces (for example a decorator over a stub). A nil client
// marks that service unavailable.
func newUnitResolverIface(t *testing.T, m2 media2Client, m1 media1Client) *profileResolver {
	t.Helper()
	sess, err := newSession(sessionConfig{endpointURL: "http://cam.local/onvif/device_service", timeout: 0})
	if err != nil {
		t.Fatalf("newSession() error = %v", err)
	}
	clients := mediaClients{}
	if m2 != nil {
		clients.media2 = m2
		clients.media2Available = true
	}
	if m1 != nil {
		clients.media1 = m1
		clients.media1Available = true
	}
	return &profileResolver{
		clients:   clients,
		session:   sess,
		cameraURL: mustURL(t, "http://cam.local/onvif/device_service"),
	}
}

// TestFallbackCallCountsPerOperation proves the fallback is applied
// INDEPENDENTLY per operation: only the operation whose Media2 call returned
// ActionNotSupported issues a single Media1 call; the Media2 operations that
// succeeded issue no Media1 call at all.
func TestFallbackCallCountsPerOperation(t *testing.T) {
	t.Parallel()
	m2 := &stubMedia2{snapshotErr: soap.ErrActionNotSupported}
	m1 := &stubMedia1{}
	r := newUnitResolver(t, m2, m1)

	profiles, err := r.resolveProfiles(context.Background())
	if err != nil {
		t.Fatalf("resolveProfiles() error = %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("profiles = %d, want 1", len(profiles))
	}
	p := profiles[0]

	// Only snapshot fell back.
	snap := opStatus(t, p, MediaOpSnapshotURI)
	if snap.Source != MediaServiceMedia1 || !snap.FellBack {
		t.Fatalf("snapshot = %+v, want media1 fell_back", snap)
	}
	// Media1 snapshot called exactly once; Media1 stream never called.
	if m1.snapshotCalls != 1 {
		t.Fatalf("media1 snapshot calls = %d, want 1", m1.snapshotCalls)
	}
	if m1.streamCalls != 0 {
		t.Fatalf("media1 stream calls = %d, want 0 (stream did not fall back)", m1.streamCalls)
	}
	// Stream remained Media2 (two calls: tcp and udp), no fallback.
	tcp := opStatus(t, p, MediaOpStreamTCP)
	udp := opStatus(t, p, MediaOpStreamUDP)
	if tcp.FellBack || udp.FellBack || tcp.Source != MediaServiceMedia2 || udp.Source != MediaServiceMedia2 {
		t.Fatalf("stream ops fell back unexpectedly: tcp=%+v udp=%+v", tcp, udp)
	}
	if m2.streamCalls != 2 {
		t.Fatalf("media2 stream calls = %d, want 2 (tcp+udp)", m2.streamCalls)
	}
}

// TestNoFallbackOnDisqualifierCallCounts proves a disqualifying Media2 error
// (NotAuthorized) never triggers a Media1 call for that operation, and the
// operation is reported with an accurate non-supported status.
func TestNoFallbackOnDisqualifierCallCounts(t *testing.T) {
	t.Parallel()
	m2 := &stubMedia2{snapshotErr: soap.ErrNotAuthorized}
	m1 := &stubMedia1{}
	r := newUnitResolver(t, m2, m1)

	profiles, err := r.resolveProfiles(context.Background())
	if err != nil {
		t.Fatalf("resolveProfiles() error = %v", err)
	}
	p := profiles[0]
	snap := opStatus(t, p, MediaOpSnapshotURI)
	if snap.FellBack || snap.Source == MediaServiceMedia1 {
		t.Fatalf("snapshot fell back on NotAuthorized: %+v", snap)
	}
	if snap.State == CapabilitySupported {
		t.Fatalf("snapshot reported supported despite NotAuthorized")
	}
	if m1.snapshotCalls != 0 {
		t.Fatalf("media1 snapshot calls = %d, want 0 (no fallback on NotAuthorized)", m1.snapshotCalls)
	}
	if p.Snapshot != nil {
		t.Fatalf("snapshot URI must not be present after a disqualified failure")
	}
}

// TestRootProfilesFatalOnDisqualifier proves a non-eligible root GetProfiles
// failure is fatal (a safe app error), never an empty success, and never a
// Media1 fallback.
func TestRootProfilesFatalOnDisqualifier(t *testing.T) {
	t.Parallel()
	m2 := &stubMedia2{profilesErr: soap.ErrNotAuthorized}
	m1 := &stubMedia1{}
	r := newUnitResolver(t, m2, m1)

	_, err := r.resolveProfiles(context.Background())
	if err == nil {
		t.Fatal("resolveProfiles() returned nil error on a disqualified root failure")
	}
	if CategoryOf(err) != CategoryAuthentication {
		t.Fatalf("category = %q, want authentication", CategoryOf(err))
	}
	if m1.profilesCalls != 0 {
		t.Fatalf("media1 profiles calls = %d, want 0", m1.profilesCalls)
	}
}

// TestRootProfilesFallbackOnActionNotSupported proves the root GetProfiles
// falls back to Media1 exactly once on ActionNotSupported and records the
// fallback provenance.
func TestRootProfilesFallbackOnActionNotSupported(t *testing.T) {
	t.Parallel()
	m2 := &stubMedia2{profilesErr: soap.ErrActionNotSupported}
	m1 := &stubMedia1{}
	r := newUnitResolver(t, m2, m1)

	profiles, err := r.resolveProfiles(context.Background())
	if err != nil {
		t.Fatalf("resolveProfiles() error = %v", err)
	}
	if len(profiles) != 1 || profiles[0].Source != MediaServiceMedia1 {
		t.Fatalf("profiles source = %+v, want one media1 profile", profiles)
	}
	if m1.profilesCalls != 1 {
		t.Fatalf("media1 profiles calls = %d, want 1", m1.profilesCalls)
	}
	prof := opStatus(t, profiles[0], MediaOpProfiles)
	if !prof.FellBack {
		t.Fatal("profiles status did not record fell_back after root ActionNotSupported")
	}
}
