package fakedevice

import "strconv"

// Media service namespaces and action URIs come from the official ONVIF WSDLs:
//   - Media ver10: https://www.onvif.org/ver10/media/wsdl/media.wsdl
//   - Media2 ver20: https://www.onvif.org/ver20/media/wsdl/media.wsdl
const (
	media1Namespace = "http://www.onvif.org/ver10/media/wsdl"
	media2Namespace = "http://www.onvif.org/ver20/media/wsdl"
)

// Media1 (ver10) action URIs.
const (
	ActionMedia1GetProfiles = media1Namespace + "/GetProfiles"
	// The official ver10 WSDL publishes this anomalous literal: unlike the
	// neighboring actions it has no slash after wsdl and does have a trailing
	// slash. Keep it literal so QName dispatch and header validation agree.
	ActionMedia1GetVideoSources                     = "http://www.onvif.org/ver10/media/wsdlGetVideoSources/"
	ActionMedia1GetVideoEncoderConfigurations       = media1Namespace + "/GetVideoEncoderConfigurations"
	ActionMedia1GetVideoEncoderConfigurationOptions = media1Namespace + "/GetVideoEncoderConfigurationOptions"
	ActionMedia1GetAudioEncoderConfigurations       = media1Namespace + "/GetAudioEncoderConfigurations"
	ActionMedia1GetStreamURI                        = media1Namespace + "/GetStreamUri"
	ActionMedia1GetSnapshotURI                      = media1Namespace + "/GetSnapshotUri"
)

// Media2 (ver20) action URIs. The official Media2 WSDL has no GetVideoSources
// operation; physical source reads remain a Media1 operation.
const (
	ActionMedia2GetProfiles                         = media2Namespace + "/GetProfiles"
	ActionMedia2GetVideoEncoderConfigurations       = media2Namespace + "/GetVideoEncoderConfigurations"
	ActionMedia2GetVideoEncoderConfigurationOptions = media2Namespace + "/GetVideoEncoderConfigurationOptions"
	ActionMedia2GetAudioEncoderConfigurations       = media2Namespace + "/GetAudioEncoderConfigurations"
	ActionMedia2GetStreamURI                        = media2Namespace + "/GetStreamUri"
	ActionMedia2GetSnapshotURI                      = media2Namespace + "/GetSnapshotUri"
)

var media1Operations = map[string]string{
	"GetProfiles":                         ActionMedia1GetProfiles,
	"GetVideoSources":                     ActionMedia1GetVideoSources,
	"GetVideoEncoderConfigurations":       ActionMedia1GetVideoEncoderConfigurations,
	"GetVideoEncoderConfigurationOptions": ActionMedia1GetVideoEncoderConfigurationOptions,
	"GetAudioEncoderConfigurations":       ActionMedia1GetAudioEncoderConfigurations,
	"GetStreamUri":                        ActionMedia1GetStreamURI,
	"GetSnapshotUri":                      ActionMedia1GetSnapshotURI,
}

var media2Operations = map[string]string{
	"GetProfiles":                         ActionMedia2GetProfiles,
	"GetVideoEncoderConfigurations":       ActionMedia2GetVideoEncoderConfigurations,
	"GetVideoEncoderConfigurationOptions": ActionMedia2GetVideoEncoderConfigurationOptions,
	"GetAudioEncoderConfigurations":       ActionMedia2GetAudioEncoderConfigurations,
	"GetStreamUri":                        ActionMedia2GetStreamURI,
	"GetSnapshotUri":                      ActionMedia2GetSnapshotURI,
}

