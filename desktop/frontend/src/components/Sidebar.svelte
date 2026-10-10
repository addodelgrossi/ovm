<script lang="ts">
  import {
    deviceStatusLabel,
    errorMessage,
    interfaceReasonLabel,
    t
  } from '../lib/i18n'
  import type { AppState } from '../lib/state.svelte'

  let {
    model,
    onManualAdd,
    onAuthenticate,
    onForget
  }: {
    model: AppState
    onManualAdd: () => void
    onAuthenticate: () => void
    onForget: () => void
  } = $props()

  let search = $state('')
  let filteredDevices = $derived.by(() => {
    const query = search.trim().toLocaleLowerCase(model.locale)
    if (!query) return model.overview.devices
    return model.overview.devices.filter((device) => {
      const status = deviceStatusLabel(model.locale, device.status)
      return [device.display_name, device.endpoint, status]
        .filter(Boolean)
        .some((value) => value?.toLocaleLowerCase(model.locale).includes(query))
    })
  })
  let showDiagnostic = $derived(
    !model.canScan ||
      Boolean(model.interfaceError) ||
      Boolean(model.scanError) ||
      Boolean(model.scanDiagnostic)
  )

  function deviceName(device: {
    display_name?: string
    endpoint: string
  }): string {
    return device.display_name?.trim() || device.endpoint
  }

  function selectInterface(event: Event): void {
    const target = event.currentTarget as HTMLSelectElement
    void model.updateSelectedInterface(target.value)
  }
</script>

