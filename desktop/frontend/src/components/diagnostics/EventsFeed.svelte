<script lang="ts">
  import { onDestroy } from 'svelte'
  import { api, type EventRecord, type Locale } from '../../lib/api'
  import { errorMessage, t } from '../../lib/i18n'

  let { locale, deviceId }: { locale: Locale; deviceId: string } = $props()

  const pollInterval = 1000
  const maxShown = 500

  let topicFilter = $state('')
  let textFilter = $state('')
  let feedId = $state('')
  let feedState = $state('')
  let busy = $state(false)
  let failure = $state('')
  let events = $state<EventRecord[]>([])
  let lastId = 0
  let timer: ReturnType<typeof setInterval> | undefined

  let shown = $derived(
    events.filter((event) => {
      const needle = textFilter.trim().toLowerCase()
      if (!needle) return true
      return describe(event).toLowerCase().includes(needle)
    })
  )

  function describe(event: EventRecord): string {
    return [
      event.topic,
      event.property_operation ?? '',
      ...[...event.source, ...event.data].map(
        (item) => `${item.name}=${item.value}`
      )
    ].join(' ')
  }

  function time(event: EventRecord): string {
    const value = event.utc_time || event.received_at
    const parsed = new Date(value)
    return Number.isNaN(parsed.getTime())
      ? value
      : parsed.toLocaleTimeString(locale)
  }

  async function start(): Promise<void> {
    busy = true
    failure = ''
    events = []
    lastId = 0
    const response = await api.startEvents(deviceId, topicFilter.trim())
    busy = false
    if (!response.data) {
      failure = response.error ? errorMessage(locale, response.error) : ''
      return
    }
    feedId = response.data.feed_id
    feedState = response.data.state
    timer = setInterval(() => void poll(), pollInterval)
  }

  async function poll(): Promise<void> {
    if (!feedId) return
    const response = await api.pollEvents(feedId, lastId)
    if (!response.data) {
      stopTimer()
      feedId = ''
      feedState = ''
      return
    }
    feedState = response.data.state
    if (response.data.events.length > 0) {
      lastId = response.data.events[response.data.events.length - 1].id
      events = [...response.data.events.reverse(), ...events].slice(0, maxShown)
    }
    if (response.data.error) failure = errorMessage(locale, response.data.error)
    if (feedState !== 'running') stopTimer()
  }

  function stopTimer(): void {
    if (timer) clearInterval(timer)
    timer = undefined
  }

  async function stop(): Promise<void> {
    stopTimer()
    const id = feedId
    feedId = ''
    feedState = ''
    if (id) await api.stopEvents(id)
  }

  onDestroy(() => {
    void stop()
  })
</script>

<section class="settings-section" aria-labelledby="events-heading">
  <h3 id="events-heading">{t(locale, 'events.title')}</h3>
  <p class="field"><small>{t(locale, 'events.explain')}</small></p>
  <div class="settings-grid">
    <label class="field">
      <span>{t(locale, 'events.topicFilter')}</span>
      <input
        type="text"
        placeholder="tns1:VideoSource//."
        disabled={feedState === 'running'}
        bind:value={topicFilter}
      />
    </label>
    <label class="field">
      <span>{t(locale, 'events.textFilter')}</span>
      <input type="search" bind:value={textFilter} />
    </label>
  </div>
  <div class="url-actions">
    {#if feedState === 'running'}
      <button type="button" class="button" onclick={() => void stop()}
        >{t(locale, 'events.stop')}</button
      >
      <span class="live-status" role="status"
        >{t(locale, 'events.listening', { count: events.length })}</span
      >
    {:else}
      <button
        type="button"
        class="button button-primary"
        disabled={busy}
        onclick={() => void start()}>{t(locale, 'events.start')}</button
      >
    {/if}
  </div>
  {#if failure}
    <p class="notice" role="alert">{failure}</p>
  {/if}
  {#if shown.length > 0}
    <div class="table-scroll diag-feed">
      <table aria-label={t(locale, 'events.title')}>
        <thead>
          <tr>
            <th scope="col">{t(locale, 'events.time')}</th>
            <th scope="col">{t(locale, 'events.topic')}</th>
            <th scope="col">{t(locale, 'events.details')}</th>
          </tr>
        </thead>
        <tbody>
          {#each shown as event (event.id)}
            <tr>
              <td>{time(event)}</td>
              <td>{event.topic}</td>
              <td>
                {[...event.source, ...event.data]
                  .map((item) => `${item.name}=${item.value}`)
                  .join(' ')}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {:else if feedState === 'running'}
    <p class="field"><small>{t(locale, 'events.waiting')}</small></p>
  {/if}
</section>
