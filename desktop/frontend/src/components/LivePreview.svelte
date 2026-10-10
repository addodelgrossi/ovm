<script lang="ts">
  import { onDestroy } from 'svelte'
  import { api, type Locale } from '../lib/api'
  import { t } from '../lib/i18n'
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
  let player: LivePlayer | undefined
  let sessionId = ''

  async function start(): Promise<void> {
    active = true
    playerState = { kind: 'connecting' }
    const response = await api.startPreview(deviceId, profileToken)
    if (!active) {
      if (response.data) void api.stopPreview(response.data.session_id)
      return
    }
    if (!response.data || !video) {
      playerState = { kind: 'failed' }
      return
    }
    sessionId = response.data.session_id
    player = new LivePlayer(video, response.data, (next) => {
      playerState = next
    })
    player.start()
  }

  function stop(): void {
    active = false
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
        return t(locale, 'live.failed')
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
