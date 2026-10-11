import {
  AddManualDevice,
  ApplyEncoder,
  ApplyImaging,
  ApplyNetwork,
  ApplyTime,
  CreateUser,
  DeleteUser,
  AuthenticateDevice,
  Bootstrap,
  BuildInfo as ReadBuildInfo,
  CancelScan,
  ForgetDevice,
  ListInterfaces,
  LoadOverview,
  LoadProfiles,
  OpenInVLC,
  PreviewStats as ReadPreviewStats,
  ReadEncoder,
  ReadImaging,
  ReadNetwork,
  ReadTime,
  ReadUsers,
  Reboot,
  ProbeStream,
  RevealClip,
  Scan,
  SelectDevice,
  SetUser,
  SoftFactoryReset,
  StartPreview,
  StartRecording,
  StopRecording,
  SystemLog,
  StartSnapshotPreview,
  StopPreview,
  UpdatePreferences,
  ClearConsole,
  ClockCheck,
  ConsoleEntries,
  ExportReport,
  PollEvents,
  RevealReport,
  SetConsoleEnabled,
  StabilityStatus,
  StartEvents,
  StartStability,
  StopEvents,
  StopStability
} from '../../wailsjs/go/main/App.js'
import { main } from '../../wailsjs/go/models'
import {
  ClipboardSetText,
  WindowFullscreen,
  WindowUnfullscreen
} from '../../wailsjs/runtime/runtime.js'

export type Locale = 'en' | 'pt-BR'
export type Theme = 'system' | 'light' | 'dark'
export type Tab = 'overview' | 'streams' | 'live' | 'settings' | 'diagnostics'
export type Transport = 'tcp' | 'udp'

export interface ConsoleEntry {
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
}

export interface ConsoleState {
  enabled: boolean
  entries: ConsoleEntry[]
}

export interface EventItem {
  name: string
  value: string
}

export interface EventRecord {
  id: number
  received_at: string
  topic: string
  utc_time?: string
  property_operation?: string
  source: EventItem[]
  data: EventItem[]
}

export interface EventFeedState {
  feed_id: string
  state: string
  error?: ApiError
  events: EventRecord[]
}

export interface ClockEstimate {
  source: string
  offset_seconds: number
  uncertainty_seconds: number
  round_trip_ms?: number
}

export interface ClockCheckResult {
  checked_at: string
  tolerance_seconds: number
  verdict: string
  date_time_type: string
  ntp_servers: string[]
  onvif?: ClockEstimate
  rtcp?: ClockEstimate
  rtcp_unavailable?: string
}

export interface StabilityCycle {
  index: number
  passed: boolean
  error?: string
  result?: ProbeResult
}

export interface StabilityReport {
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
}

export interface StabilityJobState {
  job_id: string
  state: string
  error?: ApiError
  cycles: StabilityCycle[]
  report?: StabilityReport
}

export interface StabilityRequest {
  profileToken: string
  transport: Transport
  cycles: number
  cycleSeconds: number
  pauseSeconds: number
}

export interface ReportExport {
  markdown_path: string
  json_path: string
  markdown: string
}

export interface ApiError {
  category: string
  code: string
  message: string
}

export interface ApiResponse<T> {
  data?: T
  error?: ApiError
}

export interface AuthPolicy {
  allow_basic: boolean
  allow_basic_over_http: boolean
  allow_legacy_digest: boolean
}

export interface Device {
  id: string
  endpoint: string
  endpoint_reference?: string
  display_name?: string
  discovery_source?: string
  has_credentials: boolean
  auth_policy: AuthPolicy
  selected: boolean
  status: string
  created_at?: string
  updated_at?: string
}

export interface Overview {
  devices: Device[]
  selected_device_id?: string
  selected_host_interface?: string
  locale: string
  theme: string
}

export interface CapabilityFlags {
  demo: boolean
  rtsp_playback_available: boolean
  vlc_available: boolean
  live_preview_available: boolean
}

export interface PreviewStats {
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
}

export interface ProbeResult {
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
}

export interface Resolution {
  width: number
  height: number
}

export interface EncoderSettings {
  configuration_token: string
  source: string
  encoding: string
  width: number
  height: number
  frame_rate: number
  bitrate_kbps: number
  gop_length: number
  codec_profile: string
}

