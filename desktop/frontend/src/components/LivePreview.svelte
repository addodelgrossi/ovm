<script lang="ts">
  import { onDestroy } from 'svelte'
  import { api, type Locale, type PreviewStats } from '../lib/api'
  import { errorMessage, t } from '../lib/i18n'
  import { LivePlayer, type LivePlayerState } from '../lib/livePlayer'

  let {
    locale,
    deviceId,
    profileToken,
    profileName
  }: {
    locale: Locale
    deviceId: string
    profileToken: string
    profileName: string
  } = $props()

  let video = $state<HTMLVideoElement>()
  let active = $state(false)
  let playerState = $state<LivePlayerState | undefined>()
  let stats = $state<PreviewStats | undefined>()
  let player: LivePlayer | undefined
  let sessionId = ''
  let statsTimer: ReturnType<typeof setInterval> | undefined

  const statsIntervalMs = 1000

  async function refreshStats(): Promise<void> {
    if (!sessionId) return
    const response = await api.previewStats(sessionId)
    if (response.data && active) stats = response.data
  }

  function number(value: number, digits = 1): string {
    return new Intl.NumberFormat(locale, {
      maximumFractionDigits: digits,
      minimumFractionDigits: digits
    }).format(value)
  }

  async function start(): Promise<void> {
    active = true
    playerState = { kind: 'connecting' }
    const response = await api.startPreview(deviceId, profileToken)
    if (!active) {
      if (response.data) void api.stopPreview(response.data.session_id)
      return
    }
    if (!response.data || !video) {
      playerState = {
        kind: 'failed',
        detail: response.error
          ? errorMessage(locale, response.error)
          : undefined
      }
      return
    }
    sessionId = response.data.session_id
    statsTimer = setInterval(() => void refreshStats(), statsIntervalMs)
    player = new LivePlayer(video, response.data, (next) => {
      playerState = next
    })
    player.start()
  }

  function stop(): void {
    active = false
    if (statsTimer) clearInterval(statsTimer)
    statsTimer = undefined
    stats = undefined
    player?.stop()
    player = undefined
    if (sessionId) void api.stopPreview(sessionId)
    sessionId = ''
    playerState = undefined
  }

  function statusText(state: LivePlayerState): string {
    switch (state.kind) {
      case 'connecting':
        return t(locale, 'live.connecting')
      case 'playing':
        return t(locale, 'live.playing')
      case 'ended':
        return t(locale, 'live.ended')
      case 'mse_unavailable':
        return t(locale, 'live.mseUnavailable')
      case 'codec_unsupported':
        return t(locale, 'live.codecUnsupported', { codec: state.codec })
      default:
        return state.detail
          ? `${t(locale, 'live.failed')} ${state.detail}`
          : t(locale, 'live.failed')
    }
  }

  onDestroy(stop)
</script>

<div class="live-preview">
  <div class="live-frame" hidden={!active}>
    <video
      bind:this={video}
      muted
      autoplay
      playsinline
      aria-label={t(locale, 'live.label', { profile: profileName })}
    ></video>
  </div>
  <div class="url-actions">
    {#if active}
      <button type="button" class="button button-secondary" onclick={stop}
        >{t(locale, 'live.stop')}</button
      >
    {:else}
      <button
        type="button"
        class="button button-secondary"
        onclick={() => void start()}>{t(locale, 'live.start')}</button
      >
    {/if}
    {#if stats?.first_frame_received}
      <dl class="live-stats" aria-label={t(locale, 'live.statsLabel')}>
        <div>
          <dt>{t(locale, 'live.statResolution')}</dt>
          <dd>{stats.width} × {stats.height}</dd>
        </div>
        <div>
          <dt>{t(locale, 'live.statFps')}</dt>
          <dd>{number(stats.frames_per_second)} fps</dd>
        </div>
        <div>
          <dt>{t(locale, 'live.statBitrate')}</dt>
          <dd>{number(stats.bitrate_kbps, 0)} kbps</dd>
        </div>
        <div>
          <dt>{t(locale, 'live.statGop')}</dt>
          <dd>
            {stats.gop_frames > 0
              ? t(locale, 'live.gopValue', {
                  frames: String(stats.gop_frames),
                  seconds: number(stats.gop_seconds, 2)
                })
              : '—'}
          </dd>
        </div>
        <div>
          <dt>{t(locale, 'live.statFirstFrame')}</dt>
          <dd>{stats.time_to_first_frame_ms} ms</dd>
        </div>
        <div>
          <dt>{t(locale, 'live.statLost')}</dt>
          <dd>{stats.lost_packets}</dd>
        </div>
      </dl>
    {/if}
    {#if playerState}
      <span
        class="live-status"
        data-state={playerState.kind}
        role="status"
        aria-live="polite">{statusText(playerState)}</span
      >
    {/if}
  </div>
</div>
