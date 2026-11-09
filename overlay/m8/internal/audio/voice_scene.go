package audio

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// probeCall reports whether a cellular call is currently up on this device.
// F50 answers AT+CLCC through the vendor mu300-at wrapper; M8 has no such tool
// and the call objects live in ModemManager instead.
func probeCall(ctx context.Context, d Devices) (bool, error) {
	p, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if d.CallProbe == probeMM {
		out, err := exec.CommandContext(p, "mmcli", "-m", "any", "--voice-list-calls").Output()
		if err != nil {
			return false, errors.New("cellular call status unavailable")
		}
		return strings.Contains(string(out), "Call/"), nil
	}
	out, err := exec.CommandContext(p, "/usr/bin/mu300-at", "-t", "3", "AT+CLCC").Output()
	if err != nil || !strings.Contains(string(out), "OK") {
		return false, errors.New("cellular call status unavailable")
	}
	return strings.Contains(string(out), "+CLCC:"), nil
}

const (
	voiceCaptureCtl  = "S_VOICE_CAP_C SWITCH"
	voicePlaybackCtl = "S_VOICE_PCM_P SWITCH"
)

// voiceScene switches both DSP voice routes at once; kept for callers that do
// not need the staged bring-up.
func voiceScene(ctx context.Context, card string, on bool) error {
	if err := voiceSwitch(ctx, card, voicePlaybackCtl, on); err != nil {
		return err
	}
	return voiceSwitch(ctx, card, voiceCaptureCtl, on)
}

// voiceSwitch flips a single DSP voice scene control.
func voiceSwitch(ctx context.Context, card, ctl string, on bool) error {
	state := "off"
	if on {
		state = "on"
	}
	if e := run(ctx, "amixer", "-q", "-c", card, "cset", "name="+ctl, state); e != nil {
		return fmt.Errorf("voice scene control %s: %w", ctl, e)
	}
	return nil
}

// checkMMTools validates the M8 side tool set; the vendor scene manager is not
// present there and must not be demanded.
func CheckMMTools() error {
	return checkMMToolsWith(exec.LookPath)
}

func checkMMToolsWith(lookup func(string) (string, error)) error {
	for _, name := range []string{"mmcli", "amixer", "aplay", "arecord"} {
		if _, err := lookup(name); err != nil {
			return fmt.Errorf("audio dependency unavailable: %s", name)
		}
	}
	return nil
}
