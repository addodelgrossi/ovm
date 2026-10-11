package app

import (
	"context"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
)

// badURIMedia2 returns structurally invalid or disallowed URIs so the tests can
// prove an invalid returned URI never enters the DTO and instead becomes that
// operation's safe non-supported status.
type badURIMedia2 struct {
	*stubMedia2
	streamURI   string
	snapshotURI string
}

func (b *badURIMedia2) GetStreamUri(ctx context.Context, p media2.StreamProtocol, token string, opts ...media2.RequestOptions) (string, error) {
	_, _ = b.stubMedia2.GetStreamUri(ctx, p, token, opts...)
	return b.streamURI, nil
}

func (b *badURIMedia2) GetSnapshotUri(ctx context.Context, token string, opts ...media2.RequestOptions) (string, error) {
	_, _ = b.stubMedia2.GetSnapshotUri(ctx, token, opts...)
	return b.snapshotURI, nil
}

// TestInvalidReturnedStreamURIStaysOutOfDTO proves a returned stream URI that
// fails validation (here a cross-host rtsp URI) is reported as the stream
// operation's unknown status and never populates the DTO stream fields.
func TestInvalidReturnedStreamURIStaysOutOfDTO(t *testing.T) {
	t.Parallel()
	m2 := &badURIMedia2{
		stubMedia2:  &stubMedia2{},
		streamURI:   "rtsp://attacker.invalid:554/stream", // cross-host
		snapshotURI: "http://cam.local/snap",
	}
	r := newUnitResolverIface(t, m2, nil)
	// The resolver's camera host is cam.local; the stream host differs.

	profiles, err := r.resolveProfiles(context.Background())
	if err != nil {
		t.Fatalf("resolveProfiles() error = %v", err)
	}
	p := profiles[0]
	if p.Stream != nil && (p.Stream.TCPURI != "" || p.Stream.UDPURI != "") {
		t.Fatalf("invalid stream URI leaked into DTO: %+v", p.Stream)
	}
	tcp := opStatus(t, p, MediaOpStreamTCP)
	if tcp.State == CapabilitySupported {
		t.Fatalf("stream_tcp reported supported for an invalid URI: %+v", tcp)
	}
}

// TestInvalidReturnedSnapshotURIStaysOutOfDTO proves a returned snapshot URI
// with a disallowed scheme is reported as the snapshot operation's unknown
// status and never populates the DTO.
func TestInvalidReturnedSnapshotURIStaysOutOfDTO(t *testing.T) {
	t.Parallel()
	m2 := &badURIMedia2{
		stubMedia2:  &stubMedia2{},
		streamURI:   "rtsp://cam.local:554/stream",
		snapshotURI: "ftp://cam.local/snap", // disallowed scheme
	}
	r := newUnitResolverIface(t, m2, nil)

	profiles, err := r.resolveProfiles(context.Background())
	if err != nil {
		t.Fatalf("resolveProfiles() error = %v", err)
	}
	p := profiles[0]
	if p.Snapshot != nil && p.Snapshot.URI != "" {
		t.Fatalf("invalid snapshot URI leaked into DTO: %+v", p.Snapshot)
	}
	snap := opStatus(t, p, MediaOpSnapshotURI)
	if snap.State == CapabilitySupported {
		t.Fatalf("snapshot reported supported for an invalid URI: %+v", snap)
	}
}
