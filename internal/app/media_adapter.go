package app

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/addodelgrossi/ovm/pkg/onvif/auth"
	"github.com/addodelgrossi/ovm/pkg/onvif/device"
	media1 "github.com/addodelgrossi/ovm/pkg/onvif/media"
	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
)

// defaultMediaTimeout bounds a whole media use case when the caller supplies no
// timeout.
const defaultMediaTimeout = 20 * time.Second

// The official ONVIF Media service WSDL namespaces used to classify GetServices
// entries. They are duplicated from the wire packages' Namespace constants on
// purpose: the classification lives at the application boundary and must match
// the exact strings the device advertises.
const (
	media2WSDLNamespace = "http://www.onvif.org/ver20/media/wsdl"
	media1WSDLNamespace = "http://www.onvif.org/ver10/media/wsdl"
)

// productionMedia is the production MediaProfilesUseCase and SnapshotUseCase. It
// shares the exact authenticated session with the Device inventory use case
// through the single *session built in session.go; it never duplicates auth
// negotiation, token generation, Digest/Basic policy, retries, or clock-offset
// logic, and it never constructs a second Digest transport.
type productionMedia struct {
	// httpClient is the base HTTP client. A nil value uses a fresh default
	// client per request.
	httpClient *http.Client
}

// NewProductionMediaProfiles returns the production MediaProfilesUseCase.
func NewProductionMediaProfiles() MediaProfilesUseCase { return &productionMedia{} }

// NewProductionSnapshot returns the production SnapshotUseCase.
func NewProductionSnapshot() SnapshotUseCase { return &productionMedia{} }

// mediaClients bundles the resolved, same-origin Media2 and Media1 clients and
// the per-service availability flags. A nil client means the service was not
// advertised at all. Both clients are constructed over the session's single
// soap.Client, so every Media SOAP call shares the same authenticated
// RoundTripper and cache.
type mediaClients struct {
	media2 media2Client
	media1 media1Client

	media2Available bool
	media1Available bool
}

// resolveServices drives Device GetServices (through the shared session) and
// resolves the operational Media2 and Media1 endpoints. Each advertised Media
// XAddr is validated same-origin with the camera BEFORE any credential or WSSE
// is applied, using its exact operational value (path and query preserved).
//
// The security contract is fail-closed and explicit: if a relevant Media
// service is ADVERTISED but its endpoint is unsafe, cross-origin, or a client
// cannot be constructed over it, that is a FATAL protocol/security error — it is
// never silently treated as "absent" so Media1 could be used in its place. A
// genuine absence (the Media2 service is simply not listed) is not an error and
// Media1 may then be used directly.
func (p *productionMedia) resolveServices(ctx context.Context, cameraURL *url.URL, seam *authDeviceSeam) (mediaClients, error) {
	services, err := seam.Services(ctx)
	if err != nil {
		return mediaClients{}, mapMediaError(err)
	}

	var out mediaClients
	// Collect the relevant Media1/Media2 entries. A duplicate same-namespace
	// advertisement is AMBIGUOUS: both candidates may pass same-origin checks,
	// but device response order would otherwise decide which path/query
	// receives authenticated operations. That is a fail-closed condition — a
	// deterministic sort cannot make an ambiguous credential scope safe — so a
	// duplicate namespace is rejected as a protocol error BEFORE any Media
	// operation runs and before any credential or WSSE is applied to either
	// endpoint. Rejection is independent of response order.
	relevant := make([]device.Service, 0, len(services))
	namespaceCount := map[string]int{}
	for _, svc := range services {
		if svc.Namespace == media2WSDLNamespace || svc.Namespace == media1WSDLNamespace {
			relevant = append(relevant, svc)
			namespaceCount[svc.Namespace]++
		}
	}
	for _, namespace := range []string{media1WSDLNamespace, media2WSDLNamespace} {
		if namespaceCount[namespace] > 1 {
			return mediaClients{}, securityError("the camera advertises duplicate media services")
		}
	}
	// Deterministic iteration: sort the (now at most one per namespace) entries
	// by namespace so the construction order is stable for review and tests.
	sort.SliceStable(relevant, func(i, j int) bool { return relevant[i].Namespace < relevant[j].Namespace })

	for _, svc := range relevant {
		operational := svc.OperationalXAddr()
		if operational == "" {
			// Advertised but with no usable endpoint: fail closed.
			return mediaClients{}, securityError("an advertised media service has no usable endpoint")
		}
		// Same-origin validation on the EXACT operational endpoint BEFORE any
		// authenticated transport is built. The raw query is preserved on the
		// returned URL for request construction but must never be surfaced.
		endpoint, verr := validateSameOriginEndpoint(cameraURL, operational)
		if verr != nil {
			// A relevant service that is present but unsafe/cross-origin is a
			// fatal security error, not a silent downgrade to Media1.
			return mediaClients{}, securityError("an advertised media service endpoint is not same-origin with the camera")
		}
		switch svc.Namespace {
		case media2WSDLNamespace:
			c, e := media2.NewClient(seam.session.soapClient, endpoint.String())
			if e != nil {
				return mediaClients{}, securityError("the media2 client could not be constructed for the advertised endpoint")
			}
			out.media2 = c
			out.media2Available = true
		case media1WSDLNamespace:
			c, e := media1.NewClient(seam.session.soapClient, endpoint.String())
			if e != nil {
				return mediaClients{}, securityError("the media1 client could not be constructed for the advertised endpoint")
			}
			out.media1 = c
			out.media1Available = true
		}
	}
	return out, nil
}

