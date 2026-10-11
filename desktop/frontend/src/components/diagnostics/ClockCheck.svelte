<script lang="ts">
  import {
    api,
    type ClockCheckResult,
    type ClockEstimate,
    type Locale
  } from '../../lib/api'
  import { errorMessage, t, type TranslationKey } from '../../lib/i18n'

  let {
    locale,
    deviceId,
    profileToken
  }: { locale: Locale; deviceId: string; profileToken: string } = $props()

  let busy = $state(false)
  let failure = $state('')
  let result = $state<ClockCheckResult | undefined>()

  const verdictKeys: Record<string, TranslationKey> = {
    in_sync: 'clock.inSync',
    out_of_sync: 'clock.outOfSync',
    uncertain: 'clock.uncertain'
  }

  function seconds(value: number): string {
    return new Intl.NumberFormat(locale, {
      maximumFractionDigits: 3
    }).format(value)
  }

  function estimates(value: ClockCheckResult): ClockEstimate[] {
    return [value.onvif, value.rtcp].filter(
      (estimate): estimate is ClockEstimate => estimate !== undefined
    )
  }

  async function run(): Promise<void> {
    busy = true
    failure = ''
    const response = await api.clockCheck(deviceId, profileToken)
    busy = false
    result = response.data
    if (response.error) failure = errorMessage(locale, response.error)
  }
</script>

<section class="settings-section" aria-labelledby="clock-heading">
  <h3 id="clock-heading">{t(locale, 'clock.title')}</h3>
  <p class="field"><small>{t(locale, 'clock.explain')}</small></p>
  <div class="url-actions">
    <button
      type="button"
      class="button button-primary"
      disabled={busy}
      onclick={() => void run()}
      >{busy ? t(locale, 'clock.running') : t(locale, 'clock.run')}</button
    >
  </div>
  {#if failure}
    <p class="notice" role="alert">{failure}</p>
  {/if}
  {#if result}
    <div class="diag-result" data-verdict={result.verdict}>
      <p role="status">
        {t(locale, verdictKeys[result.verdict] ?? 'clock.uncertain', {
          tolerance: result.tolerance_seconds
        })}
      </p>
      <table aria-label={t(locale, 'clock.title')}>
        <thead>
          <tr>
            <th scope="col">{t(locale, 'clock.source')}</th>
            <th scope="col">{t(locale, 'clock.offset')}</th>
            <th scope="col">{t(locale, 'clock.uncertainty')}</th>
          </tr>
        </thead>
        <tbody>
          {#each estimates(result) as estimate (estimate.source)}
            <tr>
              <th scope="row">{estimate.source.toUpperCase()}</th>
              <td>{seconds(estimate.offset_seconds)} s</td>
              <td>± {seconds(estimate.uncertainty_seconds)} s</td>
            </tr>
          {/each}
        </tbody>
      </table>
      {#if result.rtcp_unavailable}
        <small
          >{t(locale, 'clock.rtcpUnavailable', {
            reason: result.rtcp_unavailable
          })}</small
        >
      {/if}
      <small
        >{t(locale, 'clock.config', {
          mode: result.date_time_type || '—',
          servers: result.ntp_servers.join(', ') || '—'
        })}</small
      >
    </div>
  {/if}
</section>
