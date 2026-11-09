package audio

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type Devices struct {
	Card              int
	Capture, Playback string
}

var cardPattern = regexp.MustCompile(`(?m)^\s*(\d+)\s+\[sprdphone[^\]]*\]`)
var pcmPattern = regexp.MustCompile(`(?m)^(\d+)-(\d+):\s+(\S+)`)

func ParseDevices(cards, pcm string) (Devices, error) {
	m := cardPattern.FindStringSubmatch(cards)
	if m == nil {
		return Devices{}, fmt.Errorf("MU300 sound card not registered")
	}
	card, _ := strconv.Atoi(m[1])
	d := Devices{Card: card}
	for _, m := range pcmPattern.FindAllStringSubmatch(pcm, -1) {
		c, _ := strconv.Atoi(m[1])
		if c != card {
			continue
		}
		n, _ := strconv.Atoi(m[2])
		switch strings.TrimSpace(m[3]) {
		case "FE_ST_DUMP":
			d.Capture = fmt.Sprintf("hw:%d,%d", card, n)
		case "FE_ST_VOICE_PCM_P":
			d.Playback = fmt.Sprintf("hw:%d,%d", card, n)
		}
	}
	if d.Capture == "" || d.Playback == "" {
		return d, fmt.Errorf("MU300 call PCM endpoints missing")
	}
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
