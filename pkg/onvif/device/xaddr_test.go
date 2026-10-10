package device_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/device"
)

// servicesPayload builds a GetServicesResponse with a single Service whose
// XAddr is the supplied value. The namespace is a stable synthetic value; the
// test is only interested in the XAddr handling.
func servicesPayload(xaddr string) string {
	return `<tds:GetServicesResponse` + tdsNS + ttNS + `>` +
		`<tds:Service><tds:Namespace>urn:synthetic:service</tds:Namespace>` +
		`<tds:XAddr>` + xaddr + `</tds:XAddr>` +
		`<tds:Version><tt:Major>1</tt:Major><tt:Minor>0</tt:Minor></tds:Version>` +
		`</tds:Service></tds:GetServicesResponse>`
}

// TestServiceOperationalXAddrPreservesQueryButRedactsPublic proves that an
// advertised service endpoint carrying a path and a query (which may itself be
// a credential) is preserved verbatim through OperationalXAddr for request
// construction, while the exported XAddr field is query-redacted for safe
// presentation. The synthetic query marker below is NOT a real secret; it is a
// recognizable token used only to prove it never leaks into the public value,
// JSON, or fmt output.
func TestServiceOperationalXAddrPreservesQueryButRedactsPublic(t *testing.T) {
	t.Parallel()
	const queryMarker = "synthetic-query-marker" // not a credential; a leak probe
	raw := "http://device.invalid/onvif/media_service?access=" + queryMarker

	client, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK, soap12Envelope(servicesPayload(raw)))
	defer closeFn()

	svcs, err := client.GetServices(context.Background())
	if err != nil {
		t.Fatalf("GetServices() error = %v", err)
	}
	if len(svcs) != 1 {
		t.Fatalf("services = %d, want 1", len(svcs))
	}
	svc := svcs[0]

	// Operational value preserves the full path and the query verbatim.
	if svc.OperationalXAddr() != raw {
		t.Fatalf("OperationalXAddr() = %q, want %q", svc.OperationalXAddr(), raw)
	}
	// Public XAddr preserves the path but redacts the query.
	if strings.Contains(svc.XAddr, queryMarker) {
		t.Fatalf("public XAddr leaked the query marker: %q", svc.XAddr)
	}
	if !strings.Contains(svc.XAddr, "/onvif/media_service") {
		t.Fatalf("public XAddr dropped the required path: %q", svc.XAddr)
	}

	// JSON serialization must never expose the operational value or query.
	encoded, err := json.Marshal(svc)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(encoded), queryMarker) {
		t.Fatalf("JSON leaked the query marker: %s", encoded)
	}

	// fmt rendering (including the reflective %#v and %+v verbs) must never
	// expose the operational value, because Service defines String and GoString
	// to render only redacted fields.
	formatted := fmt.Sprintf("%v %+v %s %#v", svc, svc, svc, svc)
	if strings.Contains(formatted, queryMarker) {
		t.Fatalf("fmt output leaked the query marker: %s", formatted)
	}
}

// TestServiceOperationalXAddrRejectsUnsafe proves the decoder fails closed on
// unsafe advertised endpoints rather than retaining them: userinfo, fragments,
// and unsupported schemes are each rejected with ErrInvalidField, and the error
// text never echoes the offending value. (Control characters embedded in the
// XML body are rejected even earlier by the SOAP XML parser; the operational
// validator's own control-char guard is covered by the internal unit test.)
func TestServiceOperationalXAddrRejectsUnsafe(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		xaddr string
	}{
		{"userinfo", "http://user:pass@device.invalid/onvif/media"},
		{"fragment", "http://device.invalid/onvif/media#frag"},
		{"unsupported scheme", "ftp://device.invalid/onvif/media"},
		{"relative", "/onvif/media"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK, soap12Envelope(servicesPayload(tc.xaddr)))
			defer closeFn()

			_, err := client.GetServices(context.Background())
			if err == nil {
				t.Fatalf("GetServices() accepted unsafe XAddr %q, want rejection", tc.xaddr)
			}
			if !errors.Is(err, device.ErrInvalidField) {
				t.Fatalf("error = %v, want ErrInvalidField", err)
			}
			// The error must not echo the offending value.
			if strings.Contains(err.Error(), "device.invalid") || strings.Contains(err.Error(), "user:pass") {
				t.Fatalf("error leaked the offending XAddr: %v", err)
			}
		})
	}
}
