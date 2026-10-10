# Synthetic media fixtures

`pattern-320x240-15fps.h265` is project-owned synthetic media: the color-bar
and moving-block pattern produced by `H264Source` (320x240, 15 fps), re-encoded
once to H.265 Main so tests and the demo can exercise H.265 without a camera.
It contains 30 pictures with an IDR every 15 pictures, no B-frames, an access
unit delimiter before every picture, and VPS/SPS/PPS repeated before each IDR.

It was generated with FFmpeg and libx265 from the synthetic H.264 stream:

```sh
ffmpeg -f h264 -r 15 -i synthetic-320x240.h264 -frames:v 30 -c:v libx265 \
  -preset ultrafast \
  -x265-params "aud=1:keyint=15:min-keyint=15:bframes=0:repeat-headers=1:scenecut=0" \
  -f hevc pattern-320x240-15fps.h265
```

The input stream is reproduced by dumping 45 access units from
`NewH264Source(H264Config{Width: 320, Height: 240, FPS: 15, GOP: 15})` in
Annex B form. No camera footage or third-party content is involved.
