package app

import (
	"context"
	"slices"
	"strings"
	"testing"

	media1 "github.com/addodelgrossi/ovm/pkg/onvif/media"
	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
)

func TestSelectByTokenRequiresExactlyOneMatch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		tokens    []string
		requested string
		wantIndex int
		wantError bool
	}{
		{name: "sole unfiltered result", tokens: []string{"only"}, wantIndex: 0},
		{name: "one explicit match", tokens: []string{"other", "wanted"}, requested: "wanted", wantIndex: 1},
		{name: "empty result", requested: "wanted", wantError: true},
		{name: "no explicit match", tokens: []string{"other"}, requested: "wanted", wantError: true},
		{name: "ambiguous unfiltered result", tokens: []string{"a", "b"}, wantError: true},
		{name: "duplicate explicit match", tokens: []string{"wanted", "other", "wanted"}, requested: "wanted", wantError: true},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			index, err := selectByToken(len(test.tokens), test.requested, func(index int) string {
				return test.tokens[index]
			})
			if !test.wantError {
				if err != nil || index != test.wantIndex {
					t.Fatalf("selectByToken() = (%d, %v), want (%d, nil)", index, err, test.wantIndex)
				}
				return
			}
			if err == nil || CategoryOf(err) != CategoryProtocol {
				t.Fatalf("selectByToken() error = %v, category = %q; want protocol", err, CategoryOf(err))
			}
			if test.requested != "" && strings.Contains(err.Error(), test.requested) {
				t.Fatalf("selection error leaked the requested token: %q", err)
			}
		})
	}
}

func TestFetchProfilesRequestsAssociationsExplicitly(t *testing.T) {
	t.Parallel()
	media2Stub := &stubMedia2{}
	resolver := newUnitResolver(t, media2Stub, nil)
	if _, _, _, err := resolver.fetchProfiles(context.Background()); err != nil {
		t.Fatalf("fetchProfiles() error = %v", err)
	}
	if len(media2Stub.profileTypeCalls) != 1 {
		t.Fatalf("GetProfiles calls = %d, want 1", len(media2Stub.profileTypeCalls))
	}
	// Keep the expected WSDL literals independent of production constants.
	want := []media2.ConfigurationType{"VideoSource", "VideoEncoder", "AudioEncoder"}
	if !slices.Equal(media2Stub.profileTypeCalls[0], want) {
		t.Fatalf("GetProfiles Type filters = %v, want %v", media2Stub.profileTypeCalls[0], want)
	}
}

func TestMedia1SelectorsRejectDuplicateIdentities(t *testing.T) {
	t.Parallel()
	const ambiguousToken = "ambiguous-identity-marker"

	t.Run("profiles", func(t *testing.T) {
		t.Parallel()
		media1Stub := &stubMedia1{profiles: []media1.Profile{{Token: ambiguousToken}, {Token: ambiguousToken}}}
		resolver := newUnitResolver(t, &stubMedia2{}, media1Stub)
		resolver.profilesSource = MediaServiceMedia2
		_, ok, err := resolver.media1ProfileFor(context.Background(), normProfile{token: ambiguousToken})
		if err == nil || ok || CategoryOf(err) != CategoryProtocol {
			t.Fatalf("media1ProfileFor() = (ok=%v, err=%v), want protocol ambiguity", ok, err)
		}
		if strings.Contains(err.Error(), ambiguousToken) {
			t.Fatalf("profile ambiguity leaked token: %v", err)
		}
	})

	t.Run("physical video sources", func(t *testing.T) {
		t.Parallel()
		media1Stub := &stubMedia1{videoSources: []media1.VideoSource{{Token: ambiguousToken}, {Token: ambiguousToken}}}
		resolver := newUnitResolver(t, nil, media1Stub)
		status := resolver.resolveVideoSource(context.Background(), normProfile{videoSourceToken: ambiguousToken})
		if status.State != CapabilityUnknown || !strings.Contains(status.Detail, "ambiguous") {
			t.Fatalf("source status = %+v, want safe ambiguity", status)
		}
		if strings.Contains(status.Detail, ambiguousToken) {
			t.Fatalf("source ambiguity leaked token: %+v", status)
		}
	})

	t.Run("video encoders", func(t *testing.T) {
		t.Parallel()
		media1Stub := &stubMedia1{videoEncoders: []media1.VideoEncoderConfiguration{{Token: ambiguousToken}, {Token: ambiguousToken}}}
		resolver := newUnitResolver(t, nil, media1Stub)
		dto, status := resolver.media1VideoEncoder(context.Background(), normProfile{videoEncoderConfigToken: ambiguousToken})
		if dto != nil || status.State != CapabilityUnknown || !strings.Contains(status.Detail, "ambiguous") {
			t.Fatalf("video selection = (%+v, %+v), want safe ambiguity", dto, status)
		}
		if strings.Contains(status.Detail, ambiguousToken) {
			t.Fatalf("video ambiguity leaked token: %+v", status)
		}
	})

	t.Run("audio encoders", func(t *testing.T) {
		t.Parallel()
		media1Stub := &stubMedia1{audioEncoders: []media1.AudioEncoderConfiguration{{Token: ambiguousToken}, {Token: ambiguousToken}}}
		resolver := newUnitResolver(t, nil, media1Stub)
		dto, status := resolver.media1AudioEncoder(context.Background(), normProfile{audioEncoderConfigToken: ambiguousToken})
		if dto != nil || status.State != CapabilityUnknown || !strings.Contains(status.Detail, "ambiguous") {
			t.Fatalf("audio selection = (%+v, %+v), want safe ambiguity", dto, status)
		}
		if strings.Contains(status.Detail, ambiguousToken) {
			t.Fatalf("audio ambiguity leaked token: %+v", status)
		}
	})
}
