package device

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Device Management configuration operations. They decode with encoding/xml
// using namespace-qualified tags: elements match by namespace URI and local
// name, the SOAP layer has already bounded the payload size and structure,
// and errors never echo values. Passwords are only ever sent, never decoded.
const (
	ActionGetNTP                   = Namespace + "/GetNTP"
	ActionSetNTP                   = Namespace + "/SetNTP"
	ActionSetSystemDateAndTime     = Namespace + "/SetSystemDateAndTime"
	ActionGetUsers                 = Namespace + "/GetUsers"
	ActionCreateUsers              = Namespace + "/CreateUsers"
	ActionSetUser                  = Namespace + "/SetUser"
	ActionDeleteUsers              = Namespace + "/DeleteUsers"
	ActionSetNetworkInterfaces     = Namespace + "/SetNetworkInterfaces"
	ActionGetNetworkDefaultGateway = Namespace + "/GetNetworkDefaultGateway"
	ActionSetNetworkDefaultGateway = Namespace + "/SetNetworkDefaultGateway"
	ActionGetDNS                   = Namespace + "/GetDNS"
	ActionSetDNS                   = Namespace + "/SetDNS"
	ActionSystemReboot             = Namespace + "/SystemReboot"
	ActionSetSystemFactoryDefault  = Namespace + "/SetSystemFactoryDefault"
	ActionGetSystemLog             = Namespace + "/GetSystemLog"
)

// ErrInvalidInput reports a caller value that cannot be sent safely. Its
// message never echoes the value.
var ErrInvalidInput = errors.New("device: invalid request input")

// NetworkHost is tt:NetworkHost: an IPv4 or IPv6 address or a DNS name.
type NetworkHost struct {
	// Type is "IPv4", "IPv6", or "DNS".
	Type    string
	Address string
}

// NTPInformation is tt:NTPInformation.
type NTPInformation struct {
	FromDHCP bool
	// DHCPServers are the servers learned from DHCP (tt:NTPFromDHCP).
	DHCPServers []NetworkHost
	// Manual are the configured servers (tt:NTPManual).
	Manual []NetworkHost
}

// SystemDateTimeChange is the SetSystemDateAndTime request. UTC is sent only
// for Manual mode.
type SystemDateTimeChange struct {
	// DateTimeType is "Manual" or "NTP".
	DateTimeType    string
	DaylightSavings bool
	// TimeZone is a POSIX TZ string; empty omits it.
	TimeZone string
	UTC      time.Time
}

// User is tt:User without a password.
type User struct {
	Username string
	// UserLevel is Administrator, Operator, User, Anonymous, or Extended.
	UserLevel string
}

// UserChange is a user with the password to set.
type UserChange struct {
	Username  string
	Password  string
	UserLevel string
}

// IPv4Change is the IPv4 part of tt:NetworkInterfaceSetConfiguration.
type IPv4Change struct {
	DHCP         bool
	Address      string
	PrefixLength int
}

// DNSInformation is tt:DNSInformation restricted to IP addresses.
type DNSInformation struct {
	FromDHCP      bool
	SearchDomains []string
	FromDHCPAddrs []string
	Manual        []string
}

// FactoryDefault is tt:FactoryDefaultType.
type FactoryDefault string

const (
	// FactoryDefaultSoft keeps network settings.
	FactoryDefaultSoft FactoryDefault = "Soft"
	// FactoryDefaultHard resets everything, including network settings.
	FactoryDefaultHard FactoryDefault = "Hard"
)

// rawCall performs one Device operation and returns the operation XML.
func (c *Client) rawCall(ctx context.Context, action, body string, opts []RequestOptions) ([]byte, error) {
	_, raw, err := c.call(ctx, action, []byte(body), opts)
	return raw, err
}

func wrap(local, inner string) string {
	return `<tds:` + local + ` xmlns:tds="` + Namespace + `" xmlns:tt="` + SchemaNamespace + `">` + inner + `</tds:` + local + `>`
}

