package fakedevice

import (
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Device Management configuration actions modeled by the synthetic device.
const (
	ActionGetNTP                   = deviceActionBase + "/GetNTP"
	ActionSetNTP                   = deviceActionBase + "/SetNTP"
	ActionSetSystemDateAndTime     = deviceActionBase + "/SetSystemDateAndTime"
	ActionGetUsers                 = deviceActionBase + "/GetUsers"
	ActionCreateUsers              = deviceActionBase + "/CreateUsers"
	ActionSetUser                  = deviceActionBase + "/SetUser"
	ActionDeleteUsers              = deviceActionBase + "/DeleteUsers"
	ActionSetNetworkInterfaces     = deviceActionBase + "/SetNetworkInterfaces"
	ActionGetNetworkDefaultGateway = deviceActionBase + "/GetNetworkDefaultGateway"
	ActionSetNetworkDefaultGateway = deviceActionBase + "/SetNetworkDefaultGateway"
	ActionGetDNS                   = deviceActionBase + "/GetDNS"
	ActionSetDNS                   = deviceActionBase + "/SetDNS"
	ActionSystemReboot             = deviceActionBase + "/SystemReboot"
	ActionSetSystemFactoryDefault  = deviceActionBase + "/SetSystemFactoryDefault"
	ActionGetSystemLog             = deviceActionBase + "/GetSystemLog"
)

// DeviceSettings is the mutable device-management state.
type DeviceSettings struct {
	// DateTimeType is "Manual" (default) or "NTP".
	DateTimeType    string
	TimeZone        string
	DaylightSavings bool
	NTPFromDHCP     bool
	// NTPServers are IPv4 addresses or DNS names.
	NTPServers []string
	// Users replace Config.Username/Password as the authenticated accounts.
	// When empty, the configured username becomes the only Administrator.
	Users       []UserData
	Gateways    []string
	DNSFromDHCP bool
	DNSServers  []string
	// NetworkRebootNeeded is returned by SetNetworkInterfaces.
	NetworkRebootNeeded bool
	// RebootDuration keeps the device unavailable after SystemReboot.
	RebootDuration time.Duration
	SystemLog      string
}

// UserData is one synthetic account.
type UserData struct {
	Username string
	Password string
	Level    string
}

func (s DeviceSettings) withDefaults(username, password string) DeviceSettings {
	if s.DateTimeType == "" {
		s.DateTimeType = "Manual"
	}
	if len(s.Users) == 0 && username != "" {
		s.Users = []UserData{{Username: username, Password: password, Level: "Administrator"}}
	} else {
		s.Users = slices.Clone(s.Users)
	}
	if s.Gateways == nil {
		s.Gateways = []string{"192.0.2.1"}
	}
	s.NTPServers = slices.Clone(s.NTPServers)
	s.DNSServers = slices.Clone(s.DNSServers)
	if s.SystemLog == "" {
		s.SystemLog = "synthetic: device started\n"
	}
	return s
}

// credential returns the password of an account.
func (c *normalizedConfig) credential(username string) (string, bool) {
	for _, user := range c.Settings.Users {
		if secureEqual(user.Username, username) {
			return user.Password, true
		}
	}
	return "", false
}

func init() {
	for local, action := range map[string]string{
		"GetNTP": ActionGetNTP, "SetNTP": ActionSetNTP, "SetSystemDateAndTime": ActionSetSystemDateAndTime,
		"GetUsers": ActionGetUsers, "CreateUsers": ActionCreateUsers, "SetUser": ActionSetUser, "DeleteUsers": ActionDeleteUsers,
		"SetNetworkInterfaces": ActionSetNetworkInterfaces, "GetNetworkDefaultGateway": ActionGetNetworkDefaultGateway,
		"SetNetworkDefaultGateway": ActionSetNetworkDefaultGateway, "GetDNS": ActionGetDNS, "SetDNS": ActionSetDNS,
		"SystemReboot": ActionSystemReboot, "SetSystemFactoryDefault": ActionSetSystemFactoryDefault, "GetSystemLog": ActionGetSystemLog,
	} {
		deviceOperations[local] = action
	}
	extraHandlers[ActionGetNTP] = (*Device).getNTP
	extraHandlers[ActionSetNTP] = (*Device).setNTP
	extraHandlers[ActionSetSystemDateAndTime] = (*Device).setSystemDateAndTime
	extraHandlers[ActionGetUsers] = (*Device).getUsers
	extraHandlers[ActionCreateUsers] = (*Device).createUsers
	extraHandlers[ActionSetUser] = (*Device).setUser
	extraHandlers[ActionDeleteUsers] = (*Device).deleteUsers
	extraHandlers[ActionSetNetworkInterfaces] = (*Device).setNetworkInterfaces
	extraHandlers[ActionGetNetworkDefaultGateway] = (*Device).getGateway
	extraHandlers[ActionSetNetworkDefaultGateway] = (*Device).setGateway
	extraHandlers[ActionGetDNS] = (*Device).getDNS
	extraHandlers[ActionSetDNS] = (*Device).setDNS
	extraHandlers[ActionSystemReboot] = (*Device).systemReboot
	extraHandlers[ActionSetSystemFactoryDefault] = (*Device).factoryDefault
	extraHandlers[ActionGetSystemLog] = (*Device).getSystemLog
}

var invalidArgument = FaultSpec{Kind: FaultInvalidArgVal, Reason: "InvalidArgVal: the request is not valid"}

func (d *Device) deviceResponse(response http.ResponseWriter, version SOAPVersion, local, inner string) {
	d.writeSuccess(response, version, `<tds:`+local+` xmlns:tds="`+deviceNamespace+`">`+inner+`</tds:`+local+`>`)
}

func (d *Device) appendLog(next *normalizedConfig, line string) {
	next.Settings.SystemLog += "synthetic: " + line + "\n"
}

func hostXML(element, host string) string {
	if address, err := netip.ParseAddr(host); err == nil && address.Is4() {
		return `<tt:` + element + `><tt:Type>IPv4</tt:Type><tt:IPv4Address>` + address.String() + `</tt:IPv4Address></tt:` + element + `>`
	}
	return `<tt:` + element + `><tt:Type>DNS</tt:Type><tt:DNSname>` + escapeXML(host) + `</tt:DNSname></tt:` + element + `>`
}

func (d *Device) getNTP(response http.ResponseWriter, version SOAPVersion, _ operationWire) {
	settings := d.cfg().Settings
	inner := `<tt:FromDHCP>` + strconv.FormatBool(settings.NTPFromDHCP) + `</tt:FromDHCP>`
	for _, server := range settings.NTPServers {
		inner += hostXML("NTPManual", server)
	}
	d.deviceResponse(response, version, "GetNTPResponse", `<tds:NTPInformation>`+inner+`</tds:NTPInformation>`)
}

func (d *Device) setNTP(response http.ResponseWriter, version SOAPVersion, operation operationWire) {
	fromDHCP, ok := childText(operation.Elements, deviceNamespace, "FromDHCP")
	if !ok || (fromDHCP != "true" && fromDHCP != "false") {
		d.writeFault(response, version, invalidArgument)
		return
	}
	var servers []string
	for _, element := range operation.Elements {
		if element.XMLName.Space != deviceNamespace || element.XMLName.Local != "NTPManual" {
			continue
		}
		kind, _ := childText(element.Elements, typeNamespace, "Type")
		value, _ := childText(element.Elements, typeNamespace, map[string]string{"IPv4": "IPv4Address", "IPv6": "IPv6Address", "DNS": "DNSname"}[kind])
		if value == "" {
			d.writeFault(response, version, invalidArgument)
			return
		}
		servers = append(servers, value)
	}
	d.mutate(func(next *normalizedConfig) {
		next.Settings.NTPFromDHCP = fromDHCP == "true"
		next.Settings.NTPServers = servers
		d.appendLog(next, "NTP configuration changed")
	})
	d.deviceResponse(response, version, "SetNTPResponse", "")
}

func (d *Device) setSystemDateAndTime(response http.ResponseWriter, version SOAPVersion, operation operationWire) {
	kind, _ := childText(operation.Elements, deviceNamespace, "DateTimeType")
	daylight, _ := childText(operation.Elements, deviceNamespace, "DaylightSavings")
	if (kind != "Manual" && kind != "NTP") || (daylight != "true" && daylight != "false") {
		d.writeFault(response, version, invalidArgument)
		return
	}
	timeZone := ""
	if zone := childElement(operation.Elements, deviceNamespace, "TimeZone"); zone != nil {
		timeZone, _ = childText(zone.Elements, typeNamespace, "TZ")
	}
	var manual time.Time
	if kind == "Manual" {
		utc := childElement(operation.Elements, deviceNamespace, "UTCDateTime")
		if utc == nil {
			d.writeFault(response, version, invalidArgument)
			return
		}
		clock, date := childElement(utc.Elements, typeNamespace, "Time"), childElement(utc.Elements, typeNamespace, "Date")
		if clock == nil || date == nil {
			d.writeFault(response, version, invalidArgument)
			return
		}
		value := func(parent *textElement, local string) int {
			number, _ := childInt(parent.Elements, typeNamespace, local)
			return number
		}
		manual = time.Date(value(date, "Year"), time.Month(value(date, "Month")), value(date, "Day"),
			value(clock, "Hour"), value(clock, "Minute"), value(clock, "Second"), 0, time.UTC)
		if manual.Year() < 1970 || manual.Year() > 2100 {
			d.writeFault(response, version, invalidArgument)
			return
		}
	}
	d.mutate(func(next *normalizedConfig) {
		next.Settings.DateTimeType = kind
		next.Settings.DaylightSavings = daylight == "true"
		next.Settings.TimeZone = timeZone
		if kind == "Manual" {
			next.ClockSkew = manual.Sub(next.Clock())
		}
		d.appendLog(next, "date and time changed")
	})
	d.deviceResponse(response, version, "SetSystemDateAndTimeResponse", "")
}

func (d *Device) getUsers(response http.ResponseWriter, version SOAPVersion, _ operationWire) {
	var inner string
	for _, user := range d.cfg().Settings.Users {
		inner += `<tds:User><tt:Username>` + escapeXML(user.Username) + `</tt:Username><tt:UserLevel>` + escapeXML(user.Level) + `</tt:UserLevel></tds:User>`
	}
	d.deviceResponse(response, version, "GetUsersResponse", inner)
}

func parseUsers(operation operationWire) ([]UserData, bool) {
	var users []UserData
	for _, element := range operation.Elements {
		if element.XMLName.Space != deviceNamespace || element.XMLName.Local != "User" {
			continue
		}
		username, _ := childText(element.Elements, typeNamespace, "Username")
		level, _ := childText(element.Elements, typeNamespace, "UserLevel")
		password, hasPassword := childText(element.Elements, typeNamespace, "Password")
		if username == "" || level == "" || !hasPassword || password == "" {
			return nil, false
		}
		users = append(users, UserData{Username: username, Password: password, Level: level})
	}
	return users, len(users) > 0
}

func (d *Device) createUsers(response http.ResponseWriter, version SOAPVersion, operation operationWire) {
	users, ok := parseUsers(operation)
	if !ok {
		d.writeFault(response, version, invalidArgument)
		return
	}
	for _, user := range users {
		if _, exists := d.cfg().credential(user.Username); exists {
			d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "UsernameClash: the user already exists"})
			return
		}
	}
	d.mutate(func(next *normalizedConfig) {
		next.Settings.Users = append(slices.Clone(next.Settings.Users), users...)
		d.appendLog(next, "users created")
	})
	d.deviceResponse(response, version, "CreateUsersResponse", "")
}

