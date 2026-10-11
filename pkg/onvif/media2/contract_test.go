package media2_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

type media2OperationCase struct {
	name    string
	payload string
	call    func(*media2.Client) error
}

func media2OperationCases() []media2OperationCase {
	return []media2OperationCase{
		{"GetProfiles", `<m:GetProfilesResponse xmlns:m="` + media2.Namespace + `"/>`, callProfiles},
		{"GetStreamUri", `<m:GetStreamUriResponse xmlns:m="` + media2.Namespace + `"><m:Uri>rtsp://device.invalid/live</m:Uri></m:GetStreamUriResponse>`, callStreamURI},
		{"GetSnapshotUri", `<m:GetSnapshotUriResponse xmlns:m="` + media2.Namespace + `"><m:Uri>https://device.invalid/snapshot.jpg</m:Uri></m:GetSnapshotUriResponse>`, callSnapshotURI},
		{"GetVideoEncoderConfigurations", `<m:GetVideoEncoderConfigurationsResponse xmlns:m="` + media2.Namespace + `"/>`, callVideoEncoderConfigurations},
		{"GetVideoEncoderConfigurationOptions", `<m:GetVideoEncoderConfigurationOptionsResponse xmlns:m="` + media2.Namespace + `" xmlns:s="` + media2.SchemaNamespace + `"><m:Options GovLengthRange="1 60"><s:Encoding>H264</s:Encoding><s:QualityRange><s:Min>0</s:Min><s:Max>10</s:Max></s:QualityRange><s:ResolutionsAvailable><s:Width>1920</s:Width><s:Height>1080</s:Height></s:ResolutionsAvailable><s:BitrateRange><s:Min>64</s:Min><s:Max>8192</s:Max></s:BitrateRange></m:Options></m:GetVideoEncoderConfigurationOptionsResponse>`, callVideoEncoderOptions},
		{"GetAudioEncoderConfigurations", `<m:GetAudioEncoderConfigurationsResponse xmlns:m="` + media2.Namespace + `"/>`, callAudioEncoderConfigurations},
	}
}

