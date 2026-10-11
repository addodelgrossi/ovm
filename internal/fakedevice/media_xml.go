package fakedevice

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type mediaVersion int

const (
	mediaVer10 mediaVersion = iota
	mediaVer20
)

type requestOrigin struct {
	httpScheme string
	host       string
}

func originFromRequest(request *http.Request) requestOrigin {
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	host := request.Host
	if host == "" {
		host = request.URL.Host
	}
	if host == "" {
		host = "device.invalid"
	}
	host = safeRequestAuthority(host)
	return requestOrigin{httpScheme: scheme, host: host}
}

func safeRequestAuthority(authority string) string {
	parsed, err := url.Parse("//" + authority)
	if err != nil || parsed.User != nil || parsed.Hostname() == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "device.invalid"
	}
	return parsed.Host
}

func (origin requestOrigin) snapshotURI(path, profileToken, configuredQuery string) string {
	parsedPath, _ := url.Parse(path)
	result := url.URL{Scheme: origin.httpScheme, Host: origin.host, Path: parsedPath.Path, RawPath: parsedPath.RawPath}
	// Build the query from the always-generated profile parameter plus any
	// configured synthetic query component. The configured component is parsed
	// as query values and re-encoded with the profile so the result is a single
	// well-formed query string. The configured marker is never a credential.
	query := url.Values{}
	if configuredQuery != "" {
		if extra, err := url.ParseQuery(configuredQuery); err == nil {
			for key, values := range extra {
				for _, value := range values {
					query.Add(key, value)
				}
			}
		}
	}
	if profileToken != "" {
		query.Set("profile", profileToken)
	}
	if encoded := query.Encode(); encoded != "" {
		result.RawQuery = encoded
	}
	return result.String()
}

func (origin requestOrigin) streamURI(path string, port int, profileToken, protocol string) string {
	scheme := "rtsp"
	if protocol == "RtspsUnicast" || protocol == "RtspsMulticast" {
		scheme = "rtsps"
	}
	parsedPath, _ := url.Parse(path)
	result := url.URL{
		Scheme:  scheme,
		Host:    net.JoinHostPort(hostOnly(origin.host), strconv.Itoa(port)),
		Path:    parsedPath.Path,
		RawPath: parsedPath.RawPath,
	}
	query := url.Values{}
	if profileToken != "" {
		query.Set("profile", profileToken)
	}
	if protocol != "" {
		query.Set("transport", protocol)
	}
	result.RawQuery = query.Encode()
	return result.String()
}

func hostOnly(authority string) string {
	if host, _, err := net.SplitHostPort(authority); err == nil {
		return host
	}
	return strings.Trim(authority, "[]")
}

func mediaPrefix(version mediaVersion) string {
	if version == mediaVer20 {
		return "tr2"
	}
	return "trt"
}

// buildMediaEnvelope binds the operation prefix to the selected service
// namespace while retaining the SOAP version chosen for this exchange.
func buildMediaEnvelope(config normalizedConfig, soapVersion SOAPVersion, media mediaVersion, payload string) string {
	envelopePrefix := config.EnvelopePrefix
	mediaNamespace := media1Namespace
	if media == mediaVer20 {
		mediaNamespace = media2Namespace
	}
	prefix := mediaPrefix(media)
	return `<?xml version="1.0" encoding="UTF-8"?><` + envelopePrefix + `:Envelope xmlns:` + envelopePrefix + `="` + envelopeNamespace(soapVersion) +
		`" xmlns:` + prefix + `="` + mediaNamespace +
		`" xmlns:tt="` + typeNamespace +
		`" xmlns:ter="` + errorNamespace +
		`" xmlns:ovm="` + syntheticNamespace +
		`"><` + envelopePrefix + `:Body>` + payload + `</` + envelopePrefix + `:Body></` + envelopePrefix + `:Envelope>`
}

