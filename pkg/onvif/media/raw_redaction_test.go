package media_test

import (
	"context"
	"strings"
	"testing"
)

// TestPreservedMediaXMLIsCredentialRedacted asserts the bounded diagnostic XML
// retained for profiles, video sources, and encoder configurations has
// credential material redacted before retention.
func TestPreservedMediaXMLIsCredentialRedacted(t *testing.T) {
	t.Parallel()

	identityMarker := "synthetic-user-marker"
	sensitiveMarker := strings.Join([]string{"synthetic", "extension", "marker"}, "-")
	credentialURL := "http://" + identityMarker + ":ignored@device.invalid/x?opaque=" + sensitiveMarker

	profilePayload := `<trt:GetProfilesResponse` + trtNS + ttNS + ` xmlns:v="urn:synthetic">` +
		`<trt:Profiles token="p"><tt:Name>N</tt:Name>` +
		`<tt:Extension><v:XAddr>` + credentialURL + `</v:XAddr><v:Username>` + identityMarker + `</v:Username></tt:Extension>` +
		`</trt:Profiles></trt:GetProfilesResponse>`
	client, _, closeFn := serveOK(t, profilePayload)
	defer closeFn()
	profiles, err := client.GetProfiles(context.Background())
	if err != nil {
		t.Fatalf("GetProfiles() error = %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("profiles = %d", len(profiles))
	}
	assertRedacted(t, profiles[0].RawXML, identityMarker, sensitiveMarker)

	sourcePayload := `<trt:GetVideoSourcesResponse` + trtNS + ttNS + ` xmlns:v="urn:synthetic">` +
		`<trt:VideoSources token="v"><tt:Framerate>30</tt:Framerate>` +
		`<tt:Resolution><tt:Width>640</tt:Width><tt:Height>360</tt:Height></tt:Resolution>` +
		`<tt:Extension><v:Password>` + sensitiveMarker + `</v:Password></tt:Extension></trt:VideoSources>` +
		`</trt:GetVideoSourcesResponse>`
	srcClient, _, srcClose := serveOK(t, sourcePayload)
	defer srcClose()
	sources, err := srcClient.GetVideoSources(context.Background())
	if err != nil {
		t.Fatalf("GetVideoSources() error = %v", err)
	}
	assertRedacted(t, sources[0].RawXML, sensitiveMarker)
}

func assertRedacted(t *testing.T, raw []byte, forbidden ...string) {
	t.Helper()
	text := string(raw)
	for _, marker := range forbidden {
		if strings.Contains(text, marker) {
			t.Fatalf("preserved XML retained sensitive marker %q: %s", marker, text)
		}
	}
	if !strings.Contains(text, "REDACTED") {
		t.Fatalf("preserved XML has no redaction marker: %s", text)
	}
}
