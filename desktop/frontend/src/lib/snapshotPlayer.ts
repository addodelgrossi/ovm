import type { PreviewSession } from './api'

export type SnapshotPlayerState =
  { kind: 'running'; fps: number } | { kind: 'failed' }

// The fallback adapts between 1 and 5 frames per second.
const minIntervalMs = 200
const maxIntervalMs = 1000
// Give up after this many consecutive failures.
const maxConsecutiveFailures = 3

/**
 * Chooses the next polling interval: twice the last fetch time, bounded to
 * 1-5 fps, so a slow camera or network is never flooded.
 */
export function nextSnapshotInterval(fetchMs: number): number {
  return Math.min(maxIntervalMs, Math.max(minIntervalMs, fetchMs * 2))
}

/**
 * Polls authenticated JPEG snapshots from the loopback bridge into an image
 * element when the WebView cannot decode the live codec.
 */
export class SnapshotPlayer {
  private readonly abort = new AbortController()
  private objectURL = ''
  private stopped = false
  private timer: ReturnType<typeof setTimeout> | undefined

  constructor(
    private readonly image: HTMLImageElement,
    private readonly session: PreviewSession,
    private readonly onState: (state: SnapshotPlayerState) => void,
    private readonly fetchImpl: typeof fetch = fetch.bind(window),
    private readonly now: () => number = () => performance.now()
  ) {}

  start(): void {
    void this.tick(0)
  }

  stop(): void {
    if (this.stopped) return
    this.stopped = true
    this.abort.abort()
    if (this.timer) clearTimeout(this.timer)
    if (this.objectURL) URL.revokeObjectURL(this.objectURL)
    this.objectURL = ''
  }

  private async tick(failures: number): Promise<void> {
    if (this.stopped) return
    const started = this.now()
    let ok = false
    try {
      const response = await this.fetchImpl(this.session.url, {
        headers: { Authorization: `Bearer ${this.session.token}` },
        signal: this.abort.signal,
        cache: 'no-store'
      })
      if (response.ok) {
        const blob = await response.blob()
        if (this.stopped) return
        const previous = this.objectURL
        this.objectURL = URL.createObjectURL(blob)
        this.image.src = this.objectURL
        if (previous) URL.revokeObjectURL(previous)
        ok = true
      }
    } catch {
      if (this.stopped) return
    }
    const interval = nextSnapshotInterval(this.now() - started)
    if (ok) {
      this.onState({ kind: 'running', fps: 1000 / interval })
    } else if (failures + 1 >= maxConsecutiveFailures) {
      this.onState({ kind: 'failed' })
      this.stop()
      return
    }
    this.timer = setTimeout(
      () => void this.tick(ok ? 0 : failures + 1),
      ok ? interval : maxIntervalMs
    )
  }
}
