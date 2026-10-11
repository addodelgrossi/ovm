<script lang="ts">
  import { onDestroy } from 'svelte'
  import {
    api,
    type Locale,
    type StabilityJobState,
    type Transport
  } from '../../lib/api'
  import { errorMessage, t, type TranslationKey } from '../../lib/i18n'

  let {
    locale,
    deviceId,
    profileToken
  }: { locale: Locale; deviceId: string; profileToken: string } = $props()

  let cycles = $state(10)
  let cycleSeconds = $state(20)
  let pauseSeconds = $state(1)
  let transport = $state<Transport>('tcp')
  let busy = $state(false)
  let failure = $state('')
  let job = $state<StabilityJobState | undefined>()
  let timer: ReturnType<typeof setInterval> | undefined

  let running = $derived(job?.state === 'running')
  let passed = $derived(job?.cycles.filter((cycle) => cycle.passed).length ?? 0)
  let failed = $derived((job?.cycles.length ?? 0) - passed)
  let estimate = $derived(
    Math.ceil((cycles * (cycleSeconds + pauseSeconds + 2)) / 60)
  )

  const verdictKeys: Record<string, TranslationKey> = {
    pass: 'stability.pass',
    fail: 'stability.fail',
    incomplete: 'stability.incomplete'
  }

  function usePreset(): void {
    cycles = 100
    cycleSeconds = 20
    pauseSeconds = 1
  }

  async function start(): Promise<void> {
    busy = true
    failure = ''
    const response = await api.startStability(deviceId, {
      profileToken,
      transport,
      cycles,
      cycleSeconds,
      pauseSeconds
    })
    busy = false
    if (!response.data) {
      failure = response.error ? errorMessage(locale, response.error) : ''
      return
    }
    job = response.data
    timer = setInterval(() => void poll(), 1000)
  }

  async function poll(): Promise<void> {
    if (!job) return
    const response = await api.stabilityStatus(job.job_id)
    if (!response.data) {
      stopTimer()
      return
    }
    job = response.data
    if (response.data.error) failure = errorMessage(locale, response.data.error)
    if (response.data.state !== 'running') stopTimer()
  }

  function stopTimer(): void {
    if (timer) clearInterval(timer)
    timer = undefined
  }

  async function stop(): Promise<void> {
    if (job) await api.stopStability(job.job_id)
  }

  function number(value: number): string {
    return new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(
      value
    )
  }

  onDestroy(() => {
    stopTimer()
  })
</script>

<section class="settings-section" aria-labelledby="stability-heading">
  <h3 id="stability-heading">{t(locale, 'stability.title')}</h3>
  <p class="field"><small>{t(locale, 'stability.explain')}</small></p>
  <div class="settings-grid">
    <label class="field">
      <span>{t(locale, 'stability.cycles')}</span>
      <input
        type="number"
        min="1"
        max="1000"
        disabled={running}
        bind:value={cycles}
      />
    </label>
    <label class="field">
      <span>{t(locale, 'stability.cycleSeconds')}</span>
      <input
        type="number"
        min="1"
        max="600"
        disabled={running}
        bind:value={cycleSeconds}
      />
    </label>
    <label class="field">
      <span>{t(locale, 'stability.pauseSeconds')}</span>
      <input
        type="number"
        min="0"
        max="60"
        disabled={running}
        bind:value={pauseSeconds}
      />
    </label>
    <label class="field">
      <span>{t(locale, 'stability.transport')}</span>
      <select disabled={running} bind:value={transport}>
        <option value="tcp">TCP</option>
        <option value="udp">UDP</option>
      </select>
    </label>
  </div>
  <div class="url-actions">
    {#if running}
      <button type="button" class="button" onclick={() => void stop()}
        >{t(locale, 'stability.stop')}</button
      >
    {:else}
      <button
        type="button"
        class="button button-primary"
        disabled={busy || !profileToken}
        onclick={() => void start()}>{t(locale, 'stability.start')}</button
      >
      <button type="button" class="button" onclick={usePreset}
        >{t(locale, 'stability.preset')}</button
      >
    {/if}
    <small>{t(locale, 'stability.estimate', { minutes: estimate })}</small>
  </div>
  {#if failure}
    <p class="notice" role="alert">{failure}</p>
  {/if}
  {#if job}
    <p class="live-status" role="status">
      {t(locale, 'stability.progress', {
        done: job.cycles.length,
        total: job.report?.cycles_requested ?? cycles,
        passed,
        failed
      })}
    </p>
    {#if job.cycles.length > 0}
      <div class="stability-strip" aria-hidden="true">
        {#each job.cycles as cycle (cycle.index)}
          <span
            class:cycle-pass={cycle.passed}
            class:cycle-fail={!cycle.passed}
            title={cycle.error ?? ''}
          ></span>
        {/each}
      </div>
    {/if}
    {#if job.report}
      {@const report = job.report}
      <div class="diag-result" data-verdict={report.verdict}>
        <p role="status">
          {t(locale, verdictKeys[report.verdict] ?? 'stability.incomplete', {
            passed: report.passed,
            total: report.cycles_completed
          })}
        </p>
        <table aria-label={t(locale, 'stability.title')}>
          <tbody>
            <tr>
              <th scope="row">{t(locale, 'stability.ttff')}</th>
              <td
                >{report.min_ttff_ms} / {report.median_ttff_ms} / {report.max_ttff_ms}
                ms</td
              >
            </tr>
            <tr>
              <th scope="row">{t(locale, 'stability.fps')}</th>
              <td>{number(report.mean_fps)}</td>
            </tr>
            <tr>
              <th scope="row">{t(locale, 'stability.bitrate')}</th>
              <td>{number(report.mean_bitrate_kbps)} kbps</td>
            </tr>
            <tr>
              <th scope="row">{t(locale, 'stability.lost')}</th>
              <td>{report.lost_packets}</td>
            </tr>
          </tbody>
        </table>
        {#each report.cycles.filter((cycle) => !cycle.passed) as cycle (cycle.index)}
          <small
            >{t(locale, 'stability.failedCycle', {
              index: cycle.index,
              error: cycle.error ?? ''
            })}</small
          >
        {/each}
      </div>
    {/if}
  {/if}
</section>
