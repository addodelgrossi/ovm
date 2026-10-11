package app

import (
	"context"
	"net/url"
)

// StreamTransport is the closed set of transports the desktop may request
// when resolving a stream for an external player.
type StreamTransport string

const (
	// StreamTransportTCP requests RTSP interleaved over TCP.
	StreamTransportTCP StreamTransport = "tcp"
	// StreamTransportUDP requests RTP over UDP.
	StreamTransportUDP StreamTransport = "udp"
)

// StreamSelection identifies a stored device, an ONVIF profile, and one of the
// supported transports. It deliberately contains no URL or credential field:
// callers cannot ask the Manager to launch or return an arbitrary URI.
type StreamSelection struct {
	DeviceID     string
	ProfileToken string
	Transport    StreamTransport
}

// ResolveStreamURI re-resolves media profiles through the authenticated
// backend and returns the selected credential-free presentation URI, with the
// camera's query preserved, for immediate trusted backend use. The URI must never be returned from a
// Wails response; the desktop host consumes it directly as a process argument.
func (m *Manager) ResolveStreamURI(ctx context.Context, selection StreamSelection) (string, error) {
	if err := m.ensureOpen(); err != nil {
		return "", err
	}
	if err := ValidateSnapshotProfileToken(selection.ProfileToken); err != nil {
		return "", managerErrorFromApp(err)
	}
	if selection.Transport != StreamTransportTCP && selection.Transport != StreamTransportUDP {
		return "", newManagerError(CategoryUsage, "the stream transport is not supported", nil)
	}

	device, resolveErr := m.resolveDevice(selection.DeviceID)
	if resolveErr != nil {
		return "", resolveErr
	}
	profiles, err := m.Profiles(ctx, device.ID)
	if err != nil {
		return "", err
	}

	var matched *MediaProfileDTO
	for index := range profiles.Profiles {
		if profiles.Profiles[index].Token != selection.ProfileToken {
			continue
		}
		if matched != nil {
			return "", newManagerError(CategoryProtocol, "the camera returned ambiguous media profiles", nil)
		}
		matched = &profiles.Profiles[index]
	}
	if matched == nil {
		return "", newManagerError(CategoryUsage, "the media profile was not found", nil)
	}

	operation := MediaOpStreamUDP
	if selection.Transport == StreamTransportTCP {
		operation = MediaOpStreamTCP
	}
	if !profileOperationSupported(*matched, operation) || matched.Stream == nil {
		return "", newManagerError(CategoryProtocol, "the requested stream transport is unavailable", nil)
	}

	streamURI := matched.Stream.UDPURI
	if selection.Transport == StreamTransportTCP {
		streamURI = matched.Stream.TCPURI
	}
	if !safeResolvedStreamURI(device.Endpoint, streamURI) {
		return "", newManagerError(CategoryProtocol, "the resolved stream URI is not safe", nil)
	}
	return streamURI, nil
}

func profileOperationSupported(profile MediaProfileDTO, operation MediaOperationName) bool {
	for _, status := range profile.Operations {
		if status.Operation == operation {
			return status.State == CapabilitySupported
		}
	}
	return false
}

// safeResolvedStreamURI is a final fail-closed check at the Manager boundary.
// The media adapter already performs this validation while building its DTO;
// repeating the critical properties here ensures an external process can only
// receive an absolute RTSP(S) URI for the selected camera, with no userinfo,
// fragment, or encoded control character. The query is allowed because
// cameras use it to select channels and substreams.
func safeResolvedStreamURI(deviceEndpoint, streamURI string) bool {
	if streamURI == "" || containsControl(streamURI) {
		return false
	}
	camera, err := url.Parse(deviceEndpoint)
	if err != nil || camera.Hostname() == "" {
		return false
	}
	parsed, err := url.Parse(streamURI)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" {
		return false
	}
	if parsed.Scheme != "rtsp" && parsed.Scheme != "rtsps" {
		return false
	}
	if parsed.User != nil || parsed.Fragment != "" || parsed.RawFragment != "" {
		return false
	}
	if !hostnameEqual(parsed.Hostname(), camera.Hostname()) || urlHasDecodedControl(parsed) {
		return false
	}
	return true
}