func (d *Device) setUser(response http.ResponseWriter, version SOAPVersion, operation operationWire) {
	users, ok := parseUsers(operation)
	if !ok {
		d.writeFault(response, version, invalidArgument)
		return
	}
	for _, user := range users {
		if _, exists := d.cfg().credential(user.Username); !exists {
			d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "UsernameMissing: the user does not exist"})
			return
		}
	}
	d.mutate(func(next *normalizedConfig) {
		accounts := slices.Clone(next.Settings.Users)
		for _, user := range users {
			index := slices.IndexFunc(accounts, func(a UserData) bool { return a.Username == user.Username })
			accounts[index] = user
		}
		next.Settings.Users = accounts
		d.appendLog(next, "user password changed")
	})
	d.deviceResponse(response, version, "SetUserResponse", "")
}

func (d *Device) deleteUsers(response http.ResponseWriter, version SOAPVersion, operation operationWire) {
	var names []string
	for _, element := range operation.Elements {
		if element.XMLName.Space == deviceNamespace && element.XMLName.Local == "Username" {
			names = append(names, strings.TrimSpace(element.Value))
		}
	}
	if len(names) == 0 {
		d.writeFault(response, version, invalidArgument)
		return
	}
	d.mutate(func(next *normalizedConfig) {
		next.Settings.Users = slices.DeleteFunc(slices.Clone(next.Settings.Users), func(u UserData) bool { return slices.Contains(names, u.Username) })
		d.appendLog(next, "users deleted")
	})
	d.deviceResponse(response, version, "DeleteUsersResponse", "")
}

