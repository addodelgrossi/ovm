package app

import (
	"context"
	"testing"

	media1 "github.com/addodelgrossi/ovm/pkg/onvif/media"
	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
)

// TestMedia2StreamTransportValues proves the orchestration requests the exact
// Media2 transport protocol per transport: TCP uses StreamRTSP and UDP uses
// StreamRtspUnicast. The values are captured at the injected client seam.
func TestMedia2StreamTransportValues(t *testing.T) {
	t.Parallel()
	m2 := &stubMedia2{}
	r := newUnitResolver(t, m2, &stubMedia1{})

	if _, err := r.resolveProfiles(context.Background()); err != nil {
		t.Fatalf("resolveProfiles() error = %v", err)
	}
	if len(m2.streamProtocols) != 2 {
		t.Fatalf("stream protocol calls = %d, want 2 (tcp, udp)", len(m2.streamProtocols))
	}
	if m2.streamProtocols[0] != media2.StreamRTSP {
		t.Fatalf("TCP protocol = %q, want StreamRTSP", m2.streamProtocols[0])
	}
	if m2.streamProtocols[1] != media2.StreamRtspUnicast {
		t.Fatalf("UDP protocol = %q, want StreamRtspUnicast", m2.streamProtocols[1])
	}
}

// TestMedia1StreamTransportValues proves that when Media2 is absent, the Media1
// stream setup uses TransportRTSP for TCP and TransportUDP for UDP, both with
// RTP unicast.
func TestMedia1StreamTransportValues(t *testing.T) {
	t.Parallel()
	m1 := &stubMedia1{}
	r := newUnitResolver(t, nil, m1)

	if _, err := r.resolveProfiles(context.Background()); err != nil {
		t.Fatalf("resolveProfiles() error = %v", err)
	}
	if len(m1.streamSetups) != 2 {
		t.Fatalf("stream setup calls = %d, want 2 (tcp, udp)", len(m1.streamSetups))
	}
	tcp := m1.streamSetups[0]
	if tcp.Stream != media1.StreamRTPUnicast || tcp.Protocol != media1.TransportRTSP {
		t.Fatalf("TCP setup = %+v, want RTP-Unicast/TransportRTSP", tcp)
	}
	udp := m1.streamSetups[1]
	if udp.Stream != media1.StreamRTPUnicast || udp.Protocol != media1.TransportUDP {
		t.Fatalf("UDP setup = %+v, want RTP-Unicast/TransportUDP", udp)
	}
}
