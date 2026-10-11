package media_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media"
)

func TestFixtureBackedMediaResponses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		path   string
		soap11 bool
		call   func(*media.Client) error
	}{
		{"GetProfiles prefix variation", "media1-get-profiles-prefix-variation.xml", true, callProfiles},
		{"GetStreamUri", "media1-get-stream-uri-success.xml", false, callStreamURI},
		{"GetSnapshotUri", "media1-get-snapshot-uri-success.xml", false, callSnapshotURI},
		{"GetVideoSources", "media1-get-video-sources-success.xml", false, callVideoSources},
		{"GetVideoEncoderConfigurations", "media1-get-video-encoder-configurations-success.xml", false, callVideoEncoderConfigurations},
		{"GetVideoEncoderConfigurationOptions", "media1-get-video-encoder-configuration-options-success.xml", false, callVideoEncoderOptions},
		{"GetAudioEncoderConfigurations", "media1-get-audio-encoder-configurations-success.xml", false, callAudioEncoderConfigurations},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			data := readMediaFixture(t, testCase.path)
			if testCase.soap11 {
				client, closeFn := newSOAP11RawClient(t, string(data))
				defer closeFn()
				if err := testCase.call(client); err != nil {
					t.Fatalf("fixture operation error = %v", err)
				}
				return
			}
			client, _, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK, string(data))
			defer closeFn()
			if err := testCase.call(client); err != nil {
				t.Fatalf("fixture operation error = %v", err)
			}
		})
	}
}

func readMediaFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "fixtures", "media", name)
	//nolint:gosec // The caller supplies only fixed table constants owned by this test.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	return data
}
