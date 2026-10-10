import { describe, expect, it } from 'vitest'
import { nextSnapshotInterval } from './snapshotPlayer'

describe('nextSnapshotInterval', () => {
  it('adapts between 5 and 1 frames per second', () => {
    expect(nextSnapshotInterval(10)).toBe(200)
    expect(nextSnapshotInterval(150)).toBe(300)
    expect(nextSnapshotInterval(900)).toBe(1000)
  })
})
