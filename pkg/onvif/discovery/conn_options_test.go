package discovery

import (
	"errors"
	"net"
	"testing"
)

type recordingMulticastConfigurer struct {
	iface    *net.Interface
	ttl      int
	ifaceErr error
	ttlErr   error
}

func (r *recordingMulticastConfigurer) SetMulticastInterface(iface *net.Interface) error {
	r.iface = iface
	return r.ifaceErr
}

func (r *recordingMulticastConfigurer) SetMulticastTTL(ttl int) error {
	r.ttl = ttl
	return r.ttlErr
}

func TestConfigureActiveMulticastPinsEgressAndTTL(t *testing.T) {
	configurer := &recordingMulticastConfigurer{}
	iface := &net.Interface{Index: 7, Name: "en7"}
	if err := configureActiveMulticast(configurer, iface); err != nil {
		t.Fatalf("configureActiveMulticast: %v", err)
	}
	if configurer.iface != iface {
		t.Fatalf("egress interface = %+v, want %+v", configurer.iface, iface)
	}
	if configurer.ttl != 1 {
		t.Fatalf("multicast TTL = %d, want 1", configurer.ttl)
	}
}

func TestConfigureActiveMulticastStopsAfterOptionFailure(t *testing.T) {
	ifaceErr := errors.New("interface option failed")
	configurer := &recordingMulticastConfigurer{ifaceErr: ifaceErr}
	if err := configureActiveMulticast(configurer, &net.Interface{Index: 2}); !errors.Is(err, ifaceErr) {
		t.Fatalf("error = %v, want wrapped interface option failure", err)
	}
	if configurer.ttl != 0 {
		t.Fatalf("TTL setter ran after interface failure: ttl=%d", configurer.ttl)
	}
}
