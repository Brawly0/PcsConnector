package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

type msg struct {
	T        string `json:"t"`
	Name     string `json:"name,omitempty"`
	Platform string `json:"platform,omitempty"`
	Text     string `json:"text,omitempty"`
	Size     int64  `json:"size,omitempty"`
	W        int    `json:"w,omitempty"`
	H        int    `json:"h,omitempty"`
	Ev       []inEv `json:"ev,omitempty"`
	On       bool   `json:"on,omitempty"`
	Mute     bool   `json:"mute,omitempty"`
}

func (s *session) send(m msg) error {
	b, _ := json.Marshal(m)
	return s.write(typJSON, b)
}

type dirs struct{ home, inbox, outbox, sent string }

type app struct {
	dirs dirs
	key  []byte

	clipOn atomic.Bool
	done   chan struct{}
	once   sync.Once

	mu     sync.Mutex
	ln     net.Listener
	cur    *session
	peer   *msg   // set once the peer's hello arrives
	peerIP string // remote address of the current connection
	target string // laptop: address being dialled (typed, or found on the network)

	scr screenState

	kvm   kvmState
	kvmOn atomic.Bool

	aud audioState // sound sharing (audio.go)

	fast fastState // UDP fast path for mouse moves (fastpath.go)
}

func newApp(d dirs, token string, noClip bool) (*app, error) {
	for _, p := range []string{d.inbox, d.outbox, d.sent} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return nil, err
		}
	}
	a := &app{dirs: d, key: deriveKey(token), done: make(chan struct{})}
	a.clipOn.Store(!noClip)
	a.kvmOn.Store(true)
	p := loadPrefs(d)
	a.aud.want.Store(p.SoundIn)
	a.aud.wantMute.Store(!p.SoundKeepPeer)
	unmuteAfterCrash(d.home)
	debug.FreeOSMemory() // hand scrypt's 16 MB scratch space back to the OS
	go a.watchOutbox()
	return a, nil
}

func (a *app) stopped() bool {
	select {
	case <-a.done:
		return true
	default:
		return false
	}
}

func (a *app) close() {
	a.once.Do(func() {
		close(a.done)
		a.mu.Lock()
		ln, cur := a.ln, a.cur
		a.mu.Unlock()
		if ln != nil {
			ln.Close()
		}
		if cur != nil {
			cur.close()
		}
		a.screenStop()
	})
}

func (a *app) current() *session {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cur
}

func (a *app) peerInfo() (*msg, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.peer, a.peerIP
}

// serve accepts connections; the newest one replaces the previous.
func (a *app) serve(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.ln = ln
	a.mu.Unlock()
	if c := a.fastListen(port); c != nil {
		go a.fastHostRead(c, port) // answers laptops looking for this PC's code
	}
	log.Printf("Waiting for the other computer on port %d", port)
	for {
		c, err := ln.Accept()
		if err != nil {
			if a.stopped() {
				return nil
			}
			return err
		}
		go a.handle(newSession(c, a.key))
	}
}

// dial connects to the PC and reconnects until the app is closed. With no host it finds the PC on the
// local network by its pairing code (discover.go); with a host it still searches after repeated
// failures, in case the PC's address changed.
func (a *app) dial(host string, port int) {
	if c := a.fastListen(port); c != nil {
		go a.fastLaptopRead(c)
	}
	auto := host == ""
	if auto {
		log.Print("Looking for the PC on this network...")
	} else {
		log.Printf("Connecting to %s ...", net.JoinHostPort(host, fmt.Sprint(port)))
	}
	// log every failure while it changes, then a reminder now and then so a stuck link is visible
	lastErr, fails, lastFound := "", 0, ""
	for !a.stopped() {
		target, tport := host, port
		if auto || (fails > 0 && fails%3 == 0) {
			if f, ok := a.discover(port, 3*time.Second); ok {
				if f.ip != lastFound {
					log.Printf("Found %s at %s", f.name, f.ip)
					lastFound = f.ip
				}
				target, tport = f.ip, f.port
			} else if auto {
				fails++
				if fails == 2 || fails%20 == 0 {
					log.Print("No PC with this code found yet. Is PcsConnector open on the PC, on the same network? Over a VPN, enter the PC's address instead.")
				}
				continue // discover already waited
			}
		}
		a.mu.Lock()
		a.target = target
		a.mu.Unlock()
		addr := net.JoinHostPort(target, fmt.Sprint(tport))
		c, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err == nil {
			if fails > 0 && !auto {
				log.Printf("Reached %s after %d failed attempt(s)", addr, fails)
			}
			lastErr, fails = "", 0
			a.handle(newSession(c, a.key))
			if !a.stopped() {
				log.Print("Reconnecting...")
			}
		} else if !a.stopped() {
			fails++
			if e := err.Error(); e != lastErr || fails%4 == 0 {
				lastErr = e
				log.Printf("Cannot reach the PC at %s (attempt %d): %v", addr, fails, err)
			}
		}
		select {
		case <-a.done:
		case <-time.After(3 * time.Second):
		}
	}
}

