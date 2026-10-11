package app

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ReportSchema identifies the device report format.
const ReportSchema = "ovm.device-report/v1"

// ReportDevice identifies the camera. It holds no credentials.
type ReportDevice struct {
	DisplayName     string `json:"display_name,omitempty"`
	Endpoint        string `json:"endpoint"`
	Manufacturer    string `json:"manufacturer,omitempty"`
	Model           string `json:"model,omitempty"`
	FirmwareVersion string `json:"firmware_version,omitempty"`
	SerialNumber    string `json:"serial_number,omitempty"`
	HardwareID      string `json:"hardware_id,omitempty"`
	Hostname        string `json:"hostname,omitempty"`
}

// ReportProfile is a media profile with credential- and query-free URIs.
type ReportProfile struct {
	Token        string  `json:"token"`
	Name         string  `json:"name,omitempty"`
	Codec        string  `json:"codec,omitempty"`
	CodecProfile string  `json:"codec_profile,omitempty"`
	Width        int     `json:"width,omitempty"`
	Height       int     `json:"height,omitempty"`
	FrameRateFPS float64 `json:"frame_rate_fps,omitempty"`
	BitrateKbps  int     `json:"bitrate_kbps,omitempty"`
	GOPLength    int     `json:"gop_length,omitempty"`
	RTSPURI      string  `json:"rtsp_uri,omitempty"`
	SnapshotURI  string  `json:"snapshot_uri,omitempty"`
}

// ReportImaging is the image state of the first profile's video source.
type ReportImaging struct {
	ProfileToken string             `json:"profile_token"`
	Settings     ImagingSettingsDTO `json:"settings"`
}

// ReportUsers lists account names and levels, never passwords.
type ReportUsers struct {
	Accounts                 []UserDTO `json:"accounts"`
	DefaultCredentialWarning bool      `json:"default_credential_warning"`
}

// ReportGap records a section the camera could not provide.
type ReportGap struct {
	Section string `json:"section"`
	Reason  string `json:"reason"`
}

// DeviceReport is a deterministic, credential-free snapshot of a camera:
// the same camera state and inputs always render the same bytes.
type DeviceReport struct {
	Schema      string            `json:"schema"`
	Generator   string            `json:"generator"`
	GeneratedAt string            `json:"generated_at"`
	Device      ReportDevice      `json:"device"`
	Profiles    []ReportProfile   `json:"profiles"`
	Imaging     *ReportImaging    `json:"imaging,omitempty"`
	Time        *TimeView         `json:"time,omitempty"`
	Network     *NetworkView      `json:"network,omitempty"`
	Users       *ReportUsers      `json:"users,omitempty"`
	Clock       *ClockCheckResult `json:"clock_check,omitempty"`
	Stability   *StabilityReport  `json:"stability,omitempty"`
	Unavailable []ReportGap       `json:"unavailable"`
}

// ReportOptions adds results the technician already measured.
type ReportOptions struct {
	Generator string
	Clock     *ClockCheckResult
	Stability *StabilityReport
	Now       func() time.Time
}

// reportSources gathers the camera state; Manager implements it and tests
// substitute it.
type reportSources struct {
	device    ReportDevice
	inventory func() (Inventory, error)
	profiles  func() (MediaProfilesResult, error)
	imaging   func(profileToken string) (ImagingView, error)
	time      func() (TimeView, error)
	network   func() (NetworkView, error)
	users     func() (UsersView, error)
}