export interface EncoderCodecOptions {
  encoding: string
  resolutions: Resolution[] | null
  frame_rates: number[] | null
  frame_rate_min: number
  frame_rate_max: number
  bitrate_min: number
  bitrate_max: number
  gop_min: number
  gop_max: number
  profiles: string[] | null
}

export interface EncoderView {
  profile_token: string
  current: EncoderSettings
  codecs: EncoderCodecOptions[] | null
}

export interface EncoderChange {
  encoding: string
  width: number
  height: number
  frame_rate: number
  bitrate_kbps: number
  gop_length: number
  codec_profile: string
}

// MutationOutcome is 'exact', 'adjusted', 'not_applied', or 'disconnected'.
export type MutationOutcome = string

export interface FieldChange {
  field: string
  before: string
  requested: string
  after: string
}

export interface MutationReport {
  outcome: MutationOutcome
  changes: FieldChange[] | null
}

export interface EncoderApplyResult {
  profile_token: string
  before: EncoderSettings
  after: EncoderSettings
  report: MutationReport
}

export interface ImagingRange {
  min: number
  max: number
}

export interface ImagingSettings {
  brightness?: number
  color_saturation?: number
  contrast?: number
  sharpness?: number
  ir_cut_filter?: string
}

export interface ImagingOptions {
  brightness?: ImagingRange
  color_saturation?: ImagingRange
  contrast?: ImagingRange
  sharpness?: ImagingRange
  ir_cut_modes: string[] | null
}

export interface ImagingView {
  video_source_token: string
  settings: ImagingSettings
  options: ImagingOptions
}

export interface ImagingApplyResult {
  before: ImagingSettings
  after: ImagingSettings
  report: MutationReport
}

export interface TimeView {
  date_time_type: string
  time_zone: string
  daylight_savings: boolean
  device_utc?: string
  clock_offset_seconds?: number
  uncertainty_seconds: number
  ntp_from_dhcp: boolean
  ntp_servers: string[] | null
}

export interface TimeChange {
  mode: string
  ntp_servers: string[]
  time_zone: string
  daylight_savings: boolean
}

export interface TimeApplyResult {
  before: TimeView
  after: TimeView
  report: MutationReport
}

export interface CameraUser {
  username: string
  level: string
}

export interface UsersView {
  users: CameraUser[] | null
  current_username: string
  default_credential_warning: boolean
}

export interface UsersApplyResult {
  users: CameraUser[] | null
  report: MutationReport
  sign_in_verified: boolean
}

export interface NetworkView {
  interface_token: string
  interface_name: string
  hw_address: string
  dhcp: boolean
  address: string
  prefix_length: number
  gateways: string[] | null
  dns_from_dhcp: boolean
  dns_servers: string[] | null
}

export interface NetworkChange {
  dhcp: boolean
  address: string
  prefix_length: number
  gateway: string
  dns_servers: string[]
}

export interface NetworkApplyResult {
  before: NetworkView
  after?: NetworkView
  report: MutationReport
  reboot_requested: boolean
  new_endpoint?: string
}

export interface MaintenanceResult {
  message: string
  back_online: boolean
  downtime_seconds: number
}

export interface SystemLogResult {
  type: string
  text: string
  truncated: boolean
}

export interface RecordingResult {
  path: string
  bytes: number
  interrupted: boolean
}

export interface PreviewSession {
  session_id: string
  url: string
  token: string
}

export interface BootstrapResult {
  overview: Overview
  capabilities: CapabilityFlags
}

export interface HostInterface {
  index: number
  name: string
  loopback: boolean
  usable: boolean
  ipv4?: string[]
  reason: string
}

export interface DiscoveryDiagnostic {
  category: string
  detail?: string
}

export interface ScanConflict {
  endpoint_reference?: string
  endpoint?: string
  reason: string
  detail?: string
}

export interface ScanResult {
  devices: Device[]
  added?: string[]
  updated?: string[]
  conflicts?: ScanConflict[]
  diagnostic?: DiscoveryDiagnostic
}

export interface OperationStatus {
  operation: string
  state: string
  detail?: string
}