// Profiles discovers services, resolves profiles preferring Media2 with the
// narrow Media1 fallback, and returns deterministically-sorted, credential-free
// DTOs. Every call runs through the one shared session. Explicit parent
// cancellation returns the assembled partial result with CategoryCancelled.
func (p *productionMedia) Profiles(ctx context.Context, request MediaProfilesRequest) (MediaProfilesResult, error) {
	cameraURL, seam, cleanupTimeout, err := p.prepare(request.Host, request.Timeout, request.Credentials)
	if err != nil {
		return MediaProfilesResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, cleanupTimeout)
	defer cancel()

	// Negotiate the clock once over the always-same-origin Device service.
	seam.negotiate(ctx)

	clients, err := p.resolveServices(ctx, cameraURL, seam)
	if err != nil {
		return MediaProfilesResult{}, err
	}
	if !clients.media2Available && !clients.media1Available {
		return MediaProfilesResult{}, newError(CategoryProtocol, "the camera advertises no usable media service", nil)
	}

	resolver := &profileResolver{clients: clients, session: seam.session, cameraURL: cameraURL}
	profiles, perr := resolver.resolveProfiles(ctx)
	if perr != nil {
		// A fatal root error (for example a non-eligible GetProfiles failure).
		return MediaProfilesResult{}, perr
	}

	result := MediaProfilesResult{
		Profiles:        profiles,
		Media2Available: clients.media2Available,
		Media1Available: clients.media1Available,
	}
	// Explicit cancellation surfaces as a partial result plus a cancelled
	// category: the profiles assembled so far are returned.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, mapMediaError(ctxErr)
	}
	return result, nil
}

// prepare canonicalizes the host, builds the single authenticated session used
// for GetServices, clock negotiation, and every Media/snapshot call, wraps it in
// the typed Device adapter, and returns the parsed camera URL and effective
// timeout. Exactly one Digest client is constructed here per top-level use case.
func (p *productionMedia) prepare(host string, timeout time.Duration, creds *Credentials) (*url.URL, *authDeviceSeam, time.Duration, error) {
	operational, err := canonicalizeOperationalDeviceEndpoint(host)
	if err != nil {
		return nil, nil, 0, usageError("the endpoint is not valid")
	}
	endpointURL := operational.String()
	cameraURL, err := parseSafeOperationalURL(endpointURL)
	if err != nil {
		return nil, nil, 0, usageError("the endpoint is not valid")
	}

	if timeout <= 0 {
		timeout = defaultMediaTimeout
	}

	sess, err := newSession(sessionConfig{
		endpointURL:    endpointURL,
		creds:          creds,
		timeout:        timeout,
		baseHTTPClient: p.httpClient,
		sampler:        auth.ClockSampler{Attempts: negotiationAttempts},
		clock:          time.Now,
	})
	if err != nil {
		return nil, nil, 0, err
	}
	deviceClient, err := device.NewClient(sess.soapClient, endpointURL)
	if err != nil {
		return nil, nil, 0, newError(CategoryInternal, "the device client could not be constructed", err)
	}
	sess.setClockReader(deviceReadClock(deviceClient, nil))
	seam := &authDeviceSeam{session: sess, client: deviceClient}
	return cameraURL, seam, timeout, nil
}