func buildMediaProfiles(config normalizedConfig, media mediaVersion, request mediaRequest) string {
	prefix := mediaPrefix(media)
	var body string
	for _, profile := range mediaProfiles(config.MediaInventory, media) {
		if request.profileToken != "" && request.profileToken != profile.Token {
			continue
		}
		fixed := "false"
		if profile.Fixed {
			fixed = "true"
		}
		namePrefix := "tt"
		if media == mediaVer20 {
			namePrefix = prefix
		}
		// Configuration members are optional in both profile wire models.
		// Media1 emits configured associations; Media2 emits only the
		// configuration categories explicitly requested by Type, as required by
		// the Media2 GetProfiles contract.
		var associations string
		if media == mediaVer20 {
			associations = buildMedia2ProfileConfigurations(config.MediaInventory, profile, request)
		} else {
			associations = buildMedia1ProfileConfigurations(config.MediaInventory, profile)
		}
		body += `<` + prefix + `:Profiles token="` + escapeXML(profile.Token) + `" fixed="` + fixed + `">` +
			`<` + namePrefix + `:Name>` + escapeXML(profile.Name) + `</` + namePrefix + `:Name>` +
			associations +
			`</` + prefix + `:Profiles>`
	}
	return `<` + prefix + `:GetProfilesResponse>` + body + `</` + prefix + `:GetProfilesResponse>`
}

func mediaProfiles(inventory MediaInventory, media mediaVersion) []MediaProfileData {
	if media == mediaVer10 && len(inventory.Media1Profiles) > 0 {
		return inventory.Media1Profiles
	}
	return inventory.Profiles
}

// buildMedia1ProfileConfigurations emits the embedded tt:VideoSourceConfiguration,
// tt:VideoEncoderConfiguration, and tt:AudioEncoderConfiguration members in the
// official tt:Profile sequence order. Each member carries its reference token
// attribute and the minimal required fields of its configuration entity so the
// public decoder can validate the sequence and extract association tokens.
func buildMedia1ProfileConfigurations(inventory MediaInventory, profile MediaProfileData) string {
	var body string
	// Use the Media1-specific association accessors so a profile whose Media1
	// tokens differ from its Media2 tokens renders the correct ver10 values.
	if cfgToken := profile.media1VideoSourceConfigurationToken(); cfgToken != "" {
		// Resolve the physical source by identity before rendering the
		// association. Missing or duplicate references fail closed instead of
		// producing invented Bounds or selecting by wire order.
		sourceToken := profile.media1VideoSourceToken()
		if sourceToken != "" {
			if source, ok := findVideoSource(inventory, sourceToken); ok {
				body += `<tt:VideoSourceConfiguration token="` + escapeXML(cfgToken) + `">` +
					`<tt:Name>VideoSourceConfig</tt:Name><tt:UseCount>1</tt:UseCount>` +
					`<tt:SourceToken>` + escapeXML(source.Token) + `</tt:SourceToken>` +
					`<tt:Bounds x="0" y="0" width="` + itoa(source.Width) + `" height="` + itoa(source.Height) + `"></tt:Bounds>` +
					`</tt:VideoSourceConfiguration>`
			}
		}
	}
	if token := profile.media1VideoEncoderToken(); token != "" {
		if encoder, ok := findMedia1VideoEncoder(inventory, token); ok {
			body += buildMedia1VideoEncoderConfigElement(encoder)
		}
	}
	if token := profile.media1AudioEncoderToken(); token != "" {
		if audio, ok := findMedia1AudioEncoder(inventory, token); ok {
			body += buildMedia1AudioEncoderConfigElement(audio)
		}
	}
	return body
}

