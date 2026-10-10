import { api } from './api'
import type {
  ApiError,
  AuthenticateInput,
  BuildInfo,
  CapabilityFlags,
  Device,
  HostInterface,
  Inventory,
  Locale,
  MediaProfilesResult,
  OvmApi,
  Overview,
  Tab,
  Theme,
  Transport
} from './api'
import { normalizeLocale, type TranslationKey } from './i18n'
import { applyTheme, isTheme, readStoredTheme } from './theme'

export type AppPhase = 'loading' | 'ready' | 'error'
export type ToastTone = 'success' | 'warning' | 'error' | 'info'

export interface Toast {
  id: number
  key?: TranslationKey
  message?: string
  error?: ApiError
  values?: Record<string, string | number>
  tone: ToastTone
}

export interface ClockMeasurement {
  differenceMs: number
  uncertaintyMs: number
}

export interface InventorySnapshot {
  inventory: Inventory
  clock?: ClockMeasurement
}

export interface ProfilesState {
  loading: boolean
  data?: MediaProfilesResult
  error?: ApiError
}

const emptyOverview = (): Overview => ({
  devices: [],
  locale: 'en',
  theme: readStoredTheme()
})

const defaultCapabilities = (): CapabilityFlags => ({
  demo: false,
  rtsp_playback_available: false,
  vlc_available: false
})

function replaceDevice(devices: Device[], device: Device): Device[] {
  if (!device.id) return devices
  const index = devices.findIndex((candidate) => candidate.id === device.id)
  if (index === -1) return [...devices, device]
  return devices.map((candidate) =>
    candidate.id === device.id ? device : candidate
  )
}

function clockMeasurement(
  inventory: Inventory,
  startedAt: number,
  finishedAt: number
): ClockMeasurement | undefined {
  const cameraUtc = inventory.system_date_and_time?.utc
  if (!cameraUtc) return undefined
  const cameraTime = Date.parse(cameraUtc)
  if (!Number.isFinite(cameraTime)) return undefined

  const midpoint = startedAt + (finishedAt - startedAt) / 2
  return {
    differenceMs: cameraTime - midpoint,
    // ONVIF timestamps in this boundary have whole-second precision.
    uncertaintyMs: Math.ceil((finishedAt - startedAt) / 2) + 500
  }
}

export class AppState {
  phase = $state<AppPhase>('loading')
  overview = $state<Overview>(emptyOverview())
  capabilities = $state<CapabilityFlags>(defaultCapabilities())
  interfaces = $state<HostInterface[]>([])
  buildInfo = $state<BuildInfo>({
    version: 'dev',
    commit: 'unknown',
    date: 'unknown'
  })
  activeTab = $state<Tab>('overview')
  globalError = $state<ApiError | undefined>()
  interfaceError = $state<ApiError | undefined>()
  scanError = $state<ApiError | undefined>()
  scanDiagnostic = $state<{ category: string; detail?: string } | undefined>()
  scanning = $state(false)
  cancellingScan = $state(false)
  refreshing = $state(false)
  addingManual = $state(false)
  authenticatingDeviceId = $state<string | undefined>()
  forgettingDeviceId = $state<string | undefined>()
  inventoryByDevice = $state<Record<string, InventorySnapshot>>({})
  profilesByDevice = $state<Record<string, ProfilesState>>({})
  toasts = $state<Toast[]>([])

  private readonly client: OvmApi
  private toastSequence = 0
  private scanSequence = 0
  // Timer bookkeeping is intentionally not rendered as reactive state.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity
  private readonly toastTimers = new Map<
    number,
    ReturnType<typeof setTimeout>
  >()

  constructor(client: OvmApi = api) {
    this.client = client
    applyTheme(isTheme(this.overview.theme) ? this.overview.theme : 'system')
  }

  get locale(): Locale {
    return normalizeLocale(this.overview.locale)
  }

  get theme(): Theme {
    return isTheme(this.overview.theme) ? this.overview.theme : 'system'
  }

  get selectedDevice(): Device | undefined {
    const selectedID = this.overview.selected_device_id
    return this.overview.devices.find((device) => device.id === selectedID)
  }

  get selectedHostInterface(): HostInterface | undefined {
    const selected = this.overview.selected_host_interface
    return this.interfaces.find((item) => item.name === selected)
  }

  get canScan(): boolean {
    const selected = this.overview.selected_host_interface
    if (selected) {
      return this.interfaces.some(
        (networkInterface) =>
          networkInterface.name === selected && networkInterface.usable
      )
    }
    return this.interfaces.some((networkInterface) => networkInterface.usable)
  }

