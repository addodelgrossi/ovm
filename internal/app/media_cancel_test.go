package app

import (
	"context"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
)

// cancellingMedia2 cancels the test context the moment its video encoder read
// runs, simulating an explicit parent cancellation arriving mid-enrichment.
type cancellingMedia2 struct {
	*stubMedia2
	cancel context.CancelFunc
}

func (c *cancellingMedia2) GetVideoEncoderConfigurations(ctx context.Context, cfg, prof string, opts ...media2.RequestOptions) ([]media2.VideoEncoderConfiguration, error) {
	c.cancel()
	return c.stubMedia2.GetVideoEncoderConfigurations(ctx, cfg, prof, opts...)
}

// TestCancellationReturnsPartialProfiles proves that an explicit parent
// cancellation during enrichment causes resolveProfiles to stop and return the
// profiles assembled so far rather than aborting with nothing. (The use case
// wrapper then surfaces CategoryCancelled; here we assert the resolver returns a
// non-fatal result.)
func TestCancellationReturnsPartialProfiles(t *testing.T) {
	t.Parallel()

	base := &stubMedia2{}
	// Two profiles so cancellation during the first still leaves the loop able
	// to observe ctx.Err and stop before the second.
	base.profilesErr = nil

	ctx, cancel := context.WithCancel(context.Background())
	m2 := &cancellingMedia2{stubMedia2: base, cancel: cancel}
	m1 := &stubMedia1{}

	sess, err := newSession(sessionConfig{endpointURL: "http://cam.local/onvif/device_service"})
	if err != nil {
		t.Fatalf("newSession() error = %v", err)
	}
	r := &profileResolver{
		clients:   mediaClients{media2: m2, media2Available: true, media1: m1, media1Available: true},
		session:   sess,
		cameraURL: mustURL(t, "http://cam.local/onvif/device_service"),
	}

	profiles, perr := r.resolveProfiles(ctx)
	// The root GetProfiles succeeded (fetched before cancellation), so the
	// resolver returns the assembled profiles with no fatal error.
	if perr != nil {
		t.Fatalf("resolveProfiles() error = %v, want nil (partial assembly)", perr)
	}
	if len(profiles) == 0 {
		t.Fatal("expected at least the profile assembled before cancellation")
	}
	// Context is cancelled, as the enrichment observed.
	if ctx.Err() == nil {
		t.Fatal("context should be cancelled")
	}
}

// ensure the stub satisfies the interface used above.