export interface SystemDateAndTime {
  date_time_type?: string
  daylight_savings: boolean
  time_zone?: string
  utc?: string
}

export interface DeviceInformation {
  manufacturer?: string
  model?: string
  firmware_version?: string
  serial_number?: string
  hardware_id?: string
}

export interface DeviceCapabilities {
  device_xaddr?: string
  media_xaddr?: string
  events_xaddr?: string
  imaging_xaddr?: string
  ptz_xaddr?: string
  analytics_xaddr?: string
}

export interface Service {
  namespace?: string
  xaddr?: string
  version_major?: number
  version_minor?: number
  has_version: boolean
}

export interface Scope {
  definition?: string
  item?: string
}

export interface Hostname {
  from_dhcp: boolean
  name?: string
}

export interface IPAddress {
  address?: string
  prefix_length?: number
  from_dhcp: boolean
}

export interface CameraNetworkInterface {
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
  ipv4_addresses?: IPAddress[]
}

export interface Inventory {
  endpoint: string
  operations: OperationStatus[]
  system_date_and_time?: SystemDateAndTime
  device_information?: DeviceInformation
  capabilities?: DeviceCapabilities
  services?: Service[]
  scopes?: Scope[]
  hostname?: Hostname
  network_interfaces?: CameraNetworkInterface[]
}

export interface AuthenticateResult {
  device: Device
  inventory: Inventory
}

export interface MediaOperationStatus {
  operation: string
  state: string
  source?: string
  fell_back?: boolean
  detail?: string
}

export interface VideoConfig {
  config_token?: string
  codec?: string
  width?: number
  height?: number
  frame_rate_fps?: number
  has_frame_rate?: boolean
  bitrate_kbps?: number
  gop_length?: number
  profile?: string
}

export interface AudioConfig {
  config_token?: string
  codec?: string
  bitrate_kbps?: number
  sample_rate_khz?: number
}

export interface MediaProfile {
  token: string
  name?: string
  fixed?: boolean
  has_fixed?: boolean
  source?: string
  video_source_config_token?: string
  video_source_token?: string
  video_encoder_config_token?: string
  video?: VideoConfig
  audio?: AudioConfig
  stream?: {
    tcp_uri?: string
    udp_uri?: string
  }
  snapshot?: {
    uri?: string
  }
  operations?: MediaOperationStatus[]
}

export interface MediaProfilesResult {
  profiles: MediaProfile[]
  media2_available: boolean
  media1_available: boolean
}

export interface BuildInfo {
  version: string
  commit: string
  date: string
}

export interface AuthenticateInput {
  device_id: string
  username: string
  password: string
  allow_basic: boolean
  allow_basic_over_http: boolean
  allow_legacy_digest: boolean
  timeout_milliseconds: number
}

export interface PreferenceInput {
  locale?: Locale
  theme?: Theme
  selected_host_interface?: string
}

