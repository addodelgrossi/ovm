package synthmedia

// bitWriter accumulates an H.264 RBSP most-significant bit first.
type bitWriter struct {
	buf   []byte
	nbits int
}

func (w *bitWriter) writeBit(bit uint) {
	if w.nbits%8 == 0 {
		w.buf = append(w.buf, 0)
	}
	if bit != 0 {
		w.buf[len(w.buf)-1] |= 0x80 >> (w.nbits % 8)
	}
	w.nbits++
}

// writeBits writes the low n bits of value, most significant first.
func (w *bitWriter) writeBits(value uint64, n int) {
	for i := n - 1; i >= 0; i-- {
		w.writeBit(uint((value >> i) & 1))
	}
}

// writeUE writes an unsigned Exp-Golomb code (ue(v)).
func (w *bitWriter) writeUE(value uint64) {
	coded := value + 1
	length := 0
	for v := coded; v > 1; v >>= 1 {
		length++
	}
	w.writeBits(0, length)
	w.writeBits(coded, length+1)
}

// writeSE writes a signed Exp-Golomb code (se(v)).
func (w *bitWriter) writeSE(value int64) {
	if value > 0 {
		w.writeUE(uint64(2*value - 1))
		return
	}
	w.writeUE(uint64(-2 * value))
}

// alignZero pads with zero bits to the next byte boundary.
func (w *bitWriter) alignZero() {
	for w.nbits%8 != 0 {
		w.writeBit(0)
	}
}

// writeAlignedBytes appends whole bytes; the writer must be byte aligned.
func (w *bitWriter) writeAlignedBytes(data []byte) {
	w.buf = append(w.buf, data...)
	w.nbits += 8 * len(data)
}

// trailing writes rbsp_trailing_bits and returns the RBSP.
func (w *bitWriter) trailing() []byte {
	w.writeBit(1)
	w.alignZero()
	return w.buf
}

// nalUnit prefixes the NAL header and inserts emulation-prevention bytes so no
// start-code prefix can appear inside the payload.
func nalUnit(refIdc, naluType byte, rbsp []byte) []byte {
	out := make([]byte, 0, len(rbsp)+len(rbsp)/64+2)
	out = append(out, refIdc<<5|naluType)
	zeros := 0
	for _, b := range rbsp {
		if zeros >= 2 && b <= 3 {
			out = append(out, 3)
			zeros = 0
		}
		out = append(out, b)
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out
}
