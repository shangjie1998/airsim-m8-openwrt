package audio

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Devices describes the two hostless call PCM endpoints on the sound card.
//
// F50 (vendor 5.4 kernel) exposes the downlink as FE_ST_DUMP and needs 48k
// stereo capture / 16k mono playback. M8 (mainline 6.18, ums9620) exposes a
// separate DMA-backed voice capture FE, FE_ST_VOICE_C, which streams 8k mono
// directly; FE_ST_DUMP has no backend DAIs there and cannot be opened at all.
type Devices struct {
	Card              int
	Capture, Playback string
	CaptureRate       int
	CaptureChannels   int
	CapturePeriod     int
	CaptureBuffer     int
	PlaybackRate      int
	PlaybackChannels  int
	PlaybackPeriod    int
	PlaybackBuffer    int
	// NeedsVoiceScene is set when the card only delivers call audio after the
	// AP voice switches (S_VOICE_CAP_C / S_VOICE_PCM_P) are turned on.
	NeedsVoiceScene bool
	// CallProbe selects how the bridge verifies an ongoing cellular call.
	CallProbe string
}

const (
	probeMU300 = "mu300-at"
	probeMM    = "mmcli"
)

var cardPattern = regexp.MustCompile(`(?m)^\s*(\d+)\s+\[sprdphone[^\]]*\]`)
var pcmPattern = regexp.MustCompile(`(?m)^(\d+)-(\d+):\s+(\S+)`)

func ParseDevices(cards, pcm string) (Devices, error) {
	m := cardPattern.FindStringSubmatch(cards)
	if m == nil {
		return Devices{}, fmt.Errorf("MU300 sound card not registered")
	}
	card, _ := strconv.Atoi(m[1])
	d := Devices{Card: card}
	seen := map[string]string{}
	for _, m := range pcmPattern.FindAllStringSubmatch(pcm, -1) {
		c, _ := strconv.Atoi(m[1])
		if c != card {
			continue
		}
		n, _ := strconv.Atoi(m[2])
		seen[strings.TrimSpace(m[3])] = fmt.Sprintf("hw:%d,%d", card, n)
	}
	// M8: prefer the DMA-backed voice capture FE whenever the card offers it.
	if v, ok := seen["FE_ST_VOICE_C"]; ok {
		d.Capture = v
		d.CaptureRate, d.CaptureChannels = 8000, 1
		d.CapturePeriod, d.CaptureBuffer = 640, 1280
	} else if v, ok := seen["FE_ST_DUMP"]; ok {
		d.Capture = v
		d.CaptureRate, d.CaptureChannels = 48000, 2
		d.CapturePeriod, d.CaptureBuffer = 960, 5760
	}
	d.Playback = seen["FE_ST_VOICE_PCM_P"]
	if d.Capture == "" || d.Playback == "" {
		return d, fmt.Errorf("MU300 call PCM endpoints missing")
	}
	// M8's FE_ST_VOICE_PCM_P consumes 16 kHz wideband voice samples even though
	// the paired capture FE is 8 kHz mono. Driving it with 8 kHz audio plays
	// everything at double speed, so route the native 8 kHz frame through the
	// upsampler instead of writing it straight to the DSP.
	d.PlaybackRate, d.PlaybackChannels = 16000, 1
	d.PlaybackPeriod, d.PlaybackBuffer = 320, 1920
	// The 8 kHz mono voice capture endpoint means this is the mainline M8 build,
	// where ModemManager owns the modem and the vendor toolchain does not exist.
	// A mu300-at present there is only an AT relay shim, not the F50 stack.
	d.CallProbe = probeMU300
	if d.CaptureChannels == 1 {
		d.CallProbe = probeMM
	} else if _, err := os.Stat("/usr/bin/mu300-at"); err != nil {
		d.CallProbe = probeMM
	}
	// The AP voice scene switches exist only on the mainline ums9620 build.
	d.NeedsVoiceScene = d.CaptureChannels == 1
	return d, nil
}
func Discover() (Devices, error) {
	c, e := os.ReadFile("/proc/asound/cards")
	if e != nil {
		return Devices{}, e
	}
	p, e := os.ReadFile("/proc/asound/pcm")
	if e != nil {
		return Devices{}, e
	}
	return ParseDevices(string(c), string(p))
}
