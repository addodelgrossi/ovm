<script lang="ts">
  import type { Device, DeviceCapabilities, Locale } from '../lib/api'
  import {
    deviceStatusLabel,
    operationLabel,
    supportLabel,
    t,
    type TranslationKey
  } from '../lib/i18n'
  import type { AppState } from '../lib/state.svelte'

  let {
    state,
    device,
    onAuthenticate
  }: {
    state: AppState
    device: Device
    onAuthenticate: () => void
  } = $props()

  let snapshot = $derived(state.inventoryByDevice[device.id])
  let inventory = $derived(snapshot?.inventory)
  let information = $derived(inventory?.device_information)
  let hostInterface = $derived(state.selectedHostInterface)

  const capabilityFields: Array<{
    field: keyof DeviceCapabilities
    key: TranslationKey
  }> = [
    { field: 'device_xaddr', key: 'capability.device' },
    { field: 'media_xaddr', key: 'capability.media' },
    { field: 'events_xaddr', key: 'capability.events' },
    { field: 'imaging_xaddr', key: 'capability.imaging' },
    { field: 'ptz_xaddr', key: 'capability.ptz' },
    { field: 'analytics_xaddr', key: 'capability.analytics' }
  ]

  let reportedCapabilities = $derived(
    capabilityFields.filter(({ field }) => inventory?.capabilities?.[field])
  )

  function valueOrDash(value?: string | number): string {
    return value === undefined || value === '' ? '—' : String(value)
  }

  function localizedDate(value: string, locale: Locale): string {
    const parsed = new Date(value)
    return Number.isNaN(parsed.getTime())
      ? value
      : new Intl.DateTimeFormat(locale, {
          dateStyle: 'medium',
          timeStyle: 'medium',
          timeZone: 'UTC'
        }).format(parsed)
  }

  function seconds(value: number, locale: Locale): string {
    return new Intl.NumberFormat(locale, {
      minimumFractionDigits: 1,
      maximumFractionDigits: 2,
      signDisplay: 'exceptZero'
    }).format(value / 1000)
  }
</script>

