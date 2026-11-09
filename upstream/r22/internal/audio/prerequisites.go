package audio

import (
	"fmt"
	"os/exec"
)

// CheckTools is read-only: discovering a sound card does not mean that its
// userspace scene manager survived a system update. Never launch a DSP probe
// from health/status requests.
func CheckTools() error {
	return checkTools(exec.LookPath)
}

func checkTools(lookup func(string) (string, error)) error {
	for _, name := range []string{"/usr/bin/mu300-at", "/usr/bin/mu300-voice", "mu300-audio-dsp", "pcmhost", "ctlset", "amixer", "aplay", "arecord"} {
		if _, err := lookup(name); err != nil {
			return fmt.Errorf("audio dependency unavailable: %s", name)
		}
	}
	return nil
}
