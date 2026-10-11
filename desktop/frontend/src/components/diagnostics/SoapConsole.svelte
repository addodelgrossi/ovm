<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import { api, type ConsoleEntry, type Locale } from '../../lib/api'
  import { errorMessage, t } from '../../lib/i18n'

  let { locale }: { locale: Locale } = $props()

  const maxShown = 500

  let enabled = $state(false)
  let entries = $state<ConsoleEntry[]>([])
  let failure = $state('')
  let filter = $state('')
  let selected = $state<number | undefined>()
  let lastId = 0
  let timer: ReturnType<typeof setInterval> | undefined

  let shown = $derived(
    entries.filter((entry) =>
      `${entry.action} ${entry.endpoint} ${entry.outcome}`
        .toLowerCase()
        .includes(filter.trim().toLowerCase())
    )
  )
  let current = $derived(entries.find((entry) => entry.id === selected))

  onMount(() => {
    void poll()
    timer = setInterval(() => void poll(), 1500)
  })

  onDestroy(() => {
    if (timer) clearInterval(timer)
  })

  async function poll(): Promise<void> {
    const response = await api.consoleEntries(lastId)
    if (!response.data) {
      failure = response.error ? errorMessage(locale, response.error) : ''
      return
    }
    enabled = response.data.enabled
    if (response.data.entries.length > 0) {
      lastId = response.data.entries[response.data.entries.length - 1].id
      entries = [...entries, ...response.data.entries].slice(-maxShown)
    }
  }

  async function toggle(): Promise<void> {
    const response = await api.setConsoleEnabled(!enabled)
    if (response.data) {
      enabled = response.data.enabled
      if (!enabled) {
        entries = []
        lastId = 0
        selected = undefined
      }
    }
  }

  async function clear(): Promise<void> {
    await api.clearConsole()
    entries = []
    selected = undefined
  }

  function action(entry: ConsoleEntry): string {
    const slash = entry.action.lastIndexOf('/')
    return slash >= 0 ? entry.action.slice(slash + 1) : entry.action
  }

  async function copyEntry(entry: ConsoleEntry): Promise<void> {
    await api.copyText(
      `<!-- ${entry.action} ${entry.endpoint} -->\n${entry.request_xml}\n\n${entry.response_xml}`
    )
  }
</script>

<section class="settings-section" aria-labelledby="console-heading">
  <h3 id="console-heading">{t(locale, 'console.title')}</h3>
  <p class="field"><small>{t(locale, 'console.explain')}</small></p>
  <div class="url-actions">
    <label class="field checkbox-field">
      <input type="checkbox" checked={enabled} onchange={() => void toggle()} />
      <span>{t(locale, 'console.capture')}</span>
    </label>
    <button
      type="button"
      class="button"
      disabled={entries.length === 0}
      onclick={() => void clear()}>{t(locale, 'console.clear')}</button
    >
    <input
      type="search"
      class="console-filter"
      placeholder={t(locale, 'console.filter')}
      bind:value={filter}
    />
  </div>
  {#if failure}
    <p class="notice" role="alert">{failure}</p>
  {/if}
  {#if shown.length > 0}
    <div class="table-scroll diag-feed">
      <table aria-label={t(locale, 'console.title')}>
        <thead>
          <tr>
            <th scope="col">{t(locale, 'events.time')}</th>
            <th scope="col">{t(locale, 'console.operation')}</th>
            <th scope="col">{t(locale, 'console.status')}</th>
            <th scope="col">ms</th>
          </tr>
        </thead>
        <tbody>
          {#each shown as entry (entry.id)}
            <tr
              class="console-row"
              class:selected={entry.id === selected}
              onclick={() => (selected = entry.id)}
            >
              <td>{new Date(entry.time).toLocaleTimeString(locale)}</td>
              <td>
                <button
                  type="button"
                  class="link-button"
                  onclick={() => (selected = entry.id)}>{action(entry)}</button
                >
              </td>
              <td>{entry.outcome} {entry.status_code || ''}</td>
              <td>{entry.duration_ms}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {:else if enabled}
    <p class="field"><small>{t(locale, 'console.empty')}</small></p>
  {/if}
  {#if current}
    <div class="console-detail">
      <div class="url-actions">
        <strong>{action(current)}</strong>
        <code>{current.endpoint}</code>
        <button
          type="button"
          class="button"
          onclick={() => current && void copyEntry(current)}
          >{t(locale, 'action.copy')}</button
        >
      </div>
      <h4>{t(locale, 'console.request')}</h4>
      <pre class="report-preview">{current.request_xml || '—'}</pre>
      <h4>{t(locale, 'console.response')}</h4>
      <pre class="report-preview">{current.response_xml || '—'}</pre>
    </div>
  {/if}
</section>