func (d *Device) setNetworkInterfaces(response http.ResponseWriter, version SOAPVersion, operation operationWire) {
	token, _ := childText(operation.Elements, deviceNamespace, "InterfaceToken")
	index := slices.IndexFunc(d.cfg().Inventory.NetworkInterfaces, func(n NetworkInterfaceData) bool { return n.Token == token })
	configuration := childElement(operation.Elements, deviceNamespace, "NetworkInterface")
	if index < 0 || configuration == nil {
		d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "InvalidNetworkInterface: the interface does not exist"})
		return
	}
	ipv4 := childElement(configuration.Elements, typeNamespace, "IPv4")
	if ipv4 == nil {
		d.writeFault(response, version, invalidArgument)
		return
	}
	dhcp, _ := childText(ipv4.Elements, typeNamespace, "DHCP")
	address, prefix := "", 0
	if manual := childElement(ipv4.Elements, typeNamespace, "Manual"); manual != nil {
		address, _ = childText(manual.Elements, typeNamespace, "Address")
		prefix, _ = childInt(manual.Elements, typeNamespace, "PrefixLength")
		if parsed, err := netip.ParseAddr(address); err != nil || !parsed.Is4() || prefix < 1 || prefix > 32 {
			d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "InvalidIPv4Address: the address is not valid"})
			return
		}
	} else if dhcp != "true" {
		d.writeFault(response, version, invalidArgument)
		return
	}
	d.mutate(func(next *normalizedConfig) {
		interfaces := slices.Clone(next.Inventory.NetworkInterfaces)
		entry := &interfaces[index]
		entry.IPv4FromDHCP = dhcp == "true"
		if address != "" {
			entry.IPv4Address, entry.IPv4Prefix = address, prefix
		}
		next.Inventory.NetworkInterfaces = interfaces
		d.appendLog(next, "network interface changed")
	})
	d.deviceResponse(response, version, "SetNetworkInterfacesResponse",
		`<tds:RebootNeeded>`+strconv.FormatBool(d.cfg().Settings.NetworkRebootNeeded)+`</tds:RebootNeeded>`)
}

