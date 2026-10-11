<script lang="ts">
  import { untrack } from 'svelte'
  import { api, type Device, type Locale, type MediaProfile } from '../lib/api'
  import { errorMessage, t, type TranslationKey } from '../lib/i18n'
  import {
    clearProgress,
    h264Change,
    loadProgress,
    nextStep,
    quickSetupSteps,
    saveProgress,
    type QuickSetupProgress,
    type QuickSetupStep
  } from '../lib/quickSetup'

  let {
    locale,
    device,
    profiles,
    onClose,
    onChanged
  }: {
    locale: Locale
    device: Device
    profiles: MediaProfile[]
    onClose: () => void
    onChanged: () => void
  } = $props()

  // The dialog is opened for one device; it is not reused across devices.
  let progress = $state<QuickSetupProgress>(
    untrack(() => loadProgress(device.id))
  )
  let step = $derived(nextStep(progress))
  let busy = $state(false)
  let failure = $state('')
  let notes = $state<string[]>([])

  let username = $state('')
  let level = $state('Administrator')
  let defaultWarning = $state(false)
  let password = $state('')
  let confirmation = $state('')
  let ntpServers = $state('pool.ntp.org')
  let timeZone = $state('')

  const stepTitles: Record<QuickSetupStep, TranslationKey> = {
    password: 'quick.passwordTitle',
    h264: 'quick.h264Title',
    day: 'quick.dayTitle',
    ntp: 'quick.ntpTitle'
  }

  $effect(() => {
    void prepare(step)
  })

  async function prepare(current: QuickSetupStep | undefined): Promise<void> {
    failure = ''
    if (current === 'password') {
      const response = await api.readUsers(device.id)
      if (response.data) {
        username = response.data.current_username
        defaultWarning = response.data.default_credential_warning
        level =
          response.data.users?.find((user) => user.username === username)
            ?.level ?? 'Administrator'
      } else if (response.error) {
        failure = errorMessage(locale, response.error)
      }
    }
    if (current === 'ntp') {
      const response = await api.readTime(device.id)
      if (response.data) {
        timeZone = response.data.time_zone
        if (response.data.ntp_servers?.length)
          ntpServers = response.data.ntp_servers.join(', ')
      }
    }
  }

  function mark(current: QuickSetupStep, status: 'done' | 'skipped'): void {
    progress = { ...progress, [current]: status }
    saveProgress(device.id, progress)
  }

  function outcomeNote(outcome: string, label: string): string {
    return t(locale, 'quick.outcome', {
      step: label,
      outcome: t(locale, `settings.outcome.${outcome}` as TranslationKey)
    })
  }

  async function runPassword(): Promise<boolean> {
    if (password.length < 8 || password !== confirmation) {
      failure = t(locale, 'quick.passwordMismatch')
      return false
    }
    const response = await api.setUser(device.id, username, password, level)
    password = ''
    confirmation = ''
    if (response.error) {
      failure = errorMessage(locale, response.error)
      return false
    }
    notes = [
      ...notes,
      response.data?.sign_in_verified
        ? t(locale, 'quick.passwordVerified')
        : t(locale, 'quick.passwordUnverified')
    ]
    return response.data?.sign_in_verified ?? false
  }

  async function runH264(): Promise<boolean> {
    let ok = true
    for (const profile of profiles) {
      const view = await api.readEncoder(device.id, profile.token)
      if (!view.data) {
        if (view.error) failure = errorMessage(locale, view.error)
        ok = false
        continue
      }
      const change = h264Change(view.data)
      if (!change) {
        notes = [
          ...notes,
          view.data.current.encoding.toUpperCase() === 'H264'
            ? t(locale, 'quick.alreadyH264', { profile: profile.token })
            : t(locale, 'quick.noH264', { profile: profile.token })
        ]
        continue
      }
      const response = await api.applyEncoder(device.id, profile.token, change)
      if (response.error) {
        failure = errorMessage(locale, response.error)
        ok = false
        continue
      }
      if (response.data) {
        notes = [
          ...notes,
          outcomeNote(response.data.report.outcome, profile.token)
        ]
        ok &&= response.data.report.outcome !== 'not_applied'
      }
    }
    onChanged()
    return ok
  }

  async function runDay(): Promise<boolean> {
    const profile = profiles[0]
    if (!profile) return false
    const response = await api.applyImaging(device.id, profile.token, {
      ir_cut_filter: 'ON'
    })
    if (response.error) {
      failure = errorMessage(locale, response.error)
      return false
    }
    if (!response.data) return false
    notes = [...notes, outcomeNote(response.data.report.outcome, 'IR cut')]
    return response.data.report.outcome === 'exact'
  }

  async function runNTP(): Promise<boolean> {
    const servers = ntpServers
      .split(',')
      .map((server) => server.trim())
      .filter(Boolean)
    const response = await api.applyTime(device.id, {
      mode: 'NTP',
      ntp_servers: servers,
      time_zone: timeZone.trim(),
      daylight_savings: false
    })
    if (response.error) {
      failure = errorMessage(locale, response.error)
      return false
    }
    if (!response.data) return false
    notes = [...notes, outcomeNote(response.data.report.outcome, 'NTP')]
    return (
      response.data.report.outcome === 'exact' ||
      response.data.report.outcome === 'adjusted'
    )
  }

  async function apply(): Promise<void> {
    if (!step) return
    busy = true
    failure = ''
    const runners: Record<QuickSetupStep, () => Promise<boolean>> = {
      password: runPassword,
      h264: runH264,
      day: runDay,
      ntp: runNTP
    }
    const current = step
    const ok = await runners[current]()
    busy = false
    if (ok) mark(current, 'done')
  }

  function restart(): void {
    clearProgress(device.id)
    progress = loadProgress(device.id)
    notes = []
  }

  function keydown(event: KeyboardEvent): void {
    if (event.key === 'Escape' && !busy) {
      event.preventDefault()
      onClose()
    }
  }