// buildMedia1VideoEncoderConfigElement renders a tt:VideoEncoderConfiguration
// in the official child order, including the RateControl and codec blocks so the
// decoder can extract configured rate-control and codec detail.
func buildMedia1VideoEncoderConfigElement(encoder VideoEncoderConfigurationData) string {
	common := `<tt:Name>` + escapeXML(encoder.Name) + `</tt:Name>` +
		`<tt:UseCount>` + itoa(encoder.UseCount) + `</tt:UseCount>` +
		`<tt:Encoding>` + escapeXML(encoder.Encoding) + `</tt:Encoding>` +
		`<tt:Resolution><tt:Width>` + itoa(encoder.Width) + `</tt:Width><tt:Height>` + itoa(encoder.Height) + `</tt:Height></tt:Resolution>`
	codec := ""
	switch encoder.Encoding {
	case "H264":
		codec = `<tt:H264><tt:GovLength>` + itoa(encoder.GovLength) + `</tt:GovLength><tt:H264Profile>Main</tt:H264Profile></tt:H264>`
	case "MPEG4":
		codec = `<tt:MPEG4><tt:GovLength>` + itoa(encoder.GovLength) + `</tt:GovLength><tt:Mpeg4Profile>SP</tt:Mpeg4Profile></tt:MPEG4>`
	}
	return `<tt:VideoEncoderConfiguration token="` + escapeXML(encoder.Token) + `">` + common +
		`<tt:Quality>` + itoa(encoder.Quality) + `</tt:Quality>` +
		`<tt:RateControl><tt:FrameRateLimit>` + itoa(encoder.FrameRateLimit) + `</tt:FrameRateLimit><tt:EncodingInterval>1</tt:EncodingInterval><tt:BitrateLimit>` + itoa(encoder.BitrateLimit) + `</tt:BitrateLimit></tt:RateControl>` +
		codec + disabledMulticastXML() + `<tt:SessionTimeout>PT60S</tt:SessionTimeout></tt:VideoEncoderConfiguration>`
}

// buildMedia1AudioEncoderConfigElement renders a tt:AudioEncoderConfiguration
// in the official child order.
func buildMedia1AudioEncoderConfigElement(audio AudioEncoderConfigurationData) string {
	return `<tt:AudioEncoderConfiguration token="` + escapeXML(audio.Token) + `">` +
		`<tt:Name>` + escapeXML(audio.Name) + `</tt:Name><tt:UseCount>` + itoa(audio.UseCount) + `</tt:UseCount>` +
		`<tt:Encoding>` + escapeXML(audio.Encoding) + `</tt:Encoding>` +
		`<tt:Bitrate>` + itoa(audio.Bitrate) + `</tt:Bitrate><tt:SampleRate>` + itoa(audio.SampleRate) + `</tt:SampleRate>` +
		disabledMulticastXML() + `<tt:SessionTimeout>PT60S</tt:SessionTimeout></tt:AudioEncoderConfiguration>`
}

// buildMedia2ProfileConfigurations emits only the requested members of the
// tr2:ConfigurationSet, in the exact WSDL sequence. Every emitted member is
// rendered from the configuration referenced by the profile. The required
// VideoSource Bounds use the matching physical source dimensions.
func buildMedia2ProfileConfigurations(inventory MediaInventory, profile MediaProfileData, request mediaRequest) string {
	var members string
	if configurationTypeSelected(request, "VideoSource") && profile.VideoSourceConfigurationToken != "" {
		if source, ok := findVideoSource(inventory, profile.VideoSourceToken); ok {
			members += `<tr2:VideoSource token="` + escapeXML(profile.VideoSourceConfigurationToken) + `">` +
				`<tt:Name>VideoSourceConfig</tt:Name><tt:UseCount>1</tt:UseCount>` +
				`<tt:SourceToken>` + escapeXML(source.Token) + `</tt:SourceToken>` +
				`<tt:Bounds x="0" y="0" width="` + itoa(source.Width) + `" height="` + itoa(source.Height) + `"></tt:Bounds>` +
				`</tr2:VideoSource>`
		}
	}
	if configurationTypeSelected(request, "VideoEncoder") && profile.VideoEncoderToken != "" {
		if encoder, ok := findMedia2VideoEncoder(inventory, profile.VideoEncoderToken); ok {
			members += `<tr2:VideoEncoder token="` + escapeXML(encoder.Token) + `" GovLength="` + itoa(encoder.GovLength) + `" Profile="` + escapeXML(encoder.Profile) + `">` +
				media2VideoEncoderContent(encoder) + `</tr2:VideoEncoder>`
		}
	}
	if configurationTypeSelected(request, "AudioEncoder") && profile.AudioEncoderToken != "" {
		if audio, ok := findMedia2AudioEncoder(inventory, profile.AudioEncoderToken); ok {
			members += `<tr2:AudioEncoder token="` + escapeXML(audio.Token) + `">` +
				media2AudioEncoderContent(audio) + `</tr2:AudioEncoder>`
		}
	}
	if members == "" {
		return ""
	}
	return `<tr2:Configurations>` + members + `</tr2:Configurations>`
}

