<script lang="ts">
  import type { Device } from '../lib/api'
  import { t } from '../lib/i18n'
  import type { AppState } from '../lib/state.svelte'
  import EncoderSettings from './settings/EncoderSettings.svelte'

  let { model, device }: { model: AppState; device: Device } = $props()

  let profiles = $derived(
    model.profilesByDevice[device.id]?.data?.profiles ?? []
  )
</script>

<section class="view-stack" aria-labelledby="settings-heading">
  <header class="view-heading">
    <div>
      <p class="eyebrow">{t(model.locale, 'settings.eyebrow')}</p>
      <h2 id="settings-heading">{t(model.locale, 'settings.title')}</h2>
    </div>
  </header>
  <p class="safe-url-notice">{t(model.locale, 'settings.verifiedNotice')}</p>

  {#if profiles.length > 0}
    {#key device.id}
      <EncoderSettings
        locale={model.locale}
        deviceId={device.id}
        {profiles}
        onApplied={() => void model.loadProfiles(device.id, true)}
      />
    {/key}
  {/if}
</section>
