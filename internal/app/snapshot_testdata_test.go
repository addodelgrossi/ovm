package app

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// projectJPEGBytes returns deterministic, project-owned JPEG test data produced
// by the standard library. Success tests must use a fully decodable image rather
// than marker-only pseudo-JPEG bytes.
func projectJPEGBytes() []byte {
	imageData := image.NewGray(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			imageData.SetGray(x, y, color.Gray{Y: uint8((x + y) * 16)})
		}
	}
	var output bytes.Buffer
	if err := jpeg.Encode(&output, imageData, &jpeg.Options{Quality: 90}); err != nil {
		panic("encode project JPEG fixture: " + err.Error())
	}
	return output.Bytes()
}

func projectPNGBytes() []byte {
	imageData := image.NewGray(image.Rect(0, 0, 2, 2))
	var output bytes.Buffer
	if err := png.Encode(&output, imageData); err != nil {
		panic("encode project PNG fixture: " + err.Error())
	}
	return output.Bytes()
}

// projectJPEGWithDimensions rewrites the baseline SOF dimensions in the small
// project JPEG without allocating an image of the requested size. It lets tests
// prove oversized dimensions are rejected after DecodeConfig and before a full
// decoder allocation.
func projectJPEGWithDimensions(t *testing.T, width, height int) []byte {
	t.Helper()
	if width <= 0 || width > 0xffff || height <= 0 || height > 0xffff {
		t.Fatalf("test JPEG dimensions out of uint16 range: %dx%d", width, height)
	}
	data := projectJPEGBytes()
	for index := 0; index+8 < len(data); index++ {
		if data[index] != 0xff || data[index+1] != 0xc0 {
			continue
		}
		// The range guard above proves both conversions are lossless.
		width16 := uint16(width)   //nolint:gosec // Explicitly range-checked above.
		height16 := uint16(height) //nolint:gosec // Explicitly range-checked above.
		binary.BigEndian.PutUint16(data[index+5:index+7], height16)
		binary.BigEndian.PutUint16(data[index+7:index+9], width16)
		return data
	}
	t.Fatal("project JPEG has no baseline SOF marker")
	return nil
}
