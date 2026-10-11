import type { EncoderChange, EncoderView } from './api'

export type QuickSetupStep = 'password' | 'h264' | 'day' | 'ntp'
export type StepStatus = 'pending' | 'done' | 'skipped'
export type QuickSetupProgress = Record<QuickSetupStep, StepStatus>

export const quickSetupSteps: QuickSetupStep[] = [
  'password',
  'h264',
  'day',
  'ntp'
]

const storagePrefix = 'ovm.quickSetup.'

export function emptyProgress(): QuickSetupProgress {
  return {
    password: 'pending',
    h264: 'pending',
    day: 'pending',
    ntp: 'pending'
  }
}

function storage(): Storage | undefined {
  try {
    return globalThis.localStorage
  } catch {
    return undefined
  }
}

// loadProgress restores a device's wizard state so it resumes where the
// technician left it. Unknown or corrupt values fall back to pending.
export function loadProgress(deviceId: string): QuickSetupProgress {
  const progress = emptyProgress()
  try {
    const raw = storage()?.getItem(storagePrefix + deviceId)
    if (!raw) return progress
    const saved = JSON.parse(raw) as Record<string, unknown>
    for (const step of quickSetupSteps) {
      const value = saved[step]
      if (value === 'done' || value === 'skipped') progress[step] = value
    }
  } catch {
    // A missing or unreadable store starts from the beginning.
  }
  return progress
}

export function saveProgress(
  deviceId: string,
  progress: QuickSetupProgress
): void {
  try {
    storage()?.setItem(storagePrefix + deviceId, JSON.stringify(progress))
  } catch {
    // Progress is a convenience; the camera state is the source of truth.
  }
}

export function clearProgress(deviceId: string): void {
  try {
    storage()?.removeItem(storagePrefix + deviceId)
  } catch {
    // Nothing to clear.
  }
}

export function nextStep(
  progress: QuickSetupProgress
): QuickSetupStep | undefined {
  return quickSetupSteps.find((step) => progress[step] === 'pending')
}

function clamp(value: number, min: number, max: number): number {
  if (max > 0 && value > max) return max
  if (value < min) return min
  return value
}

// h264Change keeps as much of the current encoder as the camera's H.264
// options allow. It returns undefined when the encoder is already H.264 or
// the camera offers no H.264.
export function h264Change(view: EncoderView): EncoderChange | undefined {
  const current = view.current
  if (current.encoding.toUpperCase() === 'H264') return undefined
  const options = (view.codecs ?? []).find(
    (codec) => codec.encoding.toUpperCase() === 'H264'
  )
  if (!options) return undefined

  const resolutions = options.resolutions ?? []
  const sameSize = resolutions.find(
    (r) => r.width === current.width && r.height === current.height
  )
  const largest = [...resolutions].sort(
    (a, b) => b.width * b.height - a.width * a.height
  )[0]
  const size = sameSize ??
    largest ?? { width: current.width, height: current.height }

  const rates = options.frame_rates ?? []
  let frameRate = clamp(
    current.frame_rate,
    options.frame_rate_min,
    options.frame_rate_max
  )
  if (rates.length > 0 && !rates.includes(frameRate)) {
    frameRate = [...rates].sort(
      (a, b) =>
        Math.abs(a - current.frame_rate) - Math.abs(b - current.frame_rate)
    )[0]
  }

  const profiles = options.profiles ?? []
  const codecProfile =
    ['High', 'Main', 'Baseline'].find((p) => profiles.includes(p)) ??
    profiles[0] ??
    ''

  return {
    encoding: 'H264',
    width: size.width,
    height: size.height,
    frame_rate: frameRate,
    bitrate_kbps: clamp(
      current.bitrate_kbps,
      options.bitrate_min,
      options.bitrate_max
    ),
    gop_length: clamp(current.gop_length, options.gop_min, options.gop_max),
    codec_profile: codecProfile
  }
}
