// Package synthmedia generates project-owned synthetic media for tests and the
// desktop demo. Its H.264 source is a minimal Constrained Baseline encoder that
// needs no codec library: IDR pictures are coded entirely with I_PCM
// macroblocks, and P pictures repaint only the macroblocks a moving block
// touched, skipping the rest. The output is valid, decodable H.264, but it is
// not representative of camera compression efficiency.
package synthmedia

import (
	"errors"
	"time"
)

const (
	naluTypeNonIDR = 1
	naluTypeIDR    = 5
	naluTypeSPS    = 7
	naluTypePPS    = 8

	// log2MaxFrameNum is log2_max_frame_num_minus4 + 4 in the SPS.
	log2MaxFrameNum = 4

	// I_PCM mb_type values for I and P slices (H.264 Tables 7-11 and 7-13).
	mbTypeIPCMInI = 25
	mbTypeIPCMInP = 30

	blockMBs = 2
)

// H264Config describes the synthetic stream geometry and cadence.
type H264Config struct {
	// Width and Height are in pixels and must be multiples of 16.
	Width  int
	Height int
	// FPS is the nominal frame rate (1-60).
	FPS int
	// GOP is the number of pictures per IDR period (1-600).
	GOP int
}

// ErrInvalidConfig reports an unsupported H264Config.
var ErrInvalidConfig = errors.New("synthmedia: invalid H.264 configuration")

// H264Source produces an endless synthetic H.264 elementary stream. It is not
// safe for concurrent use.
type H264Source struct {
	config  H264Config
	mbW     int
	mbH     int
	sps     []byte
	pps     []byte
	frame   int
	idrPics int
}

// NewH264Source validates config and prepares the parameter sets.
func NewH264Source(config H264Config) (*H264Source, error) {
	if config.Width < 32 || config.Width > 1920 || config.Width%16 != 0 ||
		config.Height < 32 || config.Height > 1088 || config.Height%16 != 0 ||
		config.FPS < 1 || config.FPS > 60 || config.GOP < 1 || config.GOP > 600 {
		return nil, ErrInvalidConfig
	}
	source := &H264Source{config: config, mbW: config.Width / 16, mbH: config.Height / 16}
	source.sps = source.buildSPS()
	source.pps = buildPPS()
	return source, nil
}

// SPS returns a copy of the sequence parameter set NAL unit.
func (s *H264Source) SPS() []byte { return append([]byte(nil), s.sps...) }

// PPS returns a copy of the picture parameter set NAL unit.
func (s *H264Source) PPS() []byte { return append([]byte(nil), s.pps...) }

// FrameDuration is the nominal interval between access units.
func (s *H264Source) FrameDuration() time.Duration {
	return time.Second / time.Duration(s.config.FPS)
}

// NextAccessUnit returns the next access unit as NAL units without start codes
// and reports whether it is an IDR. IDR access units carry the SPS and PPS
// in-band, as most cameras do.
func (s *H264Source) NextAccessUnit() ([][]byte, bool) {
	index := s.frame
	s.frame++
	sinceIDR := index % s.config.GOP
	if sinceIDR == 0 {
		slice := s.idrSlice(index)
		s.idrPics++
		return [][]byte{s.SPS(), s.PPS(), slice}, true
	}
	return [][]byte{s.pSlice(index, sinceIDR)}, false
}

func (s *H264Source) buildSPS() []byte {
	var w bitWriter
	w.writeBits(66, 8)   // profile_idc: Baseline
	w.writeBits(0xC0, 8) // constraint_set0 and set1: Constrained Baseline
	w.writeBits(31, 8)   // level_idc 3.1
	w.writeUE(0)         // seq_parameter_set_id
	w.writeUE(log2MaxFrameNum - 4)
	w.writeUE(2) // pic_order_cnt_type: output order equals decoding order
	w.writeUE(1) // max_num_ref_frames
	w.writeBit(0)
	w.writeUE(uint64(s.mbW - 1)) //nolint:gosec // validated: at most 119
	w.writeUE(uint64(s.mbH - 1)) //nolint:gosec // validated: at most 67
	w.writeBit(1)                // frame_mbs_only_flag
	w.writeBit(1)                // direct_8x8_inference_flag
	w.writeBit(0)                // frame_cropping_flag
	w.writeBit(0)                // vui_parameters_present_flag
	return nalUnit(3, naluTypeSPS, w.trailing())
}

func buildPPS() []byte {
	var w bitWriter
	w.writeUE(0)      // pic_parameter_set_id
	w.writeUE(0)      // seq_parameter_set_id
	w.writeBit(0)     // entropy_coding_mode_flag: CAVLC
	w.writeBit(0)     // bottom_field_pic_order_in_frame_present_flag
	w.writeUE(0)      // num_slice_groups_minus1
	w.writeUE(0)      // num_ref_idx_l0_default_active_minus1
	w.writeUE(0)      // num_ref_idx_l1_default_active_minus1
	w.writeBit(0)     // weighted_pred_flag
	w.writeBits(0, 2) // weighted_bipred_idc
	w.writeSE(0)      // pic_init_qp_minus26
	w.writeSE(0)      // pic_init_qs_minus26
	w.writeSE(0)      // chroma_qp_index_offset
	w.writeBit(1)     // deblocking_filter_control_present_flag
	w.writeBit(0)     // constrained_intra_pred_flag
	w.writeBit(0)     // redundant_pic_cnt_present_flag
	return nalUnit(3, naluTypePPS, w.trailing())
}