// profileResolver resolves profiles and per-profile media values, applying the
// Media2-preferred-with-narrow-Media1-fallback rule independently per
// operation. It holds the shared session so every Media call reuses the one
// authenticated transport, the shared clock offset, and the single resample
// gate. It also caches the lazy Media1 whole-list reads (profiles, video
// sources, video/audio encoder configurations) so a fallback never re-fetches
// an unfiltered list more than once.
type profileResolver struct {
	clients   mediaClients
	session   *session
	cameraURL *url.URL

	// profilesSource records which service answered the root GetProfiles. When
	// it is Media2 and an individual encoder/source operation falls back to
	// Media1, the fallback must resolve the SAME profile token through the
	// cached Media1 GetProfiles and use that Media1 profile's own association
	// tokens — Media1 and Media2 are distinct service models whose per-profile
	// configuration references are not interchangeable.
	profilesSource MediaSourceService

	// Lazy Media1 caches. Each is fetched at most once; the error is cached too
	// so a repeated miss is not retried per profile.
	m1Profiles     []media1.Profile
	m1ProfilesErr  error
	m1ProfilesDone bool
	m1VideoSrc     []media1.VideoSource
	m1VideoSrcErr  error
	m1VideoSrcDone bool
	m1VideoEnc     []media1.VideoEncoderConfiguration
	m1VideoEncErr  error
	m1VideoEncDone bool
	m1AudioEnc     []media1.AudioEncoderConfiguration
	m1AudioEncErr  error
	m1AudioEncDone bool
}

// resolveProfiles fetches the profile list (Media2 preferred, Media1 fallback
// only on the narrow predicate), then enriches each profile and sorts by token.
// A non-eligible root GetProfiles failure is fatal and returns a safe app error
// rather than an empty success.
func (r *profileResolver) resolveProfiles(ctx context.Context) ([]MediaProfileDTO, error) {
	profiles, source, fellBack, err := r.fetchProfiles(ctx)
	if err != nil {
		return nil, mapMediaError(err)
	}
	r.profilesSource = source

	profilesStatus := MediaOperationStatus{
		Operation: MediaOpProfiles,
		State:     CapabilitySupported,
		Source:    source,
		FellBack:  fellBack,
	}

	dtos := make([]MediaProfileDTO, 0, len(profiles))
	for _, prof := range profiles {
		if ctx.Err() != nil {
			// Honor explicit cancellation: stop enriching further profiles and
			// return what has been assembled.
			break
		}
		dto := MediaProfileDTO{
			Token:                   prof.token,
			Name:                    prof.name,
			Fixed:                   prof.fixed,
			HasFixed:                prof.hasFixed,
			Source:                  source,
			VideoSourceConfigToken:  prof.videoSourceConfigToken,
			VideoSourceToken:        prof.videoSourceToken,
			VideoEncoderConfigToken: prof.videoEncoderConfigToken,
		}
		statuses := map[MediaOperationName]MediaOperationStatus{
			MediaOpProfiles: profilesStatus,
		}
		r.enrich(ctx, prof, &dto, statuses)
		dto.Operations = orderedStatuses(statuses)
		dtos = append(dtos, dto)
	}

	sort.SliceStable(dtos, func(i, j int) bool { return dtos[i].Token < dtos[j].Token })
	return dtos, nil
}

