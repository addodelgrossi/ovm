<script lang="ts">
  import { onMount } from 'svelte'
  import AuthDialog from './components/AuthDialog.svelte'
  import ConfirmForgetDialog from './components/ConfirmForgetDialog.svelte'
  import ManualDeviceDialog from './components/ManualDeviceDialog.svelte'
  import OverviewView from './components/OverviewView.svelte'
  import Sidebar from './components/Sidebar.svelte'
  import StreamsView from './components/StreamsView.svelte'
  import LiveGridView from './components/LiveGridView.svelte'
  import SettingsView from './components/SettingsView.svelte'
  import DiagnosticsView from './components/DiagnosticsView.svelte'
  import CommandPalette from './components/CommandPalette.svelte'
  import QuickSetupDialog from './components/QuickSetupDialog.svelte'
  import type { Command } from './lib/commands'
  import ToastRegion from './components/ToastRegion.svelte'
  import type { AuthenticateInput, Locale, Tab, Theme } from './lib/api'
  import { errorMessage, t, type TranslationKey } from './lib/i18n'
  import { createAppState } from './lib/state.svelte'

  const model = createAppState()
  let manualDialogOpen = $state(false)
  let authDialogOpen = $state(false)
  let forgetDialogOpen = $state(false)
  let paletteOpen = $state(false)
  let quickSetupOpen = $state(false)

  let paletteCommands = $derived(buildCommands())

  function buildCommands(): Command[] {
    const locale = model.locale
    const commands: Command[] = []
    const device = model.selectedDevice
    if (device) {
      const tabs: [Tab, TranslationKey][] = [
        ['overview', 'tabs.overviewShortcut'],
        ['streams', 'tabs.streamsShortcut'],
        ['settings', 'tabs.settingsShortcut'],
        ['diagnostics', 'tabs.diagnosticsShortcut']
      ]
      if (model.capabilities.live_preview_available)
        tabs.splice(2, 0, ['live', 'tabs.liveShortcut'])
      for (const [tab, key] of tabs) {
        commands.push({
          id: `tab-${tab}`,
          label: t(locale, 'palette.goTo', { tab: t(locale, key) }),
          run: () => void model.activateTab(tab)
        })
      }
      if (device.has_credentials) {
        commands.push({
          id: 'quick-setup',
          label: t(locale, 'quick.title'),
          hint: t(locale, 'palette.quickHint'),
          run: () => openQuickSetup()
        })
      }
      commands.push({
        id: 'authenticate',
        label: t(locale, 'action.authenticate'),
        run: () => (authDialogOpen = true)
      })
    }
    commands.push(
      {
        id: 'scan',
        label: t(locale, 'palette.scan'),
        run: () => void model.scan()
      },
      {
        id: 'manual',
        label: t(locale, 'sidebar.manualAdd'),
        run: () => (manualDialogOpen = true)
      },
      {
        id: 'refresh',
        label: t(locale, 'action.refresh'),
        hint: '⌘/Ctrl+R',
        run: () => void model.refresh()
      }
    )
    for (const candidate of model.overview.devices) {
      if (candidate.id === device?.id) continue
      commands.push({
        id: `select-${candidate.id}`,
        label: t(locale, 'palette.select', {
          name: candidate.display_name || candidate.endpoint
        }),
        run: () => void model.selectDevice(candidate.id)
      })
    }
    for (const theme of ['system', 'light', 'dark'] as Theme[]) {
      commands.push({
        id: `theme-${theme}`,
        label: t(locale, 'palette.theme', {
          theme: t(locale, `preferences.theme.${theme}` as TranslationKey)
        }),
        run: () => void model.updateTheme(theme)
      })
    }
    commands.push({
      id: 'language',
      label: locale === 'en' ? 'Português (Brasil)' : 'English',
      hint: t(locale, 'preferences.language'),
      run: () => void model.updateLocale(locale === 'en' ? 'pt-BR' : 'en')
    })
    return commands
  }

  function openQuickSetup(): void {
    if (!model.selectedDevice) return
    void model.loadProfiles(model.selectedDevice.id)
    quickSetupOpen = true
  }

  onMount(() => {
    void model.bootstrap()
    return () => model.dispose()
  })

  function isEditableTarget(target: EventTarget | null): boolean {
    if (!(target instanceof HTMLElement)) return false
    return (
      target.isContentEditable ||
      Boolean(
        target.closest('input, textarea, select, [contenteditable="true"]')
      )
    )
  }

  function keyboardShortcut(event: KeyboardEvent): void {
    if (
      (event.metaKey || event.ctrlKey) &&
      !event.altKey &&
      event.key.toLowerCase() === 'k' &&
      !manualDialogOpen &&
      !authDialogOpen &&
      !forgetDialogOpen &&
      !quickSetupOpen
    ) {
      event.preventDefault()
      paletteOpen = !paletteOpen
      return
    }
    if (
      paletteOpen ||
      quickSetupOpen ||
      !(event.metaKey || event.ctrlKey) ||
      event.altKey ||
      isEditableTarget(event.target) ||
      manualDialogOpen ||
      authDialogOpen ||
      forgetDialogOpen
    ) {
      return
    }

    const key = event.key.toLowerCase()
    if (key === 'r') {
      event.preventDefault()
      void model.refresh()
      return
    }
    if (key === '1' && model.selectedDevice) {
      event.preventDefault()
      void model.activateTab('overview')
      return
    }
    if (key === '2' && model.selectedDevice) {
      event.preventDefault()
      void model.activateTab('streams')
      return
    }
    if (
      key === '3' &&
      model.selectedDevice &&
      model.capabilities.live_preview_available
    ) {
      event.preventDefault()
      void model.activateTab('live')
      return
    }
    if (key === '4' && model.selectedDevice) {
      event.preventDefault()
      void model.activateTab('settings')
      return
    }
    if (key === '5' && model.selectedDevice) {
      event.preventDefault()
      void model.activateTab('diagnostics')
    }
  }

  function activateTab(tab: Tab): void {
    void model.activateTab(tab)
  }

  function updateLocale(event: Event): void {
    void model.updateLocale(
      (event.currentTarget as HTMLSelectElement).value as Locale
    )
  }

  function updateTheme(event: Event): void {
    void model.updateTheme(
      (event.currentTarget as HTMLSelectElement).value as Theme
    )
  }

  async function addManualDevice(
    endpoint: string,
    displayName: string
  ): Promise<boolean> {
    return model.addManualDevice(endpoint, displayName)
  }

  async function authenticate(input: AuthenticateInput): Promise<boolean> {
    return model.authenticate(input)
  }

  async function forgetSelected(): Promise<boolean> {
    const selected = model.selectedDevice
    return selected ? model.forgetDevice(selected.id) : false
  }