func configurationTypeSelected(request mediaRequest, configurationType string) bool {
	// Media2 GetProfiles with no Type returns profile metadata without any
	// configuration information. All and explicit categories opt in.
	if len(request.configurationTypes) == 0 {
		return false
	}
	for _, requested := range request.configurationTypes {
		if requested == "All" || requested == configurationType {
			return true
		}
	}
	return false
}

func media2VideoEncoderContent(encoder VideoEncoder2ConfigurationData) string {
	return `<tt:Name>` + escapeXML(encoder.Name) + `</tt:Name><tt:UseCount>` + itoa(encoder.UseCount) + `</tt:UseCount>` +
		`<tt:Encoding>` + escapeXML(encoder.Encoding) + `</tt:Encoding>` +
		`<tt:Resolution><tt:Width>` + itoa(encoder.Width) + `</tt:Width><tt:Height>` + itoa(encoder.Height) + `</tt:Height></tt:Resolution>` +
		media2RateControlXML(encoder) +
		`<tt:Quality>` + ftoa(encoder.Quality) + `</tt:Quality>`
}

func media2RateControlXML(encoder VideoEncoder2ConfigurationData) string {
	return `<tt:RateControl><tt:FrameRateLimit>` + ftoa(encoder.FrameRateLimit) + `</tt:FrameRateLimit>` +
		`<tt:BitrateLimit>` + itoa(encoder.BitrateLimit) + `</tt:BitrateLimit>` +
		`<tt:AverageBitRate>` + itoa(encoder.AverageBitRate) + `</tt:AverageBitRate></tt:RateControl>`
}

func media2AudioEncoderContent(audio AudioEncoder2ConfigurationData) string {
	return `<tt:Name>` + escapeXML(audio.Name) + `</tt:Name><tt:UseCount>` + itoa(audio.UseCount) + `</tt:UseCount>` +
		`<tt:Encoding>` + escapeXML(audio.Encoding) + `</tt:Encoding>` +
		`<tt:Bitrate>` + itoa(audio.Bitrate) + `</tt:Bitrate><tt:SampleRate>` + itoa(audio.SampleRate) + `</tt:SampleRate>`
}

// The find helpers reject multiple matches instead of selecting whichever
// duplicate appears first. Valid device configurations already reject
// duplicates, but these checks keep internal selection deterministic.
func findVideoSource(inventory MediaInventory, token string) (VideoSourceData, bool) {
	var match VideoSourceData
	found := false
	for _, source := range inventory.VideoSources {
		if source.Token != token {
			continue
		}
		if found {
			return VideoSourceData{}, false
		}
		match, found = source, true
	}
	return match, found
}

func findMedia1VideoEncoder(inventory MediaInventory, token string) (VideoEncoderConfigurationData, bool) {
	var match VideoEncoderConfigurationData
	found := false
	for _, encoder := range inventory.VideoEncoders {
		if encoder.Token != token {
			continue
		}
		if found {
			return VideoEncoderConfigurationData{}, false
		}
		match, found = encoder, true
	}
	return match, found
}

func findMedia1AudioEncoder(inventory MediaInventory, token string) (AudioEncoderConfigurationData, bool) {
	var match AudioEncoderConfigurationData
	found := false
	for _, audio := range inventory.AudioEncoders {
		if audio.Token != token {
			continue
		}
		if found {
			return AudioEncoderConfigurationData{}, false
		}
		match, found = audio, true
	}
	return match, found
}

