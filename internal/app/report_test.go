package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func fixedReportSources() reportSources {
	brightness := 50.0
	offset := 0.25
	return reportSources{
		device: ReportDevice{DisplayName: "Gate | north", Endpoint: "http://192.0.2.10/onvif/device_service"},
		inventory: func() (Inventory, error) {
			return Inventory{DeviceInformation: &DeviceInformationDTO{Manufacturer: "Acme", Model: "Cam\n1"}, Hostname: &HostnameDTO{Name: "cam1"}}, nil
		},
		profiles: func() (MediaProfilesResult, error) {
			return MediaProfilesResult{Profiles: []MediaProfileDTO{
				{Token: "sub", Video: &VideoConfigDTO{Codec: "H264", Width: 640, Height: 360}},
				{Token: "main", Name: "Main", Video: &VideoConfigDTO{Codec: "H264", Profile: "High", Width: 1920, Height: 1080, FrameRateFPS: 25, BitrateKbps: 4096, GOPLength: 50},
					Stream: &StreamURIDTO{TCPURI: "rtsp://192.0.2.10/main"}},
			}}, nil
		},
		imaging: func(token string) (ImagingView, error) {
			return ImagingView{Settings: ImagingSettingsDTO{Brightness: &brightness, IrCutFilter: "ON"}}, nil
		},
		time: func() (TimeView, error) {
			return TimeView{DateTimeType: "NTP", TimeZone: "UTC0", NTPServers: []string{"192.0.2.1"}, ClockOffsetSeconds: &offset, UncertaintySeconds: 1}, nil
		},
		network: func() (NetworkView, error) {
			return NetworkView{}, newError(CategoryProtocol, "the camera reports no enabled IPv4 interface", nil)
		},
		users: func() (UsersView, error) {
			return UsersView{Users: []UserDTO{{Username: "zed", Level: "User"}, {Username: "admin", Level: "Administrator"}}, DefaultCredentialWarning: true}, nil
		},
	}
}

func TestBuildReportIsDeterministicAndRecordsGaps(t *testing.T) {
	t.Parallel()
	options := ReportOptions{Generator: "ovm test", Now: func() time.Time { return time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC) }}
	first, _ := json.Marshal(buildReport(fixedReportSources(), options))
	second, _ := json.Marshal(buildReport(fixedReportSources(), options))
	if string(first) != string(second) {
		t.Fatal("two reports of the same state differ")
	}
	report := buildReport(fixedReportSources(), options)
	if report.Profiles[0].Token != "main" || report.Imaging.ProfileToken != "main" || report.Users.Accounts[0].Username != "admin" {
		t.Fatalf("report ordering = %+v", report)
	}
	if len(report.Unavailable) != 1 || report.Unavailable[0].Section != "network" || report.Network != nil {
		t.Fatalf("gaps = %+v", report.Unavailable)
	}
	if report.Device.Model != "Cam\n1" || report.GeneratedAt != "2026-10-10T12:00:00Z" {
		t.Fatalf("device = %+v", report.Device)
	}
}

func TestRenderReportMarkdownEscapesCameraValues(t *testing.T) {
	t.Parallel()
	report := buildReport(fixedReportSources(), ReportOptions{Generator: "ovm test", Now: time.Now,
		Stability: &StabilityReport{Verdict: StabilityFail, Passed: 1, CyclesCompleted: 2, CyclesRequested: 2, CycleSeconds: 20,
			Cycles: []StabilityCycle{{Index: 1, Passed: true}, {Index: 2, Error: "the stream could not be reached"}}}})
	markdown := RenderReportMarkdown(report)
	for _, want := range []string{
		`# Device report: Gate \| north`, "| Model | Cam 1 |", "| main | Main | H264 High | 1920x1080 | 25 | 4096 | 50 | rtsp://192.0.2.10/main |",
		"| IR cut filter | Day (ON) |", "**Warning:**", "- Cycle 2: the stream could not be reached", "- network: the camera reports no enabled IPv4 interface",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown lacks %q:\n%s", want, markdown)
		}
	}
}