func TestEveryOperationPrefixToleranceAndSOAP11Fallback(t *testing.T) {
	t.Parallel()
	for _, operation := range media2OperationCases() {
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
	for _, operation := range media2OperationCases() {
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

type recordingMedia2Transport struct {
	request  soap.Request
	response *soap.Response
	err      error
}

func (transport *recordingMedia2Transport) Call(_ context.Context, _ string, request soap.Request) (*soap.Response, error) {
	transport.request = request
	return transport.response, transport.err
}

func TestRequestOptionsAndNilResponseSafety(t *testing.T) {
	t.Parallel()
	header := []byte(`<wsse:Security xmlns:wsse="urn:test"><wsse:Token>opaque</wsse:Token></wsse:Security>`)
	transport := &recordingMedia2Transport{response: &soap.Response{BodyXML: []byte(`<m:GetProfilesResponse xmlns:m="` + media2.Namespace + `"/>`)}}
	client, err := media2.NewClient(transport, "http://device.invalid/media2")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, err := client.GetProfiles(context.Background(), "", nil, media2.RequestOptions{HeaderXML: header}); err != nil {
		t.Fatalf("GetProfiles() error = %v", err)
	}
	if !bytes.Equal(transport.request.HeaderXML, header) {
		t.Fatal("HeaderXML was not passed through unchanged")
	}

	nilTransport := &recordingMedia2Transport{}
	nilClient, _ := media2.NewClient(nilTransport, "http://device.invalid/media2")
	if _, err := nilClient.GetProfiles(context.Background(), "", nil); !errors.Is(err, media2.ErrNilResponse) {
		t.Fatalf("nil response error = %v, want ErrNilResponse", err)
	}

	oversizeTransport := &recordingMedia2Transport{response: &soap.Response{BodyXML: bytes.Repeat([]byte("x"), (2<<20)+1)}}
	oversizeClient, _ := media2.NewClient(oversizeTransport, "http://device.invalid/media2")
	if _, err := oversizeClient.GetProfiles(context.Background(), "", nil); !errors.Is(err, media2.ErrMalformedXML) {
		t.Fatalf("oversize response error = %v, want ErrMalformedXML", err)
	}
}

func TestURIVariantsAndSafeValidation(t *testing.T) {
	t.Parallel()
	streamURI := `rtsps://viewer:camera-password@[2001:db8::20]:8554/live?session=opaque`
	payload := `<m:GetStreamUriResponse xmlns:m="` + media2.Namespace + `"><m:Uri>` + streamURI + `</m:Uri></m:GetStreamUriResponse>`
	client, _, closeFn := serveOK(t, payload)
	defer closeFn()
	got, err := client.GetStreamUri(context.Background(), media2.StreamRtspsUnicast, "p")
	if err != nil || got != streamURI {
		t.Fatalf("GetStreamUri() = (%q, %v)", got, err)
	}

	secret := "must-not-appear"
	invalidPayload := `<m:GetSnapshotUriResponse xmlns:m="` + media2.Namespace + `"><m:Uri>ftp://` + secret + `@device.invalid/image</m:Uri></m:GetSnapshotUriResponse>`
	invalidClient, _, invalidClose := serveOK(t, invalidPayload)
	defer invalidClose()
	_, err = invalidClient.GetSnapshotUri(context.Background(), "p")
	if !errors.Is(err, media2.ErrInvalidField) {
		t.Fatalf("error = %v, want ErrInvalidField", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("URI validation error leaked input: %v", err)
	}
}

func TestProfileFilterCardinality(t *testing.T) {
	t.Parallel()
	transport := &recordingMedia2Transport{response: &soap.Response{BodyXML: []byte(`<m:GetProfilesResponse xmlns:m="` + media2.Namespace + `"/>`)}}
	client, _ := media2.NewClient(transport, "http://device.invalid/media2")
	// Use literals so this contract test does not pass merely because a
	// production constant drifted with the validation table.
	for _, types := range [][]media2.ConfigurationType{
		{"All", "VideoEncoder"},
		{"VideoEncoder", "VideoEncoder"},
	} {
		if _, err := client.GetProfiles(context.Background(), "", types); !errors.Is(err, media2.ErrInvalidInput) {
			t.Fatalf("types %v error = %v, want ErrInvalidInput", types, err)
		}
	}
}

func TestRequiredFieldsAndRanges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		payload string
		call    func(*media2.Client) error
		want    error
	}{
		{"UseCount required", `<m:GetVideoEncoderConfigurationsResponse xmlns:m="` + media2.Namespace + `" xmlns:s="` + media2.SchemaNamespace + `"><m:Configurations token="v"><s:Name>N</s:Name><s:Encoding>H264</s:Encoding><s:Resolution><s:Width>1</s:Width><s:Height>1</s:Height></s:Resolution><s:Quality>1</s:Quality></m:Configurations></m:GetVideoEncoderConfigurationsResponse>`, callVideoEncoderConfigurations, media2.ErrMissingField},
		{"resolution required", `<m:GetVideoEncoderConfigurationsResponse xmlns:m="` + media2.Namespace + `" xmlns:s="` + media2.SchemaNamespace + `"><m:Configurations token="v"><s:Name>N</s:Name><s:UseCount>1</s:UseCount><s:Encoding>H264</s:Encoding><s:Quality>1</s:Quality></m:Configurations></m:GetVideoEncoderConfigurationsResponse>`, callVideoEncoderConfigurations, media2.ErrMissingField},
		{"finite quality", `<m:GetVideoEncoderConfigurationsResponse xmlns:m="` + media2.Namespace + `" xmlns:s="` + media2.SchemaNamespace + `"><m:Configurations token="v"><s:Name>N</s:Name><s:UseCount>1</s:UseCount><s:Encoding>H264</s:Encoding><s:Resolution><s:Width>1</s:Width><s:Height>1</s:Height></s:Resolution><s:Quality>+Inf</s:Quality></m:Configurations></m:GetVideoEncoderConfigurationsResponse>`, callVideoEncoderConfigurations, media2.ErrInvalidField},
		{"audio bitrate required", `<m:GetAudioEncoderConfigurationsResponse xmlns:m="` + media2.Namespace + `" xmlns:s="` + media2.SchemaNamespace + `"><m:Configurations token="a"><s:Name>N</s:Name><s:UseCount>1</s:UseCount><s:Encoding>PCMU</s:Encoding><s:SampleRate>8</s:SampleRate></m:Configurations></m:GetAudioEncoderConfigurationsResponse>`, callAudioEncoderConfigurations, media2.ErrMissingField},
		{"options range ordering", `<m:GetVideoEncoderConfigurationOptionsResponse xmlns:m="` + media2.Namespace + `" xmlns:s="` + media2.SchemaNamespace + `"><m:Options><s:Encoding>H264</s:Encoding><s:QualityRange><s:Min>10</s:Min><s:Max>1</s:Max></s:QualityRange><s:ResolutionsAvailable><s:Width>1</s:Width><s:Height>1</s:Height></s:ResolutionsAvailable><s:BitrateRange><s:Min>1</s:Min><s:Max>2</s:Max></s:BitrateRange></m:Options></m:GetVideoEncoderConfigurationOptionsResponse>`, callVideoEncoderOptions, media2.ErrInvalidField},
		{"GovLengthRange remains positive", `<m:GetVideoEncoderConfigurationOptionsResponse xmlns:m="` + media2.Namespace + `" xmlns:s="` + media2.SchemaNamespace + `"><m:Options GovLengthRange="0 60"><s:Encoding>H264</s:Encoding><s:QualityRange><s:Min>1</s:Min><s:Max>10</s:Max></s:QualityRange><s:ResolutionsAvailable><s:Width>1</s:Width><s:Height>1</s:Height></s:ResolutionsAvailable><s:BitrateRange><s:Min>1</s:Min><s:Max>2</s:Max></s:BitrateRange></m:Options></m:GetVideoEncoderConfigurationOptionsResponse>`, callVideoEncoderOptions, media2.ErrInvalidField},
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
	if !errors.Is(err, media2.ErrUnexpectedElement) {
		t.Fatalf("root QName error = %v, want ErrUnexpectedElement", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("root QName error leaked namespace: %v", err)
	}

	cases := []struct {
		name    string
		payload string
		call    func(*media2.Client) error
	}{
		{"foreign list child", `<m:GetProfilesResponse xmlns:m="` + media2.Namespace + `" xmlns:x="urn:foreign"><x:Profiles/></m:GetProfilesResponse>`, callProfiles},
		{"unexpected list child", `<m:GetAudioEncoderConfigurationsResponse xmlns:m="` + media2.Namespace + `"><m:Unexpected/></m:GetAudioEncoderConfigurationsResponse>`, callAudioEncoderConfigurations},
		{"nested simple content", `<m:GetSnapshotUriResponse xmlns:m="` + media2.Namespace + `"><m:Uri><m:Nested/></m:Uri></m:GetSnapshotUriResponse>`, callSnapshotURI},
		{"out of sequence", `<m:GetVideoEncoderConfigurationsResponse xmlns:m="` + media2.Namespace + `" xmlns:s="` + media2.SchemaNamespace + `"><m:Configurations token="v"><s:UseCount>1</s:UseCount><s:Name>N</s:Name><s:Encoding>H264</s:Encoding><s:Resolution><s:Width>1</s:Width><s:Height>1</s:Height></s:Resolution><s:Quality>1</s:Quality></m:Configurations></m:GetVideoEncoderConfigurationsResponse>`, callVideoEncoderConfigurations},
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
	for _, operation := range media2OperationCases() {
		operation := operation
		t.Run(operation.name, func(t *testing.T) {
			t.Parallel()
			transport := &recordingMedia2Transport{err: context.Canceled}
			client, err := media2.NewClient(transport, "http://device.invalid/media2")
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			if err := operation.call(client); !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
		})
	}
}
