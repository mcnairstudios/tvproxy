package extradata

import "fmt"

// bitReader reads bits and exp-golomb codes from a byte slice.
type bitReader struct {
	data []byte
	pos  int // bit position
}

func newBitReader(data []byte) *bitReader {
	return &bitReader{data: data}
}

// readBits reads n bits (up to 32) and returns as uint32.
func (r *bitReader) readBits(n int) (uint32, error) {
	if n < 0 || n > 32 {
		return 0, fmt.Errorf("bitReader: invalid bit count %d", n)
	}
	if n == 0 {
		return 0, nil
	}
	if (r.pos + n) > len(r.data)*8 {
		return 0, fmt.Errorf("bitReader: read past end of data (pos=%d, need=%d, have=%d bits)", r.pos, n, len(r.data)*8)
	}

	var val uint32
	for i := 0; i < n; i++ {
		byteIdx := r.pos >> 3
		bitIdx := 7 - (r.pos & 7)
		val = (val << 1) | uint32((r.data[byteIdx]>>bitIdx)&1)
		r.pos++
	}
	return val, nil
}

// readBit reads a single bit.
func (r *bitReader) readBit() (uint8, error) {
	v, err := r.readBits(1)
	return uint8(v), err
}

// readUE reads an unsigned exp-golomb coded value.
// Exp-golomb encoding: count leading zeros, then read that many bits + 1.
// Value = 2^leadingZeros - 1 + readBits(leadingZeros)
func (r *bitReader) readUE() (uint32, error) {
	leadingZeros := 0
	for {
		bit, err := r.readBit()
		if err != nil {
			return 0, err
		}
		if bit == 1 {
			break
		}
		leadingZeros++
		if leadingZeros > 31 {
			return 0, fmt.Errorf("bitReader: exp-golomb leading zeros exceed 31")
		}
	}
	if leadingZeros == 0 {
		return 0, nil
	}
	suffix, err := r.readBits(leadingZeros)
	if err != nil {
		return 0, err
	}
	return (1 << leadingZeros) - 1 + suffix, nil
}

// readSE reads a signed exp-golomb coded value.
// k = readUE(); if k is even: -(k/2), if k is odd: (k+1)/2
func (r *bitReader) readSE() (int32, error) {
	k, err := r.readUE()
	if err != nil {
		return 0, err
	}
	if k%2 == 0 {
		return -int32(k / 2), nil
	}
	return int32((k + 1) / 2), nil
}

// skip advances the bit position by n bits.
func (r *bitReader) skip(n int) {
	r.pos += n
}
