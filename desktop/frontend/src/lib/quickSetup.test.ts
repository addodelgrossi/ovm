import { beforeEach, describe, expect, it } from 'vitest'
import type { EncoderView } from './api'
import {
  clearProgress,
  emptyProgress,
  h264Change,
  loadProgress,
  nextStep,
  saveProgress
} from './quickSetup'

const h265View: EncoderView = {
  profile_token: 'main',
  current: {
    configuration_token: 'enc0',
    source: 'media2',
    encoding: 'H265',
    width: 2560,
    height: 1440,
    frame_rate: 24,
    bitrate_kbps: 12000,
    gop_length: 200,
    codec_profile: 'Main'
  },
  codecs: [
    {
      encoding: 'H265',
      resolutions: [{ width: 2560, height: 1440 }],
      frame_rates: [25, 30],
      frame_rate_min: 1,
      frame_rate_max: 30,
      bitrate_min: 256,
      bitrate_max: 16000,
      gop_min: 1,
      gop_max: 250,
      profiles: ['Main']
    },
    {
      encoding: 'H264',
      resolutions: [
        { width: 1280, height: 720 },
        { width: 1920, height: 1080 }
      ],
      frame_rates: [15, 25, 30],
      frame_rate_min: 1,
      frame_rate_max: 30,
      bitrate_min: 256,
      bitrate_max: 8192,
      gop_min: 1,
      gop_max: 120,
      profiles: ['Baseline', 'Main', 'High']
    }
  ]
}

describe('h264Change', () => {
  it('keeps what H.264 allows and clamps the rest', () => {
    expect(h264Change(h265View)).toEqual({
      encoding: 'H264',
      width: 1920,
      height: 1080,
      frame_rate: 25,
      bitrate_kbps: 8192,
      gop_length: 120,
      codec_profile: 'High'
    })
  })

  it('does nothing for an H.264 encoder or a camera without H.264', () => {
    expect(
      h264Change({
        ...h265View,
        current: { ...h265View.current, encoding: 'H264' }
      })
    ).toBeUndefined()
    expect(
      h264Change({ ...h265View, codecs: [h265View.codecs![0]] })
    ).toBeUndefined()
  })
})

describe('quick setup progress', () => {
  beforeEach(() => localStorage.clear())

  it('resumes at the first pending step', () => {
    const progress = emptyProgress()
    progress.password = 'done'
    progress.h264 = 'skipped'
    saveProgress('device-1', progress)
    const restored = loadProgress('device-1')
    expect(restored).toEqual(progress)
    expect(nextStep(restored)).toBe('day')
    expect(nextStep(loadProgress('device-2'))).toBe('password')
    clearProgress('device-1')
    expect(nextStep(loadProgress('device-1'))).toBe('password')
  })

  it('ignores corrupt stored state', () => {
    localStorage.setItem('ovm.quickSetup.device-1', '{"password":"hacked"')
    expect(loadProgress('device-1')).toEqual(emptyProgress())
    localStorage.setItem('ovm.quickSetup.device-1', '{"password":"maybe"}')
    expect(loadProgress('device-1').password).toBe('pending')
  })
})
