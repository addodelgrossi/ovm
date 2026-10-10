<script lang="ts">
  import type { Device, MediaProfile } from '../lib/api'
  import { t } from '../lib/i18n'
  import type { AppState } from '../lib/state.svelte'
  import EncoderSettings from './settings/EncoderSettings.svelte'
  import ImageSettings from './settings/ImageSettings.svelte'
  import MaintenanceSettings from './settings/MaintenanceSettings.svelte'
  import NetworkSettings from './settings/NetworkSettings.svelte'
  import TimeSettings from './settings/TimeSettings.svelte'
  import UsersSettings from './settings/UsersSettings.svelte'

  let { model, device }: { model: AppState; device: Device } = $props()

  let profiles = $derived(
    model.profilesByDevice[device.id]?.data?.profiles ?? []
  )
  let chosen = $state('')
  let profileToken = $derived(
    profiles.some((profile) => profile.token === chosen)
      ? chosen
      : (profiles[0]?.token ?? '')
  )

  function profileLabel(profile: MediaProfile): string {
    return profile.name?.trim() || profile.token
  }
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
    <label class="field settings-profile">
      <span>{t(model.locale, 'encoder.profile')}</span>
      <select
        value={profileToken}
        onchange={(event) =>
          (chosen = (event.currentTarget as HTMLSelectElement).value)}
      >
        {#each profiles as profile (profile.token)}
          <option value={profile.token}>{profileLabel(profile)}</option>
        {/each}
      </select>
    </label>
    {#key `${device.id}:${profileToken}`}
      <EncoderSettings
        locale={model.locale}
        deviceId={device.id}
        {profileToken}
        onApplied={() => void model.loadProfiles(device.id, true)}
      />
      <ImageSettings
        locale={model.locale}
        deviceId={device.id}
        {profileToken}
      />
    {/key}
  {/if}

  {#key device.id}
    <TimeSettings locale={model.locale} deviceId={device.id} />
    <UsersSettings locale={model.locale} deviceId={device.id} />
    <NetworkSettings
      locale={model.locale}
      deviceId={device.id}
      onMoved={() => void model.refresh()}
    />
    <MaintenanceSettings locale={model.locale} deviceId={device.id} />
  {/key}
</section>