// MediaInventory is the configurable, project-authored data returned by both
// synthetic Media services. Slices are copied by New, and normalizeConfig
// validates required values and bounds before the handler can serve them.
type MediaInventory struct {
	Profiles []MediaProfileData
	// Media1Profiles optionally supplies a DISTINCT profile list for the Media1
	// (ver10) GetProfiles rendering. When nil, the Media1 rendering uses
	// Profiles (with the Media1 association accessors), so by default both
	// services advertise the same profile tokens. A camera whose two services
	// expose different profile token sets is modeled by setting this field.
	Media1Profiles            []MediaProfileData
	VideoSources              []VideoSourceData
	VideoEncoders             []VideoEncoderConfigurationData
	VideoEncoderOptions       VideoEncoderOptionsData
	AudioEncoders             []AudioEncoderConfigurationData
	Media2VideoEncoders       []VideoEncoder2ConfigurationData
	Media2VideoEncoderOptions VideoEncoder2OptionsData
	Media2AudioEncoders       []AudioEncoder2ConfigurationData

	// SnapshotPath and StreamPath are path components, never absolute external
	// URLs. Response builders derive the authority from the current request so
	// every URI remains under the fake server's origin/host.
	SnapshotPath string
	StreamPath   string

	// SnapshotQuery is an optional, synthetic query component (for example
	// "sig=opaque-token") that the fake includes in every generated snapshot
	// URI IN ADDITION TO the generated profile parameter. It models a camera
	// whose snapshot URI requires an operator- or device-supplied query to
	// authorize the GET. It is a project-authored synthetic marker, NEVER a
	// credential, and exists so a test can prove the operational query reaches
	// the wire while remaining absent from returned metadata. It must be a bare
	// "key=value[&key=value...]" fragment with no leading "?" and no control
	// characters.
	SnapshotQuery string
}

// MediaProfileData is one synthetic profile, normally the main or sub stream.
type MediaProfileData struct {
	Token string
	Name  string
	Fixed bool
	// VideoSourceConfigurationToken is the reference token of the embedded
	// video-source configuration, distinct from VideoSourceToken (the physical
	// tt:SourceToken the configuration points at).
	VideoSourceConfigurationToken string
	VideoSourceToken              string
	VideoEncoderToken             string
	AudioEncoderToken             string

	// Media1-specific association overrides. ONVIF Media1 (ver10) and Media2
	// (ver20) are distinct service models whose per-profile configuration
	// references need not share tokens. By default the fields above are used
	// for BOTH service renderings (so existing configurations and the built-in
	// defaults keep identical tokens). When any Media1 override below is set,
	// the Media1 GetProfiles rendering uses it in place of the shared field,
	// letting a configuration express a profile whose Media1 associations
	// differ from its Media2 associations. An empty override means "use the
	// shared field", so partial overrides are supported.
	Media1VideoSourceConfigurationToken string
	Media1VideoSourceToken              string
	Media1VideoEncoderToken             string
	Media1AudioEncoderToken             string
}

// media1VideoSourceConfigurationToken returns the Media1 video source
// configuration token, preferring the Media1 override and falling back to the
// shared field.
func (p MediaProfileData) media1VideoSourceConfigurationToken() string {
	if p.Media1VideoSourceConfigurationToken != "" {
		return p.Media1VideoSourceConfigurationToken
	}
	return p.VideoSourceConfigurationToken
}

// media1VideoSourceToken returns the Media1 physical source token, preferring
// the Media1 override and falling back to the shared field.
func (p MediaProfileData) media1VideoSourceToken() string {
	if p.Media1VideoSourceToken != "" {
		return p.Media1VideoSourceToken
	}
	return p.VideoSourceToken
}

// media1VideoEncoderToken returns the Media1 video encoder configuration token,
// preferring the Media1 override and falling back to the shared field.
func (p MediaProfileData) media1VideoEncoderToken() string {
	if p.Media1VideoEncoderToken != "" {
		return p.Media1VideoEncoderToken
	}
	return p.VideoEncoderToken
}

// media1AudioEncoderToken returns the Media1 audio encoder configuration token,
// preferring the Media1 override and falling back to the shared field.
func (p MediaProfileData) media1AudioEncoderToken() string {
	if p.Media1AudioEncoderToken != "" {
		return p.Media1AudioEncoderToken
	}
	return p.AudioEncoderToken
}

// VideoSourceData is one synthetic physical video source.
type VideoSourceData struct {
	Token     string
	Framerate int
	Width     int
	Height    int
}

