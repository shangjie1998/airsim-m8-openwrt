package audio

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestDiscoverUsesNames(t *testing.T) {
	for _, card := range []string{"0", "1"} {
		d, e := ParseDevices(" "+card+" [sprdphonesc2730]: sprdphone-sc273\n", card+"-16: FE_ST_DUMP (*)\n"+card+"-53: FE_ST_VOICE_PCM_P (*)\n")
		if e != nil || d.Capture != "hw:"+card+",16" {
			t.Fatalf("%+v %v", d, e)
		}
	}
}
func TestResamplerContinuity(t *testing.T) {
	in := make([]byte, 3840*3)
	for i := 0; i < len(in)/4; i++ {
		v := uint16(int16(12000 * math.Sin(2*math.Pi*1000*float64(i)/48000)))
		binary.LittleEndian.PutUint16(in[i*4:], v)
	}
	a := NewDownsampler().Convert(in)
	d := NewDownsampler()
	var b []byte
	for i := 0; i < len(in); i += 3840 {
		b = append(b, d.Convert(in[i:i+3840])...)
	}
	if string(a) != string(b) || len(a) != 960 {
		t.Fatal("framing or history lost")
	}
}
func TestAntiAliasing(t *testing.T) {
	energy := func(hz float64) float64 {
		d := NewDownsampler()
		raw := make([]byte, 48000*4)
		for i := 0; i < 48000; i++ {
			binary.LittleEndian.PutUint16(raw[i*4:], uint16(int16(16000*math.Sin(2*math.Pi*hz*float64(i)/48000))))
		}
		p := d.Convert(raw)
		var power float64
		for i := 320; i < len(p); i += 2 {
			v := float64(int16(binary.LittleEndian.Uint16(p[i:])))
			power += v * v
		}
		return power
	}
	if energy(6000)/energy(1000) > 0.001 {
		t.Fatal("insufficient alias rejection")
	}
}
func TestUplinkFrameSize(t *testing.T) {
	u := &Upsampler{}
	if len(u.Convert(make([]byte, 320))) != 640 {
		t.Fatal("wrong uplink duration")
	}
}
