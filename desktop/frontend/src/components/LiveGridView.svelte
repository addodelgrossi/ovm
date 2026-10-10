<script lang="ts">
  import { onDestroy } from 'svelte'
  import { api } from '../lib/api'
  import { t } from '../lib/i18n'
  import type { AppState } from '../lib/state.svelte'
  import LivePreview from './LivePreview.svelte'

  let { model }: { model: AppState } = $props()

  interface StreamOption {
    key: string
    deviceId: string
    profileToken: string
    label: string
    profileName: string
  }

  const positions = [0, 1, 2, 3]
  let selections = $state<string[]>(['', '', '', ''])
  let expanded = $state<number | undefined>()

  let options = $derived.by((): StreamOption[] => {
    const out: StreamOption[] = []
    for (const device of model.overview.devices) {
      const profiles = model.profilesByDevice[device.id]?.data?.profiles ?? []
      for (const profile of profiles) {
        if (!profile.stream?.tcp_uri) continue
        const profileName = profile.name?.trim() || profile.token
        out.push({
          key: `${device.id}\u0000${profile.token}`,
          deviceId: device.id,
          profileToken: profile.token,
          profileName,
          label: t(model.locale, 'grid.option', {
            device: device.display_name || device.endpoint,
            profile: profileName
          })
        })
      }
    }
    return out
  })

  function optionFor(key: string): StreamOption | undefined {
    return options.find((option) => option.key === key)
  }

  function choose(index: number, event: Event): void {
    const value = (event.currentTarget as HTMLSelectElement).value
    selections = selections.map((key, i) => (i === index ? value : key))
  }

  function expand(index: number): void {
    expanded = index
    api.setFullscreen(true)
  }

  function collapse(): void {
    if (expanded === undefined) return
    expanded = undefined
    api.setFullscreen(false)
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') collapse()
  }

  onDestroy(collapse)
</script>

<svelte:window onkeydown={onKeydown} />

<section class="view-stack" aria-labelledby="grid-heading">
  <header class="view-heading">
    <div>
      <p class="eyebrow">{t(model.locale, 'grid.eyebrow')}</p>
      <h2 id="grid-heading">{t(model.locale, 'grid.title')}</h2>
    </div>
  </header>
  <p class="safe-url-notice">{t(model.locale, 'grid.hint')}</p>

  <div class="live-grid">
    {#each positions as index (index)}
      {@const selected = optionFor(selections[index])}
      <article
        class="grid-tile"
        class:grid-tile-expanded={expanded === index}
        aria-label={t(model.locale, 'grid.tile', { index: String(index + 1) })}
      >
        <div class="grid-tile-controls">
          <select
            aria-label={t(model.locale, 'grid.choose', {
              index: String(index + 1)
            })}
            value={selections[index]}
            onchange={(event) => choose(index, event)}
          >
            <option value="">{t(model.locale, 'grid.none')}</option>
            {#each options as option (option.key)}
              <option value={option.key}>{option.label}</option>
            {/each}
          </select>
          {#if selected}
            {#if expanded === index}
              <button
                type="button"
                class="button button-secondary"
                onclick={collapse}>{t(model.locale, 'grid.collapse')}</button
              >
            {:else}
              <button
                type="button"
                class="button button-secondary"
                onclick={() => expand(index)}
                >{t(model.locale, 'grid.expand')}</button
              >
            {/if}
          {/if}
        </div>
        {#if selected}
          {#key selected.key}
            <LivePreview
              locale={model.locale}
              deviceId={selected.deviceId}
              profileToken={selected.profileToken}
              profileName={selected.profileName}
            />
          {/key}
        {/if}
      </article>
    {/each}
  </div>
</section>