<section class="view-stack" aria-labelledby="overview-heading">
  <header class="view-heading">
    <div>
      <p class="eyebrow">{t(state.locale, 'overview.eyebrow')}</p>
      <h2 id="overview-heading">{t(state.locale, 'overview.title')}</h2>
    </div>
    <span class="status-chip" data-status={device.status}>
      <span class="status-dot" aria-hidden="true"></span>
      {deviceStatusLabel(state.locale, device.status)}
    </span>
  </header>

  <article class="panel identity-panel">
    <div class="panel-heading">
      <h3>{t(state.locale, 'overview.identity')}</h3>
      <button
        type="button"
        class="button button-secondary"
        onclick={onAuthenticate}
      >
        {t(state.locale, 'action.authenticate')}
      </button>
    </div>
    <dl class="definition-grid">
      <div>
        <dt>{t(state.locale, 'overview.manufacturer')}</dt>
        <dd>{valueOrDash(information?.manufacturer)}</dd>
      </div>
      <div>
        <dt>{t(state.locale, 'overview.model')}</dt>
        <dd>{valueOrDash(information?.model)}</dd>
      </div>
      <div>
        <dt>{t(state.locale, 'overview.firmware')}</dt>
        <dd>{valueOrDash(information?.firmware_version)}</dd>
      </div>
      <div>
        <dt>{t(state.locale, 'overview.serial')}</dt>
        <dd>{valueOrDash(information?.serial_number)}</dd>
      </div>
      <div>
        <dt>{t(state.locale, 'overview.hardware')}</dt>
        <dd>{valueOrDash(information?.hardware_id)}</dd>
      </div>
      <div>
        <dt>{t(state.locale, 'device.endpoint')}</dt>
        <dd class="monospace break-value">{device.endpoint}</dd>
      </div>
      <div>
        <dt>{t(state.locale, 'device.source')}</dt>
        <dd>
          {t(
            state.locale,
            device.discovery_source === 'manual'
              ? 'device.source.manual'
              : device.discovery_source === 'ws_discovery'
                ? 'device.source.ws_discovery'
                : 'device.source.unknown'
          )}
        </dd>
      </div>
      <div>
        <dt>{t(state.locale, 'device.credentials')}</dt>
        <dd>
          {device.has_credentials
            ? t(state.locale, 'device.credentialsStored')
            : t(state.locale, 'device.credentialsMissing')}
        </dd>
      </div>
    </dl>
  </article>

  {#if !inventory}
    <section
      class="empty-detail panel"
      aria-labelledby="details-unavailable-title"
    >
      <span class="empty-icon" aria-hidden="true">◇</span>
      <div>
        <h3 id="details-unavailable-title">
          {t(state.locale, 'overview.detailsUnavailable')}
        </h3>
        <p>
          {device.has_credentials
            ? t(state.locale, 'overview.detailsStored')
            : t(state.locale, 'overview.detailsMissing')}
        </p>
      </div>
      <button
        type="button"
        class="button button-primary"
        onclick={onAuthenticate}
      >
        {t(state.locale, 'action.authenticate')}
      </button>
    </section>
  {:else}
    <div class="two-column-grid">
      <article class="panel">
        <h3>{t(state.locale, 'overview.hostname')}</h3>
        <p class="large-value">{valueOrDash(inventory.hostname?.name)}</p>
        {#if inventory.hostname}
          <p class="muted">
            {inventory.hostname.from_dhcp
              ? t(state.locale, 'overview.hostnameDhcp')
              : t(state.locale, 'overview.hostnameStatic')}
          </p>
        {/if}
      </article>

      <article class="panel">
        <h3>{t(state.locale, 'overview.clock')}</h3>
        {#if inventory.system_date_and_time?.utc}
          <dl class="compact-definitions">
            <div>
              <dt>{t(state.locale, 'overview.cameraTime')}</dt>
              <dd>
                {localizedDate(
                  inventory.system_date_and_time.utc,
                  state.locale
                )}
              </dd>
            </div>
            {#if snapshot.clock}
              <div>
                <dt>{t(state.locale, 'overview.clockDifference')}</dt>
                <dd>
                  {t(state.locale, 'overview.clockValue', {
                    difference: seconds(
                      snapshot.clock.differenceMs,
                      state.locale
                    ),
                    uncertainty: seconds(
                      snapshot.clock.uncertaintyMs,
                      state.locale
                    )
                  })}
                </dd>
              </div>
            {/if}
          </dl>
          {#if snapshot.clock && Math.abs(snapshot.clock.differenceMs) > 1000}
            <p class="notice notice-warning" role="alert">
              {t(state.locale, 'overview.clockWarning')}
            </p>
          {:else if snapshot.clock}
            <p class="notice notice-success">
              {t(state.locale, 'overview.clockOkay')}
            </p>
          {/if}
        {:else}
          <p class="muted">{t(state.locale, 'overview.clockUnavailable')}</p>
        {/if}
      </article>
    </div>

    <article class="panel">
      <h3>{t(state.locale, 'overview.capabilities')}</h3>
      {#if reportedCapabilities.length > 0}
        <div class="chip-list">
          {#each reportedCapabilities as capability (capability.field)}
            <span
              class="chip chip-positive"
              title={inventory.capabilities?.[capability.field]}
            >
              {t(state.locale, capability.key)}
            </span>
          {/each}
        </div>
      {:else}
        <p class="muted">{t(state.locale, 'overview.noCapabilities')}</p>
      {/if}
    </article>

    <article class="panel">
      <h3>{t(state.locale, 'overview.services')}</h3>
      {#if inventory.services?.length}
        <div class="service-list">
          {#each inventory.services as service, index (`${service.namespace}-${index}`)}
            <div class="service-row">
              <span class="chip">{service.namespace || 'ONVIF'}</span>
              <span class="monospace break-value">{service.xaddr || '—'}</span>
              {#if service.has_version}
                <small>
                  v{service.version_major ?? 0}.{service.version_minor ?? 0}
                </small>
              {/if}
            </div>
          {/each}
        </div>
      {:else}
        <p class="muted">{t(state.locale, 'overview.noServices')}</p>
      {/if}
    </article>

    <article class="panel">
      <h3>{t(state.locale, 'overview.scopes')}</h3>
      {#if inventory.scopes?.length}
        <ul class="scope-list">
          {#each inventory.scopes as scope, index (`${scope.item}-${index}`)}
            <li>
              <span>{scope.definition || '—'}</span>
              <code>{scope.item || '—'}</code>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="muted">{t(state.locale, 'overview.noScopes')}</p>
      {/if}
    </article>

    <div class="network-comparison">
      <article class="panel host-interface-card">
        <p class="eyebrow">{t(state.locale, 'overview.hostInterface')}</p>
        <h3>
          {hostInterface?.name || t(state.locale, 'sidebar.interfaceAutomatic')}
        </h3>
        <p>{t(state.locale, 'overview.hostInterfaceHint')}</p>
        {#if hostInterface?.ipv4?.length}
          <div class="chip-list">
            {#each hostInterface.ipv4 as address (address)}
              <code class="chip">{address}</code>
            {/each}
          </div>
        {/if}
      </article>

      <article class="panel camera-interface-card">
        <p class="eyebrow">{t(state.locale, 'overview.cameraInterfaces')}</p>
        <h3>{t(state.locale, 'overview.cameraInterfaces')}</h3>
        <p>{t(state.locale, 'overview.cameraInterfacesHint')}</p>
        {#if inventory.network_interfaces?.length}
          <div class="camera-interface-list">
            {#each inventory.network_interfaces as cameraInterface, index (`${cameraInterface.token}-${index}`)}
              <section>
                <div class="panel-heading compact">
                  <strong
                    >{cameraInterface.name ||
                      cameraInterface.token ||
                      '—'}</strong
                  >
                  <span class="chip">
                    {cameraInterface.has_enabled && cameraInterface.enabled
                      ? t(state.locale, 'overview.enabled')
                      : t(state.locale, 'overview.disabled')}
                  </span>
                </div>
                <dl class="compact-definitions">
                  <div>
                    <dt>{t(state.locale, 'overview.macAddress')}</dt>
                    <dd>{valueOrDash(cameraInterface.hw_address)}</dd>
                  </div>
                  <div>
                    <dt>{t(state.locale, 'overview.mtu')}</dt>
                    <dd>
                      {cameraInterface.has_mtu
                        ? valueOrDash(cameraInterface.mtu)
                        : '—'}
                    </dd>
                  </div>
                  <div>
                    <dt>{t(state.locale, 'overview.addresses')}</dt>
                    <dd>
                      {cameraInterface.ipv4_addresses
                        ?.map((address) =>
                          address.address
                            ? `${address.address}${address.prefix_length === undefined ? '' : `/${address.prefix_length}`}`
                            : ''
                        )
                        .filter(Boolean)
                        .join(', ') || '—'}
                    </dd>
                  </div>
                  <div>
                    <dt>{t(state.locale, 'overview.dhcp')}</dt>
                    <dd>
                      {cameraInterface.has_dhcp && cameraInterface.dhcp
                        ? t(state.locale, 'overview.dhcp')
                        : t(state.locale, 'overview.manual')}
                    </dd>
                  </div>
                </dl>
              </section>
            {/each}
          </div>
        {:else}
          <p class="muted">{t(state.locale, 'overview.noCameraInterfaces')}</p>
        {/if}
      </article>
    </div>

    <article class="panel">
      <h3>{t(state.locale, 'overview.operations')}</h3>
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th scope="col">{t(state.locale, 'overview.operation')}</th>
              <th scope="col">{t(state.locale, 'overview.support')}</th>
              <th scope="col">{t(state.locale, 'overview.detail')}</th>
            </tr>
          </thead>
          <tbody>
            {#each inventory.operations as operation (operation.operation)}
              <tr>
                <th scope="row">
                  {operationLabel(state.locale, operation.operation)}
                </th>
                <td>
                  <span class="chip" data-support={operation.state}>
                    {supportLabel(state.locale, operation.state)}
                  </span>
                </td>
                <td>{operation.detail || '—'}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </article>
  {/if}
</section>