<aside class="sidebar" aria-label={t(model.locale, 'sidebar.label')}>
  <div class="titlebar-drag-region" aria-hidden="true"></div>

  <div class="brand-row">
    <div class="brand-mark" aria-hidden="true">
      <svg viewBox="0 0 32 32">
        <rect x="5" y="9" width="19" height="14" rx="4"></rect>
        <circle cx="14.5" cy="16" r="4"></circle>
        <path d="m24 13 4-2v10l-4-2z"></path>
        <path d="M9 6h11"></path>
      </svg>
    </div>
    <div>
      <p class="eyebrow">{t(model.locale, 'app.product')}</p>
      <h1>{t(model.locale, 'app.name')}</h1>
    </div>
    {#if model.capabilities.demo}
      <span class="badge badge-demo">{t(model.locale, 'demo.badge')}</span>
    {/if}
  </div>

  <div class="discovery-controls">
    <label for="host-interface">{t(model.locale, 'sidebar.interface')}</label>
    <select
      id="host-interface"
      value={model.overview.selected_host_interface ?? ''}
      onchange={selectInterface}
    >
      <option value="">{t(model.locale, 'sidebar.interfaceAutomatic')}</option>
      {#each model.interfaces as networkInterface (networkInterface.index)}
        <option
          value={networkInterface.name}
          disabled={!networkInterface.usable}
        >
          {networkInterface.usable
            ? t(model.locale, 'sidebar.interfaceAddress', {
                name: networkInterface.name,
                addresses: networkInterface.ipv4?.join(', ') || 'IPv4'
              })
            : t(model.locale, 'sidebar.interfaceReason', {
                name: networkInterface.name,
                reason: interfaceReasonLabel(
                  model.locale,
                  networkInterface.reason
                )
              })}
        </option>
      {/each}
    </select>

    <div class="button-row">
      {#if model.scanning}
        <button
          type="button"
          class="button button-danger button-grow"
          onclick={() => void model.cancelScan()}
          disabled={model.cancellingScan}
        >
          <span aria-hidden="true">■</span>
          {model.cancellingScan
            ? t(model.locale, 'sidebar.cancelling')
            : t(model.locale, 'sidebar.cancelScan')}
        </button>
      {:else}
        <button
          type="button"
          class="button button-primary button-grow"
          onclick={() => void model.scan()}
          disabled={!model.canScan}
        >
          <span aria-hidden="true">⌁</span>
          {t(model.locale, 'sidebar.scan')}
        </button>
      {/if}
      <button
        type="button"
        class="button button-secondary"
        onclick={onManualAdd}
      >
        <span aria-hidden="true">＋</span>
        {t(model.locale, 'sidebar.manualAdd')}
      </button>
    </div>
  </div>

  {#if model.overview.devices.length > 0}
    <div class="search-field">
      <label for="device-search">{t(model.locale, 'sidebar.search')}</label>
      <input
        id="device-search"
        type="search"
        bind:value={search}
        placeholder={t(model.locale, 'sidebar.searchPlaceholder')}
        autocomplete="off"
      />
    </div>
  {/if}

  <div class="device-list-panel" aria-busy={model.scanning}>
    {#if model.scanning}
      <div class="inline-loading" role="status">
        <span class="spinner" aria-hidden="true"></span>
        {t(model.locale, 'sidebar.scanning')}
      </div>
    {/if}

    {#if model.overview.devices.length === 0}
      <div class="empty-compact">
        <span class="empty-icon" aria-hidden="true">◎</span>
        <p>{t(model.locale, 'sidebar.noDevices')}</p>
        <small>{t(model.locale, 'sidebar.noDevicesHint')}</small>
      </div>
    {:else if filteredDevices.length === 0}
      <p class="empty-search" role="status">
        {t(model.locale, 'sidebar.noResults', { query: search })}
      </p>
    {:else}
      <ul class="device-list">
        {#each filteredDevices as device (device.id)}
          <li class:selected={device.id === model.overview.selected_device_id}>
            <button
              type="button"
              class="device-select"
              aria-current={device.id === model.overview.selected_device_id
                ? 'true'
                : undefined}
              onclick={() => void model.selectDevice(device.id)}
            >
              <span class="device-icon" aria-hidden="true">◉</span>
              <span class="device-copy">
                <strong>{deviceName(device)}</strong>
                <small>{device.endpoint}</small>
              </span>
              <span class="status-label" data-status={device.status}>
                <span class="status-dot" aria-hidden="true"></span>
                {deviceStatusLabel(model.locale, device.status)}
              </span>
            </button>
            {#if device.id === model.overview.selected_device_id}
              <div
                class="device-actions"
                aria-label={t(model.locale, 'sidebar.deviceActions', {
                  name: deviceName(device)
                })}
              >
                <button
                  type="button"
                  class="text-button"
                  onclick={onAuthenticate}
                >
                  {t(model.locale, 'action.authenticate')}
                </button>
                <button
                  type="button"
                  class="text-button danger-text"
                  onclick={onForget}
                >
                  {t(model.locale, 'action.forget')}
                </button>
              </div>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </div>

  {#if showDiagnostic}
    <section class="diagnostic-card" aria-labelledby="diagnostic-title">
      <h2 id="diagnostic-title">{t(model.locale, 'diagnostic.title')}</h2>
      {#if !model.canScan}
        <p><strong>{t(model.locale, 'diagnostic.noInterface')}</strong></p>
      {/if}
      {#if model.interfaceError}
        <p>{errorMessage(model.locale, model.interfaceError)}</p>
      {/if}
      {#if model.scanError}
        <p>{errorMessage(model.locale, model.scanError)}</p>
      {/if}
      {#if model.scanDiagnostic}
        <p>
          {t(model.locale, 'diagnostic.backend', {
            detail: model.scanDiagnostic.detail || model.scanDiagnostic.category
          })}
        </p>
      {/if}
      <p>{t(model.locale, 'diagnostic.honest')}</p>
      <p>{t(model.locale, 'diagnostic.manual')}</p>
    </section>
  {/if}

  <footer>
    <span>
      {t(model.locale, 'app.version', { version: model.buildInfo.version })}
    </span>
    <span class="ready-indicator" aria-hidden="true"></span>
  </footer>
</aside>
