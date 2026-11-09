package audio

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const FrameBytes = 320

type Bridge struct {
	busy      atomic.Bool
	RX        atomic.Uint64
	TX        atomic.Uint64
	Silence   atomic.Uint64
	Received  atomic.Uint64
	Dropped   atomic.Uint64
	mu        sync.Mutex
	lastError string
}

func (b *Bridge) Status() map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	return map[string]any{"active": b.busy.Load(), "uplink_frames": b.RX.Load(), "downlink_frames": b.TX.Load(), "silence_frames": b.Silence.Load(), "received_uplink_frames": b.Received.Load(), "dropped_uplink_frames": b.Dropped.Load(), "last_error": b.lastError}
}
func (b *Bridge) Handle(ctx context.Context, c net.Conn) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	r := bufio.NewReaderSize(c, 4096)
	hello := make([]byte, len("AIRSIMPCM1\n"))
	_, e := io.ReadFull(r, hello)
	if e != nil || string(hello) != "AIRSIMPCM1\n" {
		return
	}
	if !b.busy.CompareAndSwap(false, true) {
		return
	}
	defer b.busy.Store(false)
	// No probing or unauthenticated LAN connection can create a DSP scene.
	if e = b.serve(ctx, c, r, false); e != nil && !errors.Is(e, context.Canceled) && !errors.Is(e, io.EOF) && !errors.Is(e, net.ErrClosed) {
		b.mu.Lock()
		b.lastError = e.Error()
		b.mu.Unlock()
	}
}
func run(ctx context.Context, name string, args ...string) error {
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(c, name, args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}
func (b *Bridge) serve(parent context.Context, c net.Conn, r io.Reader, selfTest bool) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	devices, err := Discover()
	if err != nil {
		return err
	}
	if devices.CallProbe == probeMM {
		if err = CheckMMTools(); err != nil {
			return err
		}
	} else if err = CheckTools(); err != nil {
		return err
	}
	calling, err := probeCall(ctx, devices)
	if err != nil {
		return err
	}
	if selfTest && calling {
		return errors.New("finish current call before audio self-test")
	}
	if !selfTest && !calling {
		return errors.New("no cellular call for PCM session")
	}
	// M8 must not pick up the handset's own microphone during a call, so this
	// gateway never opens an ALSA capture stream on that platform.
	useCapture := !devices.NeedsVoiceScene
	if !selfTest {
		recordEvent("session start probe=%s playback=%s capture=%s mic=%s", devices.CallProbe, devices.Playback, devices.Capture, micState(useCapture))
	}
	card := strconv.Itoa(devices.Card)
	if devices.NeedsVoiceScene {
		// M8: only the playback route is armed here. The capture route is never
		// opened on this platform, so the handset microphone stays out of the call.
		// Both switches default to off and are cleared again on the way out.
		if err = voiceSwitch(ctx, card, voicePlaybackCtl, true); err != nil {
			return err
		}
		defer func() {
			clean, done := context.WithTimeout(context.Background(), 15*time.Second)
			defer done()
			_ = voiceSwitch(clean, card, voiceCaptureCtl, false)
			_ = voiceSwitch(clean, card, voicePlaybackCtl, false)
		}()
		// The DSP needs a moment between the mixer write and the first open.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(700 * time.Millisecond):
		}
	} else {
		if err = run(ctx, "/usr/bin/mu300-voice", "audio", "start", "dsp"); err != nil {
			return err
		}
		defer func() {
			clean, done := context.WithTimeout(context.Background(), 15*time.Second)
			defer done()
			_ = run(clean, "/usr/bin/mu300-voice", "audio", "stop")
		}()
		// Hold the AGDSP domain until *both* AP streams have fully closed. The
		// upstream watch service must be stopped by the plugin while we own audio.
		if err = run(ctx, "amixer", "-q", "-c", card, "cset", "name=agdsp_access_en", "1"); err != nil {
			return err
		}
	}
	// M8 runs the 6.18 kernel, where ALSA's own PREPARE/START recovery works; keep
	// aplay/arecord alive across an xrun or a transient EIO instead of tearing the
	// whole session down on the first error. (The old --fatal-errors flag was a
	// 5.4 vendor-DMA constraint that no longer applies here.)
	capture := exec.CommandContext(ctx, "arecord", "-q", "-D", devices.Capture, "-t", "raw",
		"-f", "S16_LE", "-r", strconv.Itoa(devices.CaptureRate), "-c", strconv.Itoa(devices.CaptureChannels),
		"--period-size="+strconv.Itoa(devices.CapturePeriod), "--buffer-size="+strconv.Itoa(devices.CaptureBuffer))
	play := exec.CommandContext(ctx, "aplay", "-q", "-D", devices.Playback, "-t", "raw",
		"-f", "S16_LE", "-r", strconv.Itoa(devices.PlaybackRate), "-c", strconv.Itoa(devices.PlaybackChannels),
		"--period-size="+strconv.Itoa(devices.PlaybackPeriod), "--buffer-size="+strconv.Itoa(devices.PlaybackBuffer))
	captureLog := &bytes.Buffer{}
	playLog := &bytes.Buffer{}
	capture.Stderr = captureLog
	play.Stderr = playLog
	// Uplink keeps emitting silent 20 ms frames when capture is off, so the
	// client still sees a steady stream.
	var captured io.ReadCloser
	if useCapture {
		captured, err = capture.StdoutPipe()
		if err != nil {
			return err
		}
	}
	played, err := play.StdinPipe()
	if err != nil {
		return err
	}
	// Playback must claim the voice route first. On ums9620 the DSP voice
	// playback FE fails with EIO about half a second after a capture stream
	// takes the same scene, so the order here is load bearing.
	if err = play.Start(); err != nil {
		return err
	}
	defer func() { cancel(); played.Close(); _ = play.Wait() }()
	if devices.NeedsVoiceScene {
		// M8 never arms the capture route: doing so hands the DSP voice capture FE
		// to us and pulls the handset microphone into the call.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	if useCapture {
		if err = capture.Start(); err != nil {
			return err
		}
		defer func() { cancel(); _ = capture.Wait() }()
	}
	// A disconnected socket unblocks all reader/writer goroutines before return.
	defer c.Close()
	c.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err = io.WriteString(c, "AIRSIMREADY"); err != nil {
		return err
	}
	c.SetDeadline(time.Time{})
	b.mu.Lock()
	b.lastError = ""
	b.mu.Unlock()
	failures := make(chan error, 3)
	frames := &uplinkQueue{}
	go func() {
		for {
			frame := make([]byte, FrameBytes)
			if _, e := io.ReadFull(r, frame); e != nil {
				failures <- e
				return
			}
			b.Received.Add(1)
			if frames.push(frame) {
				b.Dropped.Add(1)
			}
		}
	}()
	if useCapture {
		go func() {
			// M8's voice capture FE already streams 8 kHz mono 16-bit, which is the
			// native AirSIM frame rate: one 20 ms frame is exactly FrameBytes.
			direct := devices.CaptureRate == 8000 && devices.CaptureChannels == 1
			d := NewDownsampler()
			period := devices.CapturePeriod * devices.CaptureChannels * 2
			raw := make([]byte, period)
			for {
				if _, e := io.ReadFull(captured, raw); e != nil {
					// EOF from the ALSA process is a hardware-stream failure, unlike
					// a client socket closing normally. Preserve it in status.
					if ctx.Err() != nil {
						failures <- ctx.Err()
					} else {
						failures <- fmt.Errorf("ALSA capture stream ended: %s", strings.TrimSpace(captureLog.String()))
					}
					return
				}
				if direct {
					for off := 0; off+FrameBytes <= len(raw); off += FrameBytes {
						c.SetWriteDeadline(time.Now().Add(2 * time.Second))
						if e := writeFull(c, raw[off:off+FrameBytes]); e != nil {
							failures <- e
							return
						}
						b.TX.Add(1)
					}
					continue
				}
				frame := d.Convert(raw)
				c.SetWriteDeadline(time.Now().Add(2 * time.Second))
				if e := writeFull(c, frame); e != nil {
					failures <- e
					return
				}
				b.TX.Add(1)
			}
		}()
	} else {
		// Silent uplink: keep the cadence the client expects so a call without a
		// handset microphone still looks like a healthy uplink stream.
		go func() {
			t := time.NewTicker(20 * time.Millisecond)
			defer t.Stop()
			silent := make([]byte, FrameBytes)
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					c.SetWriteDeadline(time.Now().Add(2 * time.Second))
					if e := writeFull(c, silent); e != nil {
						failures <- e
						return
					}
					b.TX.Add(1)
					b.Silence.Add(1)
				}
			}
		}()
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	u := &Upsampler{}
	toPlayback := u.Convert
	if devices.PlaybackRate == 8000 && devices.PlaybackChannels == 1 {
		// M8 accepts the native 8 kHz mono frame as-is.
		toPlayback = func(frame []byte) []byte { return frame }
	}
	last := time.Now()
	lastWrite := atomic.Int64{}
	lastWrite.Store(time.Now().UnixNano())
	// A wedged ALSA pipe must not block cancellation or the stall watchdog.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				frame := frames.next()
				if frame != nil {
					b.RX.Add(1)
					last = time.Now()
				} else {
					frame = make([]byte, FrameBytes)
					b.Silence.Add(1)
				}
				if time.Since(last) > 10*time.Second {
					failures <- errors.New("PCM uplink stalled")
					return
				}
				if e := writeFull(played, toPlayback(frame)); e != nil {
					failures <- fmt.Errorf("%w (aplay: %s)", e, strings.TrimSpace(playLog.String()))
					return
				}
				lastWrite.Store(time.Now().UnixNano())
			}
		}
	}()
	watch := time.NewTicker(time.Second)
	defer watch.Stop()
	probes, gone := 0, 0
	// A hung-up call is not an audio fault. When the remote party hangs up the DSP
	// drops the voice path, and aplay/arecord then fail with EIO/EPIPE. Separate
	// that from a real mid-call stream failure so the log stays honest.
	callEnded := func() bool {
		if selfTest {
			return false
		}
		alive, e := probeCall(ctx, devices)
		if e != nil {
			return false
		}
		return !alive
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e := <-failures:
			if isStreamFault(e) {
				// The remote hang-up kills the ALSA stream a few seconds before the
				// modem reports the call gone, so one probe right here fires too early.
				// Give the call state a grace period to settle; only a stream fault that
				// outlives it counts as a real mid-call failure. 15s covers the modem's
				// own reporting lag after the remote party hangs up.
				if !selfTest {
					recordEvent("stream fault: %v", e)
					// A dropped audio stream is usually the first sign that the remote
					// party hung up, well before the periodic call-state poll sees it.
					if OnStreamFault != nil {
						OnStreamFault()
					}
				}
				grace := time.NewTimer(15 * time.Second)
				tick := time.NewTicker(time.Second)
				for {
					select {
					case <-ctx.Done():
						grace.Stop()
						tick.Stop()
						return e
					case <-grace.C:
						tick.Stop()
						if !selfTest {
							recordEvent("stream fault outlived the grace period: real mid-call failure")
						}
						return e
					case <-tick.C:
						if callEnded() {
							grace.Stop()
							tick.Stop()
							if !selfTest {
								recordEvent("stream fault resolved as a hang-up, unwinding clean")
							}
							return nil
						}
					}
				}
			}
			return e
		case <-watch.C:
			if time.Since(time.Unix(0, lastWrite.Load())) > 3*time.Second {
				return errors.New("ALSA playback stalled")
			}
			probes++
			// Two consecutive negatives, three seconds apart, before unwinding, so a
			// single stale modem reading cannot cut a live call short.
			if !selfTest && probes%3 == 0 {
				if callEnded() {
					gone++
					if gone >= 2 {
						recordEvent("call ended, unwinding clean")
						return nil
					}
				} else {
					gone = 0
				}
			}
		}
	}
}

