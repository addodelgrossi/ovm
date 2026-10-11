package device_test

import (
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/device"
)

func TestOfficialDeviceWSDLNamespaceAndActions(t *testing.T) {
	t.Parallel()

	const namespace = "http://www.onvif.org/ver10/device/wsdl"
	if device.Namespace != namespace {
		t.Fatalf("Namespace = %q, want %q", device.Namespace, namespace)
	}
	actions := map[string]string{
		"GetSystemDateAndTime": device.ActionGetSystemDateAndTime,
		"GetDeviceInformation": device.ActionGetDeviceInformation,
		"GetCapabilities":      device.ActionGetCapabilities,
		"GetServices":          device.ActionGetServices,
		"GetScopes":            device.ActionGetScopes,
		"GetHostname":          device.ActionGetHostname,
		"GetNetworkInterfaces": device.ActionGetNetworkInterfaces,
	}
	for operation, action := range actions {
		if want := namespace + "/" + operation; action != want {
			t.Errorf("%s action = %q, want %q", operation, action, want)
		}
	}
}