func findMedia2VideoEncoder(inventory MediaInventory, token string) (VideoEncoder2ConfigurationData, bool) {
	var match VideoEncoder2ConfigurationData
	found := false
	for _, encoder := range inventory.Media2VideoEncoders {
		if encoder.Token != token {
			continue
		}
		if found {
			return VideoEncoder2ConfigurationData{}, false
		}
		match, found = encoder, true
	}
	return match, found
}

func findMedia2AudioEncoder(inventory MediaInventory, token string) (AudioEncoder2ConfigurationData, bool) {
	var match AudioEncoder2ConfigurationData
	found := false
	for _, audio := range inventory.Media2AudioEncoders {
		if audio.Token != token {
			continue
		}
		if found {
			return AudioEncoder2ConfigurationData{}, false
		}
		match, found = audio, true
	}
	return match, found
}

func buildVideoSources(config normalizedConfig) string {
	var body string
	for _, source := range config.MediaInventory.VideoSources {
		body += `<trt:VideoSources token="` + escapeXML(source.Token) + `">` +
			`<tt:Framerate>` + itoa(source.Framerate) + `</tt:Framerate>` +
			`<tt:Resolution><tt:Width>` + itoa(source.Width) + `</tt:Width><tt:Height>` + itoa(source.Height) + `</tt:Height></tt:Resolution>` +
			`</trt:VideoSources>`
	}
	return `<trt:GetVideoSourcesResponse>` + body + `</trt:GetVideoSourcesResponse>`
}

func buildVideoEncoderConfigurations(config normalizedConfig, media mediaVersion, request mediaRequest) string {
	if media == mediaVer20 {
		return buildVideoEncoder2Configurations(config, request)
	}
	var body string
	for _, encoder := range config.MediaInventory.VideoEncoders {
		if !videoEncoderSelected(config.MediaInventory, encoder.Token, request) {
			continue
		}
		common := `<tt:Name>` + escapeXML(encoder.Name) + `</tt:Name>` +
			`<tt:UseCount>` + itoa(encoder.UseCount) + `</tt:UseCount>` +
			`<tt:Encoding>` + escapeXML(encoder.Encoding) + `</tt:Encoding>` +
			`<tt:Resolution><tt:Width>` + itoa(encoder.Width) + `</tt:Width><tt:Height>` + itoa(encoder.Height) + `</tt:Height></tt:Resolution>`
		codec := ""
		switch encoder.Encoding {
		case "H264":
			codec = `<tt:H264><tt:GovLength>` + itoa(encoder.GovLength) + `</tt:GovLength><tt:H264Profile>Main</tt:H264Profile></tt:H264>`
		case "MPEG4":
			codec = `<tt:MPEG4><tt:GovLength>` + itoa(encoder.GovLength) + `</tt:GovLength><tt:Mpeg4Profile>SP</tt:Mpeg4Profile></tt:MPEG4>`
		}
		body += `<trt:Configurations token="` + escapeXML(encoder.Token) + `">` + common +
			`<tt:Quality>` + itoa(encoder.Quality) + `</tt:Quality>` +
			`<tt:RateControl><tt:FrameRateLimit>` + itoa(encoder.FrameRateLimit) + `</tt:FrameRateLimit><tt:EncodingInterval>1</tt:EncodingInterval><tt:BitrateLimit>` + itoa(encoder.BitrateLimit) + `</tt:BitrateLimit></tt:RateControl>` +
			codec + disabledMulticastXML() + `<tt:SessionTimeout>PT60S</tt:SessionTimeout></trt:Configurations>`
	}
	return `<trt:GetVideoEncoderConfigurationsResponse>` + body + `</trt:GetVideoEncoderConfigurationsResponse>`
}

func buildVideoEncoder2Configurations(config normalizedConfig, request mediaRequest) string {
	var body string
	for _, encoder := range config.MediaInventory.Media2VideoEncoders {
		if !videoEncoder2Selected(config.MediaInventory, encoder.Token, request) {
			continue
		}
		body += `<tr2:Configurations token="` + escapeXML(encoder.Token) + `" GovLength="` + itoa(encoder.GovLength) + `" Profile="` + escapeXML(encoder.Profile) + `">` +
			media2VideoEncoderContent(encoder) + `</tr2:Configurations>`
	}
	return `<tr2:GetVideoEncoderConfigurationsResponse>` + body + `</tr2:GetVideoEncoderConfigurationsResponse>`
}