// OnStreamFault, when set, is invoked the moment the audio stream dies. The daemon
// uses it to ask the agent for an immediate call-state read instead of waiting for
// the next poll.
var OnStreamFault func()

// micState renders the uplink capture mode for the session timeline.
func micState(capture bool) string {
	if capture {
		return "on"
	}
	return "off"
}

// recordEvent appends a one-line audio timeline entry so a hang-up and a real
// mid-call stream fault can be told apart afterwards without a live probe.
// Best effort only: logging must never disturb the call.
func recordEvent(format string, args ...any) {
	const dir = "/opt/airsim-m8/data"
	line := time.Now().Format(time.RFC3339) + " " + fmt.Sprintf(format, args...) + "\n"
	if e := os.MkdirAll(dir, 0o755); e != nil {
		return
	}
	path := dir + "/audio-events.log"
	// The device root is a single 4 GB partition, so this timeline is capped:
	// once it reaches 512 KB only the newest half is kept, in place.
	if info, statErr := os.Stat(path); statErr == nil && info.Size() > 512*1024 {
		if data, readErr := os.ReadFile(path); readErr == nil {
			keep := data
			if len(keep) > 256*1024 {
				keep = keep[len(keep)-256*1024:]
				if at := bytes.IndexByte(keep, '\n'); at >= 0 {
					keep = keep[at+1:]
				}
			}
			_ = os.WriteFile(path, keep, 0o644)
		}
	}
	f, e := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if e != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// isStreamFault reports whether an error came from an ALSA stream dying rather
// than from the client socket closing.
func isStreamFault(e error) bool {
	if e == nil {
		return false
	}
	s := e.Error()
	return strings.Contains(s, "ALSA capture stream ended") ||
		strings.Contains(s, "ALSA playback stream ended") ||
		strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "aplay:")
}

