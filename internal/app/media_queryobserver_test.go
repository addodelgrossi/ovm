package app

import (
	"net/http"
	"strings"
)

// queryObserver wraps a fakedevice handler and records whether any request to
// the device-service path carried the expected operational query marker, then
// delegates to the real handler. It is used to prove the operational device
// endpoint's query is actually sent on the wire.
type queryObserver struct {
	inner  http.Handler
	marker string
	saw    *bool
}

func (o *queryObserver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.RawQuery, o.marker) {
		*o.saw = true
	}
	o.inner.ServeHTTP(w, r)
}

// requiringQueryObserver wraps a fakedevice handler and REQUIRES that every POST
// (SOAP) to one of the guarded Media service paths carries the exact query
// marker. A request to a guarded path whose query does not contain the marker
// is rejected with 400 BEFORE reaching the inner handler, so a client that
// dropped the operational query cannot succeed. This turns the previously weak
// "profiles resolved, therefore the query was sent" inference into a hard
// contract: resolution is only possible if the marker reached the wire. It also
// records that the marker was observed. The marker is a synthetic, non-secret
// leak probe, never a credential.
type requiringQueryObserver struct {
	inner       http.Handler
	marker      string
	guardedPath []string
	sawMarker   *bool
}

func (o *requiringQueryObserver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	guarded := false
	for _, p := range o.guardedPath {
		if r.URL.Path == p {
			guarded = true
			break
		}
	}
	if guarded && r.Method == http.MethodPost {
		if !strings.Contains(r.URL.RawQuery, o.marker) {
			http.Error(w, "missing required operational query", http.StatusBadRequest)
			return
		}
		*o.sawMarker = true
	}
	o.inner.ServeHTTP(w, r)
}

// requiringSnapshotObserver REQUIRES that the authenticated snapshot GET carries
// BOTH a configured snapshot query marker and the generated profile parameter.
// A snapshot GET to the guarded path missing either is rejected before reaching
// the inner handler, so a client that dropped the operational snapshot query
// cannot retrieve the image. It records that both markers reached the wire. The
// markers are synthetic non-secret probes, never credentials.
type requiringSnapshotObserver struct {
	inner           http.Handler
	snapshotPath    string
	configuredQuery string // e.g. "sig=snapshot-marker"
	profileParam    string // e.g. "profile"
	sawBoth         *bool
}

func (o *requiringSnapshotObserver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == o.snapshotPath {
		rawQuery := r.URL.RawQuery
		hasConfigured := strings.Contains(rawQuery, o.configuredQuery)
		hasProfile := r.URL.Query().Get(o.profileParam) != ""
		if !hasConfigured || !hasProfile {
			http.Error(w, "missing required snapshot query", http.StatusBadRequest)
			return
		}
		*o.sawBoth = true
	}
	o.inner.ServeHTTP(w, r)
}
