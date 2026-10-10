import type { PreviewSession } from './api'

export type LivePlayerState =
  | { kind: 'connecting' }
  | { kind: 'playing' }
  | { kind: 'ended' }
  | { kind: 'failed' }
  | { kind: 'mse_unavailable' }
  | { kind: 'codec_unsupported'; codec: string }

type MediaSourceConstructor = {
  new (): MediaSource
  isTypeSupported(type: string): boolean
}

// Keep a short window behind the live edge so memory stays bounded.
const retainedSeconds = 10
// Jump forward when playback falls this far behind the newest frame.
const maxLatencySeconds = 1.5
const liveEdgeOffsetSeconds = 0.2

/**
 * Chooses the MSE implementation. WebKit 17+ exposes ManagedMediaSource and
 * may omit MediaSource; older WebKit and Chromium expose MediaSource.
 */
export function mediaSourceConstructor(
  scope: Record<string, unknown> = window as unknown as Record<string, unknown>
): MediaSourceConstructor | undefined {
  const candidate = (scope.ManagedMediaSource ?? scope.MediaSource) as
    MediaSourceConstructor | undefined
  return typeof candidate === 'function' ? candidate : undefined
}

/**
 * Plays a loopback fragmented-MP4 preview in a video element through Media
 * Source Extensions. The response is a continuous byte stream: the init
 * segment followed by moof+mdat fragments, appended as they arrive.
 */
export class LivePlayer {
  private readonly abort = new AbortController()
  private objectURL = ''
  private queue: Uint8Array[] = []
  private buffer?: SourceBuffer
  private stopped = false

  constructor(
    private readonly video: HTMLVideoElement,
    private readonly session: PreviewSession,
    private readonly onState: (state: LivePlayerState) => void,
    private readonly fetchImpl: typeof fetch = fetch.bind(window)
  ) {}

  start(): void {
    const Source = mediaSourceConstructor()
    if (!Source) {
      this.onState({ kind: 'mse_unavailable' })
      return
    }
    this.onState({ kind: 'connecting' })
    const mediaSource = new Source()
    // ManagedMediaSource requires remote playback to be disabled or offered.
    this.video.disableRemotePlayback = true
    this.objectURL = URL.createObjectURL(mediaSource)
    this.video.src = this.objectURL
    mediaSource.addEventListener(
      'sourceopen',
      () => void this.run(Source, mediaSource),
      { once: true }
    )
  }

  stop(): void {
    if (this.stopped) return
    this.stopped = true
    this.abort.abort()
    this.video.removeAttribute('src')
    this.video.load()
    if (this.objectURL) URL.revokeObjectURL(this.objectURL)
  }

  private async run(
    Source: MediaSourceConstructor,
    mediaSource: MediaSource
  ): Promise<void> {
    try {
      const response = await this.fetchImpl(this.session.url, {
        headers: { Authorization: `Bearer ${this.session.token}` },
        signal: this.abort.signal,
        cache: 'no-store'
      })
      const codec = response.headers.get('X-OVM-Codec') ?? ''
      if (!response.ok || !response.body || !codec) {
        this.finish('failed')
        return
      }
      const mime = `video/mp4; codecs="${codec}"`
      if (!Source.isTypeSupported(mime)) {
        this.onState({ kind: 'codec_unsupported', codec })
        this.stop()
        return
      }
      const buffer = mediaSource.addSourceBuffer(mime)
      buffer.mode = 'segments'
      buffer.addEventListener('updateend', () => this.drain())
      this.buffer = buffer

      const reader = response.body.getReader()
      let announced = false
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        this.queue.push(value)
        this.drain()
        if (!announced && buffer.buffered.length > 0) {
          announced = true
          this.onState({ kind: 'playing' })
        }
      }
      this.finish('ended')
    } catch {
      this.finish(this.abort.signal.aborted ? 'ended' : 'failed')
    }
  }

  private finish(kind: 'ended' | 'failed'): void {
    if (this.stopped) return
    this.onState({ kind })
    this.stop()
  }

  private drain(): void {
    const buffer = this.buffer
    if (!buffer || buffer.updating || this.stopped) return
    if (this.queue.length > 0) {
      const chunk = this.queue.length === 1 ? this.queue[0] : concat(this.queue)
      this.queue = []
      try {
        buffer.appendBuffer(chunk as BufferSource)
      } catch {
        this.finish('failed')
      }
      return
    }
    this.followLiveEdge(buffer)
  }

  private followLiveEdge(buffer: SourceBuffer): void {
    const ranges = buffer.buffered
    if (ranges.length === 0) return
    const end = ranges.end(ranges.length - 1)
    const start = ranges.start(0)
    if (end - this.video.currentTime > maxLatencySeconds) {
      this.video.currentTime = Math.max(start, end - liveEdgeOffsetSeconds)
    }
    if (this.video.paused) void this.video.play().catch(() => undefined)
    if (this.video.currentTime - start > retainedSeconds * 2) {
      try {
        buffer.remove(start, this.video.currentTime - retainedSeconds)
      } catch {
        // A failed trim only costs memory; playback continues.
      }
    }
  }
}

function concat(chunks: Uint8Array[]): Uint8Array {
  const total = chunks.reduce((sum, chunk) => sum + chunk.byteLength, 0)
  const out = new Uint8Array(total)
  let offset = 0
  for (const chunk of chunks) {
    out.set(chunk, offset)
    offset += chunk.byteLength
  }
  return out
}
