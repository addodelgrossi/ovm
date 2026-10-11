<script lang="ts">
  import { api, type Locale, type ProbeResult } from '../lib/api'
  import { errorMessage, t } from '../lib/i18n'

  let {
    locale,
    deviceId,
    profileToken
  }: { locale: Locale; deviceId: string; profileToken: string } = $props()

  const seconds = 30
  let running = $state(false)
  let result = $state<ProbeResult | undefined>()
  let failure = $state('')

  async function probe(): Promise<void> {
    running = true
    failure = ''
    result = undefined
    const response = await api.probeStream(
      deviceId,
      profileToken,
      'tcp',
      seconds
    )
    running = false
    if (response.data && response.data.frames > 0) result = response.data
    if (response.error) failure = errorMessage(locale, response.error)
  }

  function number(value: number, digits = 1): string {
    return new Intl.NumberFormat(locale, {
      maximumFractionDigits: digits,
      minimumFractionDigits: digits
    }).format(value)
  }
</script>

<div class="stream-probe">
  <div class="url-actions">
    <button
      type="button"
      class="button button-secondary"
      disabled={running}
      onclick={() => void probe()}
      >{t(locale, 'probe.start', { seconds: String(seconds) })}</button
    >
    {#if running}
      <span class="live-status" role="status"
        >{t(locale, 'probe.running', { seconds: String(seconds) })}</span
      >
    {/if}
    {#if failure}
      <span class="live-status" role="alert">{failure}</span>
    {/if}
  </div>
  {#if result}
    <dl class="live-stats" aria-label={t(locale, 'probe.title')}>
      <div>
        <dt>{t(locale, 'probe.codec')}</dt>
        <dd>{result.codec.toUpperCase()}</dd>
      </div>
      <div>
        <dt>{t(locale, 'live.statResolution')}</dt>
        <dd>{result.width} × {result.height}</dd>
      </div>
      <div>
        <dt>{t(locale, 'live.statFps')}</dt>
        <dd>{number(result.frames_per_second, 2)} fps</dd>
      </div>
      <div>
        <dt>{t(locale, 'live.statBitrate')}</dt>
        <dd>{number(result.bitrate_kbps, 0)} kbps</dd>
      </div>
      <div>
        <dt>{t(locale, 'live.statGop')}</dt>
        <dd>
          {result.gop_frames > 0
            ? t(locale, 'live.gopValue', {
                frames: String(result.gop_frames),
                seconds: number(result.gop_seconds, 2)
              })
            : '—'}
        </dd>
      </div>
      <div>
        <dt>{t(locale, 'live.statFirstFrame')}</dt>
        <dd>{result.time_to_first_frame_ms} ms</dd>
      </div>
      <div>
        <dt>{t(locale, 'probe.keyframes')}</dt>
        <dd>{result.keyframes}</dd>
      </div>
      <div>
        <dt>{t(locale, 'live.statLost')}</dt>
        <dd>{result.lost_packets}</dd>
      </div>
      <div>
        <dt>{t(locale, 'probe.duration')}</dt>
        <dd>{number(result.duration_ms / 1000)} s</dd>
      </div>
      <div>
        <dd>
          {result.complete
            ? t(locale, 'probe.complete')
            : t(locale, 'probe.partial')}
        </dd>
      </div>
    </dl>
  {/if}
</div>