// expectEmpty requires an empty response element named local.
func expectEmpty(raw []byte, local string) error {
	var response struct {
		XMLName xml.Name
		Inner   string `xml:",innerxml"`
	}
	if err := xml.Unmarshal(raw, &response); err != nil || response.XMLName.Space != Namespace || response.XMLName.Local != local {
		return fmt.Errorf("%w: %s", ErrUnexpectedElement, local)
	}
	if strings.TrimSpace(response.Inner) != "" {
		return fmt.Errorf("%w: %s", ErrUnexpectedElement, local)
	}
	return nil
}

func decodeResponse(raw []byte, target any, local string) error {
	if err := xml.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("%w: %s", ErrMalformedXML, local)
	}
	return nil
}

type networkHostWire struct {
	Type string `xml:"http://www.onvif.org/ver10/schema Type"`
	IPv4 string `xml:"http://www.onvif.org/ver10/schema IPv4Address"`
	IPv6 string `xml:"http://www.onvif.org/ver10/schema IPv6Address"`
	DNS  string `xml:"http://www.onvif.org/ver10/schema DNSname"`
}

func (w networkHostWire) decode() (NetworkHost, error) {
	switch strings.TrimSpace(w.Type) {
	case "IPv4":
		address, err := netip.ParseAddr(strings.TrimSpace(w.IPv4))
		if err != nil || !address.Is4() {
			return NetworkHost{}, fmt.Errorf("%w: NetworkHost IPv4Address", ErrInvalidField)
		}
		return NetworkHost{Type: "IPv4", Address: address.String()}, nil
	case "IPv6":
		address, err := netip.ParseAddr(strings.TrimSpace(w.IPv6))
		if err != nil || !address.Is6() {
			return NetworkHost{}, fmt.Errorf("%w: NetworkHost IPv6Address", ErrInvalidField)
		}
		return NetworkHost{Type: "IPv6", Address: address.String()}, nil
	case "DNS":
		name := strings.TrimSpace(w.DNS)
		if !validHostname(name) {
			return NetworkHost{}, fmt.Errorf("%w: NetworkHost DNSname", ErrInvalidField)
		}
		return NetworkHost{Type: "DNS", Address: name}, nil
	default:
		return NetworkHost{}, fmt.Errorf("%w: NetworkHost Type", ErrInvalidField)
	}
}

func encodeNetworkHost(element string, host NetworkHost) (string, error) {
	switch host.Type {
	case "IPv4", "IPv6":
		address, err := netip.ParseAddr(host.Address)
		if err != nil || address.Is4() != (host.Type == "IPv4") {
			return "", fmt.Errorf("%w: NTP server address", ErrInvalidInput)
		}
		return `<tds:` + element + `><tt:Type>` + host.Type + `</tt:Type><tt:` + host.Type + `Address>` + address.String() +
			`</tt:` + host.Type + `Address></tds:` + element + `>`, nil
	case "DNS":
		if !validHostname(host.Address) {
			return "", fmt.Errorf("%w: NTP server name", ErrInvalidInput)
		}
		return `<tds:` + element + `><tt:Type>DNS</tt:Type><tt:DNSname>` + host.Address + `</tt:DNSname></tds:` + element + `>`, nil
	default:
		return "", fmt.Errorf("%w: NTP server type", ErrInvalidInput)
	}
}

