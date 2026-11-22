package main

// Sound sharing, either direction: the computer that wants to hear the other one asks for it
// (audio-want); the other records what it is playing (WASAPI loopback on Windows, the output's monitor
// through PipeWire on Linux) and streams it as raw 16-bit stereo PCM in typAudio frames over the
// encrypted connection. About 1.5 Mbit/s at 48 kHz while something plays; silence isn't sent.

import (
	"encoding/json"
	"log"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// audioSink plays PCM on the receiving side. write must never block the connection's read loop.
type audioSink interface {
	write(pcm []byte) bool // false once the player has died
	close()
}

type audioState struct {
	mu    sync.Mutex
	stop  func()    // sending: capture + sender
	sink  audioSink // receiving: running player
	rate  int       // receiving: sample rate of the stream
	tried time.Time // receiving: last player start attempt

	want     atomic.Bool  // play the other computer's sound here (this side's switch)
	wantMute atomic.Bool  // ...and ask it to mute its own speakers meanwhile
	send     atomic.Bool  // the other computer asked for this one's sound
	sendMute atomic.Bool  // ...with its speakers muted
	lastIn   atomic.Int64 // unix ms of the last packet received
	lastOut  atomic.Int64 // unix ms of the last packet sent
	err      atomic.Value // string: this side can't play
	peerErr  atomic.Value // string: the other side can't send (audio-err)
}

// muteMarker exists while this app has muted its computer's speakers (see unmuteAfterCrash).
var muteMarker string

// prefs are settings that outlive a pairing, kept in prefs.json.
type prefs struct {
	SoundIn       bool `json:"soundIn,omitempty"`       // play the other computer's sound here
	SoundKeepPeer bool `json:"soundKeepPeer,omitempty"` // don't mute the other computer's speakers meanwhile
}

func loadPrefs(d dirs) prefs {
	var p prefs
	if b, err := os.ReadFile(filepath.Join(d.home, "prefs.json")); err == nil {
		json.Unmarshal(b, &p)
	}
	return p
}

func savePrefs(d dirs, p prefs) {
	os.MkdirAll(d.home, 0o755)
	j, _ := json.Marshal(p)
	os.WriteFile(filepath.Join(d.home, "prefs.json"), j, 0o600)
}

// audioSet applies this side's switches from the UI, remembers them and tells the other computer.
func (a *app) audioSet(want, mute bool) {
	a.aud.want.Store(want)
	a.aud.wantMute.Store(mute)
	a.aud.peerErr.Store("")
	savePrefs(a.dirs, prefs{SoundIn: want, SoundKeepPeer: !mute})
	if s := a.current(); s != nil {
		a.audioSendWant(s)
	}
	go a.audioRestart() // playing here stops sending from here (it would loop back)
}

// audioSendWant tells the other computer whether to send its sound (on connect and on every change).
func (a *app) audioSendWant(s *session) {
	s.send(msg{T: "audio-want", On: a.aud.want.Load(), Mute: a.aud.wantMute.Load()})
}

// audioWant handles the other computer's audio-want.
func (a *app) audioWant(m msg) {
	a.aud.send.Store(m.On)
	a.aud.sendMute.Store(m.Mute)
	go a.audioRestart()
}

func (a *app) audioHalt() {
	a.aud.mu.Lock()
	st := a.aud.stop
	a.aud.stop = nil
	a.aud.mu.Unlock()
	if st != nil {
		st()
	}
}

// audioRestart starts or stops capturing and sending, as the other computer asked.
func (a *app) audioRestart() {
	a.audioHalt()
	s := a.current()
	if !a.aud.send.Load() || s == nil {
		return
	}
	if a.aud.want.Load() {
		s.send(msg{T: "audio-err", Text: "it is playing your sound, so it can't send its own back (that would echo)"})
		return
	}
	// capture and network are decoupled: a stalled connection drops sound instead of stalling capture
	q := make(chan []byte, 16)
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		for b := range q {
			if s.write(typAudio, b) != nil {
				return
			}
			a.aud.lastOut.Store(time.Now().UnixMilli())
		}
	}()
	var buf []byte
	var errOnce sync.Once
	chunk := 48000 / 50 * 4
	stop := startCapture(a.aud.sendMute.Load(), func(rate int) {
		chunk = rate / 50 * 4 // 20 ms
		buf = buf[:0]
		s.send(msg{T: "audio-start", W: rate})
	}, func(p []byte) {
		buf = append(buf, p...)
		for len(buf) >= chunk {
			if !silent(buf[:chunk]) {
				select {
				case q <- append([]byte(nil), buf[:chunk]...):
				default: // connection is behind: drop this bit
				}
			}
			buf = append(buf[:0], buf[chunk:]...)
		}
	}, func(err error) {
		errOnce.Do(func() { s.send(msg{T: "audio-err", Text: err.Error()}) })
	})
	log.Print("Sending this computer's sound to the other one")
	a.aud.mu.Lock()
	a.aud.stop = func() {
		stop()
		close(q)
		<-sent
		s.send(msg{T: "audio-stop"})
	}
	a.aud.mu.Unlock()
}