</script>

<svelte:window onkeydown={keydown} />

<div class="dialog-layer">
  <button
    type="button"
    class="dialog-scrim"
    aria-label={t(locale, 'action.close')}
    onclick={() => !busy && onClose()}
  ></button>
  <div
    class="dialog-panel quick-setup"
    role="dialog"
    tabindex="-1"
    aria-modal="true"
    aria-labelledby="quick-title"
  >
    <header class="dialog-header">
      <div>
        <p class="eyebrow">{device.display_name || device.endpoint}</p>
        <h2 id="quick-title">{t(locale, 'quick.title')}</h2>
      </div>
      <button
        type="button"
        class="icon-button"
        aria-label={t(locale, 'action.close')}
        disabled={busy}
        onclick={onClose}>×</button
      >
    </header>
    <ol class="quick-steps">
      {#each quickSetupSteps as item (item)}
        <li data-status={progress[item]} class:current={item === step}>
          <span>{t(locale, stepTitles[item])}</span>
          <small
            >{t(
              locale,
              `quick.status.${progress[item]}` as TranslationKey
            )}</small
          >
        </li>
      {/each}
    </ol>

    {#if step === 'password'}
      <p class="dialog-description">
        {t(locale, 'quick.passwordBody', { username: username || '—' })}
      </p>
      {#if defaultWarning}
        <p class="notice" role="alert">{t(locale, 'quick.defaultWarning')}</p>
      {/if}
      <label for="quick-password">{t(locale, 'quick.newPassword')}</label>
      <input
        id="quick-password"
        type="password"
        autocomplete="new-password"
        maxlength="64"
        bind:value={password}
      />
      <label for="quick-confirm">{t(locale, 'quick.confirmPassword')}</label>
      <input
        id="quick-confirm"
        type="password"
        autocomplete="new-password"
        maxlength="64"
        bind:value={confirmation}
      />
    {:else if step === 'h264'}
      <p class="dialog-description">
        {t(locale, 'quick.h264Body', { count: profiles.length })}
      </p>
    {:else if step === 'day'}
      <p class="dialog-description">{t(locale, 'quick.dayBody')}</p>
    {:else if step === 'ntp'}
      <p class="dialog-description">{t(locale, 'quick.ntpBody')}</p>
      <label for="quick-ntp">{t(locale, 'time.servers')}</label>
      <input id="quick-ntp" type="text" bind:value={ntpServers} />
      <label for="quick-tz">{t(locale, 'time.timeZone')}</label>
      <input id="quick-tz" type="text" bind:value={timeZone} />
    {:else}
      <p class="dialog-description" role="status">
        {t(locale, 'quick.finished')}
      </p>
    {/if}

    {#if failure}
      <p class="notice" role="alert">{failure}</p>
    {/if}
    {#if notes.length > 0}
      <ul class="quick-notes">
        {#each notes as note, index (index)}
          <li>{note}</li>
        {/each}
      </ul>
    {/if}

    <div class="dialog-actions">
      {#if step}
        <button
          type="button"
          class="button button-secondary"
          disabled={busy}
          onclick={() => step && mark(step, 'skipped')}
          >{t(locale, 'quick.skip')}</button
        >
        <button
          type="button"
          class="button button-primary"
          disabled={busy ||
            (step === 'password' && (!password || !confirmation)) ||
            (step !== 'password' && step !== 'ntp' && profiles.length === 0)}
          onclick={() => void apply()}
          >{busy
            ? t(locale, 'quick.applying')
            : t(locale, 'quick.apply')}</button
        >
      {:else}
        <button type="button" class="button button-secondary" onclick={restart}
          >{t(locale, 'quick.restart')}</button
        >
        <button type="button" class="button button-primary" onclick={onClose}
          >{t(locale, 'action.close')}</button
        >
      {/if}
    </div>
  </div>
</div>