func buildVideoEncoderOptions(config normalizedConfig, media mediaVersion) string {
	if media == mediaVer20 && len(config.MediaInventory.Media2EncoderOptions) > 0 {
		var body string
		for _, option := range config.MediaInventory.Media2EncoderOptions {
			attributes := ` GovLengthRange="` + itoa(option.GovLengthMin) + ` ` + itoa(option.GovLengthMax) + `"`
			if len(option.FrameRates) > 0 {
				rates := make([]string, 0, len(option.FrameRates))
				for _, rate := range option.FrameRates {
					rates = append(rates, ftoa(rate))
				}
				attributes += ` FrameRatesSupported="` + strings.Join(rates, " ") + `"`
			}
			if len(option.Profiles) > 0 {
				attributes += ` ProfilesSupported="` + escapeXML(strings.Join(option.Profiles, " ")) + `"`
			}
			resolutions := ""
			for _, resolution := range option.Resolutions {
				resolutions += resolutionXML(resolution.Width, resolution.Height)
			}
			body += `<tr2:Options` + attributes + `><tt:Encoding>` + escapeXML(option.Encoding) + `</tt:Encoding>` +
				floatRangeXML("QualityRange", option.QualityMin, option.QualityMax) + resolutions +
				intRangeXML("BitrateRange", option.BitrateMin, option.BitrateMax) + `</tr2:Options>`
		}
		return `<tr2:GetVideoEncoderConfigurationOptionsResponse>` + body + `</tr2:GetVideoEncoderConfigurationOptionsResponse>`
	}
	if media == mediaVer20 {
		options := config.MediaInventory.Media2VideoEncoderOptions
		return `<tr2:GetVideoEncoderConfigurationOptionsResponse>` +
			`<tr2:Options GovLengthRange="` + itoa(options.GovLengthMin) + ` ` + itoa(options.GovLengthMax) + `">` +
			`<tt:Encoding>` + escapeXML(options.Encoding) + `</tt:Encoding>` +
			floatRangeXML("QualityRange", options.QualityMin, options.QualityMax) +
			resolutionXML(options.WidthMax, options.HeightMax) + resolutionXML(options.WidthMin, options.HeightMin) +
			intRangeXML("BitrateRange", options.BitrateMin, options.BitrateMax) +
			`</tr2:Options></tr2:GetVideoEncoderConfigurationOptionsResponse>`
	}
	options := config.MediaInventory.VideoEncoderOptions
	codecOptions := ""
	switch options.Encoding {
	case "JPEG":
		codecOptions = `<tt:JPEG>` + resolutionXML(options.WidthMax, options.HeightMax) + resolutionXML(options.WidthMin, options.HeightMin) +
			intRangeXML("FrameRateRange", options.FrameRateMin, options.FrameRateMax) + intRangeXML("EncodingIntervalRange", 1, 1) + `</tt:JPEG>`
	case "MPEG4":
		codecOptions = `<tt:MPEG4>` + resolutionXML(options.WidthMax, options.HeightMax) + resolutionXML(options.WidthMin, options.HeightMin) +
			intRangeXML("GovLengthRange", options.GovLengthMin, options.GovLengthMax) + intRangeXML("FrameRateRange", options.FrameRateMin, options.FrameRateMax) +
			intRangeXML("EncodingIntervalRange", 1, 1) + `<tt:Mpeg4ProfilesSupported>SP</tt:Mpeg4ProfilesSupported></tt:MPEG4>`
	default:
		codecOptions = `<tt:H264>` + resolutionXML(options.WidthMax, options.HeightMax) + resolutionXML(options.WidthMin, options.HeightMin) +
			intRangeXML("GovLengthRange", options.GovLengthMin, options.GovLengthMax) + intRangeXML("FrameRateRange", options.FrameRateMin, options.FrameRateMax) +
			intRangeXML("EncodingIntervalRange", 1, 1) + `<tt:H264ProfilesSupported>Main</tt:H264ProfilesSupported></tt:H264>`
	}
	return `<trt:GetVideoEncoderConfigurationOptionsResponse><trt:Options>` +
		intRangeXML("QualityRange", options.QualityMin, options.QualityMax) + codecOptions +
		`</trt:Options></trt:GetVideoEncoderConfigurationOptionsResponse>`
}

