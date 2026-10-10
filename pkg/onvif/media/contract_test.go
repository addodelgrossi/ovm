package media_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

type mediaOperationCase struct {
	name    string
	payload string
	call    func(*media.Client) error
}

func mediaOperationCases() []mediaOperationCase {
	const uriMetadata = `<s:InvalidAfterConnect>false</s:InvalidAfterConnect><s:InvalidAfterReboot>false</s:InvalidAfterReboot><s:Timeout>PT0S</s:Timeout>`
	return []mediaOperationCase{
		{"GetProfiles", `<m:GetProfilesResponse xmlns:m="` + media.Namespace + `"/>`, callProfiles},
		{"GetStreamUri", `<m:GetStreamUriResponse xmlns:m="` + media.Namespace + `" xmlns:s="` + media.SchemaNamespace + `"><m:MediaUri><s:Uri>rtsp://device.invalid/live</s:Uri>` + uriMetadata + `</m:MediaUri></m:GetStreamUriResponse>`, callStreamURI},
		{"GetSnapshotUri", `<m:GetSnapshotUriResponse xmlns:m="` + media.Namespace + `" xmlns:s="` + media.SchemaNamespace + `"><m:MediaUri><s:Uri>https://device.invalid/snapshot.jpg</s:Uri>` + uriMetadata + `</m:MediaUri></m:GetSnapshotUriResponse>`, callSnapshotURI},
		{"GetVideoSources", `<m:GetVideoSourcesResponse xmlns:m="` + media.Namespace + `"/>`, callVideoSources},
		{"GetVideoEncoderConfigurations", `<m:GetVideoEncoderConfigurationsResponse xmlns:m="` + media.Namespace + `"/>`, callVideoEncoderConfigurations},
		{"GetVideoEncoderConfigurationOptions", `<m:GetVideoEncoderConfigurationOptionsResponse xmlns:m="` + media.Namespace + `" xmlns:s="` + media.SchemaNamespace + `"><m:Options><s:QualityRange><s:Min>0</s:Min><s:Max>10</s:Max></s:QualityRange></m:Options></m:GetVideoEncoderConfigurationOptionsResponse>`, callVideoEncoderOptions},
		{"GetAudioEncoderConfigurations", `<m:GetAudioEncoderConfigurationsResponse xmlns:m="` + media.Namespace + `"/>`, callAudioEncoderConfigurations},
	}
}

func TestEveryOperationPrefixToleranceAndSOAP11Fallback(t *testing.T) {
	t.Parallel()
	for _, operation := range mediaOperationCases() {
		operation := operation
		t.Run(operation.name, func(t *testing.T) {
			t.Parallel()
			t.Run("prefix variation", func(t *testing.T) {
				client, _, closeFn := serveOK(t, operation.payload)
				defer closeFn()
				if err := operation.call(client); err != nil {
					t.Fatalf("operation error = %v", err)
				}
			})
			t.Run("SOAP 1.1 evidence fallback", func(t *testing.T) {
				client, closeFn := newSOAP11RawClient(t, soap11Envelope(operation.payload))
				defer closeFn()
				if err := operation.call(client); err != nil {
					t.Fatalf("operation error = %v", err)
				}
			})
		})
	}
}

func TestEveryOperationPropagatesTypedFaults(t *testing.T) {
	t.Parallel()
	faults := []struct {
		name    string
		subcode string
		want    error
	}{
		{"fault", "ter:InvalidArgVal", soap.ErrInvalidArgVal},
		{"unsupported", "ter:ActionNotSupported", soap.ErrActionNotSupported},
	}
	for _, operation := range mediaOperationCases() {
		operation := operation
		for _, fault := range faults {
			fault := fault
			t.Run(operation.name+"/"+fault.name, func(t *testing.T) {
				t.Parallel()
				client, _, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", 500,
					soap12Envelope(soap12Fault("env:Sender", fault.subcode, "synthetic fault")))
				defer closeFn()
				if err := operation.call(client); !errors.Is(err, fault.want) {
					t.Fatalf("error = %v, want %v", err, fault.want)
				}
			})
		}
	}
}

type recordingMediaTransport struct {
	request  soap.Request
	response *soap.Response
	err      error
}

func (transport *recordingMediaTransport) Call(_ context.Context, _ string, request soap.Request) (*soap.Response, error) {
	transport.request = request
	return transport.response, transport.err
}

