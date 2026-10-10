<script lang="ts">
  import { untrack } from 'svelte'
  import type { AuthenticateInput, Device, Locale } from '../lib/api'
  import { t } from '../lib/i18n'

  let {
    device,
    locale,
    busy,
    onSubmit,
    onClose
  }: {
    device: Device
    locale: Locale
    busy: boolean
    onSubmit: (input: AuthenticateInput) => Promise<boolean>
    onClose: () => void
  } = $props()

  let username = $state('')
  let password = $state('')
  let allowBasic = $state(untrack(() => device.auth_policy.allow_basic))
  let allowBasicOverHTTP = $state(
    untrack(() => device.auth_policy.allow_basic_over_http)
  )
  let allowLegacyDigest = $state(
    untrack(() => device.auth_policy.allow_legacy_digest)
  )
  let usernameInput: HTMLInputElement
  let name = $derived(device.display_name?.trim() || device.endpoint)

  $effect(() => {
    const previous = document.activeElement as HTMLElement | null
    queueMicrotask(() => usernameInput?.focus())
    return () => {
      password = ''
      previous?.focus()
    }
  })

  function close(): void {
    password = ''
    onClose()
  }

  function keydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault()
      close()
    }
  }

  function updateBasic(event: Event): void {
    allowBasic = (event.currentTarget as HTMLInputElement).checked
    if (!allowBasic) allowBasicOverHTTP = false
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault()
    const input: AuthenticateInput = {
      device_id: device.id,
      username,
      password,
      allow_basic: allowBasic,
      allow_basic_over_http: allowBasicOverHTTP,
      allow_legacy_digest: allowLegacyDigest,
      timeout_milliseconds: 0
    }
    // Clear before awaiting so the secret never outlives the form submission.
    password = ''
    const succeeded = await onSubmit(input)
    if (succeeded) close()
  }
</script>

<svelte:window onkeydown={keydown} />

<div class="dialog-layer">
  <button
    type="button"
    class="dialog-scrim"
    aria-label={t(locale, 'action.close')}
    onclick={close}
  ></button>
  <div
    class="dialog-panel dialog-panel-wide"
    role="dialog"
    tabindex="-1"
    aria-modal="true"
    aria-labelledby="auth-dialog-title"
    aria-describedby="auth-dialog-description"
  >
    <header class="dialog-header">
      <div>
        <p class="eyebrow">{t(locale, 'action.authenticate')}</p>
        <h2 id="auth-dialog-title">
          {t(locale, 'dialog.authTitle', { name })}
        </h2>
      </div>
      <button
        type="button"
        class="icon-button"
        aria-label={t(locale, 'action.close')}
        onclick={close}>×</button
      >
    </header>
    <p id="auth-dialog-description" class="dialog-description">
      {t(locale, 'dialog.authBody')}
    </p>

    <form onsubmit={submit}>
      <div class="form-grid">
        <div>
          <label for="auth-username">{t(locale, 'dialog.username')}</label>
          <input
            id="auth-username"
            bind:this={usernameInput}
            bind:value={username}
            required
            maxlength="1024"
            autocomplete="username"
          />
        </div>
        <div>
          <label for="auth-password">{t(locale, 'dialog.password')}</label>
          <input
            id="auth-password"
            type="password"
            bind:value={password}
            required
            maxlength="1024"
            autocomplete="current-password"
            aria-describedby="password-hint"
          />
        </div>
      </div>
      <p id="password-hint" class="field-hint">
        {t(locale, 'dialog.passwordHint')}
      </p>

      <fieldset class="policy-fieldset">
        <legend>{t(locale, 'dialog.policyTitle')}</legend>
        <label class="check-row">
          <input
            type="checkbox"
            aria-label={t(locale, 'dialog.basic')}
            checked={allowBasic}
            onchange={updateBasic}
          />
          <span>
            <strong>{t(locale, 'dialog.basic')}</strong>
            <small>{t(locale, 'dialog.basicRisk')}</small>
          </span>
        </label>
        <label class="check-row risk-row">
          <input
            type="checkbox"
            aria-label={t(locale, 'dialog.basicHttp')}
            bind:checked={allowBasicOverHTTP}
            disabled={!allowBasic}
          />
          <span>
            <strong>{t(locale, 'dialog.basicHttp')}</strong>
            <small>{t(locale, 'dialog.basicHttpRisk')}</small>
          </span>
        </label>
        <label class="check-row">
          <input
            type="checkbox"
            aria-label={t(locale, 'dialog.legacyDigest')}
            bind:checked={allowLegacyDigest}
          />
          <span>
            <strong>{t(locale, 'dialog.legacyDigest')}</strong>
            <small>{t(locale, 'dialog.legacyDigestRisk')}</small>
          </span>
        </label>
      </fieldset>

      <div class="dialog-actions">
        <button type="button" class="button button-secondary" onclick={close}>
          {t(locale, 'action.cancel')}
        </button>
        <button
          type="submit"
          class="button button-primary"
          disabled={busy || !username.trim() || !password}
        >
          {busy ? t(locale, 'app.loading') : t(locale, 'dialog.authSubmit')}
        </button>
      </div>
    </form>
  </div>
</div>