func (s *H264Source) idrSlice(index int) []byte {
	var w bitWriter
	w.writeUE(0) // first_mb_in_slice
	w.writeUE(7) // slice_type: I, all slices
	w.writeUE(0) // pic_parameter_set_id
	w.writeBits(0, log2MaxFrameNum)
	// idr_pic_id differs between consecutive IDRs.
	w.writeUE(uint64(s.idrPics % 65536)) //nolint:gosec // non-negative and below 65536
	w.writeBit(0)                        // no_output_of_prior_pics_flag
	w.writeBit(0)                        // long_term_reference_flag
	w.writeSE(0)                         // slice_qp_delta
	w.writeUE(1)                         // disable_deblocking_filter_idc
	position := s.blockColumn(index)
	for mby := 0; mby < s.mbH; mby++ {
		for mbx := 0; mbx < s.mbW; mbx++ {
			w.writeUE(mbTypeIPCMInI)
			s.writePCM(&w, mbx, mby, position)
		}
	}
	return nalUnit(3, naluTypeIDR, w.trailing())
}

func (s *H264Source) pSlice(index, sinceIDR int) []byte {
	var w bitWriter
	w.writeUE(0)                                                        // first_mb_in_slice
	w.writeUE(5)                                                        // slice_type: P, all slices
	w.writeUE(0)                                                        // pic_parameter_set_id
	w.writeBits(uint64(sinceIDR%(1<<log2MaxFrameNum)), log2MaxFrameNum) //nolint:gosec // frame_num is non-negative and below 16
	w.writeBit(0)                                                       // num_ref_idx_active_override_flag
	w.writeBit(0)                                                       // ref_pic_list_modification_flag_l0
	w.writeBit(0)                                                       // adaptive_ref_pic_marking_mode_flag
	w.writeSE(0)                                                        // slice_qp_delta
	w.writeUE(1)                                                        // disable_deblocking_filter_idc

	previous := s.blockColumn(index - 1)
	current := s.blockColumn(index)
	total := s.mbW * s.mbH
	address := 0
	for address < total {
		run := 0
		for address+run < total && !s.touched(address+run, previous, current) {
			run++
		}
		w.writeUE(uint64(run))
		address += run
		if address == total {
			break
		}
		w.writeUE(mbTypeIPCMInP)
		s.writePCM(&w, address%s.mbW, address/s.mbW, current)
		address++
	}
	// P pictures stay reference pictures: picture order count type 2 forbids
	// consecutive non-reference frames.
	return nalUnit(2, naluTypeNonIDR, w.trailing())
}

// blockColumn is the leftmost macroblock column of the moving block.
func (s *H264Source) blockColumn(index int) int {
	span := s.mbW - blockMBs + 1
	return ((index % span) + span) % span
}

func (s *H264Source) blockRow() int { return s.mbH/2 - blockMBs/2 }

func (s *H264Source) inBlock(mbx, mby, column int) bool {
	row := s.blockRow()
	return mbx >= column && mbx < column+blockMBs && mby >= row && mby < row+blockMBs
}

func (s *H264Source) touched(address, previous, current int) bool {
	mbx, mby := address%s.mbW, address/s.mbW
	return s.inBlock(mbx, mby, previous) || s.inBlock(mbx, mby, current)
}

// barColors are 75% color bars in limited-range YCbCr.
var barColors = [8][3]byte{
	{180, 128, 128}, {162, 44, 142}, {131, 156, 44}, {112, 72, 58},
	{84, 184, 198}, {65, 100, 212}, {35, 212, 114}, {16, 128, 128},
}

var blockColor = [3]byte{235, 128, 128}

func (s *H264Source) color(x int) [3]byte {
	return barColors[x*len(barColors)/s.config.Width]
}

// writePCM writes one I_PCM macroblock: alignment, 256 luma, then 64 Cb and
// 64 Cr samples. Samples stay within limited range, so none is zero.
func (s *H264Source) writePCM(w *bitWriter, mbx, mby, column int) {
	w.alignZero()
	block := s.inBlock(mbx, mby, column)
	samples := make([]byte, 0, 384)
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			samples = append(samples, s.pixel(block, mbx*16+x)[0])
		}
	}
	for plane := 1; plane <= 2; plane++ {
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				samples = append(samples, s.pixel(block, mbx*16+x*2)[plane])
			}
		}
	}
	w.writeAlignedBytes(samples)
}

func (s *H264Source) pixel(block bool, x int) [3]byte {
	if block {
		return blockColor
	}
	return s.color(x)
}
