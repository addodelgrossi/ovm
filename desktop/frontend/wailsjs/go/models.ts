export namespace app {
  export class AudioConfigDTO {
    config_token?: string
    codec?: string
    bitrate_kbps?: number
    sample_rate_khz?: number

    static createFrom(source: any = {}) {
      return new AudioConfigDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.config_token = source['config_token']
      this.codec = source['codec']
      this.bitrate_kbps = source['bitrate_kbps']
      this.sample_rate_khz = source['sample_rate_khz']
    }
  }
  export class AuthPolicyDTO {
    allow_basic: boolean
    allow_basic_over_http: boolean
    allow_legacy_digest: boolean

    static createFrom(source: any = {}) {
      return new AuthPolicyDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.allow_basic = source['allow_basic']
      this.allow_basic_over_http = source['allow_basic_over_http']
      this.allow_legacy_digest = source['allow_legacy_digest']
    }
  }
  export class IPAddressDTO {
    address?: string
    prefix_length?: number
    from_dhcp: boolean

    static createFrom(source: any = {}) {
      return new IPAddressDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.address = source['address']
      this.prefix_length = source['prefix_length']
      this.from_dhcp = source['from_dhcp']
    }
  }
  export class NetworkInterfaceDTO {
    token?: string
    enabled: boolean
    has_enabled: boolean
    name?: string
    hw_address?: string
    mtu?: number
    has_mtu: boolean
    ipv4_enabled: boolean
    has_ipv4: boolean
    dhcp: boolean
    has_dhcp: boolean
    ipv4_addresses?: IPAddressDTO[]

    static createFrom(source: any = {}) {
      return new NetworkInterfaceDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.token = source['token']
      this.enabled = source['enabled']
      this.has_enabled = source['has_enabled']
      this.name = source['name']
      this.hw_address = source['hw_address']
      this.mtu = source['mtu']
      this.has_mtu = source['has_mtu']
      this.ipv4_enabled = source['ipv4_enabled']
      this.has_ipv4 = source['has_ipv4']
      this.dhcp = source['dhcp']
      this.has_dhcp = source['has_dhcp']
      this.ipv4_addresses = this.convertValues(
        source['ipv4_addresses'],
        IPAddressDTO
      )
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class HostnameDTO {
    from_dhcp: boolean
    name?: string

    static createFrom(source: any = {}) {
      return new HostnameDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.from_dhcp = source['from_dhcp']
      this.name = source['name']
    }
  }
  export class ScopeDTO {
    definition?: string
    item?: string

    static createFrom(source: any = {}) {
      return new ScopeDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.definition = source['definition']
      this.item = source['item']
    }
  }
  export class ServiceDTO {
    namespace?: string
    xaddr?: string
    version_major?: number
    version_minor?: number
    has_version: boolean

    static createFrom(source: any = {}) {
      return new ServiceDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.namespace = source['namespace']
      this.xaddr = source['xaddr']
      this.version_major = source['version_major']
      this.version_minor = source['version_minor']
      this.has_version = source['has_version']
    }
  }
  export class CapabilitiesDTO {
    device_xaddr?: string
    media_xaddr?: string
    events_xaddr?: string
    imaging_xaddr?: string
    ptz_xaddr?: string
    analytics_xaddr?: string

    static createFrom(source: any = {}) {
      return new CapabilitiesDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_xaddr = source['device_xaddr']
      this.media_xaddr = source['media_xaddr']
      this.events_xaddr = source['events_xaddr']
      this.imaging_xaddr = source['imaging_xaddr']
      this.ptz_xaddr = source['ptz_xaddr']
      this.analytics_xaddr = source['analytics_xaddr']
    }
  }
  export class DeviceInformationDTO {
    manufacturer?: string
    model?: string
    firmware_version?: string
    serial_number?: string
    hardware_id?: string

    static createFrom(source: any = {}) {
      return new DeviceInformationDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.manufacturer = source['manufacturer']
      this.model = source['model']
      this.firmware_version = source['firmware_version']
      this.serial_number = source['serial_number']
      this.hardware_id = source['hardware_id']
    }
  }
  export class SystemDateAndTimeDTO {
    date_time_type?: string
    daylight_savings: boolean
    time_zone?: string
    utc?: string

    static createFrom(source: any = {}) {
      return new SystemDateAndTimeDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.date_time_type = source['date_time_type']
      this.daylight_savings = source['daylight_savings']
      this.time_zone = source['time_zone']
      this.utc = source['utc']
    }
  }
  export class OperationStatus {
    operation: string
    state: string
    detail?: string

    static createFrom(source: any = {}) {
      return new OperationStatus(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.operation = source['operation']
      this.state = source['state']
      this.detail = source['detail']
    }
  }
  export class Inventory {
    endpoint: string
    operations: OperationStatus[]
    system_date_and_time?: SystemDateAndTimeDTO
    device_information?: DeviceInformationDTO
    capabilities?: CapabilitiesDTO
    services?: ServiceDTO[]
    scopes?: ScopeDTO[]
    hostname?: HostnameDTO
    network_interfaces?: NetworkInterfaceDTO[]

    static createFrom(source: any = {}) {
      return new Inventory(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.endpoint = source['endpoint']
      this.operations = this.convertValues(
        source['operations'],
        OperationStatus
      )
      this.system_date_and_time = this.convertValues(
        source['system_date_and_time'],
        SystemDateAndTimeDTO
      )
      this.device_information = this.convertValues(
        source['device_information'],
        DeviceInformationDTO
      )
      this.capabilities = this.convertValues(
        source['capabilities'],
        CapabilitiesDTO
      )
      this.services = this.convertValues(source['services'], ServiceDTO)
      this.scopes = this.convertValues(source['scopes'], ScopeDTO)
      this.hostname = this.convertValues(source['hostname'], HostnameDTO)
      this.network_interfaces = this.convertValues(
        source['network_interfaces'],
        NetworkInterfaceDTO
      )
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class DeviceDTO {
    id: string
    endpoint: string
    endpoint_reference?: string
    display_name?: string
    discovery_source?: string
    has_credentials: boolean
    auth_policy: AuthPolicyDTO
    selected: boolean
    status: string
    created_at?: string
    updated_at?: string

    static createFrom(source: any = {}) {
      return new DeviceDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.id = source['id']
      this.endpoint = source['endpoint']
      this.endpoint_reference = source['endpoint_reference']
      this.display_name = source['display_name']
      this.discovery_source = source['discovery_source']
      this.has_credentials = source['has_credentials']
      this.auth_policy = this.convertValues(
        source['auth_policy'],
        AuthPolicyDTO
      )
      this.selected = source['selected']
      this.status = source['status']
      this.created_at = source['created_at']
      this.updated_at = source['updated_at']
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class AuthenticateResult {
    device: DeviceDTO
    inventory: Inventory

    static createFrom(source: any = {}) {
      return new AuthenticateResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device = this.convertValues(source['device'], DeviceDTO)
      this.inventory = this.convertValues(source['inventory'], Inventory)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class ClockEstimate {
    source: string
    offset_seconds: number
    uncertainty_seconds: number
    round_trip_ms?: number

    static createFrom(source: any = {}) {
      return new ClockEstimate(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.source = source['source']
      this.offset_seconds = source['offset_seconds']
      this.uncertainty_seconds = source['uncertainty_seconds']
      this.round_trip_ms = source['round_trip_ms']
    }
  }
  export class ClockCheckResult {
    checked_at: string
    tolerance_seconds: number
    verdict: string
    date_time_type: string
    ntp_servers: string[]
    onvif?: ClockEstimate
    rtcp?: ClockEstimate
    rtcp_unavailable?: string

    static createFrom(source: any = {}) {
      return new ClockCheckResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.checked_at = source['checked_at']
      this.tolerance_seconds = source['tolerance_seconds']
      this.verdict = source['verdict']
      this.date_time_type = source['date_time_type']
      this.ntp_servers = source['ntp_servers']
      this.onvif = this.convertValues(source['onvif'], ClockEstimate)
      this.rtcp = this.convertValues(source['rtcp'], ClockEstimate)
      this.rtcp_unavailable = source['rtcp_unavailable']
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class ReportGap {
    section: string
    reason: string

    static createFrom(source: any = {}) {
      return new ReportGap(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.section = source['section']
      this.reason = source['reason']
    }
  }
  export class ProbeResult {
    profile_token: string
    transport: string
    codec: string
    width: number
    height: number
    frames_per_second: number
    bitrate_kbps: number
    gop_frames: number
    gop_seconds: number
    time_to_first_frame_ms: number
    frames: number
    keyframes: number
    lost_packets: number
    duration_ms: number
    complete: boolean

    static createFrom(source: any = {}) {
      return new ProbeResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.profile_token = source['profile_token']
      this.transport = source['transport']
      this.codec = source['codec']
      this.width = source['width']
      this.height = source['height']
      this.frames_per_second = source['frames_per_second']
      this.bitrate_kbps = source['bitrate_kbps']
      this.gop_frames = source['gop_frames']
      this.gop_seconds = source['gop_seconds']
      this.time_to_first_frame_ms = source['time_to_first_frame_ms']
      this.frames = source['frames']
      this.keyframes = source['keyframes']
      this.lost_packets = source['lost_packets']
      this.duration_ms = source['duration_ms']
      this.complete = source['complete']
    }
  }
  export class StabilityCycle {
    index: number
    passed: boolean
    error?: string
    result?: ProbeResult

    static createFrom(source: any = {}) {
      return new StabilityCycle(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.index = source['index']
      this.passed = source['passed']
      this.error = source['error']
      this.result = this.convertValues(source['result'], ProbeResult)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class StabilityReport {
    profile_token: string
    transport: string
    cycles_requested: number
    cycle_seconds: number
    cycles_completed: number
    passed: number
    failed: number
    verdict: string
    min_ttff_ms: number
    median_ttff_ms: number
    max_ttff_ms: number
    mean_fps: number
    mean_bitrate_kbps: number
    lost_packets: number
    started_at: string
    finished_at: string
    cycles: StabilityCycle[]

    static createFrom(source: any = {}) {
      return new StabilityReport(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.profile_token = source['profile_token']
      this.transport = source['transport']
      this.cycles_requested = source['cycles_requested']
      this.cycle_seconds = source['cycle_seconds']
      this.cycles_completed = source['cycles_completed']
      this.passed = source['passed']
      this.failed = source['failed']
      this.verdict = source['verdict']
      this.min_ttff_ms = source['min_ttff_ms']
      this.median_ttff_ms = source['median_ttff_ms']
      this.max_ttff_ms = source['max_ttff_ms']
      this.mean_fps = source['mean_fps']
      this.mean_bitrate_kbps = source['mean_bitrate_kbps']
      this.lost_packets = source['lost_packets']
      this.started_at = source['started_at']
      this.finished_at = source['finished_at']
      this.cycles = this.convertValues(source['cycles'], StabilityCycle)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class UserDTO {
    username: string
    level: string

    static createFrom(source: any = {}) {
      return new UserDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.username = source['username']
      this.level = source['level']
    }
  }
  export class ReportUsers {
    accounts: UserDTO[]
    default_credential_warning: boolean

    static createFrom(source: any = {}) {
      return new ReportUsers(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.accounts = this.convertValues(source['accounts'], UserDTO)
      this.default_credential_warning = source['default_credential_warning']
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class NetworkView {
    interface_token: string
    interface_name: string
    hw_address: string
    dhcp: boolean
    address: string
    prefix_length: number
    gateways: string[]
    dns_from_dhcp: boolean
    dns_servers: string[]

    static createFrom(source: any = {}) {
      return new NetworkView(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.interface_token = source['interface_token']
      this.interface_name = source['interface_name']
      this.hw_address = source['hw_address']
      this.dhcp = source['dhcp']
      this.address = source['address']
      this.prefix_length = source['prefix_length']
      this.gateways = source['gateways']
      this.dns_from_dhcp = source['dns_from_dhcp']
      this.dns_servers = source['dns_servers']
    }
  }
  export class TimeView {
    date_time_type: string
    time_zone: string
    daylight_savings: boolean
    device_utc?: string
    clock_offset_seconds?: number
    uncertainty_seconds: number
    ntp_from_dhcp: boolean
    ntp_servers: string[]

    static createFrom(source: any = {}) {
      return new TimeView(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.date_time_type = source['date_time_type']
      this.time_zone = source['time_zone']
      this.daylight_savings = source['daylight_savings']
      this.device_utc = source['device_utc']
      this.clock_offset_seconds = source['clock_offset_seconds']
      this.uncertainty_seconds = source['uncertainty_seconds']
      this.ntp_from_dhcp = source['ntp_from_dhcp']
      this.ntp_servers = source['ntp_servers']
    }
  }
  export class ImagingSettingsDTO {
    brightness?: number
    color_saturation?: number
    contrast?: number
    sharpness?: number
    ir_cut_filter?: string

    static createFrom(source: any = {}) {
      return new ImagingSettingsDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.brightness = source['brightness']
      this.color_saturation = source['color_saturation']
      this.contrast = source['contrast']
      this.sharpness = source['sharpness']
      this.ir_cut_filter = source['ir_cut_filter']
    }
  }
  export class ReportImaging {
    profile_token: string
    settings: ImagingSettingsDTO

    static createFrom(source: any = {}) {
      return new ReportImaging(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.profile_token = source['profile_token']
      this.settings = this.convertValues(source['settings'], ImagingSettingsDTO)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class ReportProfile {
    token: string
    name?: string
    codec?: string
    codec_profile?: string
    width?: number
    height?: number
    frame_rate_fps?: number
    bitrate_kbps?: number
    gop_length?: number
    rtsp_uri?: string
    snapshot_uri?: string

    static createFrom(source: any = {}) {
      return new ReportProfile(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.token = source['token']
      this.name = source['name']
      this.codec = source['codec']
      this.codec_profile = source['codec_profile']
      this.width = source['width']
      this.height = source['height']
      this.frame_rate_fps = source['frame_rate_fps']
      this.bitrate_kbps = source['bitrate_kbps']
      this.gop_length = source['gop_length']
      this.rtsp_uri = source['rtsp_uri']
      this.snapshot_uri = source['snapshot_uri']
    }
  }
  export class ReportDevice {
    display_name?: string
    endpoint: string
    manufacturer?: string
    model?: string
    firmware_version?: string
    serial_number?: string
    hardware_id?: string
    hostname?: string

    static createFrom(source: any = {}) {
      return new ReportDevice(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.display_name = source['display_name']
      this.endpoint = source['endpoint']
      this.manufacturer = source['manufacturer']
      this.model = source['model']
      this.firmware_version = source['firmware_version']
      this.serial_number = source['serial_number']
      this.hardware_id = source['hardware_id']
      this.hostname = source['hostname']
    }
  }
  export class DeviceReport {
    schema: string
    generator: string
    generated_at: string
    device: ReportDevice
    profiles: ReportProfile[]
    imaging?: ReportImaging
    time?: TimeView
    network?: NetworkView
    users?: ReportUsers
    clock_check?: ClockCheckResult
    stability?: StabilityReport
    unavailable: ReportGap[]

    static createFrom(source: any = {}) {
      return new DeviceReport(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.schema = source['schema']
      this.generator = source['generator']
      this.generated_at = source['generated_at']
      this.device = this.convertValues(source['device'], ReportDevice)
      this.profiles = this.convertValues(source['profiles'], ReportProfile)
      this.imaging = this.convertValues(source['imaging'], ReportImaging)
      this.time = this.convertValues(source['time'], TimeView)
      this.network = this.convertValues(source['network'], NetworkView)
      this.users = this.convertValues(source['users'], ReportUsers)
      this.clock_check = this.convertValues(
        source['clock_check'],
        ClockCheckResult
      )
      this.stability = this.convertValues(source['stability'], StabilityReport)
      this.unavailable = this.convertValues(source['unavailable'], ReportGap)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class DiscoveryDiagnostic {
    category: string
    detail?: string

    static createFrom(source: any = {}) {
      return new DiscoveryDiagnostic(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.category = source['category']
      this.detail = source['detail']
    }
  }
  export class FieldChange {
    field: string
    before: string
    requested: string
    after: string

    static createFrom(source: any = {}) {
      return new FieldChange(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.field = source['field']
      this.before = source['before']
      this.requested = source['requested']
      this.after = source['after']
    }
  }
  export class MutationReport {
    outcome: string
    changes: FieldChange[]

    static createFrom(source: any = {}) {
      return new MutationReport(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.outcome = source['outcome']
      this.changes = this.convertValues(source['changes'], FieldChange)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class EncoderSettings {
    configuration_token: string
    source: string
    encoding: string
    width: number
    height: number
    frame_rate: number
    bitrate_kbps: number
    gop_length: number
    codec_profile: string

    static createFrom(source: any = {}) {
      return new EncoderSettings(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.configuration_token = source['configuration_token']
      this.source = source['source']
      this.encoding = source['encoding']
      this.width = source['width']
      this.height = source['height']
      this.frame_rate = source['frame_rate']
      this.bitrate_kbps = source['bitrate_kbps']
      this.gop_length = source['gop_length']
      this.codec_profile = source['codec_profile']
    }
  }
  export class EncoderApplyResult {
    profile_token: string
    before: EncoderSettings
    after: EncoderSettings
    report: MutationReport

    static createFrom(source: any = {}) {
      return new EncoderApplyResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.profile_token = source['profile_token']
      this.before = this.convertValues(source['before'], EncoderSettings)
      this.after = this.convertValues(source['after'], EncoderSettings)
      this.report = this.convertValues(source['report'], MutationReport)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class EncoderChange {
    encoding: string
    width: number
    height: number
    frame_rate: number
    bitrate_kbps: number
    gop_length: number
    codec_profile: string

    static createFrom(source: any = {}) {
      return new EncoderChange(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.encoding = source['encoding']
      this.width = source['width']
      this.height = source['height']
      this.frame_rate = source['frame_rate']
      this.bitrate_kbps = source['bitrate_kbps']
      this.gop_length = source['gop_length']
      this.codec_profile = source['codec_profile']
    }
  }
  export class ResolutionDTO {
    width: number
    height: number

    static createFrom(source: any = {}) {
      return new ResolutionDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.width = source['width']
      this.height = source['height']
    }
  }
  export class EncoderCodecOptions {
    encoding: string
    resolutions: ResolutionDTO[]
    frame_rates: number[]
    frame_rate_min: number
    frame_rate_max: number
    bitrate_min: number
    bitrate_max: number
    gop_min: number
    gop_max: number
    profiles: string[]

    static createFrom(source: any = {}) {
      return new EncoderCodecOptions(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.encoding = source['encoding']
      this.resolutions = this.convertValues(
        source['resolutions'],
        ResolutionDTO
      )
      this.frame_rates = source['frame_rates']
      this.frame_rate_min = source['frame_rate_min']
      this.frame_rate_max = source['frame_rate_max']
      this.bitrate_min = source['bitrate_min']
      this.bitrate_max = source['bitrate_max']
      this.gop_min = source['gop_min']
      this.gop_max = source['gop_max']
      this.profiles = source['profiles']
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class EncoderView {
    profile_token: string
    current: EncoderSettings
    codecs: EncoderCodecOptions[]

    static createFrom(source: any = {}) {
      return new EncoderView(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.profile_token = source['profile_token']
      this.current = this.convertValues(source['current'], EncoderSettings)
      this.codecs = this.convertValues(source['codecs'], EncoderCodecOptions)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class HostInterfaceDTO {
    index: number
    name: string
    loopback: boolean
    usable: boolean
    ipv4?: string[]
    reason: string

    static createFrom(source: any = {}) {
      return new HostInterfaceDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.index = source['index']
      this.name = source['name']
      this.loopback = source['loopback']
      this.usable = source['usable']
      this.ipv4 = source['ipv4']
      this.reason = source['reason']
    }
  }
  export class HostInterfacesResult {
    interfaces: HostInterfaceDTO[]

    static createFrom(source: any = {}) {
      return new HostInterfacesResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.interfaces = this.convertValues(
        source['interfaces'],
        HostInterfaceDTO
      )
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class ImagingApplyResult {
    before: ImagingSettingsDTO
    after: ImagingSettingsDTO
    report: MutationReport

    static createFrom(source: any = {}) {
      return new ImagingApplyResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.before = this.convertValues(source['before'], ImagingSettingsDTO)
      this.after = this.convertValues(source['after'], ImagingSettingsDTO)
      this.report = this.convertValues(source['report'], MutationReport)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class ImagingRange {
    min: number
    max: number

    static createFrom(source: any = {}) {
      return new ImagingRange(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.min = source['min']
      this.max = source['max']
    }
  }
  export class ImagingOptionsDTO {
    brightness?: ImagingRange
    color_saturation?: ImagingRange
    contrast?: ImagingRange
    sharpness?: ImagingRange
    ir_cut_modes: string[]

    static createFrom(source: any = {}) {
      return new ImagingOptionsDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.brightness = this.convertValues(source['brightness'], ImagingRange)
      this.color_saturation = this.convertValues(
        source['color_saturation'],
        ImagingRange
      )
      this.contrast = this.convertValues(source['contrast'], ImagingRange)
      this.sharpness = this.convertValues(source['sharpness'], ImagingRange)
      this.ir_cut_modes = source['ir_cut_modes']
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class ImagingView {
    video_source_token: string
    settings: ImagingSettingsDTO
    options: ImagingOptionsDTO

    static createFrom(source: any = {}) {
      return new ImagingView(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.video_source_token = source['video_source_token']
      this.settings = this.convertValues(source['settings'], ImagingSettingsDTO)
      this.options = this.convertValues(source['options'], ImagingOptionsDTO)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class MaintenanceResult {
    message: string
    back_online: boolean
    downtime_seconds: number

    static createFrom(source: any = {}) {
      return new MaintenanceResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.message = source['message']
      this.back_online = source['back_online']
      this.downtime_seconds = source['downtime_seconds']
    }
  }
  export class MediaOperationStatus {
    operation: string
    state: string
    source?: string
    fell_back?: boolean
    detail?: string

    static createFrom(source: any = {}) {
      return new MediaOperationStatus(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.operation = source['operation']
      this.state = source['state']
      this.source = source['source']
      this.fell_back = source['fell_back']
      this.detail = source['detail']
    }
  }
  export class SnapshotURIDTO {
    uri?: string

    static createFrom(source: any = {}) {
      return new SnapshotURIDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.uri = source['uri']
    }
  }
  export class StreamURIDTO {
    tcp_uri?: string
    udp_uri?: string

    static createFrom(source: any = {}) {
      return new StreamURIDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.tcp_uri = source['tcp_uri']
      this.udp_uri = source['udp_uri']
    }
  }
  export class VideoConfigDTO {
    config_token?: string
    codec?: string
    width?: number
    height?: number
    frame_rate_fps?: number
    has_frame_rate?: boolean
    bitrate_kbps?: number
    gop_length?: number
    profile?: string

    static createFrom(source: any = {}) {
      return new VideoConfigDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.config_token = source['config_token']
      this.codec = source['codec']
      this.width = source['width']
      this.height = source['height']
      this.frame_rate_fps = source['frame_rate_fps']
      this.has_frame_rate = source['has_frame_rate']
      this.bitrate_kbps = source['bitrate_kbps']
      this.gop_length = source['gop_length']
      this.profile = source['profile']
    }
  }
  export class MediaProfileDTO {
    token: string
    name?: string
    fixed?: boolean
    has_fixed?: boolean
    source?: string
    video_source_config_token?: string
    video_source_token?: string
    video_encoder_config_token?: string
    video?: VideoConfigDTO
    audio?: AudioConfigDTO
    stream?: StreamURIDTO
    snapshot?: SnapshotURIDTO
    operations?: MediaOperationStatus[]

    static createFrom(source: any = {}) {
      return new MediaProfileDTO(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.token = source['token']
      this.name = source['name']
      this.fixed = source['fixed']
      this.has_fixed = source['has_fixed']
      this.source = source['source']
      this.video_source_config_token = source['video_source_config_token']
      this.video_source_token = source['video_source_token']
      this.video_encoder_config_token = source['video_encoder_config_token']
      this.video = this.convertValues(source['video'], VideoConfigDTO)
      this.audio = this.convertValues(source['audio'], AudioConfigDTO)
      this.stream = this.convertValues(source['stream'], StreamURIDTO)
      this.snapshot = this.convertValues(source['snapshot'], SnapshotURIDTO)
      this.operations = this.convertValues(
        source['operations'],
        MediaOperationStatus
      )
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class MediaProfilesResult {
    profiles: MediaProfileDTO[]
    media2_available: boolean
    media1_available: boolean

    static createFrom(source: any = {}) {
      return new MediaProfilesResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.profiles = this.convertValues(source['profiles'], MediaProfileDTO)
      this.media2_available = source['media2_available']
      this.media1_available = source['media1_available']
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class NetworkApplyResult {
    before: NetworkView
    after?: NetworkView
    report: MutationReport
    reboot_requested: boolean
    new_endpoint?: string

    static createFrom(source: any = {}) {
      return new NetworkApplyResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.before = this.convertValues(source['before'], NetworkView)
      this.after = this.convertValues(source['after'], NetworkView)
      this.report = this.convertValues(source['report'], MutationReport)
      this.reboot_requested = source['reboot_requested']
      this.new_endpoint = source['new_endpoint']
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class NetworkChange {
    dhcp: boolean
    address: string
    prefix_length: number
    gateway: string
    dns_servers: string[]

    static createFrom(source: any = {}) {
      return new NetworkChange(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.dhcp = source['dhcp']
      this.address = source['address']
      this.prefix_length = source['prefix_length']
      this.gateway = source['gateway']
      this.dns_servers = source['dns_servers']
    }
  }

  export class Overview {
    devices: DeviceDTO[]
    selected_device_id?: string
    selected_host_interface?: string
    locale: string
    theme: string

    static createFrom(source: any = {}) {
      return new Overview(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.devices = this.convertValues(source['devices'], DeviceDTO)
      this.selected_device_id = source['selected_device_id']
      this.selected_host_interface = source['selected_host_interface']
      this.locale = source['locale']
      this.theme = source['theme']
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class SOAPConsoleEntry {
    id: number
    time: string
    endpoint: string
    action: string
    version: string
    status_code: number
    outcome: string
    duration_ms: number
    request_xml: string
    response_xml: string

    static createFrom(source: any = {}) {
      return new SOAPConsoleEntry(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.id = source['id']
      this.time = source['time']
      this.endpoint = source['endpoint']
      this.action = source['action']
      this.version = source['version']
      this.status_code = source['status_code']
      this.outcome = source['outcome']
      this.duration_ms = source['duration_ms']
      this.request_xml = source['request_xml']
      this.response_xml = source['response_xml']
    }
  }
  export class ScanConflict {
    endpoint_reference?: string
    endpoint?: string
    reason: string
    detail?: string

    static createFrom(source: any = {}) {
      return new ScanConflict(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.endpoint_reference = source['endpoint_reference']
      this.endpoint = source['endpoint']
      this.reason = source['reason']
      this.detail = source['detail']
    }
  }
  export class ScanResult {
    devices: DeviceDTO[]
    added?: string[]
    updated?: string[]
    conflicts?: ScanConflict[]
    diagnostic?: DiscoveryDiagnostic

    static createFrom(source: any = {}) {
      return new ScanResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.devices = this.convertValues(source['devices'], DeviceDTO)
      this.added = source['added']
      this.updated = source['updated']
      this.conflicts = this.convertValues(source['conflicts'], ScanConflict)
      this.diagnostic = this.convertValues(
        source['diagnostic'],
        DiscoveryDiagnostic
      )
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class SystemLogResult {
    type: string
    text: string
    truncated: boolean

    static createFrom(source: any = {}) {
      return new SystemLogResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.type = source['type']
      this.text = source['text']
      this.truncated = source['truncated']
    }
  }
  export class TimeApplyResult {
    before: TimeView
    after: TimeView
    report: MutationReport

    static createFrom(source: any = {}) {
      return new TimeApplyResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.before = this.convertValues(source['before'], TimeView)
      this.after = this.convertValues(source['after'], TimeView)
      this.report = this.convertValues(source['report'], MutationReport)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class TimeChange {
    mode: string
    ntp_servers: string[]
    time_zone: string
    daylight_savings: boolean

    static createFrom(source: any = {}) {
      return new TimeChange(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.mode = source['mode']
      this.ntp_servers = source['ntp_servers']
      this.time_zone = source['time_zone']
      this.daylight_savings = source['daylight_savings']
    }
  }

  export class UsersApplyResult {
    users: UserDTO[]
    report: MutationReport
    sign_in_verified: boolean

    static createFrom(source: any = {}) {
      return new UsersApplyResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.users = this.convertValues(source['users'], UserDTO)
      this.report = this.convertValues(source['report'], MutationReport)
      this.sign_in_verified = source['sign_in_verified']
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class UsersView {
    users: UserDTO[]
    current_username: string
    default_credential_warning: boolean

    static createFrom(source: any = {}) {
      return new UsersView(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.users = this.convertValues(source['users'], UserDTO)
      this.current_username = source['current_username']
      this.default_credential_warning = source['default_credential_warning']
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
}

export namespace buildinfo {
  export class Info {
    version: string
    commit: string
    date: string

    static createFrom(source: any = {}) {
      return new Info(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.version = source['version']
      this.commit = source['commit']
      this.date = source['date']
    }
  }
}

export namespace main {
  export class BindingError {
    category: string
    code: string
    message: string

    static createFrom(source: any = {}) {
      return new BindingError(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.category = source['category']
      this.code = source['code']
      this.message = source['message']
    }
  }
  export class ActionResult {
    completed: boolean

    static createFrom(source: any = {}) {
      return new ActionResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.completed = source['completed']
    }
  }
  export class ActionResponse {
    result?: ActionResult
    error?: BindingError

    static createFrom(source: any = {}) {
      return new ActionResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], ActionResult)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class AddManualDeviceRequest {
    endpoint: string
    display_name: string

    static createFrom(source: any = {}) {
      return new AddManualDeviceRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.endpoint = source['endpoint']
      this.display_name = source['display_name']
    }
  }
  export class ApplyEncoderRequest {
    device_id: string
    profile_token: string
    change: app.EncoderChange

    static createFrom(source: any = {}) {
      return new ApplyEncoderRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
      this.profile_token = source['profile_token']
      this.change = this.convertValues(source['change'], app.EncoderChange)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class ApplyImagingRequest {
    device_id: string
    profile_token: string
    change: app.ImagingSettingsDTO

    static createFrom(source: any = {}) {
      return new ApplyImagingRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
      this.profile_token = source['profile_token']
      this.change = this.convertValues(source['change'], app.ImagingSettingsDTO)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class ApplyNetworkRequest {
    device_id: string
    change: app.NetworkChange
    confirmation: string

    static createFrom(source: any = {}) {
      return new ApplyNetworkRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
      this.change = this.convertValues(source['change'], app.NetworkChange)
      this.confirmation = source['confirmation']
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class ApplyTimeRequest {
    device_id: string
    change: app.TimeChange

    static createFrom(source: any = {}) {
      return new ApplyTimeRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
      this.change = this.convertValues(source['change'], app.TimeChange)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class AuthenticateDeviceRequest {
    device_id: string
    username: string
    password: string
    allow_basic: boolean
    allow_basic_over_http: boolean
    allow_legacy_digest: boolean
    timeout_milliseconds: number

    static createFrom(source: any = {}) {
      return new AuthenticateDeviceRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
      this.username = source['username']
      this.password = source['password']
      this.allow_basic = source['allow_basic']
      this.allow_basic_over_http = source['allow_basic_over_http']
      this.allow_legacy_digest = source['allow_legacy_digest']
      this.timeout_milliseconds = source['timeout_milliseconds']
    }
  }
  export class AuthenticateDeviceResponse {
    result?: app.AuthenticateResult
    error?: BindingError

    static createFrom(source: any = {}) {
      return new AuthenticateDeviceResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], app.AuthenticateResult)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class CapabilityFlags {
    demo: boolean
    rtsp_playback_available: boolean
    vlc_available: boolean
    live_preview_available: boolean

    static createFrom(source: any = {}) {
      return new CapabilityFlags(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.demo = source['demo']
      this.rtsp_playback_available = source['rtsp_playback_available']
      this.vlc_available = source['vlc_available']
      this.live_preview_available = source['live_preview_available']
    }
  }
  export class BootstrapResult {
    overview: app.Overview
    capabilities: CapabilityFlags

    static createFrom(source: any = {}) {
      return new BootstrapResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.overview = this.convertValues(source['overview'], app.Overview)
      this.capabilities = this.convertValues(
        source['capabilities'],
        CapabilityFlags
      )
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class BootstrapResponse {
    result?: BootstrapResult
    error?: BindingError

    static createFrom(source: any = {}) {
      return new BootstrapResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], BootstrapResult)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class CancelScanResult {
    cancelled: boolean

    static createFrom(source: any = {}) {
      return new CancelScanResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.cancelled = source['cancelled']
    }
  }
  export class CancelScanResponse {
    result?: CancelScanResult
    error?: BindingError

    static createFrom(source: any = {}) {
      return new CancelScanResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], CancelScanResult)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class ClockCheckRequest {
    device_id: string
    profile_token: string

    static createFrom(source: any = {}) {
      return new ClockCheckRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
      this.profile_token = source['profile_token']
    }
  }
  export class ClockCheckResponse {
    result?: app.ClockCheckResult
    error?: BindingError

    static createFrom(source: any = {}) {
      return new ClockCheckResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], app.ClockCheckResult)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class ConsoleEntriesRequest {
    after_id: number

    static createFrom(source: any = {}) {
      return new ConsoleEntriesRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.after_id = source['after_id']
    }
  }
  export class ConsoleState {
    enabled: boolean
    entries: app.SOAPConsoleEntry[]

    static createFrom(source: any = {}) {
      return new ConsoleState(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.enabled = source['enabled']
      this.entries = this.convertValues(source['entries'], app.SOAPConsoleEntry)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class ConsoleResponse {
    result?: ConsoleState
    error?: BindingError

    static createFrom(source: any = {}) {
      return new ConsoleResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], ConsoleState)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class DeleteUserRequest {
    device_id: string
    username: string

    static createFrom(source: any = {}) {
      return new DeleteUserRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
      this.username = source['username']
    }
  }
  export class DeviceIDOnlyRequest {
    device_id: string

    static createFrom(source: any = {}) {
      return new DeviceIDOnlyRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
    }
  }
  export class DeviceIDRequest {
    device_id: string

    static createFrom(source: any = {}) {
      return new DeviceIDRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
    }
  }
  export class DeviceResponse {
    result?: app.DeviceDTO
    error?: BindingError

    static createFrom(source: any = {}) {
      return new DeviceResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], app.DeviceDTO)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class EncoderApplyResponse {
    result?: app.EncoderApplyResult
    error?: BindingError

    static createFrom(source: any = {}) {
      return new EncoderApplyResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], app.EncoderApplyResult)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class EncoderRequest {
    device_id: string
    profile_token: string

    static createFrom(source: any = {}) {
      return new EncoderRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
      this.profile_token = source['profile_token']
    }
  }
  export class EncoderViewResponse {
    result?: app.EncoderView
    error?: BindingError

    static createFrom(source: any = {}) {
      return new EncoderViewResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], app.EncoderView)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class EventFeedRequest {
    feed_id: string
    after_id: number

    static createFrom(source: any = {}) {
      return new EventFeedRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.feed_id = source['feed_id']
      this.after_id = source['after_id']
    }
  }
  export class EventRecord {
    id: number
    received_at: string
    topic: string
    utc_time?: string
    property_operation?: string
    source: app.EventItemDTO[]
    data: app.EventItemDTO[]

    static createFrom(source: any = {}) {
      return new EventRecord(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.id = source['id']
      this.received_at = source['received_at']
      this.topic = source['topic']
      this.utc_time = source['utc_time']
      this.property_operation = source['property_operation']
      this.source = this.convertValues(source['source'], app.EventItemDTO)
      this.data = this.convertValues(source['data'], app.EventItemDTO)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class EventFeedState {
    feed_id: string
    state: string
    error?: BindingError
    events: EventRecord[]

    static createFrom(source: any = {}) {
      return new EventFeedState(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.feed_id = source['feed_id']
      this.state = source['state']
      this.error = this.convertValues(source['error'], BindingError)
      this.events = this.convertValues(source['events'], EventRecord)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class EventFeedResponse {
    result?: EventFeedState
    error?: BindingError

    static createFrom(source: any = {}) {
      return new EventFeedResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], EventFeedState)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class ExportReportRequest {
    device_id: string

    static createFrom(source: any = {}) {
      return new ExportReportRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
    }
  }
  export class FactoryResetRequest {
    device_id: string
    confirmation: string

    static createFrom(source: any = {}) {
      return new FactoryResetRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
      this.confirmation = source['confirmation']
    }
  }
  export class ImagingApplyResponse {
    result?: app.ImagingApplyResult
    error?: BindingError

    static createFrom(source: any = {}) {
      return new ImagingApplyResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], app.ImagingApplyResult)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class ImagingViewResponse {
    result?: app.ImagingView
    error?: BindingError

    static createFrom(source: any = {}) {
      return new ImagingViewResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], app.ImagingView)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class ListInterfacesRequest {
    include_loopback: boolean

    static createFrom(source: any = {}) {
      return new ListInterfacesRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.include_loopback = source['include_loopback']
    }
  }
  export class ListInterfacesResponse {
    result?: app.HostInterfacesResult
    error?: BindingError

    static createFrom(source: any = {}) {
      return new ListInterfacesResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(
        source['result'],
        app.HostInterfacesResult
      )
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class LoadProfilesRequest {
    device_id: string

    static createFrom(source: any = {}) {
      return new LoadProfilesRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
    }
  }
  export class LoadProfilesResponse {
    result?: app.MediaProfilesResult
    error?: BindingError

    static createFrom(source: any = {}) {
      return new LoadProfilesResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(
        source['result'],
        app.MediaProfilesResult
      )
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class MaintenanceResponse {
    result?: app.MaintenanceResult
    error?: BindingError

    static createFrom(source: any = {}) {
      return new MaintenanceResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], app.MaintenanceResult)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class NetworkApplyResponse {
    result?: app.NetworkApplyResult
    error?: BindingError

    static createFrom(source: any = {}) {
      return new NetworkApplyResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], app.NetworkApplyResult)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class NetworkViewResponse {
    result?: app.NetworkView
    error?: BindingError

    static createFrom(source: any = {}) {
      return new NetworkViewResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], app.NetworkView)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class OpenInVLCRequest {
    device_id: string
    profile_token: string
    transport: string

    static createFrom(source: any = {}) {
      return new OpenInVLCRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
      this.profile_token = source['profile_token']
      this.transport = source['transport']
    }
  }
  export class OverviewResponse {
    result?: app.Overview
    error?: BindingError

    static createFrom(source: any = {}) {
      return new OverviewResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], app.Overview)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class PreviewStats {
    width: number
    height: number
    frames_per_second: number
    bitrate_kbps: number
    gop_frames: number
    gop_seconds: number
    time_to_first_frame_ms: number
    frames: number
    keyframes: number
    lost_packets: number
    reconnects: number
    elapsed_ms: number
    first_frame_received: boolean

    static createFrom(source: any = {}) {
      return new PreviewStats(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.width = source['width']
      this.height = source['height']
      this.frames_per_second = source['frames_per_second']
      this.bitrate_kbps = source['bitrate_kbps']
      this.gop_frames = source['gop_frames']
      this.gop_seconds = source['gop_seconds']
      this.time_to_first_frame_ms = source['time_to_first_frame_ms']
      this.frames = source['frames']
      this.keyframes = source['keyframes']
      this.lost_packets = source['lost_packets']
      this.reconnects = source['reconnects']
      this.elapsed_ms = source['elapsed_ms']
      this.first_frame_received = source['first_frame_received']
    }
  }
  export class PreviewStatsRequest {
    session_id: string

    static createFrom(source: any = {}) {
      return new PreviewStatsRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.session_id = source['session_id']
    }
  }
  export class PreviewStatsResponse {
    result?: PreviewStats
    error?: BindingError

    static createFrom(source: any = {}) {
      return new PreviewStatsResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], PreviewStats)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class ProbeStreamRequest {
    device_id: string
    profile_token: string
    transport: string
    duration_seconds: number

    static createFrom(source: any = {}) {
      return new ProbeStreamRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
      this.profile_token = source['profile_token']
      this.transport = source['transport']
      this.duration_seconds = source['duration_seconds']
    }
  }
  export class ProbeStreamResponse {
    result?: app.ProbeResult
    error?: BindingError

    static createFrom(source: any = {}) {
      return new ProbeStreamResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], app.ProbeResult)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class RecordingRequest {
    session_id: string

    static createFrom(source: any = {}) {
      return new RecordingRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.session_id = source['session_id']
    }
  }
  export class RecordingResult {
    path: string
    bytes: number
    interrupted: boolean

    static createFrom(source: any = {}) {
      return new RecordingResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.path = source['path']
      this.bytes = source['bytes']
      this.interrupted = source['interrupted']
    }
  }
  export class RecordingResponse {
    result?: RecordingResult
    error?: BindingError

    static createFrom(source: any = {}) {
      return new RecordingResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], RecordingResult)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class ReportExport {
    markdown_path: string
    json_path: string
    markdown: string
    report: app.DeviceReport

    static createFrom(source: any = {}) {
      return new ReportExport(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.markdown_path = source['markdown_path']
      this.json_path = source['json_path']
      this.markdown = source['markdown']
      this.report = this.convertValues(source['report'], app.DeviceReport)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class ReportExportResponse {
    result?: ReportExport
    error?: BindingError

    static createFrom(source: any = {}) {
      return new ReportExportResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], ReportExport)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class RevealClipRequest {
    path: string

    static createFrom(source: any = {}) {
      return new RevealClipRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.path = source['path']
    }
  }
  export class RevealReportRequest {
    path: string

    static createFrom(source: any = {}) {
      return new RevealReportRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.path = source['path']
    }
  }
  export class ScanRequest {
    interface_name: string
    timeout_milliseconds: number

    static createFrom(source: any = {}) {
      return new ScanRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.interface_name = source['interface_name']
      this.timeout_milliseconds = source['timeout_milliseconds']
    }
  }
  export class ScanResponse {
    result?: app.ScanResult
    error?: BindingError

    static createFrom(source: any = {}) {
      return new ScanResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], app.ScanResult)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class SetConsoleEnabledRequest {
    enabled: boolean

    static createFrom(source: any = {}) {
      return new SetConsoleEnabledRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.enabled = source['enabled']
    }
  }
  export class StabilityJobRequest {
    job_id: string

    static createFrom(source: any = {}) {
      return new StabilityJobRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.job_id = source['job_id']
    }
  }
  export class StabilityJobState {
    job_id: string
    state: string
    error?: BindingError
    cycles: app.StabilityCycle[]
    report?: app.StabilityReport

    static createFrom(source: any = {}) {
      return new StabilityJobState(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.job_id = source['job_id']
      this.state = source['state']
      this.error = this.convertValues(source['error'], BindingError)
      this.cycles = this.convertValues(source['cycles'], app.StabilityCycle)
      this.report = this.convertValues(source['report'], app.StabilityReport)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class StabilityJobResponse {
    result?: StabilityJobState
    error?: BindingError

    static createFrom(source: any = {}) {
      return new StabilityJobResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], StabilityJobState)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class StartEventsRequest {
    device_id: string
    topic_filter: string

    static createFrom(source: any = {}) {
      return new StartEventsRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
      this.topic_filter = source['topic_filter']
    }
  }
  export class StartPreviewRequest {
    device_id: string
    profile_token: string

    static createFrom(source: any = {}) {
      return new StartPreviewRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
      this.profile_token = source['profile_token']
    }
  }
  export class StartPreviewResult {
    session_id: string
    url: string
    token: string

    static createFrom(source: any = {}) {
      return new StartPreviewResult(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.session_id = source['session_id']
      this.url = source['url']
      this.token = source['token']
    }
  }
  export class StartPreviewResponse {
    result?: StartPreviewResult
    error?: BindingError

    static createFrom(source: any = {}) {
      return new StartPreviewResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], StartPreviewResult)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }

  export class StartStabilityRequest {
    device_id: string
    profile_token: string
    transport: string
    cycles: number
    cycle_seconds: number
    pause_seconds: number
    accept_long_run: boolean

    static createFrom(source: any = {}) {
      return new StartStabilityRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
      this.profile_token = source['profile_token']
      this.transport = source['transport']
      this.cycles = source['cycles']
      this.cycle_seconds = source['cycle_seconds']
      this.pause_seconds = source['pause_seconds']
      this.accept_long_run = source['accept_long_run']
    }
  }
  export class StopPreviewRequest {
    session_id: string

    static createFrom(source: any = {}) {
      return new StopPreviewRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.session_id = source['session_id']
    }
  }
  export class SystemLogRequest {
    device_id: string
    type: string

    static createFrom(source: any = {}) {
      return new SystemLogRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
      this.type = source['type']
    }
  }
  export class SystemLogResponse {
    result?: app.SystemLogResult
    error?: BindingError

    static createFrom(source: any = {}) {
      return new SystemLogResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], app.SystemLogResult)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class TimeApplyResponse {
    result?: app.TimeApplyResult
    error?: BindingError

    static createFrom(source: any = {}) {
      return new TimeApplyResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], app.TimeApplyResult)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class TimeViewResponse {
    result?: app.TimeView
    error?: BindingError

    static createFrom(source: any = {}) {
      return new TimeViewResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], app.TimeView)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class UpdatePreferencesRequest {
    locale?: string
    theme?: string
    selected_host_interface?: string

    static createFrom(source: any = {}) {
      return new UpdatePreferencesRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.locale = source['locale']
      this.theme = source['theme']
      this.selected_host_interface = source['selected_host_interface']
    }
  }
  export class UserChangeRequest {
    device_id: string
    username: string
    password: string
    level: string

    static createFrom(source: any = {}) {
      return new UserChangeRequest(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.device_id = source['device_id']
      this.username = source['username']
      this.password = source['password']
      this.level = source['level']
    }
  }
  export class UsersApplyResponse {
    result?: app.UsersApplyResult
    error?: BindingError

    static createFrom(source: any = {}) {
      return new UsersApplyResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], app.UsersApplyResult)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
  export class UsersViewResponse {
    result?: app.UsersView
    error?: BindingError

    static createFrom(source: any = {}) {
      return new UsersViewResponse(source)
    }

    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source)
      this.result = this.convertValues(source['result'], app.UsersView)
      this.error = this.convertValues(source['error'], BindingError)
    }

    convertValues(a: any, classs: any, asMap: boolean = false): any {
      if (!a) {
        return a
      }
      if (a.slice && a.map) {
        return (a as any[]).map((elem) => this.convertValues(elem, classs))
      } else if ('object' === typeof a) {
        if (asMap) {
          for (const key of Object.keys(a)) {
            a[key] = new classs(a[key])
          }
          return a
        }
        return new classs(a)
      }
      return a
    }
  }
}
