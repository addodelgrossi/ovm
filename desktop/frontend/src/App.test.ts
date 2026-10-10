import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within
} from '@testing-library/svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App.svelte'
import type {
  ApiResponse,
  BootstrapResult,
  Device,
  HostInterface,
  Inventory,
  MediaProfilesResult,
  Overview,
  PreferenceInput
} from './lib/api'

const apiMock = vi.hoisted(() => ({
  bootstrap: vi.fn(),
  buildInfo: vi.fn(),
  listInterfaces: vi.fn(),
  scan: vi.fn(),
  cancelScan: vi.fn(),
  addManualDevice: vi.fn(),
  authenticate: vi.fn(),
  forgetDevice: vi.fn(),
  selectDevice: vi.fn(),
  loadOverview: vi.fn(),
  loadProfiles: vi.fn(),
  updatePreferences: vi.fn(),
  openInVLC: vi.fn(),
  copyText: vi.fn(),
  startPreview: vi.fn(),
  stopPreview: vi.fn(),
  previewStats: vi.fn(),
  startSnapshotPreview: vi.fn(),
  setFullscreen: vi.fn(),
  startRecording: vi.fn(),
  stopRecording: vi.fn()
}))

const liveSession = {
  session_id: '0123456789abcdef0123456789abcdef',
  url: 'http://127.0.0.1:49152/v1/sessions/0123456789abcdef0123456789abcdef',
  token: 'synthetic-token'
}

const snapshotSession = {
  session_id: 'fedcba9876543210fedcba9876543210',
  url: 'http://127.0.0.1:49152/v1/snapshots/fedcba9876543210fedcba9876543210',
  token: 'synthetic-token'
}

function stubMediaSource(): void {
  class PendingMediaSource {
    static isTypeSupported(): boolean {
      return true
    }
    addEventListener(): void {}
  }
  vi.stubGlobal('MediaSource', PendingMediaSource)
  vi.stubGlobal(
    'URL',
    Object.assign(URL, {
      createObjectURL: () => 'blob:stub',
      revokeObjectURL: () => undefined
    })
  )
}

vi.mock('./lib/api', () => ({ api: apiMock }))

const frontCamera: Device = {
  id: 'camera-front',
  endpoint: 'https://192.0.2.10/onvif/device_service',
  endpoint_reference: 'urn:uuid:camera-front',
  display_name: 'Front camera',
  discovery_source: 'ws_discovery',
  has_credentials: false,
  auth_policy: {
    allow_basic: false,
    allow_basic_over_http: false,
    allow_legacy_digest: false
  },
  selected: true,
  status: 'authentication_required'
}

const garageCamera: Device = {
  ...frontCamera,
  id: 'camera-garage',
  endpoint: 'http://192.0.2.11/onvif/device_service',
  endpoint_reference: 'urn:uuid:camera-garage',
  display_name: 'Garage camera',
  discovery_source: 'manual',
  selected: false,
  status: 'reachable'
}

const hostInterfaces: HostInterface[] = [
  {
    index: 4,
    name: 'en0',
    loopback: false,
    usable: true,
    ipv4: ['192.0.2.2'],
    reason: 'usable'
  },
  {
    index: 5,
    name: 'utun4',
    loopback: false,
    usable: false,
    reason: 'multicast_unsupported'
  }
]

function makeOverview(
  devices: Device[] = [frontCamera],
  selectedDeviceID: string | undefined = 'camera-front',
  locale = 'en',
  theme = 'system'
): Overview {
  return {
    devices: devices.map((device) => ({
      ...device,
      selected: device.id === selectedDeviceID
    })),
    selected_device_id: selectedDeviceID,
    selected_host_interface: 'en0',
    locale,
    theme
  }
}

function makeBootstrap(
  overview: Overview = makeOverview(),
  capabilities: BootstrapResult['capabilities'] = {
    demo: false,
    rtsp_playback_available: true,
    vlc_available: false,
    live_preview_available: false
  }
): BootstrapResult {
  return { overview, capabilities }
}

