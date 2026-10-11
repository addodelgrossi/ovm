export interface Command {
  id: string
  label: string
  hint?: string
  run: () => void
}

// filterCommands ranks commands whose label contains every query word,
// prefix matches first, keeping the original order otherwise.
export function filterCommands(commands: Command[], query: string): Command[] {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean)
  if (words.length === 0) return commands
  const scored: { command: Command; score: number; index: number }[] = []
  commands.forEach((command, index) => {
    const label = `${command.label} ${command.hint ?? ''}`.toLowerCase()
    if (!words.every((word) => label.includes(word))) return
    const score = label.startsWith(words[0])
      ? 0
      : label.includes(` ${words[0]}`)
        ? 1
        : 2
    scored.push({ command, score, index })
  })
  return scored
    .sort((a, b) => a.score - b.score || a.index - b.index)
    .map((entry) => entry.command)
}
