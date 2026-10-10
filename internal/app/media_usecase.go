package app

import (
	"context"
	"time"
)

// MediaProfilesRequest parameterizes the media profile use case.
type MediaProfilesRequest struct {
	// Host is the manual camera endpoint (host, host:port, or full URL) that
	// the adapter canonicalizes. It is the Device service endpoint and the
	// same-origin anchor for every resolved operational endpoint.
	Host string
	// Credentials, when non-nil, authenticate the session ephemerally and are
	// never stored or returned.
	Credentials *Credentials
	// Timeout bounds the whole use case; non-positive uses the adapter default.
	Timeout time.Duration
}

// MediaProfilesUseCase retrieves the normalized, credential-free media profiles
// of a camera. It discovers the Device, Media2, and Media1 service endpoints,
// prefers Media2 per supported operation, and falls back to Media1 only when a
// Media2 operation fails the narrow ActionNotSupported predicate.
type MediaProfilesUseCase interface {
	Profiles(ctx context.Context, request MediaProfilesRequest) (MediaProfilesResult, error)
}

// SnapshotRequest parameterizes the snapshot retrieval use case.
type SnapshotRequest struct {
	// Host is the manual camera endpoint, the same-origin anchor.
	Host string
	// Credentials authenticate the session ephemerally.
	Credentials *Credentials
	// ProfileToken selects the profile whose snapshot to retrieve. A non-empty
	// explicit value must pass the ReferenceToken policy (valid UTF-8, 1–64
	// Unicode code points, and no controls) before any network work; XML-special
	// characters are preserved for wire escaping. When empty, the adapter
	// selects the lexicographically smallest non-empty profile token reported by
	// the preferred media service.
	ProfileToken string
	// OutputPath is the destination file path for the JPEG. Required.
	OutputPath string
	// Overwrite permits atomically replacing an existing destination. The
	// default is false, which publishes with an atomic no-clobber operation.
	Overwrite bool
	// Timeout bounds the whole use case; non-positive uses the adapter default.
	Timeout time.Duration
	// MaxBytes caps the downloaded JPEG size. Non-positive uses the default
	// 20 MiB cap.
	MaxBytes int64
}

// SnapshotUseCase resolves a snapshot URI (preferred Media2, else Media1) for a
// profile and retrieves the JPEG over an authenticated, same-origin HTTP GET,
// writing it atomically to disk and returning only credential-free metadata.
type SnapshotUseCase interface {
	Snapshot(ctx context.Context, request SnapshotRequest) (SnapshotMetadata, error)
}