func (d *Device) getGateway(response http.ResponseWriter, version SOAPVersion, _ operationWire) {
	var inner string
	for _, gateway := range d.cfg().Settings.Gateways {
		inner += `<tt:IPv4Address>` + escapeXML(gateway) + `</tt:IPv4Address>`
	}
	d.deviceResponse(response, version, "GetNetworkDefaultGatewayResponse", `<tds:NetworkGateway>`+inner+`</tds:NetworkGateway>`)
}

func (d *Device) setGateway(response http.ResponseWriter, version SOAPVersion, operation operationWire) {
	gateways := []string{}
	for _, element := range operation.Elements {
		if element.XMLName.Space == deviceNamespace && element.XMLName.Local == "IPv4Address" {
			value := strings.TrimSpace(element.Value)
			if parsed, err := netip.ParseAddr(value); err != nil || !parsed.Is4() {
				d.writeFault(response, version, invalidArgument)
				return
			}
			gateways = append(gateways, value)
		}
	}
	d.mutate(func(next *normalizedConfig) { next.Settings.Gateways = gateways })
	d.deviceResponse(response, version, "SetNetworkDefaultGatewayResponse", "")
}

func (d *Device) getDNS(response http.ResponseWriter, version SOAPVersion, _ operationWire) {
	settings := d.cfg().Settings
	inner := `<tt:FromDHCP>` + strconv.FormatBool(settings.DNSFromDHCP) + `</tt:FromDHCP>`
	for _, server := range settings.DNSServers {
		inner += `<tt:DNSManual><tt:Type>IPv4</tt:Type><tt:IPv4Address>` + escapeXML(server) + `</tt:IPv4Address></tt:DNSManual>`
	}
	d.deviceResponse(response, version, "GetDNSResponse", `<tds:DNSInformation>`+inner+`</tds:DNSInformation>`)
}