// validHostname accepts RFC 1123 host names of at most 253 characters.
func validHostname(name string) bool {
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

// GetNTP reads the NTP configuration.
func (c *Client) GetNTP(ctx context.Context, opts ...RequestOptions) (NTPInformation, error) {
	raw, err := c.rawCall(ctx, ActionGetNTP, wrap("GetNTP", ""), opts)
	if err != nil {
		return NTPInformation{}, err
	}
	var response struct {
		XMLName xml.Name `xml:"http://www.onvif.org/ver10/device/wsdl GetNTPResponse"`
		Info    *struct {
			FromDHCP string            `xml:"http://www.onvif.org/ver10/schema FromDHCP"`
			DHCP     []networkHostWire `xml:"http://www.onvif.org/ver10/schema NTPFromDHCP"`
			Manual   []networkHostWire `xml:"http://www.onvif.org/ver10/schema NTPManual"`
		} `xml:"http://www.onvif.org/ver10/device/wsdl NTPInformation"`
	}
	if err := decodeResponse(raw, &response, "GetNTPResponse"); err != nil {
		return NTPInformation{}, err
	}
	if response.Info == nil {
		return NTPInformation{}, fmt.Errorf("%w: NTPInformation", ErrMissingField)
	}
	fromDHCP, err := strconv.ParseBool(strings.TrimSpace(response.Info.FromDHCP))
	if err != nil {
		return NTPInformation{}, fmt.Errorf("%w: NTPInformation FromDHCP", ErrInvalidField)
	}
	info := NTPInformation{FromDHCP: fromDHCP}
	for _, wire := range response.Info.DHCP {
		host, err := wire.decode()
		if err != nil {
			return NTPInformation{}, err
		}
		info.DHCPServers = append(info.DHCPServers, host)
	}
	for _, wire := range response.Info.Manual {
		host, err := wire.decode()
		if err != nil {
			return NTPInformation{}, err
		}
		info.Manual = append(info.Manual, host)
	}
	return info, nil
}

// SetNTP sets the NTP source. Manual servers are ignored when fromDHCP is
// true.
func (c *Client) SetNTP(ctx context.Context, fromDHCP bool, manual []NetworkHost, opts ...RequestOptions) error {
	if len(manual) > 8 {
		return fmt.Errorf("%w: too many NTP servers", ErrInvalidInput)
	}
	inner := `<tds:FromDHCP>` + strconv.FormatBool(fromDHCP) + `</tds:FromDHCP>`
	if !fromDHCP {
		for _, host := range manual {
			encoded, err := encodeNetworkHost("NTPManual", host)
			if err != nil {
				return err
			}
			inner += encoded
		}
	}
	raw, err := c.rawCall(ctx, ActionSetNTP, wrap("SetNTP", inner), opts)
	if err != nil {
		return err
	}
	return expectEmpty(raw, "SetNTPResponse")
}

// SetSystemDateAndTime sets the time mode, time zone, and, in Manual mode,
// the UTC time.
func (c *Client) SetSystemDateAndTime(ctx context.Context, change SystemDateTimeChange, opts ...RequestOptions) error {
	if change.DateTimeType != "Manual" && change.DateTimeType != "NTP" {
		return fmt.Errorf("%w: DateTimeType", ErrInvalidInput)
	}
	if !utf8.ValidString(change.TimeZone) || len(change.TimeZone) > 64 || strings.ContainsAny(change.TimeZone, "<>&\"'\x00\r\n") {
		return fmt.Errorf("%w: TimeZone", ErrInvalidInput)
	}
	inner := `<tds:DateTimeType>` + change.DateTimeType + `</tds:DateTimeType>` +
		`<tds:DaylightSavings>` + strconv.FormatBool(change.DaylightSavings) + `</tds:DaylightSavings>`
	if change.TimeZone != "" {
		inner += `<tds:TimeZone><tt:TZ>` + change.TimeZone + `</tt:TZ></tds:TimeZone>`
	}
	if change.DateTimeType == "Manual" {
		if change.UTC.IsZero() {
			return fmt.Errorf("%w: UTCDateTime", ErrInvalidInput)
		}
		utc := change.UTC.UTC()
		inner += `<tds:UTCDateTime><tt:Time><tt:Hour>` + strconv.Itoa(utc.Hour()) + `</tt:Hour><tt:Minute>` + strconv.Itoa(utc.Minute()) +
			`</tt:Minute><tt:Second>` + strconv.Itoa(utc.Second()) + `</tt:Second></tt:Time><tt:Date><tt:Year>` + strconv.Itoa(utc.Year()) +
			`</tt:Year><tt:Month>` + strconv.Itoa(int(utc.Month())) + `</tt:Month><tt:Day>` + strconv.Itoa(utc.Day()) +
			`</tt:Day></tt:Date></tds:UTCDateTime>`
	}
	raw, err := c.rawCall(ctx, ActionSetSystemDateAndTime, wrap("SetSystemDateAndTime", inner), opts)
	if err != nil {
		return err
	}
	return expectEmpty(raw, "SetSystemDateAndTimeResponse")
}

var userLevels = map[string]bool{"Administrator": true, "Operator": true, "User": true, "Anonymous": true, "Extended": true}

// GetUsers lists accounts. Passwords are never returned by the device and are
// never decoded.
func (c *Client) GetUsers(ctx context.Context, opts ...RequestOptions) ([]User, error) {
	raw, err := c.rawCall(ctx, ActionGetUsers, wrap("GetUsers", ""), opts)
	if err != nil {
		return nil, err
	}
	var response struct {
		XMLName xml.Name `xml:"http://www.onvif.org/ver10/device/wsdl GetUsersResponse"`
		Users   []struct {
			Username  string `xml:"http://www.onvif.org/ver10/schema Username"`
			UserLevel string `xml:"http://www.onvif.org/ver10/schema UserLevel"`
		} `xml:"http://www.onvif.org/ver10/device/wsdl User"`
	}
	if err := decodeResponse(raw, &response, "GetUsersResponse"); err != nil {
		return nil, err
	}
	users := make([]User, 0, len(response.Users))
	for _, wire := range response.Users {
		username, level := strings.TrimSpace(wire.Username), strings.TrimSpace(wire.UserLevel)
		if !validUsername(username) || !userLevels[level] {
			return nil, fmt.Errorf("%w: User", ErrInvalidField)
		}
		users = append(users, User{Username: username, UserLevel: level})
	}
	return users, nil
}

func validUsername(name string) bool {
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 64 {
		return false
	}
	for _, r := range name {
		if r < 0x21 || r == 0x7f || r == '<' || r == '>' || r == '&' || r == '"' || r == '\'' {
			return false
		}
	}
	return true
}

func encodeUsers(users []UserChange) (string, error) {
	if len(users) == 0 {
		return "", fmt.Errorf("%w: no users", ErrInvalidInput)
	}
	var builder strings.Builder
	for _, user := range users {
		if !validUsername(user.Username) || !userLevels[user.UserLevel] || user.UserLevel == "Anonymous" {
			return "", fmt.Errorf("%w: User", ErrInvalidInput)
		}
		if !utf8.ValidString(user.Password) || user.Password == "" || utf8.RuneCountInString(user.Password) > 64 || hasControlRune(user.Password) {
			return "", fmt.Errorf("%w: Password", ErrInvalidInput)
		}
		builder.WriteString(`<tds:User><tt:Username>` + user.Username + `</tt:Username><tt:Password>` + escapeText(user.Password) +
			`</tt:Password><tt:UserLevel>` + user.UserLevel + `</tt:UserLevel></tds:User>`)
	}
	return builder.String(), nil
}

// CreateUsers adds accounts.
func (c *Client) CreateUsers(ctx context.Context, users []UserChange, opts ...RequestOptions) error {
	inner, err := encodeUsers(users)
	if err != nil {
		return err
	}
	raw, err := c.rawCall(ctx, ActionCreateUsers, wrap("CreateUsers", inner), opts)
	if err != nil {
		return err
	}
	return expectEmpty(raw, "CreateUsersResponse")
}

// SetUser changes the password and level of existing accounts.
func (c *Client) SetUser(ctx context.Context, users []UserChange, opts ...RequestOptions) error {
	inner, err := encodeUsers(users)
	if err != nil {
		return err
	}
	raw, err := c.rawCall(ctx, ActionSetUser, wrap("SetUser", inner), opts)
	if err != nil {
		return err
	}
	return expectEmpty(raw, "SetUserResponse")
}

// DeleteUsers removes accounts.
func (c *Client) DeleteUsers(ctx context.Context, usernames []string, opts ...RequestOptions) error {
	if len(usernames) == 0 {
		return fmt.Errorf("%w: no users", ErrInvalidInput)
	}
	var inner strings.Builder
	for _, name := range usernames {
		if !validUsername(name) {
			return fmt.Errorf("%w: Username", ErrInvalidInput)
		}
		inner.WriteString(`<tds:Username>` + name + `</tds:Username>`)
	}
	raw, err := c.rawCall(ctx, ActionDeleteUsers, wrap("DeleteUsers", inner.String()), opts)
	if err != nil {
		return err
	}
	return expectEmpty(raw, "DeleteUsersResponse")
}

// SetNetworkInterfaces changes the IPv4 configuration of an interface and
// reports whether the device needs a reboot to apply it.
func (c *Client) SetNetworkInterfaces(ctx context.Context, interfaceToken string, ipv4 IPv4Change, opts ...RequestOptions) (bool, error) {
	if !validToken(interfaceToken) {
		return false, fmt.Errorf("%w: InterfaceToken", ErrInvalidInput)
	}
	config := `<tt:Enabled>true</tt:Enabled>`
	if ipv4.DHCP {
		config += `<tt:DHCP>true</tt:DHCP>`
	} else {
		address, err := netip.ParseAddr(ipv4.Address)
		if err != nil || !address.Is4() || ipv4.PrefixLength < 1 || ipv4.PrefixLength > 32 {
			return false, fmt.Errorf("%w: IPv4 address", ErrInvalidInput)
		}
		config += `<tt:Manual><tt:Address>` + address.String() + `</tt:Address><tt:PrefixLength>` + strconv.Itoa(ipv4.PrefixLength) +
			`</tt:PrefixLength></tt:Manual><tt:DHCP>false</tt:DHCP>`
	}
	inner := `<tds:InterfaceToken>` + escapeText(interfaceToken) + `</tds:InterfaceToken><tds:NetworkInterface><tt:Enabled>true</tt:Enabled>` +
		`<tt:IPv4>` + config + `</tt:IPv4></tds:NetworkInterface>`
	raw, err := c.rawCall(ctx, ActionSetNetworkInterfaces, wrap("SetNetworkInterfaces", inner), opts)
	if err != nil {
		return false, err
	}
	var response struct {
		XMLName      xml.Name `xml:"http://www.onvif.org/ver10/device/wsdl SetNetworkInterfacesResponse"`
		RebootNeeded string   `xml:"http://www.onvif.org/ver10/device/wsdl RebootNeeded"`
	}
	if err := decodeResponse(raw, &response, "SetNetworkInterfacesResponse"); err != nil {
		return false, err
	}
	reboot, err := strconv.ParseBool(strings.TrimSpace(response.RebootNeeded))
	if err != nil {
		return false, fmt.Errorf("%w: RebootNeeded", ErrInvalidField)
	}
	return reboot, nil
}

// GetNetworkDefaultGateway reads the IPv4 default gateways.
func (c *Client) GetNetworkDefaultGateway(ctx context.Context, opts ...RequestOptions) ([]string, error) {
	raw, err := c.rawCall(ctx, ActionGetNetworkDefaultGateway, wrap("GetNetworkDefaultGateway", ""), opts)
	if err != nil {
		return nil, err
	}
	var response struct {
		XMLName xml.Name `xml:"http://www.onvif.org/ver10/device/wsdl GetNetworkDefaultGatewayResponse"`
		Gateway *struct {
			IPv4 []string `xml:"http://www.onvif.org/ver10/schema IPv4Address"`
		} `xml:"http://www.onvif.org/ver10/device/wsdl NetworkGateway"`
	}
	if err := decodeResponse(raw, &response, "GetNetworkDefaultGatewayResponse"); err != nil {
		return nil, err
	}
	if response.Gateway == nil {
		return nil, fmt.Errorf("%w: NetworkGateway", ErrMissingField)
	}
	return parseIPv4List(response.Gateway.IPv4, "NetworkGateway")
}

// SetNetworkDefaultGateway sets the IPv4 default gateways.
func (c *Client) SetNetworkDefaultGateway(ctx context.Context, gateways []string, opts ...RequestOptions) error {
	var inner strings.Builder
	for _, gateway := range gateways {
		address, err := netip.ParseAddr(gateway)
		if err != nil || !address.Is4() {
			return fmt.Errorf("%w: gateway", ErrInvalidInput)
		}
		inner.WriteString(`<tds:IPv4Address>` + address.String() + `</tds:IPv4Address>`)
	}
	raw, err := c.rawCall(ctx, ActionSetNetworkDefaultGateway, wrap("SetNetworkDefaultGateway", inner.String()), opts)
	if err != nil {
		return err
	}
	return expectEmpty(raw, "SetNetworkDefaultGatewayResponse")
}

type ipAddressWire struct {
	Type string `xml:"http://www.onvif.org/ver10/schema Type"`
	IPv4 string `xml:"http://www.onvif.org/ver10/schema IPv4Address"`
}

// GetDNS reads the DNS configuration (IPv4 servers only).
func (c *Client) GetDNS(ctx context.Context, opts ...RequestOptions) (DNSInformation, error) {
	raw, err := c.rawCall(ctx, ActionGetDNS, wrap("GetDNS", ""), opts)
	if err != nil {
		return DNSInformation{}, err
	}
	var response struct {
		XMLName xml.Name `xml:"http://www.onvif.org/ver10/device/wsdl GetDNSResponse"`
		Info    *struct {
			FromDHCP      string          `xml:"http://www.onvif.org/ver10/schema FromDHCP"`
			SearchDomains []string        `xml:"http://www.onvif.org/ver10/schema SearchDomain"`
			DHCP          []ipAddressWire `xml:"http://www.onvif.org/ver10/schema DNSFromDHCP"`
			Manual        []ipAddressWire `xml:"http://www.onvif.org/ver10/schema DNSManual"`
		} `xml:"http://www.onvif.org/ver10/device/wsdl DNSInformation"`
	}
	if err := decodeResponse(raw, &response, "GetDNSResponse"); err != nil {
		return DNSInformation{}, err
	}
	if response.Info == nil {
		return DNSInformation{}, fmt.Errorf("%w: DNSInformation", ErrMissingField)
	}
	fromDHCP, err := strconv.ParseBool(strings.TrimSpace(response.Info.FromDHCP))
	if err != nil {
		return DNSInformation{}, fmt.Errorf("%w: DNSInformation FromDHCP", ErrInvalidField)
	}
	info := DNSInformation{FromDHCP: fromDHCP, SearchDomains: []string{}}
	for _, domain := range response.Info.SearchDomains {
		domain = strings.TrimSpace(domain)
		if !validHostname(domain) {
			return DNSInformation{}, fmt.Errorf("%w: SearchDomain", ErrInvalidField)
		}
		info.SearchDomains = append(info.SearchDomains, domain)
	}
	ipv4 := func(list []ipAddressWire) ([]string, error) {
		var values []string
		for _, entry := range list {
			if strings.TrimSpace(entry.Type) != "IPv4" {
				continue
			}
			values = append(values, entry.IPv4)
		}
		return parseIPv4List(values, "DNS")
	}
	if info.FromDHCPAddrs, err = ipv4(response.Info.DHCP); err != nil {
		return DNSInformation{}, err
	}
	if info.Manual, err = ipv4(response.Info.Manual); err != nil {
		return DNSInformation{}, err
	}
	return info, nil
}

// SetDNS sets the DNS source and IPv4 servers.
func (c *Client) SetDNS(ctx context.Context, fromDHCP bool, servers []string, opts ...RequestOptions) error {
	inner := `<tds:FromDHCP>` + strconv.FormatBool(fromDHCP) + `</tds:FromDHCP>`
	if !fromDHCP {
		for _, server := range servers {
			address, err := netip.ParseAddr(server)
			if err != nil || !address.Is4() {
				return fmt.Errorf("%w: DNS server", ErrInvalidInput)
			}
			inner += `<tds:DNSManual><tt:Type>IPv4</tt:Type><tt:IPv4Address>` + address.String() + `</tt:IPv4Address></tds:DNSManual>`
		}
	}
	raw, err := c.rawCall(ctx, ActionSetDNS, wrap("SetDNS", inner), opts)
	if err != nil {
		return err
	}
	return expectEmpty(raw, "SetDNSResponse")
}

// SystemReboot asks the device to reboot and returns its message, truncated
// to 256 characters.
func (c *Client) SystemReboot(ctx context.Context, opts ...RequestOptions) (string, error) {
	raw, err := c.rawCall(ctx, ActionSystemReboot, wrap("SystemReboot", ""), opts)
	if err != nil {
		return "", err
	}
	var response struct {
		XMLName xml.Name `xml:"http://www.onvif.org/ver10/device/wsdl SystemRebootResponse"`
		Message string   `xml:"http://www.onvif.org/ver10/device/wsdl Message"`
	}
	if err := decodeResponse(raw, &response, "SystemRebootResponse"); err != nil {
		return "", err
	}
	return truncateText(response.Message, 256), nil
}

// SetSystemFactoryDefault resets the device.
func (c *Client) SetSystemFactoryDefault(ctx context.Context, kind FactoryDefault, opts ...RequestOptions) error {
	if kind != FactoryDefaultSoft && kind != FactoryDefaultHard {
		return fmt.Errorf("%w: FactoryDefault", ErrInvalidInput)
	}
	raw, err := c.rawCall(ctx, ActionSetSystemFactoryDefault, wrap("SetSystemFactoryDefault", `<tds:FactoryDefault>`+string(kind)+`</tds:FactoryDefault>`), opts)
	if err != nil {
		return err
	}
	return expectEmpty(raw, "SetSystemFactoryDefaultResponse")
}

// GetSystemLog returns the text of the System or Access log, truncated to
// maxBytes. Binary (MTOM) logs are reported as unsupported.
func (c *Client) GetSystemLog(ctx context.Context, logType string, maxBytes int, opts ...RequestOptions) (string, error) {
	if logType != "System" && logType != "Access" {
		return "", fmt.Errorf("%w: LogType", ErrInvalidInput)
	}
	raw, err := c.rawCall(ctx, ActionGetSystemLog, wrap("GetSystemLog", `<tds:LogType>`+logType+`</tds:LogType>`), opts)
	if err != nil {
		return "", err
	}
	var response struct {
		XMLName xml.Name `xml:"http://www.onvif.org/ver10/device/wsdl GetSystemLogResponse"`
		Log     *struct {
			String *string   `xml:"http://www.onvif.org/ver10/schema String"`
			Binary *struct{} `xml:"http://www.onvif.org/ver10/schema Binary"`
		} `xml:"http://www.onvif.org/ver10/device/wsdl SystemLog"`
	}
	if err := decodeResponse(raw, &response, "GetSystemLogResponse"); err != nil {
		return "", err
	}
	if response.Log == nil || response.Log.String == nil {
		return "", fmt.Errorf("%w: SystemLog String", ErrMissingField)
	}
	return truncateText(*response.Log.String, maxBytes), nil
}

func parseIPv4List(values []string, field string) ([]string, error) {
	out := []string{}
	for _, value := range values {
		address, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil || !address.Is4() {
			return nil, fmt.Errorf("%w: %s", ErrInvalidField, field)
		}
		out = append(out, address.String())
	}
	return out, nil
}

func validToken(token string) bool {
	return token != "" && utf8.ValidString(token) && utf8.RuneCountInString(token) <= 64 && !hasControlRune(token)
}

func hasControlRune(value string) bool {
	for _, r := range value {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return true
		}
	}
	return false
}

func escapeText(value string) string {
	var builder strings.Builder
	_ = xml.EscapeText(&builder, []byte(value))
	return builder.String()
}

// truncateText bounds untrusted device text on a UTF-8 boundary.
func truncateText(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}