  async bootstrap(): Promise<void> {
    this.phase = 'loading'
    this.globalError = undefined

    const [bootstrapResponse, buildResponse] = await Promise.all([
      this.client.bootstrap(),
      this.client.buildInfo()
    ])

    if (buildResponse.data) this.buildInfo = buildResponse.data

    if (!bootstrapResponse.data) {
      this.globalError =
        bootstrapResponse.error ??
        ({
          category: 'internal',
          code: 'invalid_response',
          message: 'Bootstrap returned no application state.'
        } satisfies ApiError)
      this.phase = 'error'
      return
    }

    this.capabilities = bootstrapResponse.data.capabilities
    this.setOverview(bootstrapResponse.data.overview)
    if (bootstrapResponse.error) this.globalError = bootstrapResponse.error
    this.phase = 'ready'
    await this.loadInterfaces()
  }

  async loadInterfaces(): Promise<void> {
    const response = await this.client.listInterfaces(this.capabilities.demo)
    if (response.data) this.interfaces = response.data
    this.interfaceError = response.error
  }

  async refresh(): Promise<void> {
    if (this.refreshing) return
    this.refreshing = true

    const selectedID = this.selectedDevice?.id
    const shouldReloadProfiles = this.activeTab === 'streams' && selectedID
    const [overviewResponse, interfaceResponse, profileResponse] =
      await Promise.all([
        this.client.loadOverview(),
        this.client.listInterfaces(this.capabilities.demo),
        shouldReloadProfiles
          ? this.client.loadProfiles(selectedID)
          : Promise.resolve(undefined)
      ])

    if (overviewResponse.data) this.setOverview(overviewResponse.data)
    if (overviewResponse.error) this.globalError = overviewResponse.error
    if (interfaceResponse.data) this.interfaces = interfaceResponse.data
    this.interfaceError = interfaceResponse.error

    if (selectedID && profileResponse) {
      this.profilesByDevice = {
        ...this.profilesByDevice,
        [selectedID]: {
          loading: false,
          data: profileResponse.data,
          error: profileResponse.error
        }
      }
    }

    this.refreshing = false
    if (!overviewResponse.error && !interfaceResponse.error) {
      this.pushToast('toast.refreshed', 'success')
    }
  }

  async activateTab(tab: Tab): Promise<void> {
    this.activeTab = tab
    if (tab === 'streams' && this.selectedDevice) {
      await this.loadProfiles(this.selectedDevice.id)
    }
  }

  async updateLocale(locale: Locale): Promise<void> {
    if (locale === this.locale) return
    const previous = this.overview
    this.setOverview({ ...this.overview, locale })
    const response = await this.client.updatePreferences({ locale })
    if (response.data) this.setOverview(response.data)
    if (response.error) {
      this.setOverview(previous)
      this.pushError(response.error)
      return
    }
    this.pushToast('toast.preferenceSaved', 'success')
  }

  async updateTheme(theme: Theme): Promise<void> {
    if (theme === this.theme) return
    const previous = this.overview
    this.setOverview({ ...this.overview, theme })
    const response = await this.client.updatePreferences({ theme })
    if (response.data) this.setOverview(response.data)
    if (response.error) {
      this.setOverview(previous)
      this.pushError(response.error)
      return
    }
    this.pushToast('toast.preferenceSaved', 'success')
  }

  async updateSelectedInterface(name: string): Promise<void> {
    if (name === (this.overview.selected_host_interface ?? '')) return
    const previous = this.overview
    this.setOverview({
      ...this.overview,
      selected_host_interface: name || undefined
    })
    const response = await this.client.updatePreferences({
      selected_host_interface: name
    })
    if (response.data) this.setOverview(response.data)
    if (response.error) {
      this.setOverview(previous)
      this.pushError(response.error)
      return
    }
    this.pushToast('toast.preferenceSaved', 'success')
  }

  async scan(): Promise<void> {
    if (this.scanning || !this.canScan) return
    const sequence = ++this.scanSequence
    this.scanning = true
    this.scanError = undefined
    this.scanDiagnostic = undefined

    const response = await this.client.scan(
      this.overview.selected_host_interface ?? ''
    )
    if (sequence !== this.scanSequence) return

    if (response.data) {
      this.overview = {
        ...this.overview,
        devices: response.data.devices
      }
      this.scanDiagnostic = response.data.diagnostic
      this.pushToast('toast.scanComplete', 'success', {
        added: response.data.added?.length ?? 0,
        updated: response.data.updated?.length ?? 0
      })
    }
    if (response.error?.category !== 'cancelled') {
      this.scanError = response.error
    }
    this.scanning = false
    this.cancellingScan = false
  }

  async cancelScan(): Promise<void> {
    if (!this.scanning || this.cancellingScan) return
    this.cancellingScan = true
    const response = await this.client.cancelScan()
    if (response.error) {
      this.scanError = response.error
      this.cancellingScan = false
      return
    }
    if (response.data?.cancelled) {
      this.pushToast('toast.scanCancelled', 'info')
    }
  }

