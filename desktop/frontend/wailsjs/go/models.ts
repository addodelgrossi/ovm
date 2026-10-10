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
}
