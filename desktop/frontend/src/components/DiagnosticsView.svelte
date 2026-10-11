<script lang="ts">
  import type { Device, MediaProfile } from '../lib/api'
  import { t } from '../lib/i18n'
  import type { AppState } from '../lib/state.svelte'
  import ClockCheck from './diagnostics/ClockCheck.svelte'
  import EventsFeed from './diagnostics/EventsFeed.svelte'
  import ReportExport from './diagnostics/ReportExport.svelte'
  import SoapConsole from './diagnostics/SoapConsole.svelte'
  import StabilityTest from './diagnostics/StabilityTest.svelte'

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

<section class="view-stack" aria-labelledby="diagnostics-heading">
  <header class="view-heading">
    <div>
      <p class="eyebrow">{t(model.locale, 'diagnostics.eyebrow')}</p>
      <h2 id="diagnostics-heading">{t(model.locale, 'diagnostics.title')}</h2>
    </div>
  </header>

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
  {/if}

  {#key device.id}
    <ReportExport locale={model.locale} deviceId={device.id} />
    <ClockCheck locale={model.locale} deviceId={device.id} {profileToken} />
    <StabilityTest locale={model.locale} deviceId={device.id} {profileToken} />
    <EventsFeed locale={model.locale} deviceId={device.id} />
  {/key}
  <SoapConsole locale={model.locale} />
</section>