// VideoEncoderConfigurationData is one Media1 synthetic encoder
// configuration. Media2 uses the distinct VideoEncoder2ConfigurationData type.
type VideoEncoderConfigurationData struct {
	Token          string
	Name           string
	Encoding       string
	Width          int
	Height         int
	Quality        int
	FrameRateLimit int
	BitrateLimit   int
	GovLength      int
	UseCount       int
}

// VideoEncoderOptionsData is a synthetic set of valid configuration ranges.
type VideoEncoderOptionsData struct {
	Encoding     string
	QualityMin   int
	QualityMax   int
	WidthMin     int
	WidthMax     int
	HeightMin    int
	HeightMax    int
	FrameRateMin int
	FrameRateMax int
	BitrateMin   int
	BitrateMax   int
	GovLengthMin int
	GovLengthMax int
}

// AudioEncoderConfigurationData is one synthetic audio encoder configuration.
type AudioEncoderConfigurationData struct {
	Token      string
	Name       string
	Encoding   string
	Bitrate    int
	SampleRate int
	UseCount   int
}

// VideoEncoder2ConfigurationData and AudioEncoder2ConfigurationData model the
// distinct Media2 wire vocabulary. Their Encoding values are media subtype
// strings (for example H265, PCMU, or MP4A-LATM), not Media1 enums.
type VideoEncoder2ConfigurationData struct {
	Token          string
	Name           string
	Encoding       string
	Width          int
	Height         int
	Quality        float64
	FrameRateLimit float64
	BitrateLimit   int
	AverageBitRate int
	GovLength      int
	Profile        string
	UseCount       int
}

type AudioEncoder2ConfigurationData struct {
	Token      string
	Name       string
	Encoding   string
	Bitrate    int
	SampleRate int
	UseCount   int
}

// VideoEncoder2OptionsData uses a float quality range as required by
// tt:VideoEncoder2ConfigurationOptions.
type VideoEncoder2OptionsData struct {
	Encoding     string
	QualityMin   float64
	QualityMax   float64
	WidthMin     int
	WidthMax     int
	HeightMin    int
	HeightMax    int
	BitrateMin   int
	BitrateMax   int
	GovLengthMin int
	GovLengthMax int
}