// buildReport reads every section, recording the ones that fail.
func buildReport(sources reportSources, options ReportOptions) DeviceReport {
	now := time.Now
	if options.Now != nil {
		now = options.Now
	}
	report := DeviceReport{
		Schema: ReportSchema, Generator: options.Generator, GeneratedAt: now().UTC().Format(time.RFC3339),
		Device: sources.device, Profiles: []ReportProfile{}, Unavailable: []ReportGap{},
		Clock: options.Clock, Stability: options.Stability,
	}
	gap := func(section string, err error) {
		report.Unavailable = append(report.Unavailable, ReportGap{Section: section, Reason: reportReason(err)})
	}

	if inventory, err := sources.inventory(); err != nil && inventory.DeviceInformation == nil {
		gap("device_information", err)
	} else {
		if info := inventory.DeviceInformation; info != nil {
			report.Device.Manufacturer, report.Device.Model = info.Manufacturer, info.Model
			report.Device.FirmwareVersion, report.Device.SerialNumber, report.Device.HardwareID = info.FirmwareVersion, info.SerialNumber, info.HardwareID
		}
		if inventory.Hostname != nil {
			report.Device.Hostname = inventory.Hostname.Name
		}
	}

	profiles, err := sources.profiles()
	if err != nil {
		gap("profiles", err)
	}
	for _, profile := range profiles.Profiles {
		entry := ReportProfile{Token: profile.Token, Name: profile.Name}
		if video := profile.Video; video != nil {
			entry.Codec, entry.CodecProfile = video.Codec, video.Profile
			entry.Width, entry.Height, entry.FrameRateFPS = video.Width, video.Height, video.FrameRateFPS
			entry.BitrateKbps, entry.GOPLength = video.BitrateKbps, video.GOPLength
		}
		if profile.Stream != nil {
			entry.RTSPURI = profile.Stream.TCPURI
		}
		if profile.Snapshot != nil {
			entry.SnapshotURI = profile.Snapshot.URI
		}
		report.Profiles = append(report.Profiles, entry)
	}
	slices.SortFunc(report.Profiles, func(a, b ReportProfile) int { return strings.Compare(a.Token, b.Token) })

	if len(report.Profiles) > 0 {
		token := report.Profiles[0].Token
		if view, err := sources.imaging(token); err != nil {
			gap("imaging", err)
		} else {
			report.Imaging = &ReportImaging{ProfileToken: token, Settings: view.Settings}
		}
	}
	if view, err := sources.time(); err != nil {
		gap("time", err)
	} else {
		report.Time = &view
	}
	if view, err := sources.network(); err != nil {
		gap("network", err)
	} else {
		report.Network = &view
	}
	if view, err := sources.users(); err != nil {
		gap("users", err)
	} else {
		accounts := slices.Clone(view.Users)
		slices.SortFunc(accounts, func(a, b UserDTO) int { return strings.Compare(a.Username, b.Username) })
		report.Users = &ReportUsers{Accounts: accounts, DefaultCredentialWarning: view.DefaultCredentialWarning}
	}
	if report.Users != nil && report.Users.Accounts == nil {
		report.Users.Accounts = []UserDTO{}
	}
	return report
}

func reportReason(err error) string {
	if message := stabilityErrorMessage(err); message != "the cycle failed" {
		return message
	}
	return "the camera did not provide this section"
}

// BuildReport reads a stored device and assembles its report. Sections the
// camera does not provide are listed as unavailable rather than failing the
// whole report.
func (m *Manager) BuildReport(ctx context.Context, deviceID string, options ReportOptions) (DeviceReport, error) {
	if err := m.ensureOpen(); err != nil {
		return DeviceReport{}, err
	}
	device, resolveErr := m.resolveDevice(deviceID)
	if resolveErr != nil {
		return DeviceReport{}, resolveErr
	}
	unavailable := func(area string) error { return newManagerError(CategoryInternal, area+" is not available", nil) }
	sources := reportSources{
		device: ReportDevice{DisplayName: device.DisplayName, Endpoint: device.Endpoint},
		inventory: func() (Inventory, error) {
			return withDevice(ctx, m, deviceID, true, "inventory", func(host string, credentials *Credentials) (Inventory, error) {
				return m.inventory.Inventory(ctx, InventoryRequest{Host: host, Credentials: credentials})
			})
		},
		profiles: func() (MediaProfilesResult, error) { return m.Profiles(ctx, deviceID) },
		imaging: func(token string) (ImagingView, error) {
			if m.imaging == nil {
				return ImagingView{}, unavailable("image configuration")
			}
			return m.ReadImaging(ctx, deviceID, token)
		},
		time:    func() (TimeView, error) { return m.ReadTime(ctx, deviceID) },
		network: func() (NetworkView, error) { return m.ReadNetwork(ctx, deviceID) },
		users:   func() (UsersView, error) { return m.ReadUsers(ctx, deviceID) },
	}
	return buildReport(sources, options), nil
}

