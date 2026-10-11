<script lang="ts">
  import type { Locale } from '../lib/api'
  import { filterCommands, type Command } from '../lib/commands'
  import { t } from '../lib/i18n'

  let {
    locale,
    commands,
    onClose
  }: { locale: Locale; commands: Command[]; onClose: () => void } = $props()

  let query = $state('')
  let active = $state(0)
  let input: HTMLInputElement
  let matches = $derived(filterCommands(commands, query))

  $effect(() => {
    const previous = document.activeElement as HTMLElement | null
    queueMicrotask(() => input?.focus())
    return () => previous?.focus()
  })

  $effect(() => {
    if (active >= matches.length) active = Math.max(0, matches.length - 1)
  })

  function run(command: Command | undefined): void {
    if (!command) return
    onClose()
    command.run()
  }

  function keydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault()
      onClose()
    } else if (event.key === 'ArrowDown') {
      event.preventDefault()
      active = Math.min(active + 1, matches.length - 1)
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      active = Math.max(active - 1, 0)
    } else if (event.key === 'Enter') {
      event.preventDefault()
      run(matches[active])
    }
  }
</script>

<div class="dialog-layer palette-layer">
  <button
    type="button"
    class="dialog-scrim"
    aria-label={t(locale, 'action.close')}
    onclick={onClose}
  ></button>
  <div
    class="dialog-panel palette"
    role="dialog"
    tabindex="-1"
    aria-modal="true"
    aria-label={t(locale, 'palette.title')}
  >
    <input
      bind:this={input}
      bind:value={query}
      type="text"
      role="combobox"
      aria-expanded="true"
      aria-controls="palette-list"
      aria-activedescendant={matches[active]
        ? `palette-${matches[active].id}`
        : undefined}
      aria-label={t(locale, 'palette.title')}
      placeholder={t(locale, 'palette.placeholder')}
      autocomplete="off"
      onkeydown={keydown}
      oninput={() => (active = 0)}
    />
    <ul id="palette-list" role="listbox">
      {#each matches as command, index (command.id)}
        <li
          id={`palette-${command.id}`}
          role="option"
          aria-selected={index === active}
          class:active={index === active}
        >
          <button
            type="button"
            tabindex="-1"
            onmousemove={() => (active = index)}
            onclick={() => run(command)}
          >
            <span>{command.label}</span>
            {#if command.hint}<small>{command.hint}</small>{/if}
          </button>
        </li>
      {:else}
        <li class="palette-empty">{t(locale, 'palette.empty')}</li>
      {/each}
    </ul>
  </div>
</div>
