package app

import (
	"context"
	"math"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/addodelgrossi/ovm/pkg/onvif/device"
)

// DeviceRequest addresses a camera for device-management configuration.
type DeviceRequest struct {
	Host        string
	Credentials *Credentials
	Timeout     time.Duration
}

// TimeView is the camera's clock configuration. ONVIF reports whole seconds,
// so ClockOffsetSeconds carries at least one second of uncertainty plus the
// request round trip; it supports coarse checks only.
type TimeView struct {
	DateTimeType       string   `json:"date_time_type"`
	TimeZone           string   `json:"time_zone"`
	DaylightSavings    bool     `json:"daylight_savings"`
	DeviceUTC          string   `json:"device_utc,omitempty"`
	ClockOffsetSeconds *float64 `json:"clock_offset_seconds,omitempty"`
	UncertaintySeconds float64  `json:"uncertainty_seconds"`
	NTPFromDHCP        bool     `json:"ntp_from_dhcp"`
	NTPServers         []string `json:"ntp_servers"`
}

// TimeChange is the requested clock configuration. Manual mode sets the
// camera to this computer's UTC time.
type TimeChange struct {
	// Mode is "NTP" or "Manual".
	Mode            string   `json:"mode"`
	NTPServers      []string `json:"ntp_servers"`
	TimeZone        string   `json:"time_zone"`
	DaylightSavings bool     `json:"daylight_savings"`
}

// TimeApplyResult is the verified result of a clock change.
type TimeApplyResult struct {
	Before TimeView       `json:"before"`
	After  TimeView       `json:"after"`
	Report MutationReport `json:"report"`
}

// TimeUseCase reads and changes time and NTP configuration.
type TimeUseCase interface {
	ReadTime(ctx context.Context, request DeviceRequest) (TimeView, error)
	ApplyTime(ctx context.Context, request DeviceRequest, change TimeChange) (TimeApplyResult, error)
}

// NewProductionTime returns the production TimeUseCase.
func NewProductionTime() TimeUseCase { return &productionMedia{} }

// manualSyncTolerance is the read-back tolerance for a manual clock set: one
// second of ONVIF quantization plus request latency.
const manualSyncTolerance = 2.0

// openDevice prepares an authenticated Device session.
func (p *productionMedia) openDevice(ctx context.Context, request DeviceRequest) (*authDeviceSeam, context.Context, context.CancelFunc, error) {
	_, seam, timeout, err := p.prepare(request.Host, request.Timeout, request.Credentials)
	if err != nil {
		return nil, nil, nil, err
	}
	opCtx, cancel := context.WithTimeout(ctx, timeout)
	seam.negotiate(opCtx)
	return seam, opCtx, cancel, nil
}

func (p *productionMedia) now() time.Time {
	if p.clock != nil {
		return p.clock()
	}
	return time.Now()
}

func (p *productionMedia) readTime(ctx context.Context, seam *authDeviceSeam) (TimeView, error) {
	sampled := p.now()
	current, err := seam.SystemDateAndTime(ctx)
	if err != nil {
		return TimeView{}, err
	}
	received := p.now()
	view := TimeView{
		DateTimeType: current.DateTimeType, TimeZone: current.TimeZone, DaylightSavings: current.DaylightSavings,
		NTPServers: []string{},
	}
	if current.UTCValid {
		view.DeviceUTC = current.UTC.UTC().Format(time.RFC3339)
		midpoint := sampled.Add(received.Sub(sampled) / 2)
		offset := current.UTC.Sub(midpoint).Seconds()
		view.ClockOffsetSeconds = &offset
		view.UncertaintySeconds = 1 + received.Sub(sampled).Seconds()/2
	}
	var ntp device.NTPInformation
	err = seam.call(ctx, func(ctx context.Context, opts []device.RequestOptions) error {
		var callErr error
		ntp, callErr = seam.client.GetNTP(ctx, opts...)
		return callErr
	})
	if err != nil {
		return TimeView{}, err
	}
	view.NTPFromDHCP = ntp.FromDHCP
	servers := ntp.Manual
	if ntp.FromDHCP {
		servers = ntp.DHCPServers
	}
	for _, server := range servers {
		view.NTPServers = append(view.NTPServers, server.Address)
	}
	return view, nil
}

// ReadTime returns the clock configuration and the measured offset.
func (p *productionMedia) ReadTime(ctx context.Context, request DeviceRequest) (TimeView, error) {
	seam, opCtx, cancel, err := p.openDevice(ctx, request)
	if err != nil {
		return TimeView{}, err
	}
	defer cancel()
	view, err := p.readTime(opCtx, seam)
	if err != nil {
		return TimeView{}, mapConfigError(err)
	}
	return view, nil
}

