package app

import (
	"context"

	media1 "github.com/addodelgrossi/ovm/pkg/onvif/media"
	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
)

// media2Client and media1Client are narrow interfaces over exactly the wire
// operations the orchestration drives. They exist so pure unit tests can inject
// stubs that record call counts and return chosen typed errors without any
// network, SOAP, or fake HTTP server. The concrete *media2.Client and
// *media1.Client satisfy them directly (see the compile-time assertions below),
// so production wiring passes the real clients unchanged.
//
// Every method mirrors the real client signature including the variadic
// RequestOptions, because the session injects a fresh WS-Security header per
// call through that option.

type media2Client interface {
	GetProfiles(ctx context.Context, token string, types []media2.ConfigurationType, opts ...media2.RequestOptions) ([]media2.Profile, error)
	GetStreamUri(ctx context.Context, protocol media2.StreamProtocol, profileToken string, opts ...media2.RequestOptions) (string, error)
	GetSnapshotUri(ctx context.Context, profileToken string, opts ...media2.RequestOptions) (string, error)
	GetVideoEncoderConfigurations(ctx context.Context, configurationToken, profileToken string, opts ...media2.RequestOptions) ([]media2.VideoEncoderConfiguration, error)
	GetAudioEncoderConfigurations(ctx context.Context, configurationToken, profileToken string, opts ...media2.RequestOptions) ([]media2.AudioEncoderConfiguration, error)
}

type media1Client interface {
	GetProfiles(ctx context.Context, opts ...media1.RequestOptions) ([]media1.Profile, error)
	GetStreamUri(ctx context.Context, profileToken string, setup media1.StreamSetup, opts ...media1.RequestOptions) (media1.MediaURI, error)
	GetSnapshotUri(ctx context.Context, profileToken string, opts ...media1.RequestOptions) (media1.MediaURI, error)
	GetVideoSources(ctx context.Context, opts ...media1.RequestOptions) ([]media1.VideoSource, error)
	GetVideoEncoderConfigurations(ctx context.Context, opts ...media1.RequestOptions) ([]media1.VideoEncoderConfiguration, error)
	GetAudioEncoderConfigurations(ctx context.Context, opts ...media1.RequestOptions) ([]media1.AudioEncoderConfiguration, error)
}

// Compile-time assertions that the real wire clients satisfy the narrow
// interfaces. If a wire signature drifts, the build fails here rather than at a
// subtle call site.
var (
	_ media2Client = (*media2.Client)(nil)
	_ media1Client = (*media1.Client)(nil)
)
