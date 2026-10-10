package app

import (
	"context"
	"errors"

	"github.com/addodelgrossi/ovm/pkg/stream"
)

// LiveStreamOpener opens a live H.264 or H.265 stream. Production composition uses
// NewProductionLiveStreams; tests and the demo inject their own.
type LiveStreamOpener func(ctx context.Context, options stream.RTSPOptions) (stream.LiveSource, error)

// NewProductionLiveStreams opens RTSP sessions with the pkg/stream client.
func NewProductionLiveStreams() LiveStreamOpener {
	return func(ctx context.Context, options stream.RTSPOptions) (stream.LiveSource, error) {
		return stream.OpenRTSP(ctx, options)
	}
}

// OpenLiveStream resolves the selected profile's stream through the
// authenticated backend and opens it with the stored credentials. The URI and
// credentials stay inside the backend; only the decoded media leaves.
func (m *Manager) OpenLiveStream(ctx context.Context, selection StreamSelection) (stream.LiveSource, error) {
	if m.liveStreams == nil {
		return nil, newManagerError(CategoryInternal, "live streaming is not configured", nil)
	}
	uri, err := m.ResolveStreamURI(ctx, selection)
	if err != nil {
		return nil, err
	}
	device, resolveErr := m.resolveDevice(selection.DeviceID)
	if resolveErr != nil {
		return nil, resolveErr
	}

	options := stream.RTSPOptions{URL: uri, Transport: stream.TransportTCP}
	if selection.Transport == StreamTransportUDP {
		options.Transport = stream.TransportUDP
	}
	if device.CredentialReference != "" {
		credentials, release, credErr := m.loadCredentials(ctx, device)
		if credErr != nil {
			return nil, credErr
		}
		options.Username = credentials.username
		options.Password = credentials.password
		release()
	}
	source, openErr := m.liveStreams(ctx, options)
	options.Password = ""
	if openErr != nil {
		return nil, liveStreamError(openErr)
	}
	return source, nil
}

// liveStreamError maps typed stream failures onto safe manager errors.
func liveStreamError(err error) *ManagerError {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return managerErrorFromContext(err)
	case errors.Is(err, stream.ErrRTSPAuthentication):
		return newManagerError(CategoryAuthentication, "the camera rejected the stream credentials", err)
	case errors.Is(err, stream.ErrRTSPNetwork):
		return newManagerError(CategoryNetwork, "the camera stream could not be reached", err)
	case errors.Is(err, stream.ErrRTSPUnsupportedCodec):
		return newManagerError(CategoryProtocol, "the stream has no supported H.264 or H.265 video track", err)
	case errors.Is(err, stream.ErrRTSPNotFound):
		return newManagerError(CategoryProtocol, "the camera does not serve the selected stream", err)
	default:
		return newManagerError(CategoryProtocol, "the camera stream failed", err)
	}
}
