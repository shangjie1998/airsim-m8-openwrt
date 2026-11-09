package audio

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingSceneToolsRejectReadiness(t *testing.T) {
	for _, missing := range []string{"/usr/bin/mu300-voice", "mu300-audio-dsp", "pcmhost", "ctlset", "aplay"} {
		err := checkTools(func(name string) (string, error) {
			if name == missing {
				return "", errors.New("private underlying error")
			}
			return name, nil
		})
		if err == nil || !strings.Contains(err.Error(), missing) || strings.Contains(err.Error(), "private") {
			t.Fatalf("missing %s: %v", missing, err)
		}
	}
}

func TestBrokenVoiceSymlinkRejectsReadiness(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "mu300-voice")
	if err := os.Symlink(filepath.Join(dir, "removed-extra", "mu300-voice"), link); err != nil {
		t.Fatal(err)
	}
	err := checkTools(func(name string) (string, error) {
		if name == "/usr/bin/mu300-voice" {
			return exec.LookPath(link)
		}
		return name, nil
	})
	if err == nil {
		t.Fatal("broken scene-manager symlink reported available")
	}
	if err := checkTools(func(name string) (string, error) { return name, nil }); err != nil {
		t.Fatal(err)
	}
}