export interface OvmApi {
  bootstrap(): Promise<ApiResponse<BootstrapResult>>
  buildInfo(): Promise<ApiResponse<BuildInfo>>
  listInterfaces(
    includeLoopback: boolean
  ): Promise<ApiResponse<HostInterface[]>>
  scan(interfaceName: string): Promise<ApiResponse<ScanResult>>
  cancelScan(): Promise<ApiResponse<{ cancelled: boolean }>>
  addManualDevice(
    endpoint: string,
    displayName: string
  ): Promise<ApiResponse<Device>>
  authenticate(
    input: AuthenticateInput
  ): Promise<ApiResponse<AuthenticateResult>>
  forgetDevice(deviceId: string): Promise<ApiResponse<Overview>>
  selectDevice(deviceId: string): Promise<ApiResponse<Overview>>
  loadOverview(): Promise<ApiResponse<Overview>>
  loadProfiles(deviceId: string): Promise<ApiResponse<MediaProfilesResult>>
  updatePreferences(input: PreferenceInput): Promise<ApiResponse<Overview>>
  openInVLC(
    deviceId: string,
    profileToken: string,
    transport: Transport
  ): Promise<ApiResponse<{ completed: boolean }>>
  copyText(text: string): Promise<boolean>
  setFullscreen(fullscreen: boolean): void
  startPreview(
    deviceId: string,
    profileToken: string
  ): Promise<ApiResponse<PreviewSession>>
  stopPreview(sessionId: string): Promise<ApiResponse<{ completed: boolean }>>
  startSnapshotPreview(
    deviceId: string,
    profileToken: string
  ): Promise<ApiResponse<PreviewSession>>
  previewStats(sessionId: string): Promise<ApiResponse<PreviewStats>>
  probeStream(
    deviceId: string,
    profileToken: string,
    transport: Transport,
    durationSeconds: number
  ): Promise<ApiResponse<ProbeResult>>
  revealClip(path: string): Promise<ApiResponse<{ completed: boolean }>>
  readTime(deviceId: string): Promise<ApiResponse<TimeView>>
  applyTime(
    deviceId: string,
    change: TimeChange
  ): Promise<ApiResponse<TimeApplyResult>>
  readUsers(deviceId: string): Promise<ApiResponse<UsersView>>
  setUser(
    deviceId: string,
    username: string,
    password: string,
    level: string
  ): Promise<ApiResponse<UsersApplyResult>>
  createUser(
    deviceId: string,
    username: string,
    password: string,
    level: string
  ): Promise<ApiResponse<UsersApplyResult>>
  deleteUser(
    deviceId: string,
    username: string
  ): Promise<ApiResponse<UsersApplyResult>>
  readNetwork(deviceId: string): Promise<ApiResponse<NetworkView>>
  applyNetwork(
    deviceId: string,
    change: NetworkChange,
    confirmation: string
  ): Promise<ApiResponse<NetworkApplyResult>>
  reboot(deviceId: string): Promise<ApiResponse<MaintenanceResult>>
  softFactoryReset(
    deviceId: string,
    confirmation: string
  ): Promise<ApiResponse<MaintenanceResult>>
  systemLog(
    deviceId: string,
    type: string
  ): Promise<ApiResponse<SystemLogResult>>
  readImaging(
    deviceId: string,
    profileToken: string
  ): Promise<ApiResponse<ImagingView>>
  applyImaging(
    deviceId: string,
    profileToken: string,
    change: ImagingSettings
  ): Promise<ApiResponse<ImagingApplyResult>>
  readEncoder(
    deviceId: string,
    profileToken: string
  ): Promise<ApiResponse<EncoderView>>
  applyEncoder(
    deviceId: string,
    profileToken: string,
    change: EncoderChange
  ): Promise<ApiResponse<EncoderApplyResult>>
  startRecording(sessionId: string): Promise<ApiResponse<RecordingResult>>
  stopRecording(sessionId: string): Promise<ApiResponse<RecordingResult>>
  setConsoleEnabled(enabled: boolean): Promise<ApiResponse<ConsoleState>>
  consoleEntries(afterId: number): Promise<ApiResponse<ConsoleState>>
  clearConsole(): Promise<ApiResponse<ConsoleState>>
  startEvents(
    deviceId: string,
    topicFilter: string
  ): Promise<ApiResponse<EventFeedState>>
  pollEvents(
    feedId: string,
    afterId: number
  ): Promise<ApiResponse<EventFeedState>>
  stopEvents(feedId: string): Promise<ApiResponse<{ completed: boolean }>>
  clockCheck(
    deviceId: string,
    profileToken: string
  ): Promise<ApiResponse<ClockCheckResult>>
  startStability(
    deviceId: string,
    request: StabilityRequest
  ): Promise<ApiResponse<StabilityJobState>>
  stabilityStatus(jobId: string): Promise<ApiResponse<StabilityJobState>>
  stopStability(jobId: string): Promise<ApiResponse<{ completed: boolean }>>
  exportReport(deviceId: string): Promise<ApiResponse<ReportExport>>
  revealReport(path: string): Promise<ApiResponse<{ completed: boolean }>>
}

interface BindingEnvelope<T> {
  result?: T
  error?: ApiError
}

const bridgeError = (): ApiError => ({
  category: 'internal',
  code: 'bridge_unavailable',
  message: 'The desktop bridge is unavailable.'
})

const invalidResponseError = (): ApiError => ({
  category: 'internal',
  code: 'invalid_response',
  message: 'The desktop bridge returned an invalid response.'
})

