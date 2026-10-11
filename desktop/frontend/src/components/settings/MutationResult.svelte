<script lang="ts">
  import type { Locale, MutationReport } from '../../lib/api'
  import { t, type TranslationKey } from '../../lib/i18n'

  let { locale, report }: { locale: Locale; report: MutationReport } = $props()

  const outcomeKey = $derived(
    `settings.outcome.${report.outcome}` as TranslationKey
  )
</script>

<div class="mutation-result" data-outcome={report.outcome}>
  <p role="status">{t(locale, outcomeKey)}</p>
  {#if report.changes && report.changes.length > 0}
    <table aria-label={t(locale, 'settings.changesLabel')}>
      <thead>
        <tr>
          <th scope="col">{t(locale, 'settings.field')}</th>
          <th scope="col">{t(locale, 'settings.before')}</th>
          <th scope="col">{t(locale, 'settings.requested')}</th>
          <th scope="col">{t(locale, 'settings.after')}</th>
        </tr>
      </thead>
      <tbody>
        {#each report.changes as change (change.field)}
          <tr data-matches={change.after === change.requested}>
            <th scope="row">{change.field}</th>
            <td>{change.before}</td>
            <td>{change.requested}</td>
            <td>{change.after}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>
