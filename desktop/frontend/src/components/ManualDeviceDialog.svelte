<script lang="ts">
  import { t } from '../lib/i18n'
  import type { Locale } from '../lib/api'

  let {
    locale,
    busy,
    onSubmit,
    onClose
  }: {
    locale: Locale
    busy: boolean
    onSubmit: (endpoint: string, displayName: string) => Promise<boolean>
    onClose: () => void
  } = $props()

  let endpoint = $state('')
  let displayName = $state('')
  let endpointInput: HTMLInputElement

  $effect(() => {
    const previous = document.activeElement as HTMLElement | null
    queueMicrotask(() => endpointInput?.focus())
    return () => previous?.focus()
  })

  function close(): void {
    endpoint = ''
    displayName = ''
    onClose()
  }

  function keydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault()
      close()
    }
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault()
    if (!endpoint.trim()) return
    const succeeded = await onSubmit(endpoint.trim(), displayName.trim())
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
    class="dialog-panel"
    role="dialog"
    tabindex="-1"
    aria-modal="true"
    aria-labelledby="manual-dialog-title"
    aria-describedby="manual-dialog-description"
  >
    <header class="dialog-header">
      <div>
        <p class="eyebrow">{t(locale, 'sidebar.manualAdd')}</p>
        <h2 id="manual-dialog-title">{t(locale, 'dialog.manualTitle')}</h2>
      </div>
      <button
        type="button"
        class="icon-button"
        aria-label={t(locale, 'action.close')}
        onclick={close}>×</button
      >
    </header>
    <p id="manual-dialog-description" class="dialog-description">
      {t(locale, 'dialog.manualBody')}
    </p>

    <form onsubmit={submit}>
      <label for="manual-endpoint">{t(locale, 'dialog.endpoint')}</label>
      <input
        id="manual-endpoint"
        bind:this={endpointInput}
        bind:value={endpoint}
        required
        maxlength="4096"
        placeholder={t(locale, 'dialog.endpointPlaceholder')}
        autocomplete="url"
      />

      <label for="manual-name">{t(locale, 'dialog.displayName')}</label>
      <input
        id="manual-name"
        bind:value={displayName}
        maxlength="512"
        autocomplete="off"
      />

      <div class="dialog-actions">
        <button type="button" class="button button-secondary" onclick={close}>
          {t(locale, 'action.cancel')}
        </button>
        <button
          type="submit"
          class="button button-primary"
          disabled={busy || !endpoint.trim()}
        >
          {busy ? t(locale, 'app.loading') : t(locale, 'dialog.addDevice')}
        </button>
      </div>
    </form>
  </div>
</div>