// startCapture records this computer's sound until stop is called, starting over after errors (device
// unplugged or switched, recorder died). onFormat runs when the sample rate is known; onPCM gets 16-bit
// stereo little-endian samples; onErr gets each failure. With mute, the speakers are muted meanwhile.
func startCapture(mute bool, onFormat func(rate int), onPCM func([]byte), onErr func(error)) (stop func()) {
	quit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			err := captureOnce(quit, mute, onFormat, onPCM)
			if err == nil {
				return // stopped
			}
			log.Printf("Sound capture: %v (retrying)", err)
			onErr(err)
			select {
			case <-quit:
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
	return func() {
		close(quit)
		<-done
	}
}

// silent: all samples are zero (nothing playing). Not worth 1.5 Mbit/s.
func silent(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// audioMessage (receiving side) handles audio-start / audio-stop / audio-err.
func (a *app) audioMessage(m msg) {
	if m.T == "audio-err" {
		a.aud.peerErr.Store(m.Text)
		return
	}
	a.aud.peerErr.Store("")
	a.aud.mu.Lock()
	defer a.aud.mu.Unlock()
	if a.aud.sink != nil {
		a.aud.sink.close()
		a.aud.sink = nil
	}
	a.aud.rate = 0
	if m.T == "audio-start" && m.W >= 8000 && m.W <= 192000 {
		a.aud.rate = m.W
		a.audioOpenLocked()
	}
}

func (a *app) audioOpenLocked() {
	a.aud.tried = time.Now()
	sk, err := startPlayer(a.aud.rate)
	if err != nil {
		a.aud.err.Store(err.Error())
		log.Printf("Cannot play the other computer's sound: %v", err)
		return
	}
	a.aud.err.Store("")
	a.aud.sink = sk
}

// audioData (receiving side) plays one packet. If the player died it is restarted.
func (a *app) audioData(p []byte) {
	a.aud.lastIn.Store(time.Now().UnixMilli())
	a.aud.mu.Lock()
	defer a.aud.mu.Unlock()
	if a.aud.sink == nil {
		if a.aud.rate == 0 || time.Since(a.aud.tried) < 3*time.Second {
			return
		}
		a.audioOpenLocked()
		if a.aud.sink == nil {
			return
		}
	}
	if !a.aud.sink.write(p) {
		a.aud.sink.close()
		a.aud.sink = nil
	}
}

// audioClose runs when the connection ends.
func (a *app) audioClose() {
	a.aud.send.Store(false) // the next connection asks again
	a.audioHalt()
	a.aud.mu.Lock()
	if a.aud.sink != nil {
		a.aud.sink.close()
		a.aud.sink = nil
	}
	a.aud.rate = 0
	a.aud.mu.Unlock()
}

func (a *app) audioStatus() map[string]any {
	e, _ := a.aud.err.Load().(string)
	if pe, _ := a.aud.peerErr.Load().(string); pe != "" && a.aud.want.Load() {
		e = "The other computer can't send its sound: " + pe
	}
	now := time.Now().UnixMilli()
	return map[string]any{
		"want": a.aud.want.Load(), "mute": a.aud.wantMute.Load(), "error": e,
		"playing": now-a.aud.lastIn.Load() < 1500,
		"sending": a.aud.send.Load() && now-a.aud.lastOut.Load() < 1500,
	}
}

// toStereo16 converts interleaved float32 samples with ch channels to 16-bit stereo (first two channels;
// mono is doubled).
func toStereo16(out []byte, s []float32, ch int) []byte {
	conv := func(f float32) int16 {
		v := math.Round(float64(f) * 32767)
		return int16(max(-32768, min(32767, v)))
	}
	r := min(1, ch-1)
	for i := 0; i+ch <= len(s); i += ch {
		l, rr := conv(s[i]), conv(s[i+r])
		out = append(out, byte(l), byte(uint16(l)>>8), byte(rr), byte(uint16(rr)>>8))
	}
	return out
}

func pcm16Stereo(out []byte, s []int16, ch int) []byte {
	r := min(1, ch-1)
	for i := 0; i+ch <= len(s); i += ch {
		l, rr := s[i], s[i+r]
		out = append(out, byte(l), byte(uint16(l)>>8), byte(rr), byte(uint16(rr)>>8))
	}
	return out
}

// jitterQueue sits between the network and a blocking player: it never blocks the producer, and keeps
// the delay bounded. After a network hiccup a burst of packets arrives at once; instead of playing
// them all late from then on, it skips ahead.
type jitterQueue struct {
	q    chan []byte
	high int // drop down to low when this many packets are waiting
	low  int
}

func newJitterQueue(high, low int) *jitterQueue {
	return &jitterQueue{q: make(chan []byte, high+4), high: high, low: low}
}

func (j *jitterQueue) push(b []byte) {
	if len(j.q) >= j.high {
		for len(j.q) > j.low {
			select {
			case <-j.q:
			default:
			}
		}
	}
	select {
	case j.q <- append([]byte(nil), b...):
	default:
	}
}