func buildAudioEncoderConfigurations(config normalizedConfig, media mediaVersion, request mediaRequest) string {
	if media == mediaVer20 {
		return buildAudioEncoder2Configurations(config, request)
	}
	var body string
	for _, audio := range config.MediaInventory.AudioEncoders {
		if !audioEncoderSelected(config.MediaInventory, audio.Token, request) {
			continue
		}
		body += `<trt:Configurations token="` + escapeXML(audio.Token) + `">` +
			`<tt:Name>` + escapeXML(audio.Name) + `</tt:Name><tt:UseCount>` + itoa(audio.UseCount) + `</tt:UseCount>` +
			`<tt:Encoding>` + escapeXML(audio.Encoding) + `</tt:Encoding>` +
			`<tt:Bitrate>` + itoa(audio.Bitrate) + `</tt:Bitrate><tt:SampleRate>` + itoa(audio.SampleRate) + `</tt:SampleRate>` +
			disabledMulticastXML() + `<tt:SessionTimeout>PT60S</tt:SessionTimeout></trt:Configurations>`
	}
	return `<trt:GetAudioEncoderConfigurationsResponse>` + body + `</trt:GetAudioEncoderConfigurationsResponse>`
}

func buildAudioEncoder2Configurations(config normalizedConfig, request mediaRequest) string {
	var body string
	for _, audio := range config.MediaInventory.Media2AudioEncoders {
		if !audioEncoder2Selected(config.MediaInventory, audio.Token, request) {
			continue
		}
		body += `<tr2:Configurations token="` + escapeXML(audio.Token) + `">` +
			`<tt:Name>` + escapeXML(audio.Name) + `</tt:Name><tt:UseCount>` + itoa(audio.UseCount) + `</tt:UseCount>` +
			`<tt:Encoding>` + escapeXML(audio.Encoding) + `</tt:Encoding>` +
			`<tt:Bitrate>` + itoa(audio.Bitrate) + `</tt:Bitrate><tt:SampleRate>` + itoa(audio.SampleRate) + `</tt:SampleRate></tr2:Configurations>`
	}
	return `<tr2:GetAudioEncoderConfigurationsResponse>` + body + `</tr2:GetAudioEncoderConfigurationsResponse>`
}

func buildStreamURIResponse(media mediaVersion, origin requestOrigin, streamPath string, rtspPort int, request mediaRequest) string {
	uri := origin.streamURI(streamPath, rtspPort, request.profileToken, request.protocol)
	if media == mediaVer20 {
		return `<tr2:GetStreamUriResponse><tr2:Uri>` + escapeXML(uri) + `</tr2:Uri></tr2:GetStreamUriResponse>`
	}
	return `<trt:GetStreamUriResponse><trt:MediaUri><tt:Uri>` + escapeXML(uri) + `</tt:Uri>` +
		`<tt:InvalidAfterConnect>false</tt:InvalidAfterConnect><tt:InvalidAfterReboot>false</tt:InvalidAfterReboot>` +
		`<tt:Timeout>PT0S</tt:Timeout></trt:MediaUri></trt:GetStreamUriResponse>`
}