function makeInventory(
  cameraUTC = new Date(Date.now() - 5000).toISOString()
): Inventory {
  return {
    endpoint: frontCamera.endpoint,
    system_date_and_time: {
      date_time_type: 'NTP',
      daylight_savings: false,
      time_zone: 'UTC',
      utc: cameraUTC
    },
    device_information: {
      manufacturer: 'OVM Synthetic',
      model: 'FakeCam 1000',
      firmware_version: '1.0.0-synthetic',
      serial_number: 'SYNTHETIC-0001',
      hardware_id: 'OVM-FAKE-1'
    },
    capabilities: {
      device_xaddr: frontCamera.endpoint,
      media_xaddr: 'https://192.0.2.10/onvif/media_service',
      events_xaddr: 'https://192.0.2.10/onvif/events_service',
      ptz_xaddr: 'https://192.0.2.10/onvif/ptz_service'
    },
    services: [
      {
        namespace: 'http://www.onvif.org/ver10/device/wsdl',
        xaddr: frontCamera.endpoint,
        version_major: 2,
        version_minor: 6,
        has_version: true
      }
    ],
    scopes: [
      {
        definition: 'Fixed',
        item: 'onvif://www.onvif.org/name/FrontCamera'
      }
    ],
    hostname: { from_dhcp: true, name: 'synthetic-camera' },
    network_interfaces: [
      {
        token: 'eth0',
        name: 'Camera Ethernet',
        enabled: true,
        has_enabled: true,
        hw_address: '00:00:5E:00:53:01',
        mtu: 1500,
        has_mtu: true,
        ipv4_enabled: true,
        has_ipv4: true,
        dhcp: false,
        has_dhcp: true,
        ipv4_addresses: [
          {
            address: '203.0.113.10',
            prefix_length: 24,
            from_dhcp: false
          }
        ]
      }
    ],
    operations: [
      { operation: 'system_date_and_time', state: 'supported' },
      { operation: 'device_information', state: 'supported' },
      { operation: 'capabilities', state: 'supported' },
      { operation: 'services', state: 'supported' },
      { operation: 'scopes', state: 'supported' },
      { operation: 'hostname', state: 'supported' },
      {
        operation: 'network_interfaces',
        state: 'temporarily_unavailable',
        detail: 'Partial camera response'
      }
    ]
  }
}

const mediaProfiles: MediaProfilesResult = {
  media2_available: true,
  media1_available: true,
  profiles: [
    {
      token: 'profile_main',
      name: 'Main stream',
      fixed: true,
      has_fixed: true,
      source: 'media2',
      video_source_config_token: 'video-source-main',
      video_source_token: 'source-main',
      video_encoder_config_token: 'encoder-main',
      video: {
        codec: 'H264',
        width: 1920,
        height: 1080,
        frame_rate_fps: 30,
        has_frame_rate: true,
        bitrate_kbps: 4096,
        gop_length: 30,
        profile: 'Main'
      },
      audio: {
        codec: 'PCMU',
        bitrate_kbps: 64,
        sample_rate_khz: 8
      },
      stream: {
        tcp_uri: 'rtsp://192.0.2.10/main-tcp',
        udp_uri: 'rtsp://192.0.2.10/main-udp'
      },
      snapshot: { uri: 'https://192.0.2.10/snapshot.jpg' },
      operations: [
        {
          operation: 'profiles',
          state: 'supported',
          source: 'media2',
          fell_back: false
        },
        {
          operation: 'stream_tcp',
          state: 'supported',
          source: 'media2',
          fell_back: false
        },
        {
          operation: 'stream_udp',
          state: 'supported',
          source: 'media1',
          fell_back: true
        },
        {
          operation: 'snapshot_uri',
          state: 'supported',
          source: 'media1',
          fell_back: true
        }
      ]
    },
    {
      token: 'profile_sub',
      name: 'Sub stream',
      fixed: false,
      has_fixed: true,
      source: 'media1',
      video: {
        codec: 'H264',
        width: 640,
        height: 360,
        frame_rate_fps: 15,
        has_frame_rate: true,
        bitrate_kbps: 512,
        gop_length: 15
      },
      operations: [
        {
          operation: 'profiles',
          state: 'supported',
          source: 'media1'
        },
        {
          operation: 'audio_encoder',
          state: 'unsupported',
          source: 'media1'
        }
      ]
    }
  ]
}

function deferred<T>(): {
  promise: Promise<T>
  resolve: (value: T) => void
} {
  let resolvePromise!: (value: T) => void
  return {
    promise: new Promise<T>((resolve) => {
      resolvePromise = resolve
    }),
    resolve: resolvePromise
  }
}

function setDefaultMocks(): void {
  const overview = makeOverview()
  apiMock.bootstrap.mockResolvedValue({ data: makeBootstrap(overview) })
  apiMock.buildInfo.mockResolvedValue({
    data: { version: '0.3.0-test', commit: 'abc123', date: '2026-01-02' }
  })
  apiMock.listInterfaces.mockResolvedValue({ data: hostInterfaces })
  apiMock.scan.mockResolvedValue({
    data: { devices: overview.devices, added: [], updated: [] }
  })
  apiMock.cancelScan.mockResolvedValue({ data: { cancelled: true } })
  apiMock.addManualDevice.mockResolvedValue({ data: garageCamera })
  apiMock.authenticate.mockResolvedValue({
    data: {
      device: { ...frontCamera, has_credentials: true, status: 'reachable' },
      inventory: makeInventory()
    }
  })
  apiMock.forgetDevice.mockResolvedValue({
    data: makeOverview([], undefined)
  })
  apiMock.selectDevice.mockResolvedValue({ data: overview })
  apiMock.loadOverview.mockResolvedValue({ data: overview })
  apiMock.loadProfiles.mockResolvedValue({ data: mediaProfiles })
  apiMock.updatePreferences.mockImplementation(
    async (input: PreferenceInput): Promise<ApiResponse<Overview>> => ({
      data: {
        ...overview,
        locale: input.locale ?? overview.locale,
        theme: input.theme ?? overview.theme,
        selected_host_interface:
          input.selected_host_interface === undefined
            ? overview.selected_host_interface
            : input.selected_host_interface || undefined
      }
    })
  )
  apiMock.openInVLC.mockResolvedValue({ data: { completed: true } })
}