// RenderReportMarkdown renders a report as Markdown. Values from the camera
// are escaped so they cannot break the table layout.
func RenderReportMarkdown(report DeviceReport) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	row := func(cells ...string) {
		for i := range cells {
			cells[i] = markdownCell(cells[i])
		}
		line("| %s |", strings.Join(cells, " | "))
	}
	title := report.Device.DisplayName
	if title == "" {
		title = report.Device.Model
	}
	if title == "" {
		title = "Camera"
	}
	line("# Device report: %s", markdownCell(title))
	line("")
	line("Generated %s by %s (schema `%s`). This report contains no passwords.", report.GeneratedAt, markdownCell(report.Generator), report.Schema)
	line("")
	line("## Device")
	line("")
	row("Field", "Value")
	row("---", "---")
	device := report.Device
	for _, field := range [][2]string{
		{"Endpoint", device.Endpoint}, {"Manufacturer", device.Manufacturer}, {"Model", device.Model},
		{"Firmware", device.FirmwareVersion}, {"Serial number", device.SerialNumber},
		{"Hardware ID", device.HardwareID}, {"Hostname", device.Hostname},
	} {
		if field[1] != "" {
			row(field[0], field[1])
		}
	}
	line("")
	line("## Media profiles")
	line("")
	if len(report.Profiles) == 0 {
		line("No profiles were reported.")
	} else {
		row("Token", "Name", "Codec", "Resolution", "FPS", "Bitrate (kbps)", "GOP", "RTSP URI")
		row("---", "---", "---", "---", "---", "---", "---", "---")
		for _, p := range report.Profiles {
			resolution := ""
			if p.Width > 0 {
				resolution = fmt.Sprintf("%dx%d", p.Width, p.Height)
			}
			codec := strings.TrimSpace(p.Codec + " " + p.CodecProfile)
			row(p.Token, p.Name, codec, resolution, formatNumber(p.FrameRateFPS), intOrEmpty(p.BitrateKbps), intOrEmpty(p.GOPLength), p.RTSPURI)
		}
	}
	if report.Imaging != nil {
		line("")
		line("## Image (profile %s)", markdownCell(report.Imaging.ProfileToken))
		line("")
		settings := report.Imaging.Settings
		row("Setting", "Value")
		row("---", "---")
		row("IR cut filter", irCutLabel(settings.IrCutFilter))
		for _, field := range []struct {
			name  string
			value *float64
		}{{"Brightness", settings.Brightness}, {"Color saturation", settings.ColorSaturation}, {"Contrast", settings.Contrast}, {"Sharpness", settings.Sharpness}} {
			if field.value != nil {
				row(field.name, formatNumber(*field.value))
			}
		}
	}
	if t := report.Time; t != nil {
		line("")
		line("## Time")
		line("")
		row("Setting", "Value")
		row("---", "---")
		row("Mode", t.DateTimeType)
		row("Time zone", t.TimeZone)
		row("NTP servers", strings.Join(t.NTPServers, ", "))
		if t.ClockOffsetSeconds != nil {
			row("Clock offset", fmt.Sprintf("%+.1f s ± %.1f s", *t.ClockOffsetSeconds, t.UncertaintySeconds))
		}
	}
	if c := report.Clock; c != nil {
		line("")
		line("## Clock sync check")
		line("")
		line("Verdict: **%s** (tolerance ± %s s, checked %s).", c.Verdict, formatNumber(c.ToleranceSeconds), c.CheckedAt)
		line("")
		row("Source", "Offset (s)", "Uncertainty (s)")
		row("---", "---", "---")
		for _, estimate := range []*ClockEstimate{c.ONVIF, c.RTCP} {
			if estimate != nil {
				row(strings.ToUpper(estimate.Source), fmt.Sprintf("%+.3f", estimate.OffsetSeconds), fmt.Sprintf("%.3f", estimate.UncertaintySeconds))
			}
		}
		if c.RTCPUnavailable != "" {
			line("")
			line("RTCP: %s.", markdownCell(c.RTCPUnavailable))
		}
	}
	if n := report.Network; n != nil {
		line("")
		line("## Network")
		line("")
		row("Setting", "Value")
		row("---", "---")
		mode := "Static"
		if n.DHCP {
			mode = "DHCP"
		}
		row("Interface", strings.TrimSpace(n.InterfaceName+" "+n.HwAddress))
		row("Addressing", mode)
		if n.Address != "" {
			row("Address", n.Address+"/"+strconv.Itoa(n.PrefixLength))
		}
		row("Gateways", strings.Join(n.Gateways, ", "))
		row("DNS servers", strings.Join(n.DNSServers, ", "))
	}
	if u := report.Users; u != nil {
		line("")
		line("## Accounts")
		line("")
		if u.DefaultCredentialWarning {
			line("**Warning:** ovm signs in with a published factory-default credential. Change it.")
			line("")
		}
		row("Username", "Level")
		row("---", "---")
		for _, account := range u.Accounts {
			row(account.Username, account.Level)
		}
	}
	if s := report.Stability; s != nil {
		line("")
		line("## Stability test")
		line("")
		line("Verdict: **%s**: %d of %d cycles passed (%d requested, %s s each).", s.Verdict, s.Passed, s.CyclesCompleted, s.CyclesRequested, formatNumber(s.CycleSeconds))
		line("")
		row("Metric", "Value")
		row("---", "---")
		row("Profile", s.ProfileToken+" ("+s.Transport+")")
		row("Time to first frame (min / median / max)", fmt.Sprintf("%d / %d / %d ms", s.MinTTFFMillis, s.MedianTTFFMillis, s.MaxTTFFMillis))
		row("Mean frame rate", formatNumber(s.MeanFPS)+" fps")
		row("Mean bitrate", formatNumber(s.MeanBitrateKbps)+" kbps")
		row("Lost RTP packets", strconv.FormatUint(s.LostPackets, 10))
		var failures []StabilityCycle
		for _, cycle := range s.Cycles {
			if !cycle.Passed {
				failures = append(failures, cycle)
			}
		}
		if len(failures) > 0 {
			line("")
			line("Failed cycles:")
			line("")
			for _, cycle := range failures {
				line("- Cycle %d: %s", cycle.Index, markdownCell(cycle.Error))
			}
		}
	}
	if len(report.Unavailable) > 0 {
		line("")
		line("## Not available")
		line("")
		for _, gap := range report.Unavailable {
			line("- %s: %s", gap.Section, markdownCell(gap.Reason))
		}
	}
	return b.String()
}

func markdownCell(value string) string {
	value = strings.NewReplacer("\r", " ", "\n", " ", "|", `\|`, "`", "'").Replace(value)
	return strings.TrimSpace(value)
}

func formatNumber(value float64) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func intOrEmpty(value int) string {
	if value == 0 {
		return ""
	}
	return strconv.Itoa(value)
}

func irCutLabel(mode string) string {
	switch mode {
	case "ON":
		return "Day (ON)"
	case "OFF":
		return "Night (OFF)"
	case "AUTO":
		return "Auto"
	default:
		return "Not reported"
	}
}
