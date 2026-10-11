<script lang="ts">
  import { api, type Locale, type ReportExport } from '../../lib/api'
  import { errorMessage, t } from '../../lib/i18n'

  let { locale, deviceId }: { locale: Locale; deviceId: string } = $props()

  let busy = $state(false)
  let failure = $state('')
  let saved = $state<ReportExport | undefined>()
  let copied = $state('')

  async function exportReport(): Promise<void> {
    busy = true
    failure = ''
    copied = ''
    const response = await api.exportReport(deviceId)
    busy = false
    saved = response.data
    if (response.error) failure = errorMessage(locale, response.error)
  }

  async function copy(text: string, what: string): Promise<void> {
    copied = (await api.copyText(text)) ? what : ''
  }

  async function reveal(path: string): Promise<void> {
    const response = await api.revealReport(path)
    if (response.error) failure = errorMessage(locale, response.error)
  }
</script>

<section class="settings-section" aria-labelledby="report-heading">
  <h3 id="report-heading">{t(locale, 'report.title')}</h3>
  <p class="field"><small>{t(locale, 'report.explain')}</small></p>
  <div class="url-actions">
    <button
      type="button"
      class="button button-primary"
      disabled={busy}
      onclick={() => void exportReport()}
      >{busy
        ? t(locale, 'report.building')
        : t(locale, 'report.export')}</button
    >
  </div>
  {#if failure}
    <p class="notice" role="alert">{failure}</p>
  {/if}
  {#if saved}
    <p class="live-status" role="status">{t(locale, 'report.saved')}</p>
    <code class="diag-path">{saved.markdown_path}</code>
    <code class="diag-path">{saved.json_path}</code>
    <div class="url-actions">
      <button
        type="button"
        class="button"
        onclick={() => void copy(saved?.markdown ?? '', 'markdown')}
        >{copied === 'markdown'
          ? t(locale, 'action.copied')
          : t(locale, 'report.copyMarkdown')}</button
      >
      <button
        type="button"
        class="button"
        onclick={() => void copy(saved?.markdown_path ?? '', 'path')}
        >{copied === 'path'
          ? t(locale, 'action.copied')
          : t(locale, 'report.copyPath')}</button
      >
      <button
        type="button"
        class="button"
        onclick={() => void reveal(saved?.markdown_path ?? '')}
        >{t(locale, 'report.reveal')}</button
      >
    </div>
    <details>
      <summary>{t(locale, 'report.preview')}</summary>
      <pre class="report-preview">{saved.markdown}</pre>
    </details>
  {/if}
</section>
