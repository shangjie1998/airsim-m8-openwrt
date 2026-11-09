package audio

import (
	"encoding/binary"
	"math"
)

// Downsampler retains FIR history across frames. Input is 48 kHz stereo S16LE;
// output is 8 kHz mono. A windowed low-pass suppresses aliasing before decimation.
type Downsampler struct {
	taps       [127]float64
	history    [127]float64
	pos, phase int
}

func NewDownsampler() *Downsampler {
	d := &Downsampler{}
	sum := 0.0
	for i := range d.taps {
		x := float64(i - 63)
		v := 2 * 3400.0 / 48000
		if x != 0 {
			v = math.Sin(2*math.Pi*3400*x/48000) / (math.Pi * x)
		}
		v *= 0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/126)
		d.taps[i] = v
		sum += v
	}
	for i := range d.taps {
		d.taps[i] /= sum
	}
	return d
}
func (d *Downsampler) Convert(in []byte) []byte {
	out := make([]byte, 0, len(in)/12)
	for i := 0; i+3 < len(in); i += 4 {
		d.history[d.pos] = float64(int16(binary.LittleEndian.Uint16(in[i:])))
		d.pos = (d.pos + 1) % len(d.history)
		d.phase++
		if d.phase < 6 {
			continue
		}
		d.phase = 0
		y := 0.0
		for j, t := range d.taps {
			k := (d.pos - 1 - j + len(d.history)) % len(d.history)
			y += t * d.history[k]
		}
		v := int16(math.Max(-32768, math.Min(32767, math.Round(y))))
		out = binary.LittleEndian.AppendUint16(out, uint16(v))
	}
	return out
}

// Upsampler carries the previous sample across 20 ms frame boundaries.
type Upsampler struct{ previous int16 }

func (u *Upsampler) Convert(in []byte) []byte {
	out := make([]byte, 0, len(in)*2)
	for i := 0; i+1 < len(in); i += 2 {
		v := int16(binary.LittleEndian.Uint16(in[i:]))
		mid := int16((int32(u.previous) + int32(v)) / 2)
		out = binary.LittleEndian.AppendUint16(out, uint16(mid))
		out = binary.LittleEndian.AppendUint16(out, uint16(v))
		u.previous = v
	}
	return out
}