async function invoke<T>(
  call: () => Promise<BindingEnvelope<T>>
): Promise<ApiResponse<T>> {
  try {
    const response = await call()
    if (response.result === undefined && response.error === undefined) {
      return { error: invalidResponseError() }
    }
    return { data: response.result, error: response.error }
  } catch {
    return { error: bridgeError() }
  }
}

export const api: OvmApi = {
  bootstrap: () => invoke<BootstrapResult>(() => Bootstrap()),
  async buildInfo() {
    try {
      return { data: await ReadBuildInfo() }
    } catch {
      return { error: bridgeError() }
    }
  },
  async listInterfaces(includeLoopback) {
    const response = await invoke<{ interfaces: HostInterface[] }>(() =>
      ListInterfaces({ include_loopback: includeLoopback })
    )
    return { data: response.data?.interfaces, error: response.error }
  },
  scan: (interfaceName) =>
    invoke<ScanResult>(() =>
      Scan({ interface_name: interfaceName, timeout_milliseconds: 0 })
    ),
  cancelScan: () => invoke<{ cancelled: boolean }>(() => CancelScan()),
  addManualDevice: (endpoint, displayName) =>
    invoke<Device>(() =>
      AddManualDevice({ endpoint, display_name: displayName })
    ),
  authenticate: (input) =>
    invoke<AuthenticateResult>(() => AuthenticateDevice(input)),
  forgetDevice: (deviceId) =>
    invoke<Overview>(() => ForgetDevice({ device_id: deviceId })),
  selectDevice: (deviceId) =>
    invoke<Overview>(() => SelectDevice({ device_id: deviceId })),
  loadOverview: () => invoke<Overview>(() => LoadOverview()),
  loadProfiles: (deviceId) =>
    invoke<MediaProfilesResult>(() => LoadProfiles({ device_id: deviceId })),
  updatePreferences: (input) =>
    invoke<Overview>(() => UpdatePreferences(input)),
  openInVLC: (deviceId, profileToken, transport) =>
    invoke<{ completed: boolean }>(() =>
      OpenInVLC({
        device_id: deviceId,
        profile_token: profileToken,
        transport
      })
    ),
  startPreview: (deviceId, profileToken) =>
    invoke<PreviewSession>(() =>
      StartPreview({ device_id: deviceId, profile_token: profileToken })
    ),
  startSnapshotPreview: (deviceId, profileToken) =>
    invoke<PreviewSession>(() =>
      StartSnapshotPreview({ device_id: deviceId, profile_token: profileToken })
    ),
  stopPreview: (sessionId) =>
    invoke<{ completed: boolean }>(() =>
      StopPreview({ session_id: sessionId })
    ),
  probeStream: (deviceId, profileToken, transport, durationSeconds) =>
    invoke<ProbeResult>(() =>
      ProbeStream({
        device_id: deviceId,
        profile_token: profileToken,
        transport,
        duration_seconds: durationSeconds
      })
    ),
  readEncoder: (deviceId, profileToken) =>
    invoke<EncoderView>(() =>
      ReadEncoder({ device_id: deviceId, profile_token: profileToken })
    ),
  applyEncoder: (deviceId, profileToken, change) =>
    invoke<EncoderApplyResult>(() =>
      ApplyEncoder(
        main.ApplyEncoderRequest.createFrom({
          device_id: deviceId,
          profile_token: profileToken,
          change
        })
      )
    ),
  readImaging: (deviceId, profileToken) =>
    invoke<ImagingView>(() =>
      ReadImaging({ device_id: deviceId, profile_token: profileToken })
    ),
  applyImaging: (deviceId, profileToken, change) =>
    invoke<ImagingApplyResult>(() =>
      ApplyImaging(
        main.ApplyImagingRequest.createFrom({
          device_id: deviceId,
          profile_token: profileToken,
          change
        })
      )
    ),
  readTime: (deviceId) =>
    invoke<TimeView>(() => ReadTime({ device_id: deviceId })),
  applyTime: (deviceId, change) =>
    invoke<TimeApplyResult>(() =>
      ApplyTime(
        main.ApplyTimeRequest.createFrom({ device_id: deviceId, change })
      )
    ),
  readUsers: (deviceId) =>
    invoke<UsersView>(() => ReadUsers({ device_id: deviceId })),
  setUser: (deviceId, username, password, level) =>
    invoke<UsersApplyResult>(() =>
      SetUser({ device_id: deviceId, username, password, level })
    ),
  createUser: (deviceId, username, password, level) =>
    invoke<UsersApplyResult>(() =>
      CreateUser({ device_id: deviceId, username, password, level })
    ),
  deleteUser: (deviceId, username) =>
    invoke<UsersApplyResult>(() =>
      DeleteUser({ device_id: deviceId, username })
    ),
  readNetwork: (deviceId) =>
    invoke<NetworkView>(() => ReadNetwork({ device_id: deviceId })),
  applyNetwork: (deviceId, change, confirmation) =>
    invoke<NetworkApplyResult>(() =>
      ApplyNetwork(
        main.ApplyNetworkRequest.createFrom({
          device_id: deviceId,
          change,
          confirmation
        })
      )
    ),
  reboot: (deviceId) =>
    invoke<MaintenanceResult>(() => Reboot({ device_id: deviceId })),
  softFactoryReset: (deviceId, confirmation) =>
    invoke<MaintenanceResult>(() =>
      SoftFactoryReset({ device_id: deviceId, confirmation })
    ),
  systemLog: (deviceId, type) =>
    invoke<SystemLogResult>(() => SystemLog({ device_id: deviceId, type })),
  revealClip: (path) =>
    invoke<{ completed: boolean }>(() => RevealClip({ path })),
  startRecording: (sessionId) =>
    invoke<RecordingResult>(() => StartRecording({ session_id: sessionId })),
  stopRecording: (sessionId) =>
    invoke<RecordingResult>(() => StopRecording({ session_id: sessionId })),
  previewStats: (sessionId) =>
    invoke<PreviewStats>(() => ReadPreviewStats({ session_id: sessionId })),
  setConsoleEnabled: (enabled) =>
    invoke<ConsoleState>(() => SetConsoleEnabled({ enabled })),
  consoleEntries: (afterId) =>
    invoke<ConsoleState>(() => ConsoleEntries({ after_id: afterId })),
  clearConsole: () => invoke<ConsoleState>(() => ClearConsole()),
  startEvents: (deviceId, topicFilter) =>
    invoke<EventFeedState>(() =>
      StartEvents({ device_id: deviceId, topic_filter: topicFilter })
    ),
  pollEvents: (feedId, afterId) =>
    invoke<EventFeedState>(() =>
      PollEvents({ feed_id: feedId, after_id: afterId })
    ),
  stopEvents: (feedId) =>
    invoke<{ completed: boolean }>(() =>
      StopEvents({ feed_id: feedId, after_id: 0 })
    ),
  clockCheck: (deviceId, profileToken) =>
    invoke<ClockCheckResult>(() =>
      ClockCheck({ device_id: deviceId, profile_token: profileToken })
    ),
  startStability: (deviceId, request) =>
    invoke<StabilityJobState>(() =>
      StartStability({
        device_id: deviceId,
        profile_token: request.profileToken,
        transport: request.transport,
        cycles: request.cycles,
        cycle_seconds: request.cycleSeconds,
        pause_seconds: request.pauseSeconds,
        accept_long_run: true
      })
    ),
  stabilityStatus: (jobId) =>
    invoke<StabilityJobState>(() => StabilityStatus({ job_id: jobId })),
  stopStability: (jobId) =>
    invoke<{ completed: boolean }>(() => StopStability({ job_id: jobId })),
  exportReport: (deviceId) =>
    invoke<ReportExport>(() => ExportReport({ device_id: deviceId })),
  revealReport: (path) =>
    invoke<{ completed: boolean }>(() => RevealReport({ path })),
  async copyText(text) {
    // The native clipboard works even when the WebView blocks navigator.clipboard.
    try {
      if (await ClipboardSetText(text)) return true
    } catch {
      // Fall through to the web clipboard when the runtime is unavailable.
    }
    try {
      if (!navigator.clipboard?.writeText) return false
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      return false
    }
  },
  setFullscreen(fullscreen) {
    try {
      if (fullscreen) WindowFullscreen()
      else WindowUnfullscreen()
    } catch {
      // Outside the desktop runtime the expanded tile still fills the window.
    }
  }
}
