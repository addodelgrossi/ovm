<script lang="ts">
  import { onDestroy } from 'svelte'
  import { api, type Locale } from '../lib/api'
  import { t } from '../lib/i18n'

  let {
    locale,
    url,
    kind,
    profileName
  }: {
    locale: Locale
    url: string
    kind: string
    profileName: string
  } = $props()

  let status = $state<'idle' | 'copied' | 'failed'>('idle')
  let timer: ReturnType<typeof setTimeout> | undefined

  async function copy(): Promise<void> {
    status = (await api.copyText(url)) ? 'copied' : 'failed'

    if (timer) clearTimeout(timer)
    timer = setTimeout(() => (status = 'idle'), 2500)
  }

  onDestroy(() => {
    if (timer) clearTimeout(timer)
  })
</script>

<button
  type="button"
  class="button button-secondary"
  aria-label={status === 'copied'
    ? t(locale, 'streams.copiedLabel', { kind, profile: profileName })
    : t(locale, 'streams.copyLabel', { kind, profile: profileName })}
  onclick={() => void copy()}
>
  <span aria-hidden="true">{status === 'copied' ? '✓' : '⧉'}</span>
  {status === 'copied' ? t(locale, 'action.copied') : t(locale, 'action.copy')}
</button>
<span class="sr-only" aria-live="polite">
  {status === 'copied'
    ? t(locale, 'streams.copiedLabel', { kind, profile: profileName })
    : status === 'failed'
      ? t(locale, 'streams.copyFailed')
      : ''}
</span>
