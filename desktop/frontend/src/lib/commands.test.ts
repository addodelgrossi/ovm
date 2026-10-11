import { describe, expect, it } from 'vitest'
import { filterCommands, type Command } from './commands'

const command = (id: string, label: string, hint?: string): Command => ({
  id,
  label,
  hint,
  run: () => {}
})

describe('filterCommands', () => {
  const commands = [
    command('settings', 'Go to Settings'),
    command('report', 'Save device report'),
    command('scan', 'Scan the network', 'discover'),
    command('streams', 'Go to Streams')
  ]

  it('returns everything for an empty query', () => {
    expect(filterCommands(commands, '  ')).toHaveLength(4)
  })

  it('matches all words, hints included, and ranks prefixes first', () => {
    expect(filterCommands(commands, 'go st').map((c) => c.id)).toEqual([
      'streams'
    ])
    expect(filterCommands(commands, 'discover').map((c) => c.id)).toEqual([
      'scan'
    ])
    expect(filterCommands(commands, 's').map((c) => c.id)).toEqual([
      'report',
      'scan',
      'settings',
      'streams'
    ])
  })
})
