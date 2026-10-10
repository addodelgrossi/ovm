package fakedevice

import (
	"strings"
	"testing"
)

func TestMediaSelectionRejectsAmbiguousTokens(t *testing.T) {
	t.Parallel()
	inventory := MediaInventory{
		Profiles:            []MediaProfileData{{Token: "duplicate"}, {Token: "duplicate"}},
		Media1Profiles:      []MediaProfileData{{Token: "duplicate-m1"}, {Token: "duplicate-m1"}},
		VideoSources:        []VideoSourceData{{Token: "source"}, {Token: "source"}},
		VideoEncoders:       []VideoEncoderConfigurationData{{Token: "video"}, {Token: "video"}},
		AudioEncoders:       []AudioEncoderConfigurationData{{Token: "audio"}, {Token: "audio"}},
		Media2VideoEncoders: []VideoEncoder2ConfigurationData{{Token: "video2"}, {Token: "video2"}},
		Media2AudioEncoders: []AudioEncoder2ConfigurationData{{Token: "audio2"}, {Token: "audio2"}},
	}
	if _, ok := findMediaProfile(inventory, mediaVer20, "duplicate"); ok {
		t.Fatal("ambiguous Media2 profile selected")
	}
	if _, ok := findMediaProfile(inventory, mediaVer10, "duplicate-m1"); ok {
		t.Fatal("ambiguous Media1 profile selected")
	}
	if _, ok := findVideoSource(inventory, "source"); ok {
		t.Fatal("ambiguous video source selected")
	}
	if _, ok := findMedia1VideoEncoder(inventory, "video"); ok {
		t.Fatal("ambiguous Media1 video encoder selected")
	}
	if _, ok := findMedia1AudioEncoder(inventory, "audio"); ok {
		t.Fatal("ambiguous Media1 audio encoder selected")
	}
	if _, ok := findMedia2VideoEncoder(inventory, "video2"); ok {
		t.Fatal("ambiguous Media2 video encoder selected")
	}
	if _, ok := findMedia2AudioEncoder(inventory, "audio2"); ok {
		t.Fatal("ambiguous Media2 audio encoder selected")
	}
}

func TestMedia1ProfileSourceReferenceFailsClosed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		sourceToken string
		sources     []VideoSourceData
	}{
		{name: "missing reference", sourceToken: "referenced-source"},
		{name: "empty reference", sources: []VideoSourceData{{Token: "", Width: 640, Height: 480}}},
		{name: "duplicate reference", sourceToken: "referenced-source", sources: []VideoSourceData{
			{Token: "referenced-source", Width: 640, Height: 480},
			{Token: "referenced-source", Width: 1920, Height: 1080},
		}},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			//nolint:gosec // G101 false positive: these are synthetic ONVIF reference tokens, not credentials.
			profile := MediaProfileData{
				VideoSourceConfigurationToken: "source-config",
				VideoSourceToken:              testCase.sourceToken,
			}
			got := buildMedia1ProfileConfigurations(MediaInventory{VideoSources: testCase.sources}, profile)
			if strings.Contains(got, "VideoSourceConfiguration") || strings.Contains(got, "Bounds") {
				t.Fatalf("invalid source reference produced a Media1 association: %s", got)
			}
		})
	}
}
