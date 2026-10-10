<script lang="ts">
  import {
    api,
    type ImagingApplyResult,
    type ImagingRange,
    type ImagingSettings,
    type ImagingView,
    type Locale
  } from '../../lib/api'
  import { errorMessage, t, type TranslationKey } from '../../lib/i18n'
  import MutationResult from './MutationResult.svelte'

  let {
    locale,
    deviceId,
    profileToken
  }: { locale: Locale; deviceId: string; profileToken: string } = $props()

  type Slider = 'brightness' | 'contrast' | 'color_saturation' | 'sharpness'
  const sliders: { key: Slider; label: TranslationKey }[] = [
    { key: 'brightness', label: 'imaging.brightness' },
    { key: 'contrast', label: 'imaging.contrast' },
    { key: 'color_saturation', label: 'imaging.saturation' },
    { key: 'sharpness', label: 'imaging.sharpness' }
  ]

  let view = $state<ImagingView | undefined>()
  let form = $state<ImagingSettings>({})
  let loading = $state(false)
  let applying = $state(false)
  let failure = $state('')
  let result = $state<ImagingApplyResult | undefined>()

  $effect(() => {
    if (profileToken) void load(profileToken)
  })

  async function load(token: string): Promise<void> {
    loading = true
    failure = ''
    result = undefined
    const response = await api.readImaging(deviceId, token)
    loading = false
    view = response.data
    if (!response.data) {
      failure = response.error ? errorMessage(locale, response.error) : ''
      return
    }
    form = { ...response.data.settings }
  }

  function range(key: Slider): ImagingRange | undefined {
    return view?.options[key]
  }

  // Send only what differs from the camera, so untouched settings are not
  // rewritten.
  function changes(): ImagingSettings {
    const current = view?.settings ?? {}
    const out: ImagingSettings = {}
    for (const { key } of sliders) {
      if (form[key] !== undefined && form[key] !== current[key]) {
        out[key] = Number(form[key])
      }
    }
    if (form.ir_cut_filter && form.ir_cut_filter !== current.ir_cut_filter) {
      out.ir_cut_filter = form.ir_cut_filter
    }
    return out
  }

  async function apply(): Promise<void> {
    applying = true
    failure = ''
    result = undefined
    const response = await api.applyImaging(deviceId, profileToken, changes())
    applying = false
    if (response.error) failure = errorMessage(locale, response.error)
    if (response.data?.report) {
      result = response.data
      view = view ? { ...view, settings: { ...response.data.after } } : view
    }
  }
</script>

<section class="settings-section" aria-labelledby="imaging-heading">
  <h3 id="imaging-heading">{t(locale, 'imaging.title')}</h3>
  {#if loading}
    <p class="live-status" role="status">{t(locale, 'settings.loading')}</p>
  {:else if view}
    {#if view.options.ir_cut_modes && view.options.ir_cut_modes.length > 0}
      <fieldset class="segmented">
        <legend>{t(locale, 'imaging.irCut')}</legend>
        {#each view.options.ir_cut_modes as mode (mode)}
          <label>
            <input
              type="radio"
              name={`ircut-${profileToken}`}
              value={mode}
              bind:group={form.ir_cut_filter}
            />
            {t(locale, `imaging.mode.${mode}` as TranslationKey)}
          </label>
        {/each}
      </fieldset>
    {/if}
    <div class="settings-grid">
      {#each sliders as slider (slider.key)}
        {@const allowed = range(slider.key)}
        {#if allowed && form[slider.key] !== undefined}
          <label class="field">
            <span>{t(locale, slider.label)}: {form[slider.key]}</span>
            <input
              type="range"
              min={allowed.min}
              max={allowed.max}
              step="1"
              bind:value={form[slider.key]}
            />
          </label>
        {/if}
      {/each}
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