// SelfTest exercises the real DSP streams without placing or answering a call.
// Only the explicit CLI exposes this operation; no LuCI/public HTTP route does.
func (b *Bridge) SelfTest(ctx context.Context, duration time.Duration) error {
	if duration < time.Second || duration > 30*time.Second {
		return errors.New("self-test duration must be 1..30 seconds")
	}
	server, client := net.Pipe()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	finished := make(chan error, 1)
	go func() { defer server.Close(); finished <- b.serve(ctx, server, server, true) }()
	client.SetReadDeadline(time.Now().Add(20 * time.Second))
	ack := make([]byte, len("AIRSIMREADY"))
	if _, e := io.ReadFull(client, ack); e != nil {
		client.Close()
		cancel()
		<-finished
		return e
	}
	client.SetDeadline(time.Time{})
	go func() {
		var frame [FrameBytes]byte
		for {
			if e := writeFull(client, frame[:]); e != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}()
	go func() { io.Copy(io.Discard, client) }()
	select {
	case <-ctx.Done():
		client.Close()
		cancel()
		<-finished
		return ctx.Err()
	case e := <-finished:
		client.Close()
		cancel()
		return fmt.Errorf("audio self-test ended early: %w", e)
	case <-time.After(duration):
	}
	client.Close()
	cancel()
	<-finished
	if b.RX.Load() == 0 || b.TX.Load() == 0 {
		return errors.New("no bidirectional PCM frames")
	}
	return nil
}
func writeFull(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, e := w.Write(p)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}