func buildSnapshotURIResponse(media mediaVersion, origin requestOrigin, snapshotPath, configuredQuery string, request mediaRequest) string {
	uri := origin.snapshotURI(snapshotPath, request.profileToken, configuredQuery)
	if media == mediaVer20 {
		return `<tr2:GetSnapshotUriResponse><tr2:Uri>` + escapeXML(uri) + `</tr2:Uri></tr2:GetSnapshotUriResponse>`
	}
	return `<trt:GetSnapshotUriResponse><trt:MediaUri><tt:Uri>` + escapeXML(uri) + `</tt:Uri>` +
		`<tt:InvalidAfterConnect>false</tt:InvalidAfterConnect><tt:InvalidAfterReboot>false</tt:InvalidAfterReboot>` +
		`<tt:Timeout>PT0S</tt:Timeout></trt:MediaUri></trt:GetSnapshotUriResponse>`
}

func disabledMulticastXML() string {
	return `<tt:Multicast><tt:Address><tt:Type>IPv4</tt:Type><tt:IPv4Address>0.0.0.0</tt:IPv4Address></tt:Address>` +
		`<tt:Port>0</tt:Port><tt:TTL>1</tt:TTL><tt:AutoStart>false</tt:AutoStart></tt:Multicast>`
}

func resolutionXML(width, height int) string {
	return `<tt:ResolutionsAvailable><tt:Width>` + itoa(width) + `</tt:Width><tt:Height>` + itoa(height) + `</tt:Height></tt:ResolutionsAvailable>`
}

func intRangeXML(local string, minimum, maximum int) string {
	return `<tt:` + local + `><tt:Min>` + itoa(minimum) + `</tt:Min><tt:Max>` + itoa(maximum) + `</tt:Max></tt:` + local + `>`
}

func floatRangeXML(local string, minimum, maximum float64) string {
	return `<tt:` + local + `><tt:Min>` + ftoa(minimum) + `</tt:Min><tt:Max>` + ftoa(maximum) + `</tt:Max></tt:` + local + `>`
}

func ftoa(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func videoEncoderSelected(inventory MediaInventory, token string, request mediaRequest) bool {
	if request.configurationToken != "" && request.configurationToken != token {
		return false
	}
	if request.profileToken == "" {
		return true
	}
	profile, ok := findMediaProfile(inventory, mediaVer10, request.profileToken)
	return ok && profile.media1VideoEncoderToken() == token
}

func audioEncoderSelected(inventory MediaInventory, token string, request mediaRequest) bool {
	if request.configurationToken != "" && request.configurationToken != token {
		return false
	}
	if request.profileToken == "" {
		return true
	}
	profile, ok := findMediaProfile(inventory, mediaVer10, request.profileToken)
	return ok && profile.media1AudioEncoderToken() == token
}

func findMediaProfile(inventory MediaInventory, media mediaVersion, token string) (MediaProfileData, bool) {
	var match MediaProfileData
	found := false
	for _, profile := range mediaProfiles(inventory, media) {
		if profile.Token != token {
			continue
		}
		if found {
			return MediaProfileData{}, false
		}
		match, found = profile, true
	}
	return match, found
}

func videoEncoder2Selected(inventory MediaInventory, token string, request mediaRequest) bool {
	if request.configurationToken != "" && request.configurationToken != token {
		return false
	}
	if request.profileToken == "" {
		return true
	}
	profile, ok := findMediaProfile(inventory, mediaVer20, request.profileToken)
	return ok && profile.VideoEncoderToken == token
}

func audioEncoder2Selected(inventory MediaInventory, token string, request mediaRequest) bool {
	if request.configurationToken != "" && request.configurationToken != token {
		return false
	}
	if request.profileToken == "" {
		return true
	}
	profile, ok := findMediaProfile(inventory, mediaVer20, request.profileToken)
	return ok && profile.AudioEncoderToken == token
}

func mediaVersionForAction(action string) mediaVersion {
	switch action {
	case ActionMedia1GetProfiles,
		ActionMedia1GetVideoSources,
		ActionMedia1GetVideoEncoderConfigurations,
		ActionMedia1GetVideoEncoderConfigurationOptions,
		ActionMedia1GetAudioEncoderConfigurations,
		ActionMedia1GetStreamURI,
		ActionMedia1GetSnapshotURI:
		return mediaVer10
	default:
		return mediaVer20
	}
}
