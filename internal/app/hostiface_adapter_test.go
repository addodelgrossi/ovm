package app

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/discovery"
)

// fakeHostInterfaceLister is a deterministic hostInterfaceLister double. It
// records the request it received and returns a fixed assessment or error.
type fakeHostInterfaceLister struct {
	hosts   []discovery.HostInterface
	err     error
	gotReq  HostInterfaceRequest
	callCnt int
}

func (f *fakeHostInterfaceLister) ListHostInterfaces(_ context.Context, request HostInterfaceRequest) ([]discovery.HostInterface, error) {
	f.callCnt++
	f.gotReq = request
	return f.hosts, f.err
}

// TestHostInterfacesNormalizesAssessmentDeterministically asserts that the
// adapter maps every assessed interface into the DTO with the explicit usable
// flag, copied IPv4 strings, and the stable reason code, preserving the
// discovery package's deterministic order.
func TestHostInterfacesNormalizesAssessmentDeterministically(t *testing.T) {
	lister := &fakeHostInterfaceLister{
		hosts: []discovery.HostInterface{
			{Index: 1, Name: "lo0", Loopback: true, Usable: false, Reason: discovery.ReasonLoopbackExcluded},
			{Index: 2, Name: "en0", Usable: true, IPv4: []string{"192.168.1.10", "192.168.1.20"}, Reason: discovery.ReasonUsable},
			{Index: 3, Name: "tun0", Usable: false, Reason: discovery.ReasonPointToPoint},
			{Index: 4, Name: "flaky0", Usable: false, Reason: discovery.ReasonAddressUnavailable},
		},
	}
	uc := &productionHostInterfaces{lister: lister}

	result, err := uc.HostInterfaces(context.Background(), HostInterfaceRequest{})
	if err != nil {
		t.Fatalf("HostInterfaces: %v", err)
	}
	if len(result.Interfaces) != 4 {
		t.Fatalf("got %d interfaces, want 4", len(result.Interfaces))
	}

	wantReasons := []HostInterfaceReason{
		HostInterfaceLoopbackExcluded,
		HostInterfaceUsable,
		HostInterfacePointToPoint,
		HostInterfaceAddressUnavailable,
	}
	for i, want := range wantReasons {
		if got := result.Interfaces[i].Reason; got != want {
			t.Errorf("interface %d reason = %q, want %q", i, got, want)
		}
	}

	en0 := result.Interfaces[1]
	if !en0.Usable {
		t.Errorf("en0 usable = false, want true")
	}
	if want := []string{"192.168.1.10", "192.168.1.20"}; !slices.Equal(en0.IPv4, want) {
		t.Errorf("en0 IPv4 = %v, want %v", en0.IPv4, want)
	}

	// The DTO IPv4 slice must be an independent copy, not an alias of the
	// discovery assessment's slice.
	en0.IPv4[0] = "mutated"
	if lister.hosts[1].IPv4[0] != "192.168.1.10" {
		t.Error("mutating the DTO IPv4 slice mutated the source: slice is aliased")
	}

	for _, name := range []string{"lo0", "tun0", "flaky0"} {
		for _, dto := range result.Interfaces {
			if dto.Name == name && dto.Usable {
				t.Errorf("%s usable = true, want false", name)
			}
			if dto.Name == name && len(dto.IPv4) != 0 {
				t.Errorf("%s IPv4 = %v, want empty", name, dto.IPv4)
			}
		}
	}
}

// TestHostInterfacesRequestForwardsIncludeLoopback asserts the request's
// loopback opt-in is forwarded verbatim to the lister seam.
func TestHostInterfacesRequestForwardsIncludeLoopback(t *testing.T) {
	lister := &fakeHostInterfaceLister{}
	uc := &productionHostInterfaces{lister: lister}
	if _, err := uc.HostInterfaces(context.Background(), HostInterfaceRequest{IncludeLoopback: true}); err != nil {
		t.Fatalf("HostInterfaces: %v", err)
	}
	if !lister.gotReq.IncludeLoopback {
		t.Error("IncludeLoopback was not forwarded to the lister")
	}
}

// TestHostInterfacesPreCancelledContextShortCircuits asserts that a cancelled
// context yields a CategoryCancelled error before the lister is called.
func TestHostInterfacesPreCancelledContextShortCircuits(t *testing.T) {
	lister := &fakeHostInterfaceLister{}
	uc := &productionHostInterfaces{lister: lister}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := uc.HostInterfaces(ctx, HostInterfaceRequest{})
	if CategoryOf(err) != CategoryCancelled {
		t.Fatalf("category = %q, want %q", CategoryOf(err), CategoryCancelled)
	}
	if lister.callCnt != 0 {
		t.Errorf("lister called %d times, want 0 on a pre-cancelled context", lister.callCnt)
	}
}

// TestHostInterfacesErrorCategoryMapping asserts the cancellation/network
// category split and that no raw operating-system error reaches the user-safe
// message.
func TestHostInterfacesErrorCategoryMapping(t *testing.T) {
	rawOSErr := errors.New("ioctl SIOCGIFCONF: raw os failure that must not leak")
	cases := []struct {
		name     string
		err      error
		category ErrorCategory
	}{
		{"cancelled", context.Canceled, CategoryCancelled},
		{"deadline", context.DeadlineExceeded, CategoryNetwork},
		{"enumerate", rawOSErr, CategoryNetwork},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lister := &fakeHostInterfaceLister{err: tc.err}
			uc := &productionHostInterfaces{lister: lister}
			_, err := uc.HostInterfaces(context.Background(), HostInterfaceRequest{})
			if CategoryOf(err) != tc.category {
				t.Fatalf("category = %q, want %q", CategoryOf(err), tc.category)
			}
			if err.Error() == rawOSErr.Error() {
				t.Fatalf("raw OS error leaked into the user-safe message: %q", err.Error())
			}
			// The wrapped cause is retained for errors.Is but not rendered.
			if !errors.Is(err, tc.err) {
				t.Errorf("wrapped cause not retained for errors.Is")
			}
		})
	}
}

// TestNewProductionHostInterfacesUsesSystemLister asserts the production
// constructor wires a non-nil lister so a bare construction is usable.
func TestNewProductionHostInterfacesUsesSystemLister(t *testing.T) {
	uc := NewProductionHostInterfaces()
	if uc == nil {
		t.Fatal("NewProductionHostInterfaces returned nil")
	}
	// A real enumeration must not error on a normal host; interfaces may vary,
	// so only the error contract is asserted here.
	if _, err := uc.HostInterfaces(context.Background(), HostInterfaceRequest{}); err != nil {
		t.Fatalf("production HostInterfaces errored on a live host: %v", err)
	}
}