</script>

<svelte:head>
  <title>{t(model.locale, 'app.documentTitle')}</title>
</svelte:head>

<svelte:window onkeydown={keyboardShortcut} />

{#if model.phase === 'loading'}
  <main class="startup-state" aria-busy="true">
    <div class="startup-mark" aria-hidden="true">◎</div>
    <h1>{t(model.locale, 'app.loading')}</h1>
    <p>{t(model.locale, 'app.loadingDetail')}</p>
    <span class="spinner spinner-large" aria-hidden="true"></span>
  </main>
{:else if model.phase === 'error'}
  <main class="startup-state">
    <div class="startup-mark startup-mark-error" aria-hidden="true">!</div>
    <h1>{t(model.locale, 'app.errorTitle')}</h1>
    <p>{t(model.locale, 'app.errorDetail')}</p>
    {#if model.globalError}
      <p class="error-detail" role="alert">
        {errorMessage(model.locale, model.globalError)}
      </p>
    {/if}
    <button
      type="button"
      class="button button-primary"
      onclick={() => void model.bootstrap()}
    >
      {t(model.locale, 'action.retry')}
    </button>
  </main>
{:else}
  <div class="app-shell">
    <Sidebar
      {model}
      onManualAdd={() => (manualDialogOpen = true)}
      onAuthenticate={() => (authDialogOpen = true)}
      onForget={() => (forgetDialogOpen = true)}
    />

    <main class="content" aria-busy={model.refreshing}>
      <div class="content-inner">
        <header class="content-header">
          <div class="selected-heading">
            <p class="eyebrow">
              {model.selectedDevice
                ? t(model.locale, 'header.endpoint')
                : t(model.locale, 'app.product')}
            </p>
            <h2>
              {model.selectedDevice?.display_name?.trim() ||
                model.selectedDevice?.endpoint ||
                t(model.locale, 'app.product')}
            </h2>
            {#if model.selectedDevice?.display_name}
              <p class="endpoint-subtitle">{model.selectedDevice.endpoint}</p>
            {/if}
          </div>

          <div class="header-actions">
            {#if model.capabilities.demo}
              <span class="badge badge-demo"
                >{t(model.locale, 'demo.badge')}</span
              >
            {/if}
            <button
              type="button"
              class="icon-button header-refresh"
              aria-label={t(model.locale, 'header.refreshShortcut')}
              aria-keyshortcuts="Meta+R Control+R"
              disabled={model.refreshing}
              onclick={() => void model.refresh()}
            >
              <span aria-hidden="true">↻</span>
            </button>
            <div class="preference-control">
              <label for="app-language"
                >{t(model.locale, 'preferences.language')}</label
              >
              <select
                id="app-language"
                value={model.locale}
                onchange={updateLocale}
              >
                <option value="en">English</option>
                <option value="pt-BR">Português (Brasil)</option>
              </select>
            </div>
            <div class="preference-control">
              <label for="app-theme"
                >{t(model.locale, 'preferences.theme')}</label
              >
              <select id="app-theme" value={model.theme} onchange={updateTheme}>
                <option value="system"
                  >{t(model.locale, 'preferences.theme.system')}</option
                >
                <option value="light"
                  >{t(model.locale, 'preferences.theme.light')}</option
                >
                <option value="dark"
                  >{t(model.locale, 'preferences.theme.dark')}</option
                >
              </select>
            </div>
          </div>
        </header>

        {#if model.globalError}
          <div class="notice notice-warning" role="alert">
            {errorMessage(model.locale, model.globalError)}
          </div>
        {/if}

        {#if model.capabilities.demo}
          <div class="demo-disclosure" role="note">
            {t(model.locale, 'demo.claim')}
          </div>
        {/if}

        {#if model.selectedDevice}
          <nav class="tabs" aria-label={t(model.locale, 'tabs.label')}>
            <button
              type="button"
              role="tab"
              aria-selected={model.activeTab === 'overview'}
              aria-controls="overview-panel"
              aria-keyshortcuts="Meta+1 Control+1"
              class:active={model.activeTab === 'overview'}
              onclick={() => activateTab('overview')}
            >
              {t(model.locale, 'tabs.overviewShortcut')}
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={model.activeTab === 'streams'}
              aria-controls="streams-panel"
              aria-keyshortcuts="Meta+2 Control+2"
              class:active={model.activeTab === 'streams'}
              onclick={() => activateTab('streams')}
            >
              {t(model.locale, 'tabs.streamsShortcut')}
            </button>
            {#if model.capabilities.live_preview_available}
              <button
                type="button"
                role="tab"
                aria-selected={model.activeTab === 'live'}
                aria-controls="live-panel"
                aria-keyshortcuts="Meta+3 Control+3"
                class:active={model.activeTab === 'live'}
                onclick={() => activateTab('live')}
              >
                {t(model.locale, 'tabs.liveShortcut')}
              </button>
            {/if}
            <button
              type="button"
              role="tab"
              aria-selected={model.activeTab === 'settings'}
              aria-controls="settings-panel"
              aria-keyshortcuts="Meta+4 Control+4"
              class:active={model.activeTab === 'settings'}
              onclick={() => activateTab('settings')}
            >
              {t(model.locale, 'tabs.settingsShortcut')}
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={model.activeTab === 'diagnostics'}
              aria-controls="diagnostics-panel"
              aria-keyshortcuts="Meta+5 Control+5"
              class:active={model.activeTab === 'diagnostics'}
              onclick={() => activateTab('diagnostics')}
            >
              {t(model.locale, 'tabs.diagnosticsShortcut')}
            </button>
            {#if model.selectedDevice.has_credentials}
              <button
                type="button"
                class="button button-secondary tab-action"
                onclick={openQuickSetup}
              >
                {t(model.locale, 'quick.title')}
              </button>
            {/if}
          </nav>

          {#if model.activeTab === 'overview'}
            <div id="overview-panel" role="tabpanel">
              <OverviewView
                state={model}
                device={model.selectedDevice}
                onAuthenticate={() => (authDialogOpen = true)}
              />
            </div>
          {:else if model.activeTab === 'settings'}
            <div id="settings-panel" role="tabpanel">
              <SettingsView {model} device={model.selectedDevice} />
            </div>
          {:else if model.activeTab === 'diagnostics'}
            <div id="diagnostics-panel" role="tabpanel">
              <DiagnosticsView {model} device={model.selectedDevice} />
            </div>
          {:else if model.activeTab === 'live'}
            <div id="live-panel" role="tabpanel">
              <LiveGridView {model} />
            </div>
          {:else}
            <div id="streams-panel" role="tabpanel">
              <StreamsView {model} device={model.selectedDevice} />
            </div>
          {/if}
        {:else}
          <section class="welcome" aria-labelledby="welcome-title">
            <div class="welcome-visual" aria-hidden="true">
              <div class="camera-glyph">◉</div>
              <span></span><span></span><span></span>
            </div>
            <div>
              <p class="eyebrow">
                {t(model.locale, 'main.noSelectionEyebrow')}
              </p>
              <h2 id="welcome-title">
                {t(model.locale, 'main.noSelectionTitle')}
              </h2>
              <p class="welcome-lead">
                {t(model.locale, 'main.noSelectionBody')}
              </p>
              <ol class="steps">
                <li><span>1</span>{t(model.locale, 'main.stepOne')}</li>
                <li><span>2</span>{t(model.locale, 'main.stepTwo')}</li>
                <li><span>3</span>{t(model.locale, 'main.stepThree')}</li>
              </ol>
              <button
                type="button"
                class="button button-primary"
                onclick={() => (manualDialogOpen = true)}
              >
                {t(model.locale, 'sidebar.manualAdd')}
              </button>
            </div>
          </section>

          <section
            class="principle-grid"
            aria-label={t(model.locale, 'app.product')}
          >
            <article>
              <span aria-hidden="true">⌁</span>
              <h3>{t(model.locale, 'main.localTitle')}</h3>
              <p>{t(model.locale, 'main.localBody')}</p>
            </article>
            <article>
              <span aria-hidden="true">◇</span>
              <h3>{t(model.locale, 'main.secretTitle')}</h3>
              <p>{t(model.locale, 'main.secretBody')}</p>
            </article>
            <article>
              <span aria-hidden="true">✓</span>
              <h3>{t(model.locale, 'main.safeTitle')}</h3>
              <p>{t(model.locale, 'main.safeBody')}</p>
            </article>
          </section>
        {/if}
      </div>
    </main>
  </div>

  <ToastRegion state={model} />

  {#if manualDialogOpen}
    <ManualDeviceDialog
      locale={model.locale}
      busy={model.addingManual}
      onSubmit={addManualDevice}
      onClose={() => (manualDialogOpen = false)}
    />
  {/if}

  {#if authDialogOpen && model.selectedDevice}
    <AuthDialog
      device={model.selectedDevice}
      locale={model.locale}
      busy={model.authenticatingDeviceId === model.selectedDevice.id}
      onSubmit={authenticate}
      onClose={() => (authDialogOpen = false)}
    />
  {/if}

  {#if paletteOpen}
    <CommandPalette
      locale={model.locale}
      commands={paletteCommands}
      onClose={() => (paletteOpen = false)}
    />
  {/if}

  {#if quickSetupOpen && model.selectedDevice}
    <QuickSetupDialog
      locale={model.locale}
      device={model.selectedDevice}
      profiles={model.profilesByDevice[model.selectedDevice.id]?.data
        ?.profiles ?? []}
      onClose={() => (quickSetupOpen = false)}
      onChanged={() =>
        model.selectedDevice &&
        void model.loadProfiles(model.selectedDevice.id, true)}
    />
  {/if}

  {#if forgetDialogOpen && model.selectedDevice}
    <ConfirmForgetDialog
      device={model.selectedDevice}
      locale={model.locale}
      busy={model.forgettingDeviceId === model.selectedDevice.id}
      onConfirm={forgetSelected}
      onClose={() => (forgetDialogOpen = false)}
    />
  {/if}
{/if}