func (d *Device) setDNS(response http.ResponseWriter, version SOAPVersion, operation operationWire) {
	fromDHCP, _ := childText(operation.Elements, deviceNamespace, "FromDHCP")
	if fromDHCP != "true" && fromDHCP != "false" {
		d.writeFault(response, version, invalidArgument)
		return
	}
	servers := []string{}
	for _, element := range operation.Elements {
		if element.XMLName.Space == deviceNamespace && element.XMLName.Local == "DNSManual" {
			value, _ := childText(element.Elements, typeNamespace, "IPv4Address")
			if parsed, err := netip.ParseAddr(value); err != nil || !parsed.Is4() {
				d.writeFault(response, version, invalidArgument)
				return
			}
			servers = append(servers, value)
		}
	}
	d.mutate(func(next *normalizedConfig) {
		next.Settings.DNSFromDHCP = fromDHCP == "true"
		next.Settings.DNSServers = servers
	})
	d.deviceResponse(response, version, "SetDNSResponse", "")
}

func (d *Device) systemReboot(response http.ResponseWriter, version SOAPVersion, _ operationWire) {
	d.mutate(func(next *normalizedConfig) { d.appendLog(next, "reboot requested") })
	d.mu.Lock()
	d.rebootUntil = d.cfg().Clock().Add(d.cfg().Settings.RebootDuration)
	d.reboots++
	d.mu.Unlock()
	d.deviceResponse(response, version, "SystemRebootResponse", `<tds:Message>Rebooting in 1 second</tds:Message>`)
}

func (d *Device) factoryDefault(response http.ResponseWriter, version SOAPVersion, operation operationWire) {
	kind, _ := childText(operation.Elements, deviceNamespace, "FactoryDefault")
	if kind != "Soft" && kind != "Hard" {
		d.writeFault(response, version, invalidArgument)
		return
	}
	initial := d.initial
	d.mutate(func(next *normalizedConfig) {
		network := next.Inventory.NetworkInterfaces
		gateways, dns := next.Settings.Gateways, next.Settings.DNSServers
		users := next.Settings.Users
		*next = initial
		// A soft reset keeps network settings and accounts.
		if kind == "Soft" {
			next.Inventory.NetworkInterfaces = network
			next.Settings.Gateways, next.Settings.DNSServers = gateways, dns
			next.Settings.Users = users
		}
		d.appendLog(next, "factory default ("+strings.ToLower(kind)+") applied")
	})
	d.deviceResponse(response, version, "SetSystemFactoryDefaultResponse", "")
}

func (d *Device) getSystemLog(response http.ResponseWriter, version SOAPVersion, operation operationWire) {
	kind, _ := childText(operation.Elements, deviceNamespace, "LogType")
	if kind != "System" && kind != "Access" {
		d.writeFault(response, version, invalidArgument)
		return
	}
	d.deviceResponse(response, version, "GetSystemLogResponse",
		`<tds:SystemLog><tt:String>`+escapeXML(d.cfg().Settings.SystemLog)+`</tt:String></tds:SystemLog>`)
}

// Reboots reports how many SystemReboot requests were accepted.
func (d *Device) Reboots() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.reboots
}

// rebooting reports whether the device is inside a reboot window.
func (d *Device) rebooting() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cfg().Clock().Before(d.rebootUntil)
}
