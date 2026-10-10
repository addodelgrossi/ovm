<script lang="ts">
  import { errorMessage, t } from '../lib/i18n'
  import type { AppState } from '../lib/state.svelte'

  let { state }: { state: AppState } = $props()

  function toastText(toast: AppState['toasts'][number]): string {
    if (toast.key) return t(state.locale, toast.key, toast.values)
    if (toast.error) return errorMessage(state.locale, toast.error)
    return toast.message ?? ''
  }
</script>

<div class="toast-region" aria-live="polite" aria-relevant="additions">
  {#each state.toasts as toast (toast.id)}
    <div
      class="toast"
      data-tone={toast.tone}
      role={toast.tone === 'error' ? 'alert' : 'status'}
    >
      <span class="toast-mark" aria-hidden="true">
        {toast.tone === 'success' ? '✓' : toast.tone === 'error' ? '!' : 'i'}
      </span>
      <span>{toastText(toast)}</span>
      <button
        type="button"
        class="toast-close"
        aria-label={t(state.locale, 'action.dismiss')}
        onclick={() => state.dismissToast(toast.id)}>×</button
      >
    </div>
  {/each}
</div>
