<script lang="ts">
  import { onDestroy } from 'svelte'
  import { api, type Locale, type PreviewStats } from '../lib/api'
  import { errorMessage, t } from '../lib/i18n'
  import { LivePlayer, type LivePlayerState } from '../lib/livePlayer'
  import { SnapshotPlayer } from '../lib/snapshotPlayer'

  type PreviewState =
    LivePlayerState | { kind: 'fallback'; codec?: string; fps: number }

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
  let image = $state<HTMLImageElement>()
  let active = $state(false)
  let showingSnapshots = $state(false)
  let playerState = $state<PreviewState | undefined>()
  let stats = $state<PreviewStats | undefined>()
  let recording = $state(false)
  let recordingMessage = $state('')
  let savedPath = $state('')
  let pathCopied = $state(false)
  let player: LivePlayer | undefined
  let snapshotPlayer: SnapshotPlayer | undefined
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
      if (
        next.kind === 'mse_unavailable' ||
        next.kind === 'codec_unsupported'
      ) {
        void fallBackToSnapshots(next)
        return
      }
      playerState = next
    })
    player.start()
  }

  // The WebView cannot decode the live codec: replace the live session with
  // authenticated JPEG snapshots from the same profile.
  async function fallBackToSnapshots(reason: LivePlayerState): Promise<void> {
    stopLive()
    const codec = reason.kind === 'codec_unsupported' ? reason.codec : undefined
    const response = await api.startSnapshotPreview(deviceId, profileToken)
    if (!active) {
      if (response.data) void api.stopPreview(response.data.session_id)
      return
    }
    if (!response.data || !image) {
      playerState = reason
      return
    }
    sessionId = response.data.session_id
    showingSnapshots = true
    playerState = { kind: 'fallback', codec, fps: 1 }
    snapshotPlayer = new SnapshotPlayer(image, response.data, (next) => {
      playerState =
        next.kind === 'running'
          ? { kind: 'fallback', codec, fps: next.fps }
          : { kind: 'failed' }
    })
    snapshotPlayer.start()
  }

  async function startRecording(): Promise<void> {
    if (!sessionId || recording) return
    recordingMessage = ''
    savedPath = ''
    const response = await api.startRecording(sessionId)
    if (response.data) {
      recording = true
    } else {
      recordingMessage = t(locale, 'record.failed')
    }
  }

  async function stopRecording(): Promise<void> {
    if (!sessionId || !recording) return
    recording = false
    const response = await api.stopRecording(sessionId)
    if (!response.data) {
      recordingMessage = t(locale, 'record.failed')
      return
    }
    const values = {
      path: response.data.path,
      size: number(response.data.bytes / 1_000_000)
    }
    recordingMessage = response.data.interrupted
      ? t(locale, 'record.savedInterrupted', values)
      : t(locale, 'record.saved', values)
    savedPath = response.data.path
    pathCopied = false
  }

  async function copySavedPath(): Promise<void> {
    pathCopied = await api.copyText(savedPath)
  }

  async function revealSavedPath(): Promise<void> {
    const response = await api.revealClip(savedPath)
    if (response.error) recordingMessage = t(locale, 'record.revealFailed')
  }

  async function stopPreview(): Promise<void> {
    if (recording) await stopRecording()
    stop()
  }

  function stopLive(): void {
    if (statsTimer) clearInterval(statsTimer)
    statsTimer = undefined
    stats = undefined
    player?.stop()
    player = undefined
    if (sessionId) void api.stopPreview(sessionId)
    sessionId = ''
  }

  function stop(): void {
    active = false
    recording = false
    stopLive()
    snapshotPlayer?.stop()
    snapshotPlayer = undefined
    showingSnapshots = false
    playerState = undefined
  }

  function statusText(state: PreviewState): string {
    switch (state.kind) {
      case 'fallback':
        return state.codec
          ? t(locale, 'live.fallback', {
              codec: state.codec,
              fps: number(state.fps)
            })
          : t(locale, 'live.fallbackNoMse', { fps: number(state.fps) })
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
      hidden={showingSnapshots}
      muted
      autoplay
      playsinline
      aria-label={t(locale, 'live.label', { profile: profileName })}
    ></video>
    <img
      bind:this={image}
      hidden={!showingSnapshots}
      alt={t(locale, 'live.fallbackLabel', { profile: profileName })}
    />
  </div>
  <div class="url-actions">
    {#if active}
      <button
        type="button"
        class="button button-secondary"
        onclick={() => void stopPreview()}>{t(locale, 'live.stop')}</button
      >
      {#if !showingSnapshots && (playerState?.kind === 'connecting' || playerState?.kind === 'playing')}
        {#if recording}
          <button
            type="button"
            class="button button-secondary"
            onclick={() => void stopRecording()}
            >{t(locale, 'record.stop')}</button
          >
          <span class="live-recording" role="status"
            >{t(locale, 'record.running')}</span
          >
        {:else}
          <button
            type="button"
            class="button button-secondary"
            onclick={() => void startRecording()}
            >{t(locale, 'record.start')}</button
          >
        {/if}
      {/if}
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
        <div>
          <dt>{t(locale, 'live.statReconnects')}</dt>
          <dd>{stats.reconnects}</dd>
        </div>
      </dl>
    {/if}
    {#if recordingMessage}
      <span class="live-status" role="status">{recordingMessage}</span>
    {/if}
    {#if savedPath}
      <button
        type="button"
        class="button button-secondary"
        onclick={() => void copySavedPath()}
        >{pathCopied
          ? t(locale, 'record.pathCopied')
          : t(locale, 'record.copyPath')}</button
      >
      <button
        type="button"
        class="button button-secondary"
        onclick={() => void revealSavedPath()}
        >{t(locale, 'record.reveal')}</button
      >
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