// orderedStatuses emits the per-operation statuses in the fixed
// mediaOperationOrder, skipping any operation that did not run.
func orderedStatuses(statuses map[MediaOperationName]MediaOperationStatus) []MediaOperationStatus {
	out := make([]MediaOperationStatus, 0, len(mediaOperationOrder))
	for _, op := range mediaOperationOrder {
		if status, ok := statuses[op]; ok {
			out = append(out, status)
		}
	}
	return out
}

// normProfile is the service-agnostic normalized profile identity used before
// enrichment, carrying the typed configuration associations needed to match
// configurations without ever guessing by slice order.
type normProfile struct {
	token                   string
	name                    string
	fixed                   bool
	hasFixed                bool
	videoSourceConfigToken  string
	videoSourceToken        string
	videoEncoderConfigToken string
	audioEncoderConfigToken string
}

// enrich fills the per-profile media values and records each operation's
// independent status. A failure in one operation never affects another: each
// records its own capability state, provenance, and fallback flag.
func (r *profileResolver) enrich(ctx context.Context, prof normProfile, dto *MediaProfileDTO, statuses map[MediaOperationName]MediaOperationStatus) {
	// video_source: Media1 only (Media2 has no GetVideoSources). It resolves
	// the physical source token's details; the token itself already came from
	// the profile association.
	statuses[MediaOpVideoSource] = r.resolveVideoSource(ctx, prof)

	// video_encoder: Media2 filtered by profile token, narrow fallback to the
	// Media1 all-config read matched through the typed association.
	video, videoStatus := r.resolveVideoEncoder(ctx, prof)
	statuses[MediaOpVideoEncoder] = videoStatus
	if video != nil {
		dto.Video = video
	}

	// audio_encoder: same Media2-then-narrow-Media1 shape.
	audio, audioStatus := r.resolveAudioEncoder(ctx, prof)
	statuses[MediaOpAudioEncoder] = audioStatus
	if audio != nil {
		dto.Audio = audio
	}

	// stream_tcp and stream_udp are independent operations.
	tcpURI, tcpStatus := r.resolveStream(ctx, prof.token, true)
	statuses[MediaOpStreamTCP] = tcpStatus
	udpURI, udpStatus := r.resolveStream(ctx, prof.token, false)
	statuses[MediaOpStreamUDP] = udpStatus
	if tcpURI != "" || udpURI != "" {
		dto.Stream = &StreamURIDTO{TCPURI: tcpURI, UDPURI: udpURI}
	}

	// snapshot_uri.
	snapURI, snapStatus := r.resolveSnapshotPresentation(ctx, prof.token)
	statuses[MediaOpSnapshotURI] = snapStatus
	if snapURI != "" {
		dto.Snapshot = &SnapshotURIDTO{URI: snapURI}
	}
}

// securityError builds a CategoryProtocol error for an advertised-but-unsafe
// media service. It is distinct from a usage error because the device response
// is at fault, not the caller's input.
func securityError(message string) error {
	return newError(CategoryProtocol, message, nil)
}

// stripURIForPresentation removes the userinfo and query from a parsed URL and
// returns the safe presentation string. Path is preserved. This is used after
// a URI has already passed scheme/host/same-origin validation.
func stripURIForPresentation(parsed *url.URL) string {
	clone := *parsed
	clone.User = nil
	clone.RawQuery = ""
	clone.ForceQuery = false
	clone.Fragment = ""
	clone.RawFragment = ""
	return clone.String()
}

// stripStreamURIForPresentation removes userinfo and fragment but keeps the
// query: many cameras select the channel or substream with query parameters
// (for example "?channel=1&subtype=0"), so a query-free RTSP URI would not
// play. The query is shown and copied exactly as the camera returned it.
func stripStreamURIForPresentation(parsed *url.URL) string {
	clone := *parsed
	clone.User = nil
	clone.ForceQuery = false
	clone.Fragment = ""
	clone.RawFragment = ""
	return clone.String()
}

// hostnameEqual reports whether two hostnames are equal case-insensitively.
func hostnameEqual(a, b string) bool { return strings.EqualFold(a, b) }