func TestRequestOptionsAndNilResponseSafety(t *testing.T) {
	t.Parallel()
	header := []byte(`<wsse:Security xmlns:wsse="urn:test"><wsse:Token>opaque</wsse:Token></wsse:Security>`)
	transport := &recordingMediaTransport{response: &soap.Response{BodyXML: []byte(`<m:GetProfilesResponse xmlns:m="` + media.Namespace + `"/>`)}}
	client, err := media.NewClient(transport, "http://device.invalid/media")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, err := client.GetProfiles(context.Background(), media.RequestOptions{HeaderXML: header}); err != nil {
		t.Fatalf("GetProfiles() error = %v", err)
	}
	if !bytes.Equal(transport.request.HeaderXML, header) {
		t.Fatal("HeaderXML was not passed through unchanged")
	}

	nilTransport := &recordingMediaTransport{}
	nilClient, _ := media.NewClient(nilTransport, "http://device.invalid/media")
	if _, err := nilClient.GetProfiles(context.Background()); !errors.Is(err, media.ErrNilResponse) {
		t.Fatalf("nil response error = %v, want ErrNilResponse", err)
	}

	oversizeTransport := &recordingMediaTransport{response: &soap.Response{BodyXML: bytes.Repeat([]byte("x"), (2<<20)+1)}}
	oversizeClient, _ := media.NewClient(oversizeTransport, "http://device.invalid/media")
	if _, err := oversizeClient.GetProfiles(context.Background()); !errors.Is(err, media.ErrMalformedXML) {
		t.Fatalf("oversize response error = %v, want ErrMalformedXML", err)
	}
}

