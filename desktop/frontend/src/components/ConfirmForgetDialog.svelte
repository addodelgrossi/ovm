<script lang="ts">
  import type { Device, Locale } from '../lib/api'
  import { t } from '../lib/i18n'

  let {
    device,
    locale,
    busy,
    onConfirm,
    onClose
  }: {
    device: Device
    locale: Locale
    busy: boolean
    onConfirm: () => Promise<boolean>
    onClose: () => void
  } = $props()

  let confirmButton: HTMLButtonElement
  let name = $derived(device.display_name?.trim() || device.endpoint)

  $effect(() => {
    const previous = document.activeElement as HTMLElement | null
    queueMicrotask(() => confirmButton?.focus())
    return () => previous?.focus()
  })

  function keydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault()
      onClose()
    }
  }

  async function confirm(): Promise<void> {
    if (await onConfirm()) onClose()
  }
</script>

<svelte:window onkeydown={keydown} />

<div class="dialog-layer">
  <button
    type="button"
    class="dialog-scrim"
    aria-label={t(locale, 'action.close')}
    onclick={onClose}
  ></button>
  <div
    class="dialog-panel dialog-panel-small"
    role="alertdialog"
    tabindex="-1"
    aria-modal="true"
    aria-labelledby="forget-dialog-title"
    aria-describedby="forget-dialog-description"
  >
    <header class="dialog-header">
      <h2 id="forget-dialog-title">
        {t(locale, 'dialog.forgetTitle', { name })}
      </h2>
      <button
        type="button"
        class="icon-button"
        aria-label={t(locale, 'action.close')}
        onclick={onClose}>×</button
      >
    </header>
    <p id="forget-dialog-description" class="dialog-description">
      {t(locale, 'dialog.forgetBody')}
    </p>
    <div class="dialog-actions">
      <button type="button" class="button button-secondary" onclick={onClose}>
        {t(locale, 'action.cancel')}
      </button>
      <button
        bind:this={confirmButton}
        type="button"
        class="button button-danger"
        disabled={busy}
        onclick={() => void confirm()}
      >
        {t(locale, 'dialog.forgetConfirm')}
      </button>
    </div>
  </div>
</div>