// ApplyTime configures NTP or sets the clock manually, then reads it back.
func (p *productionMedia) ApplyTime(ctx context.Context, request DeviceRequest, change TimeChange) (TimeApplyResult, error) {
	hosts, err := validateTimeChange(change)
	if err != nil {
		return TimeApplyResult{}, err
	}
	seam, opCtx, cancel, err := p.openDevice(ctx, request)
	if err != nil {
		return TimeApplyResult{}, err
	}
	defer cancel()
	before, err := p.readTime(opCtx, seam)
	if err != nil {
		return TimeApplyResult{}, mapConfigError(err)
	}
	result := TimeApplyResult{Before: before}
	if change.Mode == "NTP" {
		err = seam.call(opCtx, func(ctx context.Context, opts []device.RequestOptions) error {
			return seam.client.SetNTP(ctx, false, hosts, opts...)
		})
		if err != nil {
			return result, mapSetError(err, "the camera rejected the NTP servers")
		}
	}
	err = seam.call(opCtx, func(ctx context.Context, opts []device.RequestOptions) error {
		return seam.client.SetSystemDateAndTime(ctx, device.SystemDateTimeChange{
			DateTimeType: change.Mode, TimeZone: change.TimeZone, DaylightSavings: change.DaylightSavings, UTC: p.now().UTC(),
		}, opts...)
	})
	if err != nil {
		return result, mapSetError(err, "the camera rejected the date and time settings")
	}

	requested := timeFields(TimeView{
		DateTimeType: change.Mode, TimeZone: change.TimeZone, DaylightSavings: change.DaylightSavings, NTPServers: change.NTPServers,
	}, change, true)
	var after TimeView
	afterFields, ok := readBack(opCtx, p.readback(), requested, func(ctx context.Context) (fieldValues, error) {
		current, err := p.readTime(ctx, seam)
		if err != nil {
			return fieldValues{}, err
		}
		after = current
		return timeFields(current, change, false), nil
	})
	if !ok {
		result.Report = MutationReport{Outcome: OutcomeDisconnected, Changes: []FieldChange{}}
		return result, nil
	}
	result.After = after
	result.Report = compareMutation(timeFields(before, change, false), requested, afterFields)
	return result, nil
}

// timeFields presents a clock configuration. In Manual mode the clock is
// reported as synchronized when the measured offset is within tolerance.
func timeFields(view TimeView, change TimeChange, requested bool) fieldValues {
	fields := newFieldValues()
	fields.set("date_time_type", view.DateTimeType)
	fields.set("time_zone", view.TimeZone)
	fields.set("daylight_savings", strconv.FormatBool(view.DaylightSavings))
	if change.Mode == "NTP" {
		fields.set("ntp_servers", strings.Join(view.NTPServers, ", "))
	}
	if change.Mode == "Manual" {
		synchronized := requested || (view.ClockOffsetSeconds != nil && math.Abs(*view.ClockOffsetSeconds) <= manualSyncTolerance)
		fields.set("clock_synchronized", strconv.FormatBool(synchronized))
	}
	return fields
}

func validateTimeChange(change TimeChange) ([]device.NetworkHost, error) {
	if change.Mode != "NTP" && change.Mode != "Manual" {
		return nil, usageError("the time mode must be NTP or Manual")
	}
	if len(change.TimeZone) > 64 || strings.ContainsAny(change.TimeZone, "<>&\"'\x00\r\n") {
		return nil, usageError("the time zone is not valid")
	}
	if change.Mode == "Manual" {
		return nil, nil
	}
	if len(change.NTPServers) == 0 || len(change.NTPServers) > 4 {
		return nil, usageError("between one and four NTP servers are required")
	}
	hosts := make([]device.NetworkHost, 0, len(change.NTPServers))
	for _, server := range change.NTPServers {
		server = strings.TrimSpace(server)
		if address, err := netip.ParseAddr(server); err == nil {
			kind := "IPv4"
			if address.Is6() {
				kind = "IPv6"
			}
			hosts = append(hosts, device.NetworkHost{Type: kind, Address: address.String()})
			continue
		}
		if !validDNSName(server) {
			return nil, usageError("an NTP server is not a valid address or host name")
		}
		hosts = append(hosts, device.NetworkHost{Type: "DNS", Address: server})
	}
	if slices.ContainsFunc(change.NTPServers, func(s string) bool { return strings.TrimSpace(s) != s }) {
		return nil, usageError("an NTP server is not a valid address or host name")
	}
	return hosts, nil
}

func validDNSName(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if r != '-' && (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
				return false
			}
		}
	}
	return true
}