func TestURIVariantsAndSafeValidation(t *testing.T) {
	t.Parallel()
	streamURI := `rtsps://viewer:camera-password@[2001:db8::20]:8554/live?session=opaque`
	streamPayload := `<m:GetStreamUriResponse xmlns:m="` + media.Namespace + `" xmlns:s="` + media.SchemaNamespace + `"><m:MediaUri><s:Uri>` + streamURI + `</s:Uri><s:InvalidAfterConnect>false</s:InvalidAfterConnect><s:InvalidAfterReboot>false</s:InvalidAfterReboot><s:Timeout>PT1M</s:Timeout></m:MediaUri></m:GetStreamUriResponse>`
	client, _, closeFn := serveOK(t, streamPayload)
	defer closeFn()
	got, err := client.GetStreamUri(context.Background(), "p", media.StreamSetup{Stream: media.StreamRTPUnicast, Protocol: media.TransportRTSP})
	if err != nil || got.URI != streamURI {
		t.Fatalf("GetStreamUri() = (%q, %v)", got.URI, err)
	}

	secret := "must-not-appear"
	invalidPayload := `<m:GetSnapshotUriResponse xmlns:m="` + media.Namespace + `" xmlns:s="` + media.SchemaNamespace + `"><m:MediaUri><s:Uri>ftp://` + secret + `@device.invalid/image</s:Uri><s:InvalidAfterConnect>false</s:InvalidAfterConnect><s:InvalidAfterReboot>false</s:InvalidAfterReboot><s:Timeout>PT0S</s:Timeout></m:MediaUri></m:GetSnapshotUriResponse>`
	invalidClient, _, invalidClose := serveOK(t, invalidPayload)
	defer invalidClose()
	_, err = invalidClient.GetSnapshotUri(context.Background(), "p")
	if !errors.Is(err, media.ErrInvalidField) {
		t.Fatalf("error = %v, want ErrInvalidField", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("URI validation error leaked input: %v", err)
	}
}

func TestRequiredFieldsEnumsAndRanges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		payload string
		call    func(*media.Client) error
		want    error
	}{
		{"MediaUri metadata required", `<m:GetSnapshotUriResponse xmlns:m="` + media.Namespace + `" xmlns:s="` + media.SchemaNamespace + `"><m:MediaUri><s:Uri>https://device.invalid/a.jpg</s:Uri></m:MediaUri></m:GetSnapshotUriResponse>`, callSnapshotURI, media.ErrMissingField},
		{"VideoSource resolution required", `<m:GetVideoSourcesResponse xmlns:m="` + media.Namespace + `" xmlns:s="` + media.SchemaNamespace + `"><m:VideoSources token="v"><s:Framerate>30</s:Framerate></m:VideoSources></m:GetVideoSourcesResponse>`, callVideoSources, media.ErrMissingField},
		{"VideoEncoding enum", `<m:GetVideoEncoderConfigurationsResponse xmlns:m="` + media.Namespace + `" xmlns:s="` + media.SchemaNamespace + `"><m:Configurations token="v"><s:Name>N</s:Name><s:UseCount>1</s:UseCount><s:Encoding>H265</s:Encoding><s:Resolution><s:Width>1</s:Width><s:Height>1</s:Height></s:Resolution><s:Quality>1</s:Quality>` + validMedia1StreamingTail + `</m:Configurations></m:GetVideoEncoderConfigurationsResponse>`, callVideoEncoderConfigurations, media.ErrInvalidField},
		{"finite quality", `<m:GetVideoEncoderConfigurationsResponse xmlns:m="` + media.Namespace + `" xmlns:s="` + media.SchemaNamespace + `"><m:Configurations token="v"><s:Name>N</s:Name><s:UseCount>1</s:UseCount><s:Encoding>H264</s:Encoding><s:Resolution><s:Width>1</s:Width><s:Height>1</s:Height></s:Resolution><s:Quality>NaN</s:Quality>` + validMedia1StreamingTail + `</m:Configurations></m:GetVideoEncoderConfigurationsResponse>`, callVideoEncoderConfigurations, media.ErrInvalidField},
		{"AudioEncoding enum", `<m:GetAudioEncoderConfigurationsResponse xmlns:m="` + media.Namespace + `" xmlns:s="` + media.SchemaNamespace + `"><m:Configurations token="a"><s:Name>N</s:Name><s:UseCount>1</s:UseCount><s:Encoding>OPUS</s:Encoding><s:Bitrate>64</s:Bitrate><s:SampleRate>48</s:SampleRate>` + validMedia1StreamingTail + `</m:Configurations></m:GetAudioEncoderConfigurationsResponse>`, callAudioEncoderConfigurations, media.ErrInvalidField},
		{"option range ordering", `<m:GetVideoEncoderConfigurationOptionsResponse xmlns:m="` + media.Namespace + `" xmlns:s="` + media.SchemaNamespace + `"><m:Options><s:QualityRange><s:Min>10</s:Min><s:Max>1</s:Max></s:QualityRange></m:Options></m:GetVideoEncoderConfigurationOptionsResponse>`, callVideoEncoderOptions, media.ErrInvalidField},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, err := invoke(t, testCase.payload, testCase.call)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestResponseQNameAndChildErrorsAreStrictAndSecretSafe(t *testing.T) {
	t.Parallel()
	secret := "response-qname-secret"
	_, err := invoke(t, `<x:GetProfilesResponse xmlns:x="urn:`+secret+`"/>`, callProfiles)
	if !errors.Is(err, media.ErrUnexpectedElement) {
		t.Fatalf("root QName error = %v, want ErrUnexpectedElement", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("root QName error leaked namespace: %v", err)
	}

	cases := []struct {
		name    string
		payload string
		call    func(*media.Client) error
	}{
		{"foreign list child", `<m:GetProfilesResponse xmlns:m="` + media.Namespace + `" xmlns:x="urn:foreign"><x:Profiles/></m:GetProfilesResponse>`, callProfiles},
		{"unexpected list child", `<m:GetVideoSourcesResponse xmlns:m="` + media.Namespace + `"><m:Unexpected/></m:GetVideoSourcesResponse>`, callVideoSources},
		{"nested simple content", `<m:GetSnapshotUriResponse xmlns:m="` + media.Namespace + `" xmlns:s="` + media.SchemaNamespace + `"><m:MediaUri><s:Uri><s:Nested/></s:Uri><s:InvalidAfterConnect>false</s:InvalidAfterConnect><s:InvalidAfterReboot>false</s:InvalidAfterReboot><s:Timeout>PT0S</s:Timeout></m:MediaUri></m:GetSnapshotUriResponse>`, callSnapshotURI},
		{"out of sequence", `<m:GetSnapshotUriResponse xmlns:m="` + media.Namespace + `" xmlns:s="` + media.SchemaNamespace + `"><m:MediaUri><s:Timeout>PT0S</s:Timeout><s:Uri>https://device.invalid/a.jpg</s:Uri><s:InvalidAfterConnect>false</s:InvalidAfterConnect><s:InvalidAfterReboot>false</s:InvalidAfterReboot></m:MediaUri></m:GetSnapshotUriResponse>`, callSnapshotURI},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, err := invoke(t, testCase.payload, testCase.call)
			if err == nil {
				t.Fatal("schema-invalid response was accepted")
			}
		})
	}
}

func TestEveryOperationPreservesContextErrors(t *testing.T) {
	t.Parallel()
	for _, operation := range mediaOperationCases() {
		operation := operation
		t.Run(operation.name, func(t *testing.T) {
			t.Parallel()
			transport := &recordingMediaTransport{err: context.Canceled}
			client, err := media.NewClient(transport, "http://device.invalid/media")
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			if err := operation.call(client); !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
		})
	}
}
