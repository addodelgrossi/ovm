package media2_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
)

func TestFixtureBackedMedia2Responses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		path string
		call func(*media2.Client) error
	}{
		{"GetProfiles", "media2-get-profiles-success.xml", callProfiles},
		{"GetStreamUri", "media2-get-stream-uri-success.xml", callStreamURI},
		{"GetSnapshotUri", "media2-get-snapshot-uri-success.xml", callSnapshotURI},
		{"GetVideoEncoderConfigurations", "media2-get-video-encoder-configurations-success.xml", callVideoEncoderConfigurations},
		{"GetVideoEncoderConfigurationOptions", "media2-get-video-encoder-configuration-options-success.xml", callVideoEncoderOptions},
		{"GetAudioEncoderConfigurations", "media2-get-audio-encoder-configurations-success.xml", callAudioEncoderConfigurations},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			data := readMedia2Fixture(t, testCase.path)
			client, _, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK, string(data))
			defer closeFn()
			if err := testCase.call(client); err != nil {
				t.Fatalf("fixture operation error = %v", err)
			}
		})
	}
}

func readMedia2Fixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "fixtures", "media", name)
	//nolint:gosec // The caller supplies only fixed table constants owned by this test.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	return data
}
