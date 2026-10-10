<script lang="ts">
  import {
    api,
    type EncoderApplyResult,
    type EncoderChange,
    type EncoderCodecOptions,
    type EncoderView,
    type Locale,
    type MediaProfile
  } from '../../lib/api'
  import { errorMessage, t } from '../../lib/i18n'
  import MutationResult from './MutationResult.svelte'

  let {
    locale,
    deviceId,
    profiles,
    onApplied
  }: {
    locale: Locale
    deviceId: string
    profiles: MediaProfile[]
    onApplied: () => void
  } = $props()

  let profileToken = $state('')
  let view = $state<EncoderView | undefined>()
  let loading = $state(false)
  let applying = $state(false)
  let failure = $state('')
  let result = $state<EncoderApplyResult | undefined>()
  let form = $state<EncoderChange>({
    encoding: '',
    width: 0,
    height: 0,
    frame_rate: 0,
    bitrate_kbps: 0,
    gop_length: 0,
    codec_profile: ''
  })

  let codec = $derived<EncoderCodecOptions | undefined>(
    view?.codecs?.find((option) => option.encoding === form.encoding)
  )
  let resolutionKey = $derived(`${form.width}x${form.height}`)

  $effect(() => {
    if (!profileToken && profiles.length > 0) profileToken = profiles[0].token
  })

  $effect(() => {
    if (profileToken) void load(profileToken)
  })

  async function load(token: string): Promise<void> {
    loading = true
    failure = ''
    result = undefined
    const response = await api.readEncoder(deviceId, token)
    loading = false
    if (!response.data) {
      view = undefined
      failure = response.error ? errorMessage(locale, response.error) : ''
      return
    }
    view = response.data
    const current = response.data.current
    form = {
      encoding: current.encoding,
      width: current.width,
      height: current.height,
      frame_rate: current.frame_rate,
      bitrate_kbps: current.bitrate_kbps,
      gop_length: current.gop_length,
      codec_profile: current.codec_profile
    }
  }

  function chooseCodec(encoding: string): void {
    const next = view?.codecs?.find((option) => option.encoding === encoding)
    form.encoding = encoding
    if (!next) return
    const resolutions = next.resolutions ?? []
    if (
      !resolutions.some(
        (item) => item.width === form.width && item.height === form.height
      ) &&
      resolutions[0]
    ) {
      form.width = resolutions[0].width
      form.height = resolutions[0].height
    }
    const rates = next.frame_rates ?? []
    if (rates.length > 0 && !rates.includes(form.frame_rate)) {
      form.frame_rate = rates[0]
    }
    const codecProfiles = next.profiles ?? []
    if (
      codecProfiles.length > 0 &&
      !codecProfiles.includes(form.codec_profile)
    ) {
      form.codec_profile = codecProfiles[0]
    }
  }

  function chooseResolution(key: string): void {
    const [width, height] = key.split('x').map(Number)
    form.width = width
    form.height = height
  }

  async function apply(): Promise<void> {
    applying = true
    failure = ''
    result = undefined
    const response = await api.applyEncoder(deviceId, profileToken, {
      ...form
    })
    applying = false
    if (response.error) failure = errorMessage(locale, response.error)
    if (response.data?.report) {
      result = response.data
      onApplied()
    }
  }

  function profileLabel(profile: MediaProfile): string {
    return profile.name?.trim() || profile.token
  }
</script>

<section class="settings-section" aria-labelledby="encoder-heading">
  <h3 id="encoder-heading">{t(locale, 'encoder.title')}</h3>
  <label class="field">
    <span>{t(locale, 'encoder.profile')}</span>
    <select bind:value={profileToken}>
      {#each profiles as profile (profile.token)}
        <option value={profile.token}>{profileLabel(profile)}</option>
      {/each}
    </select>
  </label>

  {#if loading}
    <p class="live-status" role="status">{t(locale, 'settings.loading')}</p>
  {:else if view}
    <p class="safe-url-notice">
      {t(locale, 'encoder.source', { source: view.current.source })}
    </p>
    <div class="settings-grid">
      <label class="field">
        <span>{t(locale, 'encoder.codec')}</span>
        <select
          value={form.encoding}
          onchange={(event) =>
            chooseCodec((event.currentTarget as HTMLSelectElement).value)}
        >
          {#each view.codecs ?? [] as option (option.encoding)}
            <option value={option.encoding}>{option.encoding}</option>
          {/each}
        </select>
      </label>
      <label class="field">
        <span>{t(locale, 'encoder.resolution')}</span>
        <select
          value={resolutionKey}
          onchange={(event) =>
            chooseResolution((event.currentTarget as HTMLSelectElement).value)}
        >
          {#each codec?.resolutions ?? [] as resolution (`${resolution.width}x${resolution.height}`)}
            <option value={`${resolution.width}x${resolution.height}`}
              >{resolution.width} × {resolution.height}</option
            >
          {/each}
        </select>
      </label>
      <label class="field">
        <span>{t(locale, 'encoder.frameRate')}</span>
        {#if codec?.frame_rates && codec.frame_rates.length > 0}
          <select bind:value={form.frame_rate}>
            {#each codec.frame_rates as rate (rate)}
              <option value={rate}>{rate}</option>
            {/each}
          </select>
        {:else}
          <input
            type="number"
            min={codec?.frame_rate_min}
            max={codec?.frame_rate_max}
            bind:value={form.frame_rate}
          />
        {/if}
      </label>
      <label class="field">
        <span>{t(locale, 'encoder.bitrate')}</span>
        <input
          type="number"
          min={codec?.bitrate_min}
          max={codec?.bitrate_max}
          bind:value={form.bitrate_kbps}
        />
        {#if codec && codec.bitrate_max < 2147483647}
          <small
            >{t(locale, 'encoder.bitrateRange', {
              min: String(codec.bitrate_min),
              max: String(codec.bitrate_max)
            })}</small
          >
        {/if}
      </label>
      {#if codec && codec.gop_max > 0}
        <label class="field">
          <span>{t(locale, 'encoder.gop')}</span>
          <input
            type="number"
            min={codec.gop_min}
            max={codec.gop_max}
            bind:value={form.gop_length}
          />
          <small
            >{t(locale, 'encoder.gopRange', {
              min: String(codec.gop_min),
              max: String(codec.gop_max)
            })}</small
          >
        </label>
      {/if}
      {#if codec?.profiles && codec.profiles.length > 0}
        <label class="field">
          <span>{t(locale, 'encoder.codecProfile')}</span>
          <select bind:value={form.codec_profile}>
            {#each codec.profiles as option (option)}
              <option value={option}>{option}</option>
            {/each}
          </select>
        </label>
      {/if}
    </div>
    <div class="url-actions">
      <button
        type="button"
        class="button button-primary"
        disabled={applying}
        onclick={() => void apply()}>{t(locale, 'settings.apply')}</button
      >
      {#if applying}
        <span class="live-status" role="status"
          >{t(locale, 'settings.applying')}</span
        >
      {/if}
    </div>
  {/if}
  {#if failure}
    <p class="notice" role="alert">{failure}</p>
  {/if}
  {#if result}
    <MutationResult {locale} report={result.report} />
  {/if}
</section>
