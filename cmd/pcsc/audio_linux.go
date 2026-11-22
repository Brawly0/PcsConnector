//go:build linux

package main

// Laptop side of sound sharing: raw PCM is piped into pw-cat (PipeWire), pacat (PulseAudio) or aplay
// (ALSA), whichever exists. The pipe is shrunk to one page so it can't hide a growing delay.

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

type pipePlayer struct {
	cmd  *exec.Cmd
	w    *os.File
	jq   *jitterQueue
	quit chan struct{}
	dead chan struct{}
	once sync.Once
}

func playerCommands(rate int) [][]string {
	r := fmt.Sprint(rate)
	return [][]string{
		{"pw-cat", "--playback", "--raw", "--format", "s16", "--rate", r, "--channels", "2", "--latency", "40ms",
			"--properties", `{ "media.name": "PC sound", "application.name": "PcsConnector" }`, "-"},
		{"pacat", "--playback", "--raw", "--format=s16le", "--rate=" + r, "--channels=2", "--latency-msec=60",
			"--client-name=PcsConnector", "--stream-name=PC sound"},
		{"aplay", "-q", "-t", "raw", "-f", "S16_LE", "-r", r, "-c", "2", "--buffer-time=80000", "-"},
	}
}

func startPlayer(rate int) (audioSink, error) {
	for _, c := range playerCommands(rate) {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		r, w, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		syscall.Syscall(syscall.SYS_FCNTL, w.Fd(), 1031, 4096) // F_SETPIPE_SZ: one page (~20 ms)
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Stdin = r
		if err := cmd.Start(); err != nil {
			r.Close()
			w.Close()
			continue
		}
		r.Close()
		// 20 ms packets: let at most ~120 ms pile up, then skip ahead to ~40 ms
		p := &pipePlayer{cmd: cmd, w: w, jq: newJitterQueue(6, 2), quit: make(chan struct{}), dead: make(chan struct{})}
		go p.run()
		go func() { cmd.Wait(); p.once.Do(func() { close(p.dead) }) }()
		return p, nil
	}
	return nil, errors.New("no sound player found: install pipewire-bin (pw-cat), pulseaudio-utils (pacat) or alsa-utils (aplay)")
}

func (p *pipePlayer) run() {
	for {
		select {
		case <-p.quit:
			return
		case <-p.dead:
			return
		case b := <-p.jq.q:
			if _, err := p.w.Write(b); err != nil {
				p.once.Do(func() { close(p.dead) })
				return
			}
		}
	}
}

func (p *pipePlayer) write(b []byte) bool {
	select {
	case <-p.dead:
		return false
	default:
	}
	p.jq.push(b)
	return true
}

func (p *pipePlayer) close() {
	select {
	case <-p.quit:
		return
	default:
	}
	close(p.quit)
	p.w.Close()
	if p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}
}

// captureOnce records what this computer is playing (the default output's monitor) as 48 kHz 16-bit
// stereo. PipeWire resamples whatever the output runs at. Returns nil when quit closes.
func captureOnce(quit chan struct{}, mute bool, onFormat func(int), onPCM func([]byte)) error {
	cmds := [][]string{
		{"pw-cat", "--record", "--raw", "--format", "s16", "--rate", "48000", "--channels", "2", "--latency", "20ms",
			"--properties", `{ "stream.capture.sink": true, "media.name": "Sound for the other computer", "application.name": "PcsConnector" }`, "-"},
		{"parec", "--raw", "--device=@DEFAULT_MONITOR@", "--format=s16le", "--rate=48000", "--channels=2", "--latency-msec=20",
			"--client-name=PcsConnector"},
	}
	var cmd *exec.Cmd
	for _, c := range cmds {
		if _, err := exec.LookPath(c[0]); err == nil {
			cmd = exec.Command(c[0], c[1:]...)
			break
		}
	}
	if cmd == nil {
		return errors.New("no sound recorder found: install pipewire-bin (pw-cat) or pulseaudio-utils (parec)")
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	defer cmd.Wait()
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-quit:
		case <-stopped:
		}
		cmd.Process.Kill()
	}()
	if mute {
		if was, ok := speakersMuted(); ok && !was {
			if muteMarker != "" {
				os.WriteFile(muteMarker, nil, 0o644) // see unmuteAfterCrash
			}
			setSpeakersMuted(true)
			defer func() {
				setSpeakersMuted(false)
				if muteMarker != "" {
					os.Remove(muteMarker)
				}
			}()
		}
	}
	onFormat(48000)
	buf := make([]byte, 3840)
	for {
		// whole 20 ms chunks only: a short read would split a sample and swap left and right from then on
		if _, err = io.ReadFull(out, buf); err == nil {
			onPCM(buf)
		}
		if err != nil {
			select {
			case <-quit:
				return nil
			default:
				return fmt.Errorf("the sound recorder stopped (%s)", cmd.Path)
			}
		}
	}
}

// speakersMuted reports the default output's mute switch: wpctl (PipeWire), else pactl (PulseAudio).
func speakersMuted() (muted, ok bool) {
	if b, err := exec.Command("wpctl", "get-volume", "@DEFAULT_AUDIO_SINK@").Output(); err == nil {
		return strings.Contains(string(b), "MUTED"), true
	}
	if b, err := exec.Command("pactl", "get-sink-mute", "@DEFAULT_SINK@").Output(); err == nil {
		return strings.Contains(string(b), "yes"), true
	}
	return false, false
}

func setSpeakersMuted(on bool) {
	v := "0"
	if on {
		v = "1"
	}
	if exec.Command("wpctl", "set-mute", "@DEFAULT_AUDIO_SINK@", v).Run() != nil {
		exec.Command("pactl", "set-sink-mute", "@DEFAULT_SINK@", v).Run()
	}
}

// unmuteAfterCrash: if the marker is still there, the app died while sending sound with the speakers
// muted. Unmute them.
func unmuteAfterCrash(home string) {
	muteMarker = filepath.Join(home, "sound-muted")
	if _, err := os.Stat(muteMarker); err != nil {
		return
	}
	setSpeakersMuted(false)
	os.Remove(muteMarker)
	log.Print("Unmuted the speakers (the app had stopped while sending sound)")
}
