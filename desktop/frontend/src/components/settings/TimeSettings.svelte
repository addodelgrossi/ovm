<script lang="ts">
  import {
    api,
    type Locale,
    type TimeApplyResult,
    type TimeView
  } from '../../lib/api'
  import { errorMessage, t } from '../../lib/i18n'
  import MutationResult from './MutationResult.svelte'

  let { locale, deviceId }: { locale: Locale; deviceId: string } = $props()

  let view = $state<TimeView | undefined>()
  let mode = $state('NTP')
  let servers = $state('')
  let timeZone = $state('')
  let daylight = $state(false)
  let busy = $state(false)
  let failure = $state('')
  let result = $state<TimeApplyResult | undefined>()

  $effect(() => {
    void load(deviceId)
  })

  async function load(id: string): Promise<void> {
    busy = true
    failure = ''
    const response = await api.readTime(id)
    busy = false
    view = response.data
    if (!response.data) {
      failure = response.error ? errorMessage(locale, response.error) : ''
      return
    }
    mode = response.data.date_time_type === 'Manual' ? 'Manual' : 'NTP'
    servers = (response.data.ntp_servers ?? []).join(', ')
    timeZone = response.data.time_zone
    daylight = response.data.daylight_savings
  }

  function number(value: number): string {
    return new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(
      value
    )
  }

  async function apply(): Promise<void> {
    busy = true
    failure = ''
    result = undefined
    const response = await api.applyTime(deviceId, {
      mode,
      ntp_servers:
        mode === 'NTP'
          ? servers
              .split(',')
              .map((server) => server.trim())
              .filter(Boolean)
          : [],
      time_zone: timeZone.trim(),
      daylight_savings: daylight
    })
    busy = false
    if (response.error) failure = errorMessage(locale, response.error)
    if (response.data?.report) {
      result = response.data
      if (response.data.after) view = response.data.after
    }
  }
</script>

<section class="settings-section" aria-labelledby="time-heading">
  <h3 id="time-heading">{t(locale, 'time.title')}</h3>
  {#if view}
    {#if view.device_utc}
      <p class="safe-url-notice">
        {t(locale, 'time.current', { time: view.device_utc })}
        {#if view.clock_offset_seconds !== undefined}
          · {t(locale, 'time.offset', {
            offset: number(view.clock_offset_seconds),
            uncertainty: number(view.uncertainty_seconds)
          })}
        {/if}
      </p>
      <p class="field"><small>{t(locale, 'time.precision')}</small></p>
    {/if}
    <fieldset class="segmented">
      <legend>{t(locale, 'time.mode')}</legend>
      <label
        ><input type="radio" name="time-mode" value="NTP" bind:group={mode} />
        {t(locale, 'time.modeNTP')}</label
      >
      <label
        ><input
          type="radio"
          name="time-mode"
          value="Manual"
          bind:group={mode}
        />
        {t(locale, 'time.modeManual')}</label
      >
    </fieldset>
    <div class="settings-grid">
      {#if mode === 'NTP'}
        <label class="field">
          <span>{t(locale, 'time.servers')}</span>
          <input type="text" bind:value={servers} />
        </label>
      {/if}
      <label class="field">
        <span>{t(locale, 'time.timeZone')}</span>
        <input type="text" bind:value={timeZone} />
      </label>
      <label class="field checkbox-field">
        <input type="checkbox" bind:checked={daylight} />
        <span>{t(locale, 'time.daylight')}</span>
      </label>
    </div>
    <div class="url-actions">
      <button
        type="button"
        class="button button-primary"
        disabled={busy}
        onclick={() => void apply()}>{t(locale, 'settings.apply')}</button
      >
    </div>
  {:else if busy}
    <p class="live-status" role="status">{t(locale, 'settings.loading')}</p>
  {/if}
  {#if failure}
    <p class="notice" role="alert">{failure}</p>
  {/if}
  {#if result}
    <MutationResult {locale} report={result.report} />
  {/if}
</section>