  async addManualDevice(
    endpoint: string,
    displayName: string
  ): Promise<boolean> {
    if (this.addingManual) return false
    this.addingManual = true
    const response = await this.client.addManualDevice(endpoint, displayName)

    if (response.data) {
      this.overview = {
        ...this.overview,
        devices: replaceDevice(this.overview.devices, response.data)
      }
    }
    if (response.error || !response.data) {
      if (response.error) this.pushError(response.error)
      this.addingManual = false
      return false
    }

    const selected = await this.client.selectDevice(response.data.id)
    if (selected.data) this.setOverview(selected.data)
    if (selected.error) this.pushError(selected.error)
    this.addingManual = false
    if (selected.error) return false

    this.activeTab = 'overview'
    this.pushToast('toast.deviceAdded', 'success')
    return true
  }

  async selectDevice(deviceId: string): Promise<void> {
    if (deviceId === this.overview.selected_device_id) return
    const response = await this.client.selectDevice(deviceId)
    if (response.data) this.setOverview(response.data)
    if (response.error) {
      this.pushError(response.error)
      return
    }
    this.activeTab = 'overview'
  }

  async forgetDevice(deviceId: string): Promise<boolean> {
    if (this.forgettingDeviceId) return false
    this.forgettingDeviceId = deviceId
    const response = await this.client.forgetDevice(deviceId)
    this.forgettingDeviceId = undefined

    if (response.data) this.setOverview(response.data)
    if (response.error) {
      this.pushError(response.error)
      return false
    }

    this.inventoryByDevice = Object.fromEntries(
      Object.entries(this.inventoryByDevice).filter(([id]) => id !== deviceId)
    )
    this.profilesByDevice = Object.fromEntries(
      Object.entries(this.profilesByDevice).filter(([id]) => id !== deviceId)
    )
    this.activeTab = 'overview'
    this.pushToast('toast.forgotten', 'success')
    return true
  }

  async authenticate(input: AuthenticateInput): Promise<boolean> {
    if (this.authenticatingDeviceId) return false
    this.authenticatingDeviceId = input.device_id
    const startedAt = Date.now()
    // The input is passed through once and is never assigned to reactive state.
    const response = await this.client.authenticate(input)
    const finishedAt = Date.now()
    this.authenticatingDeviceId = undefined

    if (response.data?.inventory?.endpoint) {
      this.inventoryByDevice = {
        ...this.inventoryByDevice,
        [input.device_id]: {
          inventory: response.data.inventory,
          clock: clockMeasurement(
            response.data.inventory,
            startedAt,
            finishedAt
          )
        }
      }
    }
    if (response.data?.device?.id) {
      this.overview = {
        ...this.overview,
        devices: replaceDevice(this.overview.devices, response.data.device)
      }
    }
    if (response.error) {
      this.pushError(response.error)
      return false
    }

    this.pushToast('toast.authenticated', 'success')
    return true
  }

  async loadProfiles(deviceId: string, force = false): Promise<void> {
    const current = this.profilesByDevice[deviceId]
    if (current?.loading || (current?.data && !force)) return

    this.profilesByDevice = {
      ...this.profilesByDevice,
      [deviceId]: { loading: true, data: current?.data }
    }
    const response = await this.client.loadProfiles(deviceId)
    this.profilesByDevice = {
      ...this.profilesByDevice,
      [deviceId]: {
        loading: false,
        data: response.data,
        error: response.error
      }
    }
  }

  async openInVLC(
    deviceId: string,
    profileToken: string,
    transport: Transport
  ): Promise<void> {
    const response = await this.client.openInVLC(
      deviceId,
      profileToken,
      transport
    )
    if (response.error) {
      this.pushError(response.error)
      return
    }
    this.pushToast('toast.vlcOpened', 'success')
  }

  dismissToast(id: number): void {
    this.toasts = this.toasts.filter((toast) => toast.id !== id)
    const timer = this.toastTimers.get(id)
    if (timer) clearTimeout(timer)
    this.toastTimers.delete(id)
  }

  dispose(): void {
    for (const timer of this.toastTimers.values()) clearTimeout(timer)
    this.toastTimers.clear()
  }

  private setOverview(overview: Overview): void {
    const locale = normalizeLocale(overview.locale)
    const theme: Theme = isTheme(overview.theme) ? overview.theme : 'system'
    this.overview = {
      ...overview,
      devices: overview.devices ?? [],
      locale,
      theme
    }
    applyTheme(theme)
    if (typeof document !== 'undefined') {
      document.documentElement.lang = locale
    }
  }

  private pushError(error: ApiError): void {
    this.pushToast(undefined, 'error', undefined, undefined, error)
  }

  private pushToast(
    key: TranslationKey | undefined,
    tone: ToastTone,
    values?: Record<string, string | number>,
    message?: string,
    error?: ApiError
  ): void {
    const id = ++this.toastSequence
    this.toasts = [...this.toasts, { id, key, message, error, values, tone }]
    const timer = setTimeout(() => this.dismissToast(id), 5000)
    this.toastTimers.set(id, timer)
  }
}

export function createAppState(client: OvmApi = api): AppState {
  return new AppState(client)
}
