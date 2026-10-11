<script lang="ts">
  import {
    api,
    type Locale,
    type MaintenanceResult,
    type SystemLogResult
  } from '../../lib/api'
  import { errorMessage, t } from '../../lib/i18n'

  let { locale, deviceId }: { locale: Locale; deviceId: string } = $props()

  let confirmingReboot = $state(false)
  let resetConfirmation = $state('')
  let busy = $state(false)
  let failure = $state('')
  let outcome = $state<MaintenanceResult | undefined>()
  let logType = $state('System')
  let log = $state<SystemLogResult | undefined>()

  async function run(
    call: () => Promise<{
      data?: MaintenanceResult
      error?: { category: string; code: string; message: string }
    }>
  ): Promise<void> {
    busy = true
    failure = ''
    outcome = undefined
    const response = await call()
    busy = false
    if (response.error) failure = errorMessage(locale, response.error)
    outcome = response.data
  }

  async function reboot(): Promise<void> {
    if (!confirmingReboot) {
      confirmingReboot = true
      return
    }
    confirmingReboot = false
    await run(() => api.reboot(deviceId))
  }

  async function reset(): Promise<void> {
    const confirmation = resetConfirmation
    resetConfirmation = ''
    await run(() => api.softFactoryReset(deviceId, confirmation))
  }

  async function loadLog(): Promise<void> {
    failure = ''
    const response = await api.systemLog(deviceId, logType)
    if (response.error) failure = errorMessage(locale, response.error)
    log = response.data
  }
</script>

<section class="settings-section" aria-labelledby="maintenance-heading">
  <h3 id="maintenance-heading">{t(locale, 'maintenance.title')}</h3>
  <div class="url-actions">
    <button
      type="button"
      class="button button-secondary"
      disabled={busy}
      onclick={() => void reboot()}
      >{confirmingReboot
        ? t(locale, 'maintenance.confirmReboot')
        : t(locale, 'maintenance.reboot')}</button
    >
  </div>

  <h4>{t(locale, 'maintenance.resetTitle')}</h4>
  <p class="field"><small>{t(locale, 'maintenance.resetHelp')}</small></p>
  <div class="url-actions">
    <input
      type="text"
      autocomplete="off"
      aria-label={t(locale, 'maintenance.resetConfirm')}
      placeholder={t(locale, 'maintenance.resetConfirm')}
      bind:value={resetConfirmation}
    />
    <button
      type="button"
      class="button button-secondary"
      disabled={busy || resetConfirmation !== 'RESET'}
      onclick={() => void reset()}>{t(locale, 'maintenance.reset')}</button
    >
  </div>

  {#if busy}
    <p class="live-status" role="status">{t(locale, 'maintenance.waiting')}</p>
  {/if}
  {#if outcome}
    <p class="live-status" role="status">
      {outcome.back_online
        ? t(locale, 'maintenance.back', {
            seconds: String(Math.round(outcome.downtime_seconds))
          })
        : t(locale, 'maintenance.notBack')}
    </p>
  {/if}

  <h4>{t(locale, 'maintenance.logTitle')}</h4>
  <div class="url-actions">
    <label class="field">
      <span>{t(locale, 'maintenance.logType')}</span>
      <select bind:value={logType}>
        <option value="System">{t(locale, 'maintenance.logSystem')}</option>
        <option value="Access">{t(locale, 'maintenance.logAccess')}</option>
      </select>
    </label>
    <button
      type="button"
      class="button button-secondary"
      onclick={() => void loadLog()}>{t(locale, 'maintenance.loadLog')}</button
    >
  </div>
  {#if log}
    {#if log.truncated}
      <p class="field"><small>{t(locale, 'maintenance.truncated')}</small></p>
    {/if}
    <pre
      class="system-log"
      aria-label={t(locale, 'maintenance.logTitle')}>{log.text}</pre>
  {/if}
  {#if failure}
    <p class="notice" role="alert">{failure}</p>
  {/if}
</section>