func (a *app) handle(s *session) {
	a.mu.Lock()
	old := a.cur
	a.cur = s
	a.peer = nil
	a.peerIP, _, _ = net.SplitHostPort(s.c.RemoteAddr().String())
	a.mu.Unlock()
	if old != nil {
		old.close()
	}
	defer func() {
		a.mu.Lock()
		wasCur := a.cur == s
		if wasCur {
			a.cur, a.peer = nil, nil
		}
		a.mu.Unlock()
		s.close()
		if wasCur { // a replaced session must not stop the new one's sound
			a.audioClose()
		}
		a.screenStop()
		a.kvmHalt()    // host: stop capturing so the PC's mouse is never stuck on a vanished laptop
		a.kvmRelease() // laptop: let go of any keys/buttons held when the link dropped
		if !a.stopped() {
			log.Print("Disconnected")
		}
	}()

	rx := &receiver{inbox: a.dirs.inbox}
	defer rx.abort()
	clip := startClipboard(a.clipOn.Load, func(t string) { s.send(msg{T: "clip", Text: t}) })
	defer clip.stop()
	if s.send(msg{T: "hello", Name: hostname(), Platform: runtime.GOOS}) != nil {
		return
	}
	for {
		typ, payload, err := s.read()
		if err != nil {
			if !a.stopped() {
				a.mu.Lock()
				paired := a.peer != nil
				a.mu.Unlock()
				if !paired {
					log.Printf("Connection from %s dropped before pairing (%v). The pairing codes probably differ.", a.peerIP, err)
				} else if err.Error() != "EOF" {
					log.Printf("Connection problem: %v", err)
				}
			}
			return
		}
		if typ == typFrame {
			a.screenFrame(payload)
			continue
		}
		if typ == typAudio {
			a.audioData(payload)
			continue
		}
		if typ == typChunk {
			rx.chunk(payload)
			continue
		}
		var m msg
		if json.Unmarshal(payload, &m) != nil {
			continue
		}
		switch m.T {
		case "hello":
			a.mu.Lock()
			a.peer = &m
			serving, ip := a.ln != nil, a.peerIP
			a.mu.Unlock()
			log.Printf("Connected to %s (%s)", m.Name, m.Platform)
			a.sendScreenSize()
			a.audioSendWant(s)
			if serving {
				go a.fastProbe(s, ip)
			}
		case "clip":
			if a.clipOn.Load() {
				clip.apply(m.Text)
			}
		case "file-start":
			rx.start(m.Name)
		case "screen-size":
			a.kvmPeerSize(m.W, m.H)
		case "udp-ok":
			a.fastAck(m.Size)
		case "kvm":
			a.kvmRecv(m.Ev)
		case "input":
			a.input(m.Ev)
		case "file-end":
			rx.end()
		case "audio-start", "audio-stop", "audio-err":
			a.audioMessage(m)
		case "audio-want":
			a.audioWant(m)
		default:
			a.screenMessage(s, m)
		}
	}
}

func hostname() string { h, _ := os.Hostname(); return h }