async function renderReady(): Promise<void> {
  render(App)
  await screen.findByRole('heading', { name: 'Front camera' })
}

beforeEach(() => {
  vi.resetAllMocks()
  localStorage.clear()
  document.documentElement.lang = 'en'
  document.documentElement.dataset.theme = 'system'
  setDefaultMocks()
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('App M3 integration', () => {
  it('shows bootstrap loading, then loads saved state, interfaces, and build info', async () => {
    const bootstrap = deferred<ApiResponse<BootstrapResult>>()
    apiMock.bootstrap.mockReturnValue(bootstrap.promise)

    render(App)

    expect(screen.getByRole('heading', { name: 'Opening ovm…' })).toBeTruthy()
    expect(
      screen.getByText('Loading saved devices and network interfaces.')
    ).toBeTruthy()

    bootstrap.resolve({ data: makeBootstrap() })

    await screen.findByRole('heading', { name: 'Front camera' })
    expect(apiMock.bootstrap).toHaveBeenCalledTimes(1)
    expect(apiMock.listInterfaces).toHaveBeenCalledWith(false)
    expect(screen.getByText('Version 0.3.0-test')).toBeTruthy()
  })

  it('renders a localized bootstrap error and retries successfully', async () => {
    apiMock.bootstrap
      .mockResolvedValueOnce({
        error: {
          category: 'internal',
          code: 'bridge_unavailable',
          message: 'bridge unavailable'
        }
      })
      .mockResolvedValue({ data: makeBootstrap() })

    render(App)

    await screen.findByRole('heading', { name: 'ovm could not start' })
    expect(
      screen.getByText(
        'The desktop service is unavailable. Run ovm inside the Wails application.'
      )
    ).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    await screen.findByRole('heading', { name: 'Front camera' })
    expect(apiMock.bootstrap).toHaveBeenCalledTimes(2)
  })

  it('scans, cancels an in-flight scan, and presents honest diagnostics', async () => {
    const scan = deferred<
      ApiResponse<{
        devices: Device[]
        added: string[]
        updated: string[]
        diagnostic: { category: string; detail: string }
      }>
    >()
    apiMock.scan.mockReturnValue(scan.promise)
    await renderReady()

    await fireEvent.click(screen.getByRole('button', { name: 'Scan network' }))
    expect(apiMock.scan).toHaveBeenCalledWith('en0')

    const cancel = await screen.findByRole('button', { name: 'Cancel scan' })
    await fireEvent.click(cancel)
    expect(apiMock.cancelScan).toHaveBeenCalledTimes(1)
    expect(screen.getByText('Scan cancellation requested.')).toBeTruthy()

    scan.resolve({
      data: {
        devices: [frontCamera],
        added: [],
        updated: ['camera-front'],
        diagnostic: {
          category: 'permission_or_interface',
          detail: 'No multicast response was observed'
        }
      }
    })

    expect(
      await screen.findByText(
        'Discovery reported: No multicast response was observed'
      )
    ).toBeTruthy()
    expect(
      screen.getByText(/ovm cannot determine which condition applies/)
    ).toBeTruthy()
    expect(screen.getByText(/Manual add remains available/)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Add manually' })).toBeTruthy()
  })

  it('keeps manual add available without discovery and selects the added device', async () => {
    const empty = makeOverview([], undefined)
    apiMock.bootstrap.mockResolvedValue({ data: makeBootstrap(empty) })
    apiMock.listInterfaces.mockResolvedValue({ data: [] })
    apiMock.addManualDevice.mockResolvedValue({ data: garageCamera })
    apiMock.selectDevice.mockResolvedValue({
      data: makeOverview([garageCamera], garageCamera.id)
    })

    render(App)
    await screen.findByRole('heading', { name: 'Connect your first camera' })

    const scan = screen.getByRole('button', {
      name: 'Scan network'
    }) as HTMLButtonElement
    expect(scan.disabled).toBe(true)
    const manualButtons = screen.getAllByRole('button', {
      name: 'Add manually'
    })
    expect((manualButtons[0] as HTMLButtonElement).disabled).toBe(false)
    await fireEvent.click(manualButtons[0])

    const dialog = screen.getByRole('dialog', { name: 'Add a device manually' })
    await fireEvent.input(within(dialog).getByLabelText('Device endpoint'), {
      target: { value: '192.0.2.11' }
    })
    await fireEvent.input(
      within(dialog).getByLabelText('Display name (optional)'),
      {
        target: { value: 'Garage camera' }
      }
    )
    await fireEvent.click(
      within(dialog).getByRole('button', { name: 'Add device' })
    )

    await waitFor(() => {
      expect(apiMock.addManualDevice).toHaveBeenCalledWith(
        '192.0.2.11',
        'Garage camera'
      )
    })
    expect(apiMock.selectDevice).toHaveBeenCalledWith('camera-garage')
    await screen.findByRole('heading', { name: 'Garage camera' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  it('enforces authentication policy dependencies and clears the password on submit and close', async () => {
    const authentication =
      deferred<ApiResponse<{ device: Device; inventory: Inventory }>>()
    apiMock.authenticate.mockReturnValue(authentication.promise)
    await renderReady()

    await fireEvent.click(
      screen.getAllByRole('button', { name: 'Authenticate' })[0]
    )
    const dialog = screen.getByRole('dialog', {
      name: 'Authenticate to Front camera'
    })
    const username = within(dialog).getByLabelText('Username')
    const password = within(dialog).getByLabelText(
      'Password'
    ) as HTMLInputElement
    const basic = within(dialog).getByLabelText(
      'Allow HTTP Basic authentication'
    ) as HTMLInputElement
    const basicHTTP = within(dialog).getByLabelText(
      'Allow Basic over plaintext HTTP'
    ) as HTMLInputElement
    const legacy = within(dialog).getByLabelText(
      'Allow legacy Digest without qop'
    ) as HTMLInputElement

    expect(basicHTTP.disabled).toBe(true)
    expect(
      within(dialog).getByText(/High risk: network observers/)
    ).toBeTruthy()
    await fireEvent.click(basic)
    expect(basicHTTP.disabled).toBe(false)
    await fireEvent.click(basicHTTP)
    await fireEvent.click(legacy)
    await fireEvent.input(username, { target: { value: 'operator' } })
    await fireEvent.input(password, { target: { value: 'do-not-retain' } })
    await fireEvent.click(
      within(dialog).getByRole('button', { name: 'Authenticate' })
    )

    expect(password.value).toBe('')
    expect(apiMock.authenticate).toHaveBeenCalledWith({
      device_id: 'camera-front',
      username: 'operator',
      password: 'do-not-retain',
      allow_basic: true,
      allow_basic_over_http: true,
      allow_legacy_digest: true,
      timeout_milliseconds: 0
    })

    authentication.resolve({
      data: {
        device: {
          ...frontCamera,
          has_credentials: true,
          status: 'reachable'
        },
        inventory: makeInventory()
      }
    })
    await screen.findByText('OVM Synthetic')
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())

    await fireEvent.click(
      screen.getAllByRole('button', { name: 'Authenticate' })[0]
    )
    const reopenedPassword = screen.getByLabelText(
      'Password'
    ) as HTMLInputElement
    await fireEvent.input(reopenedPassword, {
      target: { value: 'clear-on-close' }
    })
    await fireEvent.click(screen.getAllByRole('button', { name: 'Close' })[0])
    await fireEvent.click(
      screen.getAllByRole('button', { name: 'Authenticate' })[0]
    )
    expect((screen.getByLabelText('Password') as HTMLInputElement).value).toBe(
      ''
    )
  })

  it('renders complete overview data, camera-vs-Mac interfaces, support, and clock uncertainty', async () => {
    await renderReady()
    await fireEvent.click(
      screen.getAllByRole('button', { name: 'Authenticate' })[0]
    )
    const dialog = screen.getByRole('dialog')
    await fireEvent.input(within(dialog).getByLabelText('Username'), {
      target: { value: 'operator' }
    })
    await fireEvent.input(within(dialog).getByLabelText('Password'), {
      target: { value: 'temporary' }
    })
    await fireEvent.click(
      within(dialog).getByRole('button', { name: 'Authenticate' })
    )

    expect(await screen.findByText('FakeCam 1000')).toBeTruthy()
    expect(screen.getByText('1.0.0-synthetic')).toBeTruthy()
    expect(screen.getByText('SYNTHETIC-0001')).toBeTruthy()
    expect(screen.getByText('OVM-FAKE-1')).toBeTruthy()
    expect(screen.getByText('synthetic-camera')).toBeTruthy()
    expect(screen.getByText('PTZ')).toBeTruthy()
    expect(
      screen.getByText('http://www.onvif.org/ver10/device/wsdl')
    ).toBeTruthy()
    expect(
      screen.getByText('onvif://www.onvif.org/name/FrontCamera')
    ).toBeTruthy()
    expect(screen.getByText('Mac interface')).toBeTruthy()
    expect(
      screen.getByRole('heading', { name: 'Camera interfaces' })
    ).toBeTruthy()
    expect(screen.getByText('00:00:5E:00:53:01')).toBeTruthy()
    expect(screen.getByText('203.0.113.10/24')).toBeTruthy()
    expect(screen.getByText('Partial camera response')).toBeTruthy()
    expect(
      screen.getByText(/camera clock differs by more than one second/i)
    ).toBeTruthy()
    expect(screen.getByText(/±/)).toBeTruthy()
  })

  it('searches, selects, and forgets devices through mocked bindings', async () => {
    const overview = makeOverview([frontCamera, garageCamera], frontCamera.id)
    apiMock.bootstrap.mockResolvedValue({ data: makeBootstrap(overview) })
    apiMock.selectDevice.mockResolvedValue({
      data: makeOverview([frontCamera, garageCamera], garageCamera.id)
    })
    apiMock.forgetDevice.mockResolvedValue({
      data: makeOverview([frontCamera], '')
    })
    await renderReady()

    const sidebar = screen.getByRole('complementary', {
      name: 'Devices and discovery'
    })
    const search = within(sidebar).getByLabelText('Search devices')
    await fireEvent.input(search, { target: { value: 'garage' } })
    expect(within(sidebar).queryByText('Front camera')).toBeNull()
    expect(within(sidebar).getByText('Garage camera')).toBeTruthy()

    await fireEvent.click(
      within(sidebar).getByRole('button', { name: /Garage camera/ })
    )
    expect(apiMock.selectDevice).toHaveBeenCalledWith('camera-garage')
    await screen.findByRole('heading', { name: 'Garage camera' })

    await fireEvent.click(
      within(sidebar).getByRole('button', { name: 'Forget' })
    )
    const confirmation = screen.getByRole('alertdialog', {
      name: 'Forget Garage camera?'
    })
    await fireEvent.click(
      within(confirmation).getByRole('button', { name: 'Forget device' })
    )
    expect(apiMock.forgetDevice).toHaveBeenCalledWith('camera-garage')
    await screen.findByRole('heading', { name: 'Connect your first camera' })
  })

  it('supports view and refresh shortcuts without hijacking ordinary copy', async () => {
    await renderReady()

    const streamsShortcut = new KeyboardEvent('keydown', {
      key: '2',
      ctrlKey: true,
      bubbles: true,
      cancelable: true
    })
    window.dispatchEvent(streamsShortcut)
    expect(streamsShortcut.defaultPrevented).toBe(true)
    expect(await screen.findByRole('heading', { name: 'Streams' })).toBeTruthy()
    expect(apiMock.loadProfiles).toHaveBeenCalledWith('camera-front')

    const overviewShortcut = new KeyboardEvent('keydown', {
      key: '1',
      metaKey: true,
      bubbles: true,
      cancelable: true
    })
    window.dispatchEvent(overviewShortcut)
    expect(overviewShortcut.defaultPrevented).toBe(true)
    expect(
      await screen.findByRole('heading', { name: 'Overview' })
    ).toBeTruthy()

    const refreshShortcut = new KeyboardEvent('keydown', {
      key: 'r',
      ctrlKey: true,
      bubbles: true,
      cancelable: true
    })
    window.dispatchEvent(refreshShortcut)
    expect(refreshShortcut.defaultPrevented).toBe(true)
    await waitFor(() => expect(apiMock.loadOverview).toHaveBeenCalledTimes(1))

    const callsBeforeCopy = apiMock.loadOverview.mock.calls.length
    const ordinaryCopy = new KeyboardEvent('keydown', {
      key: 'c',
      ctrlKey: true,
      bubbles: true,
      cancelable: true
    })
    window.dispatchEvent(ordinaryCopy)
    expect(ordinaryCopy.defaultPrevented).toBe(false)
    expect(apiMock.loadOverview).toHaveBeenCalledTimes(callsBeforeCopy)
  })

  it('shows every media profile, provenance, fallback, safe URLs, copy feedback, and VLC unavailable state', async () => {
    apiMock.copyText.mockResolvedValue(true)
    await renderReady()

    await fireEvent.click(screen.getByRole('tab', { name: /Streams/ }))

    expect(
      await screen.findByRole('heading', { name: 'Profile Main stream' })
    ).toBeTruthy()
    expect(
      screen.getByRole('heading', { name: 'Profile Sub stream' })
    ).toBeTruthy()
    expect(screen.getByText('Profile source: Media2')).toBeTruthy()
    expect(screen.getByText('Profile source: Media1')).toBeTruthy()
    expect(screen.getByText('1920 × 1080')).toBeTruthy()
    expect(screen.getByText('30 fps')).toBeTruthy()
    expect(screen.getByText('4096 kbps')).toBeTruthy()
    expect(screen.getByText('PCMU')).toBeTruthy()
    expect(screen.getAllByText(/Media1 fallback/).length).toBeGreaterThan(0)
    expect(
      screen.getAllByText(/intentionally omit credentials\./).length
    ).toBeGreaterThan(0)
    expect(screen.getByText('rtsp://192.0.2.10/main-tcp')).toBeTruthy()
    expect(
      screen.getAllByText(
        'VLC was not found at a supported local installation path.'
      ).length
    ).toBeGreaterThan(0)

    const copy = screen.getByRole('button', {
      name: 'Copy RTSP over TCP URL for Main stream'
    })
    await fireEvent.click(copy)
    expect(apiMock.copyText).toHaveBeenCalledWith('rtsp://192.0.2.10/main-tcp')
    expect(await screen.findByText('Copied')).toBeTruthy()
  })

  it('reports a failed copy when no clipboard accepts the URL', async () => {
    apiMock.copyText.mockResolvedValue(false)
    await renderReady()
    await fireEvent.click(screen.getByRole('tab', { name: /Streams/ }))
    await screen.findByRole('heading', { name: 'Profile Main stream' })

    await fireEvent.click(
      screen.getByRole('button', {
        name: 'Copy RTSP over TCP URL for Main stream'
      })
    )
    expect(apiMock.copyText).toHaveBeenCalledWith('rtsp://192.0.2.10/main-tcp')
    expect(
      await screen.findByText(
        'Could not copy the URL. Clipboard access is unavailable.'
      )
    ).toBeTruthy()
    expect(screen.queryByText('Copied')).toBeNull()
  })

  it('opens VLC only by device, profile, and transport identifiers', async () => {
    apiMock.bootstrap.mockResolvedValue({
      data: makeBootstrap(makeOverview(), {
        demo: false,
        rtsp_playback_available: true,
        vlc_available: true,
        live_preview_available: false
      })
    })
    await renderReady()
    await fireEvent.click(screen.getByRole('tab', { name: /Streams/ }))
    await screen.findByRole('heading', { name: 'Profile Main stream' })

    const openButtons = screen.getAllByRole('button', { name: 'Open in VLC' })
    await fireEvent.click(openButtons[0])
    expect(apiMock.openInVLC).toHaveBeenCalledWith(
      'camera-front',
      'profile_main',
      'tcp'
    )
    expect(await screen.findByText('VLC launch requested.')).toBeTruthy()
  })

  it('applies and persists locale and system/light/dark theme preferences', async () => {
    let persisted = makeOverview([frontCamera], frontCamera.id, 'pt-BR', 'dark')
    apiMock.bootstrap.mockResolvedValue({ data: makeBootstrap(persisted) })
    apiMock.updatePreferences.mockImplementation(
      async (input: PreferenceInput): Promise<ApiResponse<Overview>> => {
        persisted = {
          ...persisted,
          locale: input.locale ?? persisted.locale,
          theme: input.theme ?? persisted.theme,
          selected_host_interface:
            input.selected_host_interface === undefined
              ? persisted.selected_host_interface
              : input.selected_host_interface || undefined
        }
        return { data: persisted }
      }
    )

    render(App)
    await screen.findByRole('heading', { name: 'Front camera' })
    expect(document.documentElement.lang).toBe('pt-BR')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(screen.getByRole('button', { name: 'Buscar na rede' })).toBeTruthy()

    await fireEvent.change(screen.getByLabelText('Tema'), {
      target: { value: 'light' }
    })
    await waitFor(() =>
      expect(document.documentElement.dataset.theme).toBe('light')
    )
    expect(localStorage.getItem('ovm.theme')).toBe('light')
    expect(apiMock.updatePreferences).toHaveBeenCalledWith({ theme: 'light' })

    await fireEvent.change(screen.getByLabelText('Idioma'), {
      target: { value: 'en' }
    })
    await waitFor(() => expect(document.documentElement.lang).toBe('en'))
    expect(apiMock.updatePreferences).toHaveBeenCalledWith({ locale: 'en' })
    expect(screen.getByRole('button', { name: 'Scan network' })).toBeTruthy()
  })

  it('labels demo data and offers synthetic RTSP URLs for copy but never as playable', async () => {
    apiMock.bootstrap.mockResolvedValue({
      data: makeBootstrap(makeOverview(), {
        demo: true,
        rtsp_playback_available: false,
        vlc_available: false,
        live_preview_available: false
      })
    })
    await renderReady()
    expect(apiMock.listInterfaces).toHaveBeenCalledWith(true)
    expect(screen.getAllByText('Demo').length).toBeGreaterThan(0)
    expect(
      screen.getByText(
        'Synthetic demo metadata only — no physical camera or playable RTSP stream is claimed.'
      )
    ).toBeTruthy()

    await fireEvent.click(screen.getByRole('tab', { name: /Streams/ }))
    await screen.findByRole('heading', { name: 'Profile Main stream' })
    expect(screen.getByText('rtsp://192.0.2.10/main-tcp')).toBeTruthy()
    expect(
      screen.getByRole('button', {
        name: 'Copy RTSP over TCP URL for Main stream'
      })
    ).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Open in VLC' })).toBeNull()
    expect(
      screen.getAllByText(
        'VLC is disabled in demo mode because no real RTSP stream exists. Live preview shows a synthetic test pattern.'
      ).length
    ).toBeGreaterThan(0)
  })

  it('starts and stops a live preview by identifiers and shows measured statistics', async () => {
    apiMock.bootstrap.mockResolvedValue({
      data: makeBootstrap(makeOverview(), {
        demo: true,
        rtsp_playback_available: false,
        vlc_available: false,
        live_preview_available: true
      })
    })
    apiMock.startPreview.mockResolvedValue({ data: liveSession })
    apiMock.stopPreview.mockResolvedValue({ data: { completed: true } })
    apiMock.previewStats.mockResolvedValue({
      data: {
        width: 640,
        height: 352,
        frames_per_second: 25,
        bitrate_kbps: 1834.4,
        gop_frames: 50,
        gop_seconds: 2,
        time_to_first_frame_ms: 118,
        frames: 120,
        keyframes: 3,
        lost_packets: 0,
        reconnects: 1,
        elapsed_ms: 4800,
        first_frame_received: true
      }
    })
    // A Media Source that never opens keeps the live session connecting.
    stubMediaSource()
    await renderReady()
    await fireEvent.click(screen.getByRole('tab', { name: /Streams/ }))
    await screen.findByRole('heading', { name: 'Profile Main stream' })

    await fireEvent.click(
      screen.getAllByRole('button', { name: 'Start live preview' })[0]
    )
    expect(apiMock.startPreview).toHaveBeenCalledWith(
      'camera-front',
      'profile_main'
    )
    expect(await screen.findByText('Connecting to the preview…')).toBeTruthy()

    const stats = await screen.findByLabelText(
      'Measured stream statistics',
      {},
      { timeout: 3000 }
    )
    expect(stats.textContent).toContain('640 × 352')
    expect(stats.textContent).toContain('25.0 fps')
    expect(stats.textContent).toContain('1,834 kbps')
    expect(stats.textContent).toContain('50 frames (2.00 s)')
    expect(stats.textContent).toContain('118 ms')
    expect(stats.textContent).toContain('Reconnects 1')
    expect(apiMock.previewStats).toHaveBeenCalledWith(liveSession.session_id)

    apiMock.startRecording.mockResolvedValue({
      data: { path: '', bytes: 0, interrupted: false }
    })
    apiMock.stopRecording.mockResolvedValue({
      data: {
        path: '/Users/tester/Movies/ovm/ovm-profile_main.mp4',
        bytes: 2_500_000,
        interrupted: false
      }
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Record clip' }))
    expect(apiMock.startRecording).toHaveBeenCalledWith(liveSession.session_id)
    expect(await screen.findByText('Recording…')).toBeTruthy()
    await fireEvent.click(
      screen.getByRole('button', { name: 'Stop recording' })
    )
    expect(
      await screen.findByText(
        'Clip saved to /Users/tester/Movies/ovm/ovm-profile_main.mp4 (2.5 MB).'
      )
    ).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: 'Stop preview' }))
    expect(apiMock.stopPreview).toHaveBeenCalledWith(liveSession.session_id)
  })

  it('falls back to authenticated snapshots when the WebView cannot play video', async () => {
    apiMock.bootstrap.mockResolvedValue({
      data: makeBootstrap(makeOverview(), {
        demo: true,
        rtsp_playback_available: false,
        vlc_available: false,
        live_preview_available: true
      })
    })
    apiMock.startPreview.mockResolvedValue({ data: liveSession })
    apiMock.startSnapshotPreview.mockResolvedValue({ data: snapshotSession })
    apiMock.stopPreview.mockResolvedValue({ data: { completed: true } })
    await renderReady()
    await fireEvent.click(screen.getByRole('tab', { name: /Streams/ }))
    await screen.findByRole('heading', { name: 'Profile Main stream' })

    await fireEvent.click(
      screen.getAllByRole('button', { name: 'Start live preview' })[0]
    )
    // jsdom has no Media Source Extensions, so the preview must fall back.
    expect(
      await screen.findByText(
        'This WebView cannot play live video, so ovm shows camera snapshots at about 1.0 fps instead.'
      )
    ).toBeTruthy()
    expect(apiMock.stopPreview).toHaveBeenCalledWith(liveSession.session_id)
    expect(apiMock.startSnapshotPreview).toHaveBeenCalledWith(
      'camera-front',
      'profile_main'
    )
    expect(screen.getByAltText('Snapshot preview of Main stream')).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: 'Stop preview' }))
    expect(apiMock.stopPreview).toHaveBeenCalledWith(snapshotSession.session_id)
  })

  it('builds a four-position live grid from authenticated cameras and expands a tile', async () => {
    const credentialed = [
      { ...frontCamera, has_credentials: true },
      { ...garageCamera, has_credentials: true }
    ]
    apiMock.bootstrap.mockResolvedValue({
      data: makeBootstrap(makeOverview(credentialed), {
        demo: false,
        rtsp_playback_available: false,
        vlc_available: false,
        live_preview_available: true
      })
    })
    render(App)
    await screen.findByRole('heading', { name: 'Front camera' })

    await fireEvent.click(screen.getByRole('tab', { name: /Live grid/ }))
    expect(
      await screen.findByRole('heading', { name: 'Camera grid' })
    ).toBeTruthy()
    expect(apiMock.loadProfiles).toHaveBeenCalledWith('camera-front')
    expect(apiMock.loadProfiles).toHaveBeenCalledWith('camera-garage')

    const chooser = screen.getByLabelText('Stream for position 1')
    const options = Array.from(
      (chooser as HTMLSelectElement).options,
      (option) => option.textContent
    )
    expect(options).toContain('Front camera · Main stream')
    expect(
      screen.getAllByRole('combobox', { name: /Stream for position/ })
    ).toHaveLength(4)

    const mainOption = Array.from((chooser as HTMLSelectElement).options).find(
      (option) => option.textContent === 'Front camera · Main stream'
    )
    await fireEvent.change(chooser, { target: { value: mainOption?.value } })
    expect(
      await screen.findByRole('button', { name: 'Start live preview' })
    ).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: 'Full screen' }))
    expect(apiMock.setFullscreen).toHaveBeenCalledWith(true)
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(apiMock.setFullscreen).toHaveBeenCalledWith(false)
  })

  it('reports missing MSE when the snapshot fallback is unavailable too', async () => {
    apiMock.bootstrap.mockResolvedValue({
      data: makeBootstrap(makeOverview(), {
        demo: true,
        rtsp_playback_available: false,
        vlc_available: false,
        live_preview_available: true
      })
    })
    apiMock.startPreview.mockResolvedValue({ data: liveSession })
    apiMock.startSnapshotPreview.mockResolvedValue({
      error: {
        category: 'protocol',
        code: 'operation_protocol',
        message: 'no snapshot'
      }
    })
    apiMock.stopPreview.mockResolvedValue({ data: { completed: true } })
    await renderReady()
    await fireEvent.click(screen.getByRole('tab', { name: /Streams/ }))
    await screen.findByRole('heading', { name: 'Profile Main stream' })
    await fireEvent.click(
      screen.getAllByRole('button', { name: 'Start live preview' })[0]
    )
    expect(
      await screen.findByText(
        'This WebView does not support Media Source Extensions, so live video cannot play here.'
      )
    ).toBeTruthy()
  })

  it('explains why a live preview could not start', async () => {
    apiMock.bootstrap.mockResolvedValue({
      data: makeBootstrap(makeOverview(), {
        demo: false,
        rtsp_playback_available: false,
        vlc_available: false,
        live_preview_available: true
      })
    })
    apiMock.startPreview.mockResolvedValue({
      error: {
        category: 'authentication',
        code: 'operation_authentication',
        message: 'the camera rejected the stream credentials'
      }
    })
    await renderReady()
    await fireEvent.click(screen.getByRole('tab', { name: /Streams/ }))
    await screen.findByRole('heading', { name: 'Profile Main stream' })
    await fireEvent.click(
      screen.getAllByRole('button', { name: 'Start live preview' })[0]
    )
    const status = await screen.findByText(/The live preview could not start\./)
    expect(status.textContent).toContain(
      'Authentication failed. Check the credentials and compatibility policies.'
    )
  })

  it('exposes labeled controls, live status, modal semantics, and keyboard focus', async () => {
    const empty = makeOverview([], undefined)
    apiMock.bootstrap.mockResolvedValue({ data: makeBootstrap(empty) })
    apiMock.listInterfaces.mockResolvedValue({ data: [] })
    render(App)
    await screen.findByRole('heading', { name: 'Connect your first camera' })

    expect(
      screen.getByRole('complementary', { name: 'Devices and discovery' })
    ).toBeTruthy()
    expect(screen.getByLabelText('Mac network interface')).toBeTruthy()
    expect(screen.getByLabelText('Language')).toBeTruthy()
    expect(screen.getByLabelText('Theme')).toBeTruthy()
    expect(
      document.querySelector('.toast-region')?.getAttribute('aria-live')
    ).toBe('polite')

    for (const button of screen.getAllByRole('button')) {
      const accessibleName =
        button.getAttribute('aria-label') || button.textContent?.trim()
      expect(accessibleName).toBeTruthy()
    }

    await fireEvent.click(
      screen.getAllByRole('button', { name: 'Add manually' })[0]
    )
    const dialog = screen.getByRole('dialog', { name: 'Add a device manually' })
    expect(dialog.getAttribute('aria-modal')).toBe('true')
    const endpoint = within(dialog).getByLabelText('Device endpoint')
    await waitFor(() => expect(document.activeElement).toBe(endpoint))
    expect(within(dialog).getByRole('button', { name: 'Close' })).toBeTruthy()
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toBeTruthy()
  })
})