func (m MediaInventory) withDefaults() MediaInventory {
	if m.SnapshotPath == "" {
		m.SnapshotPath = "/onvif/snapshot"
	} else if m.SnapshotPath[0] != '/' {
		m.SnapshotPath = "/" + m.SnapshotPath
	}
	if m.StreamPath == "" {
		m.StreamPath = "/onvif/stream"
	} else if m.StreamPath[0] != '/' {
		m.StreamPath = "/" + m.StreamPath
	}
	if len(m.Profiles) == 0 {
		m.Profiles = []MediaProfileData{
			//nolint:gosec // These are public synthetic reference tokens, not credentials.
			{Token: "profile_main", Name: "main", Fixed: true, VideoSourceConfigurationToken: "vsrccfg0", VideoSourceToken: "vsrc0", VideoEncoderToken: "venc_main", AudioEncoderToken: "aenc0"},
			//nolint:gosec // These are public synthetic reference tokens, not credentials.
			{Token: "profile_sub", Name: "sub", Fixed: true, VideoSourceConfigurationToken: "vsrccfg0", VideoSourceToken: "vsrc0", VideoEncoderToken: "venc_sub", AudioEncoderToken: "aenc0"},
		}
	} else {
		m.Profiles = append([]MediaProfileData(nil), m.Profiles...)
	}
	// Media1Profiles is optional and has no default: when nil the Media1
	// rendering reuses Profiles. Copy it when present so the device owns it.
	if len(m.Media1Profiles) > 0 {
		m.Media1Profiles = append([]MediaProfileData(nil), m.Media1Profiles...)
	}
	if len(m.VideoSources) == 0 {
		m.VideoSources = []VideoSourceData{{Token: "vsrc0", Framerate: 30, Width: 1920, Height: 1080}}
	} else {
		m.VideoSources = append([]VideoSourceData(nil), m.VideoSources...)
	}
	if len(m.VideoEncoders) == 0 {
		m.VideoEncoders = []VideoEncoderConfigurationData{
			//nolint:gosec // These are public synthetic reference tokens, not credentials.
			{Token: "venc_main", Name: "VideoEncoderMain", Encoding: "H264", Width: 1920, Height: 1080, Quality: 5, FrameRateLimit: 30, BitrateLimit: 4096, GovLength: 30, UseCount: 1},
			//nolint:gosec // These are public synthetic reference tokens, not credentials.
			{Token: "venc_sub", Name: "VideoEncoderSub", Encoding: "H264", Width: 640, Height: 360, Quality: 4, FrameRateLimit: 15, BitrateLimit: 1024, GovLength: 15, UseCount: 1},
		}
	} else {
		m.VideoEncoders = append([]VideoEncoderConfigurationData(nil), m.VideoEncoders...)
	}
	if m.VideoEncoderOptions == (VideoEncoderOptionsData{}) {
		m.VideoEncoderOptions = VideoEncoderOptionsData{
			Encoding: "H264", QualityMin: 1, QualityMax: 10,
			WidthMin: 320, WidthMax: 1920, HeightMin: 180, HeightMax: 1080,
			FrameRateMin: 1, FrameRateMax: 30, BitrateMin: 256, BitrateMax: 8192,
			GovLengthMin: 1, GovLengthMax: 60,
		}
	}
	if len(m.AudioEncoders) == 0 {
		m.AudioEncoders = []AudioEncoderConfigurationData{
			//nolint:gosec // This is a public synthetic reference token, not a credential.
			{Token: "aenc0", Name: "AudioEncoderMain", Encoding: "G711", Bitrate: 64, SampleRate: 8, UseCount: 1},
		}
	} else {
		m.AudioEncoders = append([]AudioEncoderConfigurationData(nil), m.AudioEncoders...)
	}
	if len(m.Media2VideoEncoders) == 0 {
		m.Media2VideoEncoders = []VideoEncoder2ConfigurationData{
			//nolint:gosec // These are public synthetic reference tokens, not credentials.
			{Token: "venc_main", Name: "VideoEncoderMain", Encoding: "H265", Width: 1920, Height: 1080, Quality: 5.5, FrameRateLimit: 30, BitrateLimit: 4096, AverageBitRate: 3072, GovLength: 30, Profile: "Main", UseCount: 1},
			//nolint:gosec // These are public synthetic reference tokens, not credentials.
			{Token: "venc_sub", Name: "VideoEncoderSub", Encoding: "H264", Width: 640, Height: 360, Quality: 4.5, FrameRateLimit: 15, BitrateLimit: 1024, AverageBitRate: 768, GovLength: 15, Profile: "High", UseCount: 1},
		}
	} else {
		m.Media2VideoEncoders = append([]VideoEncoder2ConfigurationData(nil), m.Media2VideoEncoders...)
	}
	if m.Media2VideoEncoderOptions == (VideoEncoder2OptionsData{}) {
		m.Media2VideoEncoderOptions = VideoEncoder2OptionsData{
			Encoding: "H265", QualityMin: 0.5, QualityMax: 10.5,
			WidthMin: 320, WidthMax: 1920, HeightMin: 180, HeightMax: 1080,
			BitrateMin: 256, BitrateMax: 8192, GovLengthMin: 1, GovLengthMax: 60,
		}
	}
	if len(m.Media2AudioEncoders) == 0 {
		m.Media2AudioEncoders = []AudioEncoder2ConfigurationData{
			//nolint:gosec // This is a public synthetic reference token, not a credential.
			{Token: "aenc0", Name: "AudioEncoderMain", Encoding: "PCMU", Bitrate: 64, SampleRate: 8, UseCount: 1},
		}
	} else {
		m.Media2AudioEncoders = append([]AudioEncoder2ConfigurationData(nil), m.Media2AudioEncoders...)
	}
	return m
}

func itoa(value int) string { return strconv.Itoa(value) }
