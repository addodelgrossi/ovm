<script lang="ts">
  import type { Device, MediaProfile, Transport } from '../lib/api'
  import {
    errorMessage,
    mediaSourceLabel,
    operationLabel,
    supportLabel,
    t
  } from '../lib/i18n'
  import type { AppState } from '../lib/state.svelte'
  import CopyButton from './CopyButton.svelte'

  let { model, device }: { model: AppState; device: Device } = $props()

  let profileState = $derived(model.profilesByDevice[device.id])

  function profileName(profile: MediaProfile): string {
    return profile.name?.trim() || profile.token
  }

  function source(profile: MediaProfile): string {
    return mediaSourceLabel(model.locale, profile.source)
  }

  function operationSupported(
    profile: MediaProfile,
    operationName: string
  ): boolean {
    return (
      profile.operations?.find(
        (operation) => operation.operation === operationName
      )?.state === 'supported'
    )
  }

  function openVLC(profile: MediaProfile, transport: Transport): void {
    void model.openInVLC(device.id, profile.token, transport)
  }
</script>

<section class="view-stack" aria-labelledby="streams-heading">
  <header class="view-heading">
    <div>
      <p class="eyebrow">{t(model.locale, 'streams.eyebrow')}</p>
      <h2 id="streams-heading">{t(model.locale, 'streams.title')}</h2>
    </div>
    {#if profileState?.data}
      <div
        class="media-service-badges"
        aria-label={t(model.locale, 'streams.mediaServices')}
      >
        <span
          class="chip"
          data-support={profileState.data.media2_available
            ? 'supported'
            : 'unsupported'}
        >
          Media2 {profileState.data.media2_available
            ? t(model.locale, 'streams.mediaAvailable')
            : t(model.locale, 'streams.mediaUnavailable')}
        </span>
        <span
          class="chip"
          data-support={profileState.data.media1_available
            ? 'supported'
            : 'unsupported'}
        >
          Media1 {profileState.data.media1_available
            ? t(model.locale, 'streams.mediaAvailable')
            : t(model.locale, 'streams.mediaUnavailable')}
        </span>
      </div>
    {/if}
  </header>

  {#if model.capabilities.demo}
    <div class="notice notice-demo" role="note">
      <span class="badge badge-demo">{t(model.locale, 'demo.badge')}</span>
      <p>{t(model.locale, 'demo.claim')}</p>
    </div>
  {/if}

  {#if profileState?.loading && !profileState.data}
    <div class="panel loading-panel" role="status">
      <span class="spinner" aria-hidden="true"></span>
      {t(model.locale, 'streams.loading')}
    </div>
  {/if}

  {#if profileState?.error}
    <div class="notice notice-warning" role="alert">
      <strong>{t(model.locale, 'streams.errorTitle')}</strong>
      <span>{errorMessage(model.locale, profileState.error)}</span>
    </div>
  {/if}

  {#if profileState?.data?.profiles.length === 0}
    <div class="panel empty-detail">
      <span class="empty-icon" aria-hidden="true">⌁</span>
      <p>{t(model.locale, 'streams.empty')}</p>
    </div>
  {/if}

  {#if profileState?.data?.profiles.length}
    <div class="profile-list">
      {#each profileState.data.profiles as profile (profile.token)}
        <article class="panel profile-card">
          <header class="profile-header">
            <div>
              <p class="eyebrow">
                {t(model.locale, 'streams.provenance', {
                  source: source(profile)
                })}
              </p>
              <h3>
                {t(model.locale, 'streams.profile', {
                  name: profileName(profile)
                })}
              </h3>
              <code>{profile.token}</code>
            </div>
            {#if profile.has_fixed}
              <span class="chip">
                {profile.fixed
                  ? t(model.locale, 'streams.fixed')
                  : t(model.locale, 'streams.dynamic')}
              </span>
            {/if}
          </header>

          <div class="media-grid">
            <section aria-labelledby={`video-${profile.token}`}>
              <h4 id={`video-${profile.token}`}>
                {t(model.locale, 'streams.video')}
              </h4>
              {#if profile.video}
                <dl class="compact-definitions">
                  <div>
                    <dt>{t(model.locale, 'streams.codec')}</dt>
                    <dd>{profile.video.codec || '—'}</dd>
                  </div>
                  <div>
                    <dt>{t(model.locale, 'streams.resolution')}</dt>
                    <dd>
                      {profile.video.width && profile.video.height
                        ? `${profile.video.width} × ${profile.video.height}`
                        : '—'}
                    </dd>
                  </div>
                  <div>
                    <dt>{t(model.locale, 'streams.frameRate')}</dt>
                    <dd>
                      {profile.video.has_frame_rate &&
                      profile.video.frame_rate_fps !== undefined
                        ? t(model.locale, 'streams.frameRateValue', {
                            value: profile.video.frame_rate_fps
                          })
                        : '—'}
                    </dd>
                  </div>
                  <div>
                    <dt>{t(model.locale, 'streams.bitrate')}</dt>
                    <dd>
                      {profile.video.bitrate_kbps !== undefined
                        ? t(model.locale, 'streams.bitrateValue', {
                            value: profile.video.bitrate_kbps
                          })
                        : '—'}
                    </dd>
                  </div>
                  <div>
                    <dt>{t(model.locale, 'streams.gop')}</dt>
                    <dd>{profile.video.gop_length ?? '—'}</dd>
                  </div>
                  <div>
                    <dt>{t(model.locale, 'streams.encoderProfile')}</dt>
                    <dd>{profile.video.profile || '—'}</dd>
                  </div>
                </dl>
              {:else}
                <p class="muted">{t(model.locale, 'streams.noVideo')}</p>
              {/if}
            </section>

            <section aria-labelledby={`audio-${profile.token}`}>
              <h4 id={`audio-${profile.token}`}>
                {t(model.locale, 'streams.audio')}
              </h4>
              {#if profile.audio}
                <dl class="compact-definitions">
                  <div>
                    <dt>{t(model.locale, 'streams.codec')}</dt>
                    <dd>{profile.audio.codec || '—'}</dd>
                  </div>
                  <div>
                    <dt>{t(model.locale, 'streams.bitrate')}</dt>
                    <dd>
                      {profile.audio.bitrate_kbps !== undefined
                        ? t(model.locale, 'streams.bitrateValue', {
                            value: profile.audio.bitrate_kbps
                          })
                        : '—'}
                    </dd>
                  </div>
                  <div>
                    <dt>{t(model.locale, 'streams.sampleRate')}</dt>
                    <dd>
                      {profile.audio.sample_rate_khz !== undefined
                        ? t(model.locale, 'streams.sampleRateValue', {
                            value: profile.audio.sample_rate_khz
                          })
                        : '—'}
                    </dd>
                  </div>
                </dl>
              {:else}
                <p class="muted">{t(model.locale, 'streams.noAudio')}</p>
              {/if}
            </section>
          </div>

          <section
            class="operation-strip"
            aria-label={t(model.locale, 'streams.operations')}
          >
            {#each profile.operations ?? [] as operation (`${profile.token}-${operation.operation}`)}
              <span
                class="chip"
                data-support={operation.state}
                title={operation.detail}
              >
                {operation.fell_back
                  ? t(model.locale, 'streams.operationFallback', {
                      operation: operationLabel(
                        model.locale,
                        operation.operation
                      ),
                      state: supportLabel(model.locale, operation.state),
                      source: mediaSourceLabel(model.locale, operation.source)
                    })
                  : t(model.locale, 'streams.operationSource', {
                      operation: operationLabel(
                        model.locale,
                        operation.operation
                      ),
                      state: supportLabel(model.locale, operation.state),
                      source: mediaSourceLabel(model.locale, operation.source)
                    })}
              </span>
            {/each}
          </section>

          <section
            class="url-section"
            aria-labelledby={`urls-${profile.token}`}
          >
            <h4 id={`urls-${profile.token}`}>
              {t(model.locale, 'streams.urls')}
            </h4>
            <p class="safe-url-notice">
              {t(model.locale, 'streams.safeUrlNotice')}
            </p>

            {#if model.capabilities.demo}
              <div class="notice notice-demo">
                {t(model.locale, 'streams.vlcDemo')}
              </div>
            {:else}
              <div class="url-row">
                <div>
                  <strong>{t(model.locale, 'streams.tcp')}</strong>
                  <code
                    >{profile.stream?.tcp_uri ||
                      t(model.locale, 'streams.urlUnavailable')}</code
                  >
                </div>
                <div class="url-actions">
                  {#if profile.stream?.tcp_uri}
                    <CopyButton
                      locale={model.locale}
                      url={profile.stream.tcp_uri}
                      kind={t(model.locale, 'streams.tcp')}
                      profileName={profileName(profile)}
                    />
                    {#if model.capabilities.vlc_available && model.capabilities.rtsp_playback_available && operationSupported(profile, 'stream_tcp')}
                      <button
                        type="button"
                        class="button button-secondary"
                        onclick={() => openVLC(profile, 'tcp')}
                        >{t(model.locale, 'action.openVlc')}</button
                      >
                    {/if}
                  {/if}
                </div>
              </div>

              <div class="url-row">
                <div>
                  <strong>{t(model.locale, 'streams.udp')}</strong>
                  <code
                    >{profile.stream?.udp_uri ||
                      t(model.locale, 'streams.urlUnavailable')}</code
                  >
                </div>
                <div class="url-actions">
                  {#if profile.stream?.udp_uri}
                    <CopyButton
                      locale={model.locale}
                      url={profile.stream.udp_uri}
                      kind={t(model.locale, 'streams.udp')}
                      profileName={profileName(profile)}
                    />
                    {#if model.capabilities.vlc_available && model.capabilities.rtsp_playback_available && operationSupported(profile, 'stream_udp')}
                      <button
                        type="button"
                        class="button button-secondary"
                        onclick={() => openVLC(profile, 'udp')}
                        >{t(model.locale, 'action.openVlc')}</button
                      >
                    {/if}
                  {/if}
                </div>
              </div>

              <div class="url-row">
                <div>
                  <strong>{t(model.locale, 'streams.snapshot')}</strong>
                  <code
                    >{profile.snapshot?.uri ||
                      t(model.locale, 'streams.urlUnavailable')}</code
                  >
                </div>
                <div class="url-actions">
                  {#if profile.snapshot?.uri}
                    <CopyButton
                      locale={model.locale}
                      url={profile.snapshot.uri}
                      kind={t(model.locale, 'streams.snapshot')}
                      profileName={profileName(profile)}
                    />
                  {/if}
                </div>
              </div>

              {#if !model.capabilities.rtsp_playback_available}
                <p class="notice">
                  {t(model.locale, 'streams.playbackUnavailable')}
                </p>
              {:else if !model.capabilities.vlc_available}
                <p class="notice">
                  {t(model.locale, 'streams.vlcUnavailable')}
                </p>
              {/if}
            {/if}
          </section>
        </article>
      {/each}
    </div>
  {/if}
</section>
