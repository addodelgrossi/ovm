<script lang="ts">
  import {
    api,
    type Locale,
    type NetworkApplyResult,
    type NetworkView
  } from '../../lib/api'
  import { errorMessage, t } from '../../lib/i18n'
  import MutationResult from './MutationResult.svelte'

  let {
    locale,
    deviceId,
    onMoved
  }: { locale: Locale; deviceId: string; onMoved: () => void } = $props()

  let view = $state<NetworkView | undefined>()
  let dhcp = $state(false)
  let address = $state('')
  let prefix = $state(24)
  let gateway = $state('')
  let dns = $state('')
  let reviewing = $state(false)
  let confirmation = $state('')
  let busy = $state(false)
  let failure = $state('')
  let result = $state<NetworkApplyResult | undefined>()

  let expected = $derived(dhcp ? 'DHCP' : address.trim())

  $effect(() => {
    void load(deviceId)
  })

  async function load(id: string): Promise<void> {
    const response = await api.readNetwork(id)
    view = response.data
    if (!response.data) {
      failure = response.error ? errorMessage(locale, response.error) : ''
      return
    }
    dhcp = response.data.dhcp
    address = response.data.address
    prefix = response.data.prefix_length || 24
    gateway = (response.data.gateways ?? [])[0] ?? ''
    dns = response.data.dns_from_dhcp
      ? ''
      : (response.data.dns_servers ?? []).join(', ')
  }

  async function apply(): Promise<void> {
    busy = true
    failure = ''
    result = undefined
    const response = await api.applyNetwork(
      deviceId,
      {
        dhcp,
        address: address.trim(),
        prefix_length: Number(prefix),
        gateway: gateway.trim(),
        dns_servers: dns
          .split(',')
          .map((server) => server.trim())
          .filter(Boolean)
      },
      confirmation.trim()
    )
    busy = false
    reviewing = false
    confirmation = ''
    if (response.error) failure = errorMessage(locale, response.error)
    if (response.data?.report) {
      result = response.data
      if (
        response.data.new_endpoint &&
        response.data.report.outcome !== 'disconnected'
      )
        onMoved()
    }
  }
</script>

<section class="settings-section" aria-labelledby="network-heading">
  <h3 id="network-heading">{t(locale, 'network.title')}</h3>
  {#if view}
    <p class="safe-url-notice">
      {t(locale, 'network.current', {
        address: view.address || '—',
        prefix: String(view.prefix_length),
        gateway: (view.gateways ?? []).join(', ') || '—',
        name: view.interface_name || view.interface_token,
        mac: view.hw_address || '—'
      })}
    </p>
    <label class="field checkbox-field">
      <input type="checkbox" bind:checked={dhcp} disabled={reviewing} />
      <span>{t(locale, 'network.dhcp')}</span>
    </label>
    <div class="settings-grid">
      {#if !dhcp}
        <label class="field">
          <span>{t(locale, 'network.address')}</span>
          <input type="text" bind:value={address} disabled={reviewing} />
        </label>
        <label class="field">
          <span>{t(locale, 'network.prefix')}</span>
          <input
            type="number"
            min="8"
            max="30"
            bind:value={prefix}
            disabled={reviewing}
          />
        </label>
        <label class="field">
          <span>{t(locale, 'network.gateway')}</span>
          <input type="text" bind:value={gateway} disabled={reviewing} />
        </label>
      {/if}
      <label class="field">
        <span>{t(locale, 'network.dns')}</span>
        <input type="text" bind:value={dns} disabled={reviewing} />
      </label>
    </div>
    {#if reviewing}
      <div class="notice notice-warning" role="alert">
        <p>{t(locale, 'network.warning')}</p>
        <label class="field">
          <span>{t(locale, 'network.confirmLabel', { expected })}</span>
          <input type="text" autocomplete="off" bind:value={confirmation} />
        </label>
        <div class="url-actions">
          <button
            type="button"
            class="button button-primary"
            disabled={busy || confirmation.trim() !== expected}
            onclick={() => void apply()}>{t(locale, 'network.apply')}</button
          >
          <button
            type="button"
            class="button button-secondary"
            disabled={busy}
            onclick={() => {
              reviewing = false
              confirmation = ''
            }}>{t(locale, 'network.cancel')}</button
          >
        </div>
      </div>
    {:else}
      <div class="url-actions">
        <button
          type="button"
          class="button button-secondary"
          onclick={() => (reviewing = true)}
          >{t(locale, 'network.review')}</button
        >
      </div>
    {/if}
    {#if busy}
      <p class="live-status" role="status">{t(locale, 'settings.applying')}</p>
    {/if}
  {/if}
  {#if failure}
    <p class="notice" role="alert">{failure}</p>
  {/if}
  {#if result}
    {#if result.reboot_requested}
      <p class="live-status">{t(locale, 'network.rebooted')}</p>
    {/if}
    {#if result.report.outcome === 'disconnected'}
      <p class="notice notice-warning" role="alert">
        {result.new_endpoint
          ? t(locale, 'network.lost', { endpoint: result.new_endpoint })
          : t(locale, 'network.lostDhcp')}
      </p>
    {:else if result.new_endpoint}
      <p class="live-status" role="status">
        {t(locale, 'network.moved', { endpoint: result.new_endpoint })}
      </p>
    {/if}
    <MutationResult {locale} report={result.report} />
  {/if}
</section>
