package discovery

import (
	"reflect"
	"testing"
)

func TestAggregatorMergesAndDeduplicatesDeterministically(t *testing.T) {
	a := newAggregator()
	// Same endpoint reported twice, in differing order, with overlapping data.
	// Types arrive as resolved expanded names (as the parser produces them).
	nvt := qname{Space: onvifNetworkNamespace, Local: onvifNVTLocal}.expandedName()
	dev := qname{Space: onvifDeviceNamespace, Local: onvifDeviceLocal}.expandedName()
	a.add(parsedMessage{
		Kind:               kindProbeMatch,
		EndpointReference:  "urn:uuid:dev-1",
		XAddrs:             []string{"http://b/", "http://a/"},
		Scopes:             []string{"onvif://z", "onvif://m"},
		Types:              []string{dev},
		MetadataVersion:    "1",
		MetadataVersionNum: 1,
		HasMetadataVersion: true,
	})
	a.add(parsedMessage{
		Kind:               kindProbeMatch,
		EndpointReference:  "urn:uuid:dev-1",
		XAddrs:             []string{"http://a/", "http://c/"},
		Scopes:             []string{"onvif://m"},
		Types:              []string{nvt, dev},
		MetadataVersion:    "2",
		MetadataVersionNum: 2,
		HasMetadataVersion: true,
	})
	a.add(parsedMessage{
		Kind:              kindProbeMatch,
		EndpointReference: "urn:uuid:dev-0",
		XAddrs:            []string{"http://x/"},
	})

	got := a.devicesSorted()
	want := []Device{
		{
			EndpointReference: "urn:uuid:dev-0",
			XAddrs:            []string{"http://x/"},
		},
		{
			EndpointReference: "urn:uuid:dev-1",
			XAddrs:            []string{"http://a/", "http://b/", "http://c/"},
			Scopes:            []string{"onvif://m", "onvif://z"},
			Types:             sortedCopy([]string{dev, nvt}),
			MetadataVersion:   "2",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("devices = %#v\nwant %#v", got, want)
	}
}

// TestAggregatorSelectsMetadataVersionNumerically verifies that MetadataVersion
// selection compares numerically, so version 10 beats version 9 even though
// "10" < "9" lexically. The retained textual form is the highest numeric
// value's text.
func TestAggregatorSelectsMetadataVersionNumerically(t *testing.T) {
	a := newAggregator()
	a.add(parsedMessage{
		Kind:               kindProbeMatch,
		EndpointReference:  "urn:uuid:dev-x",
		MetadataVersion:    "9",
		MetadataVersionNum: 9,
		HasMetadataVersion: true,
	})
	a.add(parsedMessage{
		Kind:               kindProbeMatch,
		EndpointReference:  "urn:uuid:dev-x",
		MetadataVersion:    "10",
		MetadataVersionNum: 10,
		HasMetadataVersion: true,
	})
	got := a.devicesSorted()
	if len(got) != 1 || got[0].MetadataVersion != "10" {
		t.Fatalf("MetadataVersion = %q, want 10 (numeric selection, not lexical)", got[0].MetadataVersion)
	}
}

// TestAggregatorIgnoresMalformedMetadataVersion verifies that a message whose
// metadata version was malformed (and therefore not flagged present) never wins
// selection over a valid numeric version, regardless of arrival order.
func TestAggregatorIgnoresMalformedMetadataVersion(t *testing.T) {
	a := newAggregator()
	a.add(parsedMessage{
		Kind:               kindProbeMatch,
		EndpointReference:  "urn:uuid:dev-y",
		MetadataVersion:    "5",
		MetadataVersionNum: 5,
		HasMetadataVersion: true,
	})
	// Malformed value: present in the raw string but not flagged numeric.
	a.add(parsedMessage{
		Kind:              kindProbeMatch,
		EndpointReference: "urn:uuid:dev-y",
		MetadataVersion:   "not-a-number",
	})
	got := a.devicesSorted()
	if len(got) != 1 || got[0].MetadataVersion != "5" {
		t.Fatalf("MetadataVersion = %q, want 5 (malformed value ignored)", got[0].MetadataVersion)
	}
}

func TestAggregatorIgnoresEmptyEndpoint(t *testing.T) {
	a := newAggregator()
	a.add(parsedMessage{Kind: kindProbeMatch, XAddrs: []string{"http://a/"}})
	if got := a.devicesSorted(); len(got) != 0 {
		t.Fatalf("devices = %v, want empty", got)
	}
}

func TestAggregatorIsOrderIndependent(t *testing.T) {
	build := func(order []string) []Device {
		a := newAggregator()
		for _, e := range order {
			a.add(parsedMessage{Kind: kindProbeMatch, EndpointReference: e, XAddrs: []string{"http://" + e}})
		}
		return a.devicesSorted()
	}
	first := build([]string{"urn:a", "urn:b", "urn:c"})
	second := build([]string{"urn:c", "urn:a", "urn:b"})
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("aggregation not order-independent:\n%v\n%v", first, second)
	}
}
